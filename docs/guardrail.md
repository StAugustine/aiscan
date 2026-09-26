# Tool guardrail

Tracking: chainreactors/cyber-harness#158.

## Architecture

The core mechanism is installed by BaseExtensions. All ToolRegistry calls,
including the main agent, subagents, ToolNode and direct Executor callers,
already enter core/tool/hooks.Execute. The guardrail subscribes to tool.before
before tool extensions load. It runs before the tool implementation has started.
Direct session commands beginning with ! also dispatch through ToolRegistry;
they must never call BashTool.Execute directly.

    Executor → tool.before → core/guardrail.Runtime.Admit
                              → registered risk screen(s)
                              → record: continue
                              → review/block: core interaction mode
                                  safe → wait → authorize/reject/expire/cancel
                                  auto (default) → provider consequence check
                                      → harmless: execute once
                                      → harmful/uncertain: error ToolResult → LLM continues

core/guardrail has no dependency on JEV, agents, CLI or Web. Its only business
payloads are Decision and Review, defined in proto/types/guardrail.proto and
generated once for Go and TypeScript. Review reuses aop.ToolCall and
aop.operation.Ref. Protocol request/reply wrappers add no mirrored execution
context, approval ID, replay token or provider metadata.

pkg/exts/guardrail owns lifecycle/configuration and the CLI/AOP adapters.
pkg/exts/jev registers two CheckFunc values (screen and confirm), implements JEV's choice API and owns its
private wire types, policy presets, credentials, HTTP budget and fallback.
No provider can call the executor through this integration.

Interaction mode belongs to core/guardrail, not JEV. A single Mode string
(`auto` or `safe`; legacy `off` maps to `auto`) configures the existing runtime; no additional business DTO,
approval tool or parallel agent loop is introduced. The original Decision and
its reason remain intact when a blocked call is offered for human authorization.

## Admission and review

- Each invocation is checked independently, using private copies of the original
  call. No cached allow decisions or tool-name bypass lists exist.
- Checks run in registration order. Severity is block > review > record; the
  first decision wins ties. Block short-circuits, review does not.
- In safe mode, both review and block judgments pause the exact
  invocation until a human authorizes or rejects it. Authorization executes it
  once; rejection/expiry returns a normal tool error to the agent.
- In auto mode (the default), flagged calls receive a second provider judgment
  about the actual consequences of the exact arguments and working directory.
  Only RECORD (demonstrably harmless) allows execution. REVIEW (uncertain) and
  BLOCK (harmful) return a nonterminal error ToolResult; the agent loop continues.
  Low-risk calls need only the first request. Every new invocation is checked.
- Each provider confirms only its own flagged calls; one provider cannot erase
  another provider's denial. Both stages receive isolated copies of the original
  invocation. Missing, invalid, canceled or failed confirmation cannot authorize.
- Automatic allowed/denied results reuse terminal Review records with
  resolution_source=auto. They never enter Pending or offer human controls.
  The original risk and consequence criteria are preserved in the reason.
- A never-configured runtime is a no-op. A configured check's error, panic, nil
  decision, invalid action or removal fails closed. Removing a plugin does not
  silently disable enforcement; create a new profile to change policy.
- Approval wakes the original waiting invocation. It neither invokes Executor
  again nor creates an approval Tool. Every attempt has a unique operation ID,
  even if a caller reuses a call ID.
- Approval, rejection, expiry, cancellation and shutdown compete for one state
  transition. Expiry/cancellation are rechecked during resolution and admission.
  Pending queries return copies. Profile shutdown cancels pending reviews and
  active checks, then drains handlers. No runnable approvals are restored from
  history after restart or profile replacement.
- CLI/Web resolve directly through the control channel, outside Session command
  queues. The Web server routes through the stored session-to-node assignment;
  the node adapter restricts operations to that live session and its descendants.
  Core additionally validates the invocation's exact session ownership.
- Decision and review events use the existing AOP stream/JSONL pipeline. JEV
  usage, confidence and latency stay in plugin diagnostics.

## Configuration

JEV is inactive without a credential. Set TYPESAFE_API_KEY in the AIScan process environment
or configure the Secret field extensions.jev.api_key. Do not paste credentials
into issues or tracked configuration. Configuring a key automatically installs
screening, including legacy enabled:false settings. Explicit enabled:true without
a key fails load. Mode selects who assesses consequences, never disables screening.

    extensions:
      guardrail:
        mode: auto  # default: JEV consequence assessment; safe: human assessment
        review_timeout: 5m
      jev:
        model: jev-1.13.0
        level: standard
        timeout: 10s
        on_error: block
        # criteria:
        #   review: "Require review for any active production probe."

