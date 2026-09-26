// Package guardrail enforces tool admission independently of policy providers
// and presentation. Approval only releases the original, waiting invocation.
package guardrail

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/cyber/aop"
	operationpb "github.com/chainreactors/cyber/aop/operation"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type CheckFunc = func(context.Context, toolhooks.CallEvent) (*Decision, error)

// Mode controls how a valid policy interception is handled, independently of
// the provider's risk classification. It is immutable for a runtime.
type Mode string

const (
	ModeSafe Mode = "safe"
	ModeAuto Mode = "auto"
)

var checks = hooks.NewPoint[toolhooks.CallEvent, *Decision]("guardrail.check").
	WithErrorPolicy(hooks.FailClosed).
	WithReducer(func(acc **Decision, _ *toolhooks.CallEvent, next *Decision) bool {
		if *acc == nil || next.Action > (*acc).Action {
			*acc = next
		}
		return next.Action == Action_ACTION_BLOCK
	})

type pending struct {
	review *Review
	ctx    context.Context
	done   chan struct{}
}

type Runtime struct {
	stream        *events.Stream
	timeout       time.Duration
	mode          Mode
	registry      *hooks.Registry
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	closed        bool
	subscriptions []*hooks.Subscription
	pending       map[string]*pending
	active        sync.WaitGroup
}

