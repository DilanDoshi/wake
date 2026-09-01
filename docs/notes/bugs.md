# Bugs seen in real use

Defects the owner hit **running the build**, as opposed to work put off or parity gaps. Three notes
files, three questions:

| File | Asks |
|---|---|
| `deferred.md` | what did we consciously put off |
| `claude-code-gap.md` | what does Claude Code do that we do not |
| **this file** | what did somebody watch go wrong |

An entry earns its place by having been *observed*, not reasoned about. That is the whole
distinction: `HANDOFF-NEXT.md` exists because the first real run hit basic UI bugs none of 2,342
tests caught, and this is where the next one goes.

**Rule for this file, inherited from `deferred.md`:** an entry leaves only when it is fixed or when
somebody decides it never will be, and the decision is written down. And **before acting on an
entry, check it still describes the tree** — four of the last dozen entries in `deferred.md` did not
by the time somebody read them.

**Say which part is a decision.** Most of what looks like a bug in this tree is a ruling meeting a
case it did not anticipate. An entry that reports a symptom without separating the two sends the
next person to delete a guard that was right.

**Status — 2026-08-25.** A merge pass landed thirteen fixed bugs and dropped their entries per the
rule above: **BUG-5, 11, 13, 14, 18, 19, 20, 22, 23, 24, 26, 27, 28** (PRs #91–#97, #105–#109, #111).
**BUG-28** closed only the crash-recovery clear it named; the `shutdown` clean-exit clear stays
deferred (see `deferred.md`). **BUG-16** is held — its supervisor (PRs #104/#113) rewrites the core
session lifecycle and collides with `main`'s own evolution of it, so it wants a rebase by its author
rather than a blind merge. **BUG-25** is partly overtaken by merged #103 and #105 — see its entry.

---


## BUG-10 — the picker takes four keys the legend says belong to something else, and says nothing

**Measured 2026-08-23**, by driving `App` directly rather than by reading `pickerKey`:

```
alt+down:  picker cursor 0 -> 1     (the legend reads `⌥↑↓ prompt history`)
alt+enter: picker open true -> false (⌥↵ is Wake's newline key)
picker view mentions any key glyph: false
```

`pickerKey` switches on `m.Type` alone and never consults `m.Alt`, so `⌥↑`, `⌥↓` and `⌥↵` are taken
by the menu. **Its two siblings both release the modifier deliberately** — `board.go` has a comment
saying so, and `cardkeys.go` checks `m.Alt` twice — so this is one of three implementations
disagreeing rather than an unconsidered case.

`↑↓` and `↵` are the sharper half. While a picker is open they move the choice and confirm it, and
the legend under the composer goes on advertising `↑↓ pick agent` and `↵ send`. **A card in the same
position draws its own key line**, and `cards.go`'s rule is quoted in `CLAUDE.md`: *"The key line is
honest in both directions."* `Picker.View` draws a header and option rows and no key line at all.

**What is decision and what is not.** That a picker claims `↑↓`/`↵` is deliberate and right — it is
a menu. That it claims them **while the only visible key line says otherwise** is the defect, and
`⌥` is not a decision at all: nothing chose it, the switch simply does not look.

*Cheapest honest fix:* release `m.Alt` in `pickerKey` the way `board.go` does, and give
`Picker.View` the key line `optionRow`'s neighbour already draws.

---

## BUG-12 — the dispatch list is unbounded and unclipped, so a short pane draws more rows than it was given

**Read from the code 2026-08-23, not yet reproduced on a screen.**

`DM.taskRowCount` is `len(rows) + 1` with no cap, and `Tasks` keeps finished dispatches, so the
count only grows. It is counted into `baseChrome`, which means it is honest about its own height —
and `DM.minHeight` therefore grows with it. What does not happen is anybody consulting that floor
when the grid splits a column: `App.paneFloor` has exactly one caller, and it is the card sizing in
`appview.go`, not the row split.

So a stacked pane (`⌃B`) in a short terminal can be handed fewer rows than its conversation needs
once that agent has dispatched anything. The frame's final cut keeps the alt screen safe — this is
not the scrolling failure — but it takes the rows off the **bottom**, which is the awareness strip
and the notice line first, and the composer and status bar as the list grows.

**The menu block in the same position is clipped** (`DM.menuRows`), which is what makes this an
inconsistency rather than an unconsidered case.

*What would settle it:* a pty test at 15 rows with a stacked pane and one dispatched task — the
harness can do this, and a screen is the only thing that can show which rows went missing.

---


## BUG-15 — `TestAnOversizedLineEndsTheSessionAndReapsWhatItSpawned` fails in a full run and passes alone

**Observed 2026-08-23**, and settled the same day by running the gate on both sides.

```
--- FAIL: TestAnOversizedLineEndsTheSessionAndReapsWhatItSpawned (12.03s)
    procgroup_unix_test.go:138: events channel never closed: the scan gave up and
        nothing ended the process it stopped reading
```

**It is `main`'s, and that is measured rather than assumed.** It failed twice in `make ci` on a
branch that changes `DecodeLine` — which is exactly the path this test exercises, so the branch was
the obvious suspect — and then failed on **pristine `origin/main` at `4246c01` with the identical
duration**, `make ci` exiting 2 on both sides with the same two tests. Isolated it passes: `-count=3`
on the branch, and the whole `internal/core` package with `-race`.

**It is the fourth entry in one class**, after BUG-3 (fixed), BUG-6 and BUG-7: a pty or process test
that passes alone and fails inside a whole-suite run. BUG-7's entry already names what they have in
common — `settle()` and its relatives are being used as *"the work is done"* when they only mean
*"the frame stopped moving"* — and this one is the same shape one layer down: the assertion is a
channel close, and the wait gives up rather than waiting for the thing that would close it.

**Why this one matters more than its severity suggests.** `make ci` is the only gate this repository
has, and it now fails on `main` for reasons that have nothing to do with any diff. Every branch
opened today has had to run the gate on `main` as well, by hand, to find out whether its own red was
real — which is a manual ownership investigation per PR, and exactly the state BUG-3's entry warned
about when it said nobody can tell a real regression from this.

*What would settle it:* the class, not the instance. Sweep `cmd/wake` and `internal/core` for waits
that stand in for completion — `settle()` before a negative assertion, a fixed timeout on a channel,
an `await` on a string that appears before the work finishes — and give the harness the primitive
BUG-7 says is missing: a wait on a **count**, and a wait on a process having ended.

---

## BUG-16 — a daemon has two ways to end and neither is "nothing left to supervise"

**Observed 2026-08-23** while cleaning leaked processes off the machine: **21 `wake daemon`
processes**, the oldest **10 days old**, and **28 orphaned scratch socket directories** under
`$TMPDIR`. Fifteen are from the pty harness's own temp dirs; the rest are real.

**`beginQuit` has exactly two callers**, both in `dispatch`: `rpc.FrameStop` (`wake stop`) and
`rpc.FrameParkAll` (`⌃Q`). There is no idle timeout, no empty-fleet exit, and `dropClient` does
nothing but delete the client. So a daemon outlives:

- every session being parked with `⌃C` and the client detaching — the book has records, no record
  has a process;
- every session ending on its own;
- the last client going away with nothing running.

**This is a design rule meeting a case, not a mechanism that failed.** The daemon is *supposed* to
outlive its client — that is what detach means, and `⌃O ↵` leaving the fleet alive is the whole
point. What was never decided is what happens when there is nothing left to outlive it *for*. And
fleets multiply the effect: each is its own directory and its own socket, so `wake --fleet a` and
`wake --fleet b` are two immortal daemons.

**Severity — and this entry got it wrong the first time.** It originally read *"accumulation, not
cost"*, on the grounds that every one of the 21 measured **0.0% CPU**. That measured the wrong
resource. A daemon is idle; **what it is holding is not.** Re-measured 2026-08-23:

```
12 leaked daemons → 26 live `claude` children → 4.48 GB resident
```

And each of those daemons is a **ticker**: `silenceLimit` is 5 minutes, so `livenessInterval()`
clamps to `livenessCheckMax` of 30s and every daemon wakes twice a minute for the life of the
process whether or not it holds anything. That is consistent with the 0.0% CPU above —
`probeQuietAgents` spawns nothing unless an agent is due — but it is the "process on a timer" shape
the second non-negotiable names, sitting inside a population that never ends.

Every `claude` under a daemon nobody will ever quit is an agent nobody will ever stop, and a headless
`claude` is tens to hundreds of MB. The daemons are free; the fleet they are keeping alive is not.
That is the severity, and it follows directly from the daemon having no way to end rather than being
a separate problem.

**The test suite is the accelerated version of what a person does**, which is why it shows up there
first — but the reason is narrower than this entry first claimed, and the first claim would have sent
somebody to build a thing that already exists.

*It said:* "`startWake` gives each pty test its own scratch socket and nothing stops the daemon
afterwards." **That is false.** `cmd/wake/screen_unix_test.go` has a `t.Cleanup` that runs `wake stop`
on the scratch socket, and `main_test.go` removes the built binary after `m.Run()`. `git log -S` puts
both in the harness's founding commit; they have always been there.

*What is true:* *the cleanup does not survive the test binary dying.* A package timeout, a `go test`
interrupted with `⌃C`, or a SIGKILL skips every `t.Cleanup` **and** the line after `m.Run()`. That is
the ordinary outcome of an agent abandoning a slow `make ci`, and it is why the machine carries 22
`wake-screen*` build directories (**373 MB**, one 17 MB binary each) and 28 `waket*` socket
directories against a harness that tidies up correctly on the happy path.

*What would settle it — a decision, not a patch, and it belongs to the owner:*

- exit when the last client disconnects **and** no session has a live process (closest to what detach
  already means, and it leaves a fleet with parked records alive only while somebody is attached —
  which may be wrong, since `/resume` is exactly the verb you attach to use);
- or an idle timeout, which is a process on a timer and the second non-negotiable says why that is
  not free;
- or leave it and give the harness a `t.Cleanup` that stops its daemon, which fixes the tests and
  none of the product.

---

## BUG-29 — `TestTheWholeLifecycleComposesFromAKeyboard` waits 15s for a woken agent, in a full run

**Observed 2026-08-23**, in the `make ci` immediately after PR #86, #87 and #88 merged together.

```
--- FAIL: TestTheWholeLifecycleComposesFromAKeyboard (15.33s)
    lifecycle_unix_test.go:189: waited 15s for the woken agent's answer
        (any of ["agent heard: zebrafish"]) and none appeared.
```

**Not caused by any of the three, and PR #87 is the one that had to be cleared** — it added two pty
tests to this package, so it is the obvious suspect for pushing a marginal timeout over. Measured
instead of argued: the whole package re-run on the merged tree passes (**192.2s**), and on `34f6e38`
— the commit before #87 — passes with the same shape (**189.5s**). Three seconds between them, which
is the two new tests, and nowhere near the margin a 15s wait would need. Isolated it passes 3 of 3.

**Fifth entry in the class**, after BUG-3 (fixed), BUG-6 (fixed), BUG-7 (fixed) and BUG-15. It is not
the same *mechanism* as the three that were fixed — those read `settle()` as "the work is done", and
this one has an honest wait — but it shares the property that makes the class expensive: **it only
fails when the whole package runs, so the gate is the only place it appears and the gate is the one
thing that has to be trustworthy.**

*What is different here, and what would settle it:* the bound is a **hard-coded 15s** in the test
rather than the harness's `screenTimeout`, so it does not move with the harness and nobody editing
`screenTimeout` would know. A wake resumes a real scripted process; under a loaded full run that can
exceed 15s while being perfectly correct. Either the wait belongs to the harness — one constant,
raised once — or this test needs the same treatment the class got: wait on the thing rather than on a
clock. Nobody has measured how close to 15s a healthy wake actually runs, and that number is what
decides which.


---


## BUG-31 — `TestAHungWorktreeAddHasItsTreeReapedOnTheDeadline` races git against its own 2s test deadline, under load

**Observed 2026-08-26**, in `make ci` runs during unrelated work (the PR #122 kill-switch fix, which
touches no daemon code). Reproduced on `origin/main`'s daemon code — the branch that saw it changes
zero lines under `internal/daemon`.

```
--- FAIL: TestAHungWorktreeAddHasItsTreeReapedOnTheDeadline (17.10s)
    worktreespawn_unix_test.go:155: the post-checkout hook never recorded a pid at …/childpid
```

**A test-harness flake, not a feature bug — the feature it covers works.** The test hangs a
`git worktree add` with a post-checkout hook (`sleep 60 & echo $! > pidFile; wait`) and asserts the
daemon's git deadline bounds the add *and reaps the whole tree* (the `sleep 60` the hook spawned). To
run in seconds it compresses the deadline with `shortGitTimeout(t, 2*time.Second)` — production
`gitTimeout` is **2 minutes** (`worktree.go`). Under a loaded full run (`make ci`'s `-race` daemon
step, memory pressure) `git worktree add` takes longer than that artificial **2s** just to *reach*
the post-checkout hook, so the 2s deadline fires and kills git's group **before the hook ever runs**.
The deadline did its job — the hung add was bounded — but git died before spawning the tree the test
wants to watch get reaped, so the hook never records a pid and `recordedPid` (which polls for 15s)
gives up at line 155. Isolated it passes 4 of 5; under the gate's `-race` daemon step it fails
reliably.

**No live-user impact.** In production the deadline is 2 minutes and a real `git worktree add` reaches
its hook in seconds, so the deadline only ever fires on a genuinely hung add (a blocking hook, an NFS
stall) — which is exactly what it is for. The flake is the *test's* compressed 2s bound losing a race
to git-under-load, which the 2-minute production bound cannot lose.

**Same class as BUG-29** — a wall-clock bound in a test that only loses the race under a full loaded
run, so the gate is the one place it appears and the gate is the thing that must be trustworthy.

*What would settle it:* the test needs git to reliably *reach* the hook before the deadline fires,
then hang there. Either raise the compressed deadline enough to clear a loaded checkout (and keep it
well under the hook's `sleep 60`), or gate the deadline's start on the hook having recorded its pid —
so the bound measures the *hang*, not the checkout. The current 2s measures both.


---




## BUG-21 — a fleet directory is never removed, and the 24-name pool is keyed on directory existence

**Observed 2026-08-23: 9 of 24 fleet names are consumed on this machine, 5 of them by directories
with no daemon and no socket.**

`nextFleetName` computes "first free" from a `ReadDir` of `~/.wake/fleets/`. `wake stop` removes the
roster, the park book and the socket — and not the directory, the lock, the log or `mcp.json`. So a
stopped fleet holds its name forever, `wake fleets` lists it forever with no state column to tell it
from a live one, and **there is no verb that removes a fleet**. Past 24 the pool numbers to
`harbor-2`.

`FleetSocketPath` also creates the directory **before the command is validated**, so `wake --fleet
typo bogus` leaves one behind.

This is BUG-16 one level up and the same shape: `beginQuit` has no idle exit and `wake` has no
fleet-removal verb, so **both halves of a fleet's lifetime are create-only.** The ruling that a
directory with no socket is still a fleet is deliberate and right; what was never decided is whether
a fleet is ever *done*.

---



## BUG-25 — every *ordinary* ending leaves the process group alive, and then deletes the only record of it

**Mostly overtaken 2026-08-25.** Merged **#103** made `retire`'s clean non-park exit sweep the group
(`core.KillGroup(a.sess.Pgid())`), so the normal-exit path this opens with no longer leaks; and merged
**#105** (the old BUG-28) closed the un-huntable-record half, keeping the roster records a failed reap
could not finish. What is left is whether the other endings it names (`Stop`/`FrameStop`, `wake stop`)
still leak the group — re-check that against the tree before dropping this.

**Found 2026-08-23.** Which endings kill the group: context cancel, an oversized line, `agent.kill()`,
and shutdown past `quitGrace`. Which do not: **normal exit, `Stop`/`FrameStop`, park (`⌃C`),
park-all (`⌃Q`) and `wake stop`.** Every killing path is a failure path; every ordinary path reaps
the leader only.

That much is documented — `session.go` says *"anything a cleanly ended session left behind is
nobody's to reap. Wake does not hunt it."* **The unrecorded half is what happens next:** `retire`
and `completePark` remove the roster row, and the roster is `reapOrphans`' only input, while the park
book carries no PID by design. So a group with a live member and a dead leader is not merely
un-hunted — it is **un-huntable**, by this daemon, by `wake status`, by `wake stop` and by every
future daemon.

`deferred.md` records the first half and is now stale on the mechanism: it says the pgid *"must be a
pgid recorded on `Session` at spawn"*, and `Session.Pgid()` exists and is deliberately never cleared
for exactly this caller. The item is one call from closable.

---


## BUG-32 — `⌘←` / `⌘→` do nothing in the composer, where macOS puts line-start / line-end

**Observed 2026-08-28** typing into the query/composer: `⌘←` and `⌘→` — the macOS reflex for
jump-to-line-start and jump-to-line-end — move the cursor nowhere.

**Most of this is a ruling, not a mechanism that failed, and it is the same ruling the pane keys
turn on.** No macOS terminal transmits `⌘` to a tty, and bubbletea v1.3.10 names *nothing* for
`⌘`+arrow: its arrow table knows modifier params 2–8, `⌘` is bit 8, so `⌘→` is param 9 and falls off
the end. That is the measured reason moving the keys between panes is `⇧`+arrow and not the `⌘`+arrow
that was asked for — `TestNoKeyIsACtrlArrow`, `keyprobe_test.go`, and the write-up in
`decisions.md`. So the bytes never reach Wake: there is no `App.key` case or composer binding that
*could* act on `⌘←`, and adding one would bind a key that is never delivered — the exact trap that
once shipped the grid keys as unpressable chords.

**What works instead, and the one part that is a genuine casualty.** The composer overrides only
`InsertNewline`; everything else is the bubbles textarea's default keymap, so `⌃A` and `Home` reach
it as line-start, `End` as line-end (where the terminal sends them), and `⌥←` / `⌥→` are word
movement (needs Option→Meta, e.g. iTerm2's *"Esc+"*). **The exception is `⌃E`**: the textarea binds
it to line-end by default, but `App.key` takes `tea.KeyCtrlE` for expand (the documented ⌃E
collision) before the composer sees it — so the readline line-end chord is shadowed, and `End` is the
only line-end that fires.

**Which part is a decision.** The `⌘`-never-arrives half is settled and not fixable in Wake's key
layer, identical to the pane keys. The residual worth a ruling is the `⌃E` shadow, which leaves
line-end with no chord where the terminal sends no `End` key (a bare MacBook keyboard: `End` is
`Fn+→`).

**What would settle it:** either (a) accept it and document that macOS line-movement in the composer
is `⌃A` / `Home` / `End` / `⌥←→` (a `live-testing.md` note, since no `go test` has a `⌘` to press),
or (b) a Wake terminal-setup that remaps `⌘←/→` to deliverable `ESC`-prefixed sequences — the same
terminal-config side effect the `⇧↵` entry in `deferred.md` weighs and does not reach for.

---

## BUG-33 — a subagent's row in the right sidebar reportedly named `general-purpose` for a dispatch that was not

**Reported 2026-08-31**, secondhand: a session dispatching a named subagent (a custom
`subagent_type`, not the built-in default) showed `general-purpose` on its row in the right sidebar
instead. No transcript of the session was available to this investigation.

**Audited and not reproduced.** The whole path was read end to end: `core.taskUpdate` reads
`subagent_type` off `task_started`/`task_progress` unconditionally into `TaskUpdate.Type`
(`internal/core/protocol.go`); `Tasks.updated` (`internal/ui/tasks.go`) applies it to `Task.Type`
only when the frame carries a non-empty value, so `task_updated`/`task_notification` — which carry
none — cannot clobber what `task_started` set; and `subagentRow`/`subagentName`
(`internal/ui/rostersubs.go`) reads `Task.Type` first, with no fallback that could read as
`general-purpose` (`subagentUnnamed` is `"subagent"`). No cross-wiring, no keying bug: rows are keyed
on `task_id`, exact, including under two concurrent dispatches.

**Three live dispatches were then recorded against a real `code-reviewer` subagent definition** (not
committed — see below) to settle whether the CLI itself was at fault: a single dispatch, two
concurrent dispatches with different `subagent_type`s, and one against a nonexistent type (which the
model declined to attempt, so it settled nothing). In every completed run, `task_started`,
`task_progress` (when it fired) and the tool-use receipt all carried the real subagent type
end to end — never `general-purpose`. **The mechanism this entry was written to find was not
found.**

**Why nothing was committed.** The corpus rule (`CLAUDE.md`'s "Never test against a live LLM... any
session that misbehaves in real use gets recorded") calls for exactly this — record the session that
went wrong. But the session that went wrong was never available to record: the report was
secondhand, the reporter's own transcripts do not carry the frames in question (Claude does not
persist `task_*` system events to the on-disk transcript at all — confirmed by scanning a large local
corpus of real sessions for `type":"system"` subtypes and finding none), and a *fresh* recording that
reproduces the correct behavior is not evidence of the bug — it is evidence against reproducing it
under the conditions tried. Committing it would have added a fixture that asserts nothing this entry
is about.

**What would settle it, if it recurs.** The three live tests above never got a `task_progress` frame
to fire (both dispatches finished before one arrived) — that is the one lifecycle shape this audit
could not exercise, and `TaskUpdate.Type`'s own doc comment already flags it as unverified for
anything but `general-purpose` in the recorded corpus. If it recurs, the one fact worth capturing
under `--debug-file` (`CLAUDE.md`'s per-session logging) is whether a `task_progress` frame for that
dispatch ever carries a *different* `subagent_type` than its `task_started` — that is the only gap
this audit did not close.

---

## Residuals carried from bugs that are fixed and merged

Their entries are gone; `git log -p docs/notes/bugs.md` still has every one in full. What is kept
here is only the part that is **still open**, so this file stays a worklist rather than an archive.

**From BUG-17 (a silent agent is reclaimed now).**
- `rpc.FrameKill` still has no producer. The owner chose automatic reclaim only, which closes the
  harm that entry described. An agent wedged in a way `ps` cannot see still has no operator verb.
- The ending's wording is core's rather than the watchdog's — *"stopped reading stdout: file already
  closed"* instead of *"something it spawned is holding its output open"*. Preferring `unreachable`
  in `snapshot` was tried and reverted: when both are set, `a.err` may carry the leader's real exit
  status and stderr tail, and burying that on the sessions most worth diagnosing is the worse trade.

**From BUG-9 (an agent's output can no longer drive the terminal).** The four existing
`oneLine`-shaped fences are still four, and `core.Contained` added a fifth shape rather than closing
that. `deferred.md` carries the unification.

**From BUG-4 (an ask stays in the conversation that put it).** If a blocked agent ever goes unnoticed
on a narrow terminal, the pane-width path is the first thing to look at — ahead of putting a card
back in the room.

**From BUG-30 (two gap producers, one invalidation).** Both producers now route through `notedGap`
for the permission mode and the turn's tool/counts, and `inDM` is reconciled at the report's
working→idle edge (`Fleet.WithStatus`) rather than on the gap itself — clearing it on the gap would
leak a still-running private turn into the room. The only residual: a gap that eats *both* the
`KindTurnEnd` event and the working→idle report for the same turn leaves `inDM` stale until the next
observed turn-end, so that agent's room prose is held back until then. Far narrower than the original,
where a daemon-side gap invalidated nothing at all.