level is permissive, standard or strict. criteria optionally overrides the
first-stage record/review/block descriptions; it is trusted operator policy, never tool data.
The second stage uses separate consequence criteria: harmless, uncertain or harmful.
Screening overrides do not predetermine the consequence verdict.
Policy is immutable for a profile. Interaction mode can change in place; each
invocation snapshots it before checking, so a mode change never releases or
cancels an existing review. The default standard
policy records local analysis and authorized low-rate probes, classifies target
modifications/high intensity/unknown effects as review, and explicit destruction,
leakage or harm as block. Strict classifies active probes as review and target
changes/unknown effects as block. The core mode determines how an interception
is handled. Internal policy failure/removal and cancellation always deny.

JEV uses https://api.typesafe.ai/v1/systemone with Bearer authentication. The
timeout applies separately to each stage, including at most two retries, only on
HTTP 429/529. Redirects are refused. Responses are bounded to 1 MiB. Tool inputs
over 64 KiB take the configured fallback instead of losing a dangerous suffix.
Screening failures use on_error (review/block, default block); legacy record now
requires review. Second-stage failures always deny. Invocation or profile
cancellation always prevents execution. Provider failure never silently allows a call.

Requests contain readable tool arguments and compact invocation context, not
conversation history. Known credential fields, common shell credential patterns,
authorization headers and the provider key are redacted. Executable arguments
remain untouched. Redaction is best effort; encoded or arbitrarily named secrets
cannot be guaranteed absent. Assess the external disclosure boundary before
sending enterprise tool data to JEV.

## Operator interface

Interactive CLI prints a notice and supports:

    /guardrail pending
    /guardrail approve <operation-id>
    /guardrail reject <operation-id>

The attached session includes its live descendants. Use --session to select a
different live session explicitly. Approval applies to one invocation only.
Noninteractive runs cannot approve interactively; review waits for a control
client or expires. Only auto mode substitutes provider assessment for human review.

Web keeps approvals inside their originating turn's conversation bubble. Multiple
approvals share one turn shell and remain in chronological order with the tool
segments and agent continuation: tool call → approval → continuation. The existing
response boundary preserves these inner steps; it does not create extra bubbles
or end the agent turn. Each approval keeps a stable operation identity, so resolving
one updates that step without moving it or hiding another pending approval. Child
session approvals stay inside their delegated session's turn. Unmatched reviews
remain visible until their originating tool events arrive.
Each new user turn gets its own card. Every human review leaves one durable,
expandable audit record in that card, including repeated calls and later turns
in the same conversation. Automatic reviews use the same record and include both
screening and consequence criteria. A newer review never replaces an earlier operation.
The record shows its command, outcome, decision reason and resolution time;
raw payloads remain secondary details, like the payload of a tool invocation.
The outer turn card counts distinct intercepted operations, including automatic
interceptions and human reviews, and shows how many still await intervention.
Passing decisions are excluded. A decision and its later review share one
operation record and never count twice. Clicking the counter focuses the first
pending review, or the first record when no reviews are pending. Resolving an operation changes its
status without increasing the total; later turns maintain independent counts.
Commands are displayed directly; additional parameters are labeled,
and raw JSON plus operation/session identifiers stay in a collapsed details panel.
The sidebar marks sessions needing intervention, including inactive sessions after
refresh. Only the live runtime pending query enables approval buttons.

Pending and terminal Review messages are canonical AOP extension payloads, persisted
through the existing Web event broker. Each operation has one inline record that
transitions to authorized, rejected, expired or canceled. Completed cards retain the
command preview and server event time in a compact expandable row after refresh
and session switches; full commands and raw details remain available on expansion.
Pending approval controls are always expanded, outside collapsed tool output.
Authorization is recorded separately from tool execution success. The same
record derives execution success/failure/cancellation from existing tool results
and operation completion events, scoped to the exact operation. It never infers
success from authorization alone.
Terminal events win over stale pending events, and replay alone never recreates an
actionable approval. Older decisions dropped before this event fix cannot be
reconstructed and are not fabricated.

