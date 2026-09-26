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
                              → registered CheckFunc(s)
                              → record: continue
                              → review/block: core interaction mode
                                  safe → wait → authorize/reject/expire/cancel
                                  auto → error ToolResult → LLM continues

core/guardrail has no dependency on JEV, agents, CLI or Web. Its only business
payloads are Decision and Review, defined in proto/types/guardrail.proto and
generated once for Go and TypeScript. Review reuses aop.ToolCall and
aop.operation.Ref. Protocol request/reply wrappers add no mirrored execution
context, approval ID, replay token or provider metadata.

pkg/exts/guardrail owns lifecycle/configuration and the CLI/AOP adapters.
pkg/exts/jev registers a CheckFunc, implements JEV's choice API and owns its
private wire types, policy presets, credentials, HTTP budget and fallback.
No provider can call the executor through this integration.

Interaction mode belongs to core/guardrail, not JEV. A single Mode string
(`safe` or `auto`) configures the existing runtime; no additional business DTO,
approval tool or parallel agent loop is introduced. The original Decision and
its reason remain intact when a blocked call is offered for human authorization.

## Admission and review

- Each invocation is checked independently, using private copies of the original
  call. No cached allow decisions or tool-name bypass lists exist.
- Checks run in registration order. Severity is block > review > record; the
  first decision wins ties. Block short-circuits, review does not.
- In safe mode (the default), both review and block judgments pause the exact
  invocation until a human authorizes or rejects it. Authorization executes it
  once; rejection/expiry returns a normal tool error to the agent.
- In auto mode, both review and block immediately return a nonterminal error
  ToolResult containing the reason and the fact that nothing executed. No human
  review is created, context is not canceled, and the existing agent loop can
  reason about the result and choose its next action. Every new call is checked
  again; auto mode does not grant a bypass or silently run the intercepted call.
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

JEV is disabled by default. Set TYPESAFE_API_KEY in the AIScan process environment
or configure the Secret field extensions.jev.api_key. Do not paste credentials
into issues or tracked configuration. An enabled plugin without a key fails load.

    extensions:
      guardrail:
        mode: safe  # safe: human authorization; auto: feedback to the LLM
        review_timeout: 5m
      jev:
        enabled: true
        model: jev-1.13.0
        level: standard
        timeout: 10s
        on_error: block
        # criteria:
        #   review: "Require review for any active production probe."

level is permissive, standard or strict. criteria optionally overrides the
record/review/block descriptions; it is trusted operator policy, never tool data.
Policy and interaction mode are immutable for a profile. The default standard
policy records local analysis and authorized low-rate probes, classifies target
modifications/high intensity/unknown effects as review, and explicit destruction,
leakage or harm as block. Strict classifies active probes as review and target
changes/unknown effects as block. The core mode determines how an interception
is handled. Internal policy failure/removal and cancellation always deny.

JEV uses https://api.typesafe.ai/v1/systemone with Bearer authentication. The
timeout covers the complete judgment, including at most two retries, only on
HTTP 429/529. Redirects are refused. Responses are bounded to 1 MiB. Tool inputs
over 64 KiB take the configured fallback instead of losing a dangerous suffix.
Malformed responses and provider errors use on_error (record/review/block,
default block). Invocation or profile cancellation always prevents execution.
Choosing on_error: record explicitly accepts execution when JEV is unavailable.

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
client or expires. No automatic approval is performed.

Web shows pending reviews with sanitized arguments, reason and expiry. It queries
current pending state on mount, reconnection, review events and periodically.
Historical events only trigger a refresh and never recreate approval buttons.

The header has a global guardrail switch and a safe/automatic mode button. They
update the JEV enabled setting and core guardrail mode respectively through the
existing profile reload flow, canceling running tasks and pending reviews.
Settings → Guardrail configures the interaction mode, policy, model, timeouts, provider
failure action and secret. A blank key retains the stored secret or uses
TYPESAFE_API_KEY on the server. Connection testing makes a real judgment request
for an inert local-read description; no tool runs and fallback never counts as
a successful connection.

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
pending-review refresh, header mode switching, and automatic feedback followed
by a safe tool call in the same agent turn. It checks durable tool results,
captures UI screenshots, and restores both JEV policy and core mode in test
teardown, including when a test times out. Run it only on a dedicated instance:
profile reload cancels active work and pending reviews.
