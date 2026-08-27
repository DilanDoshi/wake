# Bounded Agent Start Design

**Status:** approved for implementation
**Date:** 2026-08-24
**Scope:** prerequisite for the remaining BUG-16 shutdown guarantee

## Problem

`core.Session.Start` currently holds `Session.mu` while `os/exec.Cmd.Start`
performs the child's pre-exec working-directory change. A working directory on
an unavailable FUSE or network mount can leave that syscall blocked indefinitely.
The daemon has already admitted the agent row at that point, but the session has
not published a process or process group. Shutdown then blocks in `Session.Stop`
waiting for the same mutex and never reaches its grace period or group kill.

The direct launch also leaves no process identity to persist while that risky
working-directory change is in progress. If the daemon dies in that window, the
blocked child is not in the roster and a successor cannot reclaim it.

## Goals

- Give every Unix agent a PID and process group before touching its requested
  working directory.
- Keep `Session.Stop`, context cancellation, and daemon shutdown callable while
  the target directory or target exec is blocked.
- Preserve the existing synchronous launch contract: a bad directory, missing
  `claude`, or failed target exec is a failed launch, not a briefly successful
  roster row.
- Persist the supervisor's PID/PGID before releasing target exec, so a daemon crash
  leaves enough identity for the existing reaper.
- Preserve the exact `claude` argv, environment scrubbing, stdio, stderr tail,
  `WaitDelay`, process-group ownership, fake-agent harnesses, and wake rollback.
- Add no poll, ticker, timeout process, shell wrapper, or arbitrary executable
  launcher.

## Non-goals

- This is not a generic subprocess framework. The supervisor may start only the
  fixed `claude` target.
- This does not make a kernel task in uninterruptible sleep immediately die.
  It makes Wake's own shutdown bounded and leaves the PID/PGID recorded; the
  kernel may finish reaping after the blocked syscall returns.
- This does not change non-Unix process ownership. The `!unix` build keeps the
  current direct-launch behavior, matching its existing lack of process groups.
- This prerequisite does not itself close BUG-16. BUG-16 remains on its own
  branch and is rebased and reverified after this lands.

## Delivery split

The core launcher and Session lifecycle contract land first with the launcher
capability deliberately zero in every daemon Config. Activation cannot safely
land on `origin/main` by itself: a provisional helper placed directly in
`s.agents` looks idle, accepts input before `serveInput` exists, and a failed
wake needs BUG-16's exact park reservation and launch-outcome rollback.

After the prerequisite merges, BUG-16 is rebased and activates the launcher
once, using its existing durability machinery. The paid live journey runs only
on that combined state; running it while the capability is dormant would test
the unchanged direct launch path.

## Architecture

### Opaque launcher capability

`internal/core` exposes an `AgentLauncher` value whose executable path is
private. `SelfAgentLauncher` is the only constructor. `core.Config` can carry
that value, but callers cannot manufacture a different target or launcher path
with a struct literal. The daemon requests the capability; direct core callers
and existing core test seams leave it zero and retain direct launch.

On Unix, the capability names the currently running Wake executable. On
non-Unix platforms it is the zero value and selects the direct path.

### Unix self-reexec supervisor

With the capability enabled, Session starts the Wake executable again in the
daemon's already-open working directory. The first exec has no `Cmd.Dir`, so it
cannot block on the agent's requested directory. Its argv contains the exact
Claude arguments, including Wake's session UUID; the existing reaper can verify
the supervisor for its whole life.

The supervisor remains process-group leader and waits for the fixed `claude`
child rather than replacing itself. This is a deliberate architectural cost:
one small Wake supervisor process remains per active Unix agent. A same-PID
`syscall.Exec` cannot distinguish successful target exec from SIGKILL or runtime
death because all three close every CLOEXEC descriptor. An explicit post-exec
success therefore requires a surviving observer.

After ownership release, the supervisor resolves `claude` through `PATH`, makes
the target absolute, and synchronously changes its own working directory to the
requested `Dir` before a target child exists. It then starts the target with an
empty `Cmd.Dir`, exact argv, scrubbed environment, and the supervisor's
stdin/stdout/stderr. The target inherits the supervisor's cwd and process group.
The supervisor reports target-start outcome, waits for the target, reports
expected completion, and propagates its exit code or signal. The Wake parent
remains the sole `cmd.Wait` caller for the supervisor; the supervisor is the sole
waiter for its distinct target child.

`cmd/wake/main.go` and both test-package `TestMain` entry points dispatch this
private mode before ordinary command or fake-process parsing after BUG-16
activation. Core tests exercise the same supervisor today.

### Explicit control, status, and lifetime protocol

The parent creates three Unix pipes before starting the supervisor:

- fd 3 is CONTROL, parent to supervisor;
- fd 4 is STATUS, supervisor to parent;
- fd 5 is LIFETIME, supervisor to parent.

CONTROL accepts exactly one framed `RELEASE`. Before that frame the supervisor
may validate its private protocol but may not resolve, chdir to, or start the
target. Ownership rejection, cancellation, Stop, or parent death closes CONTROL
without release, so the target can never start.

STATUS is framed and never treats EOF as success:

- `READY` is written only after target `cmd.Start` returns nil, which proves the
  supervisor chdir completed and the local target exec passed Go's synchronous
  start boundary;
- `ERROR` carries one bounded length-prefixed message for target resolution,
  chdir, or start failure;
- the complete READY or ERROR must be followed by EOF; EOF before a frame,
  truncation, trailing bytes, a second frame, an unknown opcode, or a status
  frame before `RELEASE` is a launch failure.

LIFETIME remains open after READY. Once target `Wait` completes, the supervisor
writes exactly one `DONE` byte and closes fd 5 before returning or propagating
the target's exit signal. DONE followed by EOF is expected supervisor exit;
EOF before DONE, truncation, trailing bytes, a second frame, or an unknown
opcode means the supervisor died unexpectedly. The parent then serializes the
group kill, closes its stdout reader, and lets the existing pump reap the
supervisor before the PID can be reused. DONE reporting is best-effort when the
parent is already gone and never changes target Wait or exit propagation.

All three supervisor protocol descriptors are close-on-exec, CONTROL closes
immediately after valid RELEASE, and the target inherits none of them. Of the
launcher-private environment, production scrubs `WAKE_AGENT_LAUNCHER` and
`WAKE_AGENT_LAUNCHER_DIR` before target start. Core-test record, identity, and
gate variables intentionally survive where that harness needs them; they are
not production dispatcher inputs. No shell, arbitrary target, polling, ticker,
or timeout process is introduced.

### Session publication and locking

`Session.Start` remains the ordinary API and delegates to
`StartObserved(ctx, nil)`. `StartObserved` adds one cancellation-aware ownership
callback:

```go
func (s *Session) StartObserved(
    ctx context.Context,
    onProcess func(context.Context, int) error,
) error
```

The callback must make its durable update transactional with the supplied
context. BUG-16's daemon implementation checks that context immediately before
the atomic roster rename and conditionally removes the exact ID/PGID/generation
under the same roster mutex on failure. This prevents an abandoned late
callback from publishing stale ownership.

A non-nil ownership callback requires a nonzero launcher capability. The direct
zero-capability path cannot gate target exec before ownership and remains for
ordinary `Start(ctx)` compatibility only; pretending it offers the stronger
contract would recreate the rejected pre-exec window.

The sequence is:

1. Under `Session.mu`, refuse an already-starting, already-started, or stopped
   session and latch `starting`.
2. Build pipes and start the direct process or Unix helper without holding
   `Session.mu`.
3. Under `Session.mu`, publish `cmd`, `stdin`, and `pgid`, then clear `starting`.
4. Start the log, cancellation, stdout-pump, status-reader, lifetime-reader, and
   reap ownership.
5. Run `onProcess(ownershipCtx, pgid)` once in its own goroutine while the
   supervisor remains blocked on CONTROL.
6. On callback success, recheck context, Stop, and pre-outcome supervisor death;
   then write `RELEASE` and close CONTROL.
7. Wait outside `Session.mu` for explicit `READY` or `ERROR`.
8. Recheck context and stopped state before returning success.
9. After success, keep watching LIFETIME until DONE or unexpected supervisor
   death; the latter terminates the group and closes stdout before pump Wait.

If `Stop` wins before publication, it latches `stopped`; once the helper exists,
Start publishes it, records it through the callback, and tears it down rather
than allowing a process to start after its stop was consumed.

Cancellation or supervisor death cancels the ownership context and never waits
for a blocked callback. The final daemon callback's transactional pre-rename
check prevents late persistence. The stdout pump starts before readiness and
remains the sole supervisor `cmd.Wait` owner.

Failed-start cleanup is ordered:

1. cancel ownership;
2. close CONTROL without release, STATUS, and LIFETIME;
3. clear and close stdin;
4. serialize process-group termination unless pre-outcome death is already
   proven;
5. explicitly close the parent's stdout reader, so an escaped setsid descendant
   cannot hold the scanner forever;
6. open launch settlement so the pump can call `cmd.Wait`;
7. wait for reap only for live-context controlled failures; cancellation and
   possible kernel D-state return after cleanup ownership is handed off.

### Daemon ownership and rollback after BUG-16 rebase

The daemon keeps admit-before-start without publishing a provisional helper as
an active agent. A private `server.starting` map owns the ID, name, Session and
PGID, blocks duplicate admission, and counts toward the live cap, but is absent
from `fleet()` and rejects stdin-bound operations as "still starting."

