# Decisions and open questions

Rulings made during Phase 1 that are not obvious from the code, and questions still open.
**Tracked**, and moved here from `notes/` on 2026-08-09.

A decision belongs here when someone six weeks from now would otherwise ask "why is it like
that" and the answer is not in a commit message.

---

## 2026-08-28 — the completion menu offers the session's own commands and skills first, Wake's verbs after

Reported by the owner: typing `/` in a conversation, or `@thea /` in the room, did not surface the
skills configured in Claude Code — "the /menu does not show the skills that I have configured in
claude code."

**The wire was never the problem, and that is the finding worth keeping.** An operator's skills ride
in **`init.slash_commands`** — verified against a live init frame (v2.1.251, captured with a bare
`/model` so `num_turns` was `0` and it cost nothing;
`docs/superpowers/notes/2026-08-28-skills-in-slash-commands-findings.md`): a skill's name is in
`slash_commands` beside the `.claude/commands` files, and it is *also* in a separate `skills` array —
which in the probe was a **subset** of `slash_commands`. Wake already decodes `slash_commands` end to
end (airlock → `SessionFacts.SlashCommands` → daemon report → `Agent.advertised`). So nothing had to
be plumbed; **`skills` must *not* be decoded** — not merely because it is redundant, but because
`slash_commands` is the list of what `/` actually invokes, and the `skills` array can hold names that
are model-invoked only. Offering those from a `/` menu would be a lying completion. Trust the
`/`-invocable list; the names are already in it.

**What was actually wrong was the ordering, and the bound made it total.** `commandMenu` added
Wake's twelve verbs *first*, then the session's advertised set, and `completionRows` is
`max(8, len(wakeVerbs())) == 12`. So at a bare `/` the twelve Wake verbs filled the whole menu and
**every** advertised command and skill was pushed into the "N more — keep typing" overflow, reachable
only by typing past Wake's set. The reported symptom exactly: a menu of Wake's own verbs and no
skills.

