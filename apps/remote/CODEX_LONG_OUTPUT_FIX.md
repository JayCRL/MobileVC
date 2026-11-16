# Codex Long Output Fix

Date: 2026-05-15
Repo: `C:\Users\29573\Desktop\fsdownload\MobileVC`

## Problem Summary

On mobile, short Codex replies worked normally, including short Markdown,
code blocks, and rich-text content.

The visible failure only appeared on long replies, especially prompts like
"output 5000 words" or long rich-text / code-block stress tests.

Observed symptoms:

- The bottom-left busy indicator started normally.
- The "thinking" label and small indicator disappeared before the reply ended.
- The current turn looked like it produced no output at all.
- In some cases, switching away and back or reconnecting caused the missing
  content to appear later.

This was not a generic Markdown rendering failure, because short formatted
replies already rendered correctly.

## What Was Ruled Out

### Not a renderer capability problem

The frontend can already render:

- short Markdown
- short code blocks
- short rich-text replies

Therefore the problem was not "Markdown is unsupported" or "code blocks break
the renderer".

### Not a long-thinking watchdog timeout

The backend stall watchdog in `internal/engine/pty_engine.go` only warns after
60s / 90s of silence and aborts after 120s of silence.

For the long-output cases we inspected, backend logs showed continuous Codex
activity:

- turn started
- assistant deltas kept arriving during the long reply
- turn completed normally

No watchdog warning or forced abort was emitted for the main failing case.

Therefore the main issue was not "thinking too long caused automatic stop".

## Root Cause

Two bugs overlapped.

### 1. Codex assistant deltas were not streamed to the frontend in real time

File:

- `internal/engine/codex_transport.go`

Previous behavior:

- `item/agentMessage/delta` events were appended into `assistantBuffer`
- `appendAssistantDelta(...)` returned `nil`
- no live chunk was emitted to the frontend timeline
- long replies only became visible when the final completed text arrived

This explains why short replies were mostly fine:

- short replies often landed fast enough through the final completed message
- long replies spent much longer in the delta phase, so they looked empty

### 2. Codex active turns were misclassified as `WAIT_INPUT`

Files:

- `internal/engine/pty_engine.go`
- `internal/session/manager.go`
- `internal/session/projector.go`

Previous behavior:

- `CanAcceptInteractiveInput()` only meant the runner still had a writable
  input channel
- that was treated as evidence that the session was waiting for user input
- heartbeat / task snapshot logic could therefore emit `WAIT_INPUT` while a
  Codex turn was still actively generating output

Effect on mobile:

- the UI busy state was cleared too early
- the "thinking" indicator disappeared mid-reply
- the turn looked idle or input-ready even though output was still in flight

## Fix Implemented

### A. Separate "active turn" from "interactive channel"

Added a new engine capability:

- `TurnStateProvider` in `internal/engine/engine.go`

Implemented for Codex via:

- `codexAppSession.HasActiveTurn()`
- `PtyRunner.HasActiveTurn()`

New rule:

- if Codex still has an active turn, session lifecycle stays busy
- writable stdin alone is no longer enough to downgrade the runtime to
  `waiting_input`

### B. Stream assistant delta chunks live

Changed `appendAssistantDelta(...)` in:

- `internal/engine/codex_transport.go`

New behavior:

- still buffer incoming delta text
- but opportunistically flush chunk boundaries using the existing
  `codexDrainAssistantChunks(..., false)` logic
- emit live assistant log chunks before final completion

This keeps the current chunk-boundary policy instead of blindly flushing every
raw token.

### C. Preserve de-duplication on final completed text

The completed assistant text path was not replaced with a naive "emit all".

Existing completed-text reconciliation remains in place so that:

- mid-stream content can appear live
- final completed text does not duplicate the entire reply again

## Files Changed

- `internal/engine/engine.go`
- `internal/engine/codex_transport.go`
- `internal/engine/pty_engine.go`
- `internal/session/info.go`
- `internal/session/manager.go`
- `internal/session/projector.go`
- `internal/engine/pty_runner_test.go`
- `internal/session/manager_extended_test.go`
- `internal/session/projector_test.go`

