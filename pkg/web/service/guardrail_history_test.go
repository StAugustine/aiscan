package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/guardrail"
	"github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"google.golang.org/protobuf/proto"
)

type guardrailHistoryObserver func(*aop.Event)

func (f guardrailHistoryObserver) ObserveEvent(event *aop.Event) { f(event) }

func TestGuardrailDecisionsSurviveTimelineRestart(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mode        guardrail.Mode
		consequence guardrail.Action
		state       guardrail.ReviewState
		source      string
	}{
		{"safe_approved", guardrail.ModeSafe, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_APPROVED, "control"},
		{"safe_rejected", guardrail.ModeSafe, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_REJECTED, "control"},
		{"safe_expired", guardrail.ModeSafe, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_EXPIRED, ""},
		{"safe_canceled", guardrail.ModeSafe, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_CANCELED, ""},
		{"auto_allowed", guardrail.ModeAuto, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_APPROVED, "auto"},
		{"auto_harmful", guardrail.ModeAuto, guardrail.Action_ACTION_BLOCK, guardrail.ReviewState_REVIEW_STATE_REJECTED, "auto"},
		{"auto_uncertain", guardrail.ModeAuto, guardrail.Action_ACTION_REVIEW, guardrail.ReviewState_REVIEW_STATE_REJECTED, "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			approve := tc.state == guardrail.ReviewState_REVIEW_STATE_APPROVED
			path := filepath.Join(t.TempDir(), "history.db")
			store, err := NewSQLiteStore(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			createStoredSession(t, store, "session")
			service := NewService(ServiceConfig{Store: store})
			defer service.Close(context.Background())
			stream := events.New()
			timeout := time.Second
			if tc.state == guardrail.ReviewState_REVIEW_STATE_EXPIRED {
				timeout = 30 * time.Millisecond
			}
			runtime := guardrail.New(stream, timeout, tc.mode)
			defer runtime.Close(context.Background())
			_, err = runtime.Register("test", func(context.Context, toolhooks.CallEvent) (*guardrail.Decision, error) {
				return &guardrail.Decision{Action: guardrail.Action_ACTION_REVIEW, Reason: "JEV fixture / risk / policy 123456: potential side effect"}, nil
			}, func(context.Context, toolhooks.CallEvent) (*guardrail.Decision, error) {
				if tc.mode != guardrail.ModeAuto {
					t.Fatal("safe mode called automatic confirmation")
				}
				return &guardrail.Decision{Action: tc.consequence, Reason: "JEV fixture / consequence / policy abcdef: " + tc.consequence.String()}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: "session", TurnID: "turn", CallID: "call", Emitter: "control"}))
			defer cancel()
			var emitted []*aop.Event
			stream.Observe(guardrailHistoryObserver(func(event *aop.Event) {
				emitted = append(emitted, proto.CloneOf(event))
				service.BroadcastAOPEvent("session", event)
				var review guardrail.Review
				if payload := event.GetExtension(); payload != nil && payload.MessageIs(&review) && payload.UnmarshalTo(&review) == nil && review.State == guardrail.ReviewState_REVIEW_STATE_PENDING {
					switch tc.state {
					case guardrail.ReviewState_REVIEW_STATE_CANCELED:
						cancel()
					case guardrail.ReviewState_REVIEW_STATE_EXPIRED:
						// Allow the runtime's own approval timer to expire.
					default:
						if err := runtime.Resolve(ctx, review.Operation.OperationId, approve); err != nil {
							t.Error(err)
						}
					}
				}
			}))
			registry := hooks.New()
			toolhooks.Before.On(registry, "guardrail", runtime.Admit)
			executed := false
			args, _ := aop.JSONValue(map[string]string{"command": "echo safe"})
			_, err = toolhooks.Execute(ctx, registry, "bash", string(args.Data), func(context.Context, string) (*aop.ToolResult, error) {
				executed = true
				return &aop.ToolResult{}, nil
			})
			if executed != approve || (approve && err != nil) || (!approve && !errors.Is(err, operation.ErrDenied)) {
				t.Fatalf("execution=%v error=%v", executed, err)
			}
			// Re-delivery of an existing event must not duplicate its audit entry.
			for _, event := range emitted {
				service.BroadcastAOPEvent("session", event)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewSQLiteStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			history, err := reopened.ListAOPEvents(t.Context(), "session", 100)
			if err != nil {
				t.Fatal(err)
			}
			var reviews []*guardrail.Review
			if len(history) != len(emitted) {
				t.Fatalf("stored events=%d emitted=%d", len(history), len(emitted))
			}
			for i, event := range history {
				if !proto.Equal(event, emitted[i]) {
					t.Fatal("database roundtrip changed the event payload or identity")
				}
			}
			for _, event := range history {
				var review guardrail.Review
				if payload := event.GetExtension(); payload != nil && payload.MessageIs(&review) && payload.UnmarshalTo(&review) == nil {
					if event.EmittedAt == nil || event.Id == "" || event.TurnId != "turn" {
						t.Fatal("review lost its durable identity, time or turn")
					}
					reviews = append(reviews, &review)
				}
			}
			wantReviews := 1
			if tc.mode == guardrail.ModeSafe {
				wantReviews = 2
			}
			if len(reviews) != wantReviews || reviews[len(reviews)-1].State != tc.state {
				t.Fatalf("review history=%v", reviews)
			}
			terminal := reviews[len(reviews)-1]
			if tc.mode == guardrail.ModeSafe && reviews[0].State != guardrail.ReviewState_REVIEW_STATE_PENDING {
				t.Fatal("pending record missing")
			}
			if terminal.ResolutionSource != tc.source || terminal.SessionId != "session" || terminal.Decision.Action != guardrail.Action_ACTION_REVIEW {
				t.Fatal("stored review lost its source, session or initial risk")
			}
			if !strings.Contains(terminal.Decision.Reason, "policy 123456") ||
				(tc.mode == guardrail.ModeAuto && !strings.Contains(terminal.Decision.Reason, "Consequence assessment: JEV fixture / consequence / policy abcdef")) {
				t.Fatal("stored review lost a judgment stage or policy version")
			}
			if reviews[0].Operation.OperationId != terminal.Operation.OperationId || terminal.Call.Id != "call" || string(terminal.Call.Arguments.Data) != string(args.Data) {
				t.Fatal("review history lost invocation correlation")
			}
		})
	}
}
