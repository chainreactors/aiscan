# Recap

`recap.New()` installs an optional presentation worker. The standard session
profile includes it automatically; there are no recap settings or commands.
The extension owns subscription and shutdown, while `agent/recap` owns collection
and model selection.

- Collect only the current root task between `TurnStarted` and `TurnEnded`.
  Internal model/tool iterations and evaluator retries never trigger a recap.
- Read user/assistant Text parts, tool names/arguments, and textual tool results.
  Reasoning, deltas, media and protocol frames never enter the input buffer.
- Pin a bounded copy of the user goal and retain recent work up to an estimated
  10,000 input tokens, including the prompt (UTF-8 bytes / 4). Long individual
  records retain their beginning and end; older records leave the buffer first.
- Use the current provider's model catalog to select a recognized small text
  model, preferring the default model's family. Unsupported catalogs or unknown
  models use the configured default. Cache the selection until the provider or
  default model changes. An unavailable candidate (403/404/model_not_found)
  falls back once; transient errors do not cause extra requests.
- A bounded queue and one worker keep model calls off the task path. Catalog
  lookup has a 2-second deadline; generation and fallback share 15 seconds.
  Failure, queue overflow and shutdown silently skip this optional annotation.

The only new wire payload is `types.Recap { text }`, carried in an AOP extension
with the original session and turn IDs. It persists in event history but never
enters the resumed model conversation. Web renders it as subdued plain text
below that turn's final response, including after delayed delivery or replay.
The interactive terminal prints a dim `✻ summary · elapsed` line once after
completion. Elapsed time stops at task completion, excluding recap latency.
Local and remote readline consoles insert it above the prompt, preserving the
draft and cursor. If a new task has already begun, the old recap is discarded.
Static and quiet output omit it; task completion never waits for the annotation.