## Validation Performed

Go installed locally for validation:

- `C:\Users\29573\Tools\go1.25.1\bin\go.exe`

Commands run:

```powershell
& 'C:\Users\29573\Tools\go1.25.1\bin\go.exe' test ./internal/session -run 'TestService_BuildTaskSnapshotEvent_(RunningWaitInput|CodexActiveTurnBeatsInteractiveInput|BusyControllerBeatsInteractiveRunner)'
& 'C:\Users\29573\Tools\go1.25.1\bin\go.exe' test ./internal/engine -run 'TestCodexAppSession(StreamsAssistantDeltasBeforePromptWithoutDuplicateFinalText|ReadLoopHandlesLongJSONLines)'
& 'C:\Users\29573\Tools\go1.25.1\bin\go.exe' test ./internal/session
& 'C:\Users\29573\Tools\go1.25.1\bin\go.exe' build ./cmd/server
```

Results:

- targeted `internal/session` tests: passed
- targeted `internal/engine` tests: passed
- full `internal/session` package tests: passed
- backend build: passed

Note:

- full `go test ./internal/engine` still had unrelated pre-existing failures
  in other areas and was not used as the acceptance gate for this specific fix

## Local Run Used For Manual Verification

The fixed backend was built locally and started from the repo instead of the
previous globally installed packaged backend.

Health check:

- `http://127.0.0.1:8001/healthz`

Local logs:

- `C:\Users\29573\Desktop\fsdownload\MobileVC\.local-run\server-stdout.log`
- `C:\Users\29573\Desktop\fsdownload\MobileVC\.local-run\server-stderr.log`

## Expected Improvement

After this fix:

- long Codex replies should remain in a busy/running state while the turn is
  actually active
- the mobile UI should receive visible reply content during the long delta
  phase
- final completed text should still reconcile without replaying the entire
  message twice

## Remaining Watch Items

This fix addresses the main backend causes, but there are still related areas
worth monitoring:

1. frontend heartbeat / snapshot UI damping:
   if the UI still clears too aggressively under reconnect pressure, a
   frontend-side guard may still be useful

2. network instability on mobile:
   some sessions in logs also showed forced WebSocket closures from the remote
   client side; that can amplify user-visible confusion, even when the backend
   is healthy

3. Windows runtime process list:
   unrelated to the long-output fix, but still a known Windows bug:
   `ps`-based process enumeration should be replaced by a Windows-native
   implementation

## Other Issues Found During This Investigation

The items below were observed during the same debugging session but are not all
fully resolved yet.

### A. Windows runtime process list uses Unix `ps`

Status:

- pending fix

Symptoms:

- opening the runtime process / log viewer can emit:
  `list processes: exec: "ps": executable file not found in %PATH%`

Root cause:

- `internal/session/process.go` directly runs:
  `ps -axo pid=,ppid=,stat=,etime=,command=`
- this assumes a Unix-like process listing interface
- Windows does not provide that command shape

Impact:

- runtime process tree viewer is broken on Windows
- the failure currently leaks into the main session error surface, which is
  noisy and confusing

What still needs to be done:

- replace the process tree implementation with a Windows-native solution
  (PowerShell / WMI / Win32 process enumeration)
- keep process-viewer failures scoped to the process/log panel instead of
  polluting the main chat timeline

### B. Stop flow surfaces extra error noise

Status:

- pending fix

Symptoms:

- after tapping stop, the UI can show:
  - `context canceled`
  - `command exited with code 1`
  - even though a normal `stopped` message already appeared

Root cause:

- user-triggered stop emits a proper stopped event
- runner shutdown still allows follow-up cancel / exit errors to surface as
  red error cards

Impact:

- stopping looks like a failure even when it succeeded

What still needs to be done:

