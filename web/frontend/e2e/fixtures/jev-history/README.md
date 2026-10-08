# Retained JEV event fixtures

`live-events.json` and `live-protocol.jsonl` preserve the fourth real DeepSeek/JEV
experiment, including rejected compilation. `profile-events.json` comes from
the production profile integration test with simulated inference. They serve
different assertions and do not replace real failures with simulated success.
Text line endings are normalized to LF; event data is unchanged.

JEV UI tests use these fixtures by default. Set `JEV_EVENTS_FILE`,
`JEV_REPLAY_LOG` or `JEV_PROFILE_FLOW_EVENTS` to replay a new run instead.

`reflex-reuse-events.json` retains the recorded `fresh-reuse-1` Playwright Reflex run (eight judgments and five tool calls). Only the historical Question/Answer envelope is normalized to the current typed Claim/Evaluation schema: option keys and selected values are unchanged, and the original option descriptions remain in Claim context. Event identities, timestamps, native calls and results are unchanged.