Same-daemon wakes leave the old parked agent in `s.agents`; record-backed wakes
retain BUG-16's exact park reservation. Readiness success atomically removes the
starting entry and installs/replaces the active agent, then settles BUG-16's
launch outcome before input, fan-out or status success becomes observable.
Failure or shutdown removes the starting entry and settles false; it never
invents a second park-restoration path.

`rosterFile.add` is transactional and daemon recording returns its error through
`StartObserved`'s ownership callback. A helper whose PGID cannot be persisted is
terminated before readiness can succeed. Active and starting sessions are
snapshotted separately during shutdown: starting helpers are cancelled and
settled false, never labelled or booked as ParkAll agents.

## Failure matrix

| Failure | Observable result | Ownership result |
|---|---|---|
| Invalid config or launcher discovery | launch refusal before admission | no process, row, roster record, or name leak |
| Supervisor `cmd.Start` failure | synchronous launch error | no callback; starting ownership aborted |
| Ownership callback failure | synchronous launch error | no RELEASE; supervisor reaped before target start |
| Supervisor death before outcome | explicit launch failure | STATUS EOF is never accepted as READY |
| Supervisor death after READY, before target Wait | asynchronous session failure | LIFETIME EOF kills the target group, closes stdout, and reaps the supervisor |
| Missing `claude` | synchronous framed ERROR | supervisor reaped; durable provisional roster record removed |
| Bad or unavailable directory returning an error | synchronous launch error | same rollback as missing target |
| Directory blocks until shutdown | no target child and no false success frame | killable supervisor owns the blocked chdir and recorded group |
| Target exec succeeds | existing launch success | supervisor stays PGID owner, waits for Claude, then writes DONE |
| Stop races supervisor publication | launch cannot escape the stop | no RELEASE or group teardown; no fan-out success |
| Daemon is SIGKILLed before RELEASE | target never starts | CONTROL EOF ends the supervisor |
| Daemon is SIGKILLed after RELEASE | no daemon cleanup is possible | roster names supervisor UUID and PGID for the next daemon's reaper |

## Verification

- Core Unix integration tests use the package test binary as both Wake helper
  and fixed `claude` target. They assert cwd, argv, private-env removal, fd 3
  through fd 5 framing and non-inheritance, target exclusion before ownership
  and during supervisor chdir, explicit READY/ERROR/DONE, pre- and post-outcome
  supervisor death, synchronous ownership-callback failure, cancellation,
  escaped-stdout cleanup, sole-Wait ordering, exit/signal propagation, and
  direct zero-capability fallback. These gate the prerequisite PR.
- After BUG-16 rebase, real daemon/socket integration tests gate the private
  starting helper before chdir/exec, prove durable PID/PGID ownership without a
  public fleet row, reject pre-ready input, exercise roster write failure and
  ParkAll wake rollback, and assert bounded shutdown with no helper leak.
- On that combined branch, daemon and command-package fake-agent suites run through the helper, so
  spawn, fork, import, resume, manager argv/MCP configuration, stdin/stdout,
  process groups, and TestMain ordering are exercised broadly.
- Run focused tests with and without `-race`, cross-build the `!unix` fallback,
  run full `make ci`, compare its exit code and failures with pristine
  `origin/main`. The combined BUG-16 branch then runs the tagged `make live`
  journey on its scratch socket before its PR opens.
- Before the PR, mutate the lock boundary so readiness is awaited under
  `Session.mu`; the core blocked-readiness test must go red, then pass again
  after restoration.

## Residual

A helper already inside true kernel uninterruptible sleep can retain its kernel
task until the syscall returns even after SIGKILL. Wake cannot change that.
The guarantee here is that Wake publishes the identity first, its own mutexes
and shutdown return, and the existing reaper retains a later path to the group.

One persistent Wake supervisor process per active Unix agent is the explicit
cost of truthful post-exec acknowledgement and target exit propagation. The
same-PID design was rejected because it cannot tell successful exec from helper
death without target cooperation or platform-specific ptrace/pidfd machinery.
After READY, CONTROL and STATUS are closed; the steady additional parent cost is
one LIFETIME pipe reader and one blocked watcher goroutine per active agent.

One standard local-exec micro-window remains after the supervisor chdir: an
external actor that kills only the newly forked target before its binary exec
settles can close Go's exec-error pipe without an errno, so `cmd.Start` may
return nil and READY may briefly precede target `Wait` observing the signal.
Eliminating that parent-only ambiguity portably would require target cooperation
or platform-specific tracing. The supervisor still writes DONE after Wait and
propagates the signal, so the session closes promptly; the residual is the
narrow synchronous false-success window, not an unowned or unreapable process.