- distinguish user-initiated stop from genuine execution failure
- preserve the `stopped` state while suppressing expected cancel/exit noise

### C. Mobile WebSocket stability / reconnect noise

Status:

- pending validation

Symptoms:

- logs showed several mobile-side socket closures such as:
  `wsarecv: An existing connection was forcibly closed by the remote host`

Notes:

- these disconnects were not the primary cause of the long-output bug
- however, they can amplify confusion by forcing reconnect, history replay,
  or delayed visibility of replies

What still needs to be done:

- verify whether the current mobile client still drops the socket during
  long foreground replies after the long-output backend fix
- if it still happens, inspect ping / health timer / foreground transitions
  on the Flutter side

### D. Codex native session discovery under project CWD

Status:

- pending deeper validation

Symptoms:

- expected desktop Codex history for a project directory may not appear in the
  mobile session list even when the user believes those conversations belong to
  that project

Likely cause:

- current Codex native thread filtering appears to require exact normalized CWD
  equality
- a thread created from a child directory or a different exact normalized path
  may be excluded

What still needs to be done:

- inspect actual native Codex thread CWD values against the project directory
- decide whether filtering should support descendant/parent matching instead of
  only exact equality

### E. Final mobile-side verification of the long-output fix

Status:

- pending validation

What has already been confirmed:

- the patched backend now streams long Codex delta output
- targeted backend tests pass
- backend build succeeds
- the user reported the patched service already looks better than before

What still needs to be verified on device:

- the busy indicator should stay visible during a real long reply
- visible assistant content should appear before final completion
- long formatted replies should complete without looking empty
- repeated 5000-word stress tests should not regress back into silent turns

Suggested manual validation:

1. connect the phone/iPad to the patched local backend
2. send a long Codex prompt such as a 5000-word rich-text / code-block sample
3. watch whether:
   - `思考中` remains during the active turn
   - partial content appears before the final completed message
   - final content appears once without the session looking blank mid-turn

## Status Summary

### Fixed

- Codex long-output delta streaming to the mobile timeline
- Codex active-turn vs `WAIT_INPUT` misclassification in backend snapshot
  logic

### Pending validation

- on-device confirmation that long replies now remain visible and active
- whether mobile WebSocket stability still introduces reconnect confusion
- whether any frontend-side busy-indicator guard is still needed

### Pending fix

- Windows runtime process list implementation
- stop-flow noise (`context canceled`, `command exited with code 1`)
- possible Codex native session CWD matching improvements

## Mainline And Branch Split

To keep the project usable while continuing deeper debugging, the work is
split as follows.

### Mainline baseline

The mainline branch should keep only the already validated long-output fix:

- live Codex assistant delta streaming
- backend active-turn tracking vs `WAIT_INPUT`
- the passing targeted/session tests for that fix
- this root-level investigation record

This baseline is intended to remain the current usable default because it
already improved the user's long-reply experience and built cleanly.

### Dedicated follow-up branch

A separate branch should be used for the unresolved stop/send state bug around
reconnect and resumed Codex sessions.

That branch should focus on:

- why a resumed session can show a red stop button after the reply has already
  completed
- why the stop button may fail to turn red while the assistant is actively
  streaming output
- how to separate:
  - PTY/session process still alive
  - turn actively generating output
  - waiting for next user input
- how to suppress expected stop noise (`context canceled`, `command exited with
  code 1`) without hiding genuine failures

### Reverted experiment

One attempted round tried to repair the stop/send issue by adjusting:

- frontend stop/send button state derivation
- restored `executionActive` handling
- `WAIT_INPUT` execution semantics

That attempt was not accepted because the user reported it did not solve the
real issue reliably enough. It should not remain in the mainline baseline.

## Short Conclusion

The long-output failure was not caused by Markdown itself and was not mainly a
"thinking too long -> auto-pass" timeout.

It was primarily caused by:

- missing live Codex delta emission
- incorrect `WAIT_INPUT` inference while a Codex turn was still active

Both backend issues were fixed in this change set.