**The ruling (owner's override of the old "Wake first" decision):** the session's own commands and
skills come first, Wake's verbs after. The old order was defended as "a Wake command sorted among
claude's is one nobody scrolls to" — but the menu does not scroll, it narrows by typing, and the cost
of that defence was that the operator's own skills were the ones nobody ever saw at a bare slash.
Wake's verbs follow, so a bare `/` shows them only while the session advertises fewer than the bound;
past that they are an overflow away, and narrowing the draft brings a verb back once fewer than a
menu's worth of commands still match what has been typed (`/re` drops the skills that do not start
with those letters). This is what the lone-`@name` branch already did for the agent it addresses
(`@iris /` shows only iris's own); it is now the rule on every path.

**The accepted cost, stated plainly:** a session that advertises a full menu's worth (12+) now buries
*all* of Wake's verbs in the "N more" overflow at a bare `/` — including `/new` and `/resume`, which
claude's own CLI can never advertise and so have no discovery path *but* this menu. This is the mirror
of the bug being fixed (the operator's skills were the invisible ones before; Wake's verbs can be now),
and it is the deliberate call: the menu narrows by typing rather than scrolling, so `/n` and `/re`
still reach them, and the operator who owns the fleet knows its own verbs where a new user would not
know their skills were hidden. If it bites, the fix is a reserved floor for Wake's verbs or a taller
bound — both considered and declined here in favour of the surgical reorder the owner asked for.
The lone-`@name` branch is unchanged — it still shows only the addressed agent's own and no Wake
verbs, because `@thea /command` is a command *for thea* and Wake's fleet verbs are not thea's.

**The `@thea /` "doesn't pop up" half is the same cause seen from the other side, and it is
expected.** That branch shows only thea's advertised set, which is `nil` until thea's first `init`
(the list rides every turn's init, so a just-spawned agent that has taken no turn advertises nothing).
Owner confirmed it is empty only when just-spawned; once thea takes a turn its skills appear. Left as
is — a fallback to Wake's verbs there would re-introduce exactly the fleet verbs the branch exists to
keep out of a command addressed to one agent.

`completionRows` stays `max(8, len(wakeVerbs()))`: with Wake's verbs last the bound no longer
guarantees they are all seen when a session advertises a menu's worth, but it still guarantees they
fit when a session advertises none — the empty-menu case where the menu is only Wake's. The keys are
untouched: `⇥` completes, `⌃N`/`⌃P` walk, `↑↓` stay the roster's. (The owner is separately remapping
the arrows to walk the menu; nothing here touches that.) `internal/ui/completion.go`,
`completion_test.go`'s `TestTheSessionsCommandsComeBeforeWakes`.

---

## 2026-08-28 — an edit shows its diff by default, and its "updated" confirmation is dropped

Owner, comparing the DM pane to Claude Code: when an agent updates a file, the diff should be
visible without pressing ⌃E or clicking, the way Claude Code shows it.

**The diff was already correct; it was hidden.** Wake renders an edit's diff from the tool's own
input (`old_string`/`new_string` → `core.ToolDiff` → `render.Diff`), with no file read — the diff is
free, off the wire. But a lone tool call folds into a `1 tool use · 1 edit` rollup by default, so the
diff sat behind an expand. The fix is one predicate: `foldExempt` (rollup.go) now returns true for a
call carrying a `Diff`, so an edit breaks the run and draws whole — the exact mechanism a
`TaskCreate`/`TaskUpdate` checklist already used, for the same reason (the block is the point, not the
count). A run's other tools (reads, bashes) still fold around it.

**And a successful edit's result is dropped.** The exempt edit surfaced its result body — `The file X
has been updated successfully. (file state is current in your context — no need to Read it back)` —
which is pure confirmation the diff and the now-green ⏺ already carry, and which Claude Code omits. So
`toolResultBlock` returns "" for a result whose call carries a `Diff` **and did not fail**; `Append`
settles the bullet before an empty body returns (dm.go), so nothing is lost. A **failed** edit still
shows its result, because that is the error the operator has to read. The suppression is not stored —
the empty block is dropped from `d.events` — so it is **irreversible**: even ⌃E does not bring the
confirmation back. That is a small tension with ⌃E's "reveal what was folded" contract and the right
call anyway, because the line it hides is boilerplate nobody expands to read.

**Two things were deliberately left out.** *Line numbers* — the 382/383 gutter in Claude Code's own
diff — are **not on the wire**: the edit input carries only the before/after text (no file position),
and the result is the bare "updated" line above (no `cat -n` snippet; Claude Code's UI numbers from
file state it keeps, which Wake by design does not). Showing them would mean reading the edited file
from disk at draw time, which the "cheap to leave open / owns almost no state" non-negotiables push
against and which is fragile (later edits shift the lines). Owner chose to leave them out.
*Renaming `Edit` → `Update`* (Claude Code's display label) was also left out: it is core's tool
vocabulary, "Edit" is not wrong, and doing it right means verifying Claude Code's whole display
mapping (Edit/Write/MultiEdit) against a recording rather than guessing.

**`MultiEdit` gets none of this today, and that is a recorded gap rather than a decision.**
`core.toolDiff` reads a *top-level* `old_string`/`new_string`, which `Edit` and `Update` carry;
`MultiEdit` nests its hunks in an `edits` array and carries neither at the top level, so its `Diff`
is nil, `foldExempt` is false, and a `MultiEdit` still folds into `1 tool use · 1 multiedit` — and
because `diffBlock(nil)` is "", it shows **no diff even when expanded**, which is the sharper half of
the gap. So after this change a single `Edit` shows a rich inline diff by default while `MultiEdit`,
the common multi-hunk tool, shows a folded count with nothing behind it — a visible asymmetry, stated
here so it reads as a known limit rather than an oversight. (`Write` carries no before/after either,
so it too draws no diff — correctly, a created file has no "before".) Pre-existing (this change did
not touch `toolDiff`); representing several hunks as a diff is the work, and `deferred.md` carries it.
`internal/ui/rollup.go`'s `foldExempt`, `internal/ui/toolblocks.go`'s `toolResultBlock` (which the
subagent-forwarded transcript shares, so a subagent's own successful edit is suppressed there too),
`dm_test.go`.

---

## 2026-08-16 — the emergency quit, and why no key could have been one

Reported by the owner: Wake froze inside **cmux**, the screen went completely static, and neither a
double `⌃C` nor `⌃Q` closed it. Two separate defects sit under that sentence, and only the second
one is fixable here.

**What was verified, by measurement rather than by reading.** With bubbletea v1.3.10 in an alt
screen with mouse tracking, a `Update` that never returns:

| Signal | What happens to a wedged program |
|---|---|
| SIGINT | **nothing** — `handleSignals` parks on `p.msgs <- InterruptMsg{}`, an unbuffered channel only the wedged loop reads |
| SIGTERM | **nothing** — same send, same channel |
| SIGHUP | dies, by default disposition — **no terminal restore**: no `\x1b[?1049l`, no `\x1b[?1002l` |
| SIGKILL | dies, same |

So before this change the *only* ways out of a wedged Wake all left the operator inside an alt
screen, with mouse reporting on and the tty still in raw mode. That is the whole of *"I could not
close it out"*. `TestAWedgedProgramSurvivesTheSignalsBubbleTeaHandles` holds the first two rows; a
bubbletea that buffers that channel fails it, and the ruling is what gets corrected, not the test.

**Why no key binding could have fixed it.** A `tea.KeyMsg` is produced by the loop that is stuck.
Bubble Tea's input goroutine reads the tty and then *sends* on the same unbuffered `p.msgs`, so a
wedged loop stops the keyboard one message in. Any spelling of any chord in `internal/ui` is
unreachable by construction. The answer therefore had to be **below** Bubble Tea: `killswitch.go`
reads the tty itself, on a goroutine that does nothing else, and decides before the byte is handed
anywhere — which is `inbox.go`'s rule about the socket, one layer further out.

**Why the hand-off is bounded and drops.** The pump forwards through a 64-entry queue and a pipe,
and drops when the queue is full. That is the point rather than a concession: a full queue *means*
Bubble Tea has stopped reading, and a pump that blocked handing over the byte before deciding would
put the wedged consumer back in front of the key that escapes it. `TestTheKillSwitchFires-
WithNothingReadingWhatItForwards` fills the queue and the pipe behind it and then presses; a build
that hands over first fails it at its own deadline.

**Why two of the same key, against `detach.go`'s ruling.** That ruling — a same-key confirm fires on
the reflex the arm exists to catch — turns on the *first* press being invisible. ⌃C parks the focused
agent and says so; ⌃Q parks the fleet and says so. A second press is therefore never the reply to
silence; it is the reply to the first press not having worked, which is the state this exists for.
Anything between the two disarms, so parking two agents (which needs the roster cursor moved) is
still two parks.

**Why both ⌃C and ⌃Q.** The owner asked for ⌃Q — *"just ctrl q like we have, but that did not
work"* — and ⌃Q is XON. The paragraph below about `⌃Q` reaching Wake ends on the one third nothing
settles: the layer that is **not** the tty driver — tmux, screen, ssh, **cmux**. If that layer eats
⌃Q as flow control, ⌃Q⌃Q is eaten with it, and ⌃C is not flow control.

**Three residuals, written down rather than argued away.**

1. **The true end-to-end case is untested and cannot be tested here.** The suite proves the pump
   fires with nothing reading what it forwards, and that ⌃C⌃C closes a *healthy* window with exit
   130. Wedging a real Wake needs a host terminal that stops draining, which is what `go test` has
   no way to arrange — so it is in `live-testing.md` and nowhere else.
2. **A paste containing two consecutive `0x03` or `0x11` bytes fires it.** The pump reads bytes and
   does not track bracketed-paste mode. Tracking it is real complexity for a case where the cost is
   a window closing with the fleet untouched, so this is accepted rather than fixed. If it ever
   happens to anybody, the fix is to ignore the trigger between `\x1b[200~` and `\x1b[201~`.
3. **If the *host* is what is wedged, nothing in Wake helps.** cmux owns the pty; a cmux that has
   stopped relaying keystrokes never delivers the ⌃C either. This closes Wake's half.

**What is still not diagnosed: what wedged it.** The likeliest mechanism is that Wake's own draw
blocks on `os.Stdout` when the host stops draining it — `guardedOutput.Write` holds its mutex, the
renderer's flush holds `r.mtx`, and the event loop blocks in `renderer.write`. That fits "completely
static" and it fits cmux owning the pty. It is **not proven**: the fleet's `daemon.log` was empty and
the client was gone by the time anyone looked. The kill switch is the right fix either way, because
it is the fix for *every* wedge rather than for one of them — but a repeat should be captured with
`--debug-file` and a `SIGQUIT` stack dump before this is called closed.

---

## Settled since — the interrupt keymap

**Shipped as `⎋` interrupt, `⌃C` detach.** The proposal below was `⌃C` to interrupt and `⌃Q` to
leave, on the argument that `⌃C` already means "stop what you're doing". What settled it the other
way is that **Claude Code uses `⎋` for interrupt and Wake's promise is that the DM feels like
Claude Code** — so the muscle memory being matched is `⎋`, not the shell's. `⌃C` keeps the meaning
it has everywhere else in Wake: leave, and the fleet keeps working.

The original concern still holds and is worth keeping: a key that sometimes stops a turn and
sometimes closes the whole workspace costs someone an hour once and is never trusted again. `⌃C`
never stops a turn, and `⎋` never closes anything.

### Superseded 2026-08-11 — `⌃C` parks, `⌃O` detaches, `⌃Q` parks the fleet and exits

**Only the `⌃C` half is superseded.** `⎋` is still interrupt, for exactly the reason above, and the
sentence that decided it is what the new pair is built on rather than something the change had to
argue past: *"a key that sometimes stops a turn and sometimes closes the whole workspace costs
someone an hour once and is never trusted again."* That is why leaving and parking are **two keys**
rather than one that guesses which you meant. `⌃C` parks and never leaves; `⌃O` leaves and never
parks; `⎋` still stops a turn and closes nothing.

What changed underneath it is that `⌃C` detached **because stopping was irreversible** — a stray
keystroke that ended a session destroyed an hour of context with no way back, so detach was the
safe thing to put under the key everyone reaches for. Park removed that reason: the process stops,
the transcript stays on disk, and `/resume` brings it back. The spec's own ordering is *"build the
machinery first; rebind after it works"*, and that is what happened — park, wake and the park book
shipped over Tasks 2–6, and the keys were bound in Task 7.

`⌃O` was argued from the three unshadowed keys (`⌃O ⌃X ⌃Y`): it is the only one `keyprobe_test.go`
already covered, and spending it leaves **two** for the key deferred I7 owes next-blocked when it
reclaims `⇧⇥`, where one would do. `⌃Q` is not from that set at all — nothing in this build or in
`bubbles` binds it.

**The two terminal-level worries about this pair are settled by reading the dependency, not by
"should".** `⌃Q` is XON and `⌃C` is INTR, and both are the tty driver's to eat before an
application sees them — which would make `⌃Q` never arrive and `⌃C` kill Wake instead of parking.
bubbletea's `initInput` calls `MakeRaw` from **`github.com/charmbracelet/x/term@v0.2.2`** —
spelled in full because the build graph also holds `golang.org/x/term`, and only the first is on
this path — whose `term_unix.go` replicates `cfmakeraw`: it clears **`IXON`** from `Iflag` and
**`ISIG`** from `Lflag`. So on a tty bubbletea
put in raw mode, `\x11` and `\x03` both reach the decoder, and `keyprobe_test.go` is what says the
decoder names them. bubbletea's own comment agrees from the other side: *"in most cases ^C will not
send an interrupt because the terminal will be in raw mode."*

What is genuinely left is the part no source settles: something between the keystroke and Wake that
is **not** the driver — tmux, screen, an ssh client, a terminal that does not honour raw mode. That
is in `docs/live-testing.md`, with the tell for each. Note the failure shapes differ: an eaten `⌃Q`
does nothing at all, while a `⌃C` that arrives as a signal closes Wake through bubbletea's handler
(`InterruptMsg` → `ErrInterrupted`), which exits **without parking and without printing a line**.
If a terminal does eat `⌃Q`, the fleet-park key moves and `⌃X`/`⌃Y` are what is left.

Confirmed safe by recording before it shipped: a session **survives** an interrupt and its
`session_id` never changes. Six fixtures show an aborted turn followed by a normal successful turn
in the same process; an idle interrupt is a no-op; an interrupted session `--resume`s losslessly.
Since confirmed further: interrupting a session with a permission ask outstanding **withdraws the
ask** on a `control_cancel_request`, and the process stays usable.

Still open: `x` on a roster row to stop one agent, which needs a roster UI to put it on.


---

## Settled

**`auto` stays the default permission mode.** A review argued it leaves the whole permission
path — and Phase 2's headline attention trigger — rarely exercised. True, but it is the stated
product intent from the original brief. What changed instead: every spawn site must set the mode
*explicitly* rather than inherit a constructor default, so the choice is visible where it is made.

**Stop closes stdin; it does not signal.** An agent killed mid-`Edit` leaves a half-written file,
and Wake is not entitled to do that to someone's repo. A hard kill exists for a wedged agent,
behind a confirmation, and is never what `stop` does. Group-kill belongs on the cancel path only.

**`Raw` does not cross the wire** (`json:"-"` on `core.Event`). Measured at **6.83×** — 85.4% of
the event stream — for a field the receiving side is forbidden to read, since parsing it outside
`internal/core` would violate the airlock. Tagged on the core type rather than stripped in the
transport, because a `Frame` constructor is unenforceable: the daemon's own broadcast builds
frames directly. Cost accepted: no attach-and-dump-the-raw-line from the client. Better mitigation
if ever needed is teeing `claude`'s stdout to a file — the *original* bytes, unmangled by two JSON
round-trips.

**The DM does not de-duplicate on `Echoed`.** It picks a label, never visibility. Suppression
keyed on that flag is right in exactly one of the two possible behaviors of
`--replay-user-messages` and badly wrong in the other, and which one is real has never been
recorded. If the assumption is wrong, a message renders under the muted label — nothing hides,
nothing doubles. The single-source rule belongs to the App, which owns both halves.

**`SetEscapeHTML(false)` on the wire.** Go escapes `<`/`>`/`&` into six bytes to protect JSON
embedded in a `<script>` tag. Wake writes to a unix socket read by a Go decoder; that threat model
does not exist here. Measured cost of leaving it on: 0.19% on the corpus but **1.87×** at ~19%
bracket density — HTML, XML, JSX, SVG — which re-opens a connection-fatal size ceiling.

**~~The airlock is a file, not a package — for now.~~ It is now four files**, and this entry
predicted the trigger correctly: it said an `initialize` handshake *or `control_cancel_request`*
would push `protocol.go` past the 800 hard max. Subagent attribution got there first, taking it to
1031 — and the restatement arrived **during** the change rather than before it, which is exactly
what this entry said not to do. The split is by direction and job: `protocol.go` decodes,
`wire.go` holds shapes, `vocabulary.go` maps Claude's words to Wake's, `encode.go` writes. All in
the 200–400 band, enforced by `airlock_test.go`, which fails on a fifth member or one outside
`internal/core`. `control_cancel_request` is now recorded **and decoded** — `core.KindRequestWithdrawn`, merged 2026-08-09. So the entry's prediction was right about the trigger and wrong about which change would reach it first.

**`make test` runs with and without `-race`.** Twice the no-race run caught what the detector hid:
`Getpgid` failing on a zombie, and a timing-dependent exit-status assertion. Both passed 5/5 under
`-race` and failed 5/5 without. A green race run is not evidence on its own.

**Four lifecycle verbs, not two.** park (`⌃C`, and eventually automatic under a live cap) · detach
(`⌃O`) · stop (one agent) · quit (everything; `⌃Q` parks on the way out, `wake stop` does not).
Conflating any pair is how a fleet becomes unstoppable or a machine fills with orphans. The spec had
only detach until someone asked how you close an agent at all. The keys in this line were `⌃C` for
detach until 2026-08-11; see the supersession above.

---

## Ruling: the last-read rule stays where it was drawn, against the messaging-app convention

Every chat client shows one "unread" line and moves it. Task 11 built that first and replaced it,
and the reason is specific to this workflow rather than a general preference.

**Moving it erases the landmark at the moment it is in use.** The case the marker exists for is
somebody half way through a 400-line answer who glances at another agent and comes back. A single
line that relocates on every departure has jumped to the bottom by then; worse, the version that
clears itself when nothing arrived during the glance makes it *vanish*. Both destroy the thing the
reader was using to find their place, and departures are the whole subject.

That reason stands alone, and it is the only one. **A second argument was written and has been
deleted: "relocating a rendered rule costs the 248ms re-wrap".** It is true of `chunked` +
`transcript` as they are built and not of the problem — a rule drawn as a view-time overlay in
`transcript.view()` moves for free, at O(height) per frame. A load-bearing comment that argues from
a constraint we imposed on ourselves is how the next person gets talked out of the right fix, so
the cost is a fact about the current implementation and never a reason for the design.

So a rule is drawn once, where it was earned, and the label is past tense: **"you left off here"**
does not stop being true, where "new since you left" would quietly become false the next time
somebody left.

**A conversation keeps the newest three, and that is a decision rather than a threshold.** Three is
the count the workflow names — three concurrent research sessions — so "here, and before that here"
stays legible and no further. Each rule is two lines in `Accent`, the colour reserved for what the
reader is hunting for, and they do not spread evenly: every one sits in the band a returning reader
scrolls back through, so past about ten the accent stops answering "where did I stop".

**The anchor is an event index, never a line offset, and it is plural.** `transcript.scroll` indexes
lines that a width change re-derives from scratch, so an offset that was right before a re-wrap
points somewhere else afterwards while still looking valid — nothing fails, the boundary quietly
moves. The marker is a *block* instead, emitted by the one rule both the incremental append and the
re-wrap ask.

**It shipped singular, and that made this paragraph false in exactly the case the feature exists
for.** One `mark int` + `marked bool` behind a transcript holding one rule per absence: a re-wrap
regenerated the newest rule and dropped every earlier one, and an ordinary glance-and-return cleared
the flag so a later re-wrap emitted none at all. The surviving rule looked right. **A state that
regenerates N artefacts has to hold N anchors** — that is the generalisable half, and it is the same
failure this very entry names two paragraphs up, arriving through the fix for it.

*How it stayed green:* two tests, each individually falsifiable, whose **union nobody wrote** — one
asserted that two absences leave two rules and never re-wrapped; the other re-wrapped and only ever
had one absence. Read together in a report they sound like complete coverage. **When two tests are
each about one half of a property, write the case where both halves are true at once**; that
intersection is where the defect lived, and it is a new entry in this project's catalogue of ways a
suite stays green over a broken invariant.

## Ruling: the parent→fork edge is Wake's memory, not Wake's state

Nothing on Claude's wire says a session was forked or from what — a fork's `init` carries 23
top-level keys and not one names an ancestor, the parent emits nothing at all while a fork is taken
from it, and this is true for a live parent exactly as for a dead one. So Wake has to record it, and
the only question is where.

**It lives on `daemon.agent` and on `rpc.SessionStatus`, and not in the on-disk roster.** Three
arguments, and the second is the one that decides it:

- `roster.go` is "the minimum needed to find a process again". Ancestry helps nobody find or verify
  a process group.
- **Nothing turns a roster record back into an agent.** `loadRoster`'s only consumers are
  `reapOrphans` and `daemon.FleetOnDisk`, so a persisted parent id would outlive the parent's *name* — and
  a later daemon would report an edge to a session it holds nothing about. That is "a name is never
  an address" failing one level up.
- The durable copy already exists and is not Wake's. A fork's transcript preserves the parent's
  per-message `uuid`s across both recorded generations, which is a forensic route to lineage after
  the fact. The recording note is explicit that this is not a substitute for recording the edge —
  and recording it, for the fleet that is alive, is exactly what the in-memory edge does.

The corollary is the naming rule, untouched: an **id** crosses the socket and whoever draws it
resolves the name from the report it is already holding. In the DM header a parent that cannot be
named is not drawn at all, because eight hex characters there are the thing names exist to replace;
in `wake status` the short id is the right fallback, because that is the column that prints ids and
`wake attach` resolves them.

**And a reused name is not ancestry, on both surfaces.** A name goes back to the pool when its
session ends while the ending stays in the report, so a report can hold an ended `alex` that is the
parent and a live `alex` that is not. `cmd/wake`'s `sessionNames` drops a name held by more than one
row and falls back to the short id; `ui.parentName` drops it and falls back to nothing, because the
DM header is the surface `@name` routes on and `Fleet.ByName` resolves the live one. Same ruling,
two fallbacks, and the difference between them is which column the reader is allowed to copy.

## Ruling: the park book is a second file, not a wider roster

Named 2026-08-10 by Phase 3 Task 4, which is the task that makes `roster.go`'s own header false if
it is read one word wider than it is written. **This is the first time Wake persists something it
reads back into live state**, so the file it goes in is a decision rather than a location.

`roster.go` says *"nothing turns a roster record back into an agent"*, and that is still true and
still the reason the roster starts empty: `loadRoster`'s two consumers are `reapOrphans`, which
signals what it can verify and clears the file, and `daemon.FleetOnDisk`, which assembles a report. The
argument is about **live processes**. A parked session is the opposite shape, and the three
consequences are what make it a second file rather than a wider first one:

- **The reaper must never see it.** A record in `sessions.json` names a pgid, and a parked
  session's pgid is gone and may since have been recycled — the one thing `reapOrphans` is built
  never to signal on a guess.
- **It carries no PID at all**, and that absence is the type-level statement of what a parked
  session is. `record` has one; `parkedRecord` does not, and the on-disk key set is asserted in
  both directions so a `pid` cannot appear there quietly.
- **It is read back into `s.agents`**, which is precisely what the paragraph above says nothing
  does. That is the contradiction, and it is resolved by two files with two contracts rather than
  by widening one. `CLAUDE.md`'s registry paragraph was rewritten in the same change: half of the
  old argument survives untouched and half of it is now about the other file.

**And a restored row is a claim, not a proof.** `completePark` writes the entry in `retire`, after
core's `Wait` has returned, so the daemon that wrote it had watched *its own* process go — and that
is all that travels. Anything can start on the id between two daemons, so a restored row asserts
identity and location and nothing about liveness, and `unpark` re-proves the rest through
`resumeSafe`. The corollary is that **`restoreParked` starts nothing**: a restore that resumed N
sessions would resume N ids it had never checked.

**A file format is where rung 6 bites.** A test that writes with the writer and reads with the
reader proves round-tripping and nothing about the bytes: a reader and a writer that agree on the
wrong key are perfectly consistent and unreadable by every other build, including the next version
of the same file. So the format is asserted twice around one side — the key set is read out of raw
JSON, and a record is decoded from a literal somebody typed — and both directions are required, so
that a missing key is a field that does not survive a restart and an extra one is state Wake has
quietly started owning.

**The clear belongs to the quit verb and not to shutdown**, and the asymmetry with the roster is
the ruling. The roster is debris however a daemon died, so it goes unconditionally. The book's
entries name transcripts that are still on disk and processes that are already gone, so they cost
nothing to keep — the question is not *"did this daemon exit"* but *"did somebody end this
fleet"*. `wake stop` is the only thing in the product that writes `FrameQuit`; a SIGTERM or a
laptop lid reaches `Serve` as a cancelled context. A book cleared on every exit would put the whole
feature one signal away from being lost, and `⌃Q` must therefore arrive as its own kind rather than
as a quit.

*Built 2026-08-11 by Task 5, and the shape the sentence above asked for.* `rpc.FrameParkAll` is that
kind, and `daemon.quitVerb` is the answer to "why is this daemon ending" — **three** values, because
there are three ways out and only two of them are verbs. `quitNone` is the zero value and it is the
signal: it decides nothing, so the book is left exactly as it is. The first verb wins, because two
clients ending the daemon at once cannot be resolved into a third meaning and the two wrong answers
are not equally wrong — a park that became a stop clears a book somebody meant to keep and nothing
afterwards reports it.

**And a kill withdraws a park only from a session that has not already ended.** `agent.kill` cleared
the park label unconditionally, which was right for its one caller and wrong the moment `⌃Q` gave it
a second. `ended` is set in `retire`, after core's `Wait` returned, so it is Wake's own proof that
the process is gone: a signal sent after it cut nothing and the transcript is the one the agent
finished writing. The reachable failure is `shutdown`'s own grace, which samples `finished()` on a
20 ms tick and kills whatever had not ended by the last look — so a fleet parked by `⌃Q` lost a
session per agent that ended inside that gap, silently, and more of them the busier the machine.
Both fields are under the agent's lock, so the test is atomic with the clear rather than a
check-then-act.

**What the timestamp is for, and why it is not a `parkedAt` on the agent.** `agent.parked`'s doc
comment refuses one on the grounds that *"a row's QuietMS already says how long it has been since
the session spoke"*. True of an agent this daemon watched go quiet, false of one it read off a
file — `newAgent` starts the clock at now, so a session parked yesterday came back reported
`quiet 0.0s`, which is a confident wrong answer on the surface an operator scans thirty rows of.
`noteQuietSince` is the field's only reader, and having one is what makes a narrowing keyed on it
visible at all. A zero time is refused rather than used: it means a record that does not say when
it parked, and 1970 subtracted from now is two thousand years of quiet.

---

## Ruling: a promise made in prose is not a feature, and this one was made twice

"On every confirmed fork, Wake says that a fork is a snapshot" was written **three times before
anything said it**: in `daemon.forkRefusal`'s comment (Task 2, present tense), in `v1_goals.md`
(Task 3's brief, present tense), and — after Task 3 corrected both to the honest tense — in the
deferred entry that Task 3 wrote to hold it. Task 4 built it: `ui.forkOpened`, on the notice row of
the fork's own conversation, once per **confirmed** fork.

The generalisable half is not "write it down"; it was written down, four times. It is that **a
mitigation whose only surface belongs to a later task is invisible to every guard the earlier tasks
can write**, so the earlier tasks keep restating it in the only place they have, which is prose.
The cheap defence is the one this project already uses on the legend: make the claim *checkable
from the side that would notice it missing*. Here that would have been a test in the task that
first made the claim, asserting that some string in `internal/ui` says the parent's later turns do
not reach the fork — red until the sentence exists, and impossible to restate instead of build.

---

## Ruling: the manager's configuration is derived from its name, and never carried on the wire

Named 2026-08-11 by Phase 2 Task 15 (the manager), **against that task's own brief**, which
prescribed `rpc.Frame.MCPConfig` — a path to the manager's MCP config, written by the client and
read by the daemon. Two things kill it and only the first is a security argument.

**An MCP config names a command to execute.** The file is
`{"mcpServers":{"wake":{"command":…,"args":…,"env":…}}}`, and claude runs what it says at startup.
A path on the wire therefore lets anything that can dial the socket choose the command line of the
one session that holds tools acting on the whole fleet. The socket is the user's own, so this is not
a privilege boundary — and that is exactly why it is worth stating as a *design* rule rather than a
threat: the whole subject of Task 15 is bounding what a manager can reach, and a field that re-opens
it from outside is not a field to add. `--strict-mcp-config` and the two-verb list would both be
arguing about a command line somebody else chose.

**And a wire field cannot survive a park**, which is the one that would have shipped as a bug.
`⌃Q` parks **every** session, including the manager. A wake is served from the daemon's own row and
the park book; the client that spawned the manager is long gone. So a config that arrived on the
spawn frame is in neither, and `/resume manager` produces a claude process called `manager` with no
tools and no scope — answering `@manager` confidently about a fleet it cannot see, with nothing on
any surface saying what is missing. The brief's stated justification for the wire field is also
false against this code: *"the daemon was forked and its argv[0] is not necessarily something
anything can execute"* — `EnsureRunning` forks `exec.Command(os.Executable(), "daemon")`, and
`os.Executable` reads `/proc/self/exe` or `_NSGetExecutablePath` rather than argv[0].

So: `Frame.Role` carries a **role and nothing else**, `daemon.managerConfig` derives the rest from
`cfg.Name`, and it is applied in `launch` — the one place spawn, fork and wake all go through, which
is what makes the wake correct for free rather than by a second call site somebody has to remember.

**Keying on a name is a departure worth naming, and it is the second instance rather than the
first.** `internal/mcp`'s `liveSessions` already keys on `core.ManagerName`, with the argument
written down: `daemon/names.go` refuses the reserved names to every ordinary spawn and
`names_test.go` requires the daemon's reserved set to equal the router's own constants, so a session
called `manager` is one the daemon deliberately named. It is a name doing *identity* work, which
this project otherwise forbids — and what makes it defensible is that it is not an **address**:
nothing is routed by it, the id still crosses the socket, and the name is minted by the one process
that can see the whole fleet.

**The alternative was `rpc.SessionStatus.Role`**, which `deferred.md` had addressed to this task as
"a better discriminator". It was considered and not taken, and the reason is the park book: a
restored row carries a name and no role, so a Role-keyed exclusion would go on working for a live
manager and **silently stop working across a daemon restart** — `mcp.liveSessions` would start
offering the manager to itself, which is the unbounded loop that exclusion exists to prevent. Adding
Role to `parkedRecord` closes that and costs a schema change to the one file Wake reads back into
live state, whose format is asserted in both directions. Whoever wants the field owns the record.

## Ruling: the room's import verb is `/adopt`, and the corpus picked the word

Named 2026-08-12 by the branch that gave session importing a TUI surface.

The obvious spelling is `/import`, matching the shell verb it surfaces, and `deferred.md` had
proposed exactly that (*"a `/import` entry in `ui.commands`"*). **It is refused, and the evidence is
a recording rather than an argument**: `slash_commands` on claude's `init` frame advertises
`import`, in **45 of the 45** files under `testdata/stream` that carry the key. Taking it would have
replaced a working claude command with an "unknown command" refusal, and the operator's only symptom
would have been that a command stopped doing what it used to.

This is the **second** time that corpus has chosen this build's vocabulary — `/name` and `/task` are
the first, because `rename` is in it too — and the pattern is now worth stating as a rule rather
than as two instances: **grep the corpus before choosing a word, not after.** The check that catches
it (`TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising`) iterates `ui.commands`, so it
fires automatically on any word added to the map; what it cannot do is suggest a replacement, and by
the time it fires the word is already in prose, in constants and in a header.

`adopt` is in none of the 133. It is also the word `internal/daemon/import.go`'s own header already
used (*"Adopting a session Wake never started"*), so the shell verb and the room's verb differ in
spelling and agree in meaning — which is a cost, not a free lunch: an operator who knows one has to
learn the other. It is the smaller cost. The alternative was renaming the shell verb to match, which
spends a verb everybody's muscle memory already has to protect a word claude might stop advertising
tomorrow.

**The finding is pinned in both directions.** `TestImportIsNotWakesWordBecauseTheCorpusShowsClaudeAdvertisingIt`
fails if `import` leaves the corpus *and* if Wake takes it, because the half that only checks Wake
does not own the word passes over nothing the day the recordings are replaced, and the next reader
takes the word back with a green suite.

## Recurring failure modes worth naming

Patterns that have each cost more than one round.

**A mutex held across a blocking call.** Three instances: the render lock across a 5-second
terminal probe; the transport's `writeMu` across a socket write; the session's state lock across
a blocking pipe write. In every case the operation that most needs the lock — kill this thing —
is the one that cannot get it.

**A fix that relocates a silent failure instead of closing it.** Stderr capture made a failed
spawn visible and simultaneously made `cmd.Wait()` unbounded. Raising the diff cap fixed
truncation and quintupled a one-sided cut nobody had noticed.

**Widening a fix past what was asked.** The findings note took four rounds because each round
fixed its finding and planted a new contradiction in whatever the fix was widened to cover. The
implementer diagnosed the mechanism itself after round 2 and reproduced it in round 3 anyway;
fresh eyes broke the cycle.

**A test that cannot fail.** Fourteen instances, almost all caught by an agent checking its own
work — a socket test that passed with the mutex deleted, an aliasing test whose frames were too
small to force a buffer slide, a width test asserting the presence of the bug it was named to
prevent, a golden assertion whose *failure message taught a falsehood while the assertion
passed*, and a shutdown guard whose barrier was satisfied at spawn time. The habit that catches
these is deliberately reintroducing the bug and watching the test fail.

Four refinements the later rounds added, all earned the hard way:

- **"The mutation didn't pass" is not enough — say which assertion fired.** One guard's mutation
  produced a 138-second package timeout instead of a failure, and another passed *with the harm
  printed above the PASS*. A mutation that hangs and a mutation that fails look identical in a
  summary line.
- **A helper that can only be asked once turns any failure that asks it into a hang.** A
  one-shot `died` channel, a non-blocking receive that a second consumer finds empty. Close the
  channel instead of sending on it, so every consumer including a cleanup gets an answer.
- **Adding a guard is not the end of the work; reaching its failure mode is.** Two instances in
  one round of Task 10, both found by the implementer mutating its *own fixes*: restoring a
  `case cmdStop:` was trivially correct, and mutating it away left every test green — because
  nothing ran `run()` end to end, so **no test asserted that any verb reached its own command**,
  a far bigger hole than the nit that exposed it. And an `st.Running` check added inside a wait
  loop was right, and mutating it away also left the test green, because the daemon is alive
  throughout that test so the guard never fires either way. A guard whose failure mode no test
  reaches is not verified — it is a comment with syntax.
- **A test can be poisoned by a previous run of the mutation battery.** One asserted that a fixed
  `os.TempDir()` path was *absent*; the single mutation run that created it left the test failing
  on every subsequent clean run. Hermetic means per-call `MkdirTemp` with cleanup, not a constant
  path that happens to be unused today.

## A fake agent's own turns are part of the state a test asserts about

Named 2026-08-10 by Phase 3 Task 2 (the fork verb), and it is `internal/daemon`-specific in the
way that makes it easy to walk into twice.

The task's brief prescribed a mid-turn test as `fakeClaudeOnPath(t, "slow")`, a `FrameSend`, and
`c.pollState(idAlpha, rpc.StateWorking)`. It **fails 20 times out of 20** — not as a flake, and not
because the delay is short. `fakeSlowTurns` opens with `emitText("ready")` + `emitResult(…)`, and
Wake's send reaches the child's stdin *before* that opening `result` comes back up the pipe. The
result decodes to `core.KindTurnEnd`, which sets `a.owed = false` — so the daemon reports the
parent **idle for the whole of the turn it is running**, and `pollState` spends its fifteen
seconds waiting for a `working` it will never see. The transcript is unambiguous once read: `idle
quiet=20ms … 41ms … 63ms … 85ms`, then the echo lands.

Every other `slow` test in the package already has the barrier — `c.awaitEvent(idAlpha, "ready")`
before the send — and with it plus `t.Setenv(fakeDelayEnv, "10s")` the test passes. **The house
pattern was load-bearing and nobody had written down why.**

Two rules out of it:

- **A fake that emits an opening turn owes the test a barrier.** `ready` is not decoration; it is
  the edge after which `owed` means what the test thinks it means. A send written above it races
  the fake's own `result`.
- **Prefer a fake that cannot leave the state, over a fake that is slow to.** The fix that shipped
  is `mute`, which never ends a turn at all: `owed` goes true on the send and nothing clears it, so
  the state under assertion is one the parent is provably still in when the daemon reads it. 20/20
  in **1.4s**, against 20 runs of the `slow`+barrier version at ~2.1s each — most of which is the
  `quitGrace` kill of a fake still sleeping. A wall-clock window is a load-sensitive test in a
  package that has spent weeks on those; a fake that owes a turn end forever has no clock in it.

## A hand-written list standing in for something the code already declares

Named after the interrupt branch shipped **three** guards that could not fail, and all three
were the same shape: a literal in a test enumerating what the code itself already states.

- A legend guard iterating a hand-written five-entry map of glyph→key, while `CLAUDE.md`
  claimed it enforced a bijection. Adding a glyph with no binding passed; adding a binding with
  no glyph passed.
- A frame-kind distinctness map, which could not fail for a kind nobody added to it — on the one
  test whose job is to stop `stop` routing to `kill`.
- A vocabulary of policed words, where **26 of 81** were named nowhere the check could see, so
  deleting any of them failed nothing.

The tell is always the same: **the test restates a fact instead of deriving it.** The code
already declares the legend, the constants, the struct tags — so parse the declaration and
require the relationship, rather than copying it into a slice that drifts. Every one of these
was closed with `go/ast`, and each closure is about fifteen lines.

Two refinements worth keeping:

- **Derive the relationship, not just the membership.** A count is better than nothing and still
  passes when two entries change in compensating directions. A bijection between two derived sets
  cannot.
- **When a list genuinely must be hand-written — an allowlist, an excuse table — give it a
  count.** `allowlistPairCount` and `policedWordCount` exist so an exemption list cannot grow
  quietly, which is how the original 23 airlock leaks accumulated. And check the excuses
  **both ways**: an excuse whose subject no longer exists is dead text, and it is the thing that
  makes deletion a three-place edit rather than a one-line one.

---

## A mutation battery inherits the blind spots of the tests it runs

Named 2026-08-09, by the agent whose battery it was. Task 9 ran 21 mutations, killed 21, and
still shipped a guard that could not fail — because **every mutation it picked was one the
existing test *shape* could see.**

The settle that protects the 4.5× draw-path win had no guard at all. Removing the scheduling
entirely left the whole package green, and the benchmark went from 93 ms to 13.4 s per drag —
**144× slower, nothing red**, because `make test` does not run benchmarks and a benchmark reports
a number rather than failing.

The root cause was worse than a missing assertion: **every resize test delivered the settled
message by hand**, so the `tea.Cmd` the model returns was never executed by anything. The tests
drove the model and skipped the part Bubble Tea does, so no mutation to the *scheduling* could
ever be visible to them.

Two things follow:

- **Mutate the seam, not only the logic.** If a test hand-delivers what production schedules, the
  scheduling is untested and no amount of mutating the handler will say so. The fix was a test
  that runs every command `Update` hands back, the way the real loop does.
- **A benchmark is not a guard.** It measures under `-bench` and reports; it does not fail, and the
  default suite never runs it. A property worth 4.5× belongs in a test that goes red.

*And the deterministic form matters:* the count is taken behind a timer seam with a fake clock, so
it carries no wall time — `internal/daemon` spent weeks on load-sensitive tests and this package
was one sleep away from joining it. The single real-clock assertion is a **lower** bound, because
load can make a timer late and never early.

---

## A "no reachable X does Y" property has to assert per member, because no number of floors is enough

Named 2026-08-10 in the fork task, over two rounds, and it is the mutation-battery lesson above
arriving through a different door. `TestNoConfigProducesAnIllegalIdentityShape` walks a list of
`Config`s and asserts none of them builds one of the two argv shapes the CLI punishes. It has now
been insufficient twice, in the same direction, and **both times it looked thorough.**

**Round 1 — one floor.** The brief specified `built >= 4`, against the vacuous pass where every
`Config` returns an error and the loop body never runs. Necessary, not sufficient: **deleting the
fork case from `identityArgs` entirely left the test green.** Every `Config` then built a plain
`--session-id`, all six still counted toward `built`, and both assertions were satisfied by an
enumeration that contained no fork at all. A test named for the fork triple was asserting it over
a set with no fork in it.

**Round 2 — two floors, and still green.** A `forked` reach-floor was added for exactly that, and
review then *narrowed* the fork case instead of deleting it — a fourth arm, `ForkFrom != "" &&
Model != ""`, returning `--session-id` alone. **The whole tree stayed green.** `built` was 6.
`forked` was 1, satisfied by the one enumerated fork config that set no `Model`. Both negative
assertions were satisfied, because they test for flags being *wrongly present* and the downgraded
argv simply had none of them. Live consequence, had it shipped: every fork of a session naming a
model becomes an ordinary empty agent under the child's id — exit 0, empty stderr, nothing red,
and byte-for-byte the outcome `fork-session-no-resume.jsonl` recorded.

**The lesson, and it is not "add a third floor".** A floor is an aggregate over the set: it counts
*that* a class occurred. It cannot see the class occurring **for the wrong subset**, and "somebody
adds a case that captures part of the input space" is the realistic failure — the *next* task in
this plan literally adds an arm to that switch, because `identityArgs`' doc comment tells it to.

> **The invariant has to be per member.** For every `Config` that asks for a fork: it either errors
> or builds exactly the triple. That is one assertion inside the loop, it subsumes both floors, and
> it survives a fourth arm.

Same family as this project's *"an aggregate a re-wrap keeps correct while changing what it
names"* (the last-read anchor, which shipped as one slot behind N rules). An aggregate that stays
true while the thing underneath it changes identity is the shape to distrust.

Keep the floors as well — they are two lines and they still catch the empty enumeration, which the
per-member assertion cannot see by construction. Mutation-verified in both rounds: with the fork
case deleted, `forks == 0` is the only thing that fires; with it narrowed, the per-member
assertion is the only thing that fires.

### A deletion mutant asks whether the guard exists; a narrowing mutant asks whether it is total

Named 2026-08-10, and it is the single most portable thing the fork task produced. **"I added a
guard and mutated it" is weaker evidence than it feels**, because the mutation people reach for
first is a *deletion* — remove the case, delete the assertion, drop the flag — and a deletion is
the failure mode a guard is least likely to have.

The two are different questions:

| Mutation | What a kill proves | What it cannot see |
|---|---|---|
| **Deletion** — remove the behaviour outright | the guard *exists* and is wired to something | nothing about whether it covers the whole input |
| **Narrowing** — keep the behaviour, exclude a subset | the guard is **total** over its input | — |

Every aggregate assertion this project has been burned by died to the second kind and survived the
first. The fork property test killed a deleted fork case and then **survived a narrowed one**,
where the narrowing was a fourth switch arm keyed on an unrelated field. The last-read anchor is
the same shape one level up: it was not *missing*, it was one slot standing in for N, and it
regenerated the newest rule correctly while silently dropping the rest.

**And a narrowing is what the next change actually looks like.** Nobody deletes a working case;
they add an arm above it for a case they now care about, and that arm captures inputs the old one
used to serve. `identityArgs`' own doc comment instructs the next task to add exactly such an arm.
So the narrowing mutant is not the adversarial one — it is the *realistic* one, and the deletion
is the artificial one.

Practically, for any new guard:

1. Mutate by **narrowing** — add a condition that excludes some subset of the inputs the guard is
   supposed to cover — before mutating by deletion.
2. If the narrowed guard survives, the test is enumerating rather than quantifying. Fix it by
   asserting per member and by enumerating the input space rather than examples of it.
3. **Confirm the edit landed where you meant before believing either result.** A mutation applied
   to the wrong function reports SURVIVED, which reads as good news. This project has done that.

### A sampled dimension is an open dimension: enumerate where the code declares it, assert statically where it does not

Named 2026-08-10 by Phase 3 Task 2's review, and it is the **third rung of the ladder the entry
above starts**. Task 1 learned that a narrowing beats a deletion. Task 2 killed a narrowing keyed
on a field's *presence* — and then shipped a guard that a narrowing keyed on a field's **value**
walks straight past.

`forkRefusal(p rpc.SessionStatus) string` decides whether a parent may be forked. The guard over it
had two dimensions: every state, read out of `rpc`'s AST; and every *other* field populated, one
fixed non-zero value each. That killed `if p.Tool != "" { return "" }`. It did not kill either of
these, both constructed and run green against the whole package:

```go
case rpc.StateWorking:
    if p.QuietMS > 100_000 { return "" }                    // the table writes 90_000
    if p.Tool == "Read" || p.Tool == "Grep" { return "" }   // the table writes "Edit"
```

The first is not a strawman: `stateLocked` reports `working` until `silenceLimit`, so it opens a
**100 s–300 s band in which a mid-turn parent is silently forkable**. The second is a sentence
somebody actually writes.

**The ladder, and why the third rung is different in kind:**

| Rung | Mutant it kills | Method |
|---|---|---|
| Examples | the arm is deleted | a test per case |
| **Enumerate** the domain | the arm excludes a *member* | read the domain out of the code that declares it |
| **Assert statically** | the arm excludes a *value* | prove a property of the function, not of the sample |

The first two are the same move at different resolutions, and both work because the domain is
closed and *declared*: `rpc` spells out six `State…` constants, so a scan can hold the table to
them. **A field's value space is not declared and not closed**, so no finite sample closes it —
adding `QuietMS = 150_000` to the table answers those two mutants and not the class. Reaching for
more sample values is the tell that you are on the wrong rung.

The closing move is to deny the function the field. `TestForkRefusalReadsNothingButTheStateAndWhoTheSentenceIsAbout`
parses `forkRefusal`'s body and requires **every** mention of the parameter to be the `X` of a
selector naming `State`, `Name` or `ID`. Two details are load-bearing and each was chosen against a
specific evasion:

- **Every mention, not "no disallowed selector."** `helper(p)` carries no selector at all, so a
  selector-only check is defeated by moving the comparison one call down. Verified: a
  `quietEnoughToFork(p)` helper holding both predicates is caught.
- **A floor that the allowed fields are actually read.** A renamed function, a renamed parameter or
  a broken parse all yield zero matches — which reads as "reads nothing", the strongest possible
  pass for the weakest possible reason. This is `A hand-written list standing in for something the
  code already declares` arriving inside the fix for it.

**When this rung applies:** a pure function whose contract is *"the answer depends on X alone"*.
That contract is a statement about what the code may *read*, and reading is a static property. State
it statically and it stops being a sampling problem.

### Rung 4: the enumerated domain must be the *reachable* one

Named 2026-08-10 by the review of Phase 3 Task 3, and it is the rung above the one directly before
it — found by a guard built **correctly to rung 2** and still wrong.

Rung 2 says: enumerate a domain from the code that declares it, not from a hand-written list.
Task 3 did exactly that. `cmd/wake`'s fork guard scanned `internal/rpc`'s **constant block** with
`go/ast`, found six `State…` constants, and required a verdict for each. Every mutation it was
given died. It was still asserting over an input that cannot occur:

- `rpc`'s **type documentation, three lines below the constants it scanned**, states the narrower
  invariant: `StateOrphaned` *"appears only in the status of a machine whose daemon is gone"*, and
  `Status.Sessions` is *"every session the daemon holds, or — when Running is false — every one the
  last daemon recorded before it died, each StateOrphaned."*
- The producers agree, and they are the authority. `Running: true` is written in **one** non-test
  place, `daemon.(*server).fleet()`, whose rows come from `agent.snapshot()` → `stateLocked`, which
  returns only idle/working/blocked/silent/ended, plus `s.recent`, which holds retire-time snapshots
  of the same. `StateOrphaned` is written in **one** place, `daemon.FleetOnDisk`, which leaves
  `Running` false. `resolveSession` errors out on `!Running`.

So a whole cell of the table — and the production branch it covered, and the test that drove it —
described a state no caller could ever be handed. **A verdict over an impossible input reads as
coverage.**

**And then it got worse in a way worth recognising on sight.** The accompanying static guard (rung
3) had a vacuity floor requiring that `forkParent` actually reads the fields it is allowed to read.
The only read of `sess.ID` in that function was **inside the unreachable arm**. So deleting the dead
branch *failed the guard*:

> `forkParent never reads sess.ID … the scan is broken`

A removable dead branch had been converted into a pinned one. The guard stopped protecting the
behaviour and started protecting its own blind spot — and every rung below it was satisfied.

| Rung | Mutant it kills | Method |
|---|---|---|
| Examples | the arm is deleted | a test per case |
| **Enumerate** the domain | the arm excludes a *member* | read the domain out of the code that declares it |
| **Assert statically** | the arm excludes a *value* | prove a property of the function, not of the sample |
| **Derive from the producer** | the domain itself is *wider than reality* | enumerate what can arrive, not what the type permits |

**The rule: derive the domain from the producer, not from the type.** A type declares what a field
*may* contain; a producer decides what it *does* contain at a given call site. Where those differ,
an AST scan over the declarations is a superset, and the surplus is exactly where dead code hides
behind a guard that looks thorough.

The close, in this codebase's idiom, is a scan pointed at the producer — `agent.stateLocked`'s
`rpc.State…` selectors — cross-checked against the declared set **in both directions**:

- every state the producer can return needs a decision, so a seventh (park/wake's is next) is a
  build failure rather than a silent inherit;
- every declared state without a decision must carry, as data, **the producer that is the reason it
  cannot arrive** — so the day `stateLocked` starts returning it, the guard says so and demands the
  decision back rather than letting the deletion quietly become wrong.

That second half is what makes deleting the branch *safe* rather than merely correct today, and it
is the part a plain deletion would have lost. Mutation-verified four ways: a verdict added for the
unreachable state, a reachable state left undecided, the producer scan pointed at a renamed
function (the vacuity floor), and — the one that matters — `stateLocked` edited to return
`StateOrphaned`, which fires the excuse in both directions.

**Read this together with `A hand-written list standing in for something the code already
declares`.** That entry says stop copying a set into a test. This one says the set you derive still
has to be the right set: *the code that declares a domain and the code that produces it are two
different places, and only one of them is the truth about what arrives.*

### A guard is only as strong as the equality it uses, and equality is a layer decision

Also 2026-08-10, from the same review. `identityArgs` refused a fork onto the parent's own id with
`s.cfg.SessionID == s.cfg.ForkFrom` — a raw string compare. But the id space Wake actually accepts
is `uuid.Parse`'s, because that is what `daemon.mintedByWake` uses to decide an id came from Wake
at all, and `uuid.Parse` reads **six spellings as one UUID**: canonical, uppercase, braced,
`urn:uuid:`, and the 32-char undashed form in either case. Probed rather than assumed.

So a parent recorded canonically and a child handed in uppercase pass `mintedByWake`, pass the
guard, and build `--resume <x> --fork-session --session-id <X>` — the one shape the guard's own
comment says the identity model exists to keep impossible, and on macOS's case-insensitive default
filesystem both ids name the same `<uuid>.jsonl`.

**Two layers validating the same value under different notions of equality is a hole with no
symptom at either layer.** Each is locally correct. When a lower layer refuses a *relationship*
between two ids, it has to use the same equality as whoever decides those ids are ids —
`core.sameSession` now parses both and compares the parsed values, falling back to a string
compare for anything that is not a UUID. Note what it deliberately does **not** do: validity is
still the daemon's, because that is the layer that knows which ids this fleet issued. Identity and
validity are different questions and only the first one belongs this far down.

### Rung 4 read forwards: adding a member to a producer's range is the cheapest audit this project has

Named 2026-08-10 by park/wake Task 2, and it is the entry above lived rather than argued.
`agent.stateLocked` gained one arm — `rpc.StateParked` — and **five** guards in four packages went
red before any behaviour had been decided:

| Guard | What it demanded |
|---|---|
| `daemon.TestForkRefusalIsTotalOverEveryStateASessionCanBeIn` | *"rpc declares 7 session states and this test decides 6 of them"* |
| `cmd/wake.TestTheStatesAForkableParentCanBeInAreTheOnesARunningDaemonProduces` | a verdict, or the producer that is the reason it cannot arrive |
| `ui.TestTheStatesAForkCanArriveInAreTheOnesARunningDaemonProduces` | the same, for ⌃F |
| `ui.TestEveryStateTheDaemonCanReportHasARank` | a rank, derived from `rpc`'s own source |
| `ui.TestEveryStateTheDaemonCanReportHasAGlyphOfItsOwn` | a glyph, and a distinct one |

Two things are worth keeping from that.

**The guards found each other's blind spots.** Ranking parked required moving `unranked` from 6 to
7, which nobody thought about and `TestAStateThisBuildDoesNotKnowSortsLastAndNeverFirst` caught in
the same run: *"orphaned ranks 6 and an unknown state ranks 6"*. A guard over a constant nobody
edits is worth having precisely because the edit that breaks it is somewhere else.

**And the surfaces with no guard are where the work is.** Six more sites consumed the state with no
test demanding anything — `ui.live`, `mcp.liveSessions`, `cmd/wake`'s `liveSession`, `candidates`,
`runningSessions` and `runningCount` — and every one of them had a *plausible* default that was
wrong: `@all` addressing a process that is gone, a manager offered a row it cannot send to, `wake
attach` opening a composer that swallows keystrokes, an unambiguous name becoming ambiguous, a
"running now" listing offering something the next command refuses, and `wake stop` reporting a fleet
as up with nothing running in it. **The guarded sites cost minutes and the unguarded ones cost the
audit**, which is the argument for the guards stated as a ratio rather than as a principle.

### Rung 4, third shape: the reachable domain is a property of the *call site*, not of the package

Named 2026-08-11 by park/wake Task 8 (bare `wake`), and it is the entry above read the other way
round. `cmd/wake/forkguard_test.go` excuses `rpc.StateOrphaned` as unreachable, with the producer
that is the reason: `daemon.FleetOnDisk` is its only writer and is what `daemon.Status` returns
**when the dial fails**, so it arrives only on a report whose `Running` is false — which
`resolveSession` refuses before any state is looked at. Every word of that is true and it is **local
to callers that go through `resolveSession`**.

`hasFleet` does not. Bare `wake` is handed `daemon.Status`'s answer with no filter at all, precisely
because the case it exists for is a fleet parked by `⌃Q` — which has no daemon, so the report *is*
`daemon.FleetOnDisk`'s and `Running` *is* false. So the same package, the same type and the same producer
scan yield a **wider** domain one function over, and the excuse that was correct for `forkParent`
would have been a cell asserting a verdict about an input this function meets on its main path.

The move is to derive the domain from **every producer that can reach this call site**, which for
`hasFleet` is the union of `agent.stateLocked` and `daemon.FleetOnDisk` — one scan per answer
`daemon.Status` has. What makes this cheap is that the machinery already existed: `sessionStateConstants`
and `statesARunningDaemonReports` were reused verbatim and one twenty-line function was added beside
them.

**The generalisation worth carrying:** an "unreachable here" excuse is scoped to the *filter* that
makes it true, and the filter is usually somewhere else. When copying a totality guard to a second
call site, copy the domain derivation and not the exclusion list — the exclusion list is the part
that was about the other caller.

### A surviving mutant is a question, not a conclusion — and twice now the answer was a bug

Named 2026-08-11 by park/wake Task 8, **and it is the entry that task got wrong first.** Both halves
are kept, because the wrong half is the lesson.

`openRoom` asks `daemon.Status` **before** `connect`, and the stated reason is real: `connect` calls
`EnsureRunning`, which forks a daemon when nothing is listening, so asking afterwards asks a daemon
this command just created. The plan asked for the reversing mutation to be killed. It was
constructed, it compiled, and it survived the whole suite. The task reasoned out *why* — `restoreParked`
runs before the accept loop, so the daemon a dial forks reports exactly the parked rows that make
`hasFleet` true — recorded it as surviving in three artefacts rather than dropping it quietly, and
called the ordering *"a statement about the code rather than about an output"*.

**Every sentence of that was true of the configuration it had, and the configuration was the wrong
one.** The reasoning holds when the dial's outcome is "no daemon". The outcome that matters is the
other one: a daemon in **graceful shutdown** holds its listener for the whole quit grace, so
`daemon.Status` burns `statusTimeout` and returns an *error*, while `connect` — which has no
deadline, by design, because its two outcomes are events — waits the shutdown out and forks. The two
orders then observe completely different things, and the shipped one observed the wrong one: it read
the error as a zero report, called it "no fleet", and **spawned a fresh agent beside the fleet `⌃Q`
had just parked**. The mutant was the version that behaved *correctly* on the path that separates
them, which is exactly why it read as equivalent.

Three things follow.

**A surviving mutant is a question about the test suite's reach, not a fact about the code.** The
honest reading of "it survived" is *"no test constructs a configuration where the two differ"* —
which is a prompt to go and find one, not a licence to write down that none exists. This project has
now hit that twice: the `bookParked` entry two sections up, where two writers made the one under
test invisible, and this one, where the missing configuration was a *third* state of a dependency
nobody had enumerated.

**Enumerate the outcomes of the call the ordering is about.** `daemon.Status`'s own doc named three
answers — a daemon that answers, no daemon, a dead daemon's leftovers — and the fourth, *a daemon
that is there and will not speak*, existed only as an error return. Rung 4 says derive the domain
from the producer; the producer here is a function's **error path**, and an error return is a member
of its range like any other. Two other call sites in `cmd/wake` already branched on that error
(`detachLine`, `stoppingLine`); `openRoom` was the one that discarded it.

**Recording the survival is still right, and it is what made the fix cheap.** Because all three
artefacts named the mutation, the review could go straight at it rather than rediscovering that it
had ever been tried. *"We could not kill it"* and *"we did not try"* are indistinguishable in a green
suite six months later — but so are *"it is equivalent"* and *"we have not looked hard enough"*, and
only the first of those pairs was defended. **Write down the mutant, the reasoning, and the
configuration the reasoning assumes**; the third is what a reviewer can falsify.

The kill, once the configuration existed, is one test over two mutants:
`listenSilently` (bound, never accepting — this package's own name for a daemon in graceful shutdown)
plus a `parked.json` beside it. Reading the error as no-fleet fails on the **answer**; asking after
dialling fails on the **deadline**, because it cannot decide anything at all until somebody else's
shutdown ends.

### A wait for one outcome turns the defect into a timeout

Named 2026-08-10 by park/wake Task 2, mutating its own test. The assertion that a parked session
keeps its name waited for the daemon's *refusal* of a spawn asking for that name. Mutating
`completePark` to release the name killed it — in **15 seconds**, with the message *"waited 15s for
an error frame … and it never arrived"*, which names a frame that did not arrive instead of the name
that was handed away. Rewritten to wait for **either** answer and assert which, the same mutant dies
in **0.05s** saying `alex` was given to somebody else.

This is the *"say which assertion fired"* refinement one step earlier: it is not enough for the
mutation to be killed, and it is not enough to know which assertion fired — **the assertion has to be
about the thing that went wrong.** The tell is a test that waits for the *good* outcome's evidence:
whenever a property is "X is refused", the wait belongs over the whole outcome space, because the
defect is the other branch and the other branch is silent.

**It was proposed from tests that did not honour it, and a reviewer said so.** The same round left
three daemon park tests on `awaitState(id, StateParked)` — a wait for the good answer — so the
`stateLocked` ordering mutation cost **three 15-second timeouts, 45 seconds**, to say what
`awaitSettled` says in 0.02s. Same in `forkOf`, where flipping the parked fork verdict produced
*"waited 15s for the forked session in a status reply"* with the daemon's own refusal sitting unread
in the frames the failure printed; widening it to both outcomes turned that into 0.02s and the
refusal quoted. Both are now measured in the helpers' own comments. **A slow mutation is a mutation
people stop running**, which is how a battery quietly shrinks to the fast half of itself — so the
cost is part of the rule rather than a nicety, and *"an entry that names a rung and an entry that
applies it are different artefacts"* has now caught this project three times.

### And a prescribed mutation is a hypothesis about which assertion fires

The same brief prescribed two mutations, "one per assertion". Both produced the same *flag set* —
`--resume` and `--session-id` with no `--fork-session`, differing only in order — so both tripped
**both** assertions, and neither isolated anything. Worse, the first assertion (`resume && id &&
!fork`) **cannot** fire alone: it implies `fork != resume`, which is the second. It earns its keep
as a more specific diagnosis of the loud failure, not as independent coverage, and the mutation
that isolates the second one is a third the brief did not name — emit `--fork-session
--session-id` with no `--resume`, which is the silent shape itself.

**Read the predicate before believing the prescription.** A battery that reports "each assertion
killed its mutation" is worth exactly as much as the claim that they were different mutations.

### A fixture can stop being one, silently, and the guard over it then cannot fail

Named 2026-08-10 by park/wake Task 3. Its liveness test needed a process holding a session id in its
argv, and the brief's fixture was
`exec.Command("/bin/sh", "-c", "sleep 30", "--session-id", <id>)` — sh takes the extra words as
`$0`/`$1` and ignores them, so the argv reads correctly on the page and every reviewer would pass it.
**It does not survive.** `/bin/sh` execs a lone simple command in place rather than forking, so the
process `ps` actually sees is `sleep 30` and the marker is gone. The test then let the wake through,
waited out the full `testTimeout` and reported a daemon that had not refused — a *fixture* failure
wearing a *production* failure's clothes, and the obvious next move is to go looking at the daemon.

The rule generalises past shells. **Whenever a test's evidence is a property of something it
constructed rather than of the code under test, that property has to be asserted.** Here that is a
readback of the bystander's own command line, and two details decide whether it is worth anything:

- **Through a different question.** The readback goes through `inspect`, which asks about that *pid*;
  the code under test scans every command line on the machine for a marker. A precondition
  established with the function under test agrees with it by construction and can never see it break.
- **As a `Fatal`, before the behaviour.** A fixture that is not a fixture must stop the test where it
  reads as a fixture problem, not fail an assertion further down that reads as a product problem.

Same family as the vacuity floors this project already puts under every AST scan — *"reads nothing"
is the strongest possible pass for the weakest possible reason* — arriving on the other side, in a
process rather than in a parse.

### A test that cannot reach its subject agrees with every mutation of it

Also 2026-08-10, park/wake Task 3, and found by a mutation surviving rather than by review.
`TestAWakeThatCannotStartAProcessLeavesTheSessionParkedAndNamed` manufactured a failed `launch` by
pointing `PATH` at an empty directory. That takes `ps` with it — so `resumeSafe` refused *before*
`launch` was ever called, and the test passed having never executed the guard in its name. Releasing
the name unconditionally survived it.

**The tell is a precondition that is broader than the failure it is manufacturing.** "No `claude` on
PATH" was implemented as "no anything on PATH", and the extra breadth short-circuited the path under
test. The fix is to break exactly the one thing: resolve the real `ps` *before* replacing `PATH` and
symlink it into the new one.

Worth pairing with the entry above, because they are the two halves of one question — *did this test
construct the situation it names?* One fails by constructing too little, the other by constructing
too much, and neither is visible in a green run. Only a mutation of the guard finds either.

### Rung 3 was named for `identityArgs` and had never been applied to it

Found 2026-08-10 by park/wake Task 1, while moving the switch into
`internal/core/argv.go`. The rung-3 entry above is explicit that a cross product over a function's
*declared* input kills a narrowing keyed on a third field's **presence** and not one keyed on its
**value** — and it names `identityArgs`' own doc comment as the reason narrowings are the realistic
mutant. It was then never closed *there*. Constructed and run against the whole of `internal/core`
before the guard existed:

```go
case s.cfg.ForkFrom != "" && s.cfg.Model != "sonnet":
```

`Model` is `""` in one of `decorations()`' two states and `"opus"` in the other, so **both** take
the arm, every cell of the cross product builds the triple it expected, and the package stayed
green. Every fork of a session naming that model would have become an ordinary empty agent under
the child's id — exit 0, empty stderr, nothing red, which is the failure the whole switch exists to
make unrepresentable.

The first close was `TestIdentityArgsReadsNothingButTheIdentityFields`, which requires every
mention of the receiver inside `identityArgs` to be one of the fields in `identityFields`
(`ForkFrom`, `ResumeFrom` since park/wake Task 3, and `SessionID`). **That
closes the field set, not the value space, and three documents said otherwise** — see rung 5
below, which is what a reviewer beat it with. The field-set check stays, correctly described:
`buildArgs` legitimately reads `Name`, `Model`, `Effort` and `PermissionMode`, and the switch that
decides the identity block may not.

**The generalisable half:** an entry that names a rung and an entry that applies it are different
artefacts, and this project has now twice written the first and skipped the second. When a ladder
entry cites a function as its example, check whether the example is itself on that rung.

### Rung 5: guard the unit the property belongs to, and the predicate's shape, not just its inputs

Named 2026-08-10 by the review of park/wake Task 1, and won against a guard built to rung 3 and
correctly shaped for it. **An assertion over which fields a function reads does not close a value
space.** Three narrowings beat `TestIdentityArgsReadsNothingButTheIdentityFields`, all three
constructed and run **green against the whole tree**, two of them producing a punished argv shape:

| Narrowing | Why the field-set check cannot see it |
|---|---|
| `case s.cfg.ForkFrom != "" && !strings.HasPrefix(s.cfg.ForkFrom, "9")` | reads **only allowed fields**. UUIDs are hex, so forks of **one parent in sixteen** become an ordinary empty agent |
| in `buildArgs`: `if s.cfg.Model == "sonnet" { identity = identity[2:] }` | **out of scope entirely** — and `buildArgs` is the function that assembles the argv. Emits `--fork-session` with no `--resume`: exit 0, empty stderr, a plausible empty agent |
| `case s.cfg.ForkFrom != "" && forkModelAllowed()`, reading `os.Getenv` | **no receiver mention at all.** "Every mention of the receiver" defeats `preferredModel(s)`; it does not defeat `preferredModel()` |

**Ask what the property is *about*.** It is about the emitted argv, not about `identityArgs` —
`identityArgs` only hands `buildArgs` a block, and the argv is `buildArgs`' output. So the unit is
the **argv path**: the seed plus everything reachable from it by a call the package declares,
derived by walking the call graph rather than listed. That alone closes the second and third
narrowings — the reslice comes into scope, and the helper comes into scope *because the path
follows the call*.

**And constrain the form of the questions, not only their inputs.** The switch's arms are
legitimately keyed on one thing: whether an identity field is empty. So that is what is required —
a closed grammar for every expression the path uses as a truth value, including the `return` of any
single-`bool` function, which is how a predicate moves into a helper. A `HasPrefix`, a `<`, a
`len`, an env read, a tagged switch on a field, or a comparison against any string but `""` is a
build failure regardless of which field it reads. `internal/core/argvguard_test.go`.

Two things fell out that are worth keeping:

- **The raw string compare `sameSession` replaced is now a build failure**, because a `Config`
  field may only be compared against `""`. The entry two sections down argues that at length; this
  is the same ruling arriving as a shape rather than as a sentence.
- **The boundary is in the file's header rather than left for the next reviewer.** Value positions
  are unconstrained: `append(args, "--model", s.cfg.Model[:1])` leaves every static check green and
  is caught only behaviourally. A boundary a reviewer discovers is worth less than one the file
  admits.

| Rung | Mutant it kills | Method |
|---|---|---|
| Examples | the arm is deleted | a test per case |
| **Enumerate** the domain | the arm excludes a *member* | read the domain out of the code that declares it |
| **Assert statically** | the arm excludes a *value* | prove a property of the function, not of the sample |
| **Derive from the producer** | the domain itself is *wider than reality* | enumerate what can arrive, not what the type permits |
| **Guard the unit and the predicate's shape** | the narrowing keys on an *allowed* input, moves to a *caller*, or reads *nothing* | scope to everything that can produce the property, and constrain the *form* of the tests it may make |
| **Assert the premise, through a different question** | the *fixture* silently stopped being one, or the precondition is broader than the failure it manufactures | read the constructed situation back through a mechanism the code under test does not use, as a `Fatal` before the behaviour |
| **Derive a cross-surface claim from the surface that owns it** | the guard is still able to fail, and is now about the **wrong world** | never assert a fact about a part of the build you do not read |

**Rungs 1–5 close the assertion; rung 6 closes the premise.** The two entries that name it are
*"A fixture can stop being one, silently"* and *"A test that cannot reach its subject agrees with
every mutation of it"*, and they are one rung read from two sides: one fails by constructing too
little, the other by constructing too much. Neither is visible in a green run, and both were found
only by mutating the production guard. A sixth row was proposed by the review of park/wake Task 3
after the two entries shipped without one — which is how a rung gets forgotten.

### Rung 6, second shape: two writers of one artefact make the one under test invisible

Named 2026-08-11 by park/wake Task 5 (`⌃Q`'s daemon half), and it is the premise rung again — but
it fails in a way neither of the two entries above describes. Those fail by constructing too little
or too much *situation*. This one constructs the situation perfectly and then cannot **attribute**
the result, because something other than the code under test produced it.

`shutdown` calls `bookParked`, which writes every parked session into `parked.json` before the
clients are closed. `completePark` writes the **same record** for the same session, from that
agent's fan-out goroutine, whenever its `retire` gets there. So an end-to-end test that parks two
sessions, waits for the daemon to hang up and reads the file back is green in three different
worlds: the one where `bookParked` wrote it, the one where `completePark` wrote it, and the one
where both did. Three mutations proved it — all constructed, all compiled, all run against the
whole package with that test passing:

```go
func (a *agent) bookable() bool { return a.ended }                 // it ended, so book it
func (a *agent) bookable() bool { return a.ended && a.parked }     // the settled flag alone
case !a.bookable() || a.id[0] == 'b':                              // half the fleet, silently
```

**No fixture over a live daemon can separate them**, because what is being separated is which of
two goroutines got there first — and on an idle machine with two sessions, `completePark` always
does. Adding sessions, adding load or adding a barrier all change the odds and none of them changes
the question.

The closing move is **structural absence**: drive the path under test in a world where the other
producer cannot run at all. `bookParked` takes `[]*agent`, so the test builds agents with an
unstarted `core.Session` and no fan-out goroutine behind them — `completePark` has nothing to run
on, the book is empty unless `bookParked` wrote it, and the assertion is over the whole fleet in one
call so a subset skip is visible. The states come from calling the transitions `shutdown` calls
(`beginPark`, `finish`, `markParked`, `kill`) in orders `shutdown` can reach, which is rung 4: the
domain is what the producer produces, not the eight cells three booleans permit.

**Two rules out of it:**

- **When two code paths write one artefact, an assertion about the artefact is not an assertion
  about either path.** Ask what would still be true with the path under test deleted. If the answer
  is "everything", the test is about the artefact and a second test is owed the path.
- **The redundancy is usually the design, not an accident.** `bookParked` exists *because*
  `completePark` is unordered with respect to `closeClients`; the whole value it adds is ordering.
  So the thing that makes the property necessary is also the thing that hides whether it holds.

### And an ordering between two statements is a static property

Same task, same edge. `shutdown` must write the park book *before* `s.closeClients()`, because a
client whose connection dies reads that file next and a `wake` that sees the EOF on the listener
starts a daemon that reads it. The obvious test — hang up, then read the book — is a **content**
assertion taken at the edge. Move the write one statement down, below `closeClients`, and the
window is microseconds: the test fails often, passes sometimes, and a flaky guard over a
correctness property is worse than none because it gets rerun until it is green.

The property is "this statement runs before that one". The code declares it as statement order, and
statement order is static: `TestTheParkBookIsWrittenEarlyAndForgottenLate` parses `shutdown`'s own
body and requires one statement's index to be below another's, with a `Fatal` floor on each anchor
being found at all — a renamed method or a broken parse otherwise yields "no violation", which reads
as the strongest possible pass. It fails in 0.00s naming both indices. This is the same move as rung
3 applied to a different kind of property: reaching for a tighter barrier or a slower daemon is the
tell that you are on the wrong rung.

**And it is a table rather than one comparison, because the two arms pull opposite ways.** Moving
the *clear* up beside the *write* looks like tidying and is a regression: `completePark` adds an
entry from a fan-out goroutine, so a clear placed before the wait on the WaitGroup is overtaken by a
park that was still finishing and `wake stop` leaves behind the one session the operator ran it to
be rid of. So: **an entry that must exist has to be written before anybody can read the book, and an
entry that must not exist has to be removed after anything that could still write one.** Same file,
same edge, opposite ends of it — and the second ordering has no behavioural test either, because
what would falsify it is a goroutine race.

### Rung 7: a guard that asserts a fact about a part of the build it does not read will outlive that fact

Named 2026-08-11, and it is the only rung in this ladder to have been **found twice, independently,
on the same instance** before it was written down. `071a0e4` reached it by writing chapter 7 of the
user manual and noticing that the refusal it had to document was wrong; the whole-branch review
reached it by grepping for assertions that a feature does not exist. Both put it in a commit message
or a test comment and **neither put it here**, which is exactly how the entry above reports rungs get
forgotten (*"A sixth row was proposed … after the two entries shipped without one"*).

**Rungs 1–6 all ask *can this assertion fail?* Rung 7 asks *is it still about the right thing?*** —
and a green suite is evidence for the first question and silence on the second.

The instance. `cmd/wake/park_test.go` asserted, of `wake attach <parked>`'s refusal:

```go
if strings.Contains(why, "resume") || strings.Contains(why, "wake wake") {
    t.Errorf("the refusal is %q and names a verb this build does not have: a missing feature is not "+
        "trusted and a lying one is", why)
}
```

Walk the ladder against it. **Rungs 1–5: satisfied.** The assertion can fail; it is a property of the
sentence rather than of a sample. **Rung 6: satisfied.** The fixture is a genuinely parked session,
constructed through the real producer. **Not vacuous:** it matches a real string and would fire.

And it is wrong anyway. The guard encodes a claim about **the rest of the build** — *"this build has
no wake verb"* — and derives that claim from nothing. It was true the day it was written. Task 3
built `unpark`, Task 6 built `/resume`, Task 7 gave `FramePark` a sender, and the guard did not
merely go stale: it **inverted**. Writing the correct refusal became a build failure, so the lie it
was written to prevent is the lie it went on enforcing — an operator who wanted their conversation
back was pointed at `wake fork`, which makes a second session, spends a second name and leaves the
original parked.

**The second instance is not a test at all**, which is what makes this a rung rather than a testing
trick. `deferred.md`'s *"nothing in the shipped surface sends `FramePark` or `FrameWake`"* is the
same shape in prose: a claim about the whole build, in a document whose own closing condition
(*"the task that binds `⌃C` closes this"*) was met by Task 7 — which closed the two entries either
side of it and walked past this one. Its watch was set on a `wake resume` **shell verb** that still
does not exist, while the claim went wrong the day a `/resume` **command** shipped. Both entries in
the class were found by **reading**, never by running, which is the signature rung 4's entry reports
for itself.

**The closing move is one this project had already found and applied at the neighbouring site.**
Task 7 did not delete `TestResumeWithNothingParkedNamesNoKeyThatDoesNotPark` when `⌃C` started
parking — it **inverted** it into `TestResumeWithNothingParkedNamesTheKeysThatPark`, which requires
both glyphs *and checks each one against `legendEntries`*. That second half is the whole of rung 7:
the claim about the rest of the build is **derived from the surface that owns it**, so the sentence
cannot outlive the keys it names. The same move on `park_test.go` derives "is there a way back into
this conversation" from `ui.commands` — the closed set that already exists and is already counted.

**The tell is grammatical, which is what makes the audit cheap.** The failure message is about *the
build* rather than about the code under test: *"a verb this build does not have"*, *"nothing sends
this frame"*, *"the manager is not built"*, *"not yet"*. A test sentence that quantifies over the
whole build has taken a dependency it does not read.

**The audit, which is the actionable half: when a task ships a capability, grep the test tree for
assertions that the capability does *not* exist.** That is the mirror of *"adding a member to a
producer's range is the cheapest audit this project has"* — adding a **feature** to the build is the
other one. The phrasings worth sweeping are *"does not have"*, *"is not built"*, *"does not exist"*,
*"nothing sends"* and *"not yet"*, restricted to `_test.go` and read for whether the subject is the
build or an input.

Run at the close of this branch it produced **two hits and no noise**, which is the right shape for
an audit: `TestAttachRefusesAParkedSessionAndNamesAVerbThatWorks`, now inverted, and
`core.TestWithNoDefaultAddresseeNothingIsAddressed` (*"the manager is not built"*), which is still
true. Everything else the grep matched is about an input rather than about the build — a tool that
does not exist, a row that does not exist — which is the distinction to apply by hand.

**What the inversion did not do is derive the claim, and that is the half still owed.** The refusal
guard now *requires* the resume route and forbids a `wake resume` **shell** verb, which is narrower
and currently true — but it is still a sentence asserting a fact about `cmd/wake`'s verb set while
reading nothing of it, so it will expire the same way the day somebody adds the verb. Rung 7 is only
fully paid when the claim is read off `ui.commands` and the verb dispatch, the way the neighbouring
test reads `legendEntries`. Recorded in `deferred.md`.

| Rung | Mutant it kills | Method |
|---|---|---|
| 7 | the guard is still able to fail, and is now about the **wrong world** | derive a cross-surface claim from the surface that owns it; never assert a fact about the build you do not read |

**Why it is a rung and not an instance.** Every rung below it is a relationship between a test and
*its own subject* — the assertion, the domain, the predicate's shape, the fixture. Rung 7 is a
relationship between a test and a part of the build it never touches, and no amount of rigour about
the first can detect a failure of the second. It is also the only rung whose failure mode is
**active**: rungs 1–6 fail by not noticing a defect, and this one fails by making the fix a build
error.

### The 800-line convention is for non-test files, and `harness_test.go` is where that shows

Named 2026-08-11 by the review of park/wake Task 5 (m7), which noticed the task splitting
`park_test.go` at 901 lines while growing `internal/daemon/harness_test.go` from 994 to 1030 in the
same change.

Both are correct and the rule is worth stating once so the next person does not have to work it
out. `TestNoNonTestFileCrossesTheHardMax` is deliberately scoped to **non-test** files, and
`argv_test.go`'s own comment says why: *"a table test's size is its coverage, and splitting one by
line count rather than by subject is how a suite loses the reason it was arranged."*
`internal/core/session_test.go` is 1,395 lines and is not a defect.

So the test-file rule is **subject, not size**. `park_test.go` was split because it had grown a
second subject — one session parking, and the fleet parking on the way out — and the split fell out
of that, with the line count as the prompt rather than the reason. `harness_test.go` has one
subject (starting a daemon and attaching a client to it) and `awaitClose` is a method on
`testClient`, so it belongs there at any length. **A test file worth splitting is one where two
paragraphs of its header describe different things**; a long one whose header describes one thing is
long because its subject is.

### The identity flags have a home, and the ruling is a check rather than a paragraph

`airlock_test.go`'s SCOPE paragraph has said since it was written that Wake's Claude-specific CLI
flags are "a real second leak with a different shape (argv, not a decoder)" that "want their own
ruling, not a silent extension of this one". park/wake Task 1 gave the identity third of it one:
`--session-id`, `--resume`, `--fork-session` and `--continue` are spelled in
`internal/core/argv.go` and nowhere else in the tree, enforced by `argv_test.go`.

Two decisions inside it, each against a way the obvious version is wrong here:

- **Literals, not bytes.** Six non-test files name one of these flags in a *comment* explaining
  why it matters somewhere else — `cmd/wake/attach.go`, `internal/rpc/wire.go`,
  `internal/daemon/reap.go`, `reap_unix.go`, `spawn.go`, and `internal/core/session.go` itself,
  whose `Config.ForkFrom` doc has said *"a plain `--resume` reuses the parent's id"* since before
  the split. Prose cannot put a flag on a command line, and a guard those sentences failed would be
  paid for in comments deleted to get the build green. It is the ruling `airlock_test.go` already
  made in its own header. **This sentence said "five" and was wrong the day it was written**, in
  four artefacts at once, inside the change that re-derived three other numbers correctly — so the
  test's own copy now enumerates the files instead of counting them.
- **Reuse the walk, do not write a second one.** `goFiles` skips `.worktrees`, and this repository
  is developed in worktrees *under its own root*. A fresh `filepath.WalkDir` finds the sibling
  branch's `argv.go` and reports it as a second file spelling the flags — a tree-wide guard that
  goes red because a worktree exists is a guard somebody deletes. This is the same lesson
  `conflict_test.go` recorded from the other side, where inheriting a skip list silently excluded
  the tracked `docs/notes/`.
- **…and then hold the shared walk to the producer.** Reusing `goFiles` inherits its hand-written
  `skipDir`, which matches a directory's **base name at any depth**. Adding one entry hid a whole
  package from both new guards *and* the airlock's leak check while 60 files were still walked
  against a floor of 20 — verified both ways, with a planted 900-line file and a planted `--resume`
  literal. **A count is not a floor when the hole is package-shaped.** The floor is now coverage:
  every package `go list ./...` reports with non-test files must have at least one file in the
  walk. Asked of the toolchain rather than derived from the tree, because deriving it means writing
  the second walk this entry's previous bullet rules out.

Matching is **substring-within-a-literal**, not equality: `SessionArgvMarkers` builds
`"--session-id "` with the id appended, and `"--resume=<id>"` is the same flag again. Equality sees
neither.

---

## A number in a comment that nothing asserts is wrong by default

`airlock_test.go`'s SCOPE paragraph said *"the twelve claude-specific CLI flags in session.go's
buildArgs"*. It was **thirteen on the day it was written** — `ec2748e`'s own `buildArgs` spells
thirteen, checked with `git show`. The fork plan then inherited the error and prescribed
"fourteen", which would have been wrong in the same direction and by the same amount, a second
time.

Nothing went red at any point, because it is prose. The counts that *are* held —
`policedWordCount`, `allowlistPairCount`, `ask_test.go`'s per-class ask counts — have a test
behind them and are the model to copy. The rule for the ones that do not: **count from the
source, never increment the sentence.** If a number is worth stating it is worth a `grep -o` in
the commit that states it.

---

## Measurement obeys the same rule as tests

Applied after the controller reported two Criticals from a live run that were both artefacts of
its own detector. `pgrep -fl "claude --print"` never matched, because the binary resolves through
a wrapper that rewrites the argv — the agent was running the whole time. The A/B's control was
not isolated either: another agent's tests were spawning processes concurrently, and the filter
was one hard-coded name.

**A check whose negative result you have never seen turn positive is a check that cannot fail.**
Before trusting "it isn't there", make it be there on purpose and confirm you can see it. The
failure injection that settled this took four minutes and should have come first. And prefer a
detector with no pattern in it at all — listing processes by PPID cannot fail this way.

---

## 2026-08-12 — what the room costs at 30 agents, with a manager

Phase 2 Task 16. Every figure carries the one-minute load average it was taken at, because this
project has had twelve hours of timings taken under 24 phantom cores of orphaned busy-loops and
the only defence is writing the number down beside the figure. Machine: Apple M5 Max, 18 cores,
darwin 25.4.0, `go test -bench`, five rounds each unless stated.

**Verdict: the room is cheap to leave open, and the daemon underneath it is not.** The TUI at 30
agents with a manager idles at **0.80 % of one core**; the daemon spawns **86,400 `ps` processes a
day** at the same fleet size, and it does so *because* nothing is happening.

### The client, per event — and the number that was being quoted is the wrong one

`docs/live-testing.md` carried *"`Observe` at 34.2 ns"* as the room's per-event cost. Two things
are wrong with it and both matter.

It is the **chatter** arm of a four-arm benchmark, quoted as if it were the whole. An event that
moves an agent's record pays the copy-on-write over a 30-entry map, which is **1.87–2.35 µs** —
forty times the quoted figure. Chatter itself now measures 58.2–59.3 ns rather than 34.2 ns.

And `Observe` is not what an event costs the *room*. It returns the event; `Room.Append` then
renders it. `BenchmarkRoomFold` prices the pair, and it is the number to quote:

| one event, 30-agent fleet, 81-column room pane | ns/op | B/op | allocs | load |
|---|---|---|---|---|
| chatter — changes no record, draws nothing | 62.8–76.0 | 0 | 0 | 1.55–1.85 |
| tool call — moves the sidebar row, draws nothing | 1,866–2,315 | 7,672 | 35 | 1.55–1.85 |
| prose — moves the row *and* is drawn | 32,998–34,743 | 97,775 | 477 | 1.55–1.85 |

So a line of agent prose costs the room **33 µs and 98 KB**, against a `View` of 250–390 µs that
is paid once per *batch* of up to `takeLimit` (256) frames. The fold, not the draw, is the larger
half of a busy fleet's cost — which is the opposite of what the recorded figures suggested.

**What that pairing isolates**: the three arms differ only in what `fold` does with the event.
Chatter-to-tool-call is the price of the immutability; tool-call-to-prose is the price of the
room. **What it does not**: the frame (per batch, not per event), and anything below the socket.

**Every event in it differs from the one before.** `Observe` returns the receiver untouched when a
record did not move, so an arm repeating one identical tool call measures a map lookup and reports
the copy-on-write as free — 40 ns instead of 2 µs. The first draft did exactly that. The line
counter cannot see it, because a fold that returned early draws exactly as many lines as one that
did the work.

### The client, at idle — the measurement this project kept asking for

200×40, two panes, both sidebars, a 3,000-line room and a conversation open beside it. Agent count
is the **only** thing that varies between arms; the fixture fails if the whole fleet is not on
screen, because three arms drawing one frame is what "the fleet costs nothing" would look like if
it were false.

| idle, 10-second window | % of one core | load |
|---|---|---|
| 1 agent | 0.724–0.787 | 1.68–1.83 |
| 30 agents | 0.799–0.818 | 1.68–1.83 |
| 30 agents **and a manager** | 0.755–0.855 | 1.68–1.83 |
| 30 agents, room re-wrap made unconditional **on purpose** | **35.05–35.22** | 1.68–1.80 |

Thirty agents cost about **0.07 points** over one. The manager costs nothing measurable — its
range straddles the 30-agent range. And the last row is the check being made to fail: **44×**,
which is what the width fast path in `Room.View`/`SetSize` is holding back.

The Phase 1 baselines are not superseded by these, because they are a different shape:
`BenchmarkIdleCPU` sizes its terminal at **100 columns**, below `dmTakeoverColumns`, so it has
always measured *one* pane. Re-measured today: **0.481–0.520 %** empty and **0.627–0.658 %** with
3,000 events, against the recorded 0.452–0.499 %. So the room's four regions add roughly **0.15
points** over the one-pane DM at the same transcript length, and a fleet adds 0.07 on top.

**The blink is real and the worry attached to it was not.** `deferred.md` says *"`textarea.Blink`
re-renders forever while nothing happens and `View` is unmemoized"*. Counted directly: `View` runs
**10 times in 5 seconds** of idle — the blink, twice a second, exactly as described — and
`renderRoom` runs **zero** times. The blink is about 9 % of the idle figure; the rest is Bubble
Tea's own 60 Hz renderer repaint, which scales with what is on screen and not with the fleet.

**The first version of this benchmark measured an empty room, and the mutation is what found it.**
The fixture fed its transcript through `eventMsg`, which is `App.appendEvent` — a direct write into
whichever conversation is open, never `Fleet.Observe`. With a DM open, all 3,000 events landed in
the DM and the room held **nothing**. Making the room re-wrap on every frame then moved idle by
0.03 points instead of 44×, which is how the defect surfaced: *a mutation that fails to move the
number is a statement about the fixture before it is a statement about the code.* It now feeds real
`rpc.FrameEvent`s and asserts both panes are non-empty.

### The frame, and the two figures that moved

| | measured | recorded | load |
|---|---|---|---|
| `View`, DM takeover, 3,000 events | 253.8–257.1 µs | 253.8 µs | 1.56–3.56 |
| `ViewOnePane`, 200-column room | 388.5–388.9 µs | 379–404 µs | 1.56–3.56 |
| `ViewTwoPanes`, 99+100 | 302.7–304.7 µs | 299–317 µs | 1.56–3.56 |
| `ViewSecondPane/panes=1` | 121.4–125.1 µs | 122–123 µs | 1.56–3.56 |
| `ViewSecondPane/panes=2` | 307.1–310.5 µs | 302–307 µs | 1.56–3.56 |
| one re-wrap at 3,000 events | **199.6–202.5 ms** | 248 ms | 1.56–3.56 |
| a 40-column divider drag | **125.7–126.2 ms** | 93 ms | 1.56–3.56 |

The re-wrap got **cheaper** and the drag got **more expensive**, on the same machine in the same
run. A drag is one re-wrap plus forty motion messages, so the two cannot both be explained by
hardware: the re-wrap moving down 248→200 says this machine is faster than whichever one recorded
248, and the drag moving up 93→126 against that is a real regression of roughly **70 %** once the
machine is divided out. The re-wrap count is still exactly one per drag —
`BenchmarkDividerDragAcrossFortyColumns` fails otherwise.

**The candidates first written here were wrong, and the review decomposed it** (load 1.84–1.99):

| | measured |
|---|---|
| full drag (40 motions + settle) | 120.9–122.0 ms |
| settle alone, at endpoint column 139 | 118.8–119.2 ms |
| 40 motions alone, 3,000 events | 2.53–2.66 ms |
| 40 motions alone, **0 events** | 2.57–2.61 ms |

118.9 + 2.6 = 121.5, exactly. So **98 % of the figure is one re-wrap at the endpoint and 2 % is the
motions** — and the motions are transcript-*independent*, which means `mouse.go`'s "a motion that is
not a divider drag costs nothing" property is intact. The composer's target line and the pinned card
both live in `chromeHeight`, which runs **once per settle**; neither can move a 119 ms re-wrap. They
were named as candidates because they were what Phase 2 and 3 added, which is a plausible story
rather than a measurement.

What the number is actually a function of is the **endpoint column**: 59.5 ms at 100, 92.1 at 120,
119.0 at 139. `dividerColumnOf` derives that from `Layout.Regions()`, which Phase 2 changed — so
part of 93→126 may be endpoint drift rather than a slowdown at all, and comparing to 93 without
knowing where that drag ended is comparing two different measurements. **The place to look is
`Room.renderAll`/`roomBlock` across 3,000 blocks**, not the chrome.

Left as a lesson as much as a number: the first diagnosis named the two things that had recently
changed, which is how a plausible story gets recorded as a finding. The decomposition took one
benchmark run.

### The manager, which is a 31st session nothing had priced

| | measured | load |
|---|---|---|
| `roll_up`, 30 agents in one workspace | 29.5–30.7 µs | 2.43–2.90 |
| … and with the manager's own row on the report | 28.8–29.2 µs | 2.43–2.90 |
| `roll_up`, 30 agents in 30 workspaces | 35.4–36.4 µs | 2.43–2.90 |
| … and with the manager | 35.3–35.6 µs | 2.43–2.90 |
| `list_agents`, 30 agents | 20.4–20.7 µs | 2.43–2.90 |
| daemon `fleet()`, 30 agents | 5.26–5.36 µs | 2.19–3.03 |
| daemon `fleet()`, 31 agents | 5.40–5.46 µs | 2.19–3.03 |
| daemon `fleet()`, 30 agents **and a manager** | 5.38–5.46 µs | 2.19–3.03 |

**A manager plus 30 agents is not meaningfully different from 30.** `fleet()` cannot tell the
manager from an ordinary 31st row — 5.40–5.46 against 5.38–5.46 — which is
`internal/daemon/manager.go`'s "keyed on the name, applied in `launch`" design costing nothing at
report time. `roll_up` walks 31 rows and emits 30, because `liveSessions` excludes the manager, and
the filtered row is inside the noise.

And there is **no clock in `internal/mcp`** — `TestNothingInThisPackageKeepsTime` derives that from
the source — so a `wake mcp` process beside thirty agents costs 30 µs when the manager asks and
zero in between. The manager's own `claude` process is a 31st `claude` on the machine and that is
its real cost; nothing in this tree can measure it without spending money, and it belongs in
`docs/live-testing.md`.

### The park book, which is on the startup path

| | measured | load |
|---|---|---|
| ⌃Q: 30 sessions parked, one whole-book rewrite each | 3.86–3.91 ms | 2.19–3.03 |
| … one session parked into an empty book | 103.0–107.3 µs | 2.19–3.03 |
| a start reading 30 parked records back | 212.5 µs | 2.19–3.03 |
| … one record | 17.0 µs | 2.19–3.03 |

`parkBook.add` rewrites the whole file through a temp and a rename on **every** park, so ⌃Q at 30
agents is 30 rewrites — but the per-park cost only goes 103 µs → 130 µs average, because the
atomic rename dominates and the JSON does not. Four milliseconds, on a path that already waits for
processes to die. `restoreParked` is 212 µs before the accept loop, which is what a `wake` waits
behind while `EnsureRunning` decides whether a daemon exists. Neither is worth changing.

**The fixture had to stop using hex names to measure the real path.** `w00`, not `a00`:
`normalizeName` refuses a name made only of hex digits, so a hex fixture sends `restoreParked` down
its *fallback* branch — claim a pool name — and prices something production does not do. The guard
said so in its own log on the first run.

### The daemon, which is where "cheap to leave open" is broken

`deferred.md` has carried this as arithmetic since Phase 1. It is now an observation:
**1.02 `ps` spawns per quiet agent per tick**, counted over eight ticks with a real daemon, real
agents and a counting `ps` on `PATH` (load 1.5–2.0). At the production `silenceLimit` of five
minutes `livenessInterval` clamps to 30 s, so 30 agents is **86,400 process spawns a day** — the
figure the note predicted, now measured rather than derived, and derived in the test from
`livenessIntervalFor(defaultSilenceLimit)` rather than restated.

The non-negotiable says *no process spawned on a timer that could be spawned on a suspicion*. The
suspicion `probeQuietAgents` is gated on is **quiet for longer than `silenceLimit`**, and that is
true of every agent in a fleet nobody is typing at. So the gate that makes a busy fleet free makes
an **idle** one maximal, which is precisely the state the rule is written about. Thirty sequential
`fork`+`exec`s every thirty seconds, all night, to learn that nothing changed.

Set against the TUI: 0.80 % of one core for the whole room, and one process spawn per second from
the daemon beside it. **The expensive half of Wake at idle is the half with no user interface.**

Not fixed here, and the reason is unchanged: one `ps` listing every pid at once, or probing only
agents something is owed from, are both cheaper and neither weakens the three-valued answer — but
that is a daemon change with its own review.

### What each detector was made to fail

Nothing above is trusted that has not been made to move:

- the probe count: `probeQuietAgents` made a no-op → 246 spawns became **0**, both arms failed;
- the idle figure: the room's width fast path removed → 0.80 % became **35.1 %**;
- the fixture behind it: the same removal moving idle by 0.03 points is what exposed a benchmark
  measuring an empty room;
- the park-book fixture: hex names, caught by `normalizeName`'s own log;
- every benchmark asserts its own output — a digest that truncated, a report that lost rows, a
  room that stopped drawing would each otherwise report as an improvement.

---

## Ruling: the airlock leak (deferred I1) — move what is portable, enumerate what is not

The rule says "one file is the entire cost of staying Codex-ready." Reality is three files across
three packages. Restating the rule to match reality would be the cheap answer and the wrong one,
because the leaks are not all the same kind:

| Leak | Where | Kind |
|---|---|---|
| `<local-command-stdout>` open/close | `ui/dm_blocks.go` | **pure wire format** — belongs behind the airlock, no argument |
| `old_string` / `new_string` | `ui/dm_blocks.go` | Claude's Edit-tool input keys |
| eight tool→argument mappings | `render/tool.go` | Claude's tool vocabulary |
| `compact_boundary`, `permission_denied` | `ui/dm_blocks.go` | `KindSystem`'s **designed** passthrough — `Event.Text` *is* the raw subtype |
| `allowed` | `ui/dm_blocks.go` | same, for `KindRateLimit` |

**Ruling: option (b), but only for the top three rows.** `protocol.go` grows `ToolCall.Display`
(the one argument worth showing, resolved from the map moved inside) and `ToolCall.Diff{Old,New}`,
and strips the local-stdout markers in the decoder. A port then rewrites one file, which is what
the rule claims.

**The bottom two rows stay, and the rule gets amended to say so.** Inventing an `EventKind` per
system subtype is the worse trade: the set is open — `hook_started`, `thinking_tokens`, `status`,
`compact_boundary`, `permission_denied`, and the 2.1.226 binary contains states no recording has
shown — and an unrecognized subtype must still arrive as *a system event*, not degrade to
`KindUnknown`. So `Event.Text` carrying a raw subtype is a deliberate passthrough with a reason,
and the honest rule enumerates it rather than pretending it away.

**Sequencing, and this is why the ruling is worth making now:** the subagent spike established
that `Event` must grow fields anyway — `parent_tool_use_id`, `subagent_type`, `task_description`,
and `agent_id` on a permission request. Those changes land in the same three files. Doing I1
alongside them is one review instead of two, and doing it *before* them would make the second
pass rewrite the first. **Implement after Task 10 merges**, bundled with subagent attribution.
Not before: `dm_blocks.go` is in Task 10's blast radius.

---

## Ruling: the working notes are tracked, and live in `docs/notes/`

Moved from a gitignored `notes/` on 2026-08-09, after being ignored cost real work twice in one
session:

- An implementer in a worktree wrote its deferred items into **its own copy** of the ignored
  file. They did not travel with the branch. It caught that and reproduced them in its report, so
  nothing was lost — but only because it thought to check.
- The same implementer could not read four review findings at all, because I had handed them over
  in a **message** rather than a file its worktree could reach.

The original justification was "a working scratchpad, not a commitment to anyone." That stopped
being true a while ago. `deferred.md` is what a fresh agent must read before touching anything;
`decisions.md` holds the named failure modes, which are worth more than most of the code;
`v1_goals.md` is the roadmap with the corpus findings behind each item.

`/notes/` stays ignored, for genuine scratch. Two rules that follow:

- **Hand a subagent findings as a file path**, never pasted into a prompt. A path is readable, is
  reviewable, and does not consume the controller's context on every later turn.
- **If a note has to survive a branch, it must be tracked.** An ignored file is per-worktree, and
  a worktree is the unit of work here.

---

## Finding: bubbletea v1.3.10 names no `ctrl+shift+<letter>`, so `⌃⇧A` cannot be bound

Task 6 (the right sidebar) shipped "jump to the next agent that needs you" as `Roster.Next` and
bound **no key to it**, and did not put one in the legend either. The reason is in the library:
`tea.Key.String()` in v1.3.10 produces no name of the form `ctrl+shift+a` for any letter. On a
terminal without the Kitty keyboard protocol the chord arrives as the single byte `0x01` —
indistinguishable from a plain `⌃A`, which is a different intent entirely.

So the choice was between a legend entry with no key behind it, a key that silently does the
wrong thing on most terminals, and neither. **Neither** is what the legend bijection already
requires: `TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed` fails on a glyph with no
binding *and* on a binding with no glyph, so adding one half was never available. This is the
same rule that deleted `⇧⇥` from the hint line — a missing feature is not trusted and a lying one
is.

Task 10 owns picking the actual key, since it owns `App.key` and can probe what the terminal
reports. The behaviour is already written and tested; only the trigger is open.

### Probed, and the finding is stronger than "not named": nothing arrives at all

Task 10 ran the probe rather than trusting the reasoning above, and the reasoning was
*understated*. A throwaway `tea.NewProgram` fed byte sequences through `tea.WithInput` and printed
the `tea.KeyMsg` each produced, against bubbletea v1.3.10:

| pressed | bytes | what bubbletea reported |
|---|---|---|
| `⇥` | `\t` | `tab` |
| `⇧⇥` | `\x1b[Z` | `shift+tab` |
| `⌃⇧A`, no protocol | `\x01` | `ctrl+a` — indistinguishable from a plain `⌃A` |
| `⌃⇧A`, Kitty CSI-u | `\x1b[97;6u` | **no message at all** |
| `⌃⇧A`, xterm modifyOtherKeys | `\x1b[27;6;65~` | **no message at all** |
| `↵` | `\r` | `enter` |
| `⌃↵`, Kitty CSI-u | `\x1b[13;5u` | **no message at all** |
| `⌃↵`, xterm modifyOtherKeys | `\x1b[27;5;13~` | **no message at all** |
| `⌃D` `⌃W` `⌃G` `⌃R` `⌃O` | `\x04 \x17 \x07 \x12 \x0f` | `ctrl+d` `ctrl+w` `ctrl+g` `ctrl+r` `ctrl+o` |
| `⌃F` | `\x06` | `ctrl+f` |
| `⌃Q` | `\x11` | `ctrl+q` — the **decoder** names it; whether a tty delivers it is `IXON`'s question, not this one |

So enabling the Kitty protocol would not rescue either chord: v1.3.10's decoder swallows both
encodings and produces nothing, which means a terminal that *does* speak them is a terminal where
the chord is silently eaten. **`⌃⇧A` and `⌃↵` are unbindable in this library, full stop** — not
merely unnamed, and not fixable by a terminal setting.

**Resolution: `⇥` carries the next-blocked jump**, and it is in the legend as `⇥ next blocked`.
It is named by every terminal from a single `\t`, needs no protocol, and nothing else in this
build binds it - bubbles' text area has no tab binding, so nothing is shadowed either. It jumps
to the next agent `Roster.Next` finds blocked and opens that agent's DM; with nothing blocked it
selects nobody and says so, which is `Roster.Next`'s own rule about a cursor that would otherwise
lie about the fleet.

### Superseded at Task 11: `⇥` is pane focus and `⇧⇥` is the next-blocked jump

The resolution above was made when there was one composer on screen. Task 11 put a second one
beside it, and with a conversation open the room's composer could then be reached only with a
mouse - which made `@all` mouse-only for as long as one was open, in a keyboard-first app.

`⇥` conventionally means *move the focus between panes*, so it took that, and the ruling
permitting it was recorded against Task 11 in `deferred.md` before Task 10 shipped. The jump moved
to **`⇧⇥`**, which the same probe already covered: `\x1b[Z` → `shift+tab`, from every terminal,
with no protocol involved. The text area binds neither tab key, so both are taken without
shadowing anything. `keyprobe_test.go` asserts both by byte, so a bubbletea upgrade that changes
either lands in CI.

One binding does two jobs the brief asked for separately, and that was the point of the shape:
`⇥` walks a **ring** of the room and every conversation that has been opened, so with one open it
is a two-pane toggle and with three it is the cycle key. The ring is fixed and ordered by when
each was opened - attention rank reorders between frames, and a ring that reorders under a
repeated press revisits one conversation and skips another.

**The takeover width needed a rule rather than a special case.** Below 120 columns the room is not
drawn, and focusing a pane that is not drawn puts every keystroke into a composer nobody can see.
The rule is: *⇥ focuses the next conversation, and a conversation you focus is one you can see* -
so at those widths focusing the room closes the DM, and at every wider width it is already there
and nothing else happens. `resizePanes` enforces the same invariant from the other direction,
because a window manager can cross the takeover with the room focused and no key pressed at all.

**Broadcast stayed `@all` and gained no chord**, which is `core.BroadcastName`'s own argument read
forwards: a chord that depends on a terminal is worse than a word that is typed. The composer now
draws `→ @all · 3 turns` above the legend, so the cost is visible before ↵ either way.

**The probe is now a test rather than a note telling somebody to re-run one.**
`internal/ui/keyprobe_test.go` drives `tea.NewProgram` over `tea.WithInput` with these byte
sequences and asserts both halves — the five keys the room binds are named, and the four chord
encodings produce nothing. It needs no terminal, so it runs in CI: a bubbletea upgrade that
changes key decoding fails there with this table in front of whoever did the upgrade. A note
telling the next person to re-run something is a note they will trust instead.

If `TestTheChordsSpecSixAsksForProduceNoKeyMessageAtAll` ever goes red, that is not a defect —
it is the day `⌃⇧A` became bindable, and the legend can say so.

---

## Failure mode: a merge whose two sides are identical still has to be resolved

Merging Task 6 into the Phase 2 trunk produced two collisions that were *deliberate* — `maxBadge`
and `unreadBadge`, carried byte-identically on both sidebar branches because each branch had to
compile alone and neither could import the other's unwritten file. Task 7's report predicted it
in writing and said whichever merged second deletes one copy.

Two ways that still went wrong, and both are worth recognising:

- **A regex that deletes "the duplicate" deletes more than the duplicate.** Removing `maxBadge`
  from `roster.go` by pattern took the `Roster` type's opening with it and left `fmt` unused. The
  fix is to delete by exact declaration and let the compiler say what is left over — the build
  error after each removal is the check, not the regex's own confidence.
- **A conflict whose sides are identical is silently easy to commit unresolved.** `CLAUDE.md`
  reached a pushed commit carrying `<<<<<<< HEAD` / `=======` / `>>>>>>>` around **two identical
  copies of the same table row** — nothing about the content signalled a problem, and no test
  read the file. `internal/core/conflict_test.go` now walks the tracked tree for those markers,
  because four more merges of this shape are coming.

---

## Failure mode: a non-vacuity guard that waits for something the test asserts will not happen

`stayedIn` asserts two things about a window — every state the daemon reported was the one named,
*and* that it reported the session at all, because an empty window would otherwise pass by having
observed nothing. The second half is the guard, and it was satisfied by luck.

The daemon pushes a status only when a state has **moved** (`server.statesChanged`). Both callers
of `stayedIn` are asserting that a state **does not move** — a quiet agent stays idle,
a broken `ps` does not turn it silent. So the only thing that could ever put a frame in their
window was an announcement left over from their own setup, and whether it landed inside the window
or before it was decided by whether the agent's output beat the fan-out to the client. Two
unrelated timings. On a loaded CI runner the announcement came first and the window was empty:
`TestABrokenPsDoesNotTurnAQuietAgentIntoAKill` failed two of its three subtests on 2026-08-10
while twelve other runs of the same commit passed.

Three things worth recognising:

- **The failure was in the guard, not in what it guards.** An empty window is a `Fatal` about the
  harness, so on every run where the window happened to be empty the mutation these tests exist to
  catch — a living agent flipped to silent — was never held at all. The luck was load-bearing in
  both directions: sometimes it failed the build, and the rest of the time it quietly did nothing.
- **A window has to carry its own evidence.** `watchStates` now asks for status through the whole
  window rather than waiting to be told. Its question was always "what did the daemon say about
  this session over this interval", and it counted replies and pushes alike — so asking makes the
  evidence guaranteed rather than incidental, and changes nothing about what the assertion means.
  "The daemon speaks unasked" is a separate property with its own helper (`awaitState`).
- **Asking has an invariant attached.** `await`'s doc rests on replies being one to one with
  questions; a window that ended with a question outstanding would leave its reply for some later
  helper to match, answering a newer question with an older snapshot. So the window does not close
  until every question it asked has been answered.

`TestAWatchedWindowIsEvidenceWhenTheDaemonHasNothingToAnnounce` makes the precondition deliberate —
it drains the socket quiet before opening the window — so the race reproduces on demand instead of
on a loaded machine. Mutation-verified: with the asking removed it fails every run, with the
message CI printed.

---

## Failure mode: `kill $PIDS` does nothing in zsh, and the silence is the whole problem

Twenty-four busy-loop shells were found spinning at ~75% CPU each, **twelve hours** after the
command that started them had finished, holding this machine at load average 55. They flaked a
`make test` run that passed on retry — which is how they were noticed at all.

They came from a load-generating test harness shaped like this:

```sh
for i in $(seq 1 $((N + 6))); do (while :; do :; done) & done
LOADPIDS=$(jobs -p)
...
kill $LOADPIDS 2>/dev/null     # <- does nothing
```

**The root cause is zsh, not the harness's logic.** zsh does not word-split an unquoted parameter
the way bash does, so `$LOADPIDS` arrives at `kill` as **one argument** containing embedded
newlines. `kill` reports `illegal pid: 44916\n44917\n…` — and `2>/dev/null` throws that away, so
the harness prints "load killed" and exits cleanly having killed nothing.

This is the **second** occurrence in this project; the first left 22 of them at load average 82.
The lesson recorded then was "use an argv marker and `pkill -f`", and it was not enough, because it
named a remedy without naming the mechanism — so the next author wrote `kill $LOADPIDS` again and
had no reason to doubt it. **I then hit the identical bug while cleaning these up**, with `for p in
$PIDS; do kill -9 "$p"; done`, which failed the same way for the same reason.

What actually works:

```sh
ps ax -o pid,command | grep 'MARKER' | grep -v grep | awk '{print $1}' | xargs -n1 kill -9
```

Three rules, in order of how much they buy:

- **Never discard the stderr of a cleanup command.** `2>/dev/null` on a `kill` converts "I killed
  nothing" into "I killed everything", and there is no later signal — a background process that
  should have died is invisible until something else it degrades gets blamed.
- **Split explicitly**: pipe to `xargs`, or use `${(f)VAR}` in zsh. Do not rely on word-splitting
  in a shell whose word-splitting you have not checked.
- **Verify the cleanup, in the same command that performs it.** Count the survivors afterwards. A
  cleanup nobody counts is a cleanup nobody has seen work — the same rule as "a check whose
  negative result you have never seen turn positive is a check that cannot fail."

And the reason this matters beyond hygiene: `internal/daemon` has a documented history of
**load-dependent test failures**, and this project has twice diagnosed a "flaky" test that was
really a real bug. A machine carrying 24 orphaned spinners is a machine where that diagnosis is
guesswork. **Check `uptime` before believing a flake.**

## Ruling: undo for a speech act to a model is a category error, and the only place one can exist is before the write

Named 2026-08-12 while re-scoping §17's *"undo for destructive actions"* against the build.

**The item was written before park existed.** Traced against the tree, three of its five actions had
already been answered: `⌃C` and `⌃Q` are reversible (`/resume`, the park book), and `wake stop` is
irreversible **on purpose** — spec §2 makes it the one ending there is no way back from, and the
quit verb clearing the park book is that decision defended from the other side. What was left was
the interrupt and the deny.

**And for those two, "undo" names something that cannot exist.** Both are speech acts: an interrupt
is *stop*, a deny is *no*. Once the bytes are on the agent's stdin the model's context holds the
fact, and this project's own recordings say the wire has no retraction — a well-formed allow after
an ask is gone produces no frame, no error and no tool run; whether a denied ask is ever re-put is
unrecorded; an interrupted turn is billed. Anything after the write is a **new turn**, and a new
turn is not an undo however it is labelled.

> **So the only side of the socket on which an undo can exist is the one before the frame is
> written, and the way to create a moment there is to make the destructive keystroke take two
> steps.** The confirmation is not a substitute for the undo. It *is* the undo, moved to the only
> place it fits.

**That reframing is what makes the second keystroke payable.** This project has already ruled
against bounding by asking — *"bounding by asking degrades to reflex approval at thirty cards a
day"* — and that ruling stands for a confirmation whose purpose is to make you think twice. It does
not reach a confirmation whose purpose is that **the accident is no longer expressible**: the
question is not "are you sure" but "was that keystroke a decision or a character", and a repeated
letter answers it without asking anything.

**A grace window on a clock is the shape to reject.** It was the obvious reading of "undo", and it
is wrong three ways: it delays the action the operator asked for (fatal for `⎋`, where *"stop what
you are doing"* is the most urgent thing in the app), it costs a timer per keystroke, and it makes
the surface lie for the length of it. A window bounded by the *next keystroke* has none of those
and is strictly more responsive to the operator's actual attention.

**The generalisation, for the next verb somebody wants to make undoable:** ask what the far side has
already been *told*. If the answer is "nothing yet", an undo is a state change and is cheap. If the
answer is "everything", the only honest options are a guard before the write and a plainly recorded
"this cannot be undone, and here is why". Shipping something called undo that does not undo is
worse than either.

### The instance underneath it: a gate whose own comment claimed the property it half-had

`internal/ui/cardkeys.go` read a card's `a`/`d` keys *"only when the composer is empty"*, and its
header said that closed the failure of granting a tool call while somebody writes the word
"analyse". **It closes the second character of a draft onward and leaves the first** — which is the
whole exposure, because the first character of every draft is typed into an empty composer, and `a`
and `d` begin a great deal of what is typed at a room full of agents. `CLAUDE.md` repeated the
claim.

This is not the sampling family the ladder above is about. Every rung there asks whether a *test*
can see a defect; this is a **guard that is genuinely total over the wrong domain** — total over
"is there a draft", where the domain that matters is "could this keystroke become one". Nothing
about the test would have found it. What found it was reading the guard's own sentence and asking
which inputs it actually excludes.

**The tell is a gate justified by a scenario rather than by a domain.** *"…while somebody writes the
word analyse"* names an input, and an input is not a set. The correction is to state the set the
gate is supposed to exclude and check the boundary of it — here, the empty composer is *inside* the
set of drafts rather than outside it, which is visible the moment the set is written down.

### And the confirmation's own key is a decision, not a detail — the accident's recovery may not be it

Named 2026-08-12 by the review of the entry above, on the mechanism that entry describes. The
ruling — *"the only place an undo can exist is before the write"* — survives. **The first
implementation of it did not**, and the way it failed is worth more than the ruling.

A card settle was armed by `a`/`d` and confirmed by the **same rune**, with the arming press
consuming the character. The defence written into the code was:

> no draft produces the same letter twice in a row at position one

That sentence is true and it is about the wrong thing. **It is a claim about the draft the operator
*intended*.** It says nothing about the keystrokes the mechanism's own feedback loss produces — and
the universal human recovery from *"the character I typed did not appear"* is to type it again,
which was the confirm. `a`,`a` wrote the allow. Worse, it needed no confusion at all: **`add` is
three card keys in a row**, so typing `add the tests` at a room with a card up denied it.

> **A guard against an accident may not be completed by the accident's own recovery.** Ask what the
> operator does *next* after the guard fires, not what they were trying to do before it.

The correction is one key: `↵`, which no draft produces at an empty composer, and an arm only exists
while the composer is empty so `submit` has nothing to send there. It costs the same one keystroke
and removes the whole class.

**The second half is the same failure moved to the screen.** The arm's only account of itself was
the card's key line, and `roomPane` draws no card below the room's floor and clips what it does draw
— **key line first, because it is the last row**. At pane height ≤ 8 neither press drew anything, so
the two-press settle degraded to a two-press *accident* exactly where the operator had least
information. **A confirmation the operator cannot see is not a confirmation**, and the rule that
follows is general: *if the surface that carries an affordance is not drawn, the affordance is not
live*. `App.cardFullyDrawn` asks that with the same call `roomPane` makes, rather than by arithmetic
that could drift from it.

**And the third is what the screen said.** `decisions.md` was emphatic that the confirmation *is*
the undo; the card drew `press [d]eny again · any other key cancels`, which is an are-you-sure word
for word and says nothing about the fact the whole ruling rests on. The brief's fourth candidate
shape was *record plainly that it cannot be undone* — and it was recorded in a developer document,
which is not a surface an operator reads. **A ruling that only exists where developers read it has
not been delivered.**

### Failure mode: a guard written for a mechanism outlives the mechanism, vacuously

Found in the same round, on this branch's own tests. When the confirm moved from the arming rune to
`↵`, **three guards written against the same-rune mechanism kept passing and stopped asserting
anything**: each pressed the card key twice and required that nothing was sent, which the new
mechanism satisfies by simply arming twice. They were green, they were about the right property, and
they had no power. Only the mutation battery found them — the mutants they were written to kill
survived.

The tell is that the *mechanism* changed and the test's **keystrokes** did not. A test that drives a
mechanism through its keys has a dependency on those keys that no compiler checks, and a change of
key is exactly the kind of change that leaves it compiling.

**A fourth in the same round is the neighbouring shape**: a check that the card advertises its
confirm searched the *whole rendered frame* for `↵` — which the composer's legend draws
unconditionally, so it was satisfied by a surface it was not about and would have passed with the
card drawing nothing at all. **Scope a rendering assertion to the thing that renders it**, and where
a glyph is shared, assert on the line rather than on the frame.

**Practically: after any change to how a mechanism is driven, re-run the mutation battery rather
than the suite.** A green suite is evidence the tests still compile and still pass; it is not
evidence they still bite.

---

## Finding: macOS never delivers a `⌃⇧`+arrow, so the grid's first keys were unpressable

`⌃⇧→` and `⌃⇧↓` shipped as the grid's placement keys in PR #10 and could not be pressed on the
machine Wake is developed on. They are named by bubbletea, sent by every terminal, and taken by the
window server before a terminal is handed anything: macOS spends all four ctrl+shift+arrows on
window management, and the visible effect of pressing one is the desktop sliding sideways.

The bindings, from `com.apple.symbolichotkeys` — read off the running machine rather than recalled:

| Hotkey | Chord | Action |
|---|---|---|
| 79 / 80 | `⌃←` / `⌃⇧←` | Move left a space |
| 81 / 82 | `⌃→` / `⌃⇧→` | Move right a space |
| 32 / 34 | `⌃↑` / `⌃⇧↑` | Mission Control |
| 33 / 35 | `⌃↓` / `⌃⇧↓` | Application windows |

The shift variant is a second id for the same action, which is why adding `⇧` to a chord macOS owns
does not escape it. 79–82 are `enabled = 1` in the owner's own plist; 32–35 are absent from it and
so at the factory default, which is on.

**Why nothing caught it.** Every guard this tree has was green:

- `keyprobe_test.go` proved bubbletea names `\x1b[1;6C` as `ctrl+shift+right` — true, and about the
  decoder rather than the keyboard;
- `gridscreen_unix_test.go` drove the real binary through a pty with those exact bytes — true, and
  a pty is not a keyboard;
- the legend bijection proved every glyph had a binding and every binding a glyph — true, and it
  cannot see whether the byte is ever sent.

**The gap is one layer above every test in this repository**: a key can be named, decoded, handled
and rendered and still never be pressed, because the window server ate it. `docs/live-testing.md`
exists for exactly this class and the grid's keys were never added to it — one row, and the cost of
its absence was a merged feature that did nothing.

**Rulings.**

1. **A key Wake binds is a single byte or a chord no window manager claims.** `⌃Y` (`0x19`) and
   `⌃B` (`0x02`) replace the arrows. `TestNoKeyIsACtrlArrow` holds the class rather than
   the instance, so re-binding any ctrl+shift+arrow is a build failure with this note's reason in
   its message.
2. **Adding a key means adding its row to `docs/live-testing.md` in the same commit.** That file is
   the only instrument this project has for the layer above the pty.
3. **`⌃⇧`+arrow is not a way around bubbletea's silence on `ctrl+shift+<letter>`.** The library
   names `KeyCtrlShift{Up,Down,Left,Right,Home,End}` and that set reads as an escape hatch from the
   finding above it. On macOS four of the six are spoken for, and `Home`/`End` are `fn`+arrow on
   every Apple keyboard.
4. **The free-key set is now spent.** `⌃O`, `⌃X` and `⌃Y` were the keys neither bubbletea nor
   bubbles claimed; they are detach, next-blocked and open-right. Every key from here shadows
   something in the composer, and the trade has to be named where it is made.

---

## Finding: `⇧` is the only arrow family free at every layer, measured rather than assumed

Moving the keys between panes was asked for as `⌘`+arrow. It cannot work, and the audit that
established this is worth keeping because it also rules out the two obvious second choices.

**What bubbletea v1.3.10 names**, driven through `keyprobe_test.go`'s harness rather than read off
the table:

| CSI param | Modifier | Reported |
|---|---|---|
| 2 | shift | `shift+up/down/right/left` |
| 3 | alt | `alt+…` |
| 5 | ctrl | `ctrl+…` |
| 6 | ctrl+shift | `ctrl+shift+…` |
| **9** | **cmd/super** | **nothing** |
| 10, 13 | cmd+shift, ctrl+cmd | nothing |

The library's arrow table stops at param 8. The CSI modifier encoding is `1 + shift(1) + alt(2) +
ctrl(4) + super(8)`, so every combination involving `⌘` is param 9 or higher and matches no entry.
**And that is the smaller of the two walls**: no macOS terminal transmits `⌘` to a tty at all — it
belongs to the terminal's own menu shortcuts — so the sequence probed above is one nothing sends.

**Why not `⌃`+arrow**, which *is* named and *is* sent: macOS binds `⌃←/→` to "move a space" and
`⌃↑/↓` to Mission Control and Application Windows by default. That is the `⌃⇧`+arrow trap one
modifier shallower, and it would have passed every guard in the tree exactly as `⌃⇧→` did. So
`TestNoGridKeyIsACtrlShiftArrow` became `TestNoKeyIsACtrlArrow` and refuses the whole `KeyCtrl…`
+arrow class — `KeyCtrlShift…` carries the `KeyCtrl` prefix, so one check holds both.

**Why not `⌥`+arrow**: `bubbles`' text area binds `alt+left`/`alt+right` to word-backward and
word-forward (`textarea.go` `DefaultKeyMap`), and Terminal.app's default profile sends `^[b`/`^[f`
for them rather than a CSI. `⌥↑/↓` are free, but half a directional family is not one.

**Which leaves `⇧`+arrow**: named by the library, bound by neither `App.key` nor the text area, and
claimed by no macOS system shortcut. The ctrl-letter audit run beside it found exactly one letter
free of *everything* — `⌃L` — and one key cannot carry a four-way move.

**The one thing still unmeasured** is whether a given terminal sends `CSI 1;2C` at all; Terminal.app
may send a bare arrow. That failure is benign rather than silent-and-wrong — the key acts as a plain
arrow — but it is a `docs/live-testing.md` row, per ruling 2 of the entry above.

**And a test-shaped finding that came out of it.** The pty test for this passed with both bindings
deleted. Two side-by-side panes occupy the same *rows*, so `screen.rowOf` finds a draft at the same
row whichever composer took it; only the column separates them. `colOf` is the fix, and it has a
trap of its own — `s.lines()` is one rune per cell but `strings.Index` returns a **byte** offset,
and a frame drawn in three-byte box characters reported column 451 on a 200-column screen. A
green pty test is not evidence until it has been watched to fail.

---

## Ruling: a room message is echoed into held conversations only, never into ones nobody opened

A message routed from the room is written into each addressed agent's conversation as well as into
the room, carrying the `@name` as typed. The obvious extension - build the conversation for every
target so the copy is there whenever you first open it - is refused, and the reason is the
single-source rule rather than cost.

**Claude's on-disk transcript already holds that turn.** `DecodeTranscriptLine` drops sidechain
lines and anything that is not `assistant` or `user`, which means it keeps the user's own turns;
`history.go` folds them under whatever the pane holds when it opens; and neither pane
de-duplicates. So a conversation first opened *after* a room message would draw that one turn
twice - once as Wake echoed it (`@noah do this`) and once as the agent received it (`do this`) -
with the two spellings making it read as two messages rather than an obvious duplicate.

So `echoToRouted` writes only into conversations `App.dms` already holds. **That is the same rule
`App.observe` uses for the agent's own events**, which is what makes the two halves symmetric
rather than merely both-bounded: a reply to a message you sent from the room does not reach an
unopened pane either. Both halves come from the transcript when it opens. What is lost is
cosmetic and bounded to a single line: the turn that was routed while the pane was closed comes
back unlabelled.

Three consequences worth naming:

1. **`FromRoom` is presentation only**, for the reason `Echoed` is: no frame carries it, nothing on
   the wire could, and a wrong value mislabels a turn rather than hiding one. Nothing may key
   suppression or de-duplication on it.
2. **The fan-out is one map copy.** `withDM` copies `App.dms` per call, so `@all` at thirty agents
   would have been thirty copies of it for one keystroke.
3. **`historyArrived`'s drop gets marginally more likely.** Sending to the room in the window
   between a pane opening and its history landing moves the pane's event count, which is exactly
   what that guard watches for. It already self-corrects - the ask is forgotten, so the next open
   asks again - and this adds a way in rather than a new failure.

---

## Finding: three separate reasons a growing composer does not grow

The box shipped one row high, and the operator hit it the moment a draft needed
two: *"the chat bar does not expand and I can no longer see what I am typing."*
Making it grow was one line. Making it *work* was three findings, each of which
looked like the fix had failed.

**1. Typed runes never reach `repositionView`.** `Composer.Update` intercepts
`tea.KeyRunes` and calls `textarea.InsertString` directly - it must, because a
run of typed characters arrives as one `KeyMsg` whose `String()` is the
characters, so bubbles matched its keymap against them and typing the word
*delete* deleted. But bubbles keeps the cursor on screen in `repositionView`,
which runs only at the end of `Update`. So while somebody types, nothing
repositions: the box scrolled nowhere and the draft stayed pinned to its first
row. Driving one no-op message through `Update` runs it - the type switch
matches nothing and the reposition is unconditional after it.

**2. That reposition is a no-op until something has rendered.**
`viewport.ScrollDown` returns immediately while `len(m.lines) == 0`, and the
lines are set only by `SetContent`, which runs only inside `View`. So a
reposition with no render since the last keystroke silently does nothing - and
it *looked* like it worked, because any test or frame that rendered in between
made it work. This is why the first version passed when a probe printed the box
each iteration and failed when it printed it once at the end. The reposition
renders first for that reason.

**3. It repositions against the height the box had before it grew.** An update
that adds a row scrolls down to keep the cursor inside the *old*, smaller box,
and `repositionView` never scrolls back up when the box grows - it only acts
when the cursor is outside the window. A newline therefore left the draft
showing from its second line permanently. The fix is order: the box is set to
its bound *before* `textarea.Update` runs and fitted back down after, so there
is nothing to scroll until the draft genuinely exceeds the bound.

**And measuring the draft needs a text area of its own.** `textarea.Model` holds
its viewport behind a pointer, so every copy of a `Composer` shares one - and
that viewport is scrolled. Measuring the wrapped height through it returns the
height of the *window*, which reported **1** for a three-line draft; resetting it
to measure would undo the cursor-follow above. A fresh text area shares nothing
and starts unscrolled, because `SetValue` does not run `Update`. Re-wrapping the
draft by hand was refused: bubbles' `wrap` has its own rules for spaces and
double-width runes, and a second copy of them is the parallel implementation
this project forbids - one that drifts silently, because both still produce *a*
number.

**The bound belongs to the pane, not the composer.** `composerRowsIn` is what is
left after `minTranscriptHeight` and the chrome the draft does not own - the
box's edges, the hint line, the room's target line, and in a conversation the
heartbeat and status rows. A composer that capped itself at ten rows would make
the frame taller than the terminal in a pane shorter than that, and a frame one
row too tall scrolls the alt screen away on every draw.

## Ruling: the manager switch is a command, because every remaining ctrl byte is worse than no key

**2026-08-15**, when the room started seating a manager by default and needed a
way to turn one off. The obvious shape is a key — it is what was asked for — and
the honest answer is that this build has no key left to give it.

**`⌃M` is the mnemonic and it is the Enter byte.** Same class as the `⇧↵` the
grid was asked for and never got: not "already bound", but *unpressable*, because
nothing between the keyboard and Wake can tell it from `↵`. `⌃I` is Tab and `⌃J`
is the composer's newline, both for the same reason one door over.

**`⌃S` was the candidate and it is the one to write down.** It is free in
bubbles' textarea keymap and in `App.key`, and its tty argument looked
pre-paid: `IXON` governs XON/XOFF, `⌃Q` already ships on bubbletea's `MakeRaw`
clearing that flag, and `⌃S` is the other half of the same pair. **That pairing
is what kills it rather than what saves it.** `⌃S` is XOFF — *freeze output* —
and anything between the keyboard and Wake that still does flow control (tmux,
ssh, a terminal option) eats it before raw mode is reached. The screen stops
updating, Wake looks hung, and the reflex for a frozen terminal is `⌃Q` — which
in this build **parks the fleet and quits**. A key whose failure mode routes the
operator's reflex into the one key that closes the workspace is not a key to
ship, and `go test` has no tty to catch it: `⌃Q`'s own tmux/ssh exposure has sat
in `docs/live-testing.md` for exactly this reason.

The softer half is worth keeping too, because it is what raised the question:
people press `⌃S` to save in a text box even where it does nothing, and the
composer is a text box. (It is *not* the collision it looks like — on macOS,
save is `⌘S`, which never reaches a TUI as a byte at all.)

**And the default is what makes a command the better answer rather than the
consolation.** With a manager seated on every room open, this is the rarest verb
in the build — most operators will never type it. `legendEntries` is a bijection
with `App.key`, so a chord *must* take a legend slot, and a slot added last is
the entry an 80-column pane loses first: a key advertised to nobody, for an
action nobody needs. `/manager` costs no slot, is discoverable by typing `/`, and
joins the closed Wake-addressed set (`/resume`, `/new`, `/name`, `/task`, `/mcp`)
that every other rare, target-independent verb here already lives in.

`⌃L` remains the one genuinely free chord — no signal, no flow control, and its
only convention ("redraw") is one Bubble Tea owns and Wake never implemented. It
is written down here rather than spent, for whoever needs a key next.

## Ruling: a stop does not inherit park's blocked refusal, because the denial it fears cannot outlive it

**2026-08-15**, adding `/manager-stop` beside the switch. `parkTarget` refuses a
blocked agent and `/manager` inherits that refusal, so the obvious move is for
the ending to inherit it too. It does not, and the reason is that the argument
does not survive the change of verb.

**What park is afraid of is a belief, not a closed pipe.** Parking closes stdin;
an outstanding permission ask that dies that way is recorded as a **denial the
operator never made**; and the whole weight of that — the reason `⌃C` refuses and
names `esc` instead — is that the false "no" *comes back with the session*. It is
in the transcript the wake resumes, and every later turn is built on a decision
nobody made.

**A stop has no wake.** The session ends, the name goes back to the pool, and
nothing resumes that transcript: `Fleet.manager` reads ended as absent, so the
next `/manager` spawns a fresh session rather than addressing the old one. The
false denial has no future to corrupt.

**And inheriting the refusal costs something real.** A blocked manager is exactly
the one an operator is most likely to want gone — it is stopped dead, holding the
one name the daemon reserves, and the room's default addressee. A `/manager-stop`
that refused it would leave the operator with the one session they cannot get rid
of and a command that names `esc` at them, while `esc` withdraws the ask and puts
the manager back to work — the opposite of what they typed. `esc` is still there
for anybody who wants that; it is no longer the only door.

`TestSlashManagerStopEndsABlockedManagerRatherThanRefusing` holds the ruling, and
`managerStopStates` carries the per-state verdict with `rpc.StateBlocked`'s cell
naming this argument, so a later reader cannot fold the two verbs back together
without deleting a sentence that says why they are apart.

---

## 2026-08-15 — Ruling: a streamed answer is a preview, and glamour never sees one

Phase 4. `--include-partial-messages` was in CLAUDE.md's CLI-surface table and in nothing else;
`internal/core/argv.go` now emits it. The decoding was never the hard part. **The cost was, and it
came close to defeating the feature.**

Machine: Apple M5 Max, 18 cores, darwin 25.4.0, `go test -bench -benchtime 10x -count 5`, load
average 4.54–5.05 throughout — written down beside the figures for the reason the Task 16
measurement writes its own down. Every number below is from **one** run, so the arms are comparable
to each other; the absolute figures move by about 15% with the machine's load and the ratios do not.

### What the flag actually costs, before any design

One `content_block_delta` carries one output token, so the flag multiplies a session's frame rate
by its token rate. Both inputs are **derived from `testdata/stream/` rather than assumed**, over the
47 recorded turns of 50 output tokens or more: median **43.5 tokens/s**, p90 76, max 93.9. At the
top of CLAUDE.md's 15–30 range that is **~1,300 extra frames a second** for the fleet, and ~2,800 at
the recorded maximum.

The obvious implementation is to re-render the block that is growing. It is a non-starter, and the
reason is structural rather than a constant factor: `internal/render` renders behind **one
process-global mutex shared by every session in the process**, so glamour time does not parallelise
across agents — it serializes, and the sum is what every other pane's draw waits behind. Streaming
a block through it costs the *integral*, not one render.

| one whole block, at the 79-column conversation pane the idle figures use | preview (shipped) | glamour per token (rejected) | ratio |
|---|---|---|---|
| 64 tokens (≈ the recorded mean block) | 0.246–0.284 ms | 1.85–1.96 ms | 7.3× |
| 256 tokens | 1.02–1.15 ms | 20.9–21.3 ms | 19× |
| 1,024 tokens | 4.51–4.74 ms | 301–305 ms | **65×** |

**Read the shape, not just the ratio.** Four times the tokens costs the preview 4.2× and then 4.2× —
linear, which is the whole claim. It costs the rejected arm 11.2× and then 14.4×. The corpus's
longest recorded turn is 13,499 characters, ~3,375 tokens; extrapolating that curve puts one such
answer at **~3 seconds of serialized glamour** to show what one render shows in well under a
millisecond at the end.

And per fleet-second, through the real `Update` and the real `View`, in the batches `inbox.go` hands
over — thirty agents at the corpus median:

| one second of a 30-agent fleet streaming | ns/op | share of one core |
|---|---|---|
| preview, one conversation open | 7.38–8.33 ms | **0.74–0.83 %** |
| preview, thirty conversations open | 10.26–10.56 ms | 1.0 % |
| glamour per token | 61.6–63.0 ms | 6.2 % |

The second figure is a **floor and not a worst case**: each block is seeded at the recorded *mean*
length and reset every iteration, so it prices a second in the middle of an average answer. A second
in the middle of a long one is the 1,024-token row above, thirty times over.

### The four candidates, and why three of them die to the same sentence

- **Re-render per token.** The table. Dead.
- **Re-render only the last block.** The last block *is* the one growing, so this is the same
  measurement with the word "only" in front of it. Dead.
- **Coalesce on a tick.** `beat.go` is the precedent and it is the wrong precedent: the heartbeat's
  ticker is affordable because its per-tick work is *constant*, and this one's grows with the
  answer. At 4 Hz across thirty agents it is still 120 renders a second of a block that keeps
  getting longer — it lowers the rate and not the growth. It is also a poll where a wait will do,
  which the first non-negotiable forbids, and the deltas themselves are the wait.
- **Plain text until the block completes.** Shipped.

### Why the fourth one is not a compromise

The point that decides it: **Wake already receives the completed block.** Under the flag, partials
arrive *in addition to* the ordinary `assistant` frame, not instead of it. So the transcript needs
nothing from this feature — it is byte-identical to what this build drew before, and the finished
block still goes through glamour exactly once, as it always did. What the preview adds is a *view*
of a block that has not landed yet, and a view that is replaced within the turn has no business
being a record.

That is what licenses every property in `internal/ui/partial.go`:

- **It never enters `DM.events` or the transcript**, so it cannot make `Append` superlinear, cannot
  invalidate the memoized lines, and gives a width change nothing extra to re-wrap. The transcript
  is *rendered lines* precisely because re-rendering is expensive (11.3 ms of an 11.5 ms `Append` at
  4,000 events); a per-token writer into it would have been that defect returning through a new door.
- **It is bounded to `maxPreviewRows`,** and the retained text with it — the tail, because the newest
  tokens are the ones being read. That is what makes the per-token work *flat* instead of growing
  with the answer, and it is the property the linear column above is measuring.
- **It is cleared by the block that supersedes it, or by the turn ending.** The second is not
  belt-and-braces: an interrupted turn produces no completed block at all, so nothing else would
  ever clear it and half a sentence would sit under the transcript until the agent next spoke.
- **It is only accumulated for a pane on screen** (`App.wants`), and this one was found by measuring
  rather than by design — see below.

### The fourth property was a defect the first measurement could not see

The fleet-second arm was written with **one** conversation open, which is the ordinary shape and is
not the worst one. `App.dms` holds every conversation ever *opened* rather than the ones on screen —
`hideDM` keeps the transcript so that closing a pane is reversible — and `withDM` copies the whole
`map[string]DM` on every write, of a value type carrying a transcript, an event log and a composer.
So an operator who had looked at all thirty agents paid **thirty large struct copies per token**:

| one fleet-second, before `App.wants` existed | ns/op | allocated |
|---|---|---|
| one conversation open | 12.7–12.9 ms | 2 MB |
| thirty conversations open | 106–123 ms | **530 MB** |

That is ~11 % of a core and a GC firehose, for panes nobody can see — the per-agent cost multiplied
by thirty that the first non-negotiable is about, arriving through a map rather than through a
ticker. With the gate, thirty open conversations cost **10.3–10.6 ms and 19 MB**, which is what one
costs, because the grid draws the same two panes either way.

**The gate is on the accumulate and never on the clear**, and that asymmetry is what keeps a stale
preview impossible: a conversation drawn while its agent was writing, then closed, then reopened,
would otherwise come back showing a half-sentence from a block that finished long ago.
`TestClosingAPaneMidBlockLeavesNothingToComeBackTo` holds it, and gating the clear as well is a
narrowing mutant it kills by name.

Worth stating as a lesson rather than a fix, because the shape recurs: **the first benchmark measured
the configuration the feature was written in.** One conversation open is what a developer has open
while building this; thirty is what the product claims. The arm that found it differs from the
original in one parameter and that parameter is not the feature's own.

**Two things get no preview, and it is one reason between them: a preview is replaceable only on a
surface that follows one speaker.** The *room* interleaves thirty agents, so a token arriving there
would append a line rather than update one, and the finished block would land under thirty
half-sentences nothing supersedes — the rate settles it from the other side too, ~1,300 lines a
second into the one surface whose whole job is to be the filtered one. And a *subagent's* tokens are
dropped in the airlock on `parent_tool_use_id`, which is the rule `fold` already keeps for a
subagent's tool calls: `subagent-parallel.jsonl` records three streams interleaving line by line, and
attributing them would need the gutter and per-block header `dm_blocks.go` carries, on a surface that
is replaced within the turn. Both still draw the completed blocks, with that attribution.

### What the guards are, and what killed what

`BenchmarkOneBlockStreamed` and `BenchmarkStreamingFleetSecond` are a **pairing**, for the reason
`BenchmarkRoomFold`'s header gives about its own: "the preview costs 4 µs" answers no question. Both
arms do the same work to the same block at the same width and differ only in what is done per token.
Per *block* rather than per token, deliberately — the rejected arm is superlinear in exactly the
dimension a per-token average destroys.

Two details each chosen against a specific way the obvious version is wrong:

- **The rejected arm is reset every iteration and seeded to a fixed length.** Written the obvious
  way — thirty `strings.Builder`s accumulating across `b.Loop()` — it reported 57.8 ms at
  `-benchtime 3x` and 82.9 ms at `10x`. A benchmark whose answer is the duration you gave it is not
  a measurement.
- **The preview arm asserts a preview is on screen** before it stops the clock. A fold that
  discarded every partial is the fastest possible result and reads as the strongest possible pass —
  this project's "a benchmark reporting the absence of the work it exists to price", one file over
  from where it was first named.

Mutation-verified, and by **narrowing** rather than deletion, per the rung above: bounding the tail
only past ten times the drawn size fails `TestThePreviewIsBoundedToItsRowsHoweverLongTheBlockGets`
with *"retains 1560 characters, want at most 300"*; clearing on `KindTurnEnd` but not on
`KindAssistantText` fails `TestTheCompletedBlockReplacesThePreviewRatherThanDoublingIt` with *"the
sentence is on screen 2 times, want 1"*. Dropping partials in `DM.Append` altogether fails four unit
tests **and** the benchmark's own vacuity guard. Each assertion that fired names the harm rather
than the mechanism, which is what the `awaitSettled` entry asks for.

### The half that is not proven, and it is not the cost

The schema has **no recording behind it**. The corpus predates the flag, so `wireStreamEvent` is
transcribed by hand from claude 2.1.233's own zod schema — the standing this package already gives the
`TodoWrite` vocabulary: stronger than documentation, weaker than a frame. `partialEvent` is written
to survive being wrong about the envelope — every shape it does not recognise yields **no event**,
so a moved schema costs the preview and never the transcript. What that does not cover, and what
only a recording can, is the assumption the whole design rests on: that the completed `assistant`
frame still arrives. `docs/live-testing.md` §15 asks for it in those words, and `deferred.md` holds
the rest.

---

## Finding: two escapes are one message, and the feature that ignored it worked only for slow hands

`⎋⎋` was asked for as "clear the query bar like Claude Code". Making the second press mean something
was one branch. Making it *work* was one measurement.

**A terminal hands over whatever bytes are pending in a single read**, and bubbletea reads a lone ESC
followed by another byte as that byte with `Alt` set. So `\x1b\x1b` — which is what two escapes at
the speed a person presses them look like — arrives as **one** `KeyMsg`, `alt+esc`, and not as two.
Probed with `keysFor`, held by `TestTwoEscapesInOneReadCollapseIntoAltEsc`.

The first build handled two separate messages and passed every in-process test in the branch, because
an in-process test hands the model one `tea.KeyMsg` at a time and cannot see a read boundary. It also
passed a pty test that sent `\x1b`, settled, and sent `\x1b` again. It failed under a finger.

`App.key` therefore passes `m.Alt` as a *collapsed double press*, and the two speeds end in the same
place: a collapsed press stops the turn **and** clears the draft, which is what the slow one does
across its two presses. This is the same wall `⌃⇧A`, `⇧↵` and `⌃⇧→` hit from three different sides —
**the library naming a key, a terminal sending it, and the model receiving it as a distinct message
are three claims, and only the first is checkable by reading bubbletea's tables.**

**And a test-shaped finding beside it.** The pty test written for the room's working line passed with
the feature disconnected: it looked for a row carrying the agent's name and a parenthesised age, and
the *conversation pane's own heartbeat* is such a row. The mutation that proved it was itself wrong
twice — a `sed` pattern that matched nothing, run against a cached test binary — and both times the
green run read as evidence. A green pty test is not evidence until the mutation has been watched to
change it, and the mutation has to be watched to *apply*.

---

## Ruling: the room is restored from claude's transcripts, and a turn you typed comes back only if two of them hold it

The room had no history at all: `Room.said` was built from live events and nothing else, so `⌃Q`,
`wake`, `/resume all` gave back a working fleet above an empty group chat.

**Re-derived rather than kept.** A room log of Wake's own would have been exact — arrival order, no
timestamp merge, no duplicate broadcasts, and Wake's own notices surviving — and it was refused
anyway. "Wake owns almost no state" is a non-negotiable, and the four things it does own are each
something nothing else records. What was said in the room is recorded, once per participant, in the
files claude already writes. The cost of that choice is the whole of `roomhistory.go`.

**What it costs, stated so nobody re-derives it as a bug:**

1. **`@noah do this` does not come back.** A user turn on disk is indistinguishable from a private
   one — `sendRoom` strips the leading `@name` before writing, and `FromRoom` is presentation-only and
   on no wire — so the same bytes are a room message aimed at one agent and a turn typed into that
   agent's pane. The only sound discriminator is multiplicity: **the same text in two or more
   transcripts within `broadcastWindow` is a broadcast**, comes back once, and is attributed to
   nobody. One transcript is dropped. It errs toward silence, because the other direction puts a
   private turn on a shared surface and this project has a shipped ruling that a DM is private.
2. **Wake's own room-only lines are gone** — `!cmd` output, notices, the `/mcp` panel. They were never
   in anybody's transcript.
3. **A restored broadcast carries the `@name` as claude received it**, which is with the mention
   already stripped.

**The cutoff is `At`, not a line count.** `historyArrived`'s guard drops a batch if the conversation
moved since the ask, which is right for one pane. For the room — thirty sessions, all streaming —
that would drop nearly every round. The room drops *per event* instead: anything stamped at or after
the ask may also have arrived on the socket, because claude writes a turn to its transcript **and**
streams it. That makes `core.Event.At`'s zero value load-bearing rather than a gap: a live event never
has a time, so `Room.Before` can merge history under it without ever re-ordering a line somebody is
reading.

**And it asks at two moments, not on every report.** The seed and `wakeArrived`. Asking whenever an
unasked-for session appears would have caught **forks**, whose transcript is their parent's — an hour
of the same prose drawn twice under two names.

---

## Finding: three ways the room's restore was wrong, and all three tests were green

An adversarial second-model review of the branch that built it. Every one of these was shipped-shaped
— compiling, tested, and described correctly in `CLAUDE.md` and in the design doc. The tests were the
problem in two of the three.

**1. The broadcast rule could never fire.** `sendRoomHistory` answers **one transcript per frame**,
because it reads one file per ask. `roomHistoryArrived` folded each reply as it landed, so
`collapseBroadcasts` ran over one session's events and `senders` could never hold two — which drops
*every* restored turn the operator typed, including the `@all` the rule exists to bring back. The unit
test passed because it handed `roomHistoryLines` three sessions' events in one slice, **a shape
nothing on the wire produces**. This is the failure mode this file already names twice: a guard whose
subject can walk out from under it. The fix is `Room.raw` — every restored line, unfiltered — with the
collapse re-derived in `Room.Before` over all of it, which also makes the rule independent of the
order replies arrive in. The tests now drive one batch per call, and
`roomdaemonshape_test.go` exists to hold *the daemon's shape* rather than a convenient one.

**2. The `At` cutoff could not prevent a double draw on its own.** It is stamped **before** the frame
is written, and `awaitFleet` hands `converseRoom` the frames it read past on the way in — which are
replayed into the model *after* it exists. So a turn stamped before the cutoff arrives on the socket
after it, and both copies are drawn. The cutoff is kept, because it is free and right for the ordinary
case, and `askedAt.lines` is added beside it: how many room lines that session had when the ask went
out, checked again when the reply lands. **Per session, which is the only thing that makes it usable
here** — `history.go`'s guard watches one pane, and a room over thirty streaming agents would drop
every round on the first agent to speak.

**3. A fork in the seed was asked about, against this branch's own written ruling.** `liveSessions`
had no `ParentID` check. The ruling was enforced only for a *later* status push — and only by
accident, because `askRoomHistory` is never reached from an ordinary report — so a fork already in the
seed, or one resumed from parked, went straight through and drew its parent's whole conversation a
second time under a new name. **A rule written in three documents and enforced by nothing is worth
what a comment is worth.** `isFork` is now checked at both call sites and both are tested.

The general lesson is the one about *shape*: all three bugs live in the gap between the data the tests
construct and the data the wire delivers. A unit test that builds its own input is testing the
function's algebra, and the algebra was right in all three cases.

---

## Ruling: the restored room hid the question and showed the answer, and the turn is the unit

The broadcast rule dropped a user turn only one transcript held — because it might be private — and
then restored **the agent's reply to it**, which is the same conversation in the agent's words, at
transcript scale. Found by an adversarial second-model review.

**The live path had already ruled on the same unknowable, in the other direction.** `Fleet.sending`'s
own header says it: *"a reattached window has no memory of a turn it did not send, and treats what
arrives as public. That is the safe direction of the two — a reply in the room is noise, a reply
nowhere is lost."* So "restore the prose and let provenance go" was defensible, and it was **put to
the owner rather than settled here**, because it is a trade on their product and not a bug with one
right answer.

**The owner chose to tighten it**, and the argument for overriding the older ruling is *scale*: a
reattach exposes the turns in flight during one window, and a restore exposes hours. So an agent's
prose is restored only while the user turn it belongs to is one two transcripts prove was a
broadcast, and the next user turn in that transcript closes it.

**What that costs, stated because it is most of the feature.** Prose with no initiator inside the
window is dropped, and a 400-event tail usually opens mid-conversation — so a restored room is the
`@all` turns and their answers, and nothing else. A room restored from a fleet that was driven mostly
from conversation panes comes back nearly empty, which is the state this whole feature exists to fix,
arriving through the rule that protects it. `deferred.md` holds the entry.

**And it made the cap load-bearing in a way it had not been.** `Room.raw` is trimmed from the front,
and the oldest line of a turn is the broadcast that opens it — so one bound doing both jobs meant a
session with more prose than the cap lost its `@all` and restored nothing at all. Two bounds now:
`roomRawEvents` is a memory backstop, and `roomHistoryEvents` is applied to the *result* of the rule.
A bound that decides correctness rather than size is a bound in the wrong place, and this one was
found by writing the test for a long turn rather than by reasoning about it.

---

## Finding: two more in the restored room, and the second was invisible to the adversarial passes

**A repeated sender made an earlier private turn public.** A run of one text was accepted as a
broadcast on *distinct senders alone*, so typing `status` into a conversation and then broadcasting
`status` seconds later put both copies plus the second agent's in one run, counted two senders, and
promoted the **earliest** copy — the private one — to the line the room draws. That opened its turn,
and the reply to the private message was restored. One broadcast writes once per transcript, so a
session appearing twice in a run means two separate sends and nothing says which was public:
`broadcastIndex` refuses the whole run now. What it costs is a real broadcast sent twice inside five
seconds, which is the safe direction.

**A park and a wake inside one window drew the pre-park room twice.** `askedAt.lines` asks "did this
session speak *between* the ask and the reply", and a **stable non-zero baseline passes it cleanly**.
So a session spawned into this window — never asked about, correctly — that talked in the room, was
parked with ⌃C and resumed got its first ask on the wake, and its transcript covers the turns already
on screen. Nothing de-duplicates a restored line against a live one. `askRoomHistory` refuses a
session the room already holds lines for, the way it refuses a fork.

**Why nothing caught it.** Every wake test in the branch started from a session parked *before the
room opened*, where the count is zero — which is the one sub-case the guard already handled. Two
adversarial passes read the same code and missed it, and a code review reading for *state across a
whole session lifetime* rather than for algebra found it. That is the second time in this branch the
gap was in what the fixtures were shaped like rather than in the logic, and it is worth stating as a
habit: **a guard that compares a value at two moments is only as good as the range of starting values
the tests give it.**

---

## Finding: Claude Code's own keymap — and the eight chords it shares with Wake

Wake's operators arrive with Claude Code's reflexes, so *which chords mean two things* is a fact
about Claude Code and belongs beside the palette rather than in somebody's memory. The keymap
fixture is maintained by hand the way the palette is:
the bindings sit in the bundle's string pool as an action id (`app:toggleTranscript`) next to a key
name (`ctrl+o`), sometimes with the scope's label (`Global`, `Chat`) between them.

**The pool is strings, not structure, so adjacency is evidence and not proof.** Two rules make it
safe. A key must start within **64 bytes** of the end of the action id — the pool packs these at
about 32-byte spacing, so that is "the next record, or the one after it", which is what lets a scope
label sit between them. At 48 the labelled pairs are lost; at 96 the first false pair appears (an
action beside an unrelated bare `down`). And an action whose sites disagree about its key is written
to `_conflicts` rather than picked between. A global scan with no gap rule pairs `about:blank` with
`right` and `urn:uuid` with `tab`; the gap rule leaves **22 actions, each with exactly one key**, and
the same 22 come out of 2.1.232 and 2.1.233 identically.

The collisions with Wake's own legend, derived by `internal/ui/keymap_test.go` rather than
counted here — a number nothing asserts drifts, and the maps in that file are the record:

| Chord | Claude Code | Wake |
|---|---|---|
| `⌃O` | `app:toggleTranscript` | detach |
| `⌃T` | `app:toggleTodos`, `theme:toggleSyntaxHighlighting` | mention mode |
| `⇧⇥` | `chat:cycleMode`, `confirm:cycleMode` | permission mode |
| `⌃G` | `chat:externalEditor` | workspaces sidebar |
| `⌃R` | `history:search` | activity sidebar |
| `⌃B` | `task:background` | open below |
| `⌃E` | `confirm:toggleExplanation`, `theme:editCustom` | expand tool results |

`⌃E`'s third Claude Code action, `transcript:toggleShowAll`, is **not** in that table: it is an
*agreement*, filed with `⇧⇥` in `agrees` rather than excused. Both sides reveal what the pane folded
away. It was reached by accident — `⌃E` was picked because `⌃O` was spent on detach — and an
alignment nobody planned is still one an operator's habit rides on, so it is asserted, and a
rebinding on either side fails saying the alignment broke.

**`⌃O` is the only one worth a mechanism**, and the discriminator is not how confusable the chord is
— it is whether the result is on screen to be undone. Every other wrong press draws something, and
the same key puts it back. `⌃O` closes the window with the fleet still running, and the operator's
next question is whether anything was lost — a question that got harder when fleets landed, because
the way back is `wake --fleet <name>` and the window that closed was the one showing which fleet this
was. The line the client prints on the way out (`detachHint`) names neither `wake` nor `--fleet`.

## Ruling: the detach arm is confirmed by a *different* key, and is drawn for as long as it lives

**2026-08-16**, rebuilding the arm above after two reviewers killed the first version independently.
It shipped as *press `⌃O` twice*, with a one-shot `notice.Report` as its only sign. Both halves were
wrong, and neither fix implies the other.

**The confirm may not be the arming key, because a repeat is indistinguishable from intent.** A
`tea.KeyMsg` carries no key release, no timestamp and no repeat flag, so terminal auto-repeat is
literally the same message twice — and so is the ordinary human reply to a key that appeared to do
nothing, which is to press it again. That reply *is* the Claude Code reflex the arm exists to catch:
`⌃O` expands a truncated tool result there, and somebody who presses it and sees nothing expand
presses it again. **Measured rather than reasoned**: `\x0f\x0f` in one read arrives as two plain
`ctrl+o` messages (`keyprobe_test.go`), where `\x1b\x1b` collapses into a single `alt+esc`
(`escprobe_test.go`) — the ESC-prefix rule that makes `⎋⎋` detectable has no analogue here, so
nothing downstream can tell a repeat from a decision. The confirm is `↵`, which is this tree's
confirm already (a card's `a`/`d` settle on it), which no amount of `⌃O` produces, and which at an
empty composer was doing nothing more than opening the picked conversation. A **second `⌃O` cancels**
rather than re-arming, so a run of auto-repeat of any length lands somewhere harmless: odd leaves an
arm that confirms nothing, even leaves the model exactly where it started.

**And the arm must be drawn, because it outlives everything `App.disarmed` can see.** That function
is called from key and mouse paths only. A `streamMsg`, a `heartbeatMsg`, a `tea.WindowSizeMsg`, a
`geometrySettledMsg`, a `reattachedMsg` and an `errMsg` all leave it standing, so an arm can survive
arbitrarily long — while `internal/notice` is a *single most-recent-message slot* that an ending, a
dropped-frame warning, a permission-mode correction or the hang-up/reattach pair takes within
seconds. One-tell-and-unbounded-life is a detach nobody saw coming.

**Broadening the disarm to those messages was considered and is worse than the bug.** At thirty
agents a frame lands between the two presses constantly, so a stream-cancelled arm would make
detaching fail at random — and worse, would make `↵` mean *send* on the press the operator aimed at
*detach*, decided by socket timing. The card arm survives non-input messages for exactly this reason
and is safe because it is pinned on screen. So this now does the same: `legend.go` swaps `↵ send` to
`↵ detach` and `⌃O detach` to `⌃O cancel`, in **every** pane, for as long as the arm is live. Both
labels, because either alone leaves the other advertising a meaning that is not live; `↵` is entry
one, so it is the entry that survives every truncation width. No new glyph, for `escape.go`'s reason
— the legend is a bijection with `App.key`.

**A drawn *question* card still wins `↵`, and that is the safe way round.** Only a question claims it
unarmed, and only to move a cursor — `chooseCursored` goes through `pickOption`, which writes no
frame — so the card winning costs a pick that can be re-picked and an arm the legend immediately
shows as gone. The other way round costs a detach somebody aimed at an answer. A permission card
hands `↵` back, so the confirm still fires under one.

The mechanism still reuses `App.disarmed` — the card's settle already had to find the four disjoint
paths a key, a click or a digit takes into the model, and the mouse is the one a second mechanism
would miss.

**Nothing of Wake's moved for any of this.** An earlier proposal migrated Wake's keys to `⌥`+letter
and was rejected by the owner; the measurement then killed it independently, and it is PR #39's
`altprobe_test.go` rather than this branch's — `⌥P` is named from `\x1bp` and produces **no message
at all** under the Kitty CSI-u encoding, so a build on `⌥`+letter would stop working the day a
terminal turned a keyboard protocol on. `⌥`+arrow survives both encodings a terminal really sends
(`\x1b[1;3A` and `\x1b\x1b[A`), which is what carries the prompt history; those two are rows in
`keyprobe_test.go`'s legend table, because the legend now names the key. The third encoding,
modifier 9 (`\x1b[1;9A`), is how a terminal spells *meta* rather than alt and bubbletea names
nothing for it — a guard of its own, and a line in `docs/live-testing.md`, because which of the
three your terminal sends is a fact about your terminal.

**Prompt history is derived, not stored.** A conversation already holds its own user turns and an
unopened one is filled from claude's transcript by `internal/daemon/history.go`, so the walk works
on a reattach and on a session this window has never opened, with no prompt file of Wake's to go
stale. Excluded: a user frame the airlock resolved to a **notice** (Claude's abort marker is its own
English about Wake's interrupt), one carrying a **subagent** (an agent's prompt, not a human's), and
an **echoed** one — content the transcript replayed or the tooling generated. That third condition
was missing and is a 2026-08-16 fix: a bang line's output, an `/mcp` panel and a compaction summary
are all echoed `KindUserText` with no notice and no subagent, so `⌥↑` pulled generated text into the
draft where `↵` sends it back to the model as something the operator said. Reading `Echoed` here is
inside its own ruling (`core.Event.Echoed`), which bans it for *suppression and de-duplication* and
allows it where being wrong is cosmetic: a wrongly-echoed turn costs a recall somebody can retype,
and nothing here decides whether a turn is drawn.
The **room's** history is the room's own echo — the room is not a conversation, has no transcript,
and its agents' prompts were typed at other composers; what a history key means at a box is "what I
typed here".

**And the trap that bit `⌥↵` bit this too.** `App.key` switches on `m.Type` alone and bubbletea
reports `⌥↑` as `KeyUp` with `Alt` set, so without an explicit arm the roster swallows it. An
in-process test passes whatever the arm does, because it hands the model a `KeyMsg` somebody
constructed. `TestAltArrowsWalkThePromptHistoryFromRealKeyBytes` drives the bytes through the pty
harness, and it fails with the arm removed: the draft stays empty and the roster cursor moves.
## Measurement: an assistant frame's `output_tokens` is not the turn's, and is not a live counter

Claude Code's breathing bar shows the tokens of the query in flight, climbing. The obvious way to
reproduce it without new flags is to sum `message.usage.output_tokens` across the assistant frames of
the current turn — **316 of the 323 assistant frames in the corpus carry a non-zero one**, so the
field is there and looks like the answer.

**It is not.** Summed per turn and held against the `result` frame that ends the same turn:

| fixture | sum over assistant frames | `result.usage.output_tokens` |
|---|---|---|
| `interrupt-double` | 6 | **3501** |
| `interrupt-mid-tool` | 4 | **5105** |
| `interrupt-cancel-queued-empty` | 12 | **3585** |
| `interrupt-queued-survives` | 6 | **1531** |
| `interrupt-pending-basic` | 8 | **145** |
| `fork-of-fork` | 1 | **20** |

Two to three orders of magnitude out. Whatever that per-frame figure is, it is not what the message
produced, and a counter built on it would show **6 where the truth is 3501** — a wrong number rather
than a coarse one, which is the distinction `⌃Q`'s label and the roster's dropped count are both
about.

**So the only trustworthy count is on `result`, and `result` arrives when the turn ends.** The tokens
of a turn in flight do not exist while it is in flight. That is why the working line and the roster
row both show a *session total*: during turn N they report turns 1…N−1, and they step up as N ends.

**What would change it** is `--include-partial-messages` and its `stream_event` frames — present in
the 2.1.226 binary, in **zero** of the 1004 recorded lines, and listed in
`2026-08-08-stream-json-findings.md` under *"Flags and modes never exercised"*. Nobody has seen the
data, so whether it even carries a usable running count is unverified. The cost argument against it
is the first non-negotiable, and there is a shape that might answer it: the **daemon** absorbs the
chunk frames and keeps a per-agent count, and the UI reads it on the `shimmerStep` tick `beat.go`
already runs while anything is working — which is zero extra frames on the render path. The
unmeasured half is the daemon decoding N small frames per agent per turn. **Do not design around
that until it is recorded**, which is what this file has said about every unexercised flag.

---

## Ruling: the turn's tokens come off `message_delta`, and the two counts are never added

Claude Code's bar shows what the *query* has produced. Wake showed a session total on both the
working line and the roster row, which during turn N is turns 1…N−1 — a different number wearing the
same glyph.

**Two dead ends were measured before the live one was found**, and both are recorded above so nobody
walks them again: summing an assistant frame's `usage.output_tokens` is out by two to three orders of
magnitude, and `system/thinking_tokens` counts one thinking block, resets per block, and is 10% of
the truth on a text-heavy turn.

**`message_delta` is the answer, and the arithmetic on it is not addition.** The streaming docs say a
message emits **"one or more"** `message_delta` events and that their usage counts are
**cumulative** — so within a message the newest figure *replaces*, and the sum is over **messages**,
delimited by `message_start`. The first build added the deltas up and reported 250 for a message that
produced 150; the pty test, driven by a fake emitting a realistic cumulative sequence, read `↓ 900`
where the truth was `↓ 550`. Both are held by tests now, and `message_start` is read for its
*boundary* rather than for its own usage — that usage is `output_tokens: 1`, for a message that has
written nothing, and taking it would add a token per message to every turn. It needs `--include-partial-messages`, which this build has passed since PR #37 — the flag
that made the previews possible made this possible too, and the airlock had declined this event type
along with five others whose *content* arrives complete behind them. It is now read for its `usage`
rather than for its content, which is why the count is the sixth exception rather than a change of
mind about the other five.

**The two figures are never added, and that is the trap this ruling exists to name.**
`SessionFacts.OutputTokens` is what a *completed* turn produced, off the result frame, and
`TurnOutputTokens` is what one message of the turn in flight produced. Both are increments and they
describe the **same tokens** at different moments: the result restates the total of the deltas that
preceded it. A consumer folding both into one figure counts every token twice, which is why they are
two fields, two names on the wire, and two fields on `Agent` — and why `fold` clears `TurnTokens` at
the turn end that adds the result to `Tokens`.

**No fallback when the flag is absent.** A build receiving no `message_delta` draws nothing rather
than reverting to the session total. They answer different questions, and a number whose meaning
depends on a flag the reader cannot see is worse than no number — the same argument that keeps the
restored room silent about a turn it cannot prove was public.

**Still no recording.** `message_delta` joins `stream_event`, `content_block_delta` and `text_delta`
as an inbound word with no fixture behind it, and the pty test drives a fake that emits the shape
this tree *believes* is right. That covers the plumbing and not the schema. `docs/live-testing.md`
already asks for one turn recorded with the flag; that recording now settles four words instead of
three, and its own check is whether the figure the row draws reaches the result frame's total by the
time the turn ends.

---

## Ruling: bare `wake` lands on the room, and first run spawns an agent it does not open

**Every bare `wake` was landing on a conversation, and it took running the shipped binary to see
it.** `openRoom` branched three ways and two of them opened the room; the third — nothing at all —
went through `attach`, which spawns *and* opens that agent's DM beside the room. That looked like
the rare branch and is in fact the common one: with no `$WAKE_SOCKET` and no `--fleet`, a bare
`wake` makes a **new** fleet (`makesNewFleet`), and a new fleet's socket has nothing on it. So the
first-run path is not "the case a new user hits exactly once" — it is what the front door does
**every time somebody types the front door's name**.

What that cost is the product. §8's own arrangement puts the room beside a conversation, and above
`dmTakeoverColumns` the room got the narrower half of a split at 200 columns; below it — which is
every ordinary terminal — `Layout.window` slides the drawn range onto the focused pane and the room
was **not drawn at all**. The group chat is the product; the panes are substrate, and the front door
was opening on the substrate.

**The fix is a bool rather than a flow.** `openRoom` is now one statement: `conversationOnly(socket,
!reopensRoom(fleetToReopen(socket)), out)`. All three answers open the same room with the same
dialer; the branch decides only whether a session is asked for on the way. That is what makes
`TestTheRoomBareWakeOpensIsTheOneWithTheRoomsWayBack` — which exists because the wrong model
*compiles and very nearly works*, differing only in a `redial` that refuses every reattach — a
statement about bare `wake` instead of about one of its branches.

**The agent is still spawned**, because a new user's first command has to produce one; it arrives as
a roster row, which is what that session is for every other client of the same daemon anyway. `↵` or
a click on the row opens it, and `wake new` is the verb that still means *an agent, and put me in
it*. `seedRoom` reuses `requestSpawn` and `awaitSpawn` rather than spelling a second spawn frame, so
first run cannot drift from `wake new` in the id the reaper proves a process group by or the
directory claude persists the transcript to — and it needs no `FrameStatus` afterwards, because a
spawn is confirmed with `s.fleet()` and that reply *is* the room's seed.

**The narrow terminal was the one place this could have stranded somebody, and it does not.** Below
`sidebarsHideColumns` neither sidebar is drawn, so the roster row the agent arrives on is not on
screen — and `⇥` is no help, because `App.chats` is the room plus the conversations **already
opened**, and first run has opened none. What works is `↑↓` then `↵`: `pickedAgent` reads
`layout.ShowRoster`, the operator's *toggle*, which is still true, rather than the width that
decided not to draw it. Measured at 80 columns rather than reasoned about, and held by
`TestFirstRunsAgentIsReachableWhereTheRosterIsNotDrawn`. That `⇥` cannot reach an agent nobody has
opened is pre-existing and unchanged; it is visible from one more place now.

**What this cost the suite is worth writing down.** All 70 first-`startWake` call sites in the pty
suite reached a conversation by being the first `wake` on a scratch socket — 34 of them failed the
moment the front door stopped opening one, and the rest would have kept passing while asserting
something weaker (a `⌃W` back to the room is a no-op when you are already in it). They call
`startWakeInAConversation`, which is `wake new`: byte-for-byte the spawn first run used to do, so
every one of those tests kept the state it was written against. The three that are genuinely about
the front door — the second window in the two reopen tests, and the new
`openroomscreen_unix_test.go` — still call `startWake` bare. **A screen test is what caught this and
what holds it**: nothing in-process can see which pane has the terminal, which is `HANDOFF-NEXT.md`
§2's whole argument arriving a second time.

## 2026-08-21 — the token stream is recorded; two archival "no recording" claims above expire

Two entries in this file (the `wireStreamEvent` transcription ruling and `message_delta` joining
the excuse list) say the streaming schema has no recording behind it. As of
`test/partial-messages-fixture` that is history rather than status: `testdata/stream/partial-turn.jsonl`
is one streamed turn against 2.1.238, the five `notInTheCorpus` excuses are deleted, and every
inference those entries defended held against the bytes — the archival entries stand as the record
of why the transcription was trusted in the interim. Findings:
`docs/superpowers/notes/2026-08-21-partial-messages-findings.md`.

---

## Two ways an agent session has damaged this machine, and the idiom that avoids each

Both were found on 2026-08-23 by looking at `ps` rather than by anything failing loudly, and both
are *harness* failures rather than Wake bugs — a Wake user cannot hit either. They are here because
`decisions.md` is where failure modes worth recognising on sight go, and the next agent to reach for
either idiom will reach for the broken version.

### `LOADPIDS=$(jobs -p)` collects nothing in `zsh -c`, so the cleanup is a no-op

Several sessions reproduced a load-sensitive flake with the same shape:

```sh
for i in $(seq 1 18); do (while :; do :; done) & done
LOADPIDS=$(jobs -p)          # <- empty in a non-interactive shell
go test ...
kill $LOADPIDS 2>/dev/null   # <- kills nothing, silently
```

**Found: 92 of those loops still running, the oldest 18 hours old, from four different worktrees —
load average 122 on 18 cores.** Every "isolated" timing measurement anybody took during that window
was really taken at ~6.8× oversubscription, including the ones in this file's own flake entries.
`kill` with an empty argument list exits 0, and `2>/dev/null` hides the usage error, so nothing
anywhere said the cleanup had failed.

**The idiom that works** — a marker only these processes carry, then a kill by pattern, then a
*verified* count:

```sh
for i in $(seq 1 $(sysctl -n hw.ncpu)); do sh -c 'while :; do :; done # loadmarker' & done
go test ...
pkill -f loadmarker; sleep 1; echo "left: $(pgrep -f loadmarker | wc -l)"   # must print 0
```

The verification is the part that matters. A cleanup nobody checked is how 92 of these accumulated.

### `git stash` is per **repository**, not per worktree

A rebase in one worktree that reaches for `git stash` / `git stash pop` will pop **whatever is on
top of the stack**, which in a tree with a dozen live worktrees is routinely another session's
work-in-progress. Observed: a `stash pop` in a docs worktree tried to apply
`On feat/room-history-and-double-esc: wip-under-review-temp-stash` and conflicted in four
`internal/ui` files.

Nothing was lost — a conflicted `pop` **keeps** the entry — but a clean apply would have silently
merged somebody else's half-finished work into an unrelated branch, which is
[[wake-shared-checkout]]'s incident one level down.

**Rebase a docs commit without touching the stash at all:** commit first, then rebase. There is
nothing to stash if there is nothing uncommitted, and a docs-only change is one commit by
definition. If a stash is genuinely needed, `git stash list` first and read whose it is.

## Ruling: ⎋ on a question is a deny, not an interrupt

**The bug, watched:** an agent asks an `AskUserQuestion`, its card is drawn in the DM, the operator
presses ⎋ to get out of it, and the card never goes away. Reported off a build where the room still
drew that card (it now draws none — `Cards.For` only), but the root cause is the same on `main`.

**Why ⎋ did nothing.** ⎋ wrote a `FrameInterrupt`, and the daemon's interrupt handler
(`apply.go`) deliberately does **not** clear `a.pending` — it leans on the CLI withdrawing the ask.
For a *permission* the CLI does exactly that: `interrupt-permission-findings.md` records the
`control_cancel_request` arriving the moment ⎋ lands, so the card clears. For an `AskUserQuestion`
it is **unrecorded** whether the interrupt withdraws it at all (`question-findings.md` §9), and the
binary carries `tengu_auq_park_preserved_at_shutdown` — *"cancel + deny skipped; park stays
answerable"* — which reads as *the question is preserved*. So `a.pending` never cleared, every fleet
report kept naming the ask, and `Cards.Reconcile` re-pinned the card forever.

**The fix:** ⎋ on a `ShapeQuestion` card sends a **deny** (settles it), the same frame `[d]` writes,
rather than an interrupt. A deny *does* clear `a.pending` — `DenyTool` writes to stdin, and on that
succeeding `noteAnswered` clears it — so the report drops the ask and the card comes down for good.
`internal/ui/send.go`'s `interrupt`. Scoped to the focused pane and to a card whose shape is *known*
to be a question: a card rebuilt from a report on reattach carries no `Ask`, reads as a permission,
and keeps the interrupt (right for the permission it usually is; `[d]` remains the way to clear the
rarer report-only question). The card's ⎋ label becomes `dismiss` on a question
(`cardDismissHint`), because "the legend never lies" reaches the card too and ⎋ no longer interrupts
there.

**What is still unrecorded, and why we shipped anyway.** The *remedy* leans on unrecorded behaviour
as much as the premise did: `question-findings.md` §9 also has *"denying a question … whether the
agent re-asks, is unrecorded."* Two residual risks — (1) if a preserved question also skips an
explicit deny, Wake clears `pending` (reads the agent unblocked) while claude may still be blocked;
(2) a denied question that makes the model re-ask turns ⎋ into a new card rather than an exit. Both
already ride on `[d]`; ⎋ adds **no new** unrecorded dependency, only a second key onto the same one,
and when a question card is up there is no turn to interrupt anyway — answering or denying is the
only real way out. The owner chose to ship on that basis rather than block on a recording.

**The follow-up that would retire the risk** is the spike §9 names as "the obvious next" — see
`deferred.md`: record (a) interrupting a session with an `AskUserQuestion` outstanding, to confirm
it stays stuck (the bug), and (b) denying an `AskUserQuestion`, to confirm the deny unblocks claude
without a re-ask. Until then this is a UI-correctness fix built on a `[binary]` reading, and it is
labelled as one here and in the code.

---

## Ruling: the transcript reader became tree-aware for conversation rewind, and the trigger stays inside `esc`'s existing job

**2026-08-25**, landing `feat/conversation-rewind`. Claude Code's `rewind_conversation` does not
delete: it appends a `last-prompt{rewound,leafUuid}` marker and repoints the active leaf, leaving the
rewound turns physically in the transcript as a dead branch. Wake's reader
(`internal/daemon/history.go`) was linear — file order, top to bottom — which is exactly wrong the
moment a rewind has happened: reopening a pane, or restoring the room, replayed the dead branch and
resurrected exactly what the operator rewound away.

**Two alternatives were considered and rejected before the tree walk.** A *linear marker-replay* —
apply each `last-prompt{rewound}` as a truncation while streaming the file forward — is a fragile
subset of the same idea: it breaks at the tail-window edge (a marker outside the kept ring prunes
nothing) and cannot express a **stacked** rewind, where which node to prune depends on which of several
markers is newest. A *daemon rewind-ledger* — Wake records which rewinds it performed and prunes its
own state accordingly — loses on the project's own non-negotiable, "Wake owns almost no state," and it
is blind to a rewind performed in a native `claude` session Wake later imports: there is no ledger
entry for a rewind Wake never sent.

**So the transcript on disk became the single source of truth, read by walking it rather than by
remembering what happened to it.** `core.ActiveBranch` (`internal/core/activebranch.go`) takes a tree
of `{uuid, parentUuid, rewound, leafUuid}` nodes and returns the live uuid set: start at the latest
marker's leaf (or the last node written, with no rewind), and at every fork take the child written
*after* that marker — "newest branch wins." It is pure — nodes in, a uuid set out — which is what lets
it be table-tested against hand-authored trees (no rewind, one rewind, one rewind plus continuation,
stacked rewinds) with no transcript file and no daemon in the loop.

**One function serves the DM, the room and the picker, deliberately.** `internal/daemon/history.go`'s
`History` is the only reconstruction a DM or a room ever sees — `sendHistory`/`sendRoomHistory` both
go through its `answerHistory` — and `internal/daemon/rewindtargets.go`'s `RewindTargets` reuses the
identical walk to answer the picker's own prompts. A second implementation of "which turns are live"
was the exact risk the ledger alternative above would have introduced under a different name: two
readers is two chances to disagree about the same fact.

**On a successful receipt, the pane re-reads itself rather than patching its own transcript in place,
because patching is the linear-replay mistake arriving through a different door.** `noteRewind` throws
away the pane's live events and re-asks `FrameHistory` — the identical path a reopen or a reattach
already takes. That is what makes "the pane after a rewind" and "the pane after closing and reopening
it" the same code path rather than two that have to be kept in agreement by hand, and it is why a
receipt arriving late is cosmetic rather than a lost turn: presentation state, not the transcript.

**The `esc esc` gate is idle + empty, scoped to the focused pane, because the invariant it must not
break predates the feature.** Wake's rule since `⎋⎋` shipped is that mashing `esc` at a runaway agent
always interrupts; a rewind picker is a second meaning for a key that already carries one, so it may
only claim the press when neither existing meaning has anything to do — a non-empty composer still
clears its draft, and a running agent still eats the press as an interrupt. `App.rewindArmable`
re-derives both conditions on every press rather than caching them, for the reason `rewindKey` also
checks `a.rewind.Session == a.focus` rather than trusting an open picker: a status push landing between
two presses, or the operator tabbing to a different conversation with a picker still open behind them,
must not let a stale gate answer a question the current moment would answer differently.

**The manager is refused both `FrameRewind` and `FrameRewindTargets`, for a sharper reason than
`FrameMode`'s.** It fails the ordinary manager test — nothing on that surface can address a message
uuid, since `core.Event` carries none onto the MCP path — and it fails a second, newer one: a rewound
turn does not stay in view on the next look, so an agent that could trigger a rewind would have a way
to make its own prior turns disappear from the operator's read with no receipt on any surface
`list_agents` or `roll_up` exposes. `cmd/wake/mcpguard_test.go`'s `managerVerbs` carries both reasons
against the actual `FrameRewind`/`FrameRewindTargets` dispatch, not asserted in the abstract.

---

## Ruling: §2c narrows to "not panes you *operate*", 2026-08-27 — the tiled board

`board.go`'s header and the 2026-08-12 phase-4 scope call (§2c) said the board is "an overview, not
panes." The tiled-board idea (`deferred.md`, 2026-08-26) needed a ruling before it could ship, because
a tile showing live output is a small transcript. **The owner's ruling:** what §2c actually refuses is
panes you operate inside — transcripts you scroll and stdin you type — not the visual shape of a
cell, so the wording narrows from "an overview, not panes" to "an overview, not panes you
*operate*". The tiled board is view-only under four guardrails: view-only (no key reaches an agent's
stdin from a tile), bounded live tail with no scrollback, a fixed grid with no per-tile resize or pane
tree, and act-from-not-in (`↵`/click jump into the real DM to work). See
`docs/superpowers/specs/2026-08-27-tiled-board-design.md` for the full argument.

**Amendment, 2026-08-28 — the grid fills the window.** The first cut packed as many *minimum-size*
tiles as fit (`minTileWidth`) at a *fixed* seven-row height, so a big terminal drew many tiny boxes
rather than a few big ones — "too small, doesn't show enough." `tileGridFor` (`boardtile.go`) now
chooses a near-square grid for the agent count and stretches each cell to fill *both* axes (the
rows fill the height the way the columns already filled the width); a fleet larger than fits at the
minimum cell size pages through the existing cursor window. **Guardrail 2 was relaxed, deliberately
and with the owner's yes:** the live tail was capped at `maxPreviewRows` (3, the DM preview's bound);
it is now bounded to the *cell's own body* — a big cell fills with output — with retention capped at
`maxTileTailRows` per agent so the per-token work stays flat. Still view-only, still no scrollback,
still a fixed equal-cell grid with no dividers or splits (guardrails 1, 3, 4 untouched). The cap is a
tile-only field on `partial` (`rowCap`); the DM preview and the inbox fold keep the three-row bound.
`tileGridFor` is one function read by the draw, the mouse (`boardHit`) and the cursor (`stepBoard`),
so a click and a tile cannot disagree.
