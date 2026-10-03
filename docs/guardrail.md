# Tool guardrail

Tracking: chainreactors/cyber-harness#158.

## Architecture

JEV is a provider of native `choice`, `score` and `noul` primitives. The provider
owns the HTTP protocol, retries, request timeout and usage accounting. `score`
returns a weighted level index; `noul` returns a probability. Their interpretation
belongs to consumers. See the [native API](https://docs.typesafe.ai/api).

`exts/jev` defines Claim/Compile/Reflex and its execution loop over `choice`.
`exts/guardrail` is an independent consumer: it calls native `choice` directly,
without compiling or invoking a Reflex. Its `record/review/block` options, risk
presets and fallback semantics are extension policy, never provider abstractions.

All ToolRegistry calls, including agents, subagents, ToolNode and direct callers,
enter `core/tool/hooks.Execute`. Guardrail installs one `tool.before` handler.
The core owns hook dispatch, admission and cancellation; the extension owns its
policy, consequence assessment, pending reviews and CLI/AOP adapters.

    Executor → tool.before → exts/guardrail.Runtime.Admit
        → JEV choice: risk screen
        → record: continue
        → review/block:
            safe → wait for authorization/rejection/expiry/cancellation
            auto → JEV choice: consequence assessment
                → harmless: execute once
                → harmful/uncertain: error ToolResult; Agent continues

The runtime receives one immutable pair of screening/confirmation functions.
There is no nested policy registry or severity-merging engine. Independent
policies compose through the existing `tool.before` boundary; a denial cannot
be erased by another hook. JEV has no executor access.

Decision and Review remain protobuf messages in the `cyber.guardrail` namespace.
Go bindings live in the Guardrail extension; message names, fields and AOP wire
identity are unchanged. Review reuses `aop.ToolCall` and `aop.operation.Ref`.
The extension owns the interaction mode (`auto` or `safe`), lifecycle and
approval state. The core Agent has no Guardrail dependency.

## Admission and review

- Each invocation is checked independently, using private copies of the original
  call. No cached allow decisions or tool-name bypass lists exist.
- Independent admission hooks run through the existing tool registry. Guardrail
  evaluates one policy, with isolated copies of the original call for each stage.
- In safe mode, both review and block judgments pause the exact
  invocation until a human authorizes or rejects it. Authorization executes it
  once; rejection/expiry returns a normal tool error to the agent.
- In auto mode (the default), flagged calls receive a second provider judgment
  about the actual consequences of the exact arguments and working directory.
  Only RECORD (demonstrably harmless) allows execution. REVIEW (uncertain) and
  BLOCK (harmful) return a nonterminal error ToolResult; the agent loop continues.
  Low-risk calls need only the first request. Every new invocation is checked.
- Confirmation only handles this hook's flagged call. It cannot erase another
  hook's denial. Missing, invalid, canceled or failed confirmation cannot authorize.
- Automatic allowed/denied results reuse terminal Review records with
  resolution_source=auto. They never enter Pending or offer human controls.
  The original risk and consequence criteria are preserved in the reason.
- A never-configured runtime is a no-op. A configured check's error, panic, nil
  decision or invalid action fails closed. Profile shutdown cancels the runtime
  before unregistering its hook; no per-policy removal API exists.
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
  The extension additionally validates the invocation's exact session ownership.
- Decision and review events use the existing AOP stream/JSONL pipeline. JEV
  usage and latency stay in plugin diagnostics.

## Configuration

Guardrail and Reflex are selected independently. `guardrail.provider: jev`
installs JEV screening; `none` disables that policy. `jev.mode` controls only the
Reflex extension. The shared credential comes from `TYPESAFE_API_KEY` or
`extensions.jev.api_key`. Risk level, error policy and criteria belong to
`extensions.guardrail.jev`; a credential alone does not enable screening.

    extensions:
      jev:
        model: jev-1.13.0
        timeout: 10s
        mode: off  # Reflex disabled; Guardrail remains independent
      guardrail:
        provider: jev
        mode: auto  # JEV consequence assessment; safe: human assessment
        review_timeout: 5m
        jev:
          level: standard
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
changes/unknown effects as block. The extension mode determines how an interception
is handled. Internal policy failure and cancellation always deny.

JEV uses https://api.typesafe.ai/v1/systemone with Bearer authentication. The
timeout applies separately to each stage, including at most two retries, only on
HTTP 429/529. Redirects are refused. Responses are bounded to 1 MiB. Tool inputs
over 64 KiB take the configured fallback instead of losing a dangerous suffix.
Screening failures use on_error (review/block, default block).
Second-stage failures always deny. Invocation or profile
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
mode-only saves update the current Guardrail runtime through the existing config path
on the server and remote nodes, preserving connections, running sessions and
pending reviews. Environment-key presence appears only as secret metadata, never as
an editable credential value. Initial provider activation and policy/credential changes
still build and validate a new profile.
Settings → Guardrail configures the interaction mode, policy, model, timeouts, provider
failure action and secret. A blank key retains the stored secret or uses
TYPESAFE_API_KEY on the server. Connection testing makes a real judgment request
for an inert local-read description; no tool runs and fallback never counts as
a successful connection.

## Recovery and feedback

Guardrail Decision and Review payloads are persisted as canonical protobuf messages
in SQLite chat_aop_events.event_proto through the existing AOP archive. No separate
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

Tests cover the shared Execute boundary, hook composition/isolation, duplicate call IDs,
approval/rejection/timeout/cancellation/close, exact session ownership, session
descendants, CLI/AOP resolution without Session queueing, Web node routing,
JSONL payload serialization, JEV mock HTTP/retries/fallbacks/redaction, Secret
configuration roundtrip and default extension composition. Normal tests use
dummy credentials and mock HTTP. Explicitly set CYBER_JEV_LIVE_TEST=1 and
TYPESAFE_API_KEY, then run go test ./exts/guardrail -run TestLiveJEV -v -count=1
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
screenshots, and restores both JEV policy and interaction mode in test
teardown, including when a test times out. Run it only on a dedicated instance:
profile reload cancels active work and pending reviews.
