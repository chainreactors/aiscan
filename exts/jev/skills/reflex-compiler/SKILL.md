---
name: reflex-compiler
description: Compile and iteratively repair reusable synchronous Reflex programs from recorded native task evidence. Use for Claim compilation, replay mismatches, uncertain-effect recovery, parameter binding and completion-validation failures.
---

# Compile by observing and repairing

You own a compilation job, not the user's foreground task. Keep repairing until
validate_reflex accepts the artifact. There is no three-draft rule. Each rejected
submission is evidence about the defect, not a verdict that the capability is
impossible. Cancellation or provider failure preserves an unqualified candidate.

## Establish the supported capability

Read the supplied scope, native tool schemas, command usage and trajectory.
Use inspect_evidence when an argument, prerequisite, operation identity or result
is unclear. It exposes real joined results and decoded argv, including exact
Unicode and backslashes. A shell command is a bash tool argument; a command name
is not another tool name. Never dispatch foreground operations from compilation.

List the required current arguments, native effects, read operations and final
evidence. Compile the entry boundary actually being validated: if the user starts
with a URL and no browser session, include the documented opening operation and
derive its current session handle. Do not silently assume an example session.
The same function is evaluated at intermediate boundaries too. Recover completed
opening/creation and its current handle from context.history before dispatching a
new effect. A user URL is an argument; a handle created by the tool is a result,
not a missing user parameter. Inspect the current operation and continue only the
remaining work at each boundary. Do not reopen a submitted workflow simply because
the function is invoked again.
context.history is the snapshot at entry to this invocation. It does not grow
when execute returns. Keep the actual execute result in a local variable and
process that fresh result directly; never re-scan the old snapshot expecting the
new read to appear. Consuming every recorded call and then returning defer is
not a complete replay and cannot qualify the entry path.
If evidence is unavailable, explain exactly which capability or actual result is
missing. Changing a read flag or inventing a response cannot fill that gap.

## Build the artifact

Supply api_version:2, observe as a synchronous js:function(context,args), steps,
optional parameters_schema/readers, and arguments as the exact current example.
Use args or current native results for all task values. Guard all required
arguments together before external work. Example values are never runtime defaults.
JSON-encode the source and argument strings once; compare decoded strings, not
their escaped appearance. command(program, argv) handles shell encoding.

Semantic decisions use jev({type:"choice",context:"meaning and current facts",
options:["candidate","defer"]}) and return an option string directly. score uses
ordered levels and returns a weighted index; noul has no options and returns a
probability. Claim content has no question/criteria/request envelope. Keep current
evidence in the temporary context, not in a published reusable Claim.

The ordinary jev command can read the library, publish a typed Claim or request
compilation from host-recorded evidence. A function that uses these library effects
must declare the jev-library contract, step and occurrence just like every other
native effect. It receives no bootstrap identity or validation exemption.

Each write declares its step's tool-owned contract and count/count_argument.
Use read:false, that step ID, and an explicit zero-based occurrence. Two intended
identical writes are two distinct occurrences. Reads and polls use read:true and
do not need an effect step. The host ledger protects operation identities; history
length is not proof that a business action completed.

For uncertain effects, recover the native host call ID from the actual result or
history and inspect that same operation. Keep polling while fresh evidence says
pending; inspect is_error and the real field names. Returning defer hands control
to the main model immediately: it does not schedule another Reflex invocation.
If completion needs several reads, implement that progression inside the function.
Return defer when the required inspection capability is unavailable or outcomes
cannot be established, preserving the prior effect.

Return report only after all promised work is established. Requested counts,
targets and receipts must come from current results or grounded computation.
Returning only the actor does not satisfy a request for a count and receipt.
Use {evidence: actualCallId, path:["data","field"]} where appropriate.
Paths use string object keys and nonnegative integer array indices, for example
["data","elements",0,"text"]. Index only the actual current array; return the
current computed field directly when transformation is needed.

## Repair from the diagnostic

Submit every draft to validate_reflex. It checks syntax, native classification,
recorded replay and independent semantic review in the same path. Acceptance ends
the job; do not rewrite a program that has already passed validation.

Read diagnostic.code, stage, status, action, expected and actual:

| Diagnostic | Next step |
| --- | --- |
| native_call_mismatch | Compare every decoded argv value and native option with expected. Inspect exact current evidence; fix double escaping, wrong targets, missing entry work or wrong call order. |
| trajectory_incomplete | Inspect replayed/recorded and the next expected result. Implement missing polls/reads/effects. Do not return early or treat defer as continuation. |
| completion_missing | The calls replayed, but the function still handed off. Process fresh execute return values and return the requested grounded report; entry history is a snapshot. |
| unrecorded_native_call | Use already available evidence if the read is redundant. A necessary alternative execution path requires its own actual trajectory; do not fabricate it. |
| example_arguments_invalid | Supply all current example fields used by guards and calls. Inspect the trajectory to recover exact values. |
| effect_identity_invalid | Fix the manifest, declared step, explicit occurrence and requested multiplicity. |
| native_access_invalid | Correct the read/effect operation or helper, using the native contract. |
| semantic_validation_failed | Repair the specific progress, completion, authorization or evidence defect. Passing replay alone is insufficient. |
| recorded_evidence_unavailable | Retain the candidate and the precise missing evidence; do not call it qualified. |
| recorded_capability_unavailable | The recorded operation itself lacks a trusted contract. Wait for a supported real trajectory or native tool contract; code retries cannot split opaque compound results or invent operation identities. |

Change the cause identified by the diagnostic, then submit again. Prior drafts
and validation results remain in your Agent history. Final-text artifacts receive
the same validation and repair feedback as tool submissions. A null final output
means you cannot support the capability from available tools/evidence; state that
gap during the investigation rather than abandoning an ordinary code defect.
