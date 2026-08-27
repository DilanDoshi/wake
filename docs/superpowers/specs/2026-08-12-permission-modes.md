# Permission modes — `⇧⇥` for plan / auto / manual

**Status: BUILT, 2026-08-12.** All five pieces of §3 landed. The two rulings this design left to the
owner were taken: §2c is **"say it"** — no park-book field, and `ui.modeReverted` names the mode a
woken session came back in — and the cycle is **`plan` / `auto` / `default`**, spelling the third
position the way the receipt spells it so §2a's trap is dissolved rather than handled. Next-blocked
took `⌃X`; `⌃Y` is still reserved. `docs/notes/deferred.md`'s I7 entry is the summary of what
shipped.

One thing this design did not rule on and the build did: **`⇧⇥` refuses an agent blocked on a
permission ask**, the way `⌃C` does, because findings §9 items 2 and 3 leave "is the request
accepted mid-turn" and "what happens to an outstanding ask" unrecorded — and `⇧⇥` is a key an
operator can press at any moment. See `ui.modeWouldRaceAnAsk`.

Original header, kept: design, ready to implement. Written 2026-08-12 for a session that did not do
the spike.

**What it is:** the founding message's one named shortcut — *"shortcuts like shift enter for
plan/auto/manual mode"*, which in Claude Code is `⇧⇥`. Deferred **I7**. It is the last unbuilt item
from that message that changes what Wake can do rather than how it looks.

**Read first, in this order:**

1. `docs/superpowers/notes/2026-08-12-permission-mode-findings.md` — the recording spike. Every
   claim below that starts "measured" is from it, with a section number.
2. `docs/goals.md` §5 — why `⇧⇥` is currently bound to something else and what it owes back.
3. `internal/ui/composer.go`'s `Mode` type and `legendEntries` — the surface that already exists and
   has been lying about this since Phase 1.

---

## 1. Why this was blocked, and what unblocked it

`Mode` exists in `internal/ui` today, is displayed, and **is not connected to anything**. A key that
appeared to cycle it moved a label and reached no process — and it failed in the unsafe direction,
because the label that appeared to *tighten* permissions was the one that did nothing. It was cut
from the legend rather than fixed, under this project's rule that a missing control is not trusted
and a lying one is.

It stayed cut because nobody knew whether a mode could be changed at all after spawn.
`core.Config.PermissionMode` reaches the command line once and `core.Session` has no setter.

**The spike settled it.** A `control_request` with subtype `set_permission_mode` works, is
acknowledged, and the new mode is genuinely in force. That is the mechanism. Build on it.

---

## 2. The three traps, each of which sinks a naive build

**2a. The vocabulary is not the vocabulary `--permission-mode` takes** (findings §6). The words the
control request accepts are not the words the spawn flag accepts, and `manual` is the one that
differs — it comes back as `default`. A cycle built on the spawn flag's list lands on a value the
CLI names differently, and the indicator is then wrong on at least one position of the cycle rather
than only in principle. **Read §6 and take its list verbatim.**

**2b. The receipt is the source of truth, not the keystroke** (findings §10). This is `⌃F`'s pattern
and it is not optional: the key writes a frame, the daemon's answer is the confirmation, and the
label moves when the answer arrives. A label that moves on the keypress is the exact defect that got
the old indicator deleted, rebuilt on top of a working mechanism.

`init.permissionMode` is a **second observable** and its trap is one-directional. At spawn it is
normalized and must not be read as an echo of the flag — that rule is unchanged and is in
`CLAUDE.md`. *After* a `set_permission_mode` it reports the mode the session is genuinely in, so it
is the right thing to reconcile a client's belief against, and it costs nothing: it already arrives
on every turn.

**2c. A mid-session mode change does not survive a park and wake** (findings §8). A woken session
comes back in its **spawn** mode. This has to be designed for in the same change or `⇧⇥` ships a
mode that silently reverts on `⌃Q` — which is *worse than today*, because today the label is at
least honest about reaching nothing. Two acceptable answers, and the choice is the owner's:

- **Persist it.** The park book gains a mode field, and `unpark` re-applies it after the session is
  running. Costs a field in `parked.json` and a re-apply on a path that currently starts nothing.
- **Say it.** The wake notice says the session came back in its spawn mode. Costs a sentence and
  keeps the park book minimal, which `CLAUDE.md` treats as load-bearing.

