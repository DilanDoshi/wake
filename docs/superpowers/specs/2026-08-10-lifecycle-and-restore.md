# The lifecycle verbs, and bringing the room back

**Settled with the owner on 2026-08-10.** This supersedes the `⌃C`-means-detach binding that Phase 1
shipped, and it is the reason park/wake is being built now rather than later.

---

## 1. What changed, and why the order matters

> **Shipped in full, 2026-08-11.** The tense below is the tense this was written in, and it is kept:
> `⌃C` **parks** now, `⌃O` detaches, `⌃Q` parks the fleet and exits. The proposal's own ordering was
> followed — park, wake and the park book shipped first, and the keys were bound after they worked.
> **§3 is built** — bare `wake` reopens the room over whatever fleet there is, including one parked
> by `⌃Q` with no daemon behind it, and still spawns on a machine with nothing running. **§4 is
> built, and the restore is *offered*, as a line** naming who is parked and `/resume all`, because an
> automatic one would put N spawns in front of the `FrameHello` `daemon.EnsureRunning`'s
> discriminator waits on. `CLAUDE.md` names this document as the one that wins a disagreement, so
> what it says in the present tense has to be checkable against the build.
>
> **Corrected 2026-08-11 by the whole-branch review.** This note called the restore *"§6's last open
> question"*. It was the last one **it** answered, not the last one there was: §6 had four entries
> and the tasks settled three of them, leaving two reading as open — the detach key, which the very
> next clause of this note names as `⌃O`, so the document contradicted itself four lines apart. §2's
> table and key inventory carried the same staleness. §6 is the list somebody reads to decide what to
> build next, and two of its four entries would have sent them to work that was already done.

`⌃C` detaches today. It detaches **because stopping is irreversible** — there is no resume path, so a
stray keystroke that ended a session would destroy an hour of context with no way back. Detach was
the safe thing to put under the key everyone reaches for.

Park removes that reason. A parked agent's process stops, its transcript stays on disk, and
`--resume` brings it back with full context. Once that exists, `⌃C` can mean what an operator
expects it to mean.

**So park/wake is a prerequisite for the rebinding, not a companion to it.** Build the machinery
first; rebind after it works.

## 2. The verbs

| Gesture | What it does | Recoverable |
|---|---|---|
| **`⌃C`** | Park the focused agent — its process stops, its transcript is kept | Yes, `/resume` |
| **`⌃Q`** | Park the **whole fleet**, then close Wake | Yes, on next start |
| **`⌃O`** (detach) | Close Wake, leave every agent running | N/A — nothing stopped |
| **`/resume`** | Bring a parked session back | — |
| **`wake stop`** | End the fleet deliberately, as today | No |

**`⌃Q` parks rather than abandons, and that is the load-bearing choice.** The owner picked it over
"quit and leave them running", and it buys the restore in §4: a fleet that was parked on the way out
is a fleet Wake knows how to bring back. A fleet that was merely abandoned is a guess.

Detach survives as its own key because "close the terminal, let twenty agents keep working" is what
the background daemon exists for. Losing it would make the daemon pointless.

**Key choice was open and constrained; it is now made.** Detach is **`⌃O`**. Bound today:
`↵ ⎋ ⌃O ⌃C ⇥ ⇧⇥ ⌃D ⌃W ⌃G ⌃R ⇞⇟ ⌃F ⌃Q`. The constraint the choice was made under is unchanged and
still binds the **next** key: `⌃O ⌃X ⌃Y` were the only unshadowed keys left, `⌃O` is spent here, so
**`⌃X ⌃Y` remain** — and one of those is owed to next-blocked when deferred **I7** reclaims `⇧⇥` for
the permission mode, which is a founding requirement (`docs/goals.md` §5). **Nothing may be built on
`⇧⇥`.** `⌃Q` is not from that set: it is a chord nothing in this build or in `bubbles` binds, and
`internal/ui/keyprobe_test.go` asserts bubbletea names `\x11` as `ctrl+q` — whether a *terminal*
delivers it is `docs/live-testing.md`'s item 1, since `IXON` eats `⌃Q` as XON when flow control is
on and `go test` has no tty. The legend is an enforced bijection: a glyph with no binding and a
binding with no glyph are each build failures.

## 3. `wake`, with no arguments, reopens the room

Today the verbs are `daemon status stop attach new fork`, and getting back into a detached fleet
means `wake attach <somebody>` — you have to name a person to see the room. The room comes back
whole when you do (it seeds from the live fleet), so this is a missing front door rather than a
missing feature.

**Bare `wake` should reopen the room.** Two cases, and the second is why this is not a one-liner:

- **A fleet exists** → open the room on it, no target, nothing spawned.
- **No fleet exists** → this is first run, and the current behaviour (start a daemon, spawn a
  session, open it) is right. Do not make a new user type two commands to get an agent.