The header has one guardrail menu: automatic (default) or safe. Once JEV is configured,
mode-only saves update the current core runtime through the existing config path
on the server and remote nodes, preserving connections, running sessions and
pending reviews. Legacy off settings select automatic mode and cannot bypass
screening. Environment-key presence appears only as secret metadata, never as
an editable credential value. Initial provider activation and policy/credential changes
still build and validate a new profile.
Settings → Guardrail configures the interaction mode, policy, model, timeouts, provider
failure action and secret. A blank key retains the stored secret or uses
TYPESAFE_API_KEY on the server. Connection testing makes a real judgment request
for an inert local-read description; no tool runs and fallback never counts as
a successful connection.

## Recovery and feedback

Guardrail Decision and Review payloads are persisted as canonical protobuf JSON
in SQLite chat_aop_events.event_json through the existing AOP archive. No separate
approval table is needed. Records retain session/turn/operation identity, command,
screening and consequence policies, state, event time and resolution source.
The database write precedes live publication; replay deduplicates event IDs and
does not restore executable approvals. Restart tests cover automatic allow,
harmful/uncertain denial, human approval/rejection, expiry and cancellation.
This archive is not an atomic transaction with tool execution: storage failures
are logged by the event broker, and node disconnect/process loss can interrupt
delivery. It does not guarantee an audit entry during every infrastructure fault.

A disconnected control channel retains the last known pending records and sidebar
badges, marks them unavailable and disables both resolution buttons. Pending
queries have bounded deadlines. Reconnection re-queries authoritative runtime
state before re-enabling actions. Resolution requests are never queued while
disconnected; transport retries cannot replay an authorization automatically.

Repeated identical automatic interceptions are counted per session/turn/tool,
working directory and canonical arguments, with bounded memory and short,
cancellable backoff. The denial tells the LLM the attempt count and asks it to
change its approach. Every invocation is still checked; no allow decision is
cached and the agent context stays active.

JEV reasons are displayed as matched policies, not generated explanations of
individual commands. The existing reason includes the configured model, level
and a stable digest of the exact criteria used. Model/criteria version stays in the expandable details. No extra audit DTO is introduced.
Human authorization records and execution outcomes remain separate facts. Review
adds one optional resolution_source field, supplied by the trusted CLI/control
adapter, or auto for provider assessment. This identifies the channel, not a named reviewer: shared-token auth
cannot establish a personal identity. Older records keep the field absent.

## Boundary

This is tool admission, not a complete operating-system sandbox. A raw PTY input
or arbitrary effects inside a started child process are outside this boundary.
JEV judgments are probabilistic; scoped credentials, rate limits, network policy
and other existing execution controls remain necessary for production targets.

## Validation

Tests cover the shared Execute boundary, merge/isolation, duplicate call IDs,
approval/rejection/timeout/cancellation/close, exact session ownership, session
descendants, CLI/AOP resolution without Session queueing, Web node routing,
JSONL payload serialization, JEV mock HTTP/retries/fallbacks/redaction, Secret
configuration roundtrip and default extension composition. Normal tests use
dummy credentials and mock HTTP. Explicitly set CYBER_JEV_LIVE_TEST=1 and
TYPESAFE_API_KEY, then run go test ./pkg/exts/jev -run TestLiveJEV -v -count=1
to validate record/review/block against the real provider. The live test sends
synthetic descriptions; no production target or destructive command executes.

The opt-in web/frontend/e2e/guardrail-live.spec.ts runs real model-driven echo
commands against a dedicated running Web instance. Set BASE_URL, ACCESS_KEY,
CYBER_E2E_NODE and CYBER_GUARDRAIL_LIVE_E2E=1, then run
npx playwright test e2e/guardrail-live.spec.ts from web/frontend. Both provider
credentials stay on the server. The test temporarily installs explicit marker
criteria, verifies authorization of review AND block decisions, rejection,
pending-review refresh, inactive-session badges, the single header control, multiple
independent approvals within one turn (including replay), and automatic assessment
of harmless flagged calls. The agent-loop regression verifies continuation after
a consequence denial. It checks durable tool results,
checks mode changes while approval is pending, connection loss/recovery,
automatic interception counts and linked execution outcomes, captures UI
screenshots, and restores both JEV policy and core mode in test
teardown, including when a test times out. Run it only on a dedicated instance:
profile reload cancels active work and pending reviews.