Do not ship neither.

**`bypassPermissions` is refused by the CLI and Wake gets that for free** (findings §7) — it is not
in the cycle, and nothing needs to special-case it.

---

## 3. What to build

Five pieces. Each is a task; the order is a dependency order.

**3a. `core.Session.SetMode(mode string) error`** — writes the control request to the child's stdin,
exactly the way `Interrupt` does, and mints the `request_id` as it writes. `internal/core/encode.go`
gets the encoder beside `EncodeInterrupt`; the wire words live in the airlock and nowhere else.
Return the id, for the same reason `Interrupt` does: a receipt that names no session is
unattributable at 30 agents.

**3b. `rpc.FrameMode`** — a new frame kind carrying `SessionID` and the mode. A kind rather than a
field on an existing frame, for `FrameAllow`/`FrameDeny`'s reason: an empty mode would have to mean
something, and both readings are wrong.

**3c. The daemon's dispatch.** One case, routed through the agent's input queue the way park's write
is — this *is* a write to a running process's stdin, unlike `unpark`. Adding the case makes
`cmd/wake/mcpguard_test.go`'s `managerVerbs` demand a verdict, which is that guard working.

> **The verdict is refuse, and the argument is the one the two-verb list already makes.** A model
> that can loosen its own fleet's permission modes is the escalation `managerVerbs` exists to
> prevent, and unlike send and interrupt it is not undoable by looking at the room. Write that
> argument into the table rather than inheriting a default.

**3d. The key and the label.** `⇧⇥` cycles; the composer's `Mode` becomes real and moves on the
receipt (2b). Two things follow that are not optional:

- **`⇧⇥`'s current binding moves.** It holds "next blocked" today. `docs/goals.md` §5 records this
  debt with `⌃X` and `⌃Y` reserved and unspent — take one. Both are keys neither bubbletea nor
  bubbles binds (`internal/ui/keyprobe_test.go`).
- **The legend is a bijection.** `legendEntries` and the `tea.Key…` cases in `App.key` are held
  equal by a test, so the entry and the binding land in the same change or the build fails. The two
  width constants in `legend_test.go` and the numbers in `CLAUDE.md` are derived — re-measure, never
  hand-edit.

**3e. Whose mode.** `⇧⇥` in a DM is that agent's. In the room it is **the roster cursor's agent, and
only while the activity sidebar is drawn** — the rule `pickedAgent` already implements for `⌃C` and
`⎋`, and it exists because `⌃C` parked an agent the operator never chose. Use `pickedAgent`; do not
write a fourth answer to "whose agent is this".

---

## 4. What this must not become

- **Not a fleet-wide mode.** There is no `@all` for this. Thirty agents' permission modes changed by
  one keystroke is the failure the per-agent card exists to prevent, arriving one level up.
- **Not a spawn-time-only setting renamed.** `Config.PermissionMode` stays what it is: the mode the
  session starts in. This adds a change *during* a session; it does not replace the flag.
- **Not silent.** Every other state change in this build that an operator cannot see is a defect
  this project has already paid for once (`⌃C` parking an unnamed agent, a blocked agent reading as
  idle). The mode belongs on screen wherever it is in force.

---

## 5. How to know it works

The pty harness is the instrument: `cmd/wake/screen_unix_test.go`, and the existing screen tests are
the pattern. At minimum, on a real screen:

- `⇧⇥` moves the label, and the label matches what the *receipt* said rather than what the keystroke
  asked for.
- The full cycle returns to where it started and every position names a mode the CLI accepts.
- An agent in `plan` does not edit — this one needs a fake agent script that reports its own mode,
  or a recorded fixture; `withScriptedAgent` already takes a script name.
- `⌃Q`, restart, `/resume`: the mode is whatever §2c's ruling says it is, and the operator is told.

**Never against a live model.** 45+ fixtures in `testdata/stream/`, and this spike added
`mode-cycle.jsonl`, `mode-set.jsonl`, `mode-park.jsonl` and `mode-wake.jsonl` — the last two are
§2c's evidence.

---

## 6. What the spike did not settle

Findings §9, quoted rather than summarised. Read it before assuming anything about behaviour outside
what was recorded — this project's rule is that unrecorded behaviour gets a spike, not a guess.
