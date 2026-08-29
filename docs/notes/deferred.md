# Deferred work

Everything consciously put off, triaged by what it blocks. **Tracked**, and moved here from
`notes/` on 2026-08-09 — see `decisions.md` for why an ignored file turned out to be the wrong
home for work a subagent has to read.

The chronological record lives in `.superpowers/sdd/2026-08-08-wake-core-daemon-dm/progress.md`.
That file is for recovery; this one is for deciding what to do next.

**Rule for this file:** an item leaves only when it is fixed or when someone decides it never
will be, and the decision is written down. Nothing gets quietly dropped.

Last updated: **Phase 2 Task 15 — the manager**, 2026-08-11. `wake manager` starts a real `claude`
session with Wake's five tools, `--strict-mcp-config` beside them and a scoping system prompt; the
room's default addressee is it, and `@all` is not. Before that: **park/wake's eight tasks merged and
the branch reviewed as a whole**, 2026-08-11.
Park, wake, the park book, `⌃C`/`⌃Q`/`⌃O`, `/resume` and bare `wake` all run, and the lifecycle
composes end to end from a keyboard across a daemon restart. **The ranked outstanding-bug list is
the first section below** — read it before trusting any of this in front of real work. Before that:
all eleven tasks of Phase 1 merged — Task 6 (the daemon) after three fix rounds, Task 10 (the
wire-up) after four, Task 11 (CI) in its final tidy.

**Items in this file have now been wrong more often than they have been forgotten**, which is
worth stating because it changes how to read it:

- **I6's prescribed remedy was a guess.** "Add the `parent_tool_use_id` struct field" assumed a
  field that was `null` on all 67 of its corpus occurrences. It got a recording instead, and the
  recording answered five questions and reshaped what Phase 2 needs.
- **Item 2 under "Blocks Phase 1" was already done** and had been sitting here as open.
- **I5 was closed by Task 6 without anyone noticing**, exactly to its own prescription.
- **The `~15 bytes` estimate under Task 11 was wrong** — measurement said 6.
- **I1 was carried three rounds without a ruling**, which is the failure this file exists to
  prevent.

So: before acting on an entry, check it still describes the tree. Four of the last dozen did not.

---

## OWNER REQUEST, 2026-08-28 — `⇧↵` for a newline in the composer

**Asked for in this version.** Shift+Enter to insert a newline in the draft — the reflex an operator
brings from Claude Code and most chat UIs.

**It already works — exactly when it works in Claude Code, and by the same mechanism — but not on a
bare terminal.** Newline in the composer is `⌥↵` / `⌃J` (`internal/ui/composer.go`, `InsertNewline`
→ `key.WithKeys("alt+enter", "ctrl+j")`). `⇧↵` is deliberately **not** in that list, because it
cannot be: `keyprobe_test.go` has the chord producing **no `KeyMsg` at all** under either keyboard
protocol bubbletea v1.3.10 speaks (Kitty CSI-u and xterm `modifyOtherKeys`), and a terminal with no
protocol sends `⇧↵` as the byte for `↵` — which `App.key` claims for **send** before the composer
ever sees it. Claude Code does not bind `⇧↵` either: its `/terminal-setup` configures the *terminal*
to emit `ESC CR` for the chord, which bubbletea names `alt+enter` — the sequence Wake already binds.
So an operator who has run Claude Code's `/terminal-setup` (or bound Shift+Enter → `\x1b\r`
themselves) already has `⇧↵` working in Wake, and `⌃J` is the fallback that needs no configuration at
all. `docs/live-testing.md` documents this as the one key that needs the operator's own setup.

**So the deferred piece is not "bind `⇧↵`" — that is a dead end that fails silently** (it produces no
message, so a binding for it can never fire, and `keyprobe_test.go` is the guard that says so). What
is actually unbuilt is one of two things:

1. **A Wake-owned terminal-setup helper**, the equivalent of Claude Code's `/terminal-setup`, that
   writes the iTerm2 / VSCode / Ghostty keymap so `⇧↵` emits `ESC CR` without the operator having to
   have run Claude Code's first. This is the same *behaviour* Claude Code ships and would make `⇧↵`
   work out of the box — but it edits the operator's terminal config, which is a side effect on the
   machine Wake otherwise never has, and it is per-terminal-emulator surface to maintain.
2. **A bubbletea version that decodes the chord natively.** The v2 line speaks the Kitty keyboard
   protocol and, on a terminal that has it enabled (Ghostty, Kitty, WezTerm), could deliver `⇧↵` as a
   distinct `KeyMsg` — at which point Wake binds it directly: no terminal setup, no `ESC CR`
   round-trip. `keyprobe_test.go` is exactly the guard that would flip on such an upgrade and tell the
   next person the chord is now reachable; until it does, the finding it holds stands.

**The tension worth stating.** Option 1 is the only one that helps the operator on a terminal with no
keyboard protocol — but it is a config-writing side effect on the host, a category of thing Wake
avoids, and it duplicates work Claude Code already does for the operator who ran it. Option 2 is clean
but gated on a dependency upgrade nobody has scheduled, and it never reaches a bare terminal (no
protocol, no distinct chord). Neither is broken today: `⌃J` sends a newline everywhere with nothing
configured, and `⇧↵` works through the shared terminal setup.

*Blocks:* nothing. `⌃J` is the universal fallback and `⇧↵` already works via terminal setup; this is
about making the reflex work with zero operator configuration.

## OWNER REQUEST, 2026-08-28 — a ping other platforms can pick up when an agent needs the operator

**Asked for in this version.** A notification/ping — something cmux, tmux, iTerm2 or any host terminal
already knows how to surface — emitted when an agent needs the operator: it asked a question, hit a
permission it cannot answer itself, or otherwise blocked with `N need you`. The operator running a
30-agent fleet is not staring at the awareness strip, and the whole point is to be told when to look.

**What exists today is on-screen only, and it is load-bearing because nothing on the wire times out.**
A blocked agent is announced three ways, all *inside* Wake's own window: the roster row, the awareness
strip's `N need you` (`internal/ui/awareness.go`, `stateLabel`), and `⌃X` walking to the next blocked
one. The room deliberately draws **no card** for an agent blocked with its conversation closed —
`CLAUDE.md`'s "an ask belongs to its agent's conversation, and the room draws none" ruling — so those
three tells *are* it. And they matter more than decoration: nothing on the permission wire times out
(the corpus records one ask blocked **342 seconds with zero bytes out**), so an operator who has
tabbed away, detached with `⌃O`, or moved focus to another cmux pane has no signal at all that a
session is waiting. That detached/unfocused case is exactly what this request is about and exactly the
one the current tells cannot reach.

**The likely right layer is a signal, not a notification daemon** — which is also what keeps it inside
the non-negotiables. "Not a terminal emulator or multiplexer" means Wake should not grow its own
notify subsystem or learn which pane is focused; the host already owns that policy. The standard
vocabulary a TUI has here is the terminal's own activity/bell protocol: a plain `BEL` (which cmux and
tmux already treat as pane activity), or OSC 9 / OSC 777 for a desktop notification carrying a short
message on the terminals that honour it (iTerm2, kitty, WezTerm). Wake emits the *signal on the
transition into "needs you"*; cmux / the terminal decides whether to raise it and — crucially —
suppresses it for the focused pane, which is the one thing Wake cannot cheaply know and the host
trivially can. That division is the same "that's the host terminal's job" argument the non-negotiables
already make.

**It fits "cheap to leave open" only if it is strictly edge-triggered.** The emit hangs off the
*transition* an agent makes into a blocked/needs-you state — the same derivation `attention.go` and
`awareness.go` already compute — never a poll and never work per frame. Two hazards to design against,
both from fleet scale:
1. **A bell storm.** Fifteen agents crossing into blocked at once must not be fifteen bells. The unit
   is probably the fleet crossing `0 → ≥1 need you` (one ping), or a per-agent ping under a rate
   limit — not one per transition.
2. **Where the bytes go.** Bubble Tea draws through the one `*os.File` writer (`cmd/wake/output.go`);
   an OSC/BEL has to reach the tty without corrupting the alt-screen frame or being swallowed by the
   renderer. This wants the same care the clipboard's layered write already takes, and is the part
   most likely to be fiddly.

**Open questions:**
- **Which signal, and configurable?** `BEL` is the most portable (cmux/tmux activity) but carries no
  text; OSC 9/777 carries "iris needs you" but only some terminals honour it. Likely: bell by
  default, a richer notification where detected, and an off switch for operators whose terminal
  already bells noisily.
- **Which states ping?** Only "blocked / needs an answer" (question + permission), or also "turn
  finished, your move" — the latter is far noisier at fleet scale and probably out.
- **Content crossing a boundary.** A desktop notification leaves Wake's window; naming the agent
  ("iris") is fine, but anything richer (the question text itself) is content crossing to an external
  surface and should be opt-in at most.
- **Suppress when attached-and-focused?** Delegating that to the host (emit the bell, let the host's
  own unfocused-only policy decide) is cleaner than Wake reading CSI focus events, and keeps Wake out
  of the focus-tracking business entirely.

*Blocks:* nothing broken; this is an enhancement for the detached / not-watching operator, the one
case the awareness strip and roster tells structurally cannot cover.

## 2026-08-26 — record what ⎋ and `[d]` do to an `AskUserQuestion`, the spike §9 named

`fix/interrupt-question-stale-card` shipped ⎋-on-a-question as a **deny** (it was a no-op interrupt
that left the card stuck forever — see `decisions.md`, *"⎋ on a question is a deny"*). Both the
premise and the remedy lean on `[binary]` readings `question-findings.md` §9 explicitly leaves
unrecorded, so the fix is a UI-correctness change on unproven ground until the spike §9 calls "the
obvious next" is recorded:

1. **Interrupt a session with an `AskUserQuestion` outstanding.** Confirm the CLI does *not* emit a
   `control_cancel_request` / turn-end for it the way it does for a permission
   (`interrupt-permission-findings.md`) — i.e. that the interrupt genuinely leaves the question
   stuck, which is the whole reason ⎋ had to become a deny.
2. **Deny an `AskUserQuestion`.** Confirm an explicit `behavior:"deny"` control-response actually
   unblocks claude (not "cancel + deny skipped", `tengu_auq_park_preserved_at_shutdown`) and that
   the model does **not** immediately re-ask — a re-ask would turn ⎋ into a new card rather than an
   exit.

Both are cheap (one `AskUserQuestion` turn each, real `HOME`, scrub) and would either retire the two
residual risks in the ruling or send someone back to the daemon. Until then ⎋ adds no *new*
unrecorded dependency beyond the one `[d]` already carried, which is why it shipped.

## OWNER REQUEST, 2026-08-26 — a triage pass before the room, so not every agent utterance becomes a broadcast

**Asked for in this version.** A lightweight agent (haiku-class, cheap and fast) — or some other
filtering strategy — that decides whether a given message belongs in the main group chat, rather
than every eligible message landing there.

**What decides room membership today is mechanical, not judged.** Per `docs/notes/decisions.md`'s
room-history rules, whether a turn is public is answered structurally: a broadcast in is a broadcast
out — "a turn you typed comes back only when two transcripts prove it was a broadcast," and live,
`App.observe` keeps a DM-sent turn out of the room through `Fleet.inDM`. Nothing about the *content*
of what an agent says decides whether the room hears it; the room hears everything from a
conversation whose initiating turn was public. §1's premise ("agents stay quiet unless blocked or
have something to report") is a design intent, not an enforced filter — nothing in `internal/core` or
`internal/ui` currently judges an outgoing message for room-worthiness.

**The tension worth flagging before building this.** `attention.go` is a non-negotiable pure
function — "events in, ranked state out. No processes, no I/O" — precisely so the hardest logic in
the app stays testable without spawning anything. A live haiku call per candidate room message is the
opposite of that: I/O, latency, nondeterminism, and a per-message cost multiplied by fleet size. If
this is built, it likely wants to sit beside `router.go` rather than inside `attention.go`, and a
cheaper first cut — a rule-based/heuristic filter (message length, presence of a question, tool
result vs. prose, `permission_denials`, etc.) — should be considered and ruled out explicitly before
reaching for a model call, the way the profile detour was ruled out for budgets below.

**Open questions:** what "another strategy" means if not a model call (heuristics? a client-side
toggle per agent, akin to team tags?); whether the judgment happens per-event or per-turn; whether a
suppressed message is still visible from the DM view (so nothing is silently lost) or truly gone from
the operator's view of the fleet.

*Blocks:* nothing yet; this is a proposal for reducing room noise at fleet scale, not a fix for a
broken surface.

---

## OWNER REQUEST, 2026-08-26 — dropping into a subagent's conversation, the way `Explore`-style dispatch agents can be watched

**Asked for in this version.** Be able to drop into a subagent's own conversation the way one drops
into any other agent's — modelled on watching an `Explore`-type dispatch agent work.

**Most of this already exists, narrower than the ask.** Phase 3 landed dispatch viewing:
`internal/ui/taskrows.go` lists what a conversation has dispatched, `taskkeys.go` binds `⌃N`/`⌃P` to
walk the list, and `DM.Viewing` (`internal/ui/dm.go`) swaps the pane's transcript onto one dispatch's
forwarded frames — `↵` on the conversation's own row is the way back. So "open a subagent's
transcript" is built and reachable **only from inside an already-open DM whose parent dispatched it**.

**What is not built, and why the harder version may not be buildable at all.** Two gaps, of different
kinds:
1. **Reachability.** There is no way to jump to a subagent's transcript from the room, and no grid
   pane of its own — it borrows the parent DM's pane rather than opening the way `⌃Y`/`⌃B` would for
   a real agent. Nothing here is architecturally blocked; it is unbuilt surface.
