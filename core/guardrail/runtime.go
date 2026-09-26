// Package guardrail enforces tool admission independently of policy providers
// and presentation. Approval only releases the original, waiting invocation.
package guardrail

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
// the provider's risk classification. Each invocation snapshots the mode.
type Mode string

const (
	ModeSafe Mode = "safe"
	ModeAuto Mode = "auto"
	// ModeOff is a legacy value. It now selects automatic review, never bypasses checks.
	ModeOff Mode = "off"
)

// These are private dispatch state, not protocol DTOs. Each provider confirms
// only risks it identified; an allow from another provider cannot erase a denial.
type checkInput struct {
	call toolhooks.CallEvent
	mode Mode
}
type checkResult struct {
	risk, decision *Decision
}

var checks = hooks.NewPoint[checkInput, checkResult]("guardrail.check").
	WithErrorPolicy(hooks.FailClosed).
	WithReducer(func(acc *checkResult, _ *checkInput, next checkResult) bool {
		if acc.risk == nil || next.risk.Action > acc.risk.Action {
			acc.risk = next.risk
		}
		if acc.decision == nil || next.decision.Action > acc.decision.Action ||
			(next.risk.Action > Action_ACTION_RECORD && next.decision.Action == Action_ACTION_RECORD && acc.decision.Action == Action_ACTION_RECORD) {
			acc.decision = next.decision
		}
		return next.decision.Action == Action_ACTION_BLOCK
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
	rejections    map[[32]byte]int
}

func New(stream *events.Stream, timeout time.Duration, mode Mode) *Runtime {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if mode == "" || mode == ModeOff {
		mode = ModeAuto
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{stream: stream, timeout: timeout, mode: mode, registry: hooks.New(), ctx: ctx, cancel: cancel, pending: make(map[string]*pending), rejections: make(map[[32]byte]int)}
}

// Register installs a risk screen and an optional consequence assessment.
// In auto mode, only RECORD from confirm releases a flagged invocation.
// A missing confirm retains the screening decision and cannot grant a bypass.
func (r *Runtime) Register(source string, check, confirm CheckFunc) (*hooks.Subscription, error) {
	if strings.TrimSpace(source) == "" || check == nil {
		return nil, errors.New("guardrail source and check are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("guardrail is closed")
	}
	judge := func(ctx context.Context, fn CheckFunc, ev toolhooks.CallEvent) (*Decision, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d, err := fn(ctx, cloneCall(ev))
		if err != nil {
			return nil, errors.New("guardrail check failed")
		}
		if d == nil || d.Action < Action_ACTION_RECORD || d.Action > Action_ACTION_BLOCK {
			return nil, errors.New("guardrail check returned an invalid decision")
		}
		d = proto.Clone(d).(*Decision)
		d.Reason = RedactText(d.Reason)
		return d, nil
	}
	sub := checks.On(r.registry, source, func(ctx context.Context, input checkInput) (checkResult, error) {
		risk, err := judge(ctx, check, input.call)
		if err != nil {
			return checkResult{}, err
		}
		result := checkResult{risk: risk, decision: risk}
		if risk.Action != Action_ACTION_RECORD && input.mode == ModeAuto && confirm != nil {
			result.decision, err = judge(ctx, confirm, input.call)
		}
		return result, err
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
	closed, installed, mode := r.closed, len(r.subscriptions), r.mode
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
	var result checkResult
	var err error
	if !r.available(installed) {
		err = errors.New("guardrail policy was removed")
	} else {
		result, err = checks.Emit(ctx, r.registry, checkInput{call: ev, mode: mode})
		d = result.decision
	}
	invalid := err != nil || d == nil || !r.available(installed)
	if invalid {
		d = &Decision{Action: Action_ACTION_BLOCK, Reason: "Guardrail policy unavailable or invalid"}
	}
	if ctx.Err() != nil {
		d = &Decision{Action: Action_ACTION_BLOCK, Reason: "Invocation canceled"}
	}
	d.Reason = RedactText(d.Reason)
	if !invalid && result.risk != nil && ctx.Err() == nil {
		r.emit(ctx, result.risk, ev)
	} else {
		r.emit(ctx, d, ev)
	}
	// Broken policy registration and cancellation cannot be overridden by a
	// human decision; only a valid provider judgment can enter review.
	if invalid || ctx.Err() != nil {
		return denied(d.Reason), nil
	}
	var admission toolhooks.Admission
	if mode != ModeSafe && mode != ModeAuto {
		return denied("invalid guardrail mode"), nil
	}
	if mode == ModeAuto && result.risk.Action != Action_ACTION_RECORD {
		state := ReviewState_REVIEW_STATE_REJECTED
		if d.Action == Action_ACTION_RECORD {
			state = ReviewState_REVIEW_STATE_APPROVED
		}
		audit := proto.Clone(result.risk).(*Decision)
		if d != result.risk {
			audit.Reason += "\nConsequence assessment: " + d.Reason
		}
		r.emit(ctx, &Review{Call: SanitizeCall(ev.Call), Operation: ev.Operation,
			SessionId: operation.InvocationFromContext(ctx).SessionID, Decision: audit,
			State: state, ResolutionSource: "auto"}, ev)
	}
	if d.Action != Action_ACTION_RECORD {
		if mode == ModeAuto {
			// The executor returns a normal error ToolResult. It does not cancel
			// the session or terminate the agent loop, which can choose its next step.
			repeat := r.repeatedRejection(ctx, ev)
			if repeat > 1 {
				timer := time.NewTimer(time.Duration(min(repeat-1, 8)) * 250 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return denied("guardrail invocation canceled"), nil
				case <-timer.C:
				}
			}
			return denied(fmt.Sprintf("Guardrail intercepted this tool invocation (%d identical attempts in this turn); the tool was not executed. Reason: ", repeat) + d.Reason +
				" Do not repeat the same invocation. Reassess the risk and choose a safer next action or explain the limitation. Every new tool call is checked again."), nil
		}
		admission = r.review(ctx, ev, d)
	}
	// A synchronous observer or pending approval may outlive a policy teardown.
	if ctx.Err() != nil || r.ctx.Err() != nil || !r.available(installed) {
		return denied("guardrail invocation canceled or policy removed"), nil
	}
	return admission, nil
}

// SetMode affects future invocations only. It never releases existing reviews.
func (r *Runtime) SetMode(mode Mode) error {
	if mode == "" || mode == ModeOff {
		mode = ModeAuto
	}
	if mode != ModeSafe && mode != ModeAuto {
		return errors.New("invalid guardrail mode")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("guardrail is closed")
	}
	r.mode = mode
	return nil
}

// Keep only bounded diagnostic counters, never cached admission decisions.
func (r *Runtime) repeatedRejection(ctx context.Context, ev toolhooks.CallEvent) int {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.TurnID == "" {
		return 1
	}
	args := string(ev.Call.GetArguments().GetData())
	var value any
	if json.Unmarshal([]byte(args), &value) == nil {
		if canonical, err := json.Marshal(value); err == nil {
			args = string(canonical)
		}
	}
	key, _ := json.Marshal([]string{inv.SessionID, inv.TurnID, ev.Call.GetName(), ev.Call.GetWorkingDirectory(), args})
	hash := sha256.Sum256(key)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.rejections[hash]; !exists && len(r.rejections) >= 1024 {
		for old := range r.rejections {
			delete(r.rejections, old)
			break
		}
	}
	r.rejections[hash]++
	return r.rejections[hash]
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
	if source := operation.InvocationFromContext(ctx).Emitter; source == "cli" || source == "control" {
		p.review.ResolutionSource = source
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
	encoded, err := anypb.New(payload)
	if err != nil {
		return
	}
	event := &aop.Event{
		SessionId: invocation.SessionID, TurnId: invocation.TurnID, Emitter: "guardrail",
		Payload: &aop.Event_Extension{Extension: encoded},
	}
	if ev.Operation != nil {
		ref, _ := anypb.New(ev.Operation)
		event.Extensions = append(event.Extensions, ref)
	}
	r.stream.Publish(event)
}