So bare `wake` branches on whether the fleet is empty, and both halves must be tested. The empty
case is the one a new user hits exactly once and never forgives.

## 4. Restore on start

If `⌃Q` parked the fleet, the next `wake` can offer to wake it. That is the "`--resume` for all of
Wake" the owner asked for, and it is bookkeeping rather than inference **only because `⌃Q` parks**.

Constraints from the recordings — all binding, none optional:

- **`--resume <id>` reuses the session id.** Recorded in both resume pairs
  (`interrupt-then-close`/`interrupt-resume-after`, `resume-park`/`resume-wake`). So waking a parked
  agent is `--resume <its own id>` and it keeps its identity, its roster row and its name.
- **`--session-id` and `--resume` together, without `--fork-session`, is a startup failure with
  nothing on stdout.** `core.identityArgs` is a closed switch precisely so this shape cannot be
  built; park/wake's arm goes **into that switch**, not beside it.
- **Resuming in a different working directory is completely unrecorded**
  (`2026-08-10-live-fork-findings.md` §12). A restore must resume each session **in the directory it
  was recorded in**, which Wake knows. Do not resume into `$PWD`.
- **Two live processes on one id branch the transcript silently** — no error, no frame, last writer
  wins (`2026-08-09-resume-fork-findings.md` §5). So waking must prove the parked process is gone
  before it resumes the id. The liveness check is Wake's own; nothing on the wire will say.

**What the roster file is and is not.** `internal/daemon/roster.go` records each session for exactly
one reader — `wake status` on a machine whose daemon died — and **nothing turns a roster record back
into an agent**. Restore changes that, so it needs its own record with its own argument, and
`CLAUDE.md`'s paragraph on why the registry does not survive a restart has to be revisited in the
same change rather than quietly contradicted.

## 5. `/resume` means Wake grows slash commands

Wake has **none**. `/resume` is the first, and the machinery is the same one `/new agent in <dir>`
and `/add-<agent-name>` need — which `docs/goals.md` §3 names as the largest single gap between the
founding message and the build: *Wake can manage agents but cannot create or name them from inside
itself.*

That gap belongs to no phase today. `/resume` drags most of it into reach, so whoever builds the
slash-command layer should build it as a layer rather than as one command with a `/` in front of it.

Note the collision the router already has to handle: `@` is overloaded (a session name **or** a
file path, resolved live-name-first), and `/` will need the same care — a slash command and a
message that happens to start with `/` are not the same thing, and Claude Code's own slash commands
must still pass through to the agent.

## 6. What this does not settle

**Three of these four are settled. Only the second is genuinely open**, and the strikethroughs are
kept rather than deleted so the reasons stay readable.

- ~~**The detach key.** Constrained by §2; not chosen.~~ **Settled: `⌃O`**, and §2 above now says so
  in its table. The constraint it was chosen under survives and is restated there, because it binds
  the next key rather than this one: `⌃X ⌃Y` are what is left and deferred **I7** is owed one.
- **Whether parking is per-agent only, or whether the pool parks automatically** under a live cap.
  The pool is Phase 3's other half and `internal/core/pool.go` exists as a stub. Restore must not
  assume a human parked everything. **Still open, and the offer was built to it**: `ui.ParkedNotice`
  names *who* is parked rather than only how many, precisely because a pool parking under a cap
  writes the same records a human does.
- **What a parked agent looks like in the room and the sidebars**, which is the narrow half of this
  bullet and all that remains of it. ~~It is neither working nor idle nor ended, and `rpc`'s state
  set has no member for it — which means `forkRefusal`'s totality guard will demand a verdict for it
  the moment it is added.~~ **Both halves of that are spent**: `rpc.StateParked` exists, and
  `forkRefusal` was duly made to rule on it (`internal/daemon/spawn.go`), along with five other
  totality guards across four packages — the prediction was right, which is why it is no longer the
  reason. What is left is the drawing: `attention.go` ranks it between silent and ended and
  `roster.go` draws it `▪`, and the rest is owned in detail by `docs/notes/deferred.md`'s
  parked-agent entry. **Read that rather than this bullet.**
- ~~**Whether restore is automatic or offered.**~~ **Settled 2026-08-11: offered, as a line.** Not
  automatic, and the reason is `daemon.EnsureRunning`'s discriminator rather than taste — a start
  that woke N sessions would put N spawns in front of the `FrameHello` it waits on, so `wake
  status`, `wake attach` and `wake fork` would each relaunch a fleet on their way past. Not a
  prompt, because that blocks the first frame on a decision the operator may not have come here to
  make, and not a flag, because a flag is invisible to whoever needed it. The line names *who* is
  parked rather than only how many, since the bullet above says restore must not assume a human
  parked everything. `ui.ParkedNotice`.