2. **Interaction.** `renderForwarded` draws a subagent's own frames and nothing else, and there is no
   composer wired to it — you can watch, not talk. This is very possibly not a Wake gap: a Task-tool
   subagent runs *inside* its parent's own `claude` process via `--forward-subagent-text`, and has no
   `--session-id` of its own for Wake to address. Unless Claude Code exposes a way to reach a
   subagent independently (unrecorded — nothing in this file's CLI-surface section names one), "drop
   into and talk to a subagent" may be a Claude Code limitation Wake cannot design around; only the
   read-only view can be improved.

**Cheap first cut, if the read-only half is what's wanted:** make dispatch viewing reachable from the
room (drawing on `Cards.Undrawn`'s pattern of finding an ask with nowhere else to be drawn), rather
than requiring the parent DM to already be open.

*Blocks:* nothing yet; quality-of-life for supervising a fleet where several agents are mid-dispatch
at once.

---

## KNOWN GAP, 2026-08-26 — a subagent's checklist commingles into the parent's Fleet fold

**Surfaced by the task-board PR (`feat/todos-replace-dispatch-list`), pre-existing, not caused by it.**
`internal/ui/checklist.go`'s `Fleet.foldChecklist` keys purely on `sessionID` and does **not** check
`ev.Subagent`, unlike its DM-level twin `DM.foldChecklist`, which excludes a subagent's op. So if a
subagent ever emits `TaskCreate`/`TaskUpdate`, its op folds into the *parent session's*
`f.checklists[sessionID]`: the snapshot the Fleet fold attaches to `ev.Tool.Todos` (which the subagent's
forwarded view renders inline via `todoBlock`) is then the parent's commingled list, and the parent's
roster working-line `activeForm` can reflect a subagent's item.

**Why it is not fixed here.** The board pinned above the composer reads `DM.checklist`, which already
excludes subagents, so the board is correct. The commingling only touches the working line and the
subagent's *inline* forwarded list. A one-line `ev.Subagent == nil` guard in `Fleet.foldChecklist`
would stop the pollution but also blank the subagent's inline list (its `Todos` comes from that very
fold), so a real fix is a **per-subagent checklist** rather than a guard — new machinery, out of the
board PR's scope. **Unverified whether subagents emit these ops at all** (no corpus fixture does), so
this is latent until one is recorded.

*Blocks:* nothing; correctness of a subagent's own inline checklist and the parent's working line, if
subagents turn out to author task lists.

---

## ~~OWNER REQUEST, 2026-08-26 — `/color`: a per-agent status bar and name-tag colour~~ CLOSED, `feat/color-per-agent` (2026-08-27)

**Built exactly as the four answers below predicted, plus one the entry did not foresee.** `/color`
sets an agent's identity hue; it heads the agent's turns in the room, tints its conversation status
bar and its roster row. The four answers, resolved with the owner: **(1) where it lives** — a new
`Color` field, client-chosen, a third display string beside name and label (`rpc.SessionStatus.Color`,
`ui.Agent.Color`, `daemon.agent.color`); **(2) session vs client-local** — a *session attribute*, on
the wire and shared, because it reuses the `/name` round-trip with zero new state stores where a
client-local one would need a per-session-id prefs subsystem that does not exist; **(3) survives a
park** — yes, a `Color` field in `parkedRecord`, captured at park and restored at wake, the move
`Label` and `Effort` already make; **(4) value space** — a *named subset*, not free hex, but **Wake's
own bolder identity set** (`ui.identityColors`, seven contrast-tuned light/dark pairs) rather than
Claude's exact palette, on the owner's "do Claude but a little bolder" ruling — deliberately not bound
to `claude-palette.json`, the `LastRead` precedent. The manager is refused `FrameColor` (a third
display verb beside rename and label) and `agent_status` does not report the hue (operator chrome, not
a fact about what an agent is doing).

**The one the entry missed: `color` is Claude's own command.** It is advertised on 71 of the recorded
`init` frames (Claude's theme command), so the corpus rule refuses the word the way it refused
`rename` and `import`. The disciplined path to claim it is `redirectOnlyCommands`, backed by a
recording of Claude's headless `/color` showing an inert or redirect answer — which this session could
not make. The owner ruled on 2026-08-27 to claim `/color` anyway (Wake renders in its own palette, so
an agent's per-session Claude theme is irrelevant inside Wake): recorded as the
`ownerClaimedCommands` exception in `slashguard_test.go`, greppable and dated. **If a recording later
shows Claude's headless `/color` is a redirect, move the entry to `redirectOnlyCommands` and retire the
override.** That recording is the one loose end this leaves.

The original request:

**Asked for in this version.** A `/color` command to change an agent's status bar colour and its
name-tag colour as shown in the main group chat.

**Nothing today lets one agent look different from another.** `internal/ui/theme.go`'s palette is one
process-global set matched against Claude Code, used for state glyphs and chrome — not a per-session attribute. There is no field on `Agent` or
`rpc.SessionStatus` for a chosen colour, and the room's speaker labels (`userBlock` et al. in
`internal/ui/dm_blocks.go`) carry no per-agent identity styling today — thirty agents' turns are
distinguished by name text alone, not colour.

**This is the same shape of question this file's team-tags entry already worked through, and
probably wants the same four answers:** where the value lives (a new field, since `label` is the git
branch and `name` is daemon-assigned — colour would be a third, client-chosen string much like a team
tag); whether it is a session attribute or a client-local preference (a colour picked by one
operator's `/color` has no obvious reason to be visible to a second operator attached to the same
fleet, which team tags did not have to consider); whether it survives a park (`parked.json` again, and
the same "operator intent vs. process state" argument that favoured teams applies here); and what the
value space is — free RGB risks fighting the extracted palette's contrast guarantees in both light and
dark, so bounding `/color` to a named subset of the existing palette is worth considering over an
arbitrary hex.

*Blocks:* nothing yet; a legibility/personalisation request for the room at fleet scale.

---

## ~~2026-08-20 — the room's spawn path cannot set a spend ceiling~~ CLOSED, `feat/new-budget-flags` (2026-08-21)

Closed by exactly the entry's own prescription: `--max-budget-usd` and `--fallback-model` are two
rows in `takeNewFlags`' table, validated by `core.ValidBudget`/`core.ValidFallbackModel` before a
socket is dialled, threaded onto the spawn frame where `configRefusal` already fences them.
`TestEveryNewFlagReachesTheSpawnFrame` derives the set from `newFlags`' fields, so the silent-drop
hop this entry worried about is a build failure. One knock-on: `newUsage` moved to bracket
notation — with six flags the four enumerated grammar shapes no longer fit the notice row at 200
columns, and a usage the operator cannot read to its end teaches nothing. The **bare** line fits;
a refusal that prefixes it (`--x needs a value: …`) still truncates on narrower terminals, and
nothing asserts the on-screen fit — the adversarial pass measured it rather than the suite. The
original entry:

Surfaced while the owner was deciding profiles' fate (they are skipped — the flag-bundle reading
solves a problem this workflow does not have). What fell out of that conversation is a real hole:

**`/new` is the ordinary way an agent is spawned, and it cannot say a budget.** `takeNewFlags`'
table (`internal/ui/newflags.go`) is `--worktree`, `--add-dir` and the two debug flags — nothing
else. `--max-budget-usd` and `--fallback-model` exist only on the two shell verbs (`wake new`,
`wake manager`), and **there is no runtime command for either** — which is the very property the
budget's park-survival argument rests on ("nothing can restore it afterwards"). Model and effort
have the same spawn gap and do not matter: `/model` and `/effort` fix them one turn later. Budget
and fallback have no later. So every agent spawned from inside the room — the ordinary path — is
unbudgeted for its whole life, and the two flags added for fleet-scale discipline are unreachable
from the surface the fleet is actually run from.

*What closes it:* two rows in `takeNewFlags`' table, validated where the shell verbs already
validate (`core.ValidBudget`, `core.ValidFallbackModel`; the spawn frame carries both fields and
`configRefusal` already fences them daemon-side). The profile detour is explicitly **not** the fix
— owner, 2026-08-20.

*Blocks:* spend discipline at fleet scale, from the surface that matters.

---
## ~~OWNER REQUEST, 2026-08-26 — expand a room response in place, the way ⌃E expands a folded block in a DM~~ CLOSED, `feat/room-expand-response` (2026-08-27)

Closed as the entry's own "cheapest try" plus the owner's chosen split: a **click** on a folded
response toggles that one (`clickedTool`'s room branch → `Room.toggleLine`), and **`⌃E`** expands or
folds **all** of the room's collapsed responses at once (`Room.toggleExpandAll`), the room learning
the DM's `⌃E` rather than inventing a line cursor the room does not have. The full render is the one
`agentSaid` already computed and discarded past `roomInlineRows`; expanding keeps it, and the
speaker head keeps "whose it is". The default stays folded, so the room does not become the wall of
text the fold prevents. Expand state is `Room.expanded` (per-line, id-keyed, copied on write) plus
`Room.expandAll`, both bounded on reclaim/merge (`forgetExpanded`/`keptExpanded`) so they cannot
outgrow the retention cap. `⌃E`'s target being "the line under the cursor" from the note below was
dropped deliberately: every keystroke clears the selection (`App.cleared`) before `⌃E` runs, so a
mouse-anchor target would need sticky state against that rule — the click already gives per-line
precision. Tests in `internal/ui/roomexpand_test.go`.

**Asked for in this version.** In the group chat, be able to expand an agent's response inline — the
same gesture that opens a collapsed tool call or other folded block in a DM (`⌃E`).

**The room and a DM fold differently, and that is the whole of the ask.** A DM draws an agent's turn
at Claude Code fidelity: tool calls and results are rendered blocks, foldable and expanded with `⌃E`
(`App.key`'s expand case over `internal/ui/dm_blocks.go`). The room is not that surface — it is one
filtered line per broadcast, tuned so thirty agents do not bury each other (`internal/ui/chat.go`, the
`Room` view), so a long response reads as a summary rather than the full rendered block. There is
today no way to say "show me *that* one in full" without opening the agent's DM.

**What this needs is a per-line expand in the room**, and the design questions are where the DM's
answers do not carry over: the room interleaves many speakers, so an expanded block must make clear
whose it is and must re-collapse (a room full of expanded turns is the wall of text the filter exists
to prevent); the room's line is derived, not the transcript, so expanding one means reaching back to
the agent's actual turn (`Room.raw` / the history machinery already holds it) and rendering it in
place; and the key need not fight the DM's `⌃E` if the room simply routes `⌃E` to the line under the
cursor, which is the cheap answer worth trying first. It is the room learning one of the DM's
affordances, bounded so it does not become the DM.

*Blocks:* nothing yet; a room-fidelity request — the group chat gaining a way to see a full response
without leaving it.

---

## ~~OWNER REQUEST, 2026-08-26 — ⌃Q should arm before it parks-and-quits, so a stray press does not empty the screen~~ WON'T DO (owner, 2026-08-28)

**Owner's ruling, 2026-08-28: not wanted — dropped ahead of the first release.** The ask was to make
⌃Q a two-step arm/confirm (a first ⌃Q that arms, `↵` that confirms) so a stray press could not park
the fleet and close the window by accident. On review the owner decided the safety affordance is not
worth the extra gesture: a stray ⌃Q is recoverable (parking writes `parked.json`; `wake` — or
`wake --fleet <name>` for a named fleet — reopens it), nothing is lost, and the emergency path already
distinct from it (`⌃Q⌃Q` reads the tty before Bubble Tea, `cmd/wake/killswitch.go`) covers the wedged
case. Kept here as a struck-through record rather than deleted so the idea is not re-proposed as new:
it was considered and declined, not forgotten.

---

## OWNER REQUEST, 2026-08-16 — teams: a named tag on an agent, and a group that is not a directory

**Asked for in this version.** Agents carry a team tag so the fleet can be grouped by hand, rather
than only by where each session happens to have been started.

**This is spec §11's configured groups, and `internal/ui/groups.go` already says it is standing in
for them** — its own header: *"This is the v1 stand-in for spec §11's configured groups. Nothing
here persists and nothing is named by hand: the grouping is derived from where each session was
started."* So this entry is not new scope, it is the thing the current sidebar was explicitly a
placeholder for. Read that header before designing anything.

**What exists today.** The left sidebar is one row per **workspace**, and a workspace is a
*directory* — `rpc.SessionStatus.Dir`, basename as the label, unread summed per row. Derived, not
stored; nothing is named, nothing persists, and two agents in one repo cannot be told apart while
two repos on `main` are correctly two rows.

**What a team adds that a directory cannot.** Several agents in one repository on different jobs;
one job spanning several repositories. Both are the ordinary case at 15–30 sessions and neither is
expressible today.

Four things to decide, in the order they bite:

1. **Where the tag lives.** `label` is already taken — it is the git branch, from `internal/gitref`.
   A team is a *third* string beside name and label, so it wants a field on the roster and on
   `rpc.SessionStatus`. Wake already stores "roster, park book, groups, layout", so a persisted
   group set has a slot contemplated for it; a team that vanishes on daemon restart is not the
   feature.
2. **Whether `@team` routes.** This is the load-bearing one. **"A name is never an address"** — the
   non-negotiable is that `rpc.Frame` carries `SessionID` and the reaper proves a process group by
   finding that UUID in an argv. A team is a *set* of session ids, so `@backend hello` is a fan-out
   the router resolves to many frames, not a new kind of address. `internal/core/router.go` is pure
   and table-tested, which is where that resolution belongs.
3. **Whether the daemon or the client names it.** A session's *name* comes from the daemon because
   only it sees the whole fleet. A **team** is the opposite: it is the operator's own categorisation
   and nothing in the fleet can derive it, so this is the first string a client legitimately
   chooses. That asymmetry is worth writing down when it is built, because it looks like a
   violation of the naming rule and is not.
4. **Whether it survives a park.** `parked.json` holds id, directory, name, label, parked-at, and
   the park book's minimalism is deliberate. A team tag is operator intent rather than process
   state, so it is a better candidate for the book than a permission mode was — but it is still a
   field, and the rule is that the book holds the minimum that can do the job.

**Cheap first cut, if the whole thing is too much for one task:** a team is a name and a set of
session ids in the groups file, the sidebar groups by it *when set* and falls back to the directory
when not, and `@team` fans out. That is additive to `groups.go` rather than a rewrite of it, and it
leaves the park and routing questions answerable separately.

**~~Related, and probably the same task: spec §11's profiles are the other half~~ — overtaken by
the owner's ruling, 2026-08-20: profiles are skipped** ("the flag-bundle reading solves a problem
this workflow does not have"; the budget entry above records the one real gap that conversation
surfaced, and it is two `/new` flags rather than a profile). Teams stand alone. The stale
`feat/profiles` branch (259 commits behind when last measured) is reference material for nobody and
should be deleted rather than rebased.

**Status, 2026-08-21: awaiting the owner's call — do not start it.** The owner reconfirmed the
*concept* on 2026-08-20 while ruling profiles out, then held both this and the subagents-sidebar
landing the next day: *"item 1 is okay but I'm not sure about 2 and 3 yet."* Item 1 is `/new`'s
budget flags (the entry above); item 2 is the subagents landing, parked as draft PR #61; this is
item 3. It stays the largest unbuilt piece of the original design, and everything in this entry
still describes the tree — but building it before the owner says so would be scope nobody asked
for this week.
---

## FROM `feat/expand-and-worktree`, 2026-08-15

**1. `TestTheTranscriptKeysAreSpelledOnlyInDiscover` does not see a struct tag.** The guard unquotes
every `*ast.BasicLit` string in the tree and compares it to discover.go's `key*` constants. A struct
tag is one literal, so `` `json:"cwd"` `` unquotes to `json:"cwd"` and never matches `cwd` — which
means a production file could learn to read claude's on-disk transcript through a tag and the guard
would pass. Found because `internal/core/wire.go` now carries `Cwd string \`json:"cwd"\`` for the
*stream's* init frame, which is the other airlock legitimately naming the same word, and the guard
stayed green for the wrong reason.

Not widened here on purpose. Closing it means deciding **which files may name a transcript key from
a tag**, and the honest answer is probably "the four airlock files, by name, with their reason" —
an exemption list, which is the decay that guard's own header warns about. That is a ruling, and
buying it inside a feature branch is how the list starts. `internal/core/airlock_test.go`'s `"cwd"`
entry carries the same note beside the word.

**2. A session's directory now moves, and two readers of it were never designed for that.**
`agent.observe` follows `init.cwd`, so `a.dir` changes when an agent uses `EnterWorktree`. The fleet
report and the park book both read it and are correct by construction. Two things are not checked:

- **`groups.go` keys workspaces on the directory string.** An agent that moves creates a second
  workspace row and leaves the first one behind if nothing else is in it. Whether that is right —
  a worktree *is* arguably its own workspace — is a product question nobody has answered.
- **Nothing re-derives the label on a move.** `daemon/label.go` reads `.git/HEAD` at spawn, so a
  session that enters a worktree keeps the branch name of the tree it left. The status bar re-reads
  through `gitref` and is right; the roster's label is stale until a rename.

---

## IMAGE PASTE, 2026-08-15 — transport proved; image *drop* shipped 2026-08-22, and ⌃V paste works through it on macOS (tested 2026-08-28)

**The wire question is closed and recorded.** A headless session accepts an image block on stdin and
reads it: `testdata/stream/image-block.stdin.jsonl` is the line Wake would write, `image-block.jsonl`
is what came back, and the reply names two words that were only in the pixels. One live turn,
`$0.0302`. Full findings and the failure table:
`docs/superpowers/notes/2026-08-15-image-input-findings.md`.

**Drag-and-drop is built** (`internal/ui/imagedrop.go`, `composerimage.go`): a terminal delivers a
dropped file as a bracketed paste of its *path*, so Wake reads the file off disk rather than off the
pasteboard, and the two blockers below are the pasteboard's, not the drop's. A drop leaves an
`[Image #N]` chip in the draft — plain draft text, so no render change was needed — and on send the
chips are stripped out of the text while the images ride beside it as `rpc.Frame.Images`
(`core.ImageBlock` → `EncodeUserMessage`, images first, text last, never an empty content array). An
image whose media type is not one of the four claude reads, or that fails to decode, is refused at
the read and its path put back into the draft, which sidesteps the silent-degrade trap below: Wake
never sends a block it could not sniff as a real PNG/JPEG/GIF/WebP. Inbound decode is done too —
`blockEvent` turns an image block read off a transcript into a `[Image]` placeholder, so a reopened
or imported conversation shows the image was there rather than nothing.

**Clipboard paste (`⌃V`) already works on macOS — through the drop path, tested 2026-08-28.** A
macOS terminal answers ⌃V on an image by writing it to a temp file and pasting the *path* (e.g.
`…/TemporaryItems/NSIRD_screencaptureui_…/Screenshot….png`), which is a bracketed paste `droppedImage`
already hijacks: `imageDropPaths` accepts it because it is an absolute image path, and `splitDropPaths`
handles the spaces in the name. No `KeyCtrlV` case and no pasteboard read were needed — the OS did the
read and handed over a path, which is exactly the half the drop path already solved. So for the common
case there is nothing to build; this is why a grep for `KeyCtrlV` finds nothing yet ⌃V works.

**What is genuinely still deferred is the raw-bytes case, if it ever matters:** a terminal or OS that
delivers image *bytes* on paste rather than a temp-file path. That is the only thing
`internal/ui/clipboard.go` being write-only (three layers of *copy*, no read) actually blocks, and
getting bytes off the pasteboard needs more than `pbpaste`. It is outside spec §17's "in" list, and no
recorded terminal needs it — so it stays a note, not a task.

**One thing that still holds:** an image claude cannot decode or shrink becomes a *text block* the
model reads, on a turn that ends `success`, invisibly. The drop path narrows the window by refusing
an unsniffable file up front, but a file that sniffs as a valid image and is still too large for
claude's per-model budget will degrade there, and Wake sees nothing on stdout saying so.

---

## OWNER REQUESTS, 2026-08-12 — from the first live run

Three things the owner asked for after running the build. Two are fixed; the rest are here.

**Fixed the same day** (`cmd/wake/screen_unix_test.go` is the harness that found and holds them):

- The composer drew an empty box over a draft that was still there. `Composer.SetWidth` is now
  called when a pane is sized rather than only when it is drawn.
- A DM's turn appeared in the group chat. `Fleet.sending` records which pane a turn was sent from;
  the room draws a turn it did not start only when the agent needs somebody. **The parenthetical
  that used to close this line — *"a card always reaches it"* — is false since BUG-2
  (`bugs.md`): the room draws no card at all. A DM turn's ask is announced on the roster row and
  in the strip's `N need you`, and `⌃X` opens the conversation it is in.**

**Open, and both are features rather than defects:**

1. **A woken agent comes back with no account of itself.** *"If you bring an agent back it should
   have its history, just like when you /resume a claude agent."* The model keeps its context —
   claude has the transcript — so this is the **pane**, not the conversation, and Wake already says
   so on the wake: *"What it said before now is not here - claude keeps the transcript, Wake does
   not."* Closing it means reading `~/.claude/projects/<slug>/<id>.jsonl` and rendering it into the
   DM. That file is a **different Claude format from the stream** `internal/core` owns, so it goes
   through `internal/daemon/discover.go`'s airlock, which already reads three keys out of it.
2. **~~A breathing activity indicator.~~ MOSTLY BUILT** (`feat/claude-code-fidelity`).
   `internal/ui/heartbeat.go` draws Claude's own `✻ Calculating… (1m 51s)` in the conversation
   pane, and `beat.go` answers the design question the way this entry demanded: **one** ticker for
   the fleet, stopping dead when the last turn ends, with the animation read from wall-clock
   elapsed so a late tick loses a frame rather than desynchronising the sweep. `beat_test.go`
   asserts both as costs. The frames, their ping-pong, the 120ms step and the shimmer's geometry
   match Claude's rather than being eyeballed. The **word pool is Wake's own** — 205 written for
   Wake, sharing only the four plainest words with Claude's list; `heartbeatwords.go` argues why
   a lifted list was the one thing here that could not stay.
   The sidebar half is drawn too: `roster.rowGlyph` animates a working row through the *same*
   `heartbeatGlyph`, so the character beside a name and the one on its working line are one
   character at any moment. A static `◐` could not tell a session that is thinking from one that
   is wedged, which is the question a column of fifteen to thirty exists to answer.
   **Still open:** the line carries the turn's age alone where Claude also shows `↓ 59.5k tokens`
   and the effort. Tokens now decode (`core.SessionFacts`) but are spent on the status bar's
   context percentage rather than here, and **effort is on no frame Wake receives at all** — Wake
   does not pass `--effort`, so there is nothing to read.
3. **~~The conversation pane is 1:1 Claude Code, in its UX and its colours~~ MOSTLY BUILT**
   (`feat/claude-code-fidelity`) — everything but the composer, which is Wake's. The palette is no
   longer an approximation: the six theme objects are captured by hand in a fixture and
   `internal/ui/palette_test.go` holds `theme.go` to it, so a
   drifted colour fails with the value it should have been. Every colour differed; `Accent` was
   Claude's *subagent* orange rather than its accent. Diffs changed model rather than shade —
   full-width bands with word-level emphasis, which is what the four diff colours are for.
   **Still open:** task lists, thinking blocks and the spacing between turns were never measured
   against a side-by-side, and the group chat has the palette but not the treatment.

---

## OUTSTANDING BUGS at the close of `phase-1/core-daemon-dm` (park/wake), 2026-08-11

**Read this before trusting park/wake in front of real work.** It is the consolidated,
ranked list from the whole-branch review plus the eight per-task reports, re-ranked at the branch's
final commit and **written to the state after the close-out fixes**, so the two items the review
ranked 2nd and 3rd (`⌃Q`'s count, the MCP filter) are gone from it rather than listed as open.

The branch's own verdict is **sound, should merge**. Nothing below is Critical, nothing risks
data, and the one property that could destroy a transcript silently — two live processes on one
session id — is closed on every path and survived no mutation the review could construct. What is
below is what an operator can still be told wrongly, and what CI still does not prove.

**Ranked by what it blocks, not by where it was found.** If only two things are done: **1 and 2**,
and they are one piece of work.

| # | What | Severity | Blocks |
|---|---|---|---|
| 1 | Nothing shows a woken session answering **from the conversation it parked with** | Important | the central product claim |
| 2 | Neither soak lane parks or wakes | Important | confidence at 15–30 sessions |
| 3 | ~~`⌃C`/`⌃Q` report success on the keypress, before any write is confirmed~~ **CLOSED 2026-08-12** | Important | honest reporting of a failed park |
| 4 | A fresh spawn admitted after `takeAgents` starts a process `shutdown` never sees | Important | a clean `wake stop`/`⌃Q` under concurrent load |
| 5 | `⌃Q` may never reach the program on a real terminal (`IXON`/XON) | **Unknown — gates the feature** | items 3 and everything else on that key |
| 6 | `FleetOnDisk` is unbounded by its own timeout, and bare `wake` made it the front door | Important | the first thing an operator runs, on the machine already in trouble |
| 7 | Rung 7 is recorded; the guard it was found on is narrowed but still **underived** | Minor | it will expire again, the same way |
| 8 | The legend truncates mid-entry at every ordinary width | Minor | cosmetic; the dangerous version is closed |
| 9 | A woken conversation opens with no account of itself | Minor | the sentence exists one file away |
| 10 | No test reads a park book written by an **older build** | Minor | schema evolution |
| 11 | `s.agents` grows without bound for parked entries | Minor | narrowed three times, closed never |
| 12 | `gofmt` is checked by neither `make lint` nor CI | Minor | it is checked only by whoever remembers |
| 13 | `golangci-lint` walks `.worktrees/`, which is gitignored | Minor | a future worktree's files linted as this branch's |
| 14 | `/add-<agent-name>` cannot route as spelled | Minor | the next slash command `goals.md` names |
| 15 | Two spellings of one UUID are two sessions, and the park book made that durable | Minor | pre-existing; the restart is what is new |
| 16 | `"parking 1 agents"` — the notice row does not pluralise | Trivial | one sentence, read once per quit |

### 1. Nothing shows a woken session answering *from the conversation it parked with*

**Important. This is the single highest-value test still missing, and item 2 is the same work.**

Park/wake's entire promise is that context survives. The chain test now shows a woken session is a
live process that reads its stdin and answers — `@name zebrafish` after a full daemon restart
produces `agent heard: zebrafish`, which only the far side can produce. **That half is closed.**
What is not shown is **continuity**: that the process `--resume` started is holding the
conversation it parked with. Every test in the tree asserts identity (id, name, label, dir) and
liveness; none asserts memory.

The reason is structural rather than an oversight: the fake `claude` keeps no transcript, so there
is nothing for a resume to be wrong about.

**Fix:** a fake `claude` that appends turns to a file keyed by session id and, on `--resume <id>`,
replays them — then park mid-conversation, wake, and ask it something only the earlier turns can
answer. Recorded since park/wake Task 3, which asked for it in `docs/live-testing.md` *"before
anyone trusts a wake in front of a user"*; that line is still the only coverage.

### 2. Neither soak lane parks or wakes

**Important, and it needs item 1's fake, which is why they should be done together.**

`FrameWake` is exercised by unit and integration tests and by **nothing under churn**. The daemon
lane churns spawn/stop lifecycles through a real socket and never parks, so `replaceParked`'s
refusal, `withdraw`'s wake arm, `admitRefusal`'s wake sentence and the **row-before-process
ordering** — the fix Task 7 shipped for the only hazard that branches a transcript silently — have
no soak coverage at all. `FrameParkAll`/`quitPark` is unexercised in the lane whose whole subject is
shutdown.

The lane's `children before 1, after 1` detector is exactly what would catch item 4's leaked
`claude`, and this is why it does not: the path this branch built is the one path the lane does not
walk. Raised by Tasks 3, 5 and 7 independently; Task 5's review notes the one-frame version *"does
not work"*, because by the time the verb is sent the fleet is already parked — a real phase needs a
live fleet at shutdown and inverts the lane's closing assertion.

### 3. ~~`⌃C` and `⌃Q` report success on the keypress, before any write is confirmed~~ **CLOSED 2026-08-12**

**Important, pre-existing, raised as M12 by Task 7's review and left.** Both halves are now fixed;
the entry is kept rather than deleted, and the third clause below is **still open** and re-ranked.

`park` says *"parking @alex — /resume alex brings it back"* before the frame lands; if the daemon
refuses, the refusal arrives afterwards and contradicts it. **`⌃Q`'s version is worse and is why
this is still here:** the window is already closing, so a refused write's error frame has **nowhere
to land at all**. The hedge in `parkingFleet` covers the grace, not the write.

Related and separate: a park the daemon could not *write down* (`parked.add` failing) reaches only
the daemon log, while every client is still attached — Task 5's m1, addressed to the ⌃Q task and
not taken. Whoever gives the notice row a pending state should take all three.

**`⌃C` closed 2026-08-11** (`015cd1c`) — the press names the ask, `parkArrived` makes the promise on
the first report that says parked. **`⌃Q` closed 2026-08-12**, branch `fix/park-confirm`; the
argument, the measurement and what is still owed are under the dated heading at the end of this
file. **The `parked.add` clause is untouched and is now its own item** — see there.

### 4. ~~A fresh spawn admitted after `takeAgents` starts a process `shutdown` never sees~~ **CLOSED 2026-08-20**

**Closed on branch `fix/daemon-review`, by the prescribed fix:** `takeAgents` sets `taken`
under `s.mu` in the same locked step as the snapshot, and `register` and `replaceParked` refuse
while it is set. `TestAdmissionIsRefusedOnceTheFleetIsTaken` drives launch with the fleet taken and
was red before. One residual, found by the adversarial review of that branch: an `unparkRecord`
raced by shutdown now loses its park book entry - the record is removed before launch, the taken
door refuses, and `bookParked` books only snapshotted agents - so the raced wake loses the *offer*
across the restart (the transcript itself is still on claude's disk, and `wake import` reaches it).
That is `unparkRecord`'s own documented trade for every failed launch, strictly better than the
leaked process it replaces, and it is written here rather than fixed because putting the record
back is a decision about what a book entry means. The history below is kept, struck where the
close made it false.

~~**Important. Half-closed; the original sub-case is untouched.**~~

Task 7's review found and closed the *second* sub-case this branch created — a stop consumed on an
unstarted session which then exec'd anyway, which under `⌃Q` would have dropped that session from
the park book, the one outcome park exists to prevent. ~~The original is unchanged: `stopping` and
`register` are not one step, so a spawn landing between them leaks a `claude` past the grace, the
kill and the roster clear.~~

~~Not attempted deliberately — it is a shutdown-path change with `beginQuit`, `waitForAgents` and
every restart test in its blast radius.~~ **The prescribed fix** — a flag written under `s.mu` in
`takeAgents`, read by `admit` — **is what closed it**, above.

### 5. `⌃Q` may never reach the program on a real terminal

**Unknown, and it gates the feature.** `docs/live-testing.md` item 1.

`IXON` eats `⌃Q` as XON when flow control is on. Raw mode clears it, and that half is now settled by
reading rather than assumed — bubbletea's `initInput` calls `MakeRaw` from
`github.com/charmbracelet/x/term`, which clears `IXON` and `ISIG` — and `keyprobe_test.go` proves
bubbletea names `\x11` as `ctrl+q`. **What is unverified is everything between the keystroke and
Wake that is not the driver**: tmux, ssh, a terminal that ignores raw mode. `go test` has no tty.

If something eats it, the key moves to `⌃X`/`⌃Y` — which is an owner decision, and spends one of the
**two** unshadowed keys left, one of which deferred I7 is already owed.

### 6. `FleetOnDisk` is unbounded by its own timeout, and bare `wake` made it the front door

**CLOSED 2026-08-24 — PR #100 (`fix/fleetondisk-deadline`).** One `fleetProbeBudget` deadline over the whole roster loop, threaded through `alive(ctx)`/`verifyAgent(ctx,…)`, so a wedged `ps` can no longer cost N×probeTimeout serially. Follow-up **CLOSED 2026-08-25 — PR #110** (`fix/fleetondisk-fail-closed`): `FleetOnDisk` sets `Status.ProbeIncomplete` on a genuine deadline-break (never on a complete sweep), and `wake stop`'s two "the fleet is down" exits refuse to claim it when the sweep could not finish.

**Important.** Raised by Task 8, which is what changed its severity.

Worst case is roughly 3s *plus* a serial liveness check per roster record. The exposure is not new;
what is new is **who notices**. It was a third caller behind two rarely-hit ones, and bare `wake` is
now the first thing an operator types — on a machine whose daemon is gone, which is the machine that
is already in trouble. **Fix named:** one deadline over the loop, or `idsInUse`'s
one-`ps`-for-the-whole-question shape applied here.

### 7. Rung 7 is recorded; the guard it was found on is ~~narrowed but still underived~~ **half paid**

**Minor.** The **shell-verb half is closed** as of 2026-08-11 (Phase 2 Task 15).

`wake attach`'s refusal no longer forbids the literal `wake resume`. It holds every `wake <verb>`
the sentence names to the verbs `cmd/wake` actually dispatches, read off this package's own `cmd*`
constants — so a shell resume verb is free to be named the day it ships, and a renamed verb fails
here rather than becoming advice that produces "unknown command". The same check holds
`ui.NoAddressee`, which is where it was needed: `internal/ui` names a shell verb it can neither call
nor see. Mutation-verified by renaming `cmdFork`.

**What is still owed** is the *in-TUI* half: the refusal also names `/resume`, and that claim is
still a string rather than a read of `ui.commands`. It is smaller than it was — the sentence now
fails if the shell half goes wrong — and `ui.commands` is not importable from a `cmd/wake` test
without exporting it, which is the trade whoever takes it has to make.

The audit that finds the rest of this class is in `decisions.md` and is cheap: when a task ships a
capability, grep the test tree for assertions that it does not exist. **Run at the close of Task
15 it produced two hits**: this one, and `core.TestWithNoDefaultAddresseeNothingIsAddressed`
(*"the manager is not built"*), which that task falsified and rewrote. Everything else the grep
matched was about an input rather than about the build.

### 8. The legend truncates mid-entry at every ordinary width

**Minor, deferred, and the controller's finding said leaving *both* halves was not acceptable.**

Measured at 80 columns the hint line ends `… ⇥ next chat   ⇧⇥ next blocked   ⌃D` — a bare glyph
advertising nothing. The **dangerous** half is closed: `⌃Q`'s label was reordered to `quit & park
all` so no truncation can make it read as `⌃C park`, and
`TestNoWidthCutsALegendEntryIntoADifferentKeysLabel` holds that as a class at every width. The
ragged half is not. **Cutting at an entry boundary is the real fix and closes the whole class.**

Worth pairing with the structural observation Task 7's review left standing and no artefact states:
with a DM open, the last six entries (`⌃W ⌃G ⌃R ⇞⇟ ⌃F ⌃Q`) are invisible on every terminal up to
~300 columns.

### 9. A woken conversation opens with no account of itself

**Minor.** `wake attach` says *"What it said before now is not here — claude keeps the transcript,
Wake does not"*. `/resume` says *"bringing @alex back…"* and then the DM is empty. Same surprise,
same sentence needed, and the sentence already exists one file away.

### 10. No test reads a park book written by an *older build*

**Minor — smaller than it was.** Pinned from both directions now: `openroom_test.go`'s
`writeParkBook` hand-writes the JSON and has a real daemon **in another process** read it back, and
the chain test decodes the daemon's own output into `map[string]any` rather than into the writer's
struct, so a renamed field is visible. What neither covers is a **schema change** — a book written
before a field existed. The machinery is right (`loadParkBook` treats a malformed book as empty and
says so; a record with no `dir` is refused a wake with a sentence), so this is untested rather than
absent. One table test over hand-written historical shapes closes it. Task 4 notes the honest
version: the first real instance exists the day a second version ships.

### 11. `s.agents` grows without bound for parked entries

**Minor. Narrowed by Tasks 2, 3 and 4 and closed by none of them.** A session parked and never woken
stays in the map, because `holds` reads the map rather than the file and that refusal is what stops
a respawn under a parked id. Restore made it visible for the first time: a hundred book entries is a
hundred rows in every status reply. Spec §6's automatic parker under a live cap is what turns it
from bookkeeping into a question. Whoever decides the park book's lifetime decides this too.

### 12–13. Two lint gaps, both cheap

**`gofmt` is checked by neither `make lint` nor CI.** `.golangci.yml` is `default: none` with no
gofmt-family linter enabled, and CI runs only `make` targets. Raised by Task 2's review, confirmed
still open by Task 3, still open now — the tree is clean because people run it by hand.

**`golangci-lint` walks `.worktrees/`**, which is gitignored and normally empty. The review's run
emitted a warning about a file under `.worktrees/p8/` that no longer exists; harmless as a stale
cache entry, but lint reaching outside the tracked tree means a future worktree's files would be
linted as though they were this branch's. One `exclude-dirs` entry.

### 14. `/add-<agent-name>` cannot route as spelled

**Minor, and it is a design question rather than a bug.** `goals.md` §3 names `/new agent in <dir>`
and `/add-<agent-name>` as the reason to build a slash **layer** rather than one command. The router
keys on the exact bare word, and the two shapes that would route a prefix are what
`slashguard_test.go` makes a build failure — deliberately. So the choice is stated and not made:
change the syntax to `/add <name>`, which changes what `goals.md` quotes, or reopen the guard. Note
claude ships `/add-dir`, so a prefix rule would shadow a real built-in.

### 15. Two spellings of one UUID are two sessions

**Minor, pre-existing; the park book is what made it durable.** `mintedByWake` parses with
`uuid.Parse` while `s.agents`, `holds` and the park book key on the raw string, so an id differing
only in case or hyphenation is one session to one and two to the other. Nothing produces such an id
today — Wake mints them all — and what changed is that the collision now survives a restart.

### 16. `"parking 1 agents"`

**Trivial**, and listed only because it is in the sentence item 3 is about. `cmd/wake`'s exit line
pluralises through `agents(n)`; `internal/ui`'s notice row uses a bare `%d agents`. The fix is a
shared helper rather than a second one, since CLAUDE.md forbids parallel implementations — which is
why it was not taken as a drive-by.

### Not a bug, but write it down: the park book is written *after* the state flips

Found while closing out the chain test, and recorded because the next person to read
`completePark` will wonder. `markParked()` runs **first** — it is what makes `agent.stateLocked`
answer `parked` — then the roster file is rewritten, then the book entry is added. Both of those are
a create-temp/write/chmod/rename each, so **`daemon.Status` reports a session parked a whole atomic
file rewrite before the record exists.**

No live path depends on the other order: while a daemon is alive nothing consults the book, and
`shutdown` writes it before `closeClients`, with `bookParked` covering any session whose
`completePark` had not landed. It is stated here because a *test* depended on it and was wrong —
see the chain test's `awaitParkBook`.

**The one thing worth a second look** is the signal path, and it is narrow enough that it is
recorded as a question rather than as a finding: `quitNone` deliberately calls neither `bookParked`
nor `clear`, so a session parked by `⌃C` relies entirely on `completePark`'s write. A SIGTERM
landing inside that window would depend on `shutdown`'s bounded `s.wg` wait to let the write finish.
Probably safe, unverified, and cheap to check.

---

## From the cross-cutting adversarial review (7/11 merged)

The first review to look at the whole branch at once. Everything here was invisible to per-task
review by construction — each task was locally correct and the defect lives between them.

### Critical

**~~C1. `new_conversation_id` is not the new session id, and four artifacts say it is.~~ FIXED** (`194f450`), and the plan annotated 2026-08-09.
`protocol.go` puts it in `Event.Text` for `KindSessionReset`; `event.go` documents it as "the id
replacing it"; `fixtures_test.go` asserts it with the failure message *"want the new session
id"*; the plan repeats it. Verified against the corpus myself:

```
slash-commands.jsonl:31  conversation_reset  session_id=fc32ab1e  new_conversation_id=b3144871
                    :32  hook_started        session_id=6524c398   <- the actual successor
                    :36  init                session_id=6524c398
```

`b3144871` appears on **exactly one line in the whole corpus and never again.** §9 of the
findings note names this as a trap in so many words. The test cannot catch it: it checks only
`Text != ""` and `Text != ev.SessionID`, both of which the wrong value satisfies — **the
assertion passes while its own failure message teaches the falsehood.**
*Cost:* a pool built on `Event.Text` re-keys to a UUID that never existed, `--resume` targets
nothing, and the real transcript is orphaned under an id Wake discarded. Nothing consumes it
yet, which is exactly why it must be fixed before `registry.go` and `pool.go` land.
*Fixed as prescribed:* `SessionID` is read off every event and a change is the re-key;
`new_conversation_id` is dropped in the airlock rather than carried up under a name that invites
the mistake. The plan carried the falsehood in three further unannotated places — including the
deleted `NewConversationID` field and the vacuous assertion itself — until the final review found
them; all now behind a supersession note.

**C2. `DM.Append` is quadratic** — **fixed**, `57aace5`. The transcript is kept as lines and
appended to, `bubbles/viewport` is out of the DM (`SetContent` takes the whole content as one
string, so it cannot be fed incrementally), and both sequences a DM shares with its successors
are chunked so the mandatory immutable copy is bounded by a chunk rather than by the history.
Under `-race` at 4,000 events: one append 8.789 ms → 0.242 ms, of which 0.229 ms is now glamour
rendering the new block; reaching 4,000 events 17.46 s → 0.97 s; per-append cost flat from
1,000 to 40,000 events. Guarded by `dm_transcript_test.go`, which was run against the pre-fix
tree and fails there at 5.3× on time and 6.1× on allocation.
Numbers, test design and the two judgement calls:
`.superpowers/sdd/2026-08-08-wake-core-daemon-dm/dm-perf-report.md`.

### Important

**I1. Thirteen Claude wire literals live above the airlock**, in `internal/ui/dm_blocks.go`
(`old_string`, `new_string`, `compact_boundary`, `permission_denied`, `<local-command-stdout>`,
`allowed`) and `internal/render/tool.go` (eight tool→argument mappings). They are load-bearing
dispatch, not incidental. The airlock leaks by design at three points: `Event.Text` carries the
raw subtype for `KindSystem` and the raw status for `KindRateLimit`, and `ToolCall.Input` is
Claude's tool-input object verbatim. **The rule is "one file is the entire cost of staying
Codex-ready"; it is now three files across three packages.** Nothing enforces it — no lint rule,
no test.
**RULED, `docs/notes/decisions.md`:** both, split by kind. `ToolCall.Display` and
`ToolCall.Diff{Old,New}` move the tool vocabulary and the Edit keys behind the airlock, and the
decoder strips the local-stdout markers — those three are portable-with-effort and a port should
rewrite one file, as the rule claims. The two `KindSystem`/`KindRateLimit` subtype literals
**stay**, and the rule is amended to enumerate them: the subtype set is open, an unrecognized one
must still arrive as a system event rather than degrade to `KindUnknown`, so an `EventKind` per
subtype is the worse trade.
*Sequenced deliberately:* implement **after Task 10 merges**, bundled with the subagent
attribution fields (I6), because those land in the same three files and doing I1 first would make
the second pass rewrite it. `dm_blocks.go` is in Task 10's blast radius until then.

**I2. `render.Markdown` has no width bound at all.** `WithWordWrap` wraps at break
opportunities and does nothing without them. 600 display-cells of space-free Japanese at width
80 returns **602 cells**; the same string through `ToolResult` returns 80. Same package, one
bounds and one doesn't, and only the bounded ones say so. `TestMarkdownWrapsWithinRequestedWidth`
passes only because its fixture is Latin prose with spaces.
*Cost now:* the DM hard-cuts with no ellipsis and no horizontal scroll — **353 of 600 cells
rendered, 41% of the response silently gone**, in the view §8 calls "literally Claude Code."
*Cost later:* the group chat is a joined pane, and `JoinHorizontal` sizes on the widest line, so
one 602-column line shoves every neighbouring column out of the grid.

**~~I3. A permission request leaves `core` with no session attribution.~~ FIXED** (`88f2676`).
`emit` now stamps through `Session.attribute`, and a frame that names its own session keeps that
name — the ordering matters because `/clear` mints a new id mid-process and the frame is the
authority once it has one. Mutation-tested both ways. The `rpc` assertion that pinned the defect
(`TestPermissionRequestCarriesOnlyItsRequestID`) is retired; its replacement asserts both
correlators survive and is **labelled a guard rather than evidence**, because `rpc` copies an
`Event` through `encoding/json` and reads no field, so it passes against the unfixed core too.
*Residual, and it belongs to whoever owns re-keying:* an ask arriving **after** a `/clear` is
still stamped with the spawn id, which is the only id `session.go` is told about. It routes to
the right process and is the wrong id the moment something above re-keys the session to its
successor. Written down in `attribute()`'s comment.

**~~I4. `make soak` is a green no-op.~~ FIXED** (`d396aab`). `internal/core/soak_test.go`, behind
the `soak` tag, in two phases: churn at 20 concurrent for lifecycle leaks, then one long session
at a time — alone — for growth. Duration is `-soak.duration`, defaulting to 30s so it is
runnable in a review; `SOAK_DURATION=1h make soak` is the advertised run. **"Every detector is
mutation-verified" was wrong** — the final review found the *allocation-growth* detector passes
window `0` to a helper that returns `(0, false)` for `q == 0`, so `maxAllocGrowth` at `1e-9`
leaves `TestSoak` green while the same treatment of the two time ratios turns it red. It is the
**more sensitive** of the two signals (6.1× on allocation against 5.3× on time), and the symptom
prints every run: quarter 1 has no `allocs/event` figure. Fix: start from window 1. The other
detectors are (attribution, a per-session goroutine leak, and a `DM.Append`-shaped
quadratic). The Makefile target now fails if `-tags=soak` lists no `TestSoak`, so it cannot
become a no-op again.
*Worth knowing:* the two phases exist because one lane could not do both. An earlier draft had
only the churn lane and **passed the quadratic mutation untouched** — churn sessions see ~85
events and start over, so growth-with-history never gets large enough to measure. Run alongside
the churn lane the same quadratic moved the number 1.23x, buried under scheduler noise; run
alone it is 4.2x. A growth check needs a quiet machine and a long session, and neither is
optional.

**~~I5. Backpressure terminates at `claude`'s stdout, not at the daemon.~~ CLOSED by Task 6**
(verified at `ecd0fbf`, not taken on report). The finding's own prescription — *drop-and-mark for
a lagging client, not a bound* — is what got built, in both halves:

- `client.enqueue` **never blocks**: a full 512-deep queue increments `dropped` and returns. Its
  comment names the exact reason the old shape was fatal — *"it is called from a session's
  fan-out goroutine, which is the goroutine draining that agent's stdout. Anything that parks
  here parks the agent."*
- The drop is **marked, not silent**: `flush` swaps the counter and emits a gap `FrameError`
  *before* the next frame, so a client learns it lost something before rendering what follows
  the hole rather than after.
- A dedicated writer goroutine per client keeps `rpc.writeMu`'s process-wide serialization off
  the fan-out path. `writeMu` still exists as a correctness backstop for callers sharing an
  `io.Writer` with no identity to key on; the daemon routes around it rather than removing it.

*Residual, small and known:* a dropped frame is invisible to anything that reads the stream and
skips `FrameError` — which is how two daemon tests passed with the harm printed above the PASS.
Tracked as a round-3 minor.

**I6. `--forward-subagent-text` is passed and subagent frames arrive unattributed. RECORDED
(`47d3815`), still unfixed — and the recording made it bigger, not smaller.**

The original remedy — "either drop the flag or add the field, one struct field each side" — was
a guess: `parent_tool_use_id` was `null` on all 67 of its corpus occurrences, because no
recording had ever dispatched a subagent. So it got a spike instead of an implementation.
Seven fixtures, 383 lines, nine dispatches, `docs/superpowers/notes/2026-08-09-subagent-findings.md`.

*What is now known:* forwarded text arrives as **ordinary `assistant`/`user` frames** — no new
type, no new subtype — carrying `parent_tool_use_id` + `subagent_type` + `task_description`, and
the id **equals the parent's `tool_use` block id**, which makes it the attribution mechanism.
The tool is named **`Agent`** on the wire, not `Task`. Parent and subagent **share one
`session_id`**, so the session id cannot separate them. They **interleave live**, and an async
subagent streams past its own `result` and past stdin closing — so "the turn ended" does not mean
"the subagent finished".

*What that costs today, all of it silent:* `Event` has **no field** for `parent_tool_use_id`,
`subagent_type` or `task_description`, so above the airlock **a subagent's speech is the agent's
speech**. 53 `system/task_started|task_progress|task_updated|task_notification` frames collapse
to `KindSystem` with every field dropped. And `can_use_tool` carries an **`agent_id`** the
decoder drops, so a subagent's permission ask is indistinguishable from its parent's — it still
*answers* correctly, because `request_id` is the correlator, but the operator cannot see who is
asking. Everything decodes with zero `KindUnknown`, which is the problem as much as the good
news: nothing failed, so nothing announced what was lost. §9 of the note is the full table.

*Keep passing the flag* — every fixture Wake's own argv produced has the prose in it. But the
reading that the flag is what makes subagents *visible* is dead: a control recorded without it
still carries 13 non-null `parent_tool_use_id` lines, so dropping it would leave 26 `tool_use`
and 26 `tool_result` events arriving unattributed rather than quieting anything. §8 states the
A/B as narrowly as one recording per cell allows.

*Also:* `internal/render/tool.go`'s eight tool→argument mappings include neither `Agent` nor
`Task`, so an `Agent` call renders through the fallback with no subagent type or description.
Small, and it is the visible half of the same gap.

**~~I7. The composer shows a permission mode it cannot set and never reflects the real one.~~
FIXED** (2026-08-12). Shift+Tab cycled a label; there was no path from `Mode()` to a process.
The blocker was never the wiring — it was that **nothing recorded said a running session's mode
could be changed at all**, and this project does not design around unrecorded behaviour.
`docs/superpowers/notes/2026-08-12-permission-mode-findings.md` settled it with six fixtures, and
`docs/superpowers/specs/2026-08-12-permission-modes.md` is the design built from them.

What landed: `core.EncodeSetMode` / `Session.SetMode` write a `set_permission_mode` control
request; `rpc.FrameMode` carries it (a kind, not a field — an empty mode has two readings and
both are wrong); the daemon routes it through the agent's input queue; and `⇧⇥` cycles
`plan` / `auto` / `default`. Next-blocked moved to `⌃X` (`docs/goals.md` §5).

**The label moves on the receipt, never on the keystroke** — the defect that got the old
indicator deleted, rebuilt on a mechanism that works. `manual` is accepted by the CLI and
normalizes to `default`, so the asked mode and the real one genuinely differ; the cycle spells
that position the way the receipt does, and the wiring reads the receipt regardless. Every
turn's `init` is the second observable and corrects a stale belief — which is what a mode
changed by `updatedPermissions` (the `permission_suggestions` lead this entry recorded, findings
§5) produces, since that path emits **no receipt at all**.

Two things were ruled out rather than left open. A mode does not survive a park and wake
(findings §8): the ruling is **say it, not persist it** — the park book gains no field and the
wake notice names the mode the session came back in. And the manager may **not** reach the verb:
`cmd/wake/mcpguard_test.go` carries the argument, which is that loosening a fleet's modes is the
escalation that list exists to prevent and is not undoable by looking at the room.

**~~I8. There is no frame kind for answering a permission request.~~ FIXED** (`bf48c19`).
`internal/rpc` gains `FrameAllow` / `FrameDeny` plus `RequestID`, `UpdatedInput` (allow only)
and `Reason` (deny only). Two kinds rather than one kind with a `behavior` field, because a
frame arriving with an empty behavior would need a default and every default is wrong — allow
grants a call nobody approved, deny refuses one somebody did; an unrecognized *kind* just leaves
the agent blocked, which is the safe end. `omitempty` on `UpdatedInput` collapses `nil` and `{}`
to an absent key, so the transport cannot express the `{}` shape §6 records as never tested.
**Daemon side is still Task 6** — this is vocabulary, not wiring, and Task 6's interface list
should gain `FrameAllow`/`FrameDeny` alongside `FrameStop`/`FrameKill`/`FrameQuit`.

### New, from fixing I2 — bounding converts width overflow into height

`render.Markdown` now hard-wraps rather than overflowing (I2 **done**, and it retires the
`render_test.go` `len([]rune(...))` test-quality item too). But the content has to go somewhere:
a 600-cell CJK message that was 2 over-wide lines becomes **9 / 17 / 32 lines** at width
80 / 40 / 20.

The DM's viewport absorbs that. **A fixed-height joined pane will not** — and nothing caps a
markdown block's height the way `capLines` caps a diff. That lands on the group chat and the
bounded grid in Phase 2, where several panes share a fixed height and one long message can push
everything else off screen.

Two smaller residuals from the same fix, both confined to lines glamour could not lay out:
continuation lines sit flush left rather than under glamour's 2-column margin, and a long
heading's background bleeds across the break. `Markdown` bounds to `max(width, 20)`, matching
the DM's own floor, so sub-20-column panes remain the caller's problem.

### Verified clean — things later tasks can rely on

`render` does not import `core`. `rpc` never interprets an `Event` — it reads no field. `Raw`
never crosses the socket. The airlock holds *inside* `internal/core` across all five files. The
DM's width contract holds exactly at `max(w,8)` against unbreakable tokens, fenced code, 600
cells of Japanese, and a 39-char MCP tool name. `ToolCall`/`ToolResult`/`Diff` bound correctly
including wide runes. `render.Markdown` is race-clean under 20×300 concurrent renders. And a
cross-package comment in `session.go` claiming the DM renders `KindUnknown` as `""` is *true*.

---

## Blocks Phase 1 from being done

**~~1. A deliberate interrupt will report as a crash — and now nothing stands in front of it.~~
FIXED** (`0781b73`, branch `interrupt`). Both halves in one commit, which is what the entry
demanded.

*Half one, the gap.* `core.Session.Interrupt()` mints the `request_id` with `google/uuid` as it
writes and returns it; `rpc.FrameInterrupt` carries it across the socket; `dispatch` routes it
through the agent's input queue beside send/allow/deny/stop (stdin writes have to be serialized,
and it is emphatically **not** the wedged-agent verb — an agent that stopped reading stdin cannot
be interrupted by anyone, and kill is still what an operator reaches for); `App.key` binds `⎋`
and the legend gains it.

*Half two.* `session.go` records that it sent an interrupt and `ending.go`'s `interruptedExit`
suppresses exactly the recorded shape — exit status 1, **nothing at all** on stderr, and an
interrupt that actually reached the pipe. Said once through the log sink, never swallowed.

*Residual, and it is the one thing left from the findings note's §10* (narrowed in `5dc1f59`
after review — the first framing of this was too generous to itself). The exit code follows the
*last turn's* `is_error`, and `is_error` is **not** exclusive to interrupts, so a flag that never
cleared would forgive every later silent failure of any session that had ever been interrupted —
at 15–30 sessions, most of them. `Session.interrupted` is therefore **cleared by a successful
`Send`**: once Wake asks for another turn the aborted one is no longer last. Corpus-backed with no
decoding — §6's five runs that exited 0 with a completed last turn include two interrupted earlier
in their lives. A *failed* send does not clear (no turn started), and neither does an allow or a
deny, which resume the turn already in flight; that last one survived its first mutation and now
has a guard.

What is still unmodelled is one shape: a turn the **agent** started on its own (`--brief` allows
it) failing after an interrupt Wake never followed with a send. Closing it needs an aborted
`KindTurnEnd` told from a quiet one — `terminal_reason`, which nothing decodes. Whoever decodes it
should **replace** the interrupted-flag pairing rather than add to it, and delete
`Session.interrupted`.

*Decided and worth knowing:* **`cancel_queued` is not sent and the rpc frame has no field for
it.** `EncodeUserMessage` stamps no top-level uuid, so the CLI emits no `command_lifecycle` for
anything Wake sends and the receipt's `cancelled` array could not name what it had destroyed —
while `App.submit` has already echoed that message into the transcript as sent. Without the flag
a queued message still runs (`interrupt-queued-survives.jsonl`), which is the end of the trade
that loses nothing. **Stamping outgoing messages with a uuid is what re-opens the choice**, and
it is cheap: `KindMessageState` already decodes the frames it would produce, so the operator
would get "queued / started / cancelled" for their own messages as a bonus.

*Found by review of that commit and fixed in `5dc1f59`, because it is the same failure the legend
rule exists for, one level up:* the guard that was supposed to hold the legend honest iterated a
**hand-written five-entry map**, so it pinned the pairs that existed and enforced nothing about a
sixth — `⇥ next agent` with no binding passed, and `case tea.KeyTab` with no glyph passed — while
`CLAUDE.md` claimed it held both directions. The legend is now `ui.legendEntries`, a slice of
(glyph, label) pairs, and the test reads that against the `tea.Key…` cases parsed out of
`App.key`'s switch, requiring a bijection. **The lesson generalises and this file is where it
belongs: three guards on this branch could not fail, and all three were hand-written lists
standing in for something derivable.** When a check's subject is a set the code already declares,
derive it.

*Also closed by the same commit, and both were in scope because this change is what makes them
constant rather than latent:*

- **The airlock review's I4** (`final-review-airlock.md`), *"`[Request interrupted by user]`
  renders under the `› you` label"*, declined twice as a distinct finding. It was "latent today
  only because nothing can send an interrupt" — that sentence expired with this commit. Both
  literals resolve in the airlock to `core.NoticeTurnInterrupted`, keyed on the **text**, because
  the frame's key set is identical to a genuine user turn's and the text is the only
  discriminator there is; the DM draws `⊘ turn interrupted`. Resolved on the user's side only, so
  an agent whose whole message is that sentence keeps it — a case no recording has and a
  hand-written frame covers.
- **`TestEveryFrameKindIsDistinct` cannot fail for a kind not in its hand-written map** (under
  "Test quality", below). This commit added exactly such a kind, which is what made it worth
  closing rather than noting again: `TestNoFrameKindIsMissingFromTheDistinctnessMap` parses
  `wire.go` and `lifecycle.go` for `Frame… = "…"` and requires each in the map.

**~~2. 305 lines of recorded ground truth sit outside the golden test.~~ FIXED** (`194f450`).
`protocol.go` gained cases for `command_lifecycle` and `control_response`, and all nine
interrupt fixtures are now flat in `testdata/stream/` and inside the zero-unknowns assertion.
Verified: no `testdata/stream/interrupt/` directory remains, and `fixtures_test.go:60` still
fails any line that decodes to `KindUnknown`. This retired Task 2's YAGNI call on
`control_response`, which was correct at the time (zero inbound occurrences in 231 lines) and
was superseded by 12 recorded ones.

**~~3. `internal/render`'s degradation paths violate the project's own rule.~~ FIXED**, and
already fixed when Phase 2 Task 10 came to claim it — verified against the tree rather than
re-done. `render.degraded` reports through `internal/notice` and its comment carries the whole
argument: the sink could not be `log`, because `render` runs in the **TUI** process and a draw
loop failing every frame across 30 sessions makes an unconditional log a flood; `notice` holds
one entry per distinct message with a count, and draws nothing itself, so `render` still knows
nothing about a UI. `render_test.go` asserts the report and its repeat count at 100 failures.
*This is the fourth entry in this file found already done*, which is the pattern the header
warns about: check the tree before acting on an item.

---

## Blocks dogfooding at 15–30 sessions

None of these are wrong. All are unmeasured at the scale the product claims.

- **One process-global mutex serializes rendering across every session** (`internal/render`).
  Correct; a cache miss blocks every other session. Never profiled under load.
- **Unbounded renderer cache.** Dragging a terminal resize caches a `TermRenderer` plus its
  parsed style config at every intermediate width, permanently.
- **A session that ends *cleanly* still leaks what it spawned.** **CLOSED 2026-08-24 — PR #103 (`fix/clean-exit-group-sweep`):** `retire`'s ordinary non-park branch now sweeps the group via `core.KillGroup(a.sess.Pgid())`; `completePark` is untouched so a parked session's children survive its wake. Follow-up (c) zombie-reap concurrent-`Wait` ordering **CLOSED 2026-08-25 — PR #112** (`fix/wedged-exit-selfdetect`): Wake owns the stdout pipe and `awaitExit` self-detects a leader that exited while a grandchild holds stdout, so a wedged session ends cleanly and `retire`'s sweep now reaches it too; (b) wedged-logger goroutine leak remains (the log-sink owner's, not a core group-kill). Both kill paths are failure
  paths. An agent that finishes normally after `npm run dev &` leaves the dev server behind.
  Policy belongs to the pool; the *mechanism* cannot — `cmd.Process.Pid` dies with `finish`, so
  it must be a pgid recorded on `Session` at spawn.
- **A wedged logger leaks one parked goroutine per affected session.** The session itself ends
  and frees its slot; the goroutine does not. Unbounded from inside for the same reason the
  original bug existed — a component cannot bound a sink it does not own. Task 6 owns the sink.
- **A DM's scrollback is unbounded for the life of the session.** Deliberately left there by
  C2's fix: dropping old events means dropping the lines they rendered to, or a width change
  re-renders a transcript missing its beginning — so a bound on `d.events` is a bound on
  *scrollback depth*, which is a product decision about a view §8 calls "literally Claude
  Code", not a restructure. Nothing is harmed today: `Raw` is nil over rpc, so what a DM pins
  is the text it is displaying. Two notes for whoever takes it. The structure now makes a bound
  cheap — dropping the oldest chunk of a `chunked` is a trim of the header slice, needing only
  a base offset so `slice` keeps indexing absolutely. And if a single-process path lands first,
  the memory pin specifically (`Raw`, 85.4% of the serialized stream) is closed by nil'ing
  `Event.Raw` as the DM stores it: the DM reads `Kind`, `Text`, `Tool` and `Echoed` and nothing
  else. That is a different change from a bound and should not be smuggled in as one.

---

## Assigned to a specific task

**PHASE 4, TOP ITEM — `--allowed-tools` for the manager. The blast radius is bounded on the fleet
and not on the machine.** Added 2026-08-12 by Phase 2 Task 15's review (C1), at the coordinator's
direction.

**The gap.** The manager is an ordinary `claude` session. Nothing in this tree passes
`--allowed-tools` or `--disallowed-tools`, and `--strict-mcp-config` bounds MCP **servers** rather
than built-ins — so it holds `Bash`, `Write`, `Edit`, `Task` and the rest **in addition to** Wake's
five tools, spawned `auto`. This repository's own recorded corpus is what makes that concrete rather
than inferred:

| Evidence | Where |
|---|---|
| an `auto` session's `init.tools` carries `Task`, `Bash`, `Edit`, `Write`, `WebFetch` | `testdata/stream/question-answer.jsonl` |
| an `auto` session ran `Bash` with **zero** `can_use_tool` lines in the whole recording | `testdata/stream/interrupt-mid-tool.jsonl` |
| the same, a second time | `testdata/stream/interrupt-double.jsonl` |
| the recordings where a write **did** ask were `permissionMode: "default"`, not `auto` | `permission.jsonl`, `interrupt-pending-basic.jsonl` |

So Task 14's escalation — injected text → the manager → `send_to_agent` → an agent in `auto`
executes — has a **one-hop version that skips the fleet entirely**: the manager runs
`Bash("wake stop")` itself, no ask. `wake` is on the PATH it inherited from the daemon, and the
absolute path of the binary is in `mcp.json`'s `command` field, a 0600 file the manager owns and can
read. `wake stop` is this project's one irreversible verb. `Write`/`Edit` are the quieter version:
the manager's working directory is wherever `wake manager` was typed, which is a real repository.

**The fix, and its shape is already argued.** `--allowed-tools` and `--disallowed-tools` both exist
at 2.1.228 (`<tools...>`, comma or space separated, `Bash(git *)`-style patterns accepted). The right
form is a **third element of the same one-literal append in `internal/core/argv.go`**, keyed on the
same `Config.MCPConfig != ""` emptiness test — so no value of that field can express
tools-without-restriction, exactly as none can express `--mcp-config` without `--strict-mcp-config`,
and `TestTheMCPFlagsAreEmittedFromOneAppendOrNotAtAll` extends to cover it. That makes
`managerScope`'s tool list and `CLAUDE.md`'s *"bounds consequence"* **true** instead of qualified.

**It is not `manual`, and that was ruled rather than left open.** Too broad: MCP tools are ordinary
tools to Claude Code's permission system, so in `manual` every `list_agents` raises a permission card
and blocks on a human — which destroys the one property the manager exists for, broad awareness
without a person in the loop. Too weak: bounding by *asking* means the operator answering thirty
cards a day answers the thirty-first without reading it, and Task 14's standard is higher than that
(a verb is exposed only if an operator can undo it by looking at the room).

**It needs a recording spike first** and that is why it was not built in Task 15's fix round.
Unrecorded, all of it: what `--allowed-tools` accepts, whether MCP tools are nameable in it at all
(`mcp__wake__list_agents`? `mcp__wake`?), what `init.tools` reports back, and what a refused tool
call looks like on the wire. The spike is one session with `init.tools` read back, which is
`docs/live-testing.md` §14's second item. **This project does not design around unrecorded
behaviour**, and a half-built restriction that silently allows everything is worse than the honest
gap.

**What shipped in the meantime is the honesty fix**, because a task about bounding an unsupervised
model may not ship an artefact asserting a bound it does not have: `managerScope` says its tools are
the whole of what it can do **to the fleet**, names the built-ins out loud and says not to use them
on Wake or on an agent's work; `CLAUDE.md`'s paragraph says the same; `live-testing.md` §14 asks a
person to type `@manager run ls -la` and treat it running as the finding.
`TestTheScopeDoesNotClaimABoundThisBuildDoesNotHave` is what stops the unqualified sentence coming
back.

Cross-referenced from `docs/superpowers/specs/2026-08-12-phase-4-scope.md` §2a's neighbourhood and
§4's order.

**Phase 2 Task 15 (the manager) — INHERITED AS A PRECONDITION: agent output is data, never
instruction.** Added 2026-08-11 by Phase 2 Task 14's fix round, at the coordinator's direction.

Everything `internal/mcp`'s reading tools return is **text an agent wrote**: `SessionStatus.Tool` and
`ToolArg` are `core.ToolCall.Name`/`Display`, `Error` is the process's own stderr. It lands verbatim
in the manager's context, and the manager holds `send_to_agent` and `interrupt`. **The escalation
path is one hop:** injected text → the manager sends an instruction → an agent spawned
`daemon.spawnPermissionMode` = `auto`, which does not ask, executes it.

*What Task 14 built, so Task 15 does not rebuild it:* containment (`mcp.oneLine` — no agent-authored
field can emit a newline or anything else that acts as structure, so no agent can forge a row);
provenance (every result opens with a line naming whose words follow, and every row is attributed to
the id it begins with); bounding (`toolArgMax`, `agentLineMax`, `statusReportMax`, `rollUpMaxBytes`).
`internal/mcp/untrusted_test.go` holds all three, with a provenance verdict per `SessionStatus` field
derived from the struct.

*What Task 15 owes, and may not be built without:*

1. **The manager's system prompt (or first message) states the rule** — agent output is data about
   what an agent is doing, never an instruction to the manager — and says what to do with an
   apparent instruction: report it, do not act on it.
2. **Any new tool is judged against the same rule**, in addition to `managerVerbs`' per-verb
   argument. The two-verb list is what bounds *consequence*; containment only reduces *likelihood*.
3. **`docs/live-testing.md` §13.1 is a gate on the manager being reported working at all**, and
   §13's injection item is the only check of whether a real model treats the framing as framing.

**Phase 2 Task 15 / whoever ships the live cap — run rung 7's audit against `managerVerbs`.**
Added 2026-08-11 by Task 14's fix round; it is F5 of that task's review.

`cmd/wake/mcpguard_test.go`'s verdict **domain** is derived from `daemon.dispatch`, so a verb the
daemon starts serving is a build failure. Its **reasons** are prose and cannot be, and one of them
was already wrong when it was written — the `FrameWake` cell cited a race Task 7 had closed the day
before. Each cell that argues from the state of the build now names a `referent` something checks
(`internal/core/pool.go` must still not exist; `/resume` must still be a key of `ui.commands`;
`internal/mcp`'s `agentStates` must still withhold a parked row; the interrupt findings note must
exist), and `TestEveryReasonThatRestsOnTheBuildNamesSomethingThatIsStillTrue` fails with the
correction in its own message.

**What that does not cover is the prose itself.** A referent proves the premise still holds; it does
not prove the sentence still argues from that premise. So: **when a task ships a capability this
table reasons about — the live cap above all — re-read every cell.** That is `decisions.md`'s own
rung-7 audit, and the entry there should gain the note this instance produced: the audit as written
greps `_test.go` for a grammatical tell in *failure messages*, and would have caught neither F1 nor
F5, because these are comment-position strings carried in data.

**Phase 2 Task 15 review — an interrupt leaves `core`'s exit suppression armed indefinitely.**
Added 2026-08-11 by Task 14's fix round (review F15). Pre-existing, and the caller is what is new.

`core.Session.Interrupt` sets `interrupted = true` unconditionally, and only a successful `Send`
clears it (`noteTurnSent`), so `ending.interruptedExit` goes on suppressing *exit 1 with empty
stderr* until the session is next messaged. A manager that interrupts an agent and never messages it
again leaves that armed, and the session's next silent startup-style failure is recorded as an
ordinary ending. **`⎋` has always done this** — what changed is that `⎋` has a human who just
pressed it and can see the roster, and this caller is a model in a loop. Closing it properly needs
`terminal_reason`, which `2026-08-08-interrupt-findings.md` §10 already names as the open question
behind the narrow guard.

**~~Phase 2 Task 15 — the manager recognises itself by *name*, and a Role field is arriving in the
same phase.~~ RULED 2026-08-11 by Task 15: `rpc.SessionStatus` gains no Role, and the name stays.**
The entry is kept because the ruling went the other way from what it recommended.

`Frame.Role` shipped, and it carries a role and nothing else — the daemon reads it in exactly one
place, `spawnName`, to claim the one name `claim` refuses to everybody. Everything downstream is
still keyed on `core.ManagerName`: `mcp.liveSessions`, `ui.live`/`ui.service`, and
`daemon.managerConfig`, which is new.

**Why the "better discriminator" was not taken.** A restored park-book row carries a **name and no
role**. So a Role-keyed exclusion works for a live manager and **silently stops working across a
daemon restart** — `liveSessions` would start offering the manager to itself, which is the unbounded
`send_to_agent` loop the exclusion exists to prevent, arriving only after a `⌃Q` and a `wake` and
only on the surface with no human watching. The name is in the book, so the name keeps working.

**What closing it properly costs**, for whoever wants it: `Role` on `rpc.SessionStatus`, on
`daemon.agent`, on `parkedRecord` — which is the one file Wake reads back into live state, and whose
on-disk key set is asserted in **both** directions — plus a verdict in `mcp.tools_test.go`'s
`notInTheStatusReport` and in `ui.fleet_test.go`'s `notCarriedOntoAnAgent`. The schema change is the
work; the rest is bookkeeping. **Whoever adds the field owns the record.**

**Left open by the same task, and smaller: the manager is an ordinary row on every surface that
draws one.** It is in the roster, it has an attention rank, `⌃D` opens a DM on it and `⌃C` will park
it. Parking it is *recoverable* — `restoreParked` gives it its name back and `managerConfig` gives
it its tools back — so nothing is lost, but a manager sitting in the attention ranking between two
agents is a design question nobody has answered. It was deliberately not answered here: the settled
scope was routing (default addressee, broadcast exclusion), and the roster is a different surface
with a different argument.

> **Promoted 2026-08-15, and it is now the top item on this entry rather than "smaller".** The room
> seats a manager by default (`cmd/wake/ensuremanager.go`), so this stopped being a question about a
> session most fleets did not have and became one about **every fleet there is**. The evidence
> arrived as test churn and it is worth reading as a UX report, because each line is something an
> operator will meet:
>
> - **`↑` from the top agent lands on the manager.** `Roster.Move` wraps, so on the one-row roster a
>   fresh `wake` used to have, ↑ was a no-op; it is not now. Four pty tests pressed ↑ to *reveal* the
>   cursor and moved it instead — one of them onto the service, where the next key was `⌃C`.
> - **`⌃D` on "whoever ranks first" is a coin flip.** Equally idle agents are ranked stably, so the
>   top row is whatever order the daemon's map iterated in when it built the report, and the manager
>   is in that draw. Two history tests opened `@manager` instead of the agent they planted a
>   transcript for.
> - **The strip counts it**: a fleet of one agent reads `2 idle`.
> - **It takes a roster slot**, so a column too short for the fleet hides one more agent than before.
>
> None of that is wrong, and none of it was decided. The three answers on the table are: leave it
> (what ships today — it is how `⌃D` reaches the manager's conversation at all, and how anybody sees
> the service is running); rank it last unconditionally, since a service that never needs you does
> not belong in an attention order; or take it off the roster and give it a surface of its own, which
> costs the one route to its transcript unless something replaces it. **Whoever answers this owns the
> strip's count too** — the two are the same claim about whether the manager is part of "the fleet".

**Phase 3 or later — `--strict-mcp-config` for *ordinary* agents is unruled.** Added 2026-08-11 by
Phase 2 Task 15.

The manager gets `--mcp-config` and `--strict-mcp-config` as a pair, and the second is what stops it
inheriting every MCP server in the user's own configuration. **Every other agent Wake spawns still
inherits all of them**, and that is untouched rather than decided: it is what an agent started by
hand would have, it is almost certainly what an operator wants (an agent working in their repo with
their tools), and taking it away is a fleet-wide behavioural change with nobody having asked. Worth
a ruling the day somebody wants a fleet whose tool surface Wake controls — the flag is one literal
away, and the argument against is that a Wake agent that can do *less* than the same `claude` in the
same directory is a surprise nothing on screen explains.

**Phase 3 or later — a manager whose MCP server cannot start is indistinguishable from one whose
tools are empty.** Added 2026-08-11 by Phase 2 Task 15.

`managerConfig` refuses the launch if the config file cannot be **written**. It cannot check that
the file is *usable*: `wake mcp` is executed by claude, not by Wake, and its failure — a binary that
moved, a socket that went away, a protocol the client rejects — arrives as a manager whose
`tools/list` is empty and which then reports in prose that it cannot see the fleet. That is
`docs/live-testing.md` §13.1's gate stated from the other side, and it is the reason that item is a
gate rather than a checklist entry. **The cheap close, if it is ever worth it**, is a self-test at
`wake manager` time: run `wake mcp` against the same socket, write an `initialize` and a
`tools/list`, and refuse the spawn if five tools do not come back. It costs a process per manager
start and it would have caught every failure this paragraph lists.

**~~Phase 2 Task 15 (or later) — `spawn_agent` is in the spec's manager tool list and is not
built.~~ The *contradiction* is closed: spec §12 was amended 2026-08-12** with Task 14's reasoning,
after Task 15's review flagged it as a plan-mandated conflict (`CLAUDE.md` opens with "the spec wins
and this file gets fixed", and nobody had fixed either). §12 now names the five the build has and
records why `spawn_agent` is not among them; §17 carries the machine-bound gap.

**The work is unchanged and still owed**, and it is the live cap rather than the tool. Original
entry follows. Added 2026-08-11 by Phase 2 Task 14.

Spec §12 names four manager tools — `list_agents`, `agent_status`, `spawn_agent`, `send_to_agent` —
and the room plan's own file table names five, with `spawn_agent` replaced by `interrupt` and
`roll_up`. Task 14 shipped the plan's five and recorded the refusal per verb in
`cmd/wake/mcpguard_test.go`; `rpc.FrameSpawn` and `rpc.FrameFork` are refused there **for the same
reason and it is not "spawning is dangerous"**: it is that a spawn tool's failure mode is thirty
agents nobody asked for rather than one, and nothing in this build caps concurrency. Spec §6's
live-cap scheduler (`internal/core/pool.go`, **NOT BUILT**) is the prerequisite. Whoever adds the
tool owns the cap, and owes the fork tool's companion debt as well — see the `ParentID` entry below,
whose closing condition is exactly "the task that gives the manager a fork tool".

**Phase 2 Task 15 review — an acting tool confirms that the daemon *took* the verb, not that the
agent read it.** Added 2026-08-11 by Phase 2 Task 14.

`cmd/wake`'s `socketFleet.act` writes the verb and then a `FrameStatus` on the same connection, and
the daemon's synchronous per-connection dispatch makes the reply an acknowledgement: a refusal is
enqueued to this client before it. That closes every refusal `dispatch` itself produces — unknown
session, the session has ended, the input queue is full.

**What it does not close** is a write that fails *later*: `agent.submit` queues the frame and
`serveInput` performs the stdin write on the agent's own goroutine, so a failure there is reported
with an error frame addressed to a connection this tool has already closed. The TUI has the same
residual and survives it by still being attached; `wake mcp` is not. The honest fix is a client that
stays attached for the life of the manager session, which the file's own header argues *against* on
cost grounds (one more client the daemon fans every event out to, at 30 agents, for a process that is
idle almost all of its life). **The cheaper half, if this ever bites:** the manager's next
`agent_status` shows the agent idle with nothing new, which is the observable an operator or a model
would actually notice.

**Phase 2 Task 15 review — `cmd/wake/mcpguard_test.go` follows calls this package declares and stops
at the daemon's door.** Added 2026-08-11 by Phase 2 Task 14.

The guard walks from `serveMCP` and `socketFleet`'s methods through calls `cmd/wake` declares, and
collects every `rpc.Frame` composite literal it can reach. `socketFleet.List` calls `daemon.Status`,
which writes a frame *inside* `internal/daemon`, and the walk does not follow it there. Today that is
sound by inspection — `internal/daemon` exports `Status`, `Dial`, `FleetOnDisk`, `EnsureRunning`,
`Serve`, `SocketPath` and `OpenLog`, and none of them writes a session verb — but a future export
that did would be invisible. **The close, if it is ever worth it**, is to extend the walk across
package boundaries for `internal/daemon` specifically, the way `internal/core/argvguard_test.go`
walks its own package's call graph. Not done because the boundary is one package deep and stated in
the file's own header, which is the rung-5 rule about admitting what a guard does not close.

**Phase 3 fork UI — `rpc.SessionStatus.ParentID` reaches every client and two consumers
deliberately drop it.** Added 2026-08-10 by Phase 3 Task 2 (the fork verb).

The daemon now records the parent→fork edge and puts it on every status row, because nothing on
Claude's wire ever will. Two guards downstream insist that a field added to that report is either
carried or excused with a reason, and Task 2 excused both rather than widen past the wire and the
daemon:

- `internal/ui/fleet_test.go` — `notCarriedOntoAnAgent["ParentID"]`. `ui.Agent` has no field for
  it and `WithStatus` does not fold it. **The task that gives the TUI a fork action carries it in
  the same change**, because that is the task that decides where `a fork of sydney` is drawn; a
  field folded now would be state with no reader on a type Bubble Tea copies per keystroke.
- `internal/mcp/tools_test.go` — `notInTheStatusReport["ParentID"]`, with
  `notInTheStatusReportCount` at 3. **There is no fork verb on that surface**, so lineage is
  context a manager cannot act on, and the manager-legible form of it is the parent's *name*
  rather than a UUID. Note what is **not** a reason, because the first draft of this excuse
  claimed it: resolving the name is not hard. `statusReport` is handed one row, but
  `agentStatus`' own closure holds the whole report, so it is a four-line loop one level up that
  fails only when the parent has aged out of the 32-entry `recentEndings`. **The task that gives
  the manager a fork tool carries the name resolution with it.**

Neither is a silent drop: both guards fail the build if the excuse is deleted and the field is
still unrendered, and the mcp one fails if the count is not edited in the same breath.

**Phase 3 fork UI — a confirmed fork has to say that a fork is a snapshot. CLOSED by Task 4
(2026-08-10), which is the task that drew the fork action.** Added 2026-08-10 by
Phase 3 Task 2, whose `forkRefusal` comment promised this in the present tense before it existed.

*What landed:* `ui.forkOpened`, on the notice row of the fork's own conversation, the moment the
daemon's report first names it — *"@sydney is a fork of @alex. It has that conversation as of now -
nothing @alex does next reaches it."* Once per **confirmed** fork rather than once per keypress,
which is the question this entry said the fork action owned: a refused fork says the daemon's own
sentence instead, which names when the parent could be forked. `forkRefusal`'s comment and
`v1_goals.md`'s paragraph were both corrected in the same commit, so the claim and the code now
agree for the first time since it was written. **The entry stays here rather than being deleted
because the claim was made twice before anything implemented it** — once in a comment, once in a
doc written by the brief that was correcting the comment — and that pattern is worth more than the
line it produced.

The rest of this entry is the history that made it worth writing down.

`forkRefusal` is a refusal and not a lock: it declines a parent that is mid-turn *now*, and a
parent that starts a turn a millisecond after it returns is the other unrecorded composition —
"a parent taking a turn while a fork is still running" (2026-08-10 findings §12), which is
**the single most likely next thing an operator does**: fork an agent, then keep typing to the
original. Wake cannot prevent it, because the operator owns the parent's composer. The mitigation
is therefore a sentence rather than a guard — on every confirmed fork, that the fork carries the
conversation *as of the moment it was taken* and will not see what the parent does next.

It belongs to whichever task draws the fork action, because that task owns where the line goes.

*Task 3 (`wake fork <who> [name]`) deliberately did not take it, and the reason is the placement
rather than the sentence.* The verb ends in `converse`, so the only surface it has for saying
anything is the DM's notice row — which is exactly where the TUI's fork action will want it, and
that action is the one that owns whether the line appears once per fork or once per confirmed
fork. Building it here would mean either a second `notice.Report` for the TUI to reconcile with, or
a confirmation hook threaded through `openSession` for one of its two callers. **Whichever of the
two lands the line, the other reads it** — and Task 3's brief prescribed a `v1_goals.md` paragraph
claiming Wake *already* says it, which was corrected to the honest tense in the same commit. That
is the `forkRefusal` failure arriving a second time through a doc, which is why this note says so.

**Fork — the roster sidebar cannot express ancestry, and this was decided rather than missed.**
Added 2026-08-10 by Phase 3 Task 4. `rpc.SessionStatus.ParentID` reaches the client and is now
folded onto `ui.Agent`, and the right sidebar does not draw it. The sidebar is 20 columns and two
lines per agent and its job is *what this agent is doing*; ancestry is identity, which the DM header
and `wake status` both carry. **The cheap version if it is ever wanted** is one glyph before the
name on the first row — not a third line, which costs a screenful at 30 agents — and it would need a
column-width check, because `roster.go`'s rule is that a row wider than its column shoves the room
and the DM sideways rather than overflowing itself.

**Fork — `⌃F` shadows the text area's CharacterForward.** Third of three, after `⌃D` and `⌃W`, and
the cheapest: the composer is one line high, so `ctrl+f` is `→` by another name. Recorded beside the
existing `⌃D`/`⌃W` entry because somebody with readline muscle memory will find it.

**Fork — a fork whose first report already says `ended` opens nothing and says nothing.**
Added 2026-08-10 by Phase 3 Task 4, and it is narrow rather than theoretical. `isTheAwaitedFork`
opens the fork's conversation in every state a running daemon can report *except* `ended`, because a
pane with no process behind it is the empty conversation with a working-looking header
`cmd/wake.reattach` exists to prevent. A fork that **failed** to start is covered — the daemon sends
a `FrameError` addressed to the fork's own id, which clears the wait and prints why. What is left is
a fork that started, exited cleanly, and was first reported after that: `App.pendingFork` stays set,
so ⌃F looks like it did nothing beyond its own "forking @alex…". The fix is a third arm saying the
fork ended before it could be opened; it was not built because the sentence is a decision and this
task's brief did not ask for one.

*Widened 2026-08-10 by park/wake Task 2: `parked` joins `ended` in that arm, for the same reason and
with the same residual.* A fork first reported parked opens no pane and stays in `pendingForks`,
which is the entry above with one more way in. Two things separate it from the ended case and both
argue for taking them together rather than separately. It is **rarer** — it needs somebody to park a
session the daemon has only just started, in the window before the first report reaches this
client. And its answer is **different**: an ended fork's sentence is "it ended before it could be
opened", while a parked one's is "it is parked, and here is how to bring it back" — which is a
sentence `/resume` has to exist for. **So whoever builds `/resume` owns this cell**, and the choice
they are actually making is whether a parked pane opens at all: a composer over a dead process is a
dead end today and stops being one the moment there is a key that revives it.

**Park — the room has no account of a parked agent beyond its glyph and its rank, and that is spec
§6 rather than an oversight.** Added 2026-08-10 by park/wake Task 2.
`2026-08-10-lifecycle-and-restore.md` §6 lists *"what a parked agent looks like in the room and the
sidebars"* under what it does not settle, so this task took only what a guard demanded (the
attention rank, the roster glyph, the fork-arrival cell) plus routing — `ui.live`, because that is
`core.Resolve`'s input and a wrong answer there spends a turn at every agent `@all` reaches.

*What is deliberately still ended-shaped, with what it costs:*

- **`App.endedAgent` does not cover parked**, so a DM open on a parked agent accepts a message,
  writes the frame, and the operator gets the daemon's own `session … has ended` back on the notice
  row. Wrong twice over: it is not an ending, and the local refusal exists precisely so the round
  trip does not have to say it. One line to fix; the sentence beside it (`endedAdvice`) is the part
  that is a decision.
- **`App.noteEnding` does not latch on parked**, so a client whose *own* session is parked keeps a
  live-looking conversation. Not taken because the honest notice says the session was parked and
  what to do about it, and both halves of that are the room's park story.
- **`ui.hangup`'s `reattachedText` needs nothing**, and that is derived rather than assumed: `redial`
  goes through `cmd/wake.liveSession`, which now refuses a parked session above it, so no parked row
  can reach that switch. If reattach ever stops going through `liveSession`, this becomes live.
- **`Fleet.ByName` deliberately still resolves a parked agent.** It skips *ended* because a name goes
  back to the pool when a session ends; a parked one keeps its name claimed, so resolving it is
  correct - it is the same agent, and nothing else can answer to that name.
- **A parked agent can keep a stale `RequestID`.** If the process dies without a turn end, `observe`
  never clears `a.pending`, `snapshot()` still reports it, and `ui.Cards.Reconcile` keys on
  `RequestID` rather than `State` - so an answerable card sits over a dead process and answering it
  gets the daemon's refusal back. The same shape already exists for `ended` rows in `s.recent`, so
  this is one state wider rather than new; it belongs with the composer gate above, because both are
  "what the room says about a session with no process" and both want the same predicate.
- **A fleet of only parked sessions lists as "nothing is running".** `runningSessions` heads its
  listing *running now* and a parked session is not running, so `wake attach` with nothing live
  prints that - correct, and unhelpful for somebody who has sessions and cannot see them. `wake
  status` lists every session with its state, which is the surface that answers it today. Whoever
  gives the listing a second section owns the wording.

**~~Park/wake — nothing in the shipped surface sends `rpc.FramePark` or `rpc.FrameWake`.~~ CLOSED**
by park/wake Task 7 (the rebinding), 2026-08-11, which is exactly where this entry's own closing
condition put it — and **left unmarked until the whole-branch review found it on 2026-08-11**. Task 7
closed the two entries either side of this one with dated notes and walked past the one that states
the falsehood most flatly. `internal/ui/park.go` sends `FramePark` on `⌃C` and `FrameParkAll` on
`⌃Q`; `internal/ui/slash.go` sends `FrameWake` on `/resume`. All three frames have a sender, so
*"the state is producible"* and *"an operator can produce it"* are finally the same claim. The entry
is kept whole below because its last paragraph is the more useful half.

Added 2026-08-10 by park/wake Task 2; widened by Task 3, which added the second frame and no way for
an operator to send it either. No key in `App.key`, no verb in `cmd/wake`, no MCP tool: both frames
are reachable only from a client that writes them, which is what the daemon tests do. That is the
intended shape — the spec is explicit that park is a prerequisite for rebinding `⌃C` and not a
companion to it — but it is worth stating, because *"the state is producible"* and *"an operator can
produce it"* are different claims and only the first is true today. **The task that binds `⌃C`
closes this**, and it is the same task the entry above is addressed to. Note the ordering it
inherits: `wake attach`'s refusal of a parked session names `wake fork` as the only built verb that
reaches the conversation, *because naming a wake verb that does not exist is the legend rule broken
at the shell*. That sentence becomes wrong the day a `wake resume` verb ships, and it is in
`cmd/wake/match.go`.

**And the last sentence is why this entry is worth reading after it is closed: the trigger it set
was too narrow, so it never fired.** The refusal went wrong the day a **`/resume` command** shipped
— Task 6 — not the day a `wake resume` *shell verb* did, and there still is no shell verb, so the
condition being watched for is unmet to this day while the sentence it guarded was wrong for three
tasks. It was found by reading, twice, from two directions: by whoever wrote chapter 7 of the user
manual (`071a0e4`) and independently by the review. A watch written against the mechanism you expect
rather than against the claim you are protecting is a watch that reports nothing while the claim
rots — and `park_test.go`'s guard did worse than not fire, since it forbade the word `resume` in the
refusal and made the correct sentence a build failure. That is rung 7 in
`docs/notes/decisions.md`, and this entry is one of its two instances.

**~~Wake — `replaceParked` must take the row *before* a process is started, and that is a stated
precondition of the task that binds a key to wake, not a note.~~ CLOSED** by park/wake Task 7 (the
rebinding), 2026-08-11 — the task that bound the key, which is where its own ruling put it. The
entry is kept whole below because the argument is what the fix is built on; what closed it is at
the end. Added 2026-08-10 by park/wake Task
3, which found the two survivors by mutating its own work; **re-scoped by that task's review, which
rejected the framing and ruled the fix into the next task.**

Two mutations survive the whole suite. `replaceParked`'s `s.agents[a.id] != was` replaced by `false`
— accept whatever is in the row — and `launch`'s `if replaces == nil` guard around the *second*
`s.names.release`, the one on the failed-`admit` path, made unconditional. Both arms are **live
rather than dead**: each client connection dispatches on its own goroutine, so two `FrameWake`
frames for one id can be in `unpark` at the same time, and the loser reaches exactly those lines.
Neither is deterministically reachable from a test — the window is between `s.agent()` and
`replaceParked`, with a `claude` exec in it.

**Two consequences, and the second is bigger than the first report claimed.**

1. *The transcript hazard.* On that path the loser has **already started a second process on the
   id**, and `resumeSafe` provably could not have seen it: the winner's process may not have existed
   when the loser checked. Two live processes on one id branch the transcript in place with
   last-writer-wins and no error on any wire. *"A refusal, not a lock"* is the right stance for a
   stray `claude --resume` — an operator-owned process Wake never minted, the same trade
   `forkRefusal` makes — and it is **not** the right stance for two wakes Wake itself issues
   milliseconds apart, because Wake owns both sides of that one.
2. *A live `claude` past `wake stop`.* `takeAgents` empties `s.agents` under `s.mu`, and
   `register`/`replaceParked` are its only other writers. With the check gutted, a wake racing
   `beginQuit` re-inserts a **live** agent into a map `shutdown` has already snapshotted — a process
   nothing will ever stop or kill. That is the leak the daemon soak lane's `childCount` exists to
   catch and which no test in the tree reaches.

**Why the deferral is acceptable today and only today:** nothing in `cmd/`, `internal/ui/` or
`internal/mcp/` sends `rpc.FrameWake` at all, so the product cannot produce one wake, let alone two.
It becomes reachable in the same change that binds a key — a double keypress or an impatient retry
is exactly the shape.

*So this is a precondition of that task, in the same sentence as "nothing sends the frame yet":*
**binding a key to wake is the change that makes this reachable, and the row-before-process fix
lands there or the key does not.** It is an `admit`-before-`Start` reordering rather than a
test-only hook — take the row, then start the process, and release it if the start fails. The
name-release guard is the lower-stakes companion and rides along with the same fix. The loser's
error text goes with it too: today it is *"session … already exists"*, which is the spawn sentence.

> **CORRECTION, 2026-08-11 — one sentence of the premise above expired, and the ruling is
> untouched.** Park/wake Task 6 (`/resume`) makes `internal/ui/slash.go` the only non-test writer of
> `rpc.FrameWake` in the tree, so the paragraph beginning *"nothing in `cmd/`, `internal/ui/` or
> `internal/mcp/` sends"* is **now false as written** and is kept above only so this correction has
> something to correct.
>
> **What did not change is reachability, which is what the deferral actually rests on.** `/resume`
> writes a wake only for an agent the daemon reported `rpc.StateParked`, and **nothing shipped can
> produce that state**: `FramePark` and `FrameParkAll` still have no sender, so `parked.json` is
> never written and `restoreParked` restores nothing. `parkedAgents()` is empty on every real fleet,
> every `/resume` lands on `noParkedSessions`, and no operator action reaches `unpark` at all. It is
> a sender whose guard can never fire. **So the ruling stands exactly as written — the fix lands
> with the key, and the key is Task 7.**
>
> **CORRECTION TO THE CORRECTION, same day.** The first draft of this note claimed *"`/resume all`
> asks for N wakes from one keystroke, and a second `/resume all` typed while the first is still
> landing asks for the same N again — nothing in the TUI serialises them"*. **Both halves are wrong
> on the mechanism, and a wrong mechanism here sends whoever picks the fix up to the wrong place.**
> The race is two wakes of **one** id; `/resume all` is N wakes of N **distinct** ids, which collide
> with nothing. And a second `/resume all` from the same TUI **is** serialised — not by the TUI, by
> the daemon: `serveClient` ranges its connection with `for f := range frames { s.dispatch(ctx, c, f) }`,
> synchronously, one goroutine per connection, and `dispatch` calls `unpark` → `launch`, which runs
> `sess.Start` and `s.admit` inline before returning. Frame N+1 is not read until frame N's wake has
> taken or failed to take the row, so the second wake finds `isParked() == false` and gets *"session
> … is not parked"*. No second process starts.
>
> **The real exposure is two client *connections* waking one id** — two `wake` windows attached to
> one daemon, which is this product's own premise at 15–30 sessions. `/resume all` does make that
> worse, for a different reason than the one first given: it multiplies the ids simultaneously in
> flight, so one pair of windows gets N chances to collide instead of one. Two things narrow it and
> both belong in the fix's head: `resumeSafe` means the loser only loses if its `ps -Aww` completed
> before the winner's `exec` became visible, and `launch` already calls `sess.Stop()` on the loser,
> so the two-process window is bounded rather than open-ended. That is what makes this a hazard
> rather than a certainty.
>
> **The fix itself is unchanged and is a restructure of `launch`** — build the agent, `admit` it,
> *then* `Start`, and roll the row back to `was` on failure — with `register`'s and
> `replaceParked`'s rollback, `forgetLocked`'s roster removal and the concurrent-spawn refusals all
> in its blast radius. Task 6 was scoped to `internal/ui`, so it did not attempt it.

*What closed it, 2026-08-11 (Task 7):* exactly that restructure. `launch` now builds the agent,
`admit`s it, and only then `Start`s — so the loser of two wakes of one id is refused **with nothing
started**, and there is no second `claude` to branch a transcript with. `withdraw` is the one thing
the reorder costs: a launch whose process will not start puts the row back to `was` for a wake and
deletes it for a spawn, which is what keeps *"a failed wake leaves the session parked and named"*
true and what stops a failed spawn leaving a row with no fan-out goroutine behind it — a session
that reports as live forever and that `wake stop` counts. The loser's sentence moved with it:
`admitRefusal` says *"was already brought back by something else"* for a wake and keeps *"already
exists"* for a spawn, because a wake's session existing is what makes it a wake.

Four guards, and the ordering is held statically because that is where it lives:
`TestLaunchTakesTheRowBeforeItStartsAProcess` reads `launch`'s own statement list (and refuses a
`go`/`defer` in front of either anchor, which would keep both lines where they are and destroy the
property); `TestAWakeThatLosesTheRowForItsIdStartsNoProcess` drives the loser's exact state through
`launch` with **no `claude` on PATH at all**, so a refusal naming the row proves `Start` was never
reached and an exec failure proves it was; `TestReplaceParkedTakesTheRowItReadAndRefusesAnyOther`
is the direct negative the gutting mutant survived for want of — nothing had ever called it with a
row it should refuse; and `TestALaunchThatCannotStartPutsTheRowBackAndKeepsTheEnding` holds the
rollback and the remembered ending `admit` drops on the way in. All four were verified by mutation
— the gutted check, an unconditional name release, no rollback, a rollback that forgets the ending,
and the spawn sentence on the wake path each go red.

**What is *not* closed by it, and it is a different race with the same smell.** A **fresh spawn**
that wins the map race after `takeAgents` has already snapshotted the fleet still starts a process
`shutdown` will never stop — the window is between `maySpawn`'s `stopping` check and `register`, it
predates this change, and the reorder neither widens nor narrows it. Closing it is an `admit` that
refuses once the fleet has been taken, which the reorder makes *cheap* for the first time (the
refusal now happens before there is a process to stop). Not attempted here: it is a shutdown-path
change with its own blast radius and it is not the hazard the key made reachable. See the entry
below.

**UI — `⌃C` and `⌃Q` both report on the keypress, before anything has confirmed the write.** Added
2026-08-11 by Task 7's review.

`park` writes *"parking @alex — /resume alex brings it back"* and `parkFleet` writes *"parking N
agents…"* as the key is pressed. If the write is refused — a full input queue, which the daemon
answers with *"session X is not reading its input"* — the park never happens and the error frame
lands on the notice row *after* the optimistic line. The operator sees both, in that order, on one
row that holds the newest.

*Blocks:* nothing, and it is consistent rather than a slip: `⎋` reports the same way, for the same
reason — a key that says nothing is the one outcome this project treats as worse than a refusal, and
the confirmation is a status push that may be some time coming. The failure is visible and
recoverable, which is what keeps it a note.

*Why it is written down:* `⌃Q`'s version has the same shape and a worse consequence — the window is
already closing, so the error frame has nowhere to land at all. The hedge in `parkingFleet` covers
the grace, not a refused write. Whoever gives the notice row a pending state should take both.

**Daemon — the soak lane sends no `FrameWake`, so it does not exercise the wake path it is credited
with.** Added 2026-08-11 by Task 7's review.

`internal/daemon/soak_test.go` churns whole lifecycles through a real socket and checks goroutines,
child processes and the roster afterwards — which is what makes it the detector for a leaked
process. What it never sends is `rpc.FrameWake`, so `replaceParked`'s refusal, `withdraw`'s wake arm
and `admitRefusal`'s wake sentence are **not** under it, and Task 7's report credited the lane with
covering the `launch` reorder more broadly than it does. The reorder's own guards
(`launch_test.go`) do cover those; what is missing is the same paths under `-race` at churn.

*Blocks:* nothing today. It is an evidence gap rather than a defect, and it is written down because
the claim was made once and would otherwise be inherited.

*The fix:* a park-then-wake in the soak's lifecycle loop, which is a handful of lines and needs the
fake `claude` to survive a resume. Not attempted in the fix round because a soak change that flakes
is worse than a soak that covers less, and this one runs for 30s in every review.

**Daemon — a spawn that wins the map race after `takeAgents` starts a process nothing will stop.**
Added 2026-08-11 by park/wake Task 7, which found it while closing the entry above and deliberately
did not fix it.

`maySpawn` refuses a spawn once `s.stopping(ctx)`, and `shutdown` empties `s.agents` through
`takeAgents`. Nothing makes those one step. A spawn that passed `stopping` and then reaches
`register` **after** `takeAgents` has snapshotted the fleet is admitted into a map that is no longer
read, and `launch` then starts a `claude` that `shutdown`'s grace, its kill and its roster clear all
walked past — the leak the daemon soak lane's `childCount` exists to catch, and which no test in the
tree reaches. It is the same shape as the `replaceParked` hazard and it is **not** the same race: it
is the *fresh spawn* path and it predates Task 3.

> **CORRECTION, 2026-08-11, by Task 7's review — this entry's first draft said the reorder "neither
> widened nor narrowed it", and that was right about the sub-case above and wrong as a general
> claim.** Before the reorder, an agent in `s.agents` had **always already been `Start`ed**, and
> `shutdown` is built on that invariant. After it, the invariant is false for the width of an exec,
> which creates a **second** sub-case that could not exist before: `admit` puts the agent in the
> map, `shutdown` snapshots it and calls `stop()` — which reaches `core.Session.Stop` on an
> unstarted session, sets `stopped` and returns nil, **a graceful stop consumed and lost** — and
> `launch` then execs anyway, because `Start` read `s.cmd != nil` and not `s.stopped`. Nothing would
> ever close that process's stdin, so `waitForAgents` would stall the whole grace, `kill()` would
> SIGKILL it mid-turn, and `kill` clears the park flag — so under `⌃Q` that session would be
> **dropped from the park book**, which is the one outcome the whole park design exists to prevent.
>
> **Closed in the same round, by one line in `internal/core/session.go`**: `Start` now refuses a
> session that has already been stopped. That turns the path benign rather than merely narrower —
> `Start` fails, `withdraw` puts the row back, the client gets an error frame, and nothing is
> exec'd, stalls or is killed. `Send` already read `stopped` through `writer`; `Start` was the only
> lifecycle entry point that did not, and `Stop` is idempotent by design.
> `TestStartRefusesASessionThatWasStoppedBeforeItStarted` counts the exec seam rather than reading
> the error, because `err != nil` is not the claim.
>
> **What is still open is only the original sub-case**, below: a fresh spawn admitted after
> `takeAgents` starts a process that shutdown never sees. The `Start` refusal does not close that
> one — nothing stopped that session, so there is nothing for `Start` to refuse.

The wake path is already closed by that reorder for a reason worth stating, because it is not the
same reason: `replaceParked` refuses a row that is no longer `was`, and an emptied map is not `was`,
so a wake racing a shutdown is refused rather than admitted.

*The fix:* make `admit` refuse once the fleet has been taken — a flag written under `s.mu` in
`takeAgents` and read under the same lock by `register` and `replaceParked`. **The reorder is what
makes it cheap**: the refusal now lands before there is a process, so it costs an error frame rather
than a `sess.Stop()` on something that has already started. It is a shutdown-path change with
`beginQuit`, `waitForAgents` and every restart test in its blast radius, which is why it is written
down here rather than smuggled into the task that bound a key.

> **CLOSED 2026-08-20, branch `fix/daemon-review`, as prescribed above** — `s.taken`, written in
> `takeAgents` under `s.mu`, read by `register` and `replaceParked` under the same lock, with
> `admitRefusal` answering "the daemon is shutting down". See the entry under heading 4.

**~~Park — a parked session does not survive its daemon.~~ CLOSED** by Phase 3 Task 4 (the park book), 2026-08-10.

**Park — a parked session does not survive its daemon, and Task 3 did not change that.**
Added 2026-08-10 by the review of park/wake Task 2, and it is the entry that task should have
written and did not. **Re-addressed 2026-08-10 by Task 3, which built the wake verb and deliberately
did not build the record**: `rpc.FrameWake` brings back a session *this* daemon is still holding, so
it reads `s.agents` and needs nothing on disk. Spec §4's *restore on start* — "if `⌃Q` parked the
fleet, the next `wake` can offer to wake it" — is the half that needs the durable record, and it is
still unbuilt. **Now addressed to whoever builds that.**

Measured: park a session, send `FrameQuit`, and `daemon.Status(socket)` answers
`Running: false, Sessions: []` with the roster file gone. `completePark` removes the roster record
(a parked session is not an orphan and must not be reaped) and `takeAgents` empties `s.agents` on
shutdown, so **the id — the one thing park exists to preserve, and the thing `--resume` needs — is
exactly what is lost.** The transcript stays on disk, so nothing is destroyed; what is gone is
Wake's knowledge of which id to hand back.

This is correct **scope**: spec §4 puts the durable record in the restore task, and says the roster
is the wrong home for it — *"nothing turns a roster record back into an agent … Restore changes
that, so it needs its own record with its own argument."* What was wrong is that `CLAUDE.md` said
park "keeps the id" with no lifetime on it, which is the legend rule read backwards: a doc claiming
a behaviour that does not exist is what the next implementer builds on. The sentence now says *for
as long as the daemon lives*.

*What closed it:* the park book — `internal/daemon/parkbook.go`, a record whose reader turns an
entry back into a `--resume`, which is the thing `roster.go` deliberately is not. `CLAUDE.md`'s
registry paragraph was rewritten in the same change, exactly as §4 requires, and it says which half
of the old argument survives: nothing turns a *roster* record back into an agent and nothing was
added to `loadRoster`'s consumers, and a second file does turn a *parked* record back into one.

**One half of the qualifier is now a decision rather than a gap.** After a `wake stop` a parked
session still leaves no trace, because the quit verb clears the book — spec §2 makes stop the one
ending there is no way back from. A daemon that is signalled, crashed or merely restarted hands the
fleet on. `⌃Q` therefore has to arrive as its own kind rather than as a quit, and that is the task
that binds it.

*Closed on the daemon side 2026-08-11 by Task 5:* `rpc.FrameParkAll` is that kind, `daemon.quitVerb`
is why the daemon is ending, and `shutdown` writes the book before it closes its clients. What is
still open is the key — see the entry directly below.

**`⌃Q` has no key, and neither has anything else that reaches park, wake or park-all.** Added
2026-08-11 by park/wake Task 5, and it is the same sentence Task 2 wrote about `FramePark`, now
covering three frames instead of one.

`rpc.FramePark`, `rpc.FrameWake` and `rpc.FrameParkAll` are all served by the daemon and **nothing
in `cmd/`, `internal/ui/` or `internal/mcp/` writes any of them**. So the whole of park/wake is
reachable today only from a client that composes the frame itself, which in practice means this
package's own tests. *"The state is producible"* and *"an operator can produce it"* are different
claims and only the first is true.

That is deliberate scope — the lifecycle spec's §2 makes the rebinding wait on the machinery, in
that order, and the key choice is itself constrained: `⌃O ⌃X ⌃Y` are the only unshadowed keys left
and one of them is owed to next-blocked when deferred I7 reclaims `⇧⇥`. Whoever binds `⌃Q` inherits
two things from this task rather than one:

- the client half of the verb, including what the TUI does between the keypress and the daemon
  hanging up — `⌃Q` blocks for the length of the shutdown (up to `quitGrace + shutdownWait`), and a
  terminal that looks frozen for thirty seconds is the failure `wake stop`'s wait already exists to
  avoid.
- **the `replaceParked` row-before-process fix**, which is a stated precondition of binding a key to
  *wake* and is recorded above. `⌃Q` does not make it reachable — it starts no process — but the two
  land in the same surface.

**A park that could not be written down is in the daemon's log and on no operator surface.** **CLOSED 2026-08-24 (daemon/Tier-1) — PR #99 (`fix/park-writedown-visible`):** `warnUnbooked` broadcasts a `FrameError` naming the session from both `parked.add` failure arms (`completePark`/⌃C and `bookParked`/⌃Q). Tier-2 (rendering it through ⌃Q teardown) remains, owned by the ⌃Q task. Added
2026-08-11 by park/wake Task 5's fix round, from its review (m1).

`bookParked` reports a failed `parked.add` with `logf` and carries on, so an operator who parks
twenty and is offered nineteen back learns which one went missing by opening `~/.wake`'s log. The
surface exists at that moment and is deliberately unused: `bookParked` runs **before**
`closeClients`, so every client is still attached and `s.broadcast(errorFrame(a.id, …))` would
reach them.

Not used because the client half does not exist — nothing renders a fleet-park failure, and a TUI
about to be handed an EOF may not draw one it receives — so the frame would be a promise made in
prose, which is the failure mode this project has a ruling about. **The decision belongs to the
task that binds `⌃Q`**, in the same change as the key: decide whether a park that could not be
written down is announced to the attached clients before they are closed, and if so what the room
does with it while it is closing.

**The daemon soak lane does not exercise `quitPark`, and the reason is not the one first written
down.** Added 2026-08-11 by park/wake Task 5's fix round, correcting its own report (review m3).

What the review proposed is one frame — swap `reader.send(rpc.Frame{Kind: rpc.FrameQuit})` for
`FrameParkAll` at the end of `TestSoakDaemon` — and **it would exercise nothing**. The lane asserts
`live(reader.status())` is empty *before* it parks anything ("or the shutdown path is what is being
measured rather than the churn"), and `parkSome` then spawns a fixed handful and parks each one
serially. So at the moment the quit verb is sent the fleet is N **already-parked** agents with no
processes behind them: `beginPark` refuses each (they are parked), `bookParked` finds every id
already in the book and writes nothing, and no session is parked on the way out at all.

The earlier justification — *"`bookParked` starts no goroutine and holds no resource"* — was about
the wrong half, and the review is right about which half matters: under `quitPark` **every agent
takes `retire`'s `completePark` branch instead of the ordinary ending branch**, simultaneously, on
N fan-out goroutines, racing `shutdown`'s kill loop and `bookParked` on `Serve`'s goroutine. That
is the concurrency this task exists to make safe and it is exactly what the lane's goroutine and
child detectors are for.

**What a real `quitPark` phase costs**, and why it is not one frame: it needs a *live* fleet at
shutdown, which means spawning a batch after the churn drains and ending on `FrameParkAll` — and
the closing assertion inverts, from *"the park book after `wake stop` is empty"* to *"it holds
exactly the fleet that was parked"*. That is a **stronger** assertion and the book has never had
one at fleet scale, but it costs the existing one, because a daemon quits once. Two endings need
two daemons. Do it in the task that binds the key, where the frame has a producer.

**A message queued for an agent can lose its race with a fleet-wide ending, and `⌃Q` inherits that
from `wake stop` rather than adding it.** Added 2026-08-11 by park/wake Task 5.

`dispatch` routes `FramePark` through the agent's own input queue precisely so a park lands *behind*
the messages already queued for it — *"finish what I have given you, then park"*. `FrameParkAll` is
not addressed to any agent, so it is on no queue: it records the verb, the accept loop ends, and
`shutdown` stops every session directly from `Serve`'s goroutine. A `FrameSend` still sitting in an
agent's `in` channel therefore races that agent's stdin closing, and loses roughly always.

`FrameQuit` has always behaved this way and nothing has ever complained, because `wake stop` is a
shell command typed after the typing is done. `⌃Q` is a keystroke inside a composer, which is a
different ergonomic: the realistic sequence is *↵, ⌃Q*, and the message may not have been queued
long enough to be written. **What would close it** is draining each agent's input queue before the
stop rather than after — an `a.drain()` on the input goroutine, bounded, ahead of `beginPark` — and
what makes it a decision rather than a bug is that the bound is a guess about how long a fleet-wide
quit may take. Recorded here so the task that binds the key can decide it with the composer in
front of it.

**Park — a parked agent's `submit` refuses with "session … has ended".** Added 2026-08-10 by
park/wake Task 2, and it is one string rather than a mechanism: `agent.submit` selects on `a.gone`,
which a park closes exactly as an ending does, so the sentence is the ending's. Correct about what
happened to the process and wrong about what happened to the session, on the one surface an operator
reads when a message does not go. Whoever gives park a client-side gate should fix it at the same
time, because that gate is what stops the frame being sent at all.

**Park — nothing evicts a parked entry from `s.agents`, and the soak lane is where that bites
first.** Added 2026-08-10 by the review of park/wake Task 2 (M5), sharpened by trying to soak it.

The retention is deliberate and stated: `holds` has to keep refusing a respawn under a parked id,
because two live processes on one id branch the transcript silently. So the map grows with
"sessions ever parked", which for a human pressing ⌃C is nothing. **Spec §6 is what makes it a
question**: it leaves open whether the pool parks *automatically* under a live cap, and an automatic
parker is one that runs all day.

*Narrowed 2026-08-10 by park/wake Task 3, and only narrowed.* A wake now **replaces** that row
(`replaceParked`) rather than adding beside it, so park-then-wake is flat rather than growing — which
is the shape an operator actually produces. What still grows is a session parked and never woken, and
that is the case the automatic parker makes routine. The daemon soak lane parks and does not wake, so
nothing measures the flat shape either; the lane's park phase is deliberately bounded and outside the
churn for the quadratic reason above, and a wake phase would inherit that constraint.

*Measured while adding park to the daemon soak:* parking inside the churn loop makes the lane
quadratic in its own duration, because every parked row is serialised into every status reply and
the workers poll status. The park phase that shipped is therefore **bounded and outside the churn**
— it proves a parked process is gone and stays gone across a shutdown, which is what `childCount`
is for, and it deliberately does not measure retention. *What closes it* is deciding what a parked
row's lifetime is, which is the same decision as the park book above: an entry that can be
reconstructed from a durable record does not need to be held in memory forever.

*Narrowed again 2026-08-10 by Task 4, and again only narrowed.* The durable record now exists, so
the second half of that sentence is available for the first time: a parked row **can** be dropped
from `s.agents` and rebuilt from `parked.json`. It is not, and deliberately — `holds` refusing a
respawn under a parked id is what stops two processes reaching one transcript, and that check reads
the map rather than the file. Nothing has changed about the growth; what has changed is that the
fix is now cheap. Note the restore makes the shape visible for the first time too: a daemon that
starts with a hundred entries in the book puts a hundred rows in every status reply, which is the
same quadratic the soak lane's park phase was moved out of the churn to avoid.

**~~Park — the daemon soak lane cannot run more than one client.~~ FIXED** in park/wake Task 2's
fix round, and recorded because of how it hid. `soakClient.oneLifecycle` asked for the literal name
`"soak"` on every spawn, and `nameRegistry.claim` **refuses a requested name that is already held** —
so with more than one concurrent client every lifecycle after the first was refused, its session
"never appeared", and the worker waited out the full 30s `soakStep`. Measured on the committed tree
at three clients over two seconds: **2 of 9 lifecycles failed**; at the advertised twenty it is most
of the run. The lane was *red*, not slow.

Nothing caught it because the Makefile's guard asks only that `TestSoakDaemon` **exists** under the
tag — which is the check added when this target was a green no-op, one step short of the check it
needed. The fix is one line each in `oneLifecycle` and the new park phase: send no name and let the
daemon draw from the pool, which is what production does for bare `wake`. After it, 20 clients under
`-race` for 10s: 164 lifecycles, 0 errors, goroutines flat, `children before 1, after 1`.

**~~Park — the `!= rpc.FrameStop` exemption in `agent.apply` is dead, and adding park to it was
deleted rather than shipped.~~ CLOSED 2026-08-20**, branch `fix/daemon-review`: the
condition is deleted (the apply family now lives in `apply.go`), and `noteUnreachable`'s own guard
is the single answer to "was this session stopped" — which is exactly the close this entry
prescribed. Added 2026-08-10 by park/wake Task 2, which found it by mutating its
own work. `noteUnreachable` returns early when `a.stopped` is set and `agent.stop()` sets it before
the call that can fail, so a failed stop - or a failed park - can never reach it. Narrowing the
exemption back to stop alone left the whole package green, which is the mutation saying the arm
cannot fire. Left as it was because it is pre-existing, behaviourally identical, and deleting it is
a change to the stop path rather than to the park path. *What closes it* is deleting the condition
and letting `noteUnreachable`'s own guard be the single answer to "was this session stopped".

**`make lint` does not check formatting, and nothing else does either.** Added 2026-08-10 by the
review of park/wake Task 2, which found `internal/ui/roster.go` failing `gofmt -l` on a merged
commit: a map entry lost its alignment when a new key and its comment landed between two others.
`.golangci.yml` is `default: none` with seven linters named and **no gofmt-family member**, and CI
runs `make` targets rather than a second list — so nothing in the repository reads formatting.
Anyone with format-on-save then produces a spurious diff in a file they did not mean to touch.

*What closes it* is one line, and the choice is which: `gofmt` or `gofumpt` in the `enable` list, or
a `gofmt -l` step in `make lint` that fails on non-empty output. The linter is the better home —
`make lint` is the contract and CI already runs it — but adding one to `enable` is a decision about
this repository's formatting standard rather than a tidy-up, which is why it is here and not done.
**Note the config's own warning applies**: validate with `golangci-lint config verify`, because
`run` accepts an unknown key in silence.

**~~Restore — a TUI that attaches without spawning does not see the parked fleet for up to 30s.~~
FIXED** in Phase 3 Task 8, by the first of the two routes it named, and recorded because the choice
between them was the whole of the entry. Added 2026-08-10 by Phase 3 Task 4. `restoreParked` runs
before the accept loop, so the rows are in `s.agents` the instant a client can connect — but a client
only learns about them when it is handed a `Status`, and **`ui.App` never writes `FrameStatus`** (that
is deliberate: it wants only "is my session ended", and it can therefore read replies and pushes
without telling them apart). Every path into the TUI spawned or forked first, and that reply carries
the whole fleet, so nothing was wrong until the path that opens the room over an existing fleet with
no spawn existed — spec §3's bare `wake` — which would have shown an empty room until the liveness
tick, clamping to **30s**, noticed the restored rows were unreported and pushed.

*What closed it:* `cmd/wake.conversationOnly` writes one `rpc.FrameStatus` on its own connection and
consumes the reply through `awaitFleet`, **before the model exists**, and hands the model the seed and
the stream afterwards. So the invariant is untouched rather than traded away: `ui.App` still never
asks, which is what lets it fold a reply and a push without telling them apart. The reply is also
where the restore offer's names come from, so the two halves of this task read one report rather than
two. The daemon-pushes-once alternative was not taken, for the reason the entry gave — it changes
"nothing here starts a process, so the hello is as immediate as it was", and this route changes
nothing at all.

Note the one property that did **not** come free: `awaitFleet` has to take a `FrameStatusReply` and
never a `FrameStatusPush`, because a push announcing another client's state change can be sitting in
the socket already and seeding from it would open the room on a report assembled before the question
was asked. That is `daemon.Status`'s own rule arriving on a connection it does not own, and
`TestAwaitFleetTakesTheReplyAndNotAPushThatCameFirst` is what holds it.

**Restore — `restoreParked`'s `!s.register(a)` arm cannot fire, and is kept as a stop rather than a
guard.** Added 2026-08-10 by Phase 3 Task 4. `s.agents` is empty until the accept loop runs and the
book is keyed by id, so `records()` hands out one row per id and the registration always succeeds.
`register`'s answer is taken anyway because discarding it would leak the name just claimed, and
because moving the call anywhere but before the accept loop makes the arm live — which is the same
treatment `numberedLocked`'s bound gets and is stated in the code as such. This project deletes
guards no input can reach; this one is a `bool` the compiler will not let go unread, so the honest
move was to say what it is. *What closes it:* nothing, unless restore ever runs twice or later.

**Restore — `daemon.FleetOnDisk`'s roster-wins arm is reachable only through a failed roster write.**
Added 2026-08-10 by Phase 3 Task 4. The two lists are disjoint by construction: `completePark`
removes the roster record in the same call that writes the book entry. They overlap only if that
removal fails and the write succeeds, which `completePark` logs and carries on from — so the `seen`
map is real defence against a real (if rare) disk error and there is no test that reaches it,
because manufacturing it means a live process whose pgid is in the roster *and* an entry in the
book. *What closes it:* a test that writes both files by hand with a live bystander's pgid in the
roster half, which is about ten lines in `parkbook_unix_test.go` and was not worth the round here.

**Restore — `parkedCount`'s value space is not closed, and neither is `runningCount`'s.**
Added 2026-08-10 by Phase 3 Task 4. Both are `cmd/wake` counters keyed on `SessionStatus.State`, and
a deletion mutant of either dies immediately. A **narrowing** keyed on another field's value —
`s.State == rpc.StateParked && s.Name != ""` — survives every test, because every fixture in the
package names its sessions. That is rung 3: no finite sample of values closes a value space, and the
close is a static assertion that the function reads nothing but `State`. It was not written here
because the stakes are a display count and a branch in one sentence, against a guard of the size
`forkRefusal`'s took. *What closes it:* one AST scan in the shape of
`TestForkRefusalReadsNothingButTheStateAndWhoTheSentenceIsAbout`, covering both counters at once.

**Restore — two spellings of one UUID are two sessions, and the park book now makes that durable.**
Added 2026-08-10 by Phase 3 Task 4, and it is `decisions.md`'s *"a guard is only as strong as the
equality it uses"* arriving in a third place. `mintedByWake` is `uuid.Parse`, which reads six
spellings as one UUID; `s.agents`, `holds` and now `parkBook.held` are all keyed on the **raw
string**. So a client that spawns `A11A…` and another that spawns `a11a…` get two live sessions over
one transcript, and both are refused nothing. That is pre-existing and unchanged by this task — what
*is* new is that the collision now survives a restart, because both rows go into the book under two
keys. *What closes it:* keying `s.agents` on the parsed UUID where the id is known to be one, which
is the same fix `core.sameSession` already made one layer down.

**Park — forking a parked parent and then waking it is an unrecorded composition Wake now permits.**
Added 2026-08-10 by park/wake Task 2. `forkRefusal` allows a parked parent, on the strongest
evidence in the corpus - `2026-08-09-resume-fork-findings.md` §10, *"every fork here resumed a parent
whose process had already exited"*. What no recording covers is
`2026-08-10-live-fork-findings.md` §12's *"forking a session that another process is also
`--resume`-ing"*, which it calls the nastiest composition available, and a wake of a parked parent
while a fork of it is starting is exactly that shape. **This is a refusal-not-a-lock situation
identical to the parent-takes-a-turn one already in this file**: Wake cannot prevent it, because the
operator owns both verbs. *What closes it is a recording, not code* - one process forking a
transcript while a second resumes it.

**Fork — Wake does not stop a parent taking a turn while a fork of it is running.** It cannot: the
operator owns the parent's composer. This is `2026-08-10-live-fork-findings.md` §12's third bullet
and it is **unrecorded**, so what Wake does instead is refuse the states it can see and say, on
every confirmed fork, that a fork is a snapshot — which is the entry above, now closed. *What closes
this one is a recording, not code* — one process holding a fork open while its parent takes a turn.

**Fork — forking a parent that is mid-turn is refused, and the refusal is a stand-in for a
recording.** `forkRefusal` says no to `working`, `blocked` and `silent` because those are §12 items,
not because they are known to be unsafe. A recording that settles what a mid-turn fork inherits is
what would let any of them be allowed, and it is one cheap process per state.

**Fork — the AST scans in `internal/ui/forkguard_test.go` are `cmd/wake/forkguard_test.go`'s, spelled
twice.** Added 2026-08-10 by Phase 3 Task 4. Both derive the reachable session states from
`daemon.agent.stateLocked` and both read `rpc`'s constant block, and a Go test helper cannot cross a
package boundary without an exported support package that ships in the binary. Left as two copies
with a comment in each pointing at the other, because the alternative is a new non-test package
whose only consumer is a test. **If one is ever fixed, fix both** — they are answering the same
question about the same two producers.

**Fork — `TestTheKeysTheLegendNamesAreTheKeysBubbleteaReports` is a hand-written list of 8 of the 11
legend entries.** Added 2026-08-10 by Phase 3 Task 4, which added `⌃F` to it and left the shape
alone. `⌃C`, `⎋` and `⇞⇟` have never been probed against bubbletea's decoder. The list is exactly the
"hand-written list standing in for something the code already declares" this project keeps naming:
it could be derived from `legendKeyNames`, which already maps every glyph to its `tea.Key…`, if the
byte sequence for each were carried there too. Not done here because widening past what was asked is
this project's other named failure mode, and because the three unprobed keys are the three least
likely to move.

**Also from Task 3, and it is one line of test hygiene rather than a feature: six** pre-existing
tests in this package call `run(...)` without setting `WAKE_SOCKET`. `cmd/wake/reattach_test.go` —
`TestAttachWithNoIdAsksRatherThanGuessing`, `TestAttachWithTwoIdsIsRefused`,
`TestTheOtherVerbsStillTakeNoArguments`, `TestAMistypedVerbIsRefusedRatherThanSpawning`; and
`cmd/wake/identity_test.go` — `TestNewTakesAtMostOneName`, `TestTheUsageNamesTheVerbThatCreatesAnAgent`.
`daemon.SocketPath()` calls `os.MkdirAll` on `~/.wake` before the verb switch, so **every** `run()`
without `t.Setenv` does it regardless of verb, and `go test ./cmd/wake` creates that directory on
whoever runs it. `TestUnknownCommandsAreRefused` sets `t.Setenv(daemon.SocketEnv, tempSocket(t))`
with a comment saying exactly this, and Task 3's new tests copied it. The six were left alone
because widening past what was asked is this project's named failure mode, and the fix is six
identical lines whenever somebody is in those files.

*The count was four in the first draft of this entry, which is the failure this file's own header
warns about and which Task 3's report had invoked two paragraphs earlier* (`decisions.md`: **a
number in a comment that nothing asserts is wrong by default**). Verified empirically both times
it was written, by running each set under a `HOME` of its own and listing what appeared:

```
D=$(mktemp -d); HOME="$D" go test ./cmd/wake/ -run '<the set>' -count=1; ls -a "$D"
```

The tests Task 3 itself added create nothing outside their temp directories, checked the same way.

**Phase 3 — `liveSession`'s orphan arm cannot fire either, for exactly the reason `forkParent`'s
could not.** Added 2026-08-10 by Phase 3 Task 3's fix round, and left rather than taken.

`rpc.StateOrphaned` is produced only by `daemon.FleetOnDisk`, which is what `daemon.Status`
returns **when the dial fails** — so it arrives only on a report whose `Running` is false, and
`resolveSession` refuses that above with a sentence about the fleet. `daemon.fleet()` is the sole
writer of `Running: true` and its rows come from `agent.stateLocked`, which cannot return the
state. So `case rpc.StateOrphaned:` in `liveSession` is dead in the same way, with a comment that
reads as a live rule, and **no test reaches it** — `TestAttachSaysSoWhenTheDaemonDiedLeavingAgentsBehind`
drives the roster path, which `resolveSession` refuses first.

*Not taken here* because it is pre-existing code this task only relocated, and because deleting it
is a behaviour question about `wake attach` rather than about fork. *What closes it* is the same
move `forkParent` got: delete the arm, and let `cmd/wake/forkguard_test.go`'s
`TestTheStatesAForkableParentCanBeInAreTheOnesARunningDaemonProduces` — which already derives the
reachable domain from `agent.stateLocked` and fails the day that changes — cover `liveSession` too.
Whoever takes it should check `internal/mcp/tools.go`'s `s.State == rpc.StateOrphaned` at the same
time: it consumes the same two-path `daemon.Status`, so it is reachable there and **is** live code.

**Phase 3 — `internal/daemon/spawn.go` is at 523 lines and the seam is in its own header.** Added
2026-08-10 by Phase 3 Task 2 (the fork verb), and it is the sibling of the `session.go` item below.

Fork took the file from **395 to 523** against the 800 hard max, and well past the project's
200–400 typical band. It is nowhere near crossing, so **do not split it now** — the same reasoning
that says park/wake owns `session.go`'s split applies here: there is no failing test behind a
refactor and moving code mid-feature is a change with no guard.

**The seam, so it is not rediscovered:** the file's own header already says it is three subjects —
"Starting a session, fanning its events out to every client, and retiring it when it ends. Also
the watchdog". The cut is at `fanOut`: everything from `fanOut` through `statesChanged` plus
`sortSessions` — the fan-out, `retire`, `watchLiveness`, `probeQuietAgents`, `suspects`,
`statesChanged` — is **182 lines**, counted from the source on 2026-08-10, and shares no state with
the starting half beyond `s.mu`, which `server.go` owns anyway. That leaves **~341** in `spawn.go`
(spawn, fork, launch, forkSource, forkRefusal, maySpawn, spawnDir, record, register, cwdOrHome),
which is inside the band. Count again rather than believing this sentence.

The trigger to watch for: the next task that adds a starting verb — park/wake's `--resume` path is
one — lands ~40–130 lines in the first half. **Whoever crosses ~650 does the split first and their
verb second**, rather than discovering the file is over the max with the verb already in it.

*Re-measured 2026-08-10 by park/wake Task 2, which put its branch in `retire` and its body in a new
file:* `spawn.go` was **543** and `internal/daemon/park.go` **43**. Park cost `spawn.go` eight
lines rather than the forty the sentence above budgeted, because `completePark` is its own file —
which is the sibling half of the same seam, and worth saying out loud: the file's third subject is
"retiring it when it ends", and park is a *fourth* ending that does not retire.

*Re-counted 2026-08-10 by Task 4, from the source rather than by incrementing:* `spawn.go` **606**,
`park.go` **259**, `parkbook.go` **194**, `agent.go` **674**. The seam held — the restore's own
machinery (`restoreParked`, `parkedAgent`) went into `park.go` and the file format into a file of
its own, so `spawn.go` took none of it and grew only by Task 3's `launch`/`admit` work. **The
number to watch is still `agent.go` at 674** of the 800 max, now the second largest non-test file
in the tree behind `internal/ui/app.go` at 709; the seam there is the liveness policy against the
input queue.

**~~Phase 2 Task 10 — `⌃D` is advertised in the room and bound to nothing.~~ FIXED** (`98eb3f4`).
`ui.openDMHint` still reads `⌃D open DM` on every collapsed reply and `App.key` now has a case
for it: it opens the DM of the agent the roster cursor is on, and otherwise of the first agent in
attention order. `⌃D` also joined `legendEntries`, so the bijection guard covers it from both
sides now rather than only the card surface it was invisible on.

**~~Phase 2 Task 10 — `[a]llow` / `[a]nswer` / `[a]pprove` and `[d]eny` are advertised on every
card and bound to nothing.~~ FIXED** (`98eb3f4`), to both of the prescriptions below.
`internal/ui/cardkeys.go` binds them, read *before* the composer and only when it is empty -
mutation-checked by deleting that condition, which turns
`TestTypingIntoAPaneDoesNotAnswerItsCard` red (named `…IntoTheRoom…` until BUG-2 moved every card
fixture into a conversation - in the room it had stopped being able to go red at all). The answer
key is gated on `Card.Answered()` and
never falls back to `FrameAllow`; an unanswered question hands the rune back to the composer as
the character it is.

Two things the fix had to add that the item did not anticipate, both recorded because they are
now surface:

- **A gated key with no way to un-gate it is the same lie one level down.** `Card.Answered()` is
  false until every question has a choice, and nothing bound `Card.Pick`, so `[a]nswer` would have
  been permanently unreachable. Digits `1`-`9` pick an option for the question the cursor is on
  and advance to the next; the card advertises `[1-N] choose`, and it advertises `[d]eny` *alone*
  until the answer is complete, so the affordance and the gate say the same thing.
- **`[d]eny` sends a fixed reason** (`cardDenyReason`), because the composer has to be empty for
  the key to be read at all and there is nowhere to type one. The reason reaches the model
  verbatim as the tool result and is the one channel for saying what to do instead of retrying
  the identical call, so a typed one is worth having — see the new item below.

**A session with a card outstanding must still not be stopped silently** - the second bullet of
the item above, which nothing in Task 10 closed because nothing in Task 10 stops a session.
`Cards.Len()` is the check and there is no detector to add later: closing stdin on an unanswered
ask ends the turn as a permission-rule denial on a *success* turn, byte-for-byte what a deliberate
deny produces. **Whoever binds a stop verb owns this.**

**~~Phase 2 Task 11 — there is no keyboard-only way to move the focus between the room and the DM,
and `⇥` is the key it would take.~~ FIXED** (`c2e29ec`), to the ruling below and to the brief's
"cycle key between open DMs" in the same binding.

`⇥` is now **next chat**: a ring of the room and every conversation that has been opened, in the
order they were opened. With one open it is a toggle between the two panes; with three it is how
you get between three research sessions. The next-blocked jump moved to **`⇧⇥`**, which the probe
already covered and `keyprobe_test.go` now asserts by byte (`\x1b[Z` → `shift+tab`); bubbles' text
area binds neither tab key, so nothing is shadowed.

*Three things the fix had to settle that the item did not anticipate:*

- **The takeover width.** Below 120 columns the room is not drawn, and a pane that is not drawn
  holding the keys is every keystroke landing in a composer nobody can see. One rule covers it -
  ⇥ focuses the next conversation, and a conversation you focus is one you can see - so at those
  widths focusing the room closes the DM. `resizePanes` carries the same invariant from the other
  side, because the terminal can cross the takeover with the room focused and no key pressed.
- **The ring cannot come off the `dms` map.** Go randomises map iteration, so the same three
  agents would come out in a different order on every press. `App.dmOrder` holds the open order
  beside it, copied on write for the reason `Fleet` copies its own order slice.
- **And it has to shrink as well as grow**, which the first cut got wrong: an id entered on the
  first open and never left — not on `⌃W`, not when the agent ended — while `⇧⇥` opens a
  conversation too, so at 15–30 agents the ring grew on its own as they blocked. Measured at five
  conversations: **five presses of `⇥` to get from the first back to the room**, which is the pane
  whose composer is the only place `@all` is typed. `closeDM` now takes the id out of the ring and
  keeps the transcript in `dms`, so the ring is *what is open* rather than *what has ever been
  opened*, and reopening puts it back at the end. **An ended agent stays in the ring** — its
  transcript is still worth reading, and dropping a row somebody is mid-way through reading is a
  worse failure than a ring one longer.
- **One accent among two composers is worth nothing** - it answers "where do I type" wrongly half
  the time. `Composer.Focused` draws the unfocused one in the ordinary pane border, false by
  default so a room with nothing beside it keeps the accent.

*The legend cost, which the item was right to flag:* 139 columns → **154**. Paid deliberately.
At every width where two panes fit, neither pane was ever wide enough for the whole legend, so
what is on screen is decided by the *order* rather than the length - and `⇥` sits fourth, above
open and close, because with a pane open it is the only way back to the room's composer. **What
an 80-column pane now keeps is `↵ ⎋ ⌃C ⇥ ⇧⇥ ⌃D`**, which drops `⌃W close DM` off the end. That is
survivable rather than accidental: 80 is a takeover width, the DM has the whole pane there, and
⇥ to the room is what closes it - so the behaviour ⌃W names is still reachable and still bound.
Worth re-checking if the takeover threshold ever moves below 80.

*The original item, kept because the ruling in it is the one that was followed:* `⇥` carried the
next-blocked jump because it was the only key bubbletea names that nothing else binds (`⌃⇧A`
produces no `KeyMsg` at all - `decisions.md` has the table), but `⇥` conventionally means *move
focus between panes*, and Task 11 puts a second composer on screen beside the room. Task 10 was
told not to pre-empt it.

**Phase 2 Task 10 review, Minor 1 — a pinned plan card goes back through glamour on every frame.** **CLOSED 2026-08-24 — PR #101 (`perf/pinned-card-render-cache`):** a value-key memo on `Cards` (mirrors `barKey`/`withBar`), keyed `{agentID,requestID,plan,width}`, pruned in `Reconcile`/`Settle`; ~7.0ms→~0.34ms/frame. Follow-ups: renderer-cache bound, mutex profiling, the ASCII-only test.
`App.roomPane` calls `Cards.View` per draw, and a `ShapePlan` body calls `render.Markdown` (in
`cards_blocks.go`'s `body`, since 2026-08-21 the height cap moved that render out of `collapsed` and
in front of it), under `internal/render`'s process-global mutex. Measured by the reviewer at
200x40 with real fixtures: **185 µs/op with no card, 207 with a question, 231 with a plan** — about
+25% per frame for as long as a plan is pinned, and Bubble Tea draws on the cursor blink and on
every mouse-motion message during a divider drag. It is *work per frame that is already work per
change*, which the non-negotiables name, and no benchmark covers a card being up.
**Left deliberately, and the reason is that the cheap fix is the wrong shape.** Caching a rendered
string on `App` needs invalidating from three places (cards change, fleet change, width change),
which is a second source of truth for something the room already solves properly by storing lines.
The right fix is to give a card the same `chunked`/`transcript` treatment the two conversations
have — render on change, store lines, draw a window — which is a design change rather than a fix
round. **Whoever takes it should add the benchmark first**: `BenchmarkView` uses `dmApp` and has
never had a card on screen.

**Phase 2 Task 11 — a DM opened for the first time is empty, while its badge says otherwise.**
`App.observe` appends to a DM only if one is already in the map, so an agent that has been talking
since before anybody opened its conversation has every line of it in the *room* and none of it in
the DM. Measured: one assistant line, `Agent.Unread == 1`, and the DM opens blank. Pre-existing -
Task 10 built it that way and it is the honest consequence of "a DM is created when it is opened" -
but the last-read marker makes it more visible, because the marker's whole promise is that a
conversation you come back to still holds what it held, and the *first* visit is the one case
where it never did.
*What it blocks:* nothing today. *What closes it* is one of two things, and they are different
sizes: build a DM for every agent the moment the fleet learns of one (simple, and it makes the
per-agent scrollback bound below apply to 30 conversations nobody has opened), or seed a DM from
the room's own `said` history at open time (no extra memory, but the room is filtered - thinking,
tool calls and diffs were never kept, so the seeded DM would be a room transcript wearing a DM's
header, which is worse than an empty one). **Do not take the second without deciding what a
partial transcript is allowed to look like.**
*One consequence is already handled and would need revisiting with it:* `DM.Leave` records nothing
for a conversation with no events in it, because a rule above the first line a transcript ever
carries has no "before" on the other side and reads as chrome. That case exists only because the
first visit opens empty; whoever closes this item can delete the guard along with the cause.

**Phase 2 Task 11 — the room has no last-read marker, and below 120 columns it can be the pane
that is away.** `DM.Leave`/`Resume` are on the DM because the DM is what gets replaced. At a
takeover width the *room* is the one off screen while a conversation holds the pane, so an hour at
110 columns accumulates room lines with no boundary to come back to. Small - the room is drawn at
every width a split fits, which is where this workflow lives - and the fix is the same three
methods on `Room`, driven from the same two places. Left because `Room` and `DM` share `transcript`
but not the marker, and hoisting it into `transcript` is the change worth making rather than a
second copy of `markerBefore`.

**~~Phase 2 Task 11 — a long session accumulates one last-read rule per absence.~~ CLOSED by
deciding it, in Task 11's fix round, rather than deferring it.** A conversation keeps the newest
`maxLastReadRules` = **3** boundaries. Three is the count the workflow names, and the bound lives on
the anchors, which is where a cap belongs once the anchor is plural — one line rather than a
feature.

*This entry is kept struck through rather than deleted because of how wrong it was.* It described
the clutter as real and asked for a bound, and in the build that filed it the clutter was already
bounded **to one, by a bug**: the anchor was a single slot, so a width change deleted every rule but
the newest. Whoever picked this up would have implemented a cap for a symptom that could not occur,
against a history that was already being thrown away. **An item that describes a symptom is only as
good as the mechanism behind it** — check the tree before acting on one, which is what this file's
own header has said since four of the last dozen entries turned out not to describe it.

**Phase 2 Task 11 — the cap trims on the next re-wrap rather than at the moment it evicts, and the
surplus is unbounded until then.** Lines are append-only, so a rule already rendered cannot be
withdrawn from the transcript — only the anchor can be dropped. **The transcript therefore keeps one
rule per absence for as long as nobody resizes, and a single re-wrap drops all but the newest three
at once.** Measured: ten absences with no width change leaves ten rules against three anchors, and
one widen removes seven. Stated at that size deliberately — the smaller framing ("a fourth absence
leaves four rules and three anchors") is the one somebody reads and predicts 4→3 from, and this
task was already burned once by an entry claiming a bound that was really a bug.
**The promise the tests hold is about the newest three**, which sit between the same
two events at every width; what a re-wrap does to the ones beyond the cap is apply it, which moves
the screen toward the model rather than away from it and never moves a boundary that survives.
`TestTheCapIsWhatAReWrapRestoresTheTranscriptTo` pins both halves so it is a decision rather than a
surprise. *What closes it* is the view-time overlay: drawing the rule in `transcript.view()` from
the anchors instead of storing it as a block removes the incremental/re-wrap duality entirely and
makes the cap exact at every instant. The cost is that `bottom`, `atBottom`, `scrolledUp` and `view`
all have to count the injected rows, which is the scroll arithmetic two shipped guards protect — so
it is a change worth designing rather than a follow-up patch.

**Phase 2 Task 10 review, Minor 1 — still open, and its own prerequisite is still missing.**
`BenchmarkViewOnePane` and `BenchmarkViewTwoPanes` landed in Task 11 and neither has a card on
screen either, so "whoever takes it should add the benchmark first" is still the next step. What
Task 11 adds is a number to set it against: a two-pane frame is 299-317µs, so the reviewer's
measured +45µs for a pinned plan is ~15% of a split frame rather than ~25% of a single one.

**Phase 2 Task 10 review, Minor 4 — `App.regions()` reconciles a divergence no production path can
produce. RULED at Task 11: it stays, and the alternative is now worse rather than better.**
`Layout.ShowDM` is written only by `Layout.WithDM`, called only from `openDMWith` and
`closeDM`, both of which set `a.open` in the same block — so `r.DM > 0 && a.open == ""` is
unreachable today, and `TestAPaneIsNeverDrawnForADMThatDoesNotExist` builds that state by hand.
`layout.go`'s own header records three such clauses written and deleted in Task 9 for exactly this
reason. **Kept, and this is the argument for keeping it**: `Layout` is a pure value with a public
field, so the state is reachable by any caller rather than by any terminal size — the deleted Task
9 clauses were unreachable by *arithmetic*, which is a different thing — and what it prevents is
not a wrong width but a nil dereference inside the draw loop, since a zero `DM` holds a zero text
area and `View` dereferences one. The alternative that removes the duplication rather than
reconciling it is to derive the DM column from `App.open` instead of from a second boolean, which
means changing `Layout.Regions`' signature and is a Task 11 conversation. Flagged so the
whole-branch review can rule.

*Task 11's ruling: keep it, and the second reason is new.* `Regions` is now also read by
`showRoom` and by `resizePanes` to answer "is the room on screen", which is a **focus** question -
so threading `open` through the signature would make the pure layout depend on which conversation
is beside the room, and the whole reason every responsive rule here is a table test is that
`Layout` knows nothing about content. The duplication stays and `App.regions()` stays the one
place the two facts are reconciled.

**Phase 2 Task 10 — the room has no default addressee, so a draft with no `@` is refused.**
`core.Resolve` takes one and this build passes `""`. The manager session is what it is meant to
be (§7) and there is no manager yet, so the alternatives were the attached session - which would
make a room message silently go to one agent, drawn unattributed, which is the misroute
`Route.Resolved` exists to prevent - or a broadcast, which is N turns nobody asked for. **This
leaves when the manager lands.** The composer says `→ @name or @all` meanwhile, so the refusal is
visible before ↵ rather than after it.

**Phase 2 Task 10 — `[d]eny` cannot carry a typed reason.** The reason reaches the model verbatim
as the tool result and is the one channel for saying what to do instead of retrying the identical
call; a constant is a weaker answer than the operator's own words. The obstacle is the gate: card
keys are read only when the composer is empty, so the draft cannot be the reason. **What closes
it is a shape for "deny with this text" that does not re-open the letter-key hazard** - most
likely `d` on a *non-empty* draft, which is exactly the branch the gate exists to forbid, so it
needs designing rather than adding.

**Phase 2 Task 10 — a terminal shorter than a pane's floor still over-draws.** `Room.minHeight`
and `minDMHeight` are floors below which the pane stops shrinking rather than drawing a broken
box, which is deliberate and predates the room - but the frame is then taller than the terminal
and the alt screen scrolls on every draw, which is the failure the notice row's one-row rule
exists for. Reachable only at absurd sizes (7 rows and under), and now true of two panes rather
than one. **The cheap fix is a `MaxHeight` on the whole frame beside the `MaxWidth` that is
already there**, at the cost of cutting the composer off instead. Not taken, because "the pane
floors" is a documented decision and reversing it is not this task's to reverse.

**Phase 2 Task 10 — the sidebars answer no clicks.** `App.press` takes an x coordinate and
`Layout.Hit` resolves it to a region, but selecting a *row* needs the y coordinate and the row
arithmetic each sidebar does privately. A click that selected the wrong agent is worse than one
that selected none, so the rows are inert. It matters more than it looks: `⌃D` opens the DM of
whoever the roster cursor is on, and `⇥` is the only thing that moves that cursor.

**Phase 2 Task 10 — `⌃D` and `⌃W` shadow two of the text area's own bindings.** bubbles v1.0.0
maps `ctrl+d` to DeleteCharacterForward and `ctrl+w` to DeleteWordBackward, and `App.key` takes
both before the composer sees them. Deliberate - they are the keys the brief names for the panes,
and the composer is one line high where forward-delete is rarely reached for - but somebody with
readline muscle memory will find `⌃W` does not delete a word. `⌃G`, `⌃R` and `⇥` shadow nothing.

**Phase 2 Task 5 left one thing undesigned, deliberately: multi-select.** `core.AskDetail` cannot
express it and `Card.Choice` is one index per question, because every recorded question carries
`multiSelect: false` and the comma-separated answer encoding for `true` is a read of the 2.1.226
binary that §9 of the question findings explicitly forbids building against. A multi-select
question currently degrades to one chosen label, which is a real answer the tool accepts. **This
leaves only when somebody records a `multiSelect: true` ask** — at which point `AskDetail` grows
the flag, `Choice` grows a set, and `multiSelect` joins the policed vocabulary.

**~~Task 6 (daemon)~~ and ~~Task 10 (wire-up)~~ — both blocks were entirely done and still
listed here as outstanding until 2026-08-09**, when the final review verified all fifteen items
against the tree. That is the failure this file exists to prevent, so it stays recorded rather
than deleted. Every item shipped: the per-client writer plus write deadline, both cancel
mechanisms, the restart reaper, `ReadFrames` called once at construction, `render.Prime()` before
`tea.NewProgram`, the `WindowSizeMsg` write-back, `textarea.Blink`, the single-source rule,
`go mod tidy`. Two carry residuals recorded elsewhere in this file: the client-side `writeMu`
still has **no** write deadline (the daemon applied `wire.go`'s prescription, the client never
did), and never-snapshot-a-`Composer` is now *unassertable* through the exported surface rather
than merely untested.

**Task 10, added after Task 6's C1 fix** — the client must wait for `FrameHello` on the one
connection it already holds, and must **not** poll by re-dialling. Measured here on darwin:
128 connects succeed against a bound-but-never-accepting listener (the shutting-down daemon)
and **closing a pending connection does not give its backlog slot back** — dial #129 is
refused after 128 successful *closed* connects. So a 100 ms retry poll exhausts the backlog
in ~12.8 s, inside the 30 s grace, and the next probe's `ECONNREFUSED` is exactly what
`listen()` reads as a crashed daemon's stale socket: it unlinks, binds, and `reapOrphans()`
SIGKILLs the live fleet. A retry loop written the obvious way re-opens the Critical the C1 fix
closed. Full numbers and the resolution (hello-or-EOF on the held connection, one slot, no
timeout guess) in `task-10-interfaces.md` §5b.

**Left open at CI's merge**, from Task 11, each a deliberate decision rather than an oversight:

- **~~`cmd/wake` is at 76.8% — the only package under the advertised 80% — and `attach` is at
  0%.~~ FIXED**, and it was already fixed before Phase 2 Task 10 arrived to close it: the
  `Makefile` carries no exemption at all and says why in a comment — `attach` now waits for the
  daemon to confirm the spawn before it builds a TUI, so its refusal paths are reachable from a
  test and the package cleared the floor on its own. **Measured at Task 10: 83.2%**, with the
  gate at 80% and no package named. The gate keeps its two exemption variables because empty is
  how "no package is exempt" is spelled, which leaves the seam visible for the next package that
  needs a temporary floor.
- **The 1h soak belongs on a schedule or a `workflow_dispatch` button, not on every PR.** The
  reasoning for keeping it out of PR CI is sound and recorded in the workflow header — a 30s run
  on a shared runner is the configuration that *passed* the quadratic it was written to catch —
  but "not per-PR" is not the same as "never", and the cheapest honest version is a
  dispatch-only job that costs nothing until a human presses it.
- **~~`make cover` lacks `-count=1`.~~ FIXED** in Task 11's fix round.
- **`.golangci.yml` has no `formatters` section**, so `gofmt`/`goimports` are unchecked — they
  moved out of `linters` in v2. Nothing is misformatted today; a misformatted file would not
  turn CI red.
- **~~`GOOS=windows go build ./...` is not in CI.~~ FIXED**, and it earned its place: a planted
  type error in `reap_other.go` leaves **both** `make build` and `go build ./...` green on darwin,
  and only the cross-compile catches it. It protects **4** of the 5 stub files — `go build` never
  compiles test files, so `procgroup_other_test.go` is still compiled by nothing on any platform.
- **~~`ensure_test.go` and `roster_test.go` build `$TMPDIR`-relative paths ~35 bytes deep.~~
  FIXED**, and the estimate in this entry was wrong: shortening bought **6 bytes, not ~15**
  (budget 68 → 74). `/.wake/daemon.sock` is production's own shape and the root floor is
  `$TMPDIR/` + prefix + suffix, so 29 past `$TMPDIR` is the floor rather than an accident.
  *What replaced the guesswork:* `TestEverySocketPathThisSuiteBuildsFitsInSunPath` measures the
  real paths four helpers build and asserts them against `maxSocketPath`, firing at 75 and
  passing at 74 — exactly where the package itself breaks. Renaming the constant is now a
  compile error rather than a `sed` that silently finds nothing.
  *Still open, and it is why a one-package guard protects the whole tree:* `cmd/wake/main_test.go`
  and `internal/rpc/conn_test.go` still build sockets with a 1–10-digit `MkdirTemp` suffix and are
  in no budget test. They are safe only arithmetically — worst cases +18 and +20 past `$TMPDIR`
  against `internal/daemon`'s +29 — so the daemon guard is the binding constraint at every draw.
  That ordering is load-bearing and nothing enforces it.

**~~A 15-second timeout under load, seen twice.~~ DIAGNOSED AND FIXED** (`34e562f`). Kept in full
because the diagnosis is worth more than the fix. It was **two mechanisms and neither was a race
in the product**: `testClient.await` consumed frames destructively, so a frame one wait ate was
gone for the next — load merely made the interleaving likely; and `waitForExit`'s 15s bound was
half `defaultQuitGrace`, so any aborted test still holding an un-stoppable agent paid a second,
misleading 15s on top of the real failure. Under 24 busy loops on 18 cores, one variable: **13
failures over 6 no-race runs and 6 over 3 race runs, both to zero**, and a loaded run drops from
30–61s back to 15–16s because every failure was a full 15-second timeout. Independently
re-verified here: 0 failures in 3 runs under 18 loops.
*Why the daemon was not changed*, and it is a product argument rather than a test one:
`client.enqueue` drops on a full queue and the spawn confirmation is among what it can drop, so a
client must handle "the confirmation never came" regardless — **an ordering guarantee on a lossy
channel is not something anyone can write code against.** Measured over 1,000 spawns: reply first
1,000, event first 0. The reorder was never what failed.
*The original entry, kept because "one sighting is a flake, two is a pattern" is the lesson:*

1. `waitForExit` timing out in the no-race lane while a `-race` soak ran concurrently; clean in
   34 subsequent runs under the same load.
2. `TestServeLeavesNothingRunningWhenItReturns` timing out once under self-inflicted load; 0/12
   in isolation.

Both are the daemon package, both are a 15s `testTimeout`, both only under contention, neither
reproducible on demand. One sighting is a flake; two of the same shape is a pattern that has not
been diagnosed. The suspicion worth testing is that `testTimeout` is a fixed wall-clock bound
over work whose duration scales with machine load — which is exactly what a shared CI runner
supplies. It is also the reason the soak stays off PR CI, so the two decisions are linked.
*Next step is a diagnosis, not a bump:* raising the bound would make the symptom rarer and the
cause less findable. Instrument what actually takes the time when it fires.

**Task 11 (CI)** — `session_test.go`'s orphan helpers leave 30-second processes behind after
`go test`. Bounded, but CI should not accumulate them. **And a coupling nothing states:**
`testTimeout` (15s) must exceed `quitGrace + shutdownWait`, because
`TestARestartDuringShutdownDoesNotReapTheFleetItIsWaitingFor` legitimately costs both.
`shutdownWait` is a `const` with no test seam, so a future bump silently eats the 7s of slack
round 3 just bought back. Derive `testTimeout` from the two, or assert the relation once — CI on
slower hardware is what finds this otherwise.

**Left open at the daemon's merge**, all Minor, all with the reviewer's agreement that they are
Minor. Recorded here because they were living in a task report, which is not where deferred work
belongs:

- **`reapOrphans` still clears entries it could not verify.** **Reap path CLOSED 2026-08-25
  (`fix/bug28-orphan-recovery-retry`, BUG-28):** `probeAgent` is now the three-valued core
  (`reapUnknown`/`reapAlive`/`reapGone`) that `verifyAgent`'s bool wraps, and `reapOrphans` retains a
  record it could not check — or one whose confirmed-live `KillGroup` failed — clearing only the
  confirmed-gone, recycled, and reaped (`rosterFile.retain`, which seeds `r.live` so a retained record
  is re-persisted through this daemon's own writes rather than clobbered by the first spawn). No
  lifetime bound was needed for the reap path: a working `ps` resolves every record to gone-or-alive
  and clears it every start, so only a *perpetually* broken `ps` retains — where the records name real
  un-reapable orphans, which is the case forgetting them was the bug. **Remaining half — the
  clean-shutdown clear.** `server.go`'s `shutdown` still ends in an unconditional `s.roster.clear()`,
  so a retained orphan is re-forgotten the next time *this* daemon exits **cleanly** (`⌃Q`/`wake stop`)
  rather than crashing: the retry survives a crash of the daemon that retained it but not a clean stop.
  Strictly better than before (which forgot at startup, 0s of retention), but not full closure.
  Closing it is a decision about `wake stop`'s own contract — its status check reads the on-disk
  roster, so preserving an orphan record there makes `wake stop` *report* a leaked process it could
  not kill, where today it reports a clean slate — and about whether `⌃Q` and `wake stop` should
  differ; left to the owner. Original note, for the history: it was deliberate while `verifyAgent` was
  a `bool` conflating "already gone" (drop it) with "I could not ask" (keep it).
- **`unreachable` is permanent.** `noteUnreachable` sets it, nothing clears it, and `quietFor()`
  returns 0 for such an agent so `suspects()` never re-asks. Round 3 removed the false positive
  that could set it wrongly; it did not remove the permanence, which is *why* the classification
  has to stay exhaustive.
- **One trusted-answer path remains in the reaper.** A `ps` that accepts the flags without error
  yet ignores the pid *selection* would return a line, and `strings.Contains(argv, sessionID)`
  against a full listing could match anything. Contrived; worth knowing it is the last assumption
  in a chain that ends in `SIGKILL`.
- **`gapNotice` is the bare word `"dropped"`**, matched against arbitrary error text, so an agent
  error containing it fails an unrelated test. A distinctive token (`wake-gap:`) costs nothing.
- **~~`TestEveryFrameKindIsDistinct` cannot fail for a kind that is not in its hand-written
  map~~ FIXED** (`0781b73`), to its own prescription and for a concrete reason: that commit adds
  `FrameInterrupt`, which is exactly the kind that would have slipped past it.
  `TestNoFrameKindIsMissingFromTheDistinctnessMap` parses `wire.go` and `lifecycle.go` with
  `go/ast` for `Frame… = "…"` constants and requires each in the map, with a floor so the scan
  cannot silently find nothing. Mutation-checked by removing an entry from the map.
- **`watchStates`' gap-failure branch is itself unexercised** — deleting it leaves the package
  green, because no test drives a real drop while `watchStates` reads.
- **`daemon.FleetOnDisk` is unbounded by its own timeout**: `r.alive()` per record at up to
  `probeTimeout` each, serially, so 30 dead pids and a wedged `ps` is ~60s inside a function
  whose sibling path is bounded at 3s. `wake status` is the command someone runs *because*
  something is wrong, and `⌃O`'s detach line calls it. **Bare `wake` is the third caller and
  it raises this from annoying to front-door**, added 2026-08-11 by park/wake Task 8's fix
  round: `fleetToReopen` reads it on both of `daemon.Status`'s non-answering paths, so the
  worst case is the 3s status timeout *plus* this, before the command decides whether to open
  a room or spawn. Note the exposure is not new — the no-daemon path already reached it
  through `Status`, which is the ordinary case after `⌃Q` — so what the third caller changes
  is who notices. *What closes it:* one deadline over the whole loop, or `idsInUse`'s move
  (one `ps` for the whole question) applied to `r.alive()`, which is the same fix `resumeSafe`
  already took for the same shape.
- **`daemon.log` is never rotated.**

**~~Task 12 (`!cmd`) shipped its seam unwired, deliberately, and this is the wiring.~~ FIXED.**
`App.submit` calls `a.bang(text)` before anything else and `App.Update` folds `bangResultMsg`,
both merged before Phase 2 Task 10; `bangkey_test.go` holds the Enter key from both sides. The
two snippets below are what landed, and they are kept because the *ordering* argument in them is
still binding on anyone editing `submit`.

In `App.Update`'s switch, beside the other message cases:

```go
	case bangResultMsg:
		return a.bangResult(m), nil
```

In `App.submit`, after the blank-draft check and **before** the `a.ended` check — a bang is
addressed to nobody, and a session that has ended is still one somebody wants to run
`!git status` in:

```go
	if a, cmd, ok := a.bang(text); ok {
		return a, cmd
	}
```

Both were applied, verified and reverted before the commit: the whole suite, `make cover` and
`golangci-lint` were green with them in place. Two tests go with them and could not be committed
without them — an Enter that reaches `pipeClient` and asserts **no `rpc.FrameSend`** was written
(removing the interception fails it with the bang's own text in the message), and an Enter with an
ordinary message that asserts the frame still arrives. `internal/ui/bang_test.go` reaches
everything else through `App.bang` directly, so the Enter key itself is the one thing this branch
does not assert. Bodies are in the task-12 report.

**~~`bangDir` returns the empty string.~~ FIXED** (`98eb3f4`) by Phase 2 Task 10, which is what
the item said the room owed it. `App.bangDir` now reads `Agent.Dir` for the conversation the bang
was typed into, which comes from `rpc.SessionStatus.Dir`. A bang typed into the *room* still gets
the empty string, and that is the honest answer rather than a residual: the group chat is not
addressed to any agent and has no directory, so it runs where `wake` was started.

**~~Phase 3 park/wake — `internal/core/session.go` has no room for the third identity arm, and the
seam to split on is already named.~~ DONE** (`af5dce0`), by park/wake Task 1, exactly to the
prescription below and on the schedule it set: the split first, the arm second. `internal/core/argv.go`
now holds `identityArgs`, `sameSession` and `buildArgs`, moved verbatim, plus a new exported
`SessionArgvMarkers`. **Three numbers in the entry below were re-derived rather than trusted, as it
asked:**

- *"117 lines out, measured"* — it is **119**, counting the blank line that separated the block
  from `Start`. `session.go` went 767 → 648 on the move, and 654 once its header said where the
  argv went.
- *"`Start` is at exactly 50 lines signature-to-brace (48 in the body)"* — **still 50 and 48**. The
  split did not touch `Start`, so the entry's own warning ("count again rather than believing this
  sentence") holds and the count is unchanged.
- The largest non-test file in the tree is `internal/ui/app.go` at **709** — observed, on
  2026-08-10, along with `session.go` at 654, `event.go` at 565 and `agent.go` at 562. What is
  *asserted* is the **bound**, not the ranking: `TestNoNonTestFileCrossesTheHardMax` walks every
  non-test `.go` file against the 800 max, and it is the first thing that has ever checked that
  rule. `app.go` could reach 799 with this sentence silently wrong, which is why the two halves are
  now separated — the first draft of this entry said the whole thing was "asserted rather than
  observed", which claimed slightly more than the test does.

*Two guards over it were beaten in review and rebuilt; the rebuild is the interesting part.* The
static assertion first shipped over `identityArgs` and asserted which `Config` **fields** it may
read, and three documents said that closed a value space. It does not: a narrowing can key on an
*allowed* field, move to `buildArgs` (which is the function that actually assembles the argv), or
read no field at all. All three were green against the whole tree and two produced a punished argv
shape. The guard is now scoped to the **argv path** — derived by walking the call graph — and
constrains the **shape of every predicate**, not only its inputs. That is rung 5, written up in
`decisions.md`. And both tree-wide walks are now held to `go list ./...` rather than to a file
count, because `goFiles`' skip list matches a base name at any depth and one entry hid a whole
package from three guards at once.

**Still open, and inherited rather than introduced:** `airlock_test.go`'s own leak check calls
`goFiles` directly and carries no coverage floor of its own. The practical hole is closed — all
three guards live in `internal/core`, so any `skipDir` edit now fails that package's suite through
`argv_test.go` — but the airlock's check would still be individually satisfiable if it were ever
moved to another package. Cheap follow-up: have it call `nonTestGoFiles` too.

*What the entry did not anticipate, and the next task should know:* the ruling it wanted turned out
to need two decisions the prose did not make. The guard reads **string literals out of the AST, not
bytes** — six non-test files (`cmd/wake/attach.go`, `internal/rpc/wire.go`, `internal/daemon/reap.go`,
`reap_unix.go`, `spawn.go`, and `internal/core/session.go` itself) name one of these flags in a
comment explaining why it matters elsewhere, and a byte scan would have made those sentences
unwriteable. And it walks `goFiles`, `airlock_test.go`'s own walk, rather than a fresh
`filepath.WalkDir` — that one skips `.worktrees`, and a hand-rolled walk reports a *sibling
branch's* `argv.go` as a second file spelling the flags.

*The original entry, kept because its reasoning is what the task followed:*

Fork took `session.go` from **657 to 767 lines** against the project's **800 hard max**. The next
change to that file is park/wake's arm in `Session.identityArgs` — a bare `--resume <id>` with no
`--session-id` — and `identityArgs`' own doc comment says in as many words that it *belongs in
this switch*. On fork's evidence a well-documented arm in that switch costs **~40 lines** of
comment and code, which lands at **~807**. So park/wake does not "get close" to the max; it
**crosses it**, and the split is that task's problem whether or not it wants one.

*Do not do the split now, and do not defer it past park/wake either.* There is no failing test
behind it today and moving `buildArgs` away from `Start` mid-feature is a refactor with no guard,
so nothing between here and park/wake should touch it — Task 2 adds `ParentID` plumbing in `rpc`
and `daemon` and will not open this file at all. But **park/wake does the split first and its arm
second**: this is a prerequisite of that task rather than a suggestion to it, because the
alternative is landing the arm and then discovering the file is over the max with the arm already
in it.

**The seam, so it is not rediscovered:** `identityArgs`, `sameSession` and `buildArgs` move to
`internal/core/argv.go`, leaving `session.go` with process lifecycle only. That is **117 lines**
out, measured, and it is a clean cut — the three are called from exactly one place (`Start`) and
share no state with the pump, the write path or the ending path. Doing it first also puts
park/wake's arm at ~40 lines into a ~120-line file rather than at the top of an 800-line one. It
also gives the *second* airlock leak a single home: `airlock_test.go`'s SCOPE paragraph records
that Wake's fifteen claude-specific CLI flags are a leak of a different shape from Claude's JSON
and "want their own ruling", and a file whose whole subject is the argv is where that ruling would
be enforceable rather than stated.

**One number to re-derive rather than trust:** `Start` is at **exactly 50 lines** signature-to-brace
(48 in the body) against the "functions under 50 lines" convention. Fork added 4. It is at the
boundary on one reading and over it on the other, and the next line added to it crosses both.
Counted from the source on 2026-08-10; count again rather than believing this sentence, which is
the rule the same task had to apply to a flag count that had been wrong in a comment since
`ec2748e`.

**Phase 3 park/wake — the slash layer exists and `/new` and `/add-<name>` still do not.** Added
2026-08-11 by park/wake Task 6. `internal/ui/slash.go` is the layer `docs/goals.md` §3 says the two
missing verbs need — a closed set, a name, an argument, a target out of the fleet, and a frame — and
adding one is an entry in `commands`, plus the two things that entry drags with it:
`wakeCommandCount`, so a command cannot be added without the passthrough guard being looked at, and
a word that is not one of claude's, which `TestWakeOwnsNoCommandClaudeAlsoHas` checks the other way
round. What is *not* here and is the actual work: `/new` needs a `FrameSpawn` written from the TUI
(the frame exists; nothing in `internal/ui` writes one) and a directory argument the room has no
notion of, and `/add-<agent-name>` needs a group model the left sidebar does not have. **Neither
belongs to a phase today**, which is the sentence `goals.md` §3 already carries; this entry exists
so the next person finds the layer rather than building a second one beside it.

**And the layer cannot route `/add-<agent-name>` as `goals.md` spells it — the guard that closes the
value space forecloses it.** Found 2026-08-11 by Task 6's review, and it is worth stating plainly
because both this entry and `goals.md` described `/add-`'s missing work as the group model, which is
true and is **not** the routing problem. `slash` cuts the first word at a space, so `/add-sydney`
gives `word = "add-sydney"`, `commands` is keyed on the exact bare word, and it falls through as a
message — correctly, because that is the passthrough rule working. The two shapes that *would* reach
it are a prefix rule and a second lookup, and those are exactly what `slashguard_test.go` makes a
build failure: they are the shape of both mutants the review used to swallow `/r` and `/deploy`. So
whoever builds it picks one of two, deliberately:

- **change the syntax to `/add <name>`**, which routes today with no guard change at all — and is a
  change to the string `goals.md` quotes from the founding message, so it is the founding text that
  needs the note rather than the code;
- **or reopen the guard**, which means finding a rule that admits `add-<anything>` and still cannot
  admit `/r`, `/deploy` or anything in `.claude/commands/`. Note the near-miss that makes this
  sharper than it looks: **claude ships `/add-dir`.** A prefix rule on `add-` would shadow it, which
  is the exact failure the whole fence exists to prevent, arriving through the verb the fence was
  built to make room for.

`/new agent in <dir>` has no such problem — `word = "new"`, `arg = "agent in ~/p"`.

**The passthrough list is five hand-written claude commands.** `/model`, `/clear`, `/compact`,
`/context`, `/help`. Four are recorded as surviving stream-json mode; `/help` is not, and it is in
the list because it is the command somebody types first. The set claude actually ships is on the
`init` frame as `slash_commands`, **which the airlock decodes as of 2026-08-15** — so the remaining
work is the *guard*, not the decode. **Note what the list is and is not load-bearing for**:
it is a sample, and a sample cannot close "does this string start a Wake command", so nothing rests
on its completeness — `slashguard_test.go` closes that statically. What the list *does* close is the
overlap, in the one direction a static check cannot reach: whether a word Wake takes is a word
claude also has.

**`/resume` is the first shipped surface that sends `rpc.FrameWake`, and it did not bring the
row-before-process fix with it.** Added 2026-08-11 by park/wake Task 6. The `replaceParked` entry
above is the one this expires, and it carries the dated correction: read it there rather than here,
because two copies of a precondition is how one of them goes stale. Also still open and unchanged
by this task: **nothing parks anything from the shipped surface**, so today `/resume` can only bring
back a fleet a restarted daemon restored out of `parked.json`. **`noParkedSessions` therefore names
no key**, against the obvious wording and against this task's own brief, which prescribed *"⌃C parks
the conversation you are in, and ⌃Q parks the fleet on the way out"* — both true of the design and
neither true of this build, where `⌃C` **detaches**. The task that binds those keys gains the
sentence in the same change, and `TestResumeWithNothingParkedNamesNoKeyThatDoesNotPark` fails until
it does.

> **CLOSED 2026-08-11 by park/wake Task 7.** `⌃C` writes `FramePark` and `⌃Q` writes
> `FrameParkAll`, so an operator can park from the shipped surface and `/resume` has something to
> find. `noParkedSessions` now carries the prescribed sentence verbatim, and the guard was inverted
> rather than deleted: `TestResumeWithNothingParkedNamesTheKeysThatPark` requires both glyphs *and*
> checks each against `legendEntries`, so the sentence cannot outlive the keys it names — which is
> the failure mode it was written against, read the other way round. The row-before-process fix
> landed in the same change; see the `replaceParked` entry.

---

## Test quality

These are tests that pass without proving what their name claims. Each was caught by review,
none is load-bearing today, all will mislead someone eventually.

- `TestToolCallCoversEveryMappedTool` iterates the map it validates, so `Glob`, `WebFetch` and
  `WebSearch` mappings are asserted by **nothing**.
- `TestEncodeAllowCarriesUpdatedInput` compares `any` values with `!=` — a nested map or slice
  **panics at runtime**; a numeric key silently mis-compares `float64(1)` against `int(1)`.
- `render_test.go:50` measures with `len([]rune(...))` — fine while the fixture is ASCII, wrong
  the moment it isn't.
- `waitForGoroutines` compares against a process-global baseline taken at test start, so a
  goroutine unwinding from a previous test can mask a same-sized leak here. **Still open for the
  untagged tests.** The soak solves it for itself with `settledGoroutines`, which takes the
  *minimum* over a settle window so a straggler drags the baseline toward the truth instead of
  inflating it; porting that to `waitForGoroutines` is a small, separate change.

---

## Unverified capability — needs recording, not coding

The project's rule is that unrecorded behavior does not get designed around. These are the
outstanding ones, in priority order:

1. **Interrupting a session with a permission request outstanding.** The interrupt spike named
   this "the likeliest real collision — close this first." Wake will do both constantly.
2. **~~What a subagent actually looks like on the wire.~~ RECORDED** (`47d3815`) — see I6 above
   for what it found and what it costs. Its own §11 lists thirteen things it did not settle, and
   **it names the highest-value one itself: what decides async when nothing asks for it.**
   `run_in_background: true` went async twice and `false` foreground six times, but the dispatch
   that **omitted the key entirely also went async** — and nobody knows whether the CLI defaults
   it, `--brief` nudges the model, or the prompt did it. That matters more than the rest combined:
   **the async path is the one that breaks `Stop()` and `KindTurnEnd`, and Wake cannot opt out of
   it.** An async subagent streams past its own `result` and past stdin closing, so "the turn
   ended" does not mean "the work stopped", and `Stop()` — which closes stdin and lets the
   in-flight turn finish — can return while a subagent is still writing files. That is a direct
   question for the daemon's ending contract and should go to the next cross-cutting review.
   Also unsettled and worth knowing: whether `parent_tool_use_id` **nests** for a subagent that
   itself calls `Agent` (Wake's sidebar is a flat list, so this is a design question, not just a
   field); interrupting a subagent; a **denied** subagent tool call; `SendMessage` to a running
   subagent; whether a subagent can outlive the process; and `output_file`, which names an on-disk
   JSONL transcript of every subagent that Wake has never opened.
3. **`--replay-user-messages`.** `CLAUDE.md` lists it as a flag Wake depends on. What it emits
   when enabled — frame type, whether the echo is distinguishable from a genuine user turn,
   whether it sets `isReplay` — has never been observed. Task 10 owns the single-source rule and
   currently has no fixture behind it.
4. **`rate_limit_info.status`** has exactly one observed value (`"allowed"`). Any fleet
   indicator built on it is designed against a single sample, and least trustworthy in exactly
   the situation it exists to catch.
5. `cancel_async_message`, subagent interrupts, queue depth > 1, the interrupt `reason` field.

---

## Polish

Comment precision, naming, small duplication. Batch these into one pass rather than
interrupting a task for any of them.

`typeInto` duplicated inline (composer_test.go:64,141) · `"\n "` separator unnamed
(composer.go:99) · `hintIndentWidth` documents a literal it does not drive · `composer.go:48-50`
rationale off by one at width 7 · probe-derived marking invisible to `go doc` for
`EncodeUserMessage`/`EncodeDeny` · one Task 3 test exceeds its own file's 50-line and 4-level
limits · `truncate` returns the full string when `max<=1` · `Prime` is an exported symbol
outside its brief's list · three findings-note items (nullable `messaging_socket_path`, a §13
path inconsistency, "exit code 1 / zero bytes on stdout" still testimony).

---

## Power — added 2026-08-09 as a design constraint

**Update, same day: three of the hypotheses below were measured and two were wrong.** Kept in
full underneath, because being wrong about which costs matter is the point of writing them down.

- **Mouse tracking — no fix available, and my reasoning was off.** bubbletea v1.3.10 offers only
  1002 and 1003, and **1002 already reports motion only while a button is held**. There is no
  wheel-only mode to drop to. A finding, not a fix.
- **`textarea.Blink` costs ~0.05 % of one core.** Real but small, and memoizing `View` to recover
  it was judged not worth the stale-frame risk. I agree.
- **The render path was the real cost, and it is now fixed** — 4.4× less CPU under load
  (1,024 frames: 350 ms → 80 ms), a 40-column drag from 40 re-wraps down to 1, and **no cost at
  idle or per frame**: `View` 247–257 µs → 248–268 µs, idle CPU 0.467 % → 0.452–0.499 % of a core.
- **`tea.WithFPS(30)` — offered, and I am declining it.** It would cut idle from 0.31 % to 0.20 %
  of one core, at up to **33 ms of added latency on the visual echo of a keystroke**. Wake's
  design non-negotiable is that it feels like Claude Code, and a composer that lags a fast typist
  fails that for 0.11 % of a core — against 30 `claude` processes, that is noise. **Revisit only
  if idle measurement ever shows the TUI mattering next to the fleet.**
- **Still the one that scales, and it is measured now:** the daemon's ~86,400 `ps` spawns a day at
  30 agents. Phase 2 Task 16 counted **1.02 spawns per quiet agent per tick** against a real daemon
  with a counting `ps` on `PATH`, which is exactly the arithmetic this note predicted
  (`internal/daemon/psrate_test.go`). The TUI beside it idles at 0.80 % of one core with 30 agents
  and a manager. That is where the next power work goes, not the TUI, and the gap is now a
  measured one rather than an argued one.

*"Claude code is already a battery eater, we want wake to be as lightweight as possible,
especially since it's only a TUI."* Now a non-negotiable in `CLAUDE.md`. Nothing below is
measured yet except where it says so — **that is the first job**, because this project's rule is
that an asserted number is worth nothing.

**The daemon's idle cost is the one that scales with the fleet, and it is no longer arithmetic —
it was measured on 2026-08-12 and came out at exactly the figure below.** The paragraph is left as
written because the prediction and the measurement agreeing is the point; `docs/notes/decisions.md`
carries the observation and what was made to move to earn it. `livenessInterval()` is `silenceLimit/4` clamped to `[50 ms, 30 s]`, and the
default `silenceLimit` is 5 minutes — so the tick lands on the 30 s clamp. `probeQuietAgents`
then runs `ps` **per quiet agent**, and every agent is quiet once you stop typing. At 30 agents
that is **30 process spawns every 30 seconds — about 86,400 a day** to learn nothing has changed.
The three-valued liveness answer is right and worth keeping; *asking it on a timer for every
agent* is what needs rethinking. A single `ps` listing all pids at once, or probing only agents
something is actually owed from, are both cheaper and neither weakens the guard.

**The TUI's idle cost.** `textarea.Blink` (`app.go:129`) re-renders forever while nothing happens,
and `View` (`app.go:387`) is unmemoized with a value receiver, so a blink redraws the **whole
transcript** rather than the composer row. The blink is deliberate — a static cursor reads as a
hung app — so the fix is to make the redraw cheap or narrow, not to remove it.

**Mouse tracking costs more than it buys.** `attach.go:85` passes `tea.WithMouseCellMotion()`,
and `app.go:333` uses the mouse for exactly two things: wheel up and wheel down. Cell-motion mode
reports drag as well, so dragging a selection emits a message per cell crossed, each running
`Update` and a full `View`. If a cheaper mode still delivers wheel events, take it.

**Render is the expensive thing and it is process-global.** `internal/render` holds one mutex for
every session, and a width change costs **279 ms at 3,000 events** (measured). The renderer cache
is unbounded and a terminal resize drag caches an entry per intermediate width, permanently.

**Already cheap, checked while writing this:** `releasePoll` (20 ms) and `startPoll` (25 ms) are
both short-lived and bounded to a single operation — they are not idle costs. `client.enqueue`
never blocks and never spins. The daemon does no work per event beyond fan-out.

*What would settle it:* an idle-CPU measurement of both processes — TUI attached, one agent
parked, nothing happening — against the same with 30. Until that exists, everything above is a
hypothesis with arithmetic behind it, not a finding.

---

## From the identity work (2026-08-09)

**A note on how these got here:** the implementer wrote them in its own worktree's
`docs/notes/deferred.md` — and `notes/` is **gitignored, therefore per-worktree**, so they did not
travel with the branch. It flagged that rather than assuming, and reproduced them in its report.
It also could not transcribe four Minors at all, because I had put the review findings in a
*message* instead of a file the worktree could read. **Hand a subagent findings as a file path.**

- **§5 says names are "renameable" and no rename verb exists.** `wake new <name>` chooses one at
  creation; nothing changes it afterwards. This clause of the spec is silently open and was in no
  notes file until now.
- **Bare repositories are unhandled by `taskLabel`.** Measured: a bare repo labels as its
  directory basename (`wake.git`); a bare repo **nested inside a working tree** labels with the
  *enclosing* repo's branch, not its own. Defensible, untested, and one of the five cases the
  review asked about.
- **The name release on `spawn`'s register-failure path has no test** — reachable only in a
  genuine two-client race for one session id. The whole 5-statement block shows 0 coverage.
- **`matchSession`'s prefix property does not extend to the ordinal space.** With an ended `alex`
  and live `alex-2`/`alex-3`, `wake attach alex` resolves to the *ended* row and reports "session
  has ended" while two live agents match. A refusal with a misleading reason, not a wrong attach.
- **`readBounded`'s open-failure path and `findGitDir`'s depth-exhaustion return are uncovered.**
  Correct by inspection; the first is reachable (a `.git` directory with no `HEAD`).
- **`cleanLabel` strips C0 and DEL but not bidi or zero-width.** A branch containing U+202E
  reaches a `wake status` row. Cosmetic — the label never reaches argv.
- **Nothing ties the pool's longest name to `status.go`'s `titleColumn`.** The column derives 28
  from "a six-character pooled name + separator + 18", and the longest pooled name is indeed 6 —
  but they live in different packages and nothing checks it. A nine-character addition shifts
  every row.


---

## From the e2e suite (2026-08-09)

- **`TestABrokenPsDoesNotTurnAQuietAgentIntoAKill` has a ~900 ms window and fails under load.**
  Isolated properly rather than blamed on the new work: an alternating branch-vs-baseline
  experiment reproduced it **on the baseline 3/4 while the branch passed 4/4**. Pre-existing, and
  the third load-sensitive failure this package has produced — the other two were diagnosed as a
  destructive read and a bound that was half the grace it bounded.
- **`FrameAnswer` has no end-to-end loop.** The suite was built before the answer path landed. A
  loop proving `updatedInput.answers` reaches the model is the obvious next one, and it is the
  exact shape of the defect that prompted the suite.
- **Two process notes for whoever briefs the next agent**, both mine:
  - I specified a report path under gitignored `.superpowers/` **twice**, after ruling that notes
    which must survive a branch have to be tracked. The agent caught it both times. **A report
    path handed to a subagent belongs under `docs/`.**
  - An apparent process leak during the run turned out to be **another agent's concurrent
    `make cover`** on the same machine. Parallel agents share a box; a measurement that counts
    processes needs to know that.

---

**The hint line truncates mid-word at intermediate widths.** Measured through the real render
path after `⌃F fork` took the legend to eleven entries: the whole legend plus the permissions
label is **163 cells**; 147 gives all eleven keys and no mode; 80 gives `↵ ⎋ ⌃C ⇥ ⇧⇥ ⌃D`. But the
cut is a plain right-truncate, so **at 100 columns it reads `… ⌃G wo`** — it stops in the middle
of `workspaces`.

> **Re-measured 2026-08-11, and it got worse rather than going away.** The rebinding took the
> legend to thirteen entries: **194 cells**, 178 for all the keys and no mode, and 80 gives
> `↵ ⎋ ⌃O ⌃C ⇥ ⇧⇥` whole with `⌃D`'s glyph hanging off the end with no label — which is this item
> exactly, at the width the item names as the common one. The legend now fits in **no pane this
> product has** (the room at 200 columns with both sidebars open is 164), so every real pane is
> truncating and the mid-word cut is what an operator sees rather than an edge. The numbers above
> are kept as the measurement they were; the current ones are derived and held by
> `TestCLAUDEmdDescribesTheLegendItDraws` rather than restated here.

*Blocks:* nothing. It is cosmetic, and every key that is shown is real, which is the property
that actually matters and is enforced by the legend bijection.

*Why it is written down anyway:* 100 columns is an ordinary terminal, a legend that stops
mid-word reads as a rendering fault rather than as a deliberate cut, and this project deleted a
legend entry once already on the argument that a lying hint is worse than a missing one. The fix
is to truncate at an entry boundary rather than at a cell — the entries are already a slice
(`legendEntries`), so the cut has something to align to.

*Found by:* measuring for `docs/live-testing.md` rather than by a test. `narrowLegendWidth = 147`
is a clean boundary by luck of arithmetic, so the shipped guard cannot see this — it is the same
shape as the guard that could not catch the `⌃F` overflow because it looked for glyphs while the
word was what got cut. **A width test that only checks its own chosen width is not checking the
truncation.**

**Wake — the only proof a woken session came back is that a process started, not that it loaded the
conversation.** Added 2026-08-10 by park/wake Task 3. The suite asserts the argv end to end
(`--resume <id>`, no `--session-id`, no `--fork-session`), the identity that survives, and the
refusals — but the fake `claude` on PATH has no transcript and no notion of resuming one, so
*nothing here shows a woken session answering from the conversation it parked with*. The recordings
do (`resume-park.jsonl` / `resume-wake.jsonl`, two processes over one session id, the second
answering from the first's history), and the argv assertion is what ties Wake's output to them —
which is the same standing this project gives every other claude behaviour it cannot re-run in CI.
*What closes it is a recording harness, not code*: replaying a recorded resume pair through a fake
that serves a transcript would be a second implementation of `claude`, and `docs/live-testing.md` is
where "a human at a real terminal, one real agent" belongs. **Add it there before anyone trusts a
wake in front of a user.**

**Wake — `resumeSafe` costs one `ps -Aww` per keystroke, and the listing is the whole machine.**
Added 2026-08-10 by park/wake Task 3, and recorded because the power section already names the
daemon's ~86,400 `ps` spawns a day as the cost that scales. This one is on an operator's action
rather than on a timer, and `idsInUse` takes a slice so *n* ids are one process rather than *n* — the
restore-on-start path spec §4 describes wakes a whole fleet at once and must use that shape. What is
not bounded is the listing itself: on a busy machine `ps -A` output is large and it is read into
memory whole. Nothing measures it. *What closes it* is a measurement, and the threshold for caring is
restore-on-start, not `⌃C`.

---

## Lane B, 2026-08-11 — working the ranked list

A second session took the outstanding-bug list while another took Phase 2's
manager. Entries here are appended rather than edited into the list above, so
the two lanes cannot conflict in a file they both want.

### CLOSED — #1, a woken session answering from the conversation it parked with

`a7087dd`. A fake `claude` that keeps a transcript keyed by session id and reads
it back on `--resume`, plus two tests: park and wake in one daemon, and the shape
an operator actually produces — `⌃Q`, the daemon exits, a **second** daemon
restores from the park book, `/resume`. Separate tests because they fail for
different reasons, and one covering both would report the wrong cause half the
time.

The passphrase is told before the park and asked for after the wake **in a
message that does not contain it**. An echoing fake, a fake reading the wrong
file, and a fake starting empty on resume each produce an answer without it.

The mutation that makes them evidence rather than decoration: removing
`ResumeFrom` from `unpark`, so a wake spawns fresh instead of resuming, turns
both red in 0.08s. That is a **product** mutation — a test that only caught bugs
in its own fake would prove nothing about Wake.

### FOUND AND FIXED — two test binaries failed each other, and it was never about two people

`199d662`. The fixture session ids were three fixed UUID constants, so any two
`internal/daemon` or `cmd/wake` test binaries running at once each saw the
other's fake agent holding the id, and `resumeSafe` correctly refused every
wake. The failure message accuses the code under test:

    a process is still running under session a11a0000-...-00000000a11a,
    so resuming it would put two processes on one transcript

**`go test ./...` runs packages in parallel by default**, and both packages spawn
agents under those ids — so this was one `make test` away from biting a single
developer on a quiet machine, and it would have looked exactly like a flake in
whatever wake test ran second.

Now per-process: the node field carries a nonce, the tag stays at both ends so a
failure still reads `a11a…a11a`, and only the last field moves so the id stays
the valid v4 UUID `mintedByWake` requires. Two concurrent `./internal/daemon/`
runs failed 3/3 before and pass after; two concurrent `go test ./...` runs both
pass.

*Also decoupled:* the park book's format anchor compared hand-written bytes
against `idAlpha`. Those bytes are what **another build** wrote, so nothing about
them may move when this build's fixtures do — an anchor that followed the
fixture would assert only that this build can read what it just generated, which
is the round trip three lines above it.

### The lesson, one level up from the one already recorded

`decisions.md` says *"check `uptime` before believing a flake"*, after 24
orphaned busy-loop shells held this machine at load 55 for twelve hours. This is
that rule generalised: **check what else is running, not only how loaded the
machine is.** The diagnosis cost an afternoon and two near-misses where I almost
filed a bug against my own new test, because every measurement in between was
noise — including one "restore failed" that was a runaway test binary from my
own mutation run, still spawning fake agents ninety seconds later.

Three things that would each have shortened it: `ps ax | grep <fixture id>`
before believing any wake failure; noticing that *pre-existing* tests were
failing too, which is the tell that the cause is environmental rather than in the
diff; and running the base commit, which passed 3/3 and located the problem in
twenty seconds after an hour of not thinking to.

### CLOSED — #2, neither soak lane parks or wakes

Its entry was partly stale: Task 5 had already added the park phase. What was
missing was **waking** and **⌃Q**.

The lane now parks the fleet, wakes half and leaves half parked — the woken half
is a live fleet at shutdown (the churn phase drains everything before that
point, so it is the only thing making the daemon stop real processes on the way
out), and the parked half keeps the book non-empty when the quit arrives, which
is what makes the existing "stop clears the book" assertion about anything.

**The sequential wake phase was not enough, and mutation is how that was found.**
Waking each id once from one connection walks the path and no part of the hazard
it guards: gutting `replaceParked`'s pointer-identity check left the lane green.
`raceWake` asks two connections for the same id at once — the shape that matters,
because each connection is dispatched on its own goroutine — and with the check
gone the lane now reports *"11 processes still running after the daemon exited,
up from 1"*. That was `CLAUDE.md`'s own prediction about that check, asserted
nowhere until now.

`TestSoakParkAllLeavesAFleetToComeBackTo` covers ⌃Q at fleet scale, separately,
because the two quit verbs disagree about exactly one thing and folding it in
would invert the lane's closing assertion. Its header states what it does **not**
discriminate: gutting `bookParked` leaves it green, because `completePark`
writes the same records from each fan-out goroutine. What it kills is
`FrameParkAll` dispatched to `quitStop` — the book holds 0 of 20.

*Still open from that entry:* nothing exercises `admitRefusal`'s wake sentence
or `withdraw`'s wake arm **by their text** under load; the race phase reaches
both paths but asserts on the process count rather than on what the loser was
told. Low value — both have unit coverage — but it is the honest remainder.

### CLOSED — #8, the legend truncated mid-entry

`hintFitting` drops an entry that does not fit rather than slicing it. At 80
columns the line now ends cleanly at `⇧⇥ next blocked` instead of a bare `⌃D`.

Both halves of the entry go together: the ragged cut, and a cut landing inside a
label so what remains reads as a **different key's** whole label. `⌃Q`'s
`quit & park all` ordering is now belt-and-braces rather than load-bearing.

*The guard walks every width*, which is the lesson from the defect before it
rather than thoroughness: that one shipped **because** its guard sampled, and at
the sampled width the glyph survived while the label was cut. It splits on the
separator and requires each part to be a whole entry — exact where
`strings.Contains` is not, since `⇥` is a substring of `⇧⇥`.

**It made an older guard's premise false, and that guard's own vacuity floor
caught it.** `TestNoWidthCutsALegendEntryIntoADifferentKeysLabel` scanned for
entries the cell cut had shortened; there are none now. Strictly subsumed — if
no fragment is drawn, none can read as anything — so the truncation half is gone
and the half that never depended on truncation remains as
`TestEveryLegendEntryHasItsOwnLabel`. Rung 7 arriving inside my own change.

*Not closed, and it is the other half of the entry:* with a DM open the last six
entries are invisible on every terminal up to ~300 columns. That is the legend
being longer than any pane, not a truncation defect — it wants either fewer
entries or a second row, and both are design calls rather than fixes.

### CLOSED — #3, ⌃C reported success before the write was confirmed

The keypress now names the ask (`parking @alex…`) and `parkArrived` says
`@alex is parked - /resume alex brings it back` when the first fleet report
names it. That is ⌃F's rule one key over, and `CLAUDE.md` already states it:
**the report is the confirmation and the keypress is not.**

Reports once per *transition*, not once per report — every report names a parked
session for as long as it stays parked — and says nothing about sessions this
client did not ask to park, because a fleet is shared.

**⌃Q's half needed no change, and the entry was right about why.** `parkingFleet`
already names the ask rather than the outcome. What remains there is *inherent*:
the window is already closing, so a refused write has nowhere to land. Anyone
giving the notice row a pending state should reconsider it then; it cannot be
fixed by wording.

*Still open, and it is the third of the three that entry named:* a park the
daemon could not **write down** (`parked.add` failing) reaches only the daemon
log while every client is still attached. Task 5's m1. Untouched here.

*Note for whoever owns `CLAUDE.md`:* one derived number moved with this —
`internal/ui/app.go` 731 → 735, the field this adds. Its guard failed loudly
with the correction in its own message.

### CLOSED — #9, a woken conversation opened with no account of itself

`ui.TranscriptNotice` is the sentence, said by `wake attach` and now by a wake
too — **one copy**, where `cmd/wake` had it inline. `wakeArrived` says it when a
report first shows a session this client asked to wake as running again, which
is `parkArrived`'s shape and `⌃F`'s rule before it: the keypress names the ask,
the report earns the sentence.

*Second derived-number correction in two commits* — `internal/ui/app.go` 735 →
738 for the `waking` field, after 731 → 735 for `parking`. The guard failed both
times with the right number in its own message, which is the strongest argument
for that sentence being derived rather than trusted.

### CLOSED — #10, no test read a park book written by an older build

Five hand-written historical shapes: before `parked`, before `label`, before
`dir`, a build carrying fields this one has never heard of, and an empty book.
The property is one sentence — **a book from another build loses only what it
does not carry** — because `loadParkBook` returning nil is exactly how a
silently dropped fleet would look.

It went green immediately, which the entry predicted, so both mutations were run
to show it discriminates: strict decoding kills the unknown-field row, dropping
dir-less records kills the pre-`dir` row, and each kills only its own.

Task 4's honest note still stands: the first *real* instance exists the day a
second version ships. This is the machinery, checked.

---

## Lane B — the six assigned items are closed

#1, #2, #3, #8, #9, #10, plus the test-binary collision found on the way. What
each left open is recorded under its own entry above rather than here, and none
of it is a regression from this work:

- **#2** — the wake refusal sentences have unit coverage but none under load.
- **#3** — a park the daemon cannot *write down* reaches only its log (Task 5's
  m1), and `⌃Q`'s window genuinely has nowhere to land a refused write.
- **#8** — with a DM open the last six legend entries are invisible on every
  terminal up to ~300 columns. A design call: fewer entries, or a second row.
- **#10** — the first real schema change is the day a second version ships.

Items 4–7 and 11–15 were never in this lane and are untouched.

---

## Create-and-name, 2026-08-12 — `/new`, rename and task labels

`docs/goals.md` §3's largest single gap: *"Wake can manage agents but cannot
create or name them from inside itself."* `/new`, `/name` and `/task` close the
create half and the name half. What is closed, what is refused, and what is
deferred — each with what would reopen it.

### CLOSED — item 14, `/add-<agent-name>` cannot route as spelled

**Refused as spelled, and not approximated.** The choice item 14 stated was
*change the syntax to `/add <name>`, or reopen the guard*. Neither was taken,
and the third answer is the one the evidence supports. Three arguments, in
`internal/ui/slash.go`'s header, and the first is new:

- **A `<verb>-<suffix>` rule claims an operator's whole command set.** Item 14
  named the near-miss as *"claude ships `/add-dir`"*. The recorded corpus is
  worse: of the **133** commands claude advertised on `init` frames across
  `testdata/stream`, most are hyphenated and **eight begin `new-`** —
  `/new-oscar`, `/new-victor`, `/new-sierra` and the rest, which are this
  repository owner's own. The rule that reaches `add-sydney` eats all of them.
- **It is not decidable from the draft.** `/add-dir` and `/add-sydney` are the
  same shape; separating them means asking the fleet, so the set of drafts Wake
  claims would change between keystrokes and an operator would lose `/add-dir`
  for as long as some agent was called `dir`. The rule is *resolve against a
  closed set Wake owns*, and the fleet is closed but not **stable**.
- **There is nothing for it to do.** The verb adds a live agent to the group
  chat, and in this build every live agent is already in it — `Fleet.Observe`
  folds every session and `App.apply` filters none. It becomes a real verb the
  day a group has membership (`internal/ui/groups.go`), and whoever builds that
  can spell it then.

`/add <name>` was **not** shipped in its place for the third reason: a command
that does nothing is the lying feature the legend rule exists to prevent.

The ruling is guarded rather than only written down. `/add-dir ~/project` and
`/new-oscar` are in `TestAnOperatorsOwnCommandStillReachesTheAgent`, and the
realistic mutant — `word, _, _ = strings.Cut(word, "-")` in the router — is
killed three ways: the behavioural test on `/new-oscar`, the second write to
`word`, and the `"-"` literal.

**What would reopen it:** group membership. Whoever builds it owns the spelling.

### The passthrough list is no longer five words, and it found something

`TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising` reads
`slash_commands` off the recorded `init` frames — the upgrade this file has
carried since the layer shipped. It is still a sample and closes no value space;
what it closes is the **overlap**, in the one direction no static check reaches.

It found a real one before anything rested on it: the obvious verb for *"you can
either rename or assign a task"* is `/rename`, and **`rename` is in the corpus**.
The hand-written five said nothing (mutation: `resumeCommand = "rename"` — old
guard green, new guard failed naming the recording). Hence `/name` and `/task`.

**Still open:** the corpus is a sample of one machine's `claude` on the days
these were recorded, and it does **not** subsume the hand-written five — `help`
is in the list and not in the corpus. Both lists are kept. Reading
`slash_commands` off a *live* `init` frame would make it exact — and **the
decoder half landed on 2026-08-15** with the completion menu, so what is left is
a place to check it and a ruling on what a mismatch reports.

### Deferred: a parked session cannot be renamed or relabelled

Both verbs are refused for `rpc.StateParked`, with the way round in the
sentence (`/resume`, then rename). The reason is a contract rather than effort:
a parked session's two display halves live in `parked.json`, and that record is
written **once**, by `completePark`, in `retire`, after the process is provably
gone. A rename that did not rewrite it comes back under the old name after a
restart — the silently-stale display fact the park book exists to avoid — and
rewriting it would make a rename the first thing other than a park to write that
file.

**What it would take:** a per-id read and an upsert on `parkBook` (`add` already
replaces by id, so the write is one call), plus a ruling on what a record edited
by something other than a park is allowed to assert. That belongs to whoever
owns `parkbook.go`, not to a rename taking it as a drive-by.

### Deferred: a task label cannot be cleared back to the branch

`/task` with no argument is a refusal, and `normalizeLabel("")` is an error. The
alternative meaning — *put it back to what `.git/HEAD` says* — is a third verb
nothing has asked for, and it is indistinguishable from an argument dropped on
the way. If it is wanted it needs its own spelling, not an empty one.

### Deferred: an assigned label survives a park/wake by inheritance, untested

`labelFor` carries `replaces.label` rather than re-deriving, which is what makes
*"a checkout while a session was parked must not relabel it"* true — and it
makes an assigned label survive a wake for free. The existing test drives that
property with a **derived** label and a checkout in the middle; nothing drives
it with an **assigned** one. The code path is identical and the gap is a fixture,
so it is small — but it is the fixture that would notice a future `labelFor`
learning to re-derive "only when the label was not set by hand".

### Deferred: no live cap, and `/new` is now a keystroke

`/new` is the first way to start an agent without leaving the room, and there is
still no live cap anywhere in this build. It is bounded by a human typing, which
is why it ships — `cmd/wake/mcpguard_test.go` refuses `FrameSpawn` to the
manager for the same absent cap. Worth restating together because the two
entries now sit one layer apart.

### Known, not fixed: `agent.go`'s field comments predate a mutable name

`agent.parent`'s comment reads *"Immutable after newAgent and display only,
exactly like label"*. The clause is about *display only*, which is still true of
both, and `label` and `name` are now written after `newAgent` by `relabel` and
`rename`. `agent.go` belongs to another lane this week, so the sentence was not
edited; the contract that matters is enforced instead — every read of either
field on a live agent goes through the lock (`snapshot`, `rosterRecord`), and
`launch`'s one unlocked read of `replaces.label` is ordered behind `markParked`.

### Owed, and named here so it is not discovered: rung 7 on `cmd/wake`'s verbs

`internal/ui/rename.go` and `new.go` name `wake new` in prose, and
`cmd/wake/manager_test.go` already holds every `wake <verb>` any sentence names
to the real verb set — so that half is derived. What is *not* is the other
direction recorded under item 7: the parked-attach refusal still forbids a
`wake resume` shell verb while reading none of `cmd/wake`'s dispatch. This task
did not close it and did not make it worse.
## Mention mode, 2026-08-12 — what the toggle shipped without

`⌃T` and the `direct`/`open` readings are built (`internal/ui/mention.go`). Three things §7 or its
neighbours name are deliberately not in that change.

### `routing.mention` in config — the mode does not survive the process

§7 says the default is *"set via `routing.mention` in config"*, and **Wake has no config file**.
Inventing one for a two-valued enum would be the first thing ever written into it, which is the
same ruling `2026-08-09-wake-room.md` already made about persisted layout widths — and the
persisted half of that plan's list is what a profiles task owns. So `⌃T` flips it *for the session*,
which is the other half of §7's own sentence and the half that does not need a file.

**Closing condition:** the task that creates Wake's config file writes `routing.mention` into it in
the same change, defaulting to `direct`, and `App.mention`'s zero value stops being the only source
of the default. Until then the zero value is load-bearing and `TestTheRoomStartsInTheCheapReading`
holds it.

### The room plan's deferral is superseded on the toggle and stands on the persistence

The room's first plan, in its "What this phase deliberately does not build" section, declined
mention mode with a real argument: *"A mode whose current value is not the thing you are
looking at when you press `↵` is how somebody spends 30 turns by accident."* That argument was
answered rather than overruled — the target line names the mode under **both** readings and carries
the turn count under the expensive one, and `TestWhatTheComposerPromisesIsWhatEnterSends` holds the
number on screen equal to the frames ↵ writes. Its closing clause, *"belongs with profiles"*, is
now true of the config half alone and is the entry above.

### Two status documents will be wrong the moment this merges

Recorded rather than edited, because this branch is not merged and marking something built before
it is would be the same lie pointing the other way:

- `docs/superpowers/specs/2026-08-12-phase-4-scope.md` §1 lists **mention-mode toggle | NOT BUILT**,
  and its "Phase 4's floor is six items" count drops to five.
- `docs/notes/v1_goals.md`'s routing row reads *"Not built"* for the whole clause — already stale on
  three of its four parts before this branch touched anything.

**Whoever merges this flips both in the merge commit.** That is rung 7's audit applied to prose: the
grep this branch ran over `_test.go` for assertions that the capability does not exist came back
clean, and these two are the same claim living in a document instead of a test.

### The legend is now fourteen entries and a DM makes six of them unreachable

Not new, and made one worse. Lane B's #8 already recorded that *"with a DM open the last six legend
entries are invisible on every terminal up to ~300 columns"*; `⌃T mention mode` is a fourteenth
entry at the tail, so it is the first thing cut at every width and invisible in a two-pane view.
That is the truncation order working — the mode's current value is on the target line and in the
notice row, so the entry advertises the key rather than carrying the state — but the underlying
design call Lane B named (**fewer entries, or a second row**) is now carrying one more entry.

---

## 2026-08-12 — open mention mode puts a leading `@name` on an agent's stdin, and nobody has recorded what that does

Found by the review of the mention-mode toggle, in the change that created it.

`open` mode widens a resolved `@john` to the fleet and **keeps the name in the text**, deliberately:
the point of open is that the other agents can see who was addressed, and a message stripped of its
mention arrives at twenty agents saying *"ship it"* with no subject. That decision is right and the
argument in `openRoute` is the strongest in the branch.

**But it makes this the first path in the build that puts a leading `@name` on an agent's stdin.**
`internal/core/router.go`'s header records that Claude Code expands a leading `@` before the model
sees it — which is why `Route.Text` strips a mention that resolved. So in open mode, N agents
receive a draft whose first token their own CLI treats as a **file reference**.

`router.go` already accepted a residual of this shape once — *"a file literally named `alex` at the
repository root — rare"* — but bounded it with *"`Route.Resolved` is what makes it recoverable: the
room shows what it resolved to before the message goes anywhere."* **That mitigation does not reach
this case.** The room shows `→ @john · open · 20 turns`, which is true about the routing and silent
about the expansion, and the expansion now fires N times per send instead of zero.

**What is unrecorded:** what a leading `@name` that names no file actually does. Does the CLI drop
it, error, warn, or pass it through? This project does not design around unrecorded behaviour, and
nothing here does — the behaviour shipped with the contract amended to say so. But the answer
decides whether this is a non-event or a real degradation at fleet scale.

*Blocks:* nothing today. *Closes with:* a short recording spike — send `@nobody ship it` and
`@john ship it` to a real `claude` with a `john` file present and absent, four fixtures. The method
is `2026-08-09-resume-fork-findings.md` §11.

*Do not "fix" this by stripping the mention.* That was considered and rejected for the reason above;
if the recording says the expansion is harmful, the fix is in how the mention is rendered, not in
removing it.

### Fix round 1, 2026-08-12 — the premise the slash fence rests on was stated too strongly

`CLAUDE.md`, `internal/ui/slash.go`, `slashguard_test.go` and `slash_test.go`
all said a user's own `.claude/commands/*.md` are slash commands **"nothing on
this side of the socket can enumerate."** This branch's own corpus disproves it:
claude announces its whole command set on the `init` frame as `slash_commands`,
operator files included — `~/.claude/commands/new-papa.md` exists on this
machine and `new-papa` is in `testdata/stream`. The airlock **dropped** that
key, which was a decision rather than an impossibility — and it decodes it as of
2026-08-15, for the completion menu, which offers it and routes nothing on it. All four sentences are
corrected to *"nothing can enumerate them at the moment the question is asked"*.

**The ruling does not change, and the corrected premise is why.** The list is
per session and arrives after the first frame; the routing decision is per
keystroke, in a room that may hold thirty agents with different command sets and
none at all before its first spawn. A router consulting it would claim a
different set of drafts per agent and per second — which is argument 2 for
refusing `/add-<agent-name>`, arriving one door over. It also makes that refusal
**stronger**: there is now positive evidence that an operator's commands are
visible on the wire and that eight of them collide with a `new-` prefix rule.

### The option this opens, for whoever revisits the layer

**A derived passthrough guard.** Today `claudeCommandsThatMustPassThrough` is
five hand-written words and the corpus test is 133 recorded ones; both are
samples, and the static checks are what actually close the value space. The
`init` frame could make the *overlap* check exact rather than sampled — rung 2,
enumerate the domain from the thing that declares it — because claude declares
it on the wire.

What it would cost, stated so the next person starts from the real shape:

- ~~**A decoder change, not a test change.**~~ **DONE 2026-08-15**, by the completion menu rather
  than by anybody taking this entry: `initFacts` reads `slash_commands` onto
  `core.SessionFacts.SlashCommands`, tagged in Wake's own spelling so the words cross `internal/rpc`
  and the shape does not. The decoder half of this option is paid for; what is left is the ruling
  below and a place to report from.
- **A ruling on what it is for.** It cannot decide routing (per-session, late).
  It can decide a **guard**: "no command Wake owns appears in any session's
  advertised set", checked at the moment a session announces itself, reported
  once, and never used to swallow or release a draft. That is a diagnostic, and
  the difference matters — a fence that changes what it claims when an agent
  starts is the failure the fence exists for.
- **What it does not close.** A command an operator adds after `init`, and a
  machine whose first session has not started. So the hand-written list does not
  go away; it gets an exact companion rather than a replacement.

### Also from fix round 1

- **`isHex` accepts every UUID prefix from 9 to 24 characters** (`names.go`).
  The hyphen is a legal name rune and sits at UUID positions 9/14/19/24, so
  `a4f78b3d-1e2f` passes the hex rule and `matchSession` resolves a whole name
  before it looks at the id space. Pre-existing; `/name` is a second door onto
  it. **Closed in this round** — the predicate now asks "could this be the front
  of a canonical UUID" rather than "is every rune hex".
- **`agent.go`'s struct layout is the contract and it is now wrong.** `name` and
  `label` are declared above `mu` with everything below it lock-guarded, and both
  are written under `a.mu` by `rename`/`relabel`. The one real race
  (`managerAgent`) is fixed and the class is guarded by
  `TestEveryUnlockedReadOfTheDisplayHalvesHasAVerdict`, but **the fields should
  move below `mu`** and three comments should follow — `agent.go`'s `label`
  *"nothing here reads it"* and `parent` *"exactly like label"*, and `retire`'s
  *"nothing in this package holds one lock across another"*, which `agent.rename`
  (a.mu across names.mu) makes false. `agent.go` belongs to another lane this
  week; `spawn.go`'s two are corrected here.
- **A rename is silent in every other window.** Only the client that asked
  reports it; other rooms see the roster row change with no line. With the DM
  header fixed this is a UX gap rather than a hazard, and the fix is a notice
  keyed on a name changing between reports — which needs a rule about *whose*
  renames are worth a line, the same question `noteEnding` answers by reporting
  only this client's own session.

### Fix round 1 — the Minor findings, and the two left with reasons

**Closed.** `isHex` became `couldPrefixASessionID`, positional rather than an
alphabet test (M2); `/task` and `/name` stopped echoing the request as though it
were the outcome (M1); three comments corrected (M3, two of three — see above);
a stale failure message (M4); `/add-dir` demoted from "a claude built-in" to "a
word Wake does not own", since the corpus does not carry it and the argument
does not need it (M5); `absoluteDir`'s `~` arm tested against `os.UserHomeDir`
rather than a spelled path (M6).

**Left, with reasons.**

- **M7 — `handleList` borrows `noneListening`.** *"nobody is listening: every
  agent has ended, or none has started"* was written about a broadcast and is
  reused as the tail of "no live agent answers to that handle". It is accurate
  and it answers a slightly different question. It lives in `send.go`, which is
  the mention-mode lane's this week; splitting it is a two-line change in
  somebody else's file and the cost of getting it wrong is a slightly-off
  sentence on a refusal path.
- **M8 — a rename is silent in every other window**, and (new, from M1) **the
  stored label has no TUI surface for an agent that has not spoken.** Both are
  the same missing thing: a *confirmation derived from the report* rather than
  from the request. The rename half is now cosmetic — `App.renamed` moves the
  header and the roster row, so the other window sees the change, just without a
  line about it. The label half is the real gap: `speaker()` needs the agent to
  speak and `roster.go`'s `headLine` draws the name alone, so `/task` on a silent
  agent shows the operator nothing at all.

  **What it needs is a rule, not a mechanism.** The mechanism is `App.renamed`,
  which already walks every report and already knows what changed. The rule is
  *whose* changes are worth a line — the same question `noteEnding` answers by
  reporting only this client's own session, and answering it wrong at thirty
  agents is thirty notices nobody asked for. It also wants the roster to draw a
  label, which is `roster.go`'s and another lane's.
---

## Session importing, 2026-08-12 — what shipped without, and the one measurement that would change the design

`wake import` is built: discovery over `~/.claude/projects`, a picker at the shell, and an adopt
that is a **fork** rather than a resume. The recording behind all of it is
`docs/superpowers/notes/2026-08-12-session-discovery-findings.md`; its §7 is the unrecorded list and
these are the items that have a consumer.

### The lsof spike — the one that could turn "cannot" into "can"

**This is the highest-value item in this section**, and it is the only one that would reopen a
design decision rather than add a feature.

The whole of why an import is a fork is that Wake cannot tell whether a session is still open.
`resumeSafe` asks `idsInUse`, which matches `core.SessionArgvMarkers` — a flag and its value — and
§5 counted four live `claude` processes on this machine whose entire command line is the word
`claude`. No id, nothing to match. That is the shape the feature exists for.

**Unrecorded: whether a live `claude` holds its own `<uuid>.jsonl` open.** If it does, `lsof` (or
`/proc` on Linux) answers "is this session open" for *every* shape, including the bare one — which
is a real liveness check where today there is none.

*What it would change:* not necessarily the fork. It would make the **refusal** complete, which is
worth having on its own: today an import of a session somebody has open in a terminal succeeds
quietly and produces a second conversation, and the only thing standing between that and confusion
is a sentence on a notice row. With a real check it becomes a refusal, which is what 2026-08-09
findings §5 says this flow owes. Whether it should then also become a resume is a *second* question
and a bigger one — it would mean the import keeps the original session id, which is the thing this
build gave up.

*Blocks:* nothing today. *Closes with:* one recording — open a `claude` in a terminal, `lsof -p
<pid>` and look for the transcript path, then close it and look again. Ten minutes, no spend, no
LLM. `TestAnImportCannotSeeAProcessThatCarriesNoIdAndIsAForkBecauseOfIt` is written to **fail** if
this ever becomes detectable, so the signal to come back here is already wired.

### Forking a **mid-turn** parent — the same recording session, and the sentence that is waiting on it

`importedNotice` tells the operator *"This copy does not write to the original — measured on an idle
session."* The qualifier is there because the evidence is `2026-08-10` findings §5: sha256-identical,
at one moment, on one machine, with an **idle** parent that had finished its turn and flushed. §12 of
that note declines the mid-turn case in its own words — *"whether the parent's flush and the fork's
read can race"* is unknown — and §5 closes with *"the claim recorded here is 'it did not change', not
'it cannot change'."*

**The import's central case is the mid-turn case by construction**: a session somebody is using in a
terminal right now. `wake fork` guards it with `forkRefusal`; the import path **has no analogue and
cannot have one**, because Wake holds no state about a stranger's session — so the qualifier is the
whole of the mitigation.

*Closes with:* fork a live parent **while it is generating**, and sha256 its transcript before and
after — the method is `2026-08-09-resume-fork-findings.md` §11, and it is the same driver and the
same sitting as the `lsof` spike above. If the parent's file is unchanged there too, the qualifier
comes off and `importedNotice` gets the stronger sentence. If it is not, the import needs a warning
this build has no way to raise, which is a much larger finding.

*Do not remove the qualifier without that recording.* It was unqualified for one commit and pinned as
a test, so the assertion had a guard defending it.

### An import has no TUI surface — **CLOSED 2026-08-12 by `/adopt`**

It is `/adopt` rather than `/import`, and the word is the finding: `import` is in the recorded
corpus of what claude advertises on its `init` frame, in 45 of the 45 files that carry the key, so
this entry's own proposed spelling would have replaced a working command with a refusal. The cap
this entry predicted was needed and is `cmd/wake`'s `adoptRows` (10), with the true total and
`wake import` named for the rest. See `docs/notes/decisions.md` and
`.superpowers/sdd/import-command-report.md`.

### One at a time — and the founding ask was about **selection**, not about `--all`

`wake import` adopts one session per invocation, and this entry was first written as if the gap were
a convenience flag. **It is a scope gap, and the founding sentence is the evidence:** *"be able to
open a group chat among them — select which **ones** you would like to add."* Plural, and the plural
is the feature. The whole point is a room containing the three sessions you already had open, which
today is three invocations, three names drawn one at a time, and no moment at which the operator is
choosing a *set*.

What is built is the picker's first half — the listing — and the selection is degenerate: one id per
run. A real multi-select (mark rows, confirm once) is the shape the ask describes, and it is a
different piece of work from a flag.

*Why it was still right to ship one at a time:* each import is a name, a process and somebody's
money, and there is **no live cap anywhere in this build** — so an unbounded `--all` across 428
transcripts is the failure `cmd/wake/mcpguard_test.go` refuses a spawn tool for, arriving at the
shell where nobody would think to refuse it. One at a time is the bounded version of the right
feature, not a substitute for it.

*Closes with:* the live cap (`internal/core/pool.go`, still **NOT BUILT**) for the bound, and a
selection surface for the ask — which is the same work as the `/import` TUI entry above, since a pane
is where marking rows belongs. Read those two entries together.

**Half-closed 2026-08-12 by `/adopt`, and the remaining half is named below.** `/adopt <id> <id>
<id>` is one draft, one write, N frames and N conversations opening in the room — so there *is* now
a moment at which the operator chooses a set, which is what the founding sentence asks for and what
`wake import` cannot do at any number of invocations. What is **not** built is marking rows: the
selection is typed as short ids copied off the listing, not confirmed by moving a cursor down a
picker. That is a pane with its own key handling and its own focus, which is a different piece of
work from a command; `/adopt`'s listing lands in the room's transcript as a preformatted block,
which is a listing rather than a widget.

**And the live cap is still NOT BUILT, so the bound is still the operator's.** `/adopt` will write
as many frames as there are words in the draft. That is bounded by typing in a way `--all` would
not be — every session is named individually, off a listing capped at ten — but it is not a cap, and
a draft naming thirty sources is thirty processes. The live cap remains the thing that closes it.

## `/adopt` — the room's half of session importing, 2026-08-12

What the TUI entry above did not close, and what building it turned up.

### The listing in a pane is a block, not a picker

`/adopt` with no argument puts the shell's own listing into the room's transcript, fenced, capped at
ten rows. It is the right amount of surface for a command and it is not the picker the founding
sentence describes: there is no cursor, no marking, no confirm, and no filter. An operator with 428
transcripts who wants the fifth-newest one from a directory that is not in the top ten has to run
`wake import` at a shell to see it.

*Closes with:* a modal pane. That needs `App.Update` key routing, a focus that is neither of the two
composers, and a place in `layout.go` — `internal/ui/app.go`, `panes.go` and `layout.go`, none of
which is one lane's file. **The cap and the block are the honest intermediate**, not a stub: the row
count is named, the remainder is named, and the verb that lists all of them is named.

### The listing has no filter, and recency is the only order there is

`daemon.Discoverable` sorts by mtime and nothing else. In a pane that makes the cap a *recency*
window, which is the right question most of the time — the sessions somebody has open in terminals
right now are the recent ones — and useless for "the one in `delta-agent`". A directory filter is
the obvious next thing and it belongs with the pane rather than with the command, because filtering
without a cursor is a second grammar on the same draft.

### `/adopt` is not in the legend, and that is the legend rule rather than an omission

`ui.legendEntries` names **keys**. `/adopt` is a word typed into a composer, like `/resume`, `/new`,
`/name` and `/task` — none of which is in the legend either. Whether the room advertises its slash
commands anywhere at all is a real gap and it is five commands wide now rather than one.

*Closes with:* a decision about where a room says what it can do. Not a key: nothing may be built on
`⇧⇥`, and the two unshadowed chords left (`⌃X`, `⌃Y`) are worth more than a command that is already
typed in four characters.

### The two sentences an adopted session says are said in two places, not one

**Checked rather than assumed, and the first draft of this entry was wrong.** An adopted session
arriving in a room goes through `ui.startArrived` → `SnapshotNotice`, and it takes the *unnamed*
arm — `forkOpenedUnnamed` — because an import's `ParentID` is a session id no fleet holds. So the
sentence is well formed and true: *"@sydney is a fork. It has its parent's conversation as of now -
nothing the parent does next reaches it."* There is no hole.

What it does **not** say is the half that is specific to a stranger's session: that Wake cannot tell
whether the original is still open in a terminal. `/adopt` now says that itself, on the **ask** —
the card-arm argument, because the moment an operator can still decide otherwise is before the frame
is written. So the two claims `cmd/wake`'s `importedNotice` makes are both made in a room, in two
places rather than one.

*What is still owed:* one spelling behind both surfaces. `importedNotice` lives in `cmd/wake` with a
stated reason — *"there is no keystroke that imports"* — and **that reason expired the moment this
branch shipped**, which is a rung-7 instance in prose rather than in a test. The sentence names no
shell verb, so it could move to `internal/ui` and be read back, which is `ParkedNotice`'s
arrangement. Not done here because the two sentences are not the same sentence today: the shell says
it once, on arrival, with the assigned name; the room says the snapshot half on arrival and the
copy half on the ask. Unifying them is a wording decision as much as a move.

### Two of the daemon's import refusals now reach a room, and both send the reader to a shell

**Found by building `/adopt`, and it is the defect `CLAUDE.md` already records repairing one verb
over, arriving at a third door.** `importSource`'s five refusals were written when the only client
was `wake import`, so naming a shell verb in them was exactly right. Two are now reachable from a
composer:

- **already in this fleet, not parked** (`heldRefusal`) — *"open it with `wake attach <id>`, or
  branch it with `wake fork <id>`"*. In a room both of those already exist as keys: `⌃D` opens the
  conversation and `⌃F` forks it. The operator is told to leave.
- **the source has already been imported** (`explainHeldSource`) — *"Open the import with `wake
  attach @name`, or stop it and import again"*. Same shape.

The **parked** arm is already correct and is the proof the pattern is known: it leads with
`/resume <name>` *"in the room"*, because `wake attach` refuses a parked session. That repair was
made when the room grew `/resume`; the room has now grown `/adopt`, and the same two sentences one
door over did not travel.

The other three are fine as they stand: the empty-source and malformed-id arms are unreachable from
`/adopt` (it resolves client-side and sends whole UUIDs or nothing), and the no-directory arm names
no verb at all.

*Why it is not fixed here:* the daemon cannot see which surface asked, and it should not — the frame
is the same one. So the fix is a sentence naming both ways, which is a wording change in another
package's file at the end of a branch, with `TestEveryShellVerbASentenceNamesIsOneThisCommandDispatches`
and the slash-prose guard both over it. It is one edit and it should be made deliberately.

*Closes with:* `heldRefusal` and `explainHeldSource` naming the key beside the verb, the way the
parked arm names `/resume`. Note the asymmetry that makes this worth stating rather than obvious:
a refusal read at a shell and a refusal read in a room are the **same string**, and only one of the
two readers can act on `wake attach`.

### `Sessions` is a seam with one implementation and no fake in production

`ui.Sessions` is injected by `cmd/wake` and the only implementation is `machineSessions`. A model
built without one answers `/adopt` with "wake cannot read the claude sessions on this machine from
here", which is a legitimate shape (every unit test in `internal/ui` is one) and is also exactly
what a dropped `.WithSessions` looks like.
`TestEveryRoomThisPackageBuildsCanSeeThisMachine` derives the call sites and is what stands between
those two, because nothing behavioural can tell them apart.

*Unrecorded:* how long `daemon.Discoverable` takes on a 428-transcript machine. It is off the draw
goroutine, so a slow walk is a delay before the block appears rather than a frozen room — but
nobody has measured it, and the number would decide whether the cap should also bound the *walk*.


### 97 of 428 sessions cannot be imported, and 4 of them are a fixable case

`verifiedDir` proves a directory for 331 of 428. Of the 97 it cannot:

- **93 are transcripts whose directory has been deleted.** Nothing to fix; the conversation exists
  and the place it ran does not. Listed with the reason, which is the right answer.
- **4 are the `--worktree` shape** (§3): the transcript sits under the directory the session was
  *started* in while every `cwd` in it names the worktree. Wake refuses because nothing in the file
  says which of the two is the resume directory.

*What would close the second:* a recording of what `--resume` actually does for a `--worktree`
session — whether it must run in the original directory (in which case the containing slug is the
answer and Wake could decode it *for this shape*, where the ambiguity is between two known
candidates rather than unbounded) or in the worktree. Unrecorded, and 4 of 428 is not enough to
guess on.

### The preview is `lastPrompt`, and 4 of 428 have neither source

Measured: 379 of 428 carry a non-empty `lastPrompt`, 390 carry a `customTitle`, 424 carry either.
The four with neither are listed with an empty preview line, which is correct and slightly bare.

*Unrecorded:* whether either key is stable across claude versions. Both are read defensively —
`decodeString` treats any non-string shape as absent — so a format change degrades the preview and
breaks nothing.

### `daemon.oneLine` and `mcp.oneLine` are two functions doing one job

Both flatten untrusted text to a single line before it reaches a line-oriented surface. They are
genuinely not the same call today — `mcp`'s is unexported, in a package `daemon` does not import,
contains an **agent's model output** for a **model's** context, and has a bound derived from a
30-agent roll-up; `daemon`'s contains an **operator's own earlier prompt** for a **terminal**. But
the containment rule is identical and the reason it exists is identical, and two copies of a
security-relevant flattening is exactly how one of them drifts.

*Closes with:* a leaf package both can import — `internal/notice` is the precedent for that shape —
taking the character classes and leaving the bound at each call site, since the bounds are
legitimately different. Not done here because `internal/mcp` was another lane's file.

### `--fork-session --session-id <an id that already has a transcript>` is still unruled

`maySpawn` checks `s.holds(f.SessionID)` — the **fleet** — not the disk. A client that is not
`wake import` can therefore put an id that already has a transcript in `SessionID`, and
`2026-08-10` findings §12 names that exact row as the one that decides *"whether §10.5's danger is a
silent empty session or a loud exit 1"*. Unrecorded.

**Pre-existing and not this branch's**: `wake fork` has the identical hole and mints client-side too,
and it is unreachable through the shipped CLI, which mints a fresh UUID every time. Recorded here
because session importing is the change that made somebody look.

*Closes with:* one cheap process against an id whose transcript already exists — it is the row
`2026-08-10` §12 already asks for, and it belongs with fork rather than with importing.

### The airlock ruling `discover.go` asks for

`discover.go` reads claude's **on-disk transcript**, which is a second Claude format and therefore a
second leak. It is contained to one file and policed across the tree
(`TestTheTranscriptKeysAreSpelledOnlyInDiscover`), which is `argv.go`'s pattern applied to the
second offender — but `CLAUDE.md`'s non-negotiable still says the airlock is *four files in
`internal/core`*, and this is a fifth place in the tree that knows Claude's JSON.

*The question somebody has to rule on:* does the airlock's file set grow, does the rule get restated
to name *formats* rather than files, or does discovery move behind `internal/core`? The third is the
tidiest and the worst: `internal/core` sits **below** the daemon and knows nothing about fleets, and
a filesystem walk over a user's whole project history there would be the same dependency inversion
the spec's `internal/core/mcp.go` was rejected for.

*Blocks:* nothing. *Costs if left:* the next person who reads the non-negotiable will believe it.

---

## 2026-08-12 — undo for destructive actions, re-scoped against the build

§17 pairs *"mouse throughout"* with *"undo for destructive actions"*, and
`docs/superpowers/specs/2026-08-12-phase-4-scope.md` §2d already says Phase 3 changed the item's
shape. This is that re-scoping done against the code, with what was excluded and why.

### The five destructive actions, and the verdict on each

| Action | Reversible today? | Verdict |
|---|---|---|
| `wake stop` | **no, deliberately** | **excluded** — see below |
| `⌃C` park | yes, `/resume` | nothing to build |
| `⌃Q` park-all-and-quit | yes, the park book and `announceParked` | nothing to build |
| `⎋` interrupt | **no** | **no true undo exists**; prevention deferred, see below |
| a **deny** (or allow) on a card | **no** | **no true undo exists**; the two-press settle shipped instead |

**`wake stop` is excluded and no undo may be built for it.** Spec §2 makes it the one ending there
is no way back from, and that is *why park exists*. The park book already carries the argument from
the other side: `quitStop` clears the book precisely so a stop cannot leave twenty sessions for the
next `wake` to offer back — *"a stop that left twenty sessions … would make it reversible by
accident"*. An undo here would be that accident on purpose.

**Park and `⌃Q` are the two §17 was mostly about, and Phase 3 answered them.** `⌃C` also already
*refuses* the one state in which it is destructive — a blocked agent, where closing stdin writes a
denial the operator never made that survives the wake — and names `⎋` instead. So the item as
written is largely already shipped, under a different name.

### Why undo is a category error for the two that are left

Both an interrupt and a deny are **speech acts to a model**. Once the bytes are on the agent's
stdin the model's context contains the fact, and this build's own recordings say there is no
retraction on the wire:

- a well-formed allow written after an ask is gone produces **no frame, no error and no tool run**
  (`2026-08-09-interrupt-permission-findings.md`, quoted in `CLAUDE.md`);
- whether a **denied** ask is ever re-put is **unrecorded** — no fixture denies an
  `AskUserQuestion` or an `ExitPlanMode` at all (`2026-08-09-question-findings.md` §9);
- an interrupted turn is **billed and aborted**, and `modelUsage` does not come back.

So the honest reading of the four candidate shapes:

- **A re-ask** — telling the agent the operator changed their mind — is a **new turn**, not an
  undo, and it needs behaviour nobody has recorded. **Not built, and not designed around.** See the
  spike below.
- **A grace window on a clock** was rejected. It delays the action the operator asked for, and for
  `⎋` that is the wrong direction outright: *"stop what you are doing" is the thing an operator
  needs most urgently* (`send.go`). It also costs a timer per keystroke and makes the UI lie for the
  length of it.
- **A confirmation** is the only shape left — and it is worth building here **only because it is
  also the grace window**: the frame has not been written, so the action is genuinely still
  recallable, with the window bounded by a keystroke instead of a clock.

### What shipped: the two-press settle (`a0e199e`)

The card's own keys, in this task's lane. The argument, the guard list and the mutation battery are
in that commit and in `CLAUDE.md`'s card-keys paragraph. The finding underneath it is worth keeping
separately: **`cardkeys.go`'s own header claimed the empty-composer gate closed the "analyse"
failure, and it closes half of it** — the half that starts a sentence was open, because the first
character of every draft is typed into an empty composer.

**The first cut of the mechanism was defeated by the accident it exists to catch, and the fix is
`2c8f53c`.** The confirm was the arming rune, and the arm ate that rune — so the universal recovery
from a vanished character (type it again) *was* the confirm, and `add` is three card keys in a row.
The confirm is `↵`. The other half was that a card can be blocking an agent with nothing on screen:
`roomPane` clips the key line first, so at pane height ≤ 8 neither press drew anything.
`App.cardFullyDrawn` gates the whole path now. Both were found by review, both were reachable, and
both are named in the mutation battery.

**Allow was armed as well as deny, which is wider than §2d's list**, and deliberately: they are one
`settleCard` writing one irreversible frame, the accident that produces the wrong one is identical,
and an accidental *allow* runs a tool, which is worse than an accidental refusal. Arming only deny
would have left the hole in the direction `cardkeys.go`'s own comment names as unsafe.

### What did NOT ship, and who owns it

1. **~~`⎋` interrupt has no prevention, and the code is in another lane.~~ The targeting half is
   FIXED (`2c8f53c`); the rest stands.** This item was filed *"Blocks: nothing"* and that was
   wrong: it blocked the interrupt half of §2d, which is one of the two things §2d says is left.
   - **~~Its target may not be the agent whose card is up.~~ FIXED.** `interruptTarget` read the
     focused DM else the *roster cursor* and read nothing of `a.cards`. Confirmed by driving
     `App.Update` rather than by reading: with sydney's card on top and the cursor on robin,
     `tea.KeyEsc` wrote `rpc.FrameInterrupt` for **robin**. Two consequences the first filing did
     not draw — `⌃C` shares the target, so `parkWouldDeny` was keyed on the agent that was *not*
     blocked; and `⎋` is the only recorded thing that bounds an accidental allow's *consequence*
     (`interrupt-mid-tool.jsonl` interrupted a running `Bash`), so it mis-fired exactly where it
     was most needed. The card then beat the cursor — **and that middle case is gone as of BUG-2
     (`bugs.md`), with the room's card.** `interruptTarget` is two cases now, the focused
     conversation and the roster pick, so `⌃C` from the room parks whatever is picked and the cost
     recorded here no longer applies. What motivated the ordering is untouched: `parkTarget`
     refuses a blocked agent on the agent's *state*, never on whether a card is drawn.
   - **~~Still open: `⎋` is a single unmodified key that destroys an outstanding ask and a billed
     turn~~ BUILT, `feat/card-offers-interrupt` (2026-08-20), in the shape this entry predicted.**
     The card's unarmed key line carries `esc interrupt` — spelled from the legend's own constants
     so the two surfaces cannot drift — and it is the *first* clause dropped when the line will not
     fit, because the legend carries the same key on every pane where the digits and the refusal
     exist nowhere else. A two-press `⎋` stays rejected for the urgency reason; the key is still
     gated on nothing, and making the destruction visible beside the answer keys is the whole of
     what shipped.
   *Blocks:* the interrupt half of §2d. *Owned by:* whoever designs the card's own interrupt.

2. **A re-ask needs a recording spike and this task did not write one**, because nothing here was
   designed around it. What it would have to establish, none of it recorded:
   - what the CLI does with an ask that was **withdrawn and then reissued** — whether the model
     re-asks at all, and whether the new `request_id` is routable;
   - whether a **stale answer** written after a turn was interrupted errors, is ignored, or lands
     (`2026-08-09-question-findings.md` §9 lists this by name);
   - what a **denied** `AskUserQuestion` / `ExitPlanMode` reads as to the model, and whether the
     agent re-asks (same §9 — *"a denied plan is the more urgent of the two"*).
   Method: `2026-08-09-question-findings.md` §10.

3. **`[d]eny` still cannot carry a typed reason** — the existing Phase 2 Task 10 item stands, and
   the two-press settle does **not** close it. But it changes the shape available: the arm is a
   state in which a card is up, the composer is empty, and the next keystroke is already claimed by
   the card. *"Deny with this text"* could plausibly be *"press `d`, type, press `d`"* — which is
   the one branch that does not re-open the letter-key hazard, because the second `d` is only a
   confirm while an arm is live. **Not built, and it needs the same designing the original item
   asked for**; recorded here so the next person does not re-derive the obstacle.

### Residuals of what shipped, all known and none fixed

- **The arming press eats its rune.** Typing `add the tests` into an empty composer with a card up
  leaves the draft as `dd the tests`. That is the trade taken on purpose — a lost character instead
  of a lost decision — and the card says what happened while it happens. Returning the rune to the
  composer as well would need to write into `Composer` to clear it on the confirm, and it would
  make a one-character draft of `a` or `d` ambiguous with an arm. **Not** the reason it was left the
  first time, which was that `composer.go` was another lane's file; that lane has merged.
- **A digit still eats a rune the same way**, and always did: `1` on a question picks an option. It
  sends nothing, so it is not in this item's scope.
- **~~The card's key line still advertises `[a]llow` / `[d]eny` while the composer is non-empty~~
  FIXED, `feat/card-offers-interrupt` (2026-08-20)**, by exactly the threading this entry priced:
  `Cards.View` takes a `typing` bool from `cardBlock`, and while the composer holds a draft **the
  focused pane's** key line reads `keys return when the draft is sent or cleared` and brackets
  nothing — so the bracket bijection guard sees no rune on a line whose point is that no key is
  live. Scoped to the focused pane because cardKey reads only `a.cardOf(a.focus)`: an unfocused
  pane's card keeps its labels — a pre-existing looseness this entry never covered — since a draft
  in another pane's composer was never what made *its* keys unreadable. The pty harness had
  been asserting the dishonest line by name: `TestACardIsDeniedAndAnArmIsCancelled` awaited `[d]eny`
  with `z` in the draft and then had to backspace before the key worked.
- **A second client can settle the same ask**, and the arm is per-client. Unchanged by this, and
  the same residual `Cards.Reconcile` already carries.
- **`docs/superpowers/specs/2026-08-12-phase-4-scope.md` §1 still reads NOT BUILT** for undo. Left
  deliberately: it is *"a draft for the owner to argue with"* and this item is now partly shipped,
  partly deliberately excluded and partly deferred, which is a status call for the owner rather
  than for the branch. Recorded here because it is rung 7's second-instance shape — a claim about
  the build, in prose, deriving from nothing.

---

## From closing outstanding bug 3 — `⌃Q` waits for the daemon, 2026-08-12

Branch `fix/park-confirm`. `⌃C`'s half closed on 2026-08-11; this is `⌃Q`'s, plus what the work
turned up and did not take.

**What shipped.** `⌃Q` writes `rpc.FrameStatus` behind its `rpc.FrameParkAll` on the same
connection and does not end the program until the daemon has answered. `serveClient` dispatches one
connection's frames synchronously and in order, so that reply cannot come back before the ParkAll
was dispatched — which is `beginQuit(quitPark)`, the point at which the daemon is provably ending
as a park. **No new frame kind and no daemon change**, which is Task 14's precedent taken as-is.
`ui.App.ParkedFleet` gains the reason as a third return so `cmd/wake.leavingLine` cannot read the
count without it, and `parkAllLine` promises a parked fleet only where one was confirmed.

**What did not transfer from `wake mcp`, and it is the whole design difference.** `act` dials a
connection per call and blocks reading it with nothing else on it. A TUI has one long-lived
connection carrying the whole fleet's events, drained by `internal/ui/inbox.go`'s pump, and nothing
that renders *or blocks* may sit between that socket and the ring. So the answer arrives the way
every other frame does and the wait is a state on the model. `ui.parkAllSettled` is the one place
the program may end for `⌃Q`, reached from four — the reply, a write that failed, the deadline, the
hang-up — first one wins.

### Still open, and re-ranked out of item 3

**A park the daemon could not *write down* reaches only the daemon log.** Task 5's m1, carried
through item 3 and **not taken here**. `completePark`'s `s.parked.add` failure is logged and
nothing else; `agent.stateLocked` reports the session parked regardless, so `⌃C`'s confirmation
says *"@alex is parked — /resume alex brings it back"*, which is **true until the daemon exits** and
false afterwards. `bookParked` already counts this (*"N are parked and could not be written down"*)
and also only logs it. It is `internal/daemon/park.go` rather than `internal/ui`, it needs a frame
or a status field to reach a client, and it is a different fix from this one — so it is its own
item now rather than a clause on a closed one. **Severity: Important.** It is the one remaining way
an operator is told a conversation is recoverable when it is not.

**The reply loses a race with `closeClients` when there is nothing to stop, and the numbers are
measured.** `client.close()` closes the connection without draining what is queued on it, and
`shutdown` reaches `closeClients` in microseconds when `takeAgents` returns nothing live. Against a
real daemon on a real socket, 25 runs each:

| fleet | reply arrived | mean | worst |
|---|---|---|---|
| one live agent | **25/25** | 48µs | 185µs |
| nothing live | **15/25** | 57µs | 118µs |

So over a fleet somebody had already parked with `⌃C`, an EOF is the *ordinary* end of a successful
`⌃Q` as much as it is a crash — and nothing on the wire separates them, which is `wake stop`'s own
sentence about its own EOF. It is therefore reported as *"could not confirm"* with the second clause
in the error text rather than as a third branch, because a line that reads as alarm is a line nobody
reads by the third time.

**The close, if it is ever worth it, is a bounded drain in `closeClients`** — write what is queued,
under the write deadline the writer goroutine already uses, before `c.close()`. It only ever
*delays* the EOF, which strengthens rather than weakens the invariant that the park book is complete
before anything can read it. **Deliberately not taken here**: it is a change to the shutdown path,
whose ordering `EnsureRunning`'s discriminator depends on, from a branch whose subject is a client.
Whoever takes it owns `TestTheParkBookIsWrittenEarlyAndForgottenLate`'s neighbourhood.

**The acknowledgement cannot tell its own reply from a spawn's.** `launch` confirms every spawn,
fork, wake and import with the same `rpc.FrameStatusReply` and no correlator, so a `/new`, `⌃F` or
`/resume` still in flight when `⌃Q` is pressed has a reply of its own coming and
`ui.parkAllTaken` takes the first one. The window is one round trip wide on a connection that is
provably alive and the failure needs the daemon to die inside it — but it is a real narrowing gap
and it is stated in `park.go`'s own header. **Closing it needs a correlator on the frame**, which is
a daemon change, and the whole point of this instrument is that it is not one. Worth revisiting
with whatever next gives `rpc.Frame` a request id.

**`⌃Q` did not get materially slower.** The added cost is one round trip on a socket the client is
already connected to: **48µs mean, 185µs worst, over a fleet with a live agent.** The 3s
`parkAckTimeout` is only ever reached when something is already wrong, and the hang-up settles
immediately rather than waiting it out — so the slowest ordinary `⌃Q` is unchanged to the eye.

**What the operator sees between the keypress and the confirmation is unchanged, and that is the
decision.** `parkingFleet` already named the ask rather than the outcome (*"parking N agents and
closing Wake"*), so the pending state existed and was honest; what was dishonest was the **exit
line**, printed unconditionally off a flag the keypress set. At 30 agents an extra pending line
would be a row nobody reads for 48 microseconds, and a failure drawn on the notice row is drawn
inside an alt screen that is torn down a frame later. So the failure lands on the exit line, which
is the surface that outlives the window, and it is the only new sentence.

### Not taken, and named

- **`⌃Q` still exits 0 when it could not confirm.** `wake stop`'s rule is that anything short of
  "nothing alive" is a non-zero exit; this prints an honest line and exits 0, because nobody
  scripts a TUI keypress and the change has its own blast radius. Worth a ruling by whoever cares
  about `wake` in a script.
- **Item 16 (`"parking 1 agents"`) is untouched**, and the exit line's `(%s)` now carries
  `agents(n)` on the failure path too — so the *exit* line pluralises on both branches and the
  notice row still does not.
- **The `frameMsg` arm of `Update` is a production arm with no production producer.** Pre-existing;
  it now also carries `a.closing()`, which is one line of correctness on a path only tests reach.

---

## 2026-08-12 — from the liveness watchdog redesign

The finding itself (`~86,400 ps spawns a day at 30 agents`) is **fixed**, `fix/liveness-watchdog`:
one `ps` for the whole fleet, on a schedule that decays from each agent's last event and is reset
by one. Measured on the same machine and window: 30 agents 250 spawns → 4, and flat in fleet size.
What follows is what the work turned up and did not close.

**Wake — a dead child is an event, and Wake cannot receive it without changing `internal/core`.**
The brief for this work suggested watching the process rather than asking about it, on the grounds
that *"a `Wait` already runs per session in `internal/core`"*. **It does not, in the one case that
matters.** `core.pump` scans stdout to EOF and only then calls `finish`, which calls `cmd.Wait()` —
and the failure the probe exists for is precisely the scan never reaching EOF, because a grandchild
holds stdout. So `Wait` is never entered and the child stays an unreaped zombie, which is what
`ps` state `Z` is detecting. Two consequences worth writing down:

- **The zero-cost fix is real and it lives in `core`.** A `cmd.Wait()` running concurrently with the
  scan would return within `WaitDelay` of the process exiting even with the pipes held, and that
  return *is* the event — no `ps`, no schedule, nothing on a timer at all. It is not done here
  because the naive version has a serious regression in it: `Wait` closes the stdout pipe when the
  process exits, so the scanner would get `ErrFileClosed` instead of a clean EOF *depending on
  timing*, and `scanStopError` reports that as a session that failed rather than one that ended.
  Every ordinary ending would become nondeterministic. Doing it properly means ordering the
  teardown so the forced close happens only after a grace once the process is known gone — real
  work in the most load-bearing file in the tree, and it belongs to whoever owns `internal/core`.
- **`SIGCHLD` is the obvious trigger and it is a trap here.** It is free, it is a wait rather than a
  poll, and the kernel guarantees it for exactly this case. But the daemon's own `ps` children raise
  it too, and the probe cannot tell them apart — signal delivery is coalesced and the forwarding to
  the channel is asynchronous, so draining after a pass races the pass's own signal. Every variant
  tried degrades to a permanent loop at the rate limit: probe → `ps` → `SIGCHLD` → probe. It is only
  safe once the probe spawns nothing, which is the item above.

**Wake — `wait4(pid, WNOHANG|WNOWAIT)` reaps the child on darwin, and there is no portable
non-forking substitute.** Recorded as a spike rather than assumed, because it was the pivot of the
redesign: if the parent could ask the kernel "has this child exited?" without reaping, the probe
would spawn nothing and `SIGCHLD` above would be safe. Measured on darwin/arm64, go 1.26.5:

```
wait4(WNOHANG|WNOWAIT)  live child -> 0 (alive)          err=<nil>
                        zombie     -> pid (exited)       err=<nil>
                        cmd.Wait() afterwards -> "wait: no child processes"
```

So it answers **correctly** and then **reaps anyway** — no `EINVAL`, no signal that the flag was
ignored. `WNOWAIT` is documented for `waitid(2)` on macOS and not for `wait4(2)`, and XNU accepts it
silently. Building on it would have broken `core.finish`'s account of how *every* session ended,
which is the accepted-exit-0-and-wrong shape `identityArgs` already exists for. `unix.Waitid` is
**not defined for darwin** in `golang.org/x/sys` (linux only), so the portable route is closed too.
*What would re-open it:* cgo, or a darwin `waitid` wrapper — both a bigger change than the thing
they buy, unless the `core` item above is taken first, at which point neither is needed.

**Wake — two concurrent runs of `internal/daemon`'s suite on one machine fail each other's wake
tests, and it is not flakiness.** Found while verifying this work, after wrongly concluding twice
that the failures were pre-existing and then that they were mine. Neither: `resumeSafe` →
`idsInUse` runs `ps -Aww` over the **whole machine**, and the fixture session ids (`idAlpha`,
`idBeta`) are package constants — so a second `daemon.test` binary running at the same time has a
fake `claude` whose argv carries the same id, and every wake is refused with *"a process is still
running under session …"*. Observed directly, two binaries from different build dirs:

```
27782 ... claude ... --resume a11a0000-0000-4000-8000-00000000a11a --name alex    (parent 24549)
27820 ... claude ... --session-id a11a0000-…0997076aa11a --name sydney            (parent 26209)
```

It affects `TestAWokenSessionComesBackUnderItsOwnIdAndName` and six others, appears under `-race`
and without it, and is invisible on a machine running one lane. **It is a property of the fixture
ids, not of the code under test** — nothing in the daemon is wrong. *What closes it:* mint the
fixture ids per test binary (a UUID stamped once in `TestMain`) instead of using shared constants,
so two runs cannot collide. That is a change to `main_test.go` which several lanes touch, so it is
recorded here rather than taken as a drive-by. Until then, **a red wake test on a shared machine is
not evidence** — check `pgrep -f daemon\.test` before believing it.

**Wake — three `wake daemon` processes have been running for over two days from scratch sockets.**
Seen while investigating the above: pids under `/private/tmp/claude-501/wk/` and `.../wk4/`, up 2–3
days. They are on scratch sockets rather than `~/.wake/daemon.sock`, so they are leaked test or dev
daemons and not the owner's fleet — but nothing reaps a daemon whose socket directory was deleted,
and they hold whatever agents they started. **Not touched**, because `wake stop` is this project's
one irreversible verb and these are not this lane's to end. *What closes it:* the owner deciding,
or a rule about daemons whose socket file no longer exists.

**Wake — the pane header has no `✕`, so the mouse cannot close a pane.** Spec §8's own diagram draws
one (`│ @sydney ✕ │`) and §17 puts "Mouse throughout" on the In list. Proposed alongside the bounded
grid and deliberately not taken with it. *Why not:* the name is drawn on the **composer's** top
border, near the bottom of a pane rather than at its top — so a `✕` beside it needs a pane-relative
row the frame does not currently expose, and `App.paneAt` would gain arithmetic derived from
`Composer.View`'s internals rather than from `Regions`. That is a number nothing asserts, which is
the failure mode this file's own rule names. *What closes it:* have the composer report the row its
titled edge lands on, assert that against a rendered frame, and hit-test against **that** rather than
against a recomputed offset. Until then the mouse closes a pane in two steps — click it, then `⌃W`,
which is bound and in the legend.

**Wake — the composer's target line and legend are not right-padded, so the rightmost pane's short
rows stay short.** Found by `internal/ui/frame_test.go`.
`lipgloss.JoinHorizontal` pads every block but the last, so a row like ` → @name or @all` reaches 16
columns in a 90-column frame. **Reproduced on a room with no grid open at all**, so it predates the
grid and is not its doing. *Why it is not urgent:* short is the harmless direction - the renderer
clears to end of line, and it is a row *wider* than the terminal that wraps and scrolls the alt
screen. `TestTheFrameNeverExceedsTheTerminalForEveryArrangement` holds that direction and
deliberately does not hold this one. *What closes it:* pad the composer's two chrome lines to the
width they were given, and tighten that test from `>` to `!=` in the same commit.

**Wake — `output_style` and `fast_mode_state` have no picker, and both are on the `init` frame.**
This branch shipped `/effort` and `/model`; those two are the next candidates and are the *safe*
kind, because the `init` frame reports both — so a label built on either self-corrects within a turn,
which is what makes the model's picker honest and the effort one's a memory. *Why not now:* every
picker spends fence budget. `slash.go`'s rule is that a word claude advertises may be claimed only in
a form a committed recording shows inert, so each new word needs **its own** recording under
`testdata/stream/`; the two here have theirs. *What closes it:* record the bare form, add the entry to
`bareOnlyCommands` with its fixture, and give `pickerOptions` a case — the guards fail until all
three exist.

**Wake — `--max-budget-usd` is exposed as of 2026-08-16; what stays deferred is the *observation*.**
The flag is on `spawnOpts`, `rpc.Frame`, `parkedRecord` and `core.Config`, exactly as this entry
specified, and the banner names it in effort's voice — the decision it asked for. **The half that
did not close is spend against the cap.** `total_cost_usd` is a level Wake already reads, so a
percentage is arithmetically available and is still refused: the number **resets to zero on
`/clear`**, so a naive delta silently loses everything before the reset, and a progress bar built on
it would under-report at exactly the moment an operator most wants it. Accumulating per session-id
epoch is what would fix that and is a real piece of work, not a display change. The same absence is
why `agent_status` does *not* report the cap to the manager — a ceiling with no progress beside it
tells a model an agent may stop without saying whether that is imminent, and a manager holds
`send_to_agent`. *What closes it:* per-epoch spend accumulation on the agent, at which point the
cap and the spend are a measurement rather than a claim, and the manager's exemption in
`notInTheStatusReport` can be revisited in the same change. **And what happens when the ceiling is
actually reached is unrecorded** — no fixture in `testdata/stream/` covers it, so nothing is
designed around it; a recording of a session hitting its cap is the cheapest next step.

**Wake — a failover is invisible, and `--fallback-model` is passed anyway.** Landed alongside the
cap. The flag is emitted and survives a park, and *nothing* reports that it fired: the frames a
failover produces are an ordinary turn's, and the one observable that would say so — `init.model` —
is per-turn rather than per-failover, so a session that fell back reads as a session that was
started that way. *Why that is acceptable:* the alternative is not passing it, and an overloaded
model with no chain does not stop one agent, it stops every agent running that model at the same
moment, which is the failure the flag exists for. Being unable to *see* the failover is strictly
better than the failover not happening. *What closes it:* comparing `init.model` against the
`Model` the session was started with and saying so once when they differ — cheap, and deliberately
not taken here, because it is a claim about a model rather than about a cap and belongs with
whoever next touches the status bar.

*One thing found while building it and deliberately not acted on:* the 2.1.233 bundle carries
`"Fallback model cannot be the same as the main model"`, which reads like a constraint Wake should
enforce and is **the Agent SDK's own check**, thrown in the statement that pushes `--fallback-model`
onto an argv it is about to spawn. Wake builds its own argv and never takes that path, so nothing
recorded says what the CLI does with `--model opus --fallback-model opus`. A string comparison could
not close it regardless — `opus` and `claude-opus-5` may be one model and nothing in this tree
resolves an alias. *What closes it:* one recorded spawn with the two equal.

**Wake — a session's effort is readable after all, and nothing reads it.** `docs/notes/deferred.md`
and `internal/daemon/effort.go` both rested on *"effort is on no frame Wake receives at all"*, which
was true of every frame Wake receives **unasked**. The bare `/model` reply carries it —
`Current model: Opus 5 (1M context) (effort: xhigh)` — and §1 of
`docs/superpowers/notes/2026-08-13-bare-command-findings.md` records that costing `num_turns: 0` and
`$0`. So the daemon *could* confirm the level it believes, for free, instead of only remembering what
it asked for. *Why not now:* two reasons and the second is the real one. Wake claims the bare
`/model` for its own picker, so it would have to send one deliberately and then swallow the reply,
which is a turn the operator did not type appearing in their transcript. And the level would be
parsed out of **English an assistant wrote**, which is a different class of thing from reading a JSON
field — the airlock exists to keep that distinction. *What closes it:* a decision that the daemon may
issue a probe of its own, plus somewhere to put the answer that does not read as something the
operator said. Until then the pane goes on showing the level Wake **asked for** and saying nothing
about confirmation.

**Wake — the manager may not set effort or model on other agents, and this is a refusal rather than a
gap.** `cmd/wake/mcpguard_test.go` already refuses `mode` on the argument that a manager which could
set one would be *the fleet deciding it will not be asked*, in every future decision that session
makes rather than one, and that it shows up in no row the tool surface returns. Effort and model are
the same class and cost more per turn. The MCP surface stays at five tools. *What would reopen it:* a
version of `managerVerbs` where a configuration change is visible in the room as an event, at which
point the argument is about cost rather than about invisibility.

**Wake — a question card has no free-text channel, where claude's own question screen has two.** The
native screen offers *"press n to add notes"* and a *"Chat about this"* line beside the options, and
Wake's card offers neither: the options, the digits, `↑↓`/`↵`, and a refusal carrying one fixed
sentence (`cardDenyReason`). *Why not now:* nothing records how either travels. The recorded corpus
covers the answer path exactly once — `updatedInput.answers`, question text to option label
(`question-answer.jsonl:37-38`) — and there is no recording of a note riding alongside it, so a field
invented here would be §9's forbidden shape in a second place. The deny reason *does* reach the model
verbatim, which makes it the one channel that exists, and it cannot be typed because the composer has
to be empty for a card key to be read at all. *What closes it:* a recording of a question answered
with notes, or a decision that the refusal may be composed — which is the same unresolved question
`cardDenyReason`'s own entry above already names, now with a second caller.

**Wake — the per-question header and the per-option preview are resolved as of this branch; only
`multiSelect` is still dropped.** `core.AskDetail`'s doc comment used to rule out all three together,
and the argument for the first two was the room's: a preview is a document, the room is a hub, and
deep reading is DM work. That stopped holding when a question became answerable in a conversation,
which is a 1:1 pane with room to read in. `Question.Header` and `Option.Preview` now carry them,
`header` and `preview` are policed vocabulary, and `internal/ui/preview.go` draws the sample in three
tiers — beside the options, stacked under them, or dropped. **The room's original ruling survives as
the third tier**, which is why dropping is a designed outcome rather than a failure case. The
multi-select entry above is untouched and is still the only field left out.

---

## 2026-08-14 — a column drag jumps where a row drag glides

**Wake — dragging a vertical divider moves nothing until the drag ends, while dragging a stacked
column's rule tracks the pointer.** Asked for by the owner after a real screen, and it is the cost
model rather than an oversight. `App.mouse` applies a rule drag at once — `Layout.DragRule` then
`resizePanes` — because rows are free: `DM.SetSize` re-renders only when the *width* it is given
differs from the one the transcript is wrapped for, so a taller or shorter pane moves a window over
lines that already exist. A divider drag goes through `changeGeometry` instead, which puts the new
`Weights` on `App.pending` and calls `applyHeightOnly` — and `applyHeightOnly` writes the height and
nothing else. `App.regions` reads `a.layout.Weights`, so **the divider stays where it was until 80ms
after the button comes up**, then jumps to where the drag left it. `clipMidDrag` gives no partial
relief: it fires only when the *terminal* has become narrower than the layout, which a divider drag
never makes true. The debounce is load-bearing — a re-wrap is 248ms at 3,000 events, measured, and
mode 1002 delivers a motion message per cell crossed.

*Why not now:* smoothness here is not the free thing it is for rows, and the honest version of it
changes what the frame means mid-drag. The approach that keeps the cost model is to separate **the
width a pane is wrapped for** from **the width it is drawn in**: render each pane at its current
wrap width, then clip the shrinking side and pad the growing side out to the pending width, per row.
That buys a divider that follows the pointer for one ANSI-aware pass per row — the instrument
`clipMidDrag` already uses and this tree has already priced — with the real re-wrap still happening
once, on settle. What it does not buy is reflow: mid-drag the shrinking pane's lines are cut at the
new edge and the growing pane shows a blank gutter, and the prose snaps into place on release. There
is no version of that without paying the 248ms, so it is a thing to decide rather than a detail to
get right.

*The trap, and it is silent:* `DM.View` re-enters `SetSize` whenever the width it is handed differs
from `d.width`. So a pane drawn at the *pending* width re-wraps on every motion message with nothing
on any wire to say so — the debounce defeated by the change meant to make it look better. Mid-drag
the panes must be drawn at the old width and clipped afterwards, never sized to the new one.

*The second one:* `Layout.Hit`, `Layout.PaneLeft` and the selection's anchor all read `a.layout`.
Draw at the pending weights and the screen is at one geometry while the mouse is answered at
another, for the length of the drag plus the settle. A button is held throughout, so a click cannot
land in the wrong pane — but a selection taken before the drag, and the wheel, both can.

*What closes it:* a draw path that takes wrap width and draw width as two arguments rather than one,
a per-row clip-or-pad that is a strict no-op while `App.dragAt` is `noDrag`, and one source of truth
for the mouse's geometry during a drag. The guards that decide whether it worked already exist:
`TestADividerDragCostsOneReWrapPerPaneAndNotOnePerColumn`,
`TestADragCostsOneReWrapWhenTheLoopRunsEveryCommand` and
`TestTheFrameIsNeverWiderThanTheTerminalMidDrag`, plus `idle_bench_test.go`'s 248–268µs budget for
`App.View`, which is what catches a clip left running on every frame rather than only during a drag.
The two alternatives are worse and are written down so nobody re-derives them: live reflow is the
248ms the settle exists to avoid, and a rubber-band divider drawn at the pending column while the
panes stay put draws a `│` through the middle of a pane's text.

**2026-08-15 — a room message aimed at one agent does not come back.** `roomHistoryLines` restores a
turn you typed only when two or more transcripts hold the same text within `broadcastWindow`, because
one transcript cannot distinguish `@noah do this` sent from the room from `do this` typed into noah's
own pane, and a DM is private. So `@all` and `@noah,@robin` come back and a single-target room
message does not. *What it blocks:* nothing. *What closes it* is something on the wire that survives
into claude's transcript and says which surface a turn was typed on — there is nothing today, and
`FromRoom` is explicitly not it (no frame carries it and nothing on the wire could). **Do not close
this by relaxing the rule to one transcript**: that is the private-turn-in-the-group-chat outcome the
rule exists to prevent, and it is silent.

**2026-08-15 — the room's restored prefix holds nothing Wake itself wrote.** `!cmd` output, notice
rows and the `/mcp` panel are drawn into the room and are in nobody's transcript, so a reopened room
has the agents' side of an hour and none of Wake's. *What it blocks:* nothing. *What closes it* is a
room log of Wake's own, which the ruling in `decisions.md` refused for the non-negotiable rather than
for cost — so closing this means re-arguing that, not writing the file.

**2026-08-15 — the room's working line names one agent and counts the rest, so a second long turn is
a number.** `roomWorkingLine` is one row by construction: a row per working agent is thirty rows of
transcript at fleet size, and rows that come and go move the pane's height. With three agents working
the two that are not the oldest are `+2 more working` and nothing else — no name, no age. *What it
blocks:* nothing; the roster draws a spinner per row and `⌃X` reaches a blocked one. *What closes it*
is a decision about what the room may spend on chrome, which is the same question a room last-read
marker asks, so the two are worth taking together.

**2026-08-15 — a fork's own conversation is not restored to the room either.** `isFork` refuses a room
history ask for any session carrying a `ParentID`, because a fork's transcript opens with every line
its parent had at the moment it was taken — and the parent is usually in the same report, so asking
draws that prose twice under two names. What it costs is the fork's *own* turns after the fork point,
which nothing brings back. *What it blocks:* nothing. *What closes it* is knowing where the fork point
is in the file: `daemon.launch` knows the parent and the moment, and `parked.json` deliberately holds
neither a ParentID nor anything else about lineage. A `--fork-session` boundary marker in the
transcript would close it outright; nothing recorded says there is one, so **check the corpus before
assuming**.

**2026-08-15 — a session that speaks while its history ask is in flight loses that history entirely.**
`askedAt.lines` drops the whole batch rather than the overlapping turn, following `history.go`'s
shipped trade: a conversation that appears to have happened twice is worse than one that appears not
to have happened at all. On a busy fleet this is the chattiest agents losing their restore most often,
which is the opposite of who you want it for. *What it blocks:* nothing. *What closes it* is folding
per event instead of per batch — the room would need to recognise a live line it has already drawn,
and matching on `(session, kind, text)` is the obvious way and is wrong for an agent that says "done"
twice. **Do not take this without deciding what identity a room line has.**

**2026-08-15 — a restored room holds the broadcasts and nothing else, which on some fleets is almost
nothing.** An agent's prose comes back only inside a turn two transcripts prove was a broadcast, and
prose with no initiator inside the 400-event window is dropped — which is most of a tail, since a tail
usually opens mid-conversation. A fleet driven mostly from conversation panes therefore restores a
nearly empty room. **This is the deliberate cost of the privacy rule** (`decisions.md`), taken by the
owner over the alternative, so it is not a bug to be fixed by relaxing the rule. *What closes it* is
provenance that survives to disk: today nothing records which surface a turn was typed on, `FromRoom`
is explicitly not it, and `parked.json` holds nothing of the kind. A marker Wake wrote into its own
messages — a zero-width prefix, a trailing sentinel — would close it and is a change to what the
*model* reads, so it needs its own argument before anyone tries it.

**2026-08-15 — a synthetic user record closes a public turn in the restored room, and drops the prose
after it.** `collapseBroadcasts` and `broadcastIndex` treat every `KindUserText` as an operator turn,
and the corpus has two that are not: the compaction summary and the `<local-command-stdout>` line
(`compaction.jsonl`, the only line in 1004 carrying the envelope). `fold` already drops a subagent
prompt and an interrupt marker — `Subagent`, `Notice` — but neither of these two carries either, and
`stripLocalCommandStdout` unwraps the envelope rather than resolving a notice. So one of them lands
mid-transcript, reads as a new turn, closes the broadcast that was open, and the agent's public answer
after it is dropped.
*Direction:* safe. It drops prose that was public rather than exposing prose that was private, which
is the direction everything else in this feature errs.
*What it does **not** do,* checked rather than assumed: `compact_boundary` is a **system** subtype and
`DecodeTranscriptLine` keeps only `assistant` and `user` records, so the boundary frame itself never
reaches the restore. And since a run containing one session twice is now refused outright, two
identical synthetic records opening a *false* broadcast needs them to be byte-identical across exactly
two sessions inside five seconds, which session-specific prose is not.
*What closes it* is a genuine-operator-turn flag carried out of `DecodeTranscriptLine` — an airlock
change, so it needs the corpus read first for how each record is actually identifiable. **Not
`Echoed`**: `core.Event.Echoed`'s own header forbids keying suppression or de-duplication on it, in
those words, because it conflates replay with synthesis and nobody has watched what
`--replay-user-messages` emits.

---

## 2026-08-15 — token-level streaming: what shipped, and the five things it did not prove

`--include-partial-messages` is emitted and a conversation shows an answer as it is written. The
granularity ruling and every figure behind it are in `decisions.md`; the headline is that the
per-token render this feature obviously wanted is **65× the shipped design on a 1,024-token block**
and superlinear, and that the shipped design costs **under 1 % of one core** for a second of thirty
streaming agents — with or without every conversation open, which took a second measurement and a
gate (`App.wants`) to make true. **The cost was bounded, so it shipped.** What follows is what the
measurement did not reach.

### 1. ~~The schema has no recording behind it~~ — **CLOSED 2026-08-21**, by the one turn it asked for

`testdata/stream/partial-turn.jsonl` (2.1.238, `test/partial-messages-fixture`). Every claim below
held: the envelope as transcribed, the text delta as read, and — the load-bearing one — partials
arrive *in addition to* the completed `assistant` frame, byte-identical to it. The five
`notInTheCorpus` excuses are deleted and the vocabulary guard holds the words to bytes. The
multi-message token fold was recorded the same day (`debug-runtime.jsonl`, three messages summing
exactly to the result's figure); findings and provenance:
`docs/superpowers/notes/2026-08-21-partial-messages-findings.md` and
`2026-08-21-runtime-command-findings.md`. The original entry:

**The highest-value item here, and it is a recording rather than code.**

`wireStreamEvent` is transcribed by hand from claude 2.1.233's own zod schema — the standing this project
already gives the `TodoWrite` vocabulary — stronger than documentation, weaker than a frame — but it
makes `stream_event` the **one inbound shape in the airlock with nothing recorded behind it**, which
is a first. `airlock_test.go`'s `notInTheCorpus` carries the three words with that reason, so the
excuse is visible rather than implied.

`partialEvent` is written to survive being wrong: every shape it does not recognise yields **no
event at all**, so a moved schema costs the preview and never the transcript. That covers the field
names. It does **not** cover the assumption the whole design rests on:

> **Partials arrive *in addition to* the completed `assistant` frame, not instead of it.**

If that is false, a streamed turn is missing from the transcript entirely and the design has to be
rethought rather than tuned. The bundle says it is true — the SDK adapter maps `stream_event` and
`assistant` through separate arms and the consumer handles both — and no recording says anything.
`docs/live-testing.md` §15 asks for the turn, in those words, and its first check is this one.

### 2. The inbox's margin roughly halves, and its drop notice starts lying — **CLOSED 2026-08-16**

**Closed by `fix/partials-and-the-drop-notice`, to this entry's own prescription and one rule
further.** The finding below is left standing because it is the argument; what changed is:

- **A partial folds** into its session's unconsumed one in the ring rather than taking a slot
  (`inbox.folds`, a per-session index under `b.mu`, exactly as prescribed). A frame of any other
  kind for that session closes the fold, or the next block's first tokens would be appended to a
  preview that block is about to clear. Occupancy no longer depends on the token rate:
  `TestOneSessionsTokensShareOneSlot` streams 4,096 tokens into one slot.
- **A partial never evicts.** The fold alone does not say that — a session with no fold open,
  arriving into a full ring, still dropped the oldest — so one that finds no room is dropped where
  it stands. `TestAFloodOfTokensCannotEvictAnAsk` is the reproduction: before the fix, a permission
  request behind four ring-fulls of tokens was gone and 6,145 frames were reported dropped.
- **`dropped` counts the record and nothing else**, at both ends of that, which is the notice half
  this entry says goes with the buffer half.
- **`internal/daemon/client.go` got the same rule**, since its `flush` tells the same lie for the
  same reason. `partialCeiling = clientQueue / 2` reserves half the queue for the record and a
  dropped preview is not confessed. It is a ceiling rather than a fold because the daemon sends into
  a channel and cannot reach the far end of it — this file's own §3 and `client.go`'s header both
  say so.
- **Measured, not asserted.** `BenchmarkInboxStall` prices the fold at ~100ns and ~350 bytes a
  token: 161–192µs per fleet-second of tokens against 54–62µs for the same frames as record, which
  is ~2% of the 7.4–8.3ms that fleet-second costs through the real Update and View.
  `BenchmarkClientEnqueue` says a preview on a full queue is 4.4ns against a record's 8.9ns, both
  allocation-free.

What is **not** closed by it is §3 below, narrowed: the daemon's per-frame *encode and write* cost is
still unpriced, and the soak still does not stream.

---

`inbox.go` sizes `inboxFrames = 2048` against a stated busy fleet of *"30 sessions at ~100 frames/s"*
— 3,000 frames/s, so the 250 ms stall it exists to cover is ~750 frames and the ring is "nearly three
times that". This feature adds a frame per output token: **~1,300 frames/s** at the corpus median
across 30 agents and **~2,800** at its recorded maximum. So the same 250 ms stall becomes ~1,075
frames at the median and ~1,455 at the maximum, and the margin falls from **2.7× to roughly 1.9×–1.4×**.

Still positive, and the ring still covers the stall it was built for. What is worse than the
arithmetic is what happens at the boundary:

- `App.stream` reports a drop as *"this window could not draw fast enough, so the conversation above
  has a gap"* and calls `forgotModes()`. **With partials that sentence is usually false.** A dropped
  partial costs nothing — the completed block follows it — so the notice would announce a hole that
  is not there, and `forgotModes()` would discard a permission-mode belief for no reason. The wire
  says nothing that separates the two.
- The fix has a shape and it is the one `inbox.go`'s own header argues for. That header says the
  buffer *"holds the whole buffer and can choose"*, which is exactly the licence needed: a partial
  arriving for a session that already has an unconsumed partial in the ring can **fold into it**
  rather than take a slot, since deltas are additive. That makes ring occupancy independent of the
  token rate and makes the drop notice mean what it says again. It costs a per-session index under
  `b.mu`, in the one lock on the path between the socket and the draw loop, which is why it was not
  done as a drive-by.

**Whoever takes this owns the notice as well as the buffer.** The cheap half — excluding
`KindPartialText` from the dropped count — is wrong on its own: it makes the number honest and
leaves the ring just as full.

And note what `App.wants` does **not** do for this. Gating the accumulate on a pane being drawn cut
the client's *fold* cost by an order of magnitude at thirty open conversations, and it changes
nothing at all about what arrives: every partial still crosses the socket, still enters the ring,
and is still discarded one layer above it. The rate is the daemon's to reduce, not the App's.

### 3. The daemon's side of the frame rate is unmeasured — **narrowed 2026-08-16**

**The queue half is closed** (see item 2): `client.enqueue` is priced in `cost_bench_test.go` at
8.9ns for a record and 4.4ns for a preview, both allocation-free, and `partialCeiling` keeps a
flood of previews out of the room the record needs. **The encode and write half is still open**, and
it is the larger one — `rpc.WriteFrame` JSON-encodes each event once per attached client, on the
writer goroutine, and nothing has priced that against ~1,300 frames/s with several clients attached.

Every figure in `decisions.md` is **client-side**: `App.Update`, `Fleet.Observe`, `DM.Append`,
`App.View`. Nothing has priced that other end, and the other end is where the multiplication starts.
`internal/daemon`'s own `cost_bench_test.go` is where it belongs, beside `BenchmarkClientEnqueue`.

Two reasons it was left: a partial is ~150 bytes of `rpc.Frame` header around a few characters, so
the bytes are small even where the count is not; and `Event.Raw` does not cross the wire, so the
per-frame payload is genuinely the delta. Neither is a measurement. **Take one before trusting this
at 30 agents with several clients attached** — and note that the soak lanes do not stream either,
which is the same gap as items 1 and 2 of the ranked list one section up.

### 4. `withDM` copies the whole map per write, and streaming is what made that visible

**CLOSED 2026-08-24 — PR #102 (`perf/withdm-pointer-map`).** `App.dms` is now `map[string]*DM`, so `withDM` clones N pointers instead of N `DM` headers; the absent-key/write-through sites (`dm()`, `attachedName()`, `dmFor()`, `clearTaskCursor()`, `echoToRouted()`) are fixed; residual per-fleet-second allocation ~17MB→~47KB. Follow-ups: the `ttft_ms` field and the daemon-side `WriteFrame` benchmark.

**Pre-existing, not caused by this change, and now measured.** `App.withDM` is the one write path
into `App.dms` and it clones the entire `map[string]DM` on every call — deliberately, and its comment
gives the reason: Bubble Tea hands models around by value, so a shared map makes a discarded App's DM
keep growing. The cost is O(conversations ever opened) **per event**, of a value type carrying a
transcript, an event log and a composer.

Nothing noticed because the event rate was low. Streaming multiplies it by the token rate, and
`App.wants` removes the 28 undrawn conversations from the *numerator* without touching the map copy
the two drawn ones still pay: `BenchmarkStreamingFleetSecond` reports **7.4–8.3ms with one
conversation open and 10.3–10.6ms with thirty** — same tokens, same two panes drawn, ~30% apart, and
2MB against 19MB allocated. It grows with how many agents somebody has looked at, which in the
ordinary use of this app only goes up.

Bounded and small today, so it did not block this. **The fix is a real one and it is not this
feature's**: a copy-on-write map is the wrong structure for a value that is written per event and
read per frame, and the cheap version is to key `dms` on a pointer-to-immutable-DM so the clone
copies pointers rather than structs. Whoever takes it owns `withDM`'s comment, which is currently
the correct argument for the wrong data structure.

### 5. Three things decoded to nothing on purpose, each of which somebody will want

All three are one arm in `partialEvent` and none is a gap in the schema:

- **`thinking_delta`.** Claude Code streams thinking as it is generated. Wake folds thinking shut in
  the DM anyway, so streaming it would be per-token work on a block nobody is reading. Reopen it the
  day thinking is expanded by default, and reopen the *cost* question with it: the preview holds one
  tail per conversation, and a second stream needs a second one or a rule about which wins.
- **`input_json_delta`.** A tool call's arguments as they are typed, which is what Claude Code's
  own "⏺ Bash(…" filling in live is made of. It arrives whole on the `tool_use` block a moment
  later, exactly like text — so the same preview shape would work, and the *sidebar* is arguably a
  better home for it than the pane: `Agent.ToolArg` already exists and is drawn per agent.
- **`ttft_ms`, on the frame and not on the event.** Time to first token, handed over for free. The
  working line carries the turn's age and CLAUDE.md's owner-request item 2 records that Claude also
  shows `↓ 59.5k tokens` there; latency-to-first-token is the one number on this frame that says
  something the age does not — a turn that is thinking versus a turn that is queued behind a rate
  limit. It needs a field on `core.Event` and nothing else.
---

## 2026-08-15 — inline completion: what it does not do

The composer now offers what could finish a `/command` or an `@` (`internal/ui/completion.go`,
`completionpath.go`). Four things were deliberately left, and the first is the only one anybody is
likely to notice.

~~**The menu is pinned above the transcript, where Claude Code draws it under the composer.**~~
**DONE 2026-08-16**, reported from real use before anybody hit it here: at 100×30 the menu drew at
rows 0–1 with the composer at row 23, so reading it meant looking away from the word being completed.
It is `Room.menu` and `DM.menu` now, drawn last before the composer, joining the preview, the
heartbeat and the dispatch list below the transcript — which is the rule `DM.View`'s own comment
already stated for those three.

The entry was right that this costs both panes' height accounting, and right about *why* — but the
trap it named turned out to be the easy half. `startSelection` needs no care at all once the menu is
below the transcript: it stops being rows above one, so the offset simply does not move, and
`TestAMenuDoesNotMoveWhereASelectionStarts` pins that. **The expensive half was the bound**, and the
first attempt got it wrong in a way only a sweep could see. `App` clipped the menu against
`paneFloor`, which reads `minHeight` off an **unsized** pane — the status bar is empty and the
composer has no width until `View` sizes it, so the floor came back 4 where the sized pane's is 5,
and the pane drew **one row too tall across a six-height band** and correctly at both ends of it. A
frame one row too tall scrolls the alt screen away on every draw.

So the bound lives in the pane: `Room.menuRows`/`DM.menuRows` clip against `baseChrome` — the chrome
that is not the menu, split out so the menu can be measured without asking for itself — plus the
transcript's floor. **The menu never bounds itself**, which is the composer's rule for the composer's
reason. `TestAMenuNeverMakesAPaneTallerThanItWasGiven` sweeps the whole band against the same pane
with the draft taken back, rather than against the height asked for, because a pane already refuses
to shrink below its own floor and that behaviour predates the menu.

**Nothing narrows the offers except a prefix.** `/re` reaches `/resume` and `/rename`; `/rsm` reaches
nothing. Claude Code fuzzy-matches. A prefix is what makes the match explicable — the operator can
see why a row is there — and fuzzy matching over 133 commands on every keystroke is a cost nobody has
priced here. Worth doing only with a measurement in front of it.

**The `@` half offers no description and the `/` half offers none either.** The wire carries command
*names* and nothing else — `slash_commands` is an array of strings — so there is no description to
draw without asking claude for one, which is a turn per session. `internal/ui/picker.go`'s
`detailRow` is right there for the day something carries one.

~~**The directory read happens on the draw goroutine, bounded by entries and not by time.**~~
**DONE 2026-08-16**, and the entry above was wrong about what it was weighing. It read as a cost
question — network mounts are rare, local directories are microseconds — and the cost is not the
latency of the read, it is that Bubble Tea's one Update goroutine *renders and answers keys*: a
stalled `open` takes the frame, every key, and the keys that quit, with the only way out being to
kill the terminal. The "async menu one keystroke behind" it was traded against does not happen
either, because a read is a **listing** rather than an answer: it is held on the menu and narrowed
per keystroke, so typing a path costs one read per directory instead of one per character, and the
paths that are already on screen stay there while the next directory is read. `scanning` dispatches
one `tea.Cmd` at a time and `pathsScanned` folds it, matched on the directory it was of so a read
that outlived its menu is dropped. `TestADirectoryThatNeverAnswersDoesNotStopTheKeys` reproduces the
stall with a FIFO, which is the same syscall parking in the same place.

*One trade is left and is named rather than hidden:* one read at a time is a bound on goroutines,
so a directory that never answers leaves the path half silent **everywhere** — including in another
pane, over a directory that would answer — until it does. The alternative is one parked goroutine
and one held descriptor per character typed into a stalled mount, which is the worse of the two. A
per-directory bound would need a set of outstanding reads and has nobody asking for it.

~~**⌃P is unreachable inside a multi-line draft whose last token is an `@`.**~~ **DONE 2026-08-16.**
Also under-weighed here: `↑↓` are claimed unconditionally by `App.key` for the roster, so ⌃N/⌃P are
not *a* way to move between the lines of a draft, they are the **only** one — and a menu keyed on the
last token of the buffer took them from every cursor position in it, not just from the mention. The
menu is now scoped to the cursor (`Composer.AtEnd`, read by `completing`): it exists only while the
cursor is at the end of the word it describes, which is what "it costs one space" always claimed.
The claim is still on the trailing *token* rather than on any token the cursor happens to be in, and
that is deliberate — an accept replaces the whole draft, and completing mid-draft means preserving a
tail and placing a cursor inside it, which is a feature rather than a fix.

**A draft with a leading space gets no menu, and `submit` still routes it.** `slash` trims before it
routes, so ` /resume` is Wake's; `commandStem` does not, so it draws nothing. A missing convenience
rather than a lie, and the fix is one `TrimLeft` plus an accept that knows how much it trimmed.

**The directory in `/new <name> in <dir>` gets no menu, and the reader for one is already built.**
The gap is the *trigger*, not the filesystem half. `completing` dispatches on exactly two stems:
`commandStem`, which returns false the moment the draft contains a word break — so `/new john in
delta-a` stops being a command being typed at the first space — and `mentionStem`, which requires the
trailing token to start with `@`. A directory typed after `in` is neither, so it draws nothing, while
the same operator typing `@delta-a` one keystroke earlier gets a bounded, off-the-draw-goroutine
listing. Everything expensive already exists: `completionpath.go`'s `pathMenuFor`, `pathScanMax`, the
`scanning`/`pathsScanned` pair, and the listing-narrowed-per-keystroke design that makes a path cost
one read per directory. What is missing is a third stem — the remainder after `dirKeyword` in a
`/new` draft — and the one decision it forces.

*That decision is what the paths resolve against, and the two roots genuinely differ.* `pathMenuFor`
roots at `a.completionAgent().Cwd` and returns an empty menu when there is none, "because that is
where the agent resolves the reference". A `/new` has no target session at all: `absoluteDir` resolves
its directory against `os.Getwd()`, which is where `wake` was launched. A completion that quietly
borrowed the focused agent's directory would offer paths the spawn then resolves somewhere else —
and the focused agent's directory *moves*, since `EnterWorktree` is on every session's tool list.

**Ruled not worth building yet, 2026-08-16.** Owner's call: nobody has hit it in real use, and the
typed form is short enough to live with — `absoluteDir` joins a relative path against the launch
directory, so `/new john in delta-agent` is the whole command from a workspace holding several repos,
and the directory is joined across fields so a path with spaces needs no quoting. Revisit if it bites.

*Recorded because the first proposal was worse and should not come back.* A `Picker` on bare `/new
in` — the shape `parseNew` currently refuses as "a sentence somebody did not finish" — filled by a
scan of the repos under the launch directory. It is a second menu over the same filesystem, and
`Picker` is session-bound (`Targets`, `Names`, "resolved before the picker opened") where a spawn has
no session yet, so it would have been a parallel implementation of a reader that already exists.
If this comes back, it is a stem in `completing`, not a picker.

### Found while running this: `screen.settle()` can return before the keystroke has landed

**Not new, not caused by the completion menu, and it will keep costing somebody a red run.**
`TestPageKeysScrollTheConversation` failed once in five full `make ci` runs on this branch, on a
machine that happened to be running two of them at the same time. It sends `⇞`, calls `s.settle()`,
and then asserts on what is *absent*.

`settle` sleeps 80ms, samples the frame, and returns as soon as two consecutive samples match. On a
loaded machine the keystroke has not reached the program within 160ms, so **both matching samples are
pre-keystroke frames** and the assertion reads the screen as it was before the key. The failure
message prints `s.dump()`, evaluated microseconds later — by which time the frame has moved — so the
dump in the failure *contradicts the assertion*, which is what makes this expensive to diagnose: the
evidence attached to the failure shows the test passing.

Two `cmd/wake/completionscreen_unix_test.go` tests hit exactly this while being written, and the fix
there is the pattern: **give the keystroke a positive sync point before asserting on an absence.**
Those tests send `\tx` and `await` the whole resulting draft, so the space could only have come from
the accept and the frame is provably past the key. Every `send`-then-`settle`-then-assert-absence in
the screen suite has the same hole; `settle`'s own comment says it exists "for assertions about what
is *not* there", which is the case it is least safe for.

*What closes it:* either `settle` takes a frame counter rather than equality over two samples, or the
absence assertions each get a positive predecessor to await. The second is smaller and is what the
new tests already do.

### And one measurement worth recording: `internal/ui/app.go` is at 794 of 800

The completion field took it from 789. `TestNoNonTestFileCrossesTheHardMax` fails at 801 and
`TestCLAUDEmdNamesTheTwoLargestNonTestFiles` prints the count on every run, so this cannot arrive
silently — but the next person to add a field to `App` has six lines, and the file is a struct
definition plus `Update`'s dispatch, which is exactly the shape that splits cleanly. The seam is the
same one `keys.go` was taken at: `apply`, `observe`, `applyStatus` and `noteEnding` are one subject
(what a frame does to the model) and `Update`/`update`/`stream` are another (what a message does).

## 2026-08-16 — `--add-dir` and the debug flags: what shipped, and what was left

### Neither survives a park — the evidence arrived 2026-08-21, and the two halves answer differently

**The recording this entry asked for exists**: `testdata/stream/add-dir-runtime.jsonl` and
`debug-runtime.jsonl` (`test/runtime-probes`,
`docs/superpowers/notes/2026-08-21-runtime-command-findings.md`). What it showed:

- **`/add-dir` does not exist at runtime** — the CLI refuses it in the bare-effort shape
  (`num_turns: 0`, `$0`) and the directory stays unreachable. Nothing can restore an added
  directory after a park, which is the budget's own argument for surviving one: **`AddDir` belongs
  on `parkedRecord`**, or it is silently lost on every wake. Adding the field is a feature
  decision the owner holds; the evidence question is closed.
- **`/debug` works at runtime** — logging came on mid-session, provably on disk. So the debug
  flags may drop at a park; the operator can re-ask. One asymmetry: runtime `/debug` writes to
  claude's own debug directory, not the daemon-owned `debug/<name>.log` the flag feeds.

The original entry, kept as the argument:

### The original entry — neither survives a park, and the decision was not made

`parkedRecord` carries `Effort`, `Model`, `MaxBudgetUSD` and `FallbackModel` and gains nothing here,
so `⌃C` then `/resume` brings an agent back with no added directories and no debug log. That is the
shipped behaviour and it is **scope, not a ruling** — this change's subject is the spawn path.

The budget's argument for surviving a park is that *nothing* can restore a cap afterwards, so ⌃Q
would otherwise be the way to uncap a fleet. Whether that transfers is genuinely open, and the
evidence points both ways rather than one: the recorded corpus in `testdata/stream` advertises
`debug` on 101 `init` frames and does **not** carry `add-dir` — the same finding that already
demoted `/add-dir` from "a claude built-in" to "a word Wake does not own" one round earlier. So
neither "the operator can ask for it again" nor "only the record can carry it" is established for
either flag, and asserting one in a doc would be this repository's most-repeated defect.

*What closes it:* one recorded session that shows whether a running agent can be given a directory,
and whether logging can be turned on, without a respawn. Then `parkedRecord` gains the fields that
cannot be, or the doc says plainly that a park drops them and why.

### `internal/daemon/spawn.go` is at 790 of 800

`spawn` grew four lines for the `configFor` failure path, which is now fallible because a debug log
name becomes a path and that can fail. Nothing new was put in the file — both new subjects went to
`spawnconfig.go` and `debuglog.go` — but the next person has ten lines.
`TestCLAUDEmdNamesTheTwoLargestNonTestFiles` prints the count on every run, so this cannot arrive
silently. The seam if it does: `maySpawn`, `liveCount` and `capRefusal` are one subject (may this
spawn happen at all) and `launch`/`admit`/`withdraw`/`register` are another (taking the row).

---

## 2026-08-21 — next step, owner's call: give the manager a brain, not just tools

What is built, as of Phase 2 Task 15: a real `claude` session holding five bounded tools
(`internal/mcp/tools.go` — `list_agents`, `agent_status`, `roll_up`, `send_to_agent`,
`spawn_agent`, `interrupt`) under a system prompt (`managerScope`,
`internal/daemon/manager.go`) that says what each does and what it may not do. What that prompt
does not ask it to do is orchestrate. It says outright "you do not read the group chat," and it
frames every tool as something reached for when the operator asks — never as something the
manager's own model decides to reach for on its reading of the fleet's state. `@manager` answers
competently today; it does not yet notice that three agents are blocked on the same question and
batch them, or that an agent has gone quiet nine minutes into a five-agent refactor and that is
worth a status check, without a human asking first.

**"A brain" here is a policy, not a new tool.** The five tools already reach everything an
orchestrator would act through — read the fleet, message an agent, spawn one, interrupt one. The
open question is what decides *when* to use them; today that is the operator, every time, through
a message addressed to `@manager`. Turning it into a true orchestrator means deciding how much of
that judgment the manager's own model gets to exercise unprompted — and that runs straight into
what this file's "Power" section and `managerScope`'s own header already worry about:
`send_to_agent` starts a turn on an agent running in `auto`, so a manager that acts without being
asked is a manager whose consequences fan out without being asked either. Whatever gives it that
judgment owes the same discipline the rest of the manager was held to here — a bounded verb, a
guard that proves the bound, a recorded reason for each thing it may now decide on its own — not
just a longer system prompt.

Not scoped further than that. This is the next thing to design, not a design.

**2026-08-22 — a card costs a frame three times over, and it is not the card's rendering.**
Measured on this branch and on `origin/main`, `internal/ui/askdim_bench_test.go`'s
`BenchmarkFrameBehindAnAsk` at 200x40 with one pane: **~300µs with no ask and ~910µs with one**, on
both. The card renders **once** per frame (counted) and a settled ask-up frame **re-wraps nothing**
(`TestASteadyFrameWithAnAskReWrapsNothing`), so it is neither of the two things
`deferred.md`'s earlier entry on this suspected. What is left is the card *body* being composed per
frame — the preview panel through glamour, the option rows, the strip — for a block whose inputs
change only when a key is pressed. **The fix is the `chunked`/`transcript` treatment the two
conversations already have**, invalidated on the three things that move it (the card set, the
cursor, the width). Not taken here: this branch made the card richer and measured the result rather
than leaving the number unknown, and caching it is a change to how a card is held rather than to
what it draws. The richer card is ~130µs of the 910; the rest predates it.

**2026-08-22 — `--add-dir` and the debug flags still do not survive a park**, and neither does a
typed answer in flight. The second is new with answer mode and is the same shape: `Cards.writing`
is client state, so detaching mid-answer drops the draft. That is the right default — an answer
half-written is not an answer — but nothing tells the operator it happened.

---

## 2026-08-25 — feature idea, owner's ask: `@john` as a room *view* filter, not only a route

**The ask:** typing `@john` in the room should be able to narrow what the group chat *shows* — down
to the thread with john: the operator's own messages to him, the manager's messages to him, and
john's own replies — rather than only deciding where the next `↵` sends. Today a leading `@name`
governs **routing** and nothing about **display**. `roomRoute`/`slash.go` resolve it into a send
target, and mention mode (`internal/ui/mention.go`, "Mention mode, 2026-08-12" above) decides who
*receives* the turn — `direct` only john, `open` the whole fleet with john addressed. Neither touches
what the room draws. The room's own filter (spec §8: "your messages, the manager, agent→you
messages, blocking events, turn boundaries") is fleet-wide and has no per-agent narrowing; this is
a second, orthogonal filter on top of it.

**Not scoped — open questions before this is buildable:**
- **Is a mention a toggle or a query?** Does the room stay filtered for as long as the composer's
  target is `@john` (cleared the moment the target changes, no separate key), or does narrowing it
  need its own command/keystroke independent of what the next `↵` will do? The first reads naturally
  off the composer's existing target line; the second survives switching targets to glance at
  something else mid-thread.
- **What counts as "the manager sent to john"?** A broadcast the manager made while john was live
  (any `open`-mode turn), or only a turn the manager itself addressed `@john` under `direct`? The
  room already tracks *who sent* and *which surface* a line came from (`FromRoom`, `Echoed`, the
  broadcast-vs-DM rule under "The room comes back with what was said" in `CLAUDE.md`) but nothing
  today tracks *addressed to which agent* per line, which this needs.
- **Does it apply to history or only to the live room?** `roomHistoryLines`/`Room.Before` restore
  the room's transcript on open; a thread filter that only applies going forward would show an empty
  thread for a conversation that has been running for an hour.

**Where this would hook in:** `internal/ui/chat.go`'s `Room.Before` and `internal/ui/roomhistory.go`
own the room's existing filter predicate — this is a second predicate composed with it, not a
replacement. `internal/ui/send.go`'s `roomRoute` already resolves what `@john` means for sending and
is the natural source of "what is the current mention target" if the filter is meant to track the
composer live rather than be set as a separate query.

*Closed* by `feat/room-view-filter` (2026-08-27). The three open questions were ruled on (owner):
**trigger** is a live query off the composer target — a resolved lone direct `@name` narrows, and it
widens the moment the target changes or the draft clears (no key, no mode); **scope** is the focused
agent's own lines + the manager's + every broadcast + the operator's messages addressed to that agent
(open mode is a broadcast and does not narrow); **history** is included, and needs no reconstruction
because room history holds only broadcasts and public prose, which the predicate reads by kind and
session id. Design: `docs/superpowers/specs/2026-08-27-room-view-filter-design.md`; predicate in
`internal/ui/roomfocus.go`, wired through `retarget`. A hidden line stays in `said` at `rows == 0`
(the §5 render refinement over the spec's separate-backing sketch).

## 2026-08-26 — feature idea: a second board view, tiled rather than rows

**The ask:** `/board` should have two presentations of the same fleet data — the current one row per
agent, and a second "board" layout: one tile per agent, so the fleet reads as a grid rather than a
column. Switchable, not a replacement — rows stay the default.

**This sits directly against a ruling already on record.** `internal/ui/board.go`'s own header
comment: *"an OVERVIEW, not panes. No transcripts, ever; a tiled grid of thirty conversations is
unreadable by arithmetic, and chasing it is the multiplexer the non-negotiables refuse."* That
ruling is spec §8 plus the 2026-08-12 phase-4 scope call (§2c). A tile view is not necessarily the
thing being refused there — no transcript, no input, the same fields a row already shows (name,
state, task, last line) laid out as a grid instead of a column — but it is close enough in shape
that it should not be built without someone re-reading §2c and saying explicitly whether "tile"
counts as "pane" here.

**Not scoped — open question before this is buildable:** nobody has hit a case where the row view
actually falls down. Thirty rows is a tall scroll and a name-plus-glyph is thin to scan a column of
thirty for; a grid puts more of the fleet in one glance at the cost of the `lastLineCap`-length last
line not fitting a tile the way it fits a row. That is a plausible trade, not a measured one.

**Where this would hook in:** `internal/ui/board.go` draws through one code path today (`boardTitle`,
row layout, `boardKeyLine`); a second layout would be a second render function switched by a mode
field on the same model, the same relationship `App.key`'s legend already has to multiple modes
elsewhere in this tree, not a new modal.

*Blocks:* nothing today — the row-only board (`feat/board`) is what shipped. *Closes with:* an
owner ruling on whether a tile counts as "panes" under §2c, and if so, whether a board-scoped
exception is worth having ready before a fleet that has actually felt too tall as a list.

**BUILT, 2026-08-27.** The owner ruled: a tile counts as "panes" only in the sense of transcripts
you scroll and stdin you type, not in the sense of a cell's shape — so §2c narrows from "an
overview, not panes" to "an overview, not panes you *operate*", under four guardrails (view-only,
bounded live tail with no scrollback, fixed grid with no per-tile resize or pane tree, act-from-not-in)
that keep it a status wall rather than the multiplexer the non-negotiables refuse. `⇥` toggles rows
⇄ tiles while the board is up; the toggle is not persisted. See
`docs/superpowers/specs/2026-08-27-tiled-board-design.md` for the ruling and the design, and
`docs/superpowers/plans/2026-08-27-tiled-board.md` for the implementation plan. The narrowed ruling
itself is also recorded in `docs/notes/decisions.md` and in `internal/ui/board.go`'s header.

## 2026-08-27 — feature idea, owner's ask: several group chats inside one fleet, not one room per fleet

**The ask:** more than one group chat within a single Wake instance — separate room surfaces the
operator can switch between, rather than the single room a fleet has today.

**Today there is exactly one room per fleet, and that is a non-negotiable, not an omission.** The
room is `Cols[0]` and cannot be closed — "the group chat is the product; the panes are substrate"
(`internal/ui/grid.go`, and the non-negotiables table in `CLAUDE.md`). Several group chats exist
only as several **fleets**: each fleet is its own directory under `~/.wake/fleets/<name>` with its
own socket and its own single room, reached with `wake --fleet <name>` (`wake fleets` lists them).
That is multiple *instances*, not multiple rooms in one instance. **`internal/ui/groups.go` is not a
second chat** — its own header calls it "the v1 stand-in for spec §11's configured groups," and it
is only the left **sidebar** (one row per workspace/directory, derived and unnamed); it groups the
roster, it does not draw a room.

**The spec wanted this, and the vehicle for it was killed.** §11 (`Group = project, agents = repos`)
and §12 (the manager "holds a permanent seat in **every group**") both read for multiple groups per
instance, to be delivered by **profiles**. Profiles were **explicitly ruled out by the owner on
2026-08-20** ("the flag-bundle reading solves a problem this workflow does not have"; see the budget
entry above), so the spec's multi-group-per-instance vision is cancelled in its original form rather
than pending — which is exactly why this wants a fresh ruling before anyone builds it.

**Two things already deferred are adjacent, and neither is a second room:**
- **Teams** (2026-08-16, above): named tags + `@team` fan-out + sidebar grouping. Even its cheap
  first cut groups the roster and routes — **the room stays one**.
- **`@john` as a room view filter** (2026-08-25, above): narrows what the *one* room *shows* to a
  thread. A filtered view of the single room, not a second room surface.

If the ask is satisfied by "a filtered view per team over the one room," it is those two entries
composed (teams supplies the membership; the `@john` filter supplies the narrowing) and needs no new
surface. A genuine *second room* — a second `Cols[0]`-like uncloseable chat the grid switches
between — is **new scope that brushes the bounded-grid rule directly** (spec §8's "columns, each
optionally split once. Not a pane tree", and the §2c phase-4 scope call), the same wall the tiled
board runs into: it should not be built without an owner ruling on whether a second room counts as
the multiplexer the non-negotiables refuse.

**Where it would hook in, if ruled in:** the filtered-view reading lands in
`internal/ui/chat.go`'s `Room.Before` and `internal/ui/roomhistory.go` (a second predicate composed
with the room's existing filter, the same place the `@john` entry names). The genuine-second-room
reading is a `grid.go` change to allow more than one uncloseable column, which is the part §2c has to
approve first.

*Blocks:* nothing today — no code exists for this. *Closes with:* an owner ruling on which of the two
readings is meant (a filtered view over the one room, which reduces to teams + the `@john` filter; or
a second room surface, which needs a §2c scope decision), then a plan.