func New(stream *events.Stream, timeout time.Duration, mode Mode) *Runtime {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if mode == "" {
		mode = ModeSafe
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{stream: stream, timeout: timeout, mode: mode, registry: hooks.New(), ctx: ctx, cancel: cancel, pending: make(map[string]*pending)}
}

func (r *Runtime) Register(source string, check CheckFunc) (*hooks.Subscription, error) {
	if strings.TrimSpace(source) == "" || check == nil {
		return nil, errors.New("guardrail source and check are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("guardrail is closed")
	}
	sub := checks.On(r.registry, source, func(ctx context.Context, ev toolhooks.CallEvent) (*Decision, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d, err := check(ctx, cloneCall(ev))
		if err != nil {
			return nil, errors.New("guardrail check failed")
		}
		if d == nil || d.Action < Action_ACTION_RECORD || d.Action > Action_ACTION_BLOCK {
			return nil, errors.New("guardrail check returned an invalid decision")
		}
		return proto.Clone(d).(*Decision), nil
	})
	r.subscriptions = append(r.subscriptions, sub)
	return sub, nil
}

func cloneCall(ev toolhooks.CallEvent) toolhooks.CallEvent {
	if ev.Call != nil {
		ev.Call = proto.Clone(ev.Call).(*aop.ToolCall)
	}
	if ev.Operation != nil {
		ev.Operation = proto.Clone(ev.Operation).(*operationpb.Ref)
	}
	return ev
}

func (r *Runtime) Admit(ctx context.Context, ev toolhooks.CallEvent) (toolhooks.Admission, error) {
	r.mu.Lock()
	closed, installed := r.closed, len(r.subscriptions)
	if !closed {
		r.active.Add(1)
	}
	r.mu.Unlock()
	if closed {
		return denied("guardrail is closed"), nil
	}
	defer r.active.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	defer cancel()
	if r.ctx.Err() != nil {
		cancel()
	}
	if err := ctx.Err(); err != nil {
		return denied("guardrail invocation canceled"), err
	}
	if installed == 0 {
		return toolhooks.Admission{}, nil
	}
	ev = cloneCall(ev)
	var d *Decision
	var err error
	if !r.available(installed) {
		err = errors.New("guardrail policy was removed")
	} else {
		d, err = checks.Emit(ctx, r.registry, ev)
	}
	invalid := err != nil || d == nil || !r.available(installed)
	if invalid {
		d = &Decision{Action: Action_ACTION_BLOCK, Reason: "Guardrail policy unavailable or invalid"}
	}
	if ctx.Err() != nil {
		d = &Decision{Action: Action_ACTION_BLOCK, Reason: "Invocation canceled"}
	}
	d.Reason = RedactText(d.Reason)
	r.emit(ctx, d, ev)
	// Broken policy registration and cancellation cannot be overridden by a
	// human decision; only a valid provider judgment can enter review.
	if invalid || ctx.Err() != nil {
		return denied(d.Reason), nil
	}
	var admission toolhooks.Admission
	if r.mode != ModeSafe && r.mode != ModeAuto {
		return denied("invalid guardrail mode"), nil
	}
	if d.Action != Action_ACTION_RECORD {
		if r.mode == ModeAuto {
			// The executor returns a normal error ToolResult. It does not cancel
			// the session or terminate the agent loop, which can choose its next step.
			return denied("Guardrail intercepted this tool invocation; the tool was not executed. Reason: " + d.Reason +
				" Reassess the risk and choose a safer next action or explain the limitation. Every new tool call is checked again."), nil
		}
		admission = r.review(ctx, ev, d)
	}
	// A synchronous observer or pending approval may outlive a policy teardown.
	if ctx.Err() != nil || r.ctx.Err() != nil || !r.available(installed) {
		return denied("guardrail invocation canceled or policy removed"), nil
	}
	return admission, nil
}

func denied(reason string) toolhooks.Admission {
	return toolhooks.Admission{Deny: fmt.Errorf("%w: %s", operation.ErrDenied, reason)}
}

func (r *Runtime) available(installed int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && len(r.subscriptions) == installed && checks.Len(r.registry) == installed
}

func (r *Runtime) review(ctx context.Context, ev toolhooks.CallEvent, d *Decision) toolhooks.Admission {
	id := ev.Operation.GetOperationId()
	if id == "" || ev.Call == nil {
		return denied("review requires an operation and tool call")
	}
	p := &pending{ctx: ctx, done: make(chan struct{}), review: &Review{Call: SanitizeCall(ev.Call), Operation: ev.Operation, SessionId: operation.InvocationFromContext(ctx).SessionID, Decision: d, State: ReviewState_REVIEW_STATE_PENDING, ExpiresAt: timestamppb.New(time.Now().Add(r.timeout))}}
	r.mu.Lock()
	if r.closed || ctx.Err() != nil || r.pending[id] != nil {
		r.mu.Unlock()
		return denied("review unavailable")
	}
	r.pending[id] = p
	snapshot := proto.Clone(p.review).(*Review)
	r.mu.Unlock()
	// Publish after insertion so synchronous consumers may resolve immediately.
	r.emit(ctx, snapshot, ev)
	timer := time.NewTimer(time.Until(p.review.ExpiresAt.AsTime()))
	defer timer.Stop()
	select {
	case <-p.done:
	case <-ctx.Done():
		r.finish(p, ReviewState_REVIEW_STATE_CANCELED)
	case <-timer.C:
		r.finish(p, ReviewState_REVIEW_STATE_EXPIRED)
	}
	r.mu.Lock()
	state := p.review.State
	snapshot = proto.Clone(p.review).(*Review)
	r.mu.Unlock()
	r.emit(ctx, snapshot, ev)
	if state == ReviewState_REVIEW_STATE_APPROVED && ctx.Err() == nil && r.ctx.Err() == nil {
		return toolhooks.Admission{}
	}
	return denied("review " + state.String())
}

// finishLocked is the sole state transition. Cancellation and expiry are
// checked while resolving too, so delayed timer scheduling cannot allow a call.
func (r *Runtime) finishLocked(p *pending, state ReviewState) bool {
	if p.review.State != ReviewState_REVIEW_STATE_PENDING {
		return false
	}
	if r.closed || p.ctx.Err() != nil {
		state = ReviewState_REVIEW_STATE_CANCELED
	} else if !time.Now().Before(p.review.ExpiresAt.AsTime()) {
		state = ReviewState_REVIEW_STATE_EXPIRED
	}
	p.review.State = state
	delete(r.pending, p.review.Operation.GetOperationId())
	close(p.done)
	return true
}
func (r *Runtime) finish(p *pending, state ReviewState) {
	r.mu.Lock()
	r.finishLocked(p, state)
	r.mu.Unlock()
}

// Pending is scoped to an exact session, including the empty direct-call scope.
func (r *Runtime) Pending(sessionID string) []*Review {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Review, 0)
	for _, p := range r.pending {
		if p.ctx.Err() != nil || !time.Now().Before(p.review.ExpiresAt.AsTime()) {
			r.finishLocked(p, ReviewState_REVIEW_STATE_EXPIRED)
			continue
		}
		if p.review.SessionId == sessionID {
			out = append(out, proto.Clone(p.review).(*Review))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Operation.OperationId < out[j].Operation.OperationId })
	return out
}

// Resolve authorizes against caller context, never a tool argument. It does not
// retain an approval token or execute/replay a tool.
func (r *Runtime) Resolve(ctx context.Context, operationID string, approve bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.pending[operationID]
	if p == nil || p.review.SessionId != operation.InvocationFromContext(ctx).SessionID {
		return errors.New("pending review not found in this session")
	}
	state := ReviewState_REVIEW_STATE_REJECTED
	if approve {
		state = ReviewState_REVIEW_STATE_APPROVED
	}
	r.finishLocked(p, state)
	if p.review.State != state {
		return errors.New("review is no longer pending")
	}
	return nil
}

func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	r.cancel()
	for _, p := range r.pending {
		r.finishLocked(p, ReviewState_REVIEW_STATE_CANCELED)
	}
	subs := append([]*hooks.Subscription(nil), r.subscriptions...)
	r.mu.Unlock()
	for _, s := range subs {
		s.Cancel()
	}
	for _, s := range subs {
		if err := s.Close(ctx); err != nil {
			return err
		}
	}
	done := make(chan struct{})
	go func() { r.active.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) emit(ctx context.Context, payload proto.Message, ev toolhooks.CallEvent) {
	invocation := operation.InvocationFromContext(ctx)
	event := &aop.Event{SessionId: invocation.SessionID, TurnId: invocation.TurnID, Emitter: "guardrail"}
	_ = aop.SetTypedExtension(event, payload)
	if ev.Operation != nil {
		ref, _ := anypb.New(ev.Operation)
		event.Extensions = append(event.Extensions, ref)
	}
	r.stream.Publish(event)
}
