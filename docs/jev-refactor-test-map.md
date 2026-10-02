# JEV Simplification And Test Mapping

This refactor reduces duplicate state and shared execution paths in
`pkg/exts/jev`. Public APIs, configuration, prompts, judgment categories, request
budgets, native execution boundaries and library version 3 remain compatible.
Context-budget changes and browser prerequisite judgment changes are outside
this refactor.

## Implementation

| Area | Previous maintenance burden | Current ownership |
| --- | --- | --- |
| Observation | Parallel content and read maps, eagerly allocated tool calls | One candidate map of `binding`; allocate the selected call at dispatch |
| Result projection | Repeated parsing and encoding in history, summaries and private evidence | `normalizedResult` supplies normalized text and parsed data; original evidence remains intact |
| Background work | Queue and latest-state map both hold declarations | Queue holds task keys; `queued` owns the latest declaration; `latestDeclaration` reads it |
| Task state | Repeated lock, identity check and record write | `updateTask` guards each update with the current task identity |
| Library mutation | Each caller mutates shared memory and implements rollback | `updateLibrary` clones definitions, saves atomically, then publishes memory |
| Compilation status | Separately maintained `Compiled` markers | `publishedGroups` derives the JSON/status compatibility field from published Reflexes |
| Compilation | One large function mixes preparation, drafts, review and persistence | `compile.go` separates preparation, bounded generation, semantic review and publication; creation and repair share the pipeline |
| Verification | Independent projection and repeated pure evaluations for verification and witnesses | One `observationReplay` per draft shares projection and successful evaluations |

Replay preserves the two existing samples: verification checks entry plus the
latest 15 boundaries; review witnesses use the first 16 boundaries. Only
identical normalized observation environments share successful evaluations.
Nonzero omitted-evidence values remain distinct from absent values. Wording
probes run independently, and cancellation is checked before cache access.
Replay never dispatches native tools.

## Merged Tests

| Previous tests | Destination | Preserved cases |
| --- | --- | --- |
| `TestCompilerSourceEnvelopeKeepsCodeAndRejectsSurroundingProse`, `TestCompileRawCodePreservesProgramAndRejectsExtraOutput` | `observation_protocol_test.go`: `TestDecodeReflex` | Raw source, both JavaScript fences, prefixes, null, Unicode, regexes, malformed fences, prose, metadata and extra output |
| `TestObserveHasNoToolOrHostExecutionAccess`, `TestJavaScriptObserveHasNoIOAndStopsOnBudget`, function-entry budget case | `observe_test.go`: `TestObserveSandbox`, `TestObserveInvalidBindingYieldsWithoutDispatch` | Tool/command execution, filesystem, network, clock, randomness, both loop forms, cancellation, candidate limits and explicit boolean read flags |
| `TestPureFunctionProgramEntryProducesAndBoundsActualObservation` | `observe_test.go`: `TestObserveProgramEntry` and `TestObserveSandbox` | Expression/function entry, actual facts and bindings, unbounded evaluation |
| `TestResultJSONDoesNotInventMissingOrIncompleteFacts`, `TestObserveNormalizesWrappedDataAndRetainsOriginalEvidence`, `TestResultSummaryKeepsActualPayloadBeyondLongEnvelope` | `context_test.go`: `TestResultNormalization` | Plain/incomplete/invalid data, trailing prose, objects, arrays, encoded objects, short/long program echoes, exact payload, large integers and unchanged raw evidence |
| `TestLegacyToolObserversRetainClaimsForRecompilation`, `TestLibraryMigrationRetiresExprWithoutLosingClaimsOrJavaScript` | `library_migration_test.go`: `TestLibraryMigration/v1`, `/v2` | Legacy sources and Expr retirement, retained JavaScript and consumed Claims, exact backup bytes, independent recompilation and repeat restart |

Tests with different integration behavior remain separate: native observation
protocols, binding validation, pending effects, no replay, guardrail denial,
repair admission, empty-library discovery and browser workflows.

## Shared Fixtures And Relocated Tests

- `fixtures_test.go` owns the actual extension host, fake JEV service, test
  provider, scene helpers and idle waiting. Ordinary cleanup waits for background
  work before closing the host, so successful requests finish before teardown.
- `live_fixtures_test.go` shares native Live host setup, executed-action counting
  and report writing. Generic and complex Live entry points, environment flags,
  timeout/default settings, report fields and scenario outcome oracles remain.
  Action counts accumulate across all JEV receipts instead of overwriting the
  complex suite's count with the last receipt.
- Pure compiler verification tests keep their names in `verify_test.go`.
- `TestNativeArgumentsRemainOpaque` moves to `store_test.go`.
- `TestProjectionBudgetsStructuredResultsBeforeReaderEchoes` moves to
  `context_test.go`.
- `TestConnectionUsesSectionValidationBeforeRequest` moves to
  `connection_test.go`; its separate file is removed.

## Added Regression Coverage

- Failed library mutations and atomic saves preserve in-memory definitions and
  remove temporary files.
- Published/retired scenes determine `compiled` in snapshots and persisted JSON.
- Background cancellation drains merged queued work and releases idle waiters.
- Long replay histories retain both sampling policies, distinguish omitted
  evidence, reuse equivalent environments and respect cancellation.

## Validation

The following local regressions passed on Windows with Go 1.26.1:

```powershell
go test ./agent/... ./core/tool/... ./pkg/exts/native ./pkg/exts/terminal ./pkg/exts/jev ./pkg/exts/guardrail -skip Live -count=1 -timeout=5m
go test -race ./pkg/exts/jev ./agent/provider/jev ./core/tool/... -skip Live -count=1 -timeout=5m
go test -tags full,sqlite ./pkg/exts/jev -skip Live -count=1 -timeout=5m
```

All JEV paid Live switches were unset for the full/sqlite run. The three main
prompt constants and existing declaration, decision and verification judgment
texts were compared against the pre-refactor sources. No paid inference tests
were run. Coverage collection is unavailable in the installed Go toolchain
(`go: no such tool "cover"`); no coverage percentage is claimed.
