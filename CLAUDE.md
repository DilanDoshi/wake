# CLAUDE.md — Wake

**Status: Phases 1–3 built and merged.** Core, daemon, room, DM, park/wake, fork, import, and the
manager all work end to end. Phase 4 is next — see `docs/goals.md`.

The design lives in `docs/superpowers/specs/2026-08-08-wake-design.md`. **The spec is the source of
truth for *what* Wake does; this file is the source of truth for *how we build it*.** When they
disagree, the spec wins and this file gets fixed.

> The long design rationale (every ruling, every recorded failure) lives in `docs/notes/decisions.md`
> and in the file headers themselves. This file is the operating manual, not the archive.

## Public repository hygiene

Wake is a **public** repository sitting directly beside Anthropic's own product. Two habits are
load-bearing, and both nearly went wrong before the first cut — treat them as non-negotiable:

- **Never reproduce Claude Code's binary in this tree.** Do not run `strings` on the Claude binary,
  do not paste verbatim minified source (`function M6i(e,t){…}`), do not cite byte offsets, do not
  write "read out of the binary". Anthropic's terms forbid reducing the product to human-readable
  form, and reproducing its compiled source is a copyright problem. Document **behaviour** — what a
  value is, how the CLI acts — never the extraction. A value that matches Claude Code is "matched
  against Claude Code, maintained by hand", not "extracted from the binary".
- **Never paste a raw Claude frame into a doc or comment.** The `init` frame is an environment dump —
  installed skills, plugins, socket paths, the home directory. Cite a fixture and a line instead.

**Recordings.** Capture into a sterile `HOME` (or one holding only credentials, with empty `skills/`
and `commands/`), then run `scripts/scrub-fixtures.py`. `internal/core/corpus_test.go` is the guard:
it fails CI on a home-shaped path, a machine environment key, or a `slash_commands` entry that is not
a Claude built-in, a public plugin, a Wake verb, or a registered placeholder — so an operator's own
command names cannot slip into the corpus. Fix a failure by running the scrubber, never by editing
the guard or the allowlist.

**Attribution.** Wake is not affiliated with Anthropic; `README.md` and `NOTICE` say so. No Anthropic
logo, no Clawd, no `claude-*` project name. Keep the disclaimer.

**Gate and flow.** `make ci` exit 0 is the only gate (no CI release/PR automation). A feature
branches and gets a PR; a docs-only change may go straight to `main`. Run the suite from a normal
checkout **under your home directory** — the screen tests (`cmd/wake/*_unix_test.go`) render the
working directory and assume a sane path, so they fail under `/tmp` or a 100-character temp path.

**Releasing is manual** — `goreleaser release --clean`, on your command. See
[docs/RELEASING.md](docs/RELEASING.md).

## Project overview

Wake is a terminal app for developers running 15–30 Claude Code sessions at once. It turns the fleet
into a room: a filtered group chat as the primary surface, `@name` routing, a manager session, and
an attention-ranked roster. Any agent opens as a full 1:1 DM at Claude Code fidelity.

An agent is a headless `claude` process in stream-json mode with a Wake-assigned session UUID.
**Wake never screen-scrapes.** All state comes from structured JSON on stdout.

### What it does today

| Surface | Behaviour |
|---|---|
| `wake` | **Starts a new fleet**, names it, and opens it **on the room**. Running it again gives you another one, which is claude's model - so the obvious command is no longer the way back, and `wake --fleet <name>` is. It still spawns an agent when it finds nothing at all, because a new user's first command has to produce one - but that agent is a **roster row and not a pane**. Opening it beside the room made the one surface `wake` is a request about the narrower half of a split, and below `dmTakeoverColumns` the only pane drawn at all, which is every ordinary terminal |
| `wake new [name]` · `wake fork <who>` · `wake import [<id>]` | Start, branch, or adopt a session — and **open it**, which is the whole of what `wake new` has that a bare `wake` on an empty machine does not |
| `wake attach <who>` | Back into one conversation, by name or session-id prefix |
| The manager | **Started by default**, by every verb that opens the room, so `@manager` and an unaddressed message always have somewhere to go. `wake manager` still starts one from a shell |
| `--effort <level>` · `--model <model>` | What a session thinks with, on the verbs that start one (`new`, `manager`) |
| `--max-budget-usd <amt>` · `--fallback-model <m,m>` | What a session may spend, and what it fails over to when its model is overloaded. Same two verbs. Both matter at fleet scale and nowhere else: thirty unbudgeted agents, and one overloaded model stopping all thirty at once. Both survive a park, because there is no runtime command for either |
| `--worktree <name>` | Wake creates a git worktree of that name under the repository root and runs the session in it. Same token on `/new`. **Wake never passes claude's own `--worktree`** — see `internal/daemon/worktree.go` |
| `--add-dir <dir>` | A directory outside its own that this session's tools may reach, **repeatable**, on the two verbs that start a session and on `/new`. Wake confined every agent to its spawn directory and had no way to widen it, which matters more now that an agent can move itself into a worktree |
| `--debug-file <name>` · `--debug <categories>` | Per-session debug logging, so one agent of thirty can be diagnosed. **The wire carries a name and the daemon owns the directory** — `filepath.Dir(socket)/debug/<name>.log`, beside `mcp.json` and `parked.json` — because a path on the wire is a file anything that can dial the socket could choose. `--debug` only narrows the categories and is refused without a file: on its own it writes no log anywhere that can be read |
| `wake status` · `wake stop` | What is running; end everything (irreversible) |
| `wake fleets` · `--fleet <name>` | The fleets, and which one a verb is addressed to. `--fleet default` is the reserved word for the **unnamed** fleet at `~/.wake` - every fleet that existed before fleets did is that one, so without it a whole existing Wake would be reachable only through `$WAKE_SOCKET`. Only a bare `wake` makes a new fleet; every other verb with no `--fleet` still means the unnamed one. **Several fleets can run in one directory** — each is a directory under `~/.wake/fleets/` holding its own socket, and every other per-fleet file is `filepath.Dir(socket)` plus a name, so isolation is the layout rather than a rule anything enforces. A bare `wake` is the unnamed fleet at `~/.wake/`, unchanged. `$WAKE_SOCKET` still wins, and naming a fleet beside it is refused rather than one being ignored |
| Room keys | `↵` send, open the picked agent, or confirm an armed detach · `esc` interrupt (clears the draft in the room; leaves answer mode while a card is taking one) · `esc esc` clear a conversation's draft, or — idle and empty — open a rewind picker to an earlier prompt · `↑↓` walk this pane's prompt history when the draft has no row to move into, else move the query cursor · `⇧↑↓` pick agent · `⌃O` arm detach — `↵` leaves, a second `⌃O` cancels · `⌃C` park focused · `⌃Q` park all & quit · **`⌃C⌃C` or `⌃Q⌃Q` emergency quit** — read off the tty *before* Bubble Tea, so it is the one exit that still works when the window has stopped drawing · `⇥` focus · `⇧⇥` permission mode · `⌃X` next blocked · `⇧←→` move the keys to the pane that way · `⌥↵`/`⌃J` newline · `⌃F` fork · `⌃D` open here · `⌃Y` open in a new column · `⌃B` open below · `⌃W` close pane · `⌃E` expand tool results, or the room's folded responses (a click opens one) |
| Slash commands | `/resume`, `/new` (optionally `--worktree <name>`, `--add-dir <dir>`, `--debug-file <name>`, `--debug <categories>`, `--max-budget-usd <usd>`, `--fallback-model <m,m>`), `/name`, `/task`, `/color`, `/quit`, `/adopt`, `/mcp`, `/login`, `/manager`, `/manager-stop`, `/board` (`⇥` toggles a tiled live wall of live transcripts, view-only, while the board is up) — everything else is passed to the agent byte for byte |
| `/color` | Sets an agent's identity hue — one of seven named colours — so its turns in the room, the composer it types into, and its roster row are told apart by more than name text. **The status bar deliberately does not take the hue** — it recedes as chrome. `/color <colour>` or `/color @who <colour>` — and **`@who /color <colour>` from the room** works too, since the mention is the target (the same bridge `/name` and `/task` take). `/color none` clears. In the roster the hue **survives the cursor**: an open agent's row is the selected one, so the selection shows as bold rather than the accent hiding the colour. The **manager** defaults to yellow — the one session with a hue the fleet does not share (`identityStyleFor`) — so on it `/color none` returns to yellow rather than to no hue, since its empty colour is its default. A session attribute that survives a park. **The word is Claude's own** (its theme command, advertised on 71 corpus inits); Wake claims it on the owner's 2026-08-27 override rather than the corpus rule — `slashguard_test.go`'s `ownerClaimedCommands`, retired if a recording ever shows Claude's headless `/color` is a redirect |
| `/manager` | The switch: starts one when there is none, wakes a parked one, parks a running one. A command rather than a key — see `internal/ui/slash.go` for why every remaining chord is worse than a legend slot |
| `/manager-stop` | The ending, where `/manager` only parks: `rpc.FrameStop`, so the name goes back to the pool and the next `/manager` starts a fresh one. Refuses a **parked** manager (a stop reaches only a session with a process) and refuses when there is none |
| `/quit` | `/manager-stop` for an ordinary agent: `rpc.FrameStop` ends one session (irreversible, releases the name), and this window **drops its row** once the report confirms the ending — off the roster, the group chat, the ⇥ ring and the sidebar. Bare `/quit` ends the conversation you are in; `@who /quit` from the room ends that agent (the same mention→target bridge `/color` takes). The stop lets the in-flight turn finish, so a busy agent stays until it ends. Reaches a **blocked** agent (a stop has no wake — `/manager-stop`'s inversion of ⌃C); refuses a **parked** one with advice; **refuses the manager** and points at `/manager-stop`, which is its one ending. The daemon still keeps the ended row in its recent ring, so **another window shows the `·`** — the hide is per-window, since only the operator who typed it knows it ended |
| Inline completion | A draft whose word **at the cursor** starts a `/command` or an `@` offers what could finish it: the target session's own commands **and skills** (both ride in `init.slash_commands`), then Wake's own commands, then live agent names and paths under that session's directory. **The session's come first** (owner's 2026-08-28 override): Wake's twelve verbs filling the bound first was a bare `/` that never showed the operator's own Claude Code skills — they sat in the "N more" overflow. Wake's follow, still reached by their first letters and shown whole whenever the session advertises fewer than the bound. **Behind a resolved lone `@name`, only that agent's own** — Wake's fleet verbs are not the addressed agent's. `⇥` completes · `⌃N`/`⌃P` walk · `↵` still sends. Move the cursor off that word and all three go back to the text area |
| A lone `@name` in the room | **narrows the group chat to that agent's thread** — their lines, the manager's, every broadcast, and your own messages to them — for as long as it is the composer's target, widening again when the target changes or the draft clears. A *view* filter over the room's own, not a route or a mode: it adds no key, `@john hi` still routes and `@john /effort` still configures, open mode does not narrow (it widens the message, not the view), and the pane header reads `group chat › @john` |
| Bare `/effort` · `/model` | Wake draws the menu claude cannot draw headless; with an argument they pass through untouched. The menu **names the value the session is already at** (`Picker.Current`): `/effort` opens the cursor on the current level and marks it (the options are the level words); `/model` shows the current model as a `current:` line rather than a mark, since a display name does not reverse-map to one of the aliases offered |
| A dispatch ending | Leaves one line in the conversation — `● Subagent "Counting lines" finished · 24s` — coloured by outcome. The `⏺ Agent(…)` tool call above it is the start marker, so there is no started line |
| A subagent | Its work is a conversation of its own, not a paragraph in yours. **The right sidebar lists the ones still running**, indented under the agent that dispatched them, named by `subagent_type` with what each has spent; `↑↓` walk onto one and `⌃D` (or a click) opens its transcript in the pane. Running only — a finished one drops off the list and leaves the `● Subagent "…" finished` line in the transcript |
| An agent's question | Answered in the pane that put it, in a framed prompt with the transcript behind it drawn quiet. A tab strip across the top names every question and the submit step, checking the ones that have an answer; then the question, its options, an `Other…` row, and the cursored option's consequence. `↑↓` walk the options — **draft or not** · `←→` walk the questions · `↵` chooses and advances · `1-9` pick · `d` refuses. The last question advances to a **review**: every answer laid out, `Submit answers` / `Cancel`. Picking `Other…` or pressing `d` puts the composer into **answer mode**, where `↵` sends what you typed and `⎋` abandons it |
| An answer being written | A conversation shows the block as it is generated, under the transcript and above the working line. A preview, never a record — the completed block replaces it |
| The task board | The `TaskCreate`/`TaskUpdate` checklist an agent keeps for itself — its steps, with the one in flight marked — is **pinned above the composer** where Claude Code draws it, folded live from the ops. The ops draw nothing in the transcript: the board is the one place the list shows, and it shrinks when items are deleted. A subagent's own list draws inline in its dispatch transcript, since a subagent has no board of its own |
| `!cmd` | A bounded shell line whose output lands in the conversation |
| The mouse | Wheel scrolls the pane under the pointer · click focuses · drag a divider to resize · **drag across text to select it, and the release copies it** — in the transcript, the query box, **and every other rendered surface** (the roster, the sidebars, the status bar, a card), where a drag highlights the cells it crosses and the release copies them · **click a folded tool result to open that one**, which is the gesture Claude Code spends the same way · **click a run's rollup line to open that whole run** |
| A folded tool run | A message's tool calls draw as one dimmed line — `28 tool uses · 24 bash · 1 read · 3 linear-server` — the way Claude Code shows a turn's activity rather than every ⏺ and ⎿. `⌃E` opens every run in the conversation; a **click** on one rollup opens that run alone, and `⌃E` folds everything back. A `TaskCreate`/`TaskUpdate` draws nothing in the transcript at all — its checklist is live status, not activity, so it is the **task board pinned above the composer** rather than a block or a count. An **`Edit` draws its diff whole**, out of the run the way a checklist is (`foldExempt` on `Diff`) — the diff is the point of the edit, so it shows by default rather than only under `⌃E`/a click (owner's 2026-08-28 request) — and its successful `has been updated` confirmation is suppressed, since the diff and the green ⏺ already say so; a **failed** edit still shows its result, which is the error |

## Non-negotiables

Violating one is a design regression, not a style nit.

| Rule | Why |
|---|---|
| **Not a terminal emulator or multiplexer.** No PTY, no VT100, no browser panes, no arbitrary shells. | That's the host terminal's job. Chasing it is how this project dies at 40%. |
| **Cheap to leave open.** No work per frame that could be work per change, no poll where a wait will do, no process on a timer. | A per-agent cost on a ticker multiplies by 30 next to 30 `claude` processes. |
| **Only `internal/core`'s four airlock files know Claude's JSON** — `protocol.go`, `wire.go`, `vocabulary.go`, `encode.go`. Everything above sees Wake's own `Event`. | Four reviewable files are the whole cost of staying Codex-ready. Enforced by `airlock_test.go`, which also holds the file set so a fifth cannot be added quietly. |
| **Claude's CLI identity flags are spelled only in `internal/core/argv.go`** — `--session-id`, `--resume`, `--fork-session`, `--continue`. Ask `core.SessionArgvMarkers` instead. | Enforced by `argv_test.go` tree-wide. A `--resume` grown beside a `--session-id` is refused at startup with nothing on stdout. |
| **`attention.go` stays a pure function.** Events in, ranked state out. No processes, no I/O. | Hardest logic in the app; must be testable without spawning anything. |
| **The UI never touches an agent's process.** It receives messages and renders. | Keeps the daemon boundary real rather than aspirational. |
| **Wake owns almost no state.** Claude persists transcripts to `~/.claude/projects/<cwd>/<uuid>.jsonl`, and Wake *reads* one back when a conversation opens (`internal/daemon/history.go`) rather than keeping its own. Wake stores only roster, park book, groups, layout. | Wake can crash and lose nothing. The park book holds the minimum that can do the job: id, directory, name, label, parked-at. Never a PID, never a ParentID. It is read on demand rather than back into live state — a daemon restores nothing, so ⌃Q then `wake` is an empty room. |
| **Never copy cmux source.** Reach it only through its CLI. | cmux is GPL-3.0-or-later. Copying one file makes Wake GPL forever. |
| **No parallel implementations.** Extend the existing code in place, or delete and replace it. Never a second version beside the first. | Find the existing code first (grep/read it), then name what you extend or remove. |

## Load-bearing design rules

Short version of decisions that are expensive to rediscover. Each one has its full argument in the
named file's header or in `docs/notes/decisions.md`.

**Naming and addressing.** A session's name comes from the daemon (a 64-name pool), never from the
client — only the daemon can see the whole fleet. **A name is never an address:** `rpc.Frame` carries
`SessionID`, and the reaper proves a process group by finding that UUID in an argv. Names are
released when a session ends and reissued, which is why a rename has no alias and `@old` stops
resolving.

**The legend is drawn only while an arm is live, and then it is only the armed cue.** The always-on
row of key hints under the composer is gone — it was redundant with the status bar, which already
names the permission mode and the rest of what a pane is (owner's request, "keep armed cues, drop
static hints"). What survives is the safety confirmation: while a detach is armed the composer draws
`↵ detach   ⌃O cancel`, while a clear-draft arm is live it draws `esc clear draft`, and an idle,
empty conversation's second `⎋` draws `esc rewind`. That row is the only on-screen tell that the next
keypress is irreversible, which is why it stays where the static hints went. An unarmed composer
draws no legend row at all, and the permission mode is the status bar's alone now. `ui.legendEntries`
still holds the (glyph, label) pairs and is still the canonical list of what this build binds:
`TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed` requires a bijection with the `tea.Key…`
cases in `App.key`, so **a key added to `App.key` without a `legendEntries` entry is still a build
failure** even though the entry is no longer drawn. The cue's labels are derived from `legendEntries`
by `TestCLAUDEmdDescribesTheLegendItDraws`, which holds this paragraph to what the composer actually
draws; re-derive rather than hand-edit. The height that row costs is counted only when it is drawn —
`Composer.showsCue` is the one predicate `overhead` counts by and `View` draws by, the `hasBeat`
pattern one surface over, so a pane is never sized without the row and then drawn with it.

**The permission mode moves on the receipt, never on the keystroke.** `⇧⇥` cycles
`default` → `acceptEdits` → `plan` → `auto` for the agent `pickedAgent` names, writing an
`rpc.FrameMode`; the label changes when the daemon's answer arrives. This is not belt-and-braces —
`manual` is accepted by the CLI and silently normalizes to `default`, so a label built on the string
that was *sent* is wrong on a real cycle position. Every turn's `init` is a second observable and
corrects a stale belief, which is the only thing that can see a mode changed through
`updatedPermissions`, a path that emits **no receipt**.
**A mode does not survive a park**: `--resume` carries none, so a woken session comes back in its
spawn mode and `modeReverted` says so — `parked.json` gains no field. The words are core's
(`PermissionModePlan`/`Auto`/`Default`/`AcceptEdits`/`DontAsk`); `internal/ui` never spells one.

**That order is Claude Code's own rather than chosen.** `chat:cycleMode`'s
switch is asserted against as
`internal/ui/testdata/claude-mode-cycle.json`, so ⇧⇥ — the one key this build and Claude Code
agree on — walks the same positions in the same order. It costs no branch: with `default` at
position 0, `nextMode`'s off-the-cycle fallback *is* the switch's own `dontAsk` and `default:` arms,
and `auto` sits last so wrapping gives the answer its unlisted `auto` case does. **`dontAsk` is an
exit and not a position**, which is Claude Code's answer as well as the one Wake reached alone;
`bypassPermissions` is unreachable while nothing here passes `--dangerously-skip-permissions`. What
this cost is the old traversal's monotonicity — the second press now loosens — and what it kept is
that the first press from the spawn mode still tightens. Full argument: `internal/ui/mode.go`,
`docs/superpowers/notes/2026-08-12-permission-mode-findings.md` and `2026-08-16-mode-cycle-findings.md`.

**The grid is bounded, and the bound is the design.** `ui.Grid` is columns left to right, each
holding one conversation or two stacked — spec §8's "columns, each optionally split once vertically.
Not a pane tree." `⌃Y` opens a conversation in a new column, `⌃B` stacks it under the focused pane,
`⌃D` still opens *into* the focused pane, and `⌃W` closes it. A second `⌃B` takes the lower slot
rather than growing a third row, and `⌃B` from a lower pane is refused by name. **The room is
`Cols[0]` and cannot be closed** — that is what "the group chat is the product; the panes are
substrate" means structurally. Going past this into arbitrary tiling is §17's "Out" list and the
multiplexer the non-negotiables rule against.

**The mouse reaches every pane, and both axes resize.** A click focuses whichever pane it lands in,
including the halves of a stacked column — only the row tells those apart. Each vertical divider is
its own drag (`Layout.Weights`, one per column) and the rule inside a stacked column is a drag too
(`Layout.Rows`); both store a *fraction*, so a terminal resize keeps the proportion instead of
pinning one pane. **Column widths are allocated on a running total rather than per column**, which is
what makes a drag local: an edge nobody moved lands on the same cell whatever happened beyond it, and
rounding each column separately used to shift the room by a cell when the divider two columns over
moved. A width drag goes through the 80ms settle; a *row* drag does not, because only a width change
re-wraps. The wheel scrolls the transcript **under the pointer**, not the focused one — with four
panes on screen those differ, and scrolling never moves the keys.

**A drag across text selects it and the release copies it, because Wake owns the mouse and cannot
hand selection back.** Mode 1002 takes the host terminal's own drag-select away, and giving it back
would cost the divider drag and click-to-focus — and a native selection is a rectangle over the whole
terminal, so a paragraph in column 2 of a four-column grid comes with columns 1 and 3 attached on
every row. Claude Code 2.1.232 reached the same answer: it enables mouse tracking, carries its own
selection engine with a column `scope`, and ships `copyOnSelect` defaulting to on — read out of the
binary, recorded in `docs/superpowers/specs/2026-08-14-select-copy-design.md` §2. **A selection is
anchored to absolute `transcript.lines` indices**, which never renumber, so it needs no maintenance
as events arrive and the highlight rides up the screen while the pane keeps following its agent.
Three rulings, each with a test named for it: **every keystroke clears the highlight and then does
its own job** — `esc` clears *and* interrupts, because a stale highlight swallowing the press that
stops a runaway agent is an agent that does not stop; **a width change clears it and a height change
does not**, since only a re-wrap renumbers the lines it is anchored to; and **a click copies
nothing** — `head != anchor` is the whole discriminator, not a timer or a distance. The write is
layered `pbcopy` → `tmux load-buffer` → OSC 52 (DCS-wrapped under tmux, one continued string under
screen), and it reaches the terminal through the **one writer Bubble Tea draws through** — which
must embed `*os.File`, or termenv stops recognising a terminal and the whole app silently loses its
colour. `cmd/wake/selectscreen_unix_test.go` is the only thing that can see that happen.

**The query box is selectable too, and it is the transcript's own rule one surface over.** A drag on
a draft row highlights the typed characters and the release copies them; the border, the `> ` prompt
and lipgloss's trailing pad are all stripped off, so only what you typed reaches the clipboard. The
geometry is fixed by `theme.BoxStyle` and the prompt rather than measured — text starts
`composerTextLeft` columns in and stops `composerRightInset` short of the far edge — and it is
**gated to the text**: an empty box and the blank past a short line take nothing, which is what
preserves the old fence that a query-bar drag must never clamp into a transcript line. Unlike the
transcript it does not scroll under a drag — the box is a handful of rows, all on screen.
`internal/ui/composersel.go` is the whole of it; the highlight is drawn by `DM`/`Room.View` through
`highlightComposerBlock`, and `cmd/wake/selectscreen_unix_test.go` proves a real drag lands a
background on the typed cells.

**And every other rendered surface is selectable too, as a frame-wide screen selection.** The
transcript and the query box each anchor to what they draw — `transcript.lines` indices, draft-row
indices — so a highlight follows its text as the pane scrolls and events arrive; everything else Wake
draws neither scrolls nor renumbers, so a drag over it (the roster, the sidebars, the status bar, the
awareness strip, a card, a menu, a preview, the box's own borders) anchors to an absolute
`(row, column)` on screen and the highlight is drawn once over the assembled frame. It reuses
`selection`/`marked`/`selectedText` unchanged — only the anchor's meaning differs, which
`selection.onScreen` records. A press routes here when it is neither a transcript row nor a query-box
draft row nor a divider — the three surfaces with a gesture of their own — so a **divider still drags
to resize** and a **roster click still opens** its conversation, resolved on release now so a drag
copies a name and a click opens. The query box's blank interior still takes nothing, because blank
space is not text — the one fence that survives. It reads the frame **live** rather than snapshotting
it — chrome redraws on every fleet report, so like a terminal's own selection it follows the cells and
copies whatever stands under them at release. The one thing resolved at *press* is a **roster click's
target** (`rosterHit`): the roster reorders by attention, so a click must open the row that was
pressed, not whoever slid onto it before the button came up. `internal/ui/screensel.go` is the whole
of it; `View` lays the overlay over `assembleFrame`, and `endSelection` copies off that same frame, so
the copy is exactly the cells the overlay highlighted at release.

**The grid keys are letters because two prior answers were unpressable, in two different ways.**
`⇧↵` and `⌃⇧↵` are what was asked for and bubbletea v1.3.10 names neither — probed in both the Kitty
CSI-u and xterm `modifyOtherKeys` encodings, and neither produces a `KeyMsg` — while a terminal with
no keyboard protocol sends `⇧↵` as the byte it sends for `↵`, which is *send*. Same wall as `⌃⇧A`.
`⌃⇧→`/`⌃⇧↓` replaced them and failed the other way: **named by the library, sent by every terminal,
and delivered by no macOS**, because the window server spends all four ctrl+shift+arrows on spaces
and Mission Control before a terminal sees one. Every guard in this tree passed while the keys did
nothing, which is why `⌃Y` and `⌃B` are single bytes — 0x19 and 0x02, which nothing between the
keyboard and Wake claims. `keyprobe_test.go` holds the chord findings and
`TestNoKeyIsACtrlArrow` holds the macOS one; the measurement is in `docs/notes/decisions.md`.

**Moving the keys between panes is `⇧←→`, because it is the only arrow family free at every
layer** — and `⇧↑↓` move the roster instead, the job plain `↑↓` gave up to prompt history.
`⌘`+arrow is what was asked for and is dead twice: bubbletea's arrow table knows modifier
params 2–8, cmd is bit 8, so `⌘→` is param 9 and the library names *nothing* for it — and no macOS
terminal transmits `⌘` to a tty anyway. `⌃`+arrow is named and delivered and still wrong, for the
`⌃⇧`+arrow reason one paragraph up: macOS spends all four on spaces and Mission Control, so
`TestNoKeyIsACtrlArrow` refuses the whole `KeyCtrl…`+arrow class rather than the `⌃⇧` half it used
to. `⌥←/→` are the text area's word-movement. That leaves `⇧`+arrow, which `App.key` did not take
and `bubbles` does not bind. **`⇧←→` move among panes that are already *drawn* and open nothing** —
that is the whole difference from `⇥`, which walks the chat ring and will open a conversation that
is off screen, and it is why a direction with no pane in it names `⇥` instead of wrapping. A wrap in
a two-pane grid makes `⇧←` and `⇧→` the same key. `ui.Grid.Toward` is the pure half and returns
`(id, ok)` because `""` is both the room and "nothing that way". **Vertical pane movement has no key**
— `⇧↑↓` are the roster's now, so the lower slot of a split column is reached by `⇥` or a click.

**Plain `↑↓` walk the prompt history on an empty or single-line draft and move the query cursor on a
multi-line one — Claude Code's own `↑↓`, which recall the previous prompt.** `←→` have always reached
the text area; `↑↓` were the roster's unconditionally, so a hand arriving with Claude Code's
history reflex got the roster, and a multi-line draft had no way to move the cursor between its own
lines. Now `App.key` asks the focused composer first: `Composer.CanCursorUp`/`CanCursorDown` run
bubbles' own `CursorUp`/`CursorDown` on a *copy* of the text area and report whether the cursor
actually moved — the same move `App.key` delegates to, so the two can never disagree. It is a
simulated move rather than a row count on purpose: bubbles' `wrap` adds a synthetic trailing row at
exact wrap width (its `>=`) that `LineInfo.Height` counts but the cursor cannot occupy, so a
count-based predicate reported a move `CursorDown` never makes and **swallowed `↓`** — moving neither
the cursor nor the history (found by the Codex adversarial pass; pinned by
`TestCanCursorMatchesRealMovementAcrossWidths`, which sweeps widths and lengths across the exact-width
boundary). When the cursor has somewhere to go the arrow falls through to the composer (Update
rebuilds the completion menu after, `App.recompleted`); only an empty, single-line, or top/bottom-edge
cursor reaches `walkPrompts`, which is why the common case is a history recall on an empty box.
**`⌥↑↓` carry no binding of their own**: the switch is on `m.Type` alone, so a `⌥` arrow behaves
exactly as the bare one does — it is the same `walkPrompts`/cursor path, not a second key.
**The roster moved to `⇧↑↓`**, because there is no other free arrow family for it: on macOS
`⌃`+arrow and `⌃⇧`+arrow never arrive (the rule above), `⌥↑↓` are the bare arrows and `⌥←→` is
word-movement, and `⇧←→` is *pane* movement. The legend is `{"↑↓","prompt history"}` and
`{"⇧↑↓","pick agent"}`; cursor movement is a composer fall-through the legend never advertises, so the
bijection guard names `KeyUp`/`KeyDown` and the four shift-arrows exactly once each.
`internal/ui/keys.go`, `internal/ui/composer.go`.

**Wake shares a keyboard with Claude Code, and nothing of Wake's moves for it.** An operator arrives
with Claude Code's reflexes, and several chords mean something else here.
Claude's bindings are kept by hand in `internal/ui/testdata/claude-keymap.json`, and
`internal/ui/keymap_test.go` holds every collision to a written ruling — **one nobody has ruled on is
a build failure**, and the Wake side is derived from `legendEntries` so a key added later is caught by
construction. No count is written down, because a number nothing asserts drifts; the two maps in that
file are the record. Only one collision is destructive: **⌃O expands a tool result there and detaches
here**, so it is armed — the paragraph below is the whole mechanism. The rest are
one-press confusions with a visible, reversible result (⌃T, ⌃R, ⌃B, ⌃E), and **⇧⇥ and ⌃E are not
tolerated but asserted**: both sides cycle a permission mode and both reveal what a pane folded away,
so they sit in `agrees`, where a rebinding on either side fails as an alignment that broke.

**The detach is armed by ⌃O, confirmed by ↵, and cancelled by a second ⌃O — and it is *drawn* for as
long as it is live.** Two properties, and each closes a failure the first version shipped with.
**The confirm is a different key** because a same-key confirm fires on exactly the reflex the arm
exists to catch: there is no key release, no timing and no distinct signal in a `KeyMsg`, so terminal
auto-repeat — and the human reply to a key that appeared to do nothing, which is to press it again —
are the same bytes as intent. Measured rather than assumed: two ⌃O sharing one read arrive as **two
plain messages** (`keyprobe_test.go`), where two ⎋ collapse into one `alt+esc` (`escprobe_test.go`).
**And the arm is on screen** because `App.disarmed` is reached from key and mouse paths only — a
stream frame, a heartbeat, a resize, a settle and a reattach all leave it standing — while its only
other tell was a `notice.Report`, and `internal/notice` is one most-recent-message slot that routine
fleet activity takes within seconds. Broadening the disarm to those messages was considered and is
**worse**: at fleet size a frame lands between the two presses constantly, so ↵ would mean *send* on
the press aimed at *detach*, decided by socket timing. So while a detach is armed every pane draws
the cue `↵ detach   ⌃O cancel` — the composer's only legend row now, since the static hints are gone
— the way the armed pane draws `⎋ clear draft`, and `↵` leads it so it survives a narrow cue's own
truncation. It adds **no legend glyph**, for `escape.go`'s reason. A drawn
*question* card still wins ↵ and takes the arm back, which is the cheap way round: `chooseCursored`
writes no frame. Full argument: `internal/ui/detach.go`. **Prompt history is
`↑↓`** on an empty or single-line draft, Claude Code's own recall key: the history is *derived* from
the pane's own events, so it works on a reattach and on a conversation this client has never opened,
and the room's is what was typed into the room. The roster moved to `⇧↑↓` for it.

**A pane that holds the keys is always a pane that is drawn.** Below `dmTakeoverColumns` only one
column fits, and `Layout.window` slides the drawn range to keep the focused one on screen rather than
swapping the room out for the conversation. Going off screen that way still counts as *leaving* — the
last-read boundary is anchored to it — so every focus change routes through `App.refocus`, which
marks whatever stopped being drawn.

**`⎋⎋` clears a conversation's draft, and the second press is an arm rather than a timer.** The room
has cleared its draft on one press since `3f8c662`; a conversation pane's `⎋` stops the turn and the
draft deliberately survives, which left no way to clear one short of holding `⌫`. So `⎋` in a
conversation interrupts *and arms*, and a second one clears — with `App.disarmed` taking the arm back
on every other input, which is the card keys' own rule and reaches the same four paths. It arms only
when there is something to clear, so mashing `⎋` at a runaway agent still stops it every time.
**A fast `⎋⎋` is one message, not two**: two escapes sharing a read reach bubbletea as `alt+esc`, so
`App.key` passes `m.Alt` as a collapsed press and does both halves on it — measured by
`escprobe_test.go`, and a build without that branch works for slow presses and silently fails under a
finger. It adds **no legend glyph**, because it adds no `tea.Key…` case and the bijection guard would
refuse one; the armed pane swaps `⎋`'s label to `clear draft` instead. Full argument:
`internal/ui/escape.go`.

**`esc esc`'s idle case sends `rewind_conversation`, a `control_request` on the same stdin channel as
`interrupt` and `set_permission_mode` — Claude Code's own mechanism, read off the wire rather than
invented.** The request carries `target_message_uuid` and `last_seen_user_message_uuid`, both
mandatory — omitting the second is exactly what a `"stale target"` refusal means — and the receipt is
a `control_response` whose nested payload is `{rewound, targetMessageUuid, prefillText,
precedingAssistantUuid, error}`, the same "success is not a verdict" shape the permission and interrupt
receipts already have. **`session_id` never changes** — no `init`, no `conversation_reset` — so Wake
never re-keys the session over a rewind.

**On disk the transcript is an append-only tree, and a rewind does not delete.** Claude's lines carry
`uuid`/`parentUuid`; rewinding appends a `last-prompt{rewound,leafUuid}` marker and repoints the active
leaf, but the rewound turns stay in the file as a dead branch. So **the transcript reader had to become
tree-aware**: `core.ActiveBranch` (`internal/core/activebranch.go`) walks `parentUuid` from the live
leaf to the root, resolving every fork as "newest branch wins" — the child written after the latest
rewind marker. `internal/daemon/history.go` is the one caller for both a DM and the room
(`sendHistory`/`sendRoomHistory` share `answerHistory`), and `internal/daemon/rewindtargets.go`'s
`RewindTargets` reuses the same reconstruction to answer the picker's own options and its `last_seen`
tip — one reconstruction, so a reopened DM, a reattached pane and a restored room can never disagree
about which turns are gone.

**The trigger is gated to idle + empty, scoped to the focused pane, and adds no legend glyph.**
`App.rewindArmable` (`internal/ui/rewind.go`) is read fresh on every `esc` rather than cached, the same
way a card is read through `a.cardOf(a.focus)` and never `Cards.Top`: a running agent always eats `esc`
as interrupt, so mashing it at a runaway agent still stops it, and a picker left open on a conversation
the operator tabbed away from claims no keys at all. `internal/ui/escape.go`'s `escape` is what re-runs
the gate on both the slow and the collapsed press rather than trusting a stale arm. It is still
`tea.KeyEsc` — no new case, no legend entry — `⎋⎋` clear-draft's own reason. On `rewound:true`,
`noteRewind` (`internal/ui/rewind.go`) makes the pane **re-read itself tree-aware** — the same
`askHistory` a reopen already takes — and drops `prefillText` into the composer; this is deliberately
the *only* mechanism, so a live prune and a reopen can never disagree. The manager is refused both
`FrameRewind` and `FrameRewindTargets`, on `FrameMode`'s own grounds plus one of its own: nothing on
that surface can address a message uuid, and a rewound turn does not stay in view on the manager's own
read either (`cmd/wake/mcpguard_test.go`).

**The room comes back with what was said, re-derived from claude's transcripts rather than kept.**
`⌃Q` then `wake` then `/resume all` used to be a working fleet above an empty group chat. The room
asks at exactly two moments — `NewRoomApp`'s seed and `wakeArrived` — which between them are every way
a session arrives into a room missing its history; **a spawn has no transcript and a fork's is its
parent's**, already drawn under the parent. It is a **second frame kind and a second ledger**
(`rpc.FrameRoomHistory`), because `askHistory` is once per session per client and a shared one would
spend the ask a conversation opened later needs. Three rulings hold the fold together.
`core.Event.At` is stamped by `DecodeTranscriptLine` and nothing else — **the zero value is
load-bearing**, since a live event never gets a time and that is what lets `Room.Before` merge history
without re-ordering a line somebody is reading. **A batch is dropped whole if its session has said
anything since the ask**, per session rather than for the room: the cutoff alone cannot do it, because
it is stamped before the frame is written and a reattach replays frames it read *before* the model
existed — so a pre-cutoff event still reaches the room afterwards and both copies get drawn. And **a
turn you typed comes back only when two transcripts prove it was a broadcast**: on disk a room
broadcast and a private DM turn are the same bytes, so multiplicity is the only sound discriminator
and the rule errs toward silence. That rule runs in `Room.Before` over `Room.raw` — **every**
transcript restored so far — because the daemon answers **one transcript per frame**, so a collapse
applied where a reply lands can never see a second session.

**The turn is the unit, and an agent's prose is restored only inside a public one.** Deciding it line
by line hid the question and showed the answer: the private turn was dropped and the agent's reply to
it — the same conversation, in the agent's words — went into the group chat anyway. Live,
`App.observe` keeps a whole DM-sent turn out of the room through `Fleet.inDM`, and nothing on disk
records which surface a turn was typed on, so the restore carries provenance itself: prose is kept
while its session's last user turn was a proven broadcast, and the next user turn closes it. **Prose
with no initiator in the window is dropped**, which is most of a 400-event tail — and that is why
there are two bounds. `roomRawEvents` is a memory backstop on `Room.raw`; `roomHistoryEvents` is
applied *after* the rule, because trimming `raw` takes the oldest line of a turn and the oldest line
of a turn is the broadcast that made it public. Full argument: `internal/ui/roomhistory.go`.

**The room's working line is one row or none, and every figure on it belongs to one agent.**
`heartbeatLine` has been Claude Code's `✻ Calculating… (1m 51s · ↓ 11.6k tokens)` since PR #15 and was
only ever hung on `DM.View`, so the surface somebody supervising a fleet sits on said nothing while
three agents worked. A row per working agent is thirty rows taken from the transcript at fleet size,
and a block of rows that comes and goes changes a pane's height at an arbitrary moment — so
`roomWorkingLine` names the **oldest running turn** and counts the rest (`+2 more working`). Summing a
fleet's tokens beside one turn's age would be two agents' numbers in one sentence. `Room.chrome` is
`DM.chrome`'s field for `DM.chrome`'s reason: the row appears on a status push rather than on a
resize, so a `View` guarded on width and height alone drew one row more than it was given.
**The room draws its own minimal form of the line** — `✻ Sailed for 49s`, `roomHeartbeatLine`, a
past-tense word and no parenthesised token clause — where the DM keeps the fuller one. The room is
the glance, so it leans on the word and drops the tokens; the words are Wake's own nautical-and-dawn
pool in `internal/ui/roomwords.go`, short because the room shows exactly one at a time. The head
still shimmers; `roomWorkingWord` keeps an agent's own `activeForm` when it wrote one.

**A DM's working line becomes a done line rather than vanishing.** When a turn finishes the row above
the composer stops being the spinner and reads `✻ Cooked for 1m 59s · done 6:48 PM` — a past-tense
word, the turn's duration, and the wall-clock time it landed — static and dim, because the turn is
not alive and nothing about it animates. It stands until the next turn (which shows the spinner
again) or a park, an end or a gap that forgets it. **DM only**: the room is the glance and draws many
agents, so a per-agent done line there is noise. The word pool is Wake's own past-tense list
(`internal/ui/donewords.go`), authored for the same reason `heartbeatwords.go` argues and shorter for
the reason it is longer — the done line is one agent's own in its DM, never thirty side by side. The
duration and the done time are **captured at the working→idle edge** in `Fleet.WithStatus` onto
`Agent.doneAt`/`turnDur`, not derived live, and gated on `Agent.watchedStart`: an agent whose *first*
report was already working began its turn before this client attached, so the start is unknown and it
gets no line rather than one whose duration is really only the time since attach. **A park, an end and
a gap each forget it** — the first two in `WithStatus`, the gap in `ForgetTurns` — because a woken
session reports idle directly, with no working report in between, so the pre-park summary would
otherwise reappear the instant the pane reopened. `DM.hasBeat` is the one predicate `baseChrome` and
`SetSize` both count the row by —
it is the working line's row that stays occupied through the done line, so the transition costs no
height change, the alt-screen hazard `DM.chrome` exists for.

**While a `/compact` runs the DM draws a third form of that row — `✻ Compacting conversation…` — and it
is indeterminate because the wire gives it nothing else to be.** A compaction announces itself with two
`system/status` frames: a `status:"compacting"` start flag and a terminal one carrying a
`compact_result`, resolved in the airlock to `NoticeCompacting`/`NoticeCompacted` (`systemNoticeFor`,
off the payload — both share subtype `status`). **The end keys on `compact_result`, never the
`compact_boundary`**, because a *failed* compaction emits the former and no boundary at all
(`slash-commands.jsonl`). There is **no progress figure anywhere on the stream** — Claude Code's own
`2%` bar is computed inside its interactive TUI, off nothing a headless session emits — so Wake draws
Claude's line without the bar: a shimmer that says the work is live, and no percentage it would have to
invent or scrape (the non-negotiable). The state lives on `App.compacting` (session id → start), folded
by `observeCompaction` and read at draw time through `WithCompacting`, keyed by id for `tails.go`'s
reason. It **wins over the done line**: a compaction runs *between* turns — each of its several result
frames clears the turn — so the agent is idle exactly when the stale `✻ Cooked …` would otherwise show.
**DM only** for the done line's reason, and it keeps the ticker alive (`anyCompacting`) the way a working
agent does. `pruneCompacting` on every report is the backstop for a compaction cut short by a crash,
which never sends its outcome; the notices leave **no transcript block** (`noticeLabel` omits both), so
the pinned line is the only place it shows. Full argument: `internal/ui/compacting.go`.

**Every ordinary exit is a key the Update loop reads, so the emergency one is a byte read before it.**
⌃Q parks the fleet and quits, ⌃O then ↵ detaches, ⌃C parks one agent — all three are `tea.KeyMsg`,
and all three are gone the moment the loop is what has stopped. It can be: `Update` calls `View`,
`View` goes through one `os.File`, and a terminal that stops draining that file parks the write
inside the renderer's mutex — which is the goroutine that reads every message. **And a signal does
not rescue it**: bubbletea's `handleSignals` does `p.msgs <- InterruptMsg{}` on an *unbuffered*
channel only the wedged loop reads, so **SIGINT and SIGTERM are both swallowed**, leaving SIGHUP,
SIGQUIT and SIGKILL — none of which run its terminal restore, so the operator gets a shell back
inside an alt screen with mouse reporting on and the tty still raw. Measured, not reasoned about:
`TestAWedgedProgramSurvivesTheSignalsBubbleTeaHandles`. So `cmd/wake/killswitch.go` reads the tty on
a goroutine of its own and decides before Bubble Tea has seen the byte — `inbox.go`'s rule about the
socket, one layer further out. **Two of the same key**, which `detach.go` rules out for ⌃O and which
is right here for the reason that ruling turns on: a same-key confirm is wrong when the *first* press
is invisible, and both of these have a visible first press, so a second is never the reflex that
follows silence — it is the reflex that follows the first press not having worked. Anything at all
between them disarms, which is what keeps ⌃C meaning park. **Both keys, because either can be the
one that does not arrive**: ⌃Q is XON, and `decisions.md`'s own open worry is the layer that is not
the tty driver — tmux, screen, ssh, cmux. It adds **no legend glyph and no `tea.Key…` case**, which
is ⎋⎋'s reason: it is not in `App.key` at all. Arming it takes raw mode off Bubble Tea — `initInput`
claims it only for a reader that is itself a terminal, and the reader it gets is a pipe — so
`converseModel` owns the restore.

**Park is recoverable; stop is not.** `⌃C` parks the focused agent, `⌃Q` parks the fleet and exits,
`wake stop` ends everything and clears the park book. A parked session keeps its id, name, label and
directory in `parked.json` beside the socket.

**`⌃Q` waits for the daemon to say it took the park before the window closes**, and the exit line
says which of those happened. It used to write `FrameParkAll` and `tea.Quit` in one `tea.Sequence`
and print `Parking N agents.` off a flag the *keypress* set, so a write the daemon refused, dropped
or never received was indistinguishable from one it took - reachable three ways, including a nil
connection, where `a.write` hands back a nil command that a sequence runs as a success. The
instrument is a `FrameStatus` written **behind** the `FrameParkAll` on the same connection:
`serveClient` dispatches one connection's frames in order, so that reply cannot come back before
the verb was taken. **This is the one `FrameStatus` `ui.App` ever writes**, and `parkAllTaken` is
the single place a reply and a push are told apart. The residual is a window one round trip wide -
`launch` confirms every spawn, fork and wake with the same `FrameStatusReply` and no correlator, so
a `/new`, `⌃F` or `/resume` in flight when `⌃Q` is pressed has a reply of its own coming and this
takes the first. Closing that needs a correlator on the frame, which is a daemon change, and the
point of this instrument is that it is not one.

**A daemon restores none of it, and that is the design.** ⌃Q then `wake` is a **fresh room**: the
book is carried on `rpc.Status.Parked`, which is disjoint from `Sessions`, so a parked session draws
no roster row, opens no conversation, claims no name and takes no cursor. It is *addressable and
nothing else* — `/resume <name>` resolves against that list and `unparkRecord` launches from the
record. Restoring them into the fleet is what handed somebody back the whole roster and every
transcript one keypress after they quit it, which is the opposite of what ⌃Q means. The **id** is
still protected across the restart, because nothing else can be: `admit` refuses a spawn under an id
the book holds, since a second process on that transcript branches it with no error on any wire. The
**name** is not, deliberately — a daemon holding every parked name is a daemon holding the fleet — so
a resume whose name has been taken since comes back under a pooled one. Bare `wake` still opens a
room rather than spawning when the book is non-empty (`reopensRoom`); only a machine with *nothing*
at all is first run. **`⌃C` refuses a blocked agent** and names `⎋` instead: parking closes stdin,
and a permission ask that dies that way is indistinguishable from an operator deny — it survives the
wake as a "no" nobody said.

**Two live processes on one session id do not collide — they branch**, with last-writer-wins and no
error on any wire. There is nothing to detect afterwards, so every check is Wake's own and happens
before the second process exists: `resumeSafe` asks the OS (one `ps`, matched on a flag *and* its
value), every error is a refusal, and `launch` takes the row **before** it starts anything.

**Forks and imports are snapshots.** A fork is `--resume <parent> --fork-session --session-id <new>`
emitted as one literal; the parent's transcript is byte-identical afterwards. Import is a fork rather
than a resume — it costs the original id, and it is the only safe primitive, because a `claude`
somebody started by hand carries no id in its argv for `resumeSafe` to find.

**Anything waiting on a spawn waits on the id it minted, never the parent's.** A client waiting on
the wrong id does not fail — it waits forever with nothing printed. The daemon addresses every fork
refusal to the fork's own id for exactly this reason.

**The socket is drained by a goroutine that does not draw** (`internal/ui/inbox.go`). Bubble Tea has
one Update goroutine and it renders; the daemon hangs up on a client whose write blocks for 5s, so a
window drag used to disconnect a live conversation. Nothing that renders may sit between the socket
and the ring. Geometry changes go through one pending value and one 80ms settle
(`internal/ui/geometry.go`) — measured 93ms against 4,681ms without it.

**A preview may never cost the record a slot, and it is never called a gap.**
`--include-partial-messages` makes every output token an ordinary `rpc.Frame` — ~1,300/s across a
fleet at the corpus median and ~2,800/s at its maximum, against ~100/s of everything else — so a
token taking a ring slot spent a 250ms stall's whole buffer on previews and evicted **completed
blocks, permission requests, receipts and turn endings**. Nothing on the permission wire times out,
so an evicted `can_use_tool` is an agent blocked forever with nothing on screen, and `App.wants`
cannot help because it runs only *after* frames leave the ring. Two rules close it, both in
`inbox.go`: a partial **folds** into its session's unconsumed one rather than taking a slot — deltas
are additive, so occupancy stops depending on the token rate and a frame of any other kind for that
session closes the fold — and a partial **never evicts**, so one arriving into a full ring is dropped
where it stands. It is **not counted as `dropped`** at either end of that, because the notice says
the conversation above has a gap and then calls `forgotModes`, and a lost token is neither: the
completed block follows it. `internal/daemon/client.go` holds the same rule for its own queue —
`partialCeiling` reserves half of `clientQueue` for the record, and a dropped preview is not
confessed. Measured rather than asserted: the fold costs ~100ns and ~350 bytes a token
(`BenchmarkInboxStall`, 161–192µs per fleet-second against 54–62µs for the same frames as record),
and a preview on a full client queue got *cheaper* to refuse, 4.4ns against 8.9ns
(`BenchmarkClientEnqueue`).

**The composer grows with the draft, and the pane decides how far.** An empty box is one row and gains one per wrapped row of what is typed, up to `maxComposerRows` — but the bound handed to it is `composerRowsIn`, which is what the pane has left after the transcript's floor and the chrome the draft does not own. **The composer never bounds itself**: a box that grew to its own cap in a short pane, or in one of four grid panes, would make the frame taller than the terminal, which is the alt-screen failure the rule below exists for. Three findings paid for in `docs/notes/decisions.md`: typed runes reach the draft through `InsertString`, which is the one path that does *not* end in bubbles' `repositionView`; that reposition is a silent no-op until something has rendered the text area, because `viewport.ScrollDown` returns early while it holds no lines; and it runs against the height the box had *before* the fit, so an update that adds a row is given the bound first and fitted back down after. Measuring the draft uses a text area of its own — the real one shares a scrolled viewport by pointer, and measuring through it reported one row for a three-line draft.

**A pane's chrome is the third thing its transcript's height depends on, and the only one that is not an argument.** `DM.chrome` records what `chromeHeight` returned when the transcript was last sized, and `View` re-sizes when it moves. The heartbeat's row appears when an agent starts a turn and the status bar's when the first fact about a session arrives — neither is a resize, so a `View` guarded on width and height alone drew **one row more than it was given**, and a frame one row too tall scrolls the alt screen away on every draw. Caught by the pty harness with an empty screen and by nothing else; `TestThePaneStaysInBoundsWhenItsAgentArrivesAfterSizing` holds the order `App.dmPane` really uses — size first, agent second.

**A streamed answer is a preview and never a record, and that is what makes it affordable.**
`--include-partial-messages` multiplies a session's frame rate by its output token rate — the
recorded corpus's median is **43.5 tokens a second** and its fastest 93.9 — so the only question is
what may be done per token at thirty of those at once. The obvious answer, re-rendering the block
that is growing, is measured and dead: `internal/render` runs behind **one process-global mutex
shared by every session**, and streaming a block through it costs the integral rather than one
render — **303ms for a single 1,024-token block against 4.6ms for the preview, 65×**, and visibly
superlinear (7× at 64 tokens, 19× at 256). A tick does not fix it either; it lowers the rate and
not the growth, and it is a poll where a wait will do. So `core.KindPartialText` never enters
`DM.events` or the transcript: it is a **plain-text tail**, bounded to `maxPreviewRows`, wrapped on
change and never through glamour, cleared by the completed block or by the turn ending — which is
the interrupted case where no block ever arrives. The transcript is byte-identical to what it was;
the completed block still goes through glamour exactly once, as it always did. **And a token is
accumulated only for a pane on screen** (`App.wants`): `App.dms` holds every conversation ever
*opened* and `withDM` copies the whole map of `DM` values per write, so an operator who had looked
at all thirty agents was paying thirty large struct copies per token — measured at 106–123ms per
fleet-second before that gate and **10.3–10.6ms after it**, with allocation down from 530MB/s to
19MB/s. The *clear* is deliberately not gated, or a conversation closed mid-turn would come back
showing a sentence that finished long ago. **And leaving drops the tail outright** (`DM.Leave`),
because freezing it without dropping it is worse than either: a pane reopened *before* its block
landed appended the new tokens to the old ones and drew a sentence the agent never wrote. Leaving is
the one event on every path a pane stops being drawn on, which is exactly the set `App.wants` stops
accumulating for, so the two halves are one rule. One second of a
thirty-agent fleet streaming at the corpus median costs **7.4–8.3ms, under 1% of one core** — against
62ms for per-token rendering *of an average-length block*, which is a floor rather than a
worst case. `BenchmarkStreamingFleetSecond` and `BenchmarkOneBlockStreamed` are the pairing; the
full argument is `internal/ui/partial.go`'s header. Two things get **no** preview, for one reason
between them — a preview is replaceable only on a surface that follows one speaker: **the room**,
which interleaves thirty agents, and **a subagent's tokens**, which `partialEvent` drops on
`parent_tool_use_id` the way `fold` already drops a subagent's tool calls. Both still draw the
completed blocks, with the attribution `dm_blocks.go` gives them.

**Only a *width* change returns a reader to the newest line.** Both panes re-wrap on width, so a
scrolled offset stops pointing at what was being read. A height change does not. For the same
reason, the last-read marker is anchored to an **event**, never a scroll offset, and a conversation
keeps the newest three.

**Card keys are runes, and a permission or a plan is settled by the rune then `↵`.** `a`/`d` exist
only while a card is up and are read only when the composer is empty — but the *first* character of
every draft is typed into an empty composer, so the arm-then-confirm is what turns an accident into a
lost character instead of a granted tool call. A settled card cannot be unsettled. Every input that
is not the confirm takes the arm back, and that needs four call sites (key, composer, **mouse**,
digit). The key line is honest in both directions: it offers `esc interrupt` beside the answer keys —
the one key that destroys the ask, dropped first when the line will not fit — and while a draft makes
the runes unreadable **the focused pane's** line advertises none of them, saying instead what brings
them back; an unfocused pane's card keeps its labels, because its keys were never the draft's to
pause.

**A question is settled by a review step instead, which is strictly stronger than the arm it
replaces.** `ShapeQuestion` walks `N+1` steps — the questions, then `stepReview` — and the last one
lists every answer that is about to travel beside `Submit answers` / `Cancel`. The arm named a verb;
the review names the answers, so `[a]nswer` is gone from a question's key line and `[d]eny` stays.
`ShapePermission` and `ShapePlan` keep the arm: they have one named action and nothing to review.
**The review earns no binding of its own** — it is drawn through the questions' own `optionRow`, so
`↑↓`, `↵` and the digits reach it unchanged, which is what keeps the card a bijection with what
`cardkeys.go` binds. Submit with a question unanswered takes the operator *to* that question rather
than writing a short answer the encoder refuses beneath them. Full argument: `internal/ui/cardsteps.go`.

**Free text is a mode, entered deliberately, and it is the one place `↵` on a draft is not a
message.** `Other…` is a synthetic row past the options the model supplied; picking it — or pressing
`[d]eny` — titles the composer for what it holds and puts the card into answer mode. A typed answer
costs nothing on the wire: `Card.answers` is already question text to a *label*, and the operator's
own words are a label the list did not contain. An empty refusal still sends `cardDenyReason`,
because a blank one reads as a tool that failed for no reason. **In answer mode the card takes `↵`
and nothing else** — every other key goes to the draft, which is not tidiness: a digit that still
reached the options while a refusal was being written was the disarm rule's own accident arriving
through the box that replaced the arm (`d`, then `1` to choose instead, and the `↵` after it denied
the agent). `⎋` abandons the draft and leaves the turn alone. Full argument:
`internal/ui/cardanswer.go`.

**An ask belongs to its agent's conversation, and the room draws none.** `Cards.For` is the only way
a card reaches a surface: a conversation puts its own agent's ask (`App.cardOf`), and `id == ""` — the
room — puts nothing. The room used to take the oldest ask whose agent had no pane on screen, and
**that is the rule this reverses**, on the owner's report: leaving iris's conversation moved the
question being answered into the group chat. It is wrong for that surface twice over — the room holds
one card's worth of rows, so a fleet with several agents blocked at once (the case this build exists
for) saw one of them and `+N more waiting`; and it interleaves thirty agents, so the question arrived
stripped of the turn that raised it. **Drawn-ness no longer enters into it**: a conversation puts its
ask whether its column is on screen or slid past, so moving the keys moves nothing else.
What the room shows instead is **nothing** — deliberately, and it is the trade: an agent blocked with
its conversation closed is announced by the roster row and the awareness strip's `N need you`, and
`⌃X` opens the next one. Nothing on that wire times out (the corpus records one ask blocked 342
seconds with zero bytes out), so those two tells are load-bearing rather than decoration.
`cardKey` reads `App.cardOf(a.focus)`, never `Cards.Top`; only two blocked agents can tell those
apart, which is what `TestTheKeysAnswerTheCardTheFocusedPaneDraws` is. `interruptTarget` lost its
middle case with the room's card — from the room, `⎋` and `⌃C` act on the roster's pick, which is
the only thing on that surface naming an agent.

**And a card is drawn over the composer, which gives the mouse a second reader.** A screen row is
only a transcript line once you know how many of a pane's rows are conversation, and the pane cannot
be asked: its transcript is sized by the last *geometry* change, while its chrome moves without one —
a card goes up, a completion menu follows the word being typed, an answer streams a preview, the
draft grows a row — and `View` re-lays a **copy** for the frame and drops it. So the stored height is
too big by exactly the rows that chrome took, and `pointIn` clamps any row under the transcript into
a real line: a drag across the **query bar** highlighted and copied an answer nobody dragged over,
landing further from the pointer the further back the reader had scrolled, because `extendSelection`
reads those same rows as "the drag has left the bottom edge" and scrolls one line per motion message.
`App.transcriptRows` sizes the menu-carrying copy through the draw's own `SetSize` and reads the
height back, so the two cannot disagree; `startSelection` fences the anchor on it and keeps it as
`App.selRows`. Below that fence a press now falls to `startComposerSelection` rather than being
discarded — the query box's own draft rows take a selection, and the chrome around them takes a
frame-wide screen selection instead of nothing (see `composersel.go` and `screensel.go`). Two rulings
hold it together. **The anchor is taken before the keys move** — `refocus`
re-sizes the panes and a picker belongs to whichever pane holds them, so a measurement after it
measures a frame nobody clicked. And **a drag's edge stays the window it was taken in**, which is
`selTop`'s own rule: a motion message arrives per cell crossed, and re-rendering a pane's chrome on
each one is the work per mouse pixel `mouse.go` is written to avoid. Each producer of that chrome has
a test that goes red without the measurement — the card, the completion menu and the preview — and
each finds its rows on the pane as it is **drawn** rather than trusting the arithmetic under test.

**`↑↓`, `←→` and `↵` belong to a question card only while one is drawn in the focused pane — and the
arrows are read whether or not there is a draft.** `↑↓` walks the options, `←→` walks the steps, and
`↵` chooses the cursored option — claude's own question keys — all handed back on every other shape,
because `↑↓` is the roster's and a yes/no has nothing for a cursor to walk. The digits still pick,
and they are what a narrow pane keeps: `questionKeys` drops the move keys first when the line will
not fit, because the digits and the refusal are the only way to answer and the only way out.

**`cardKey`'s composer gate is an argument about characters, so it applies to characters only.** `a`,
`d` and the digits are letters people type and keep it; **the arrows never needed it**, and applying
it to them meant a card's own keys reverted to the roster the moment anything was typed — with the
agent still blocked and still asking, which is the state somebody is most likely to be in, because
they had started writing a reply to the thing that stopped them. `↵` keeps the gate outside answer
mode, since a draft is a message to send. `←→` are claimed **only on an empty composer**: a draft
needs them for its own cursor, and they were in no `App.key` case at all, so on an empty one they
reached the text area and did nothing — the claim costs nothing and takes nothing.

**The manager can send, interrupt and spawn, and nothing else.** Spawn was refused until 2026-08-12
and is allowed now because `daemon.liveCap` exists: `maySpawn` refuses past 30 live sessions on every
path, and the tool's directory must be one the fleet already occupies, so it adds no reach onto the
machine. Send and interrupt are undoable by looking at the room; spawn is not, which is why its own
cell calls it the weakest one there.
Park, wake, fork, rename, label, import, stop, allow/deny and **mode** are refused with a recorded
argument each in `cmd/wake/mcpguard_test.go`. Mode is the newest and the shortest path on the list: a
manager that could set a permission mode would be the fleet deciding it will not be asked, in every
future decision that session makes rather than one, and unlike a message it shows up in no row this
surface returns. Everything the tools return is text an agent's model wrote, so every
emitted line goes through `mcp.oneLine` — a newline in a tool argument used to forge a row and let one
agent speak in Wake's voice about another. **And it is bounded on the machine too, as of
2026-08-12**: `argv.go` emits `--tools ""` from the same literal as `--mcp-config`, which empties the
built-in set — no `Bash`, no `Write`, no `Edit` — while MCP tools pass through untouched. It is
**not** `--allowed-tools`, which this project planned to use and which bounds nothing at all
(`docs/superpowers/notes/2026-08-12-tool-bounding-findings.md` §3).

**The room seats a manager by default, and `/manager` is the switch.** Spec §12 gives the manager "a
permanent seat in every group" and the build had no way to seat it: `wake manager` at a shell was the
only thing that produced one, so the room refused every unaddressed draft and pointed the operator
*out* of the room to fix it. Every verb that opens the TUI now writes `ui.ManagerFrames` on its
connection before Bubble Tea exists — `requestFleet`'s slot and its argument — and `/manager` is the
same decision under a command: **absent → spawn, parked → wake, running → park**, with ⌃C's own
`parkTarget` refusing the one state park must not touch. One `Fleet.manager` decides which row is the
manager (ended is not — the name went back to the pool, so a spawn gets it and a wake has nothing to
address), one `spawnManagerFrame` spells starting one, and the parked arm goes through `/resume`'s
`bringBack`. **A manager start opens no pane**, which is `cmd/wake/manager.go`'s service ruling
arriving in `startArrived`. Two consequences worth knowing: the off switch does not persist, because
`parkedRecord` carries no reason and "parked by `/manager`" is the same record as "parked by ⌃Q", so
the next `wake` turns it back on — and **the manager is an ordinary row on every surface**, so it
takes a roster row, a place in the attention ranking and a slot in the strip's count. That last one
is `deferred.md`'s open question, and it is now unavoidable rather than rare. It is a **command
rather than a key**: with the default on, this is the rarest verb in the build, `legendEntries` is a
bijection with `App.key` so a chord still costs a `legendEntries` entry to add, and every remaining
ctrl byte is worse — `⌃M`/`⌃I` are Enter and Tab, `⌃J` is the composer's newline, and `⌃S`
is XOFF beside a `⌃Q` already bound to *park the fleet and quit*.

**`/manager-stop` is the ending, and it is a second word rather than an argument.** `/manager` parks;
this writes `rpc.FrameStop`, so the session ends and the name returns to the pool — which is why the
next `/manager` *spawns* rather than waking. A separate verb because `managerTakesNoArgument` already
refuses `/manager off` on the grounds that a toggle firing under a word it did not read does the
opposite of what was typed half the time. Two arms refuse: **a parked manager**, because the daemon
refuses a stop at a session with no process in two different shapes — `has ended` for a ⌃C row whose
`gone` channel is closed, `unknown session` for a park-book record after a restart, which has no row
at all — so the refusal names `/manager`, which wakes it first; and **no manager at all**,
which is where an *ended* one lands too, since `Fleet.manager` reads ended as absent. **It does not
borrow `parkTarget`'s blocked refusal**, and that inversion is deliberate: park refuses a blocked
agent because the denial nobody made *survives the wake*, and a stop has no wake. See
`docs/notes/decisions.md`.

**`/quit` is `/manager-stop` for an ordinary agent, and its removal is a client-side fold rather
than a daemon change.** The stop half is identical — `rpc.FrameStop`, irreversible, releases the
name, reaches a blocked agent and refuses a parked one — so `internal/ui/quit.go` mirrors
`service.go`'s rulings. What is new is the *removal*: an ended session is deliberately kept as a `·`
row (`Fleet.WithStatus`, the daemon's `recentEndings`) so a client learns of an ending it **missed**,
and a `/quit` is an ending the operator **typed**. So `awaitingQuit` remembers the ask and
`departedQuit` — run from `applyStatus` on every report, because the daemon re-reports the ending
until it leaves the recent ring — drops the agent from the fleet, the ⇥ ring, the grid and the roster
cursor once the ending is **confirmed**, never on the keystroke (⌃C park's own rule: a stop lets the
turn finish, so a working agent stays until it ends). It is the one place a live-reported agent
*leaves* `Fleet.Agents()`, so `Fleet.drop` and `forgetConversation` are the whole of it; the hide is
**per-window** (the `quitting` set is on `App`, not the wire), so another operator's roster keeps the
`·`. `quitting` is pruned once the daemon stops reporting an id (it has left the recent ring), so the watch
set stays bounded rather than growing one entry per `/quit`. Both entry points reach one handler: bare
`/quit` targets the focused conversation, `@who /quit` rides the `mentionCommand` bridge, so `quit` is a
`roomTargetCommand`. **The manager is refused** and pointed at `/manager-stop` — one ending path for
the manager, not a second verb that also drops its row. Full argument: `internal/ui/quit.go`'s header.

**The manager's configuration is a function of its name**, applied in `launch`, never a field on the
wire — a path on the wire would let anything that can dial the socket choose that session's command
line, and a wire field cannot survive a park. `--mcp-config` is emitted only ever beside
`--strict-mcp-config` and `--tools ""`: without the first the manager inherits every MCP server on
the machine, and without the second it inherits Claude Code's whole built-in toolset.

**A routed message is echoed into the room and into every *held* conversation it reached, spelled as
it was typed.** `sendRoom` writes the room's one echo — one broadcast is one thing you said — and
`echoToRouted` writes the same text into each addressed agent's DM, mention included, marked
`core.Event.FromRoom` so `userBlock` can head it `› you · from the room`. The mention is kept
because it is the *only* thing separating it from a turn typed into that composer: `r.Text` strips a
leading `@name` before sending, since Claude Code expands one before the model sees it, while a DM
sends what you typed verbatim. **Only conversations `App.dms` already holds** — the same rule
`App.observe` uses for the agent's own events, which is what makes the two halves symmetric. An
unopened one is filled from claude's transcript, `DecodeTranscriptLine` keeps user lines, and
neither pane de-duplicates, so materializing would draw one turn twice in two spellings.
`FromRoom` is presentation only, for `Echoed`'s reason. Full argument: `docs/notes/decisions.md`.

**A peer's cross-session message shows in the room, headed by the sender.** Claude Code's own
peer channel injects one session's message into another wrapped as a `<cross-session-message
from-name="…">…</cross-session-message>` envelope. Probed 2026-08-31: without `--replay-user-messages`
the envelope reaches only the recipient's on-disk transcript, so the room — fed by the live stream —
never saw it; with the flag it replays live as a `user` frame carrying the envelope. So Wake now emits
the flag (`argv.go`), the airlock resolves the envelope to `core.KindCrossSession` (`wire.go`'s
`crossSession`, one decoder for the live stream and the transcript both), and the room admits it
(`fold`) attributed to the **sender** — `Fleet.crossSpeaker` resolves `from-name` to a fleet agent for
its identity colour, else a bare name for an outside session — with a `↪` lead so it reads apart from
the sender's own room turn, folded past `roomInlineRows` like a reply. It survives a room restore
too: `collapseBroadcasts` keeps a `KindCrossSession` line unconditionally (a first-class room event,
not agent prose gated by an open broadcast) and `roomHistoryLines` heads it with the sender.
**The discriminator is the envelope on *string* content, not a wire flag:** `crossSession` fires only
where a user frame's content is a bare string — which is what Claude injects a peer message as — and
never on the array content `EncodeUserMessage` writes, so a message that merely *contains* the
envelope (pasted, quoted, or composed by an agent through the manager's `send`) stays the user's own
turn and cannot forge a peer line. **The one place the flag would have double-rendered is the DM**,
whose live feed `observe` now drops every replayed `KindUserText` from (`replayedUserEcho`): the
operator's own send has `sendDM`'s local echo as its single source, and the manager's sends, a
compaction summary and `<local-command-stdout>` are replayed echoes the room already drops too — they
return on reopen off disk. `event.go`'s `Echoed` comment reserves that single-source call for the App
that owns the local echo, which is `observe`. Full argument:
`docs/superpowers/specs/2026-08-31-cross-session-messages-in-room-design.md`.

**Slash commands resolve against a closed set Wake owns; anything else is text.** Claude's own
`/model`, `/clear`, `/compact` and a user's `~/.claude/commands/*.md` all have to keep reaching the
agent, and nothing can enumerate the latter at the moment the question is asked. `/add-<agent-name>`
is refused: it is not decidable from the draft, and every live agent is already in the room.

**There are two kinds of command, and the second may claim one of claude's own words in one form.**
`/resume`, `/new`, `/name`, `/task`, `/mcp`, `/manager`, `/manager-stop`, `/board` are addressed to **Wake** —
target-independent, routed before anything else, because `/resume` has to work on a parked session
and `/manager` on a fleet that has none. `/manager-stop` is target-independent for a sharper reason:
stop is the one verb nothing brings back, so it may not be aimed by a roster cursor. `/effort` and
`/model` are
addressed to a **session**, so they run after `App.route` has resolved one, on the remainder it
produced. Both are words claude advertises, and the corpus rule narrows rather than bends: **a word
claude advertises may be claimed only in a form claude is recorded doing nothing with**, and
`bareOnlyCommands` names the fixture that earned each one — checked to exist, so a word cannot be
exempted by assertion. Anything with an argument passes through byte for byte. Both routers live in
`slash.go` because `TestNothingButTheRouterKnowsWhatASlashMeans` holds *what a leading slash means*
to one file; a surface that must build a command takes `configureVerb` instead.

**The completion menu offers; it never routes, and it never takes `↑↓` or `↵`.** Every `init` frame
carries `slash_commands` — the session's own commands and the operator's `.claude/commands` files
together, 133 across the corpus — and the airlock dropped the key until now. Decoded onto
`core.SessionFacts` and folded onto `ui.Agent`, it is what the composer offers under a draft that
begins a `/command` or an `@`. **It rides the fleet report as well as the init event**
(`rpc.SessionStatus.Commands`, folded by `withCommands`): the event alone leaves a client that
attached after an agent's init with an empty menu for it, so the report carries it too — the only
route to a late attach, the same one `Effort` and `Budget` take. **It cannot decide routing** for `slash.go`'s own reason: the list is
per session and arrives after the first frame, while a draft is judged per keystroke — a menu may be
wrong about a machine that has started nothing, a fence may not. The keys are `⇥` to complete and
`⌃N`/`⌃P` to walk, read above `App.key`'s switch the way `cardKey` and `pickerKey` are, so they take
no legend entry and the menu advertises them on itself. The menu never takes `↑↓` or `↵`:
the menu arrives while somebody types rather than because they asked, so it may not give the one
irreversible key a second meaning. It is rebuilt on a keystroke and on a fleet report and never per
frame.

**The menu belongs to a cursor and to a pane, and its directory read is not on the goroutine that
draws.** All three were the trailing token of a string. **The cursor:** `⌃N`/`⌃P` shadow the text
area's line keys, so a menu claimed by an `@` at the end of the buffer would take them from every
cursor position in it. A menu exists only while the cursor is at the end of the word it
describes (`Composer.AtEnd`), which is what the "it costs one space" trade always claimed. Plain `↑↓`
walk the prompt history or move the cursor within the draft (see below), never the menu; because a
menu is only up while the cursor is already at the end of its word, `↑` there either climbs to a line
above — off the trailing token, closing the menu on the rebuild — or, on a single-line draft, recalls
a prompt, and either way the menu is left to the offers.
**The pane:** `completion.pane` is the conversation it was built for, because two panes holding the
same characters are not holding the same menu — two repositories with a `README.md` each completed
one from the other, and that reference *resolves*. **The read:** `pathScanMax` bounds the entry count
and nothing bounds the latency, so a hard NFS mount or a stalled sshfs on the Update goroutine is a
window that stops drawing and stops answering the keys that would quit it. It is a `tea.Cmd` tagged
with the directory it read, one at a time — a directory that never answers costs one goroutine, not
one per character — and a read is a *listing*, narrowed per keystroke, so a path costs one read per
directory rather than one per character. One directory, never a walk.

**Open mention mode widens a message; it does not widen a knob.** `@john hello` in open mode reaches
the fleet and keeps the name in the text, which is a property of something being *said*.
`@john /effort` configures one session, so it stays with john — `roomRoute.direct` carries the
pre-widening reading rather than the router being consulted twice.

**A `Picker` is not a `Card`, and the reason is mechanical.** `Cards.Reconcile` rebuilds the open set
from every fleet report and drops what is absent; a picker has no request id and is in no report, so
one held there would be deleted by the next status push. It shares `optionRow` and is dismissed
where an armed card is — in `App.update`, on the keys that go on to the composer.

**A session's directory moves, and it is two fields because two questions have
two answers.** `EnterWorktree` and `ExitWorktree` are on the tools list of every session Wake spawns
— only the manager's built-in set is bounded — so the spawn directory stops being the running one
without Wake doing anything. `init.cwd` is decoded in the airlock, carried on
`core.SessionFacts.Dir`, and folded by `agent.observe` into **`a.cwd`**, which the fleet report
carries as `rpc.SessionStatus.Cwd` for the roster row, the status bar's branch and the workspaces
sidebar. `internal/ui` reads only that one — `Agent.Cwd`, folded by `runningIn`.

**`a.dir` is where the session was *started*, and it never moves.** park writes it down, `unpark`
launches from it, and a fork runs in it, because **claude locates a transcript by the directory the
process started in even when every frame names a worktree** — `discover.go`'s 58-of-428 case, and
`forkSource`'s own refusal says so in as many words. The first version of this followed the cwd into
`a.dir` and had one field, which made park record the worktree and a wake resume against the wrong
project slug: an empty conversation under a live session id, which is the branching hazard arriving
through the display half. **And `internal/mcp`'s `fleetOccupies` bounds the manager's spawn tool on
`Dir` precisely because an operator chose it** — one field would have let an agent widen where a
manager may spawn by moving itself.

The cwd is **absolute or refused**: it arrives on the child's own stdout rather than on a Frame, so
it passes no wire fence, and `agentAuthored["Cwd"]` records it as the agent's own. `launch` refuses a
non-absolute `Config.Dir` for every caller — spawn, fork, import and wake — where `maySpawn` reached
only the spawn frame.

**Wake creates a worktree; Wake never removes one, and it never passes `--worktree`.** The path is
`<repo>/.wake/worktrees/<name>` on branch `wake/<name>`, anchored to the repository root rather than
to the client's directory. A name that is not one path segment is refused before git is reached, on
both sides of the socket. A `git worktree add` that fails **refuses the spawn** rather than falling
back to the repository — an agent in the shared tree is exactly what asking for a worktree meant to
avoid. Removal is left to `git worktree remove`: a worktree holds uncommitted work, so removing one
automatically would be a second irreversible verb, and `wake stop` is meant to be the only one.

**A path on the wire is fenced by what it becomes, and the two spawn paths become
different things.** `--add-dir` names directories a session's tools may reach, and a client that can
dial this socket already chooses `Frame.Dir` — so an added directory names nothing it could not have
named there, and it gets `Dir`'s fence and no narrower one: **absolute or refused**, which is also
what stops a word that reads as a flag, since `-rf` is not an absolute path. A separate dash test was
written, found to have an empty domain and deleted; three comments had already credited it with the
absoluteness test's work. `..` is deliberately allowed for the same reason in reverse: refusing it
reads like a fence and is not one, since it grants the same directory its cleaned form does while
refusing `$PWD/../lib`. **`--debug-file` is the opposite case and absolute is no fence at all for
it**:
`/Users/someone/.zshrc` is absolute, and this one becomes a file the daemon's child creates and truncates,
with no transcript, no room line and no permission ask to see it happen. So it is
`--mcp-config`'s ruling one field over — the wire carries a **name**, `rpc.ValidDebugFileName` is
`ValidWorktreeName`'s sibling, and the daemon places it. Both are refused before a name is claimed
(`configRefusal`) and again at `launch` (`launchRefusal`), which is the door `Config.Dir` already
goes through for every caller. **Neither survives a park**, and as of 2026-08-21 the two halves of
that are established in opposite directions (`testdata/stream/add-dir-runtime.jsonl`,
`debug-runtime.jsonl`): `/add-dir` **does not exist at runtime** — the CLI refuses it — so nothing
can restore an added directory after a park, which is the budget's own argument for a
`parkedRecord` field; `/debug` **works at runtime**, so the debug flags may drop and be re-asked.
Adding the field is a feature decision the owner holds; see `deferred.md`.

**Effort was the one thing Wake set and could not confirm, and now the daemon confirms it.** A model
is on every `init` frame, so a wrong label is wrong for one turn. Effort is on no frame Wake receives
*unasked* — so the pane once showed only the level Wake **asked for**, with nothing saying "applied".
The fix is a probe: the daemon sends a bare `/model`, whose reply names the level
(`Current model: … (effort: xhigh)`, `num_turns:0`/`$0`), reads it back, and carries it on the report
as `confirmedEffort`. The status bar prefers the confirmed level, falls back to the asked-for one
until the probe answers, and shows nothing when Wake chose none and no probe has returned. The reply
is **suppressed** at `fanOut` (`internal/daemon/fanout.go` — `absorbProbe`, a counter so overlapping
probes both suppress) so it reaches no client, and the on-disk reply is **filtered** out of restored
history on its own `Current model:` shape (`internal/daemon/history.go`). The parse lives in the
airlock (`core.EffortFromModelReply`). The probe fires once on `init` and again after an `/effort`
change. See `internal/daemon/effort.go` and
`docs/notes/decisions.md`.

**The same probe confirms the *model*, for the same reason effort needed it.** A model is on every
`init` frame, but `/model <arg>` changes it with no turn and so no new `init` — the id on the wire is
a turn stale, and the status bar showed the old model until the next query. So `/model` fires the
same bare-`/model` probe `/effort` does, its reply also names the model
(`core.ModelFromModelReply` reads `Current model: Opus 5 (1M context)` off the same line as the
effort clause), and the daemon carries it as `rpc.SessionStatus.ConfirmedModel`. The status bar
**prefers the confirmed model over the init-frame id**, falling back to the id until the probe
answers. Not park-persisted (a woken session re-probes) and not written to `a.model` — the park
book's model is the alias Wake asked for at spawn, a separate fact from the rendered name. Detected
by `noteModel` in `apply`, beside `noteEffort`.

**Discovery verifies a directory; it never decodes one.** The project-dir slug is lossy, so
`verifiedDir` holds three facts against each other and answers exactly one directory or none.
`slugOf` may only ever appear as an operand of `==` or `!=` — constructing a path from a slug is what
runs a session in the wrong place.

**`wake stop` never claims more than it can see.** The EOF on its connection has two producers and
nothing on the wire separates them, so it waits for the socket file to be unlinked (a stat, not a
dial) and then asks `daemon.Status`, whose third case is the on-disk roster filtered by which
processes still exist.

## Key locations

Update this table in the same commit that creates a path. **A row naming a path that does not exist
yet says so in bold** — a table that cannot be told apart from a build is worse than no table.

| What | Where |
|---|---|
| Entrypoint and verb dispatch | `cmd/wake/main.go` — eleven verbs, two not user commands |
| Bare `wake`: the front door and its branch | `cmd/wake/openroom.go` — `openRoom` (the bool), `seedRoom` (the frame the bool chooses) · `openroomguard_test.go` for the per-state verdicts · `openroomscreen_unix_test.go` for which pane has the terminal, which nothing in process can see |
| Attach, spawn handshake, the detach line | `cmd/wake/attach.go` |
| Resolving "which session did they mean" | `cmd/wake/match.go` |
| `wake fork` | `cmd/wake/fork.go` |
| `wake import` — the picker and adopting one | `cmd/wake/import.go` |
| `wake setup-terminal` — the CLI shape: detect, confirm, apply/undo | `cmd/wake/setupterminal.go` |
| Host-terminal detection, per-terminal knowledge, file I/O, the first-run marker | `internal/termsetup/` — `terminal.go` (`Detect`, pure over an env map) · `multiplexer.go` · `knowledge.go` (`Info`, the verified snippets — Ghostty's `\x1b\r`, Alacritty/VS Code's TOML/JSON-safe Unicode escape for ESC) · `apply.go` (`Apply`/`Undo`/`Status`, append-only and idempotent, auto-writable only for Ghostty/Kitty/Alacritty) · `firstrun.go` (`PromptSeen`/`MarkPromptSeen`, the one-time marker under `$XDG_CONFIG_HOME/wake/`) |
| The one-time first-run offer | `cmd/wake/termsetupprompt.go` — `promptTerminalSetupOnce`, called from `converseModel` so every path that opens a TUI passes through it once |
| Adopting sessions from inside the room | `internal/ui/adopt.go` — `adopt`/`adoptArrived`/`adoptAll` (why the word is not `/import`, why the machine is read from a `tea.Cmd`, why the picker goes to the room and never to a DM, and why the whole set is refused when one name does not resolve) · `adoptguard_test.go`, where the minted id and the frame kind are held statically because both failures are silent |
| The seam a room is handed to see this machine | `cmd/wake/adopt.go` — `machineSessions` (holds no state, caches nothing) · `adoptRows` (why a pane is capped and a terminal is not) |
| `wake status` · `wake stop` | `cmd/wake/status.go` · `cmd/wake/stop.go` |
| `wake manager` · `wake mcp` | `cmd/wake/manager.go` · `cmd/wake/mcp.go` (+ `mcpguard_test.go`) |
| Seating a manager on the way into the room | `cmd/wake/ensuremanager.go` — called by `openroom.go` and `attach.go` |
| The manager switch, and what a fleet with none needs | `internal/ui/service.go` — `ManagerFrames` (pure, both callers) · `Fleet.manager` · `App.manager` · `App.managerStop` |
| Ending one agent and dropping its row | `internal/ui/quit.go` — `quitAgent`, `quitTarget` (bare = the focused conversation, `@who` = that agent), `awaitingQuit`, `departedQuit` (the confirm-on-report drop, run from `applyStatus`), `forgetConversation`, `Fleet.drop` (the one place a live-reported agent leaves the fleet) · `quit_test.go` |
| Claude JSON airlock | `internal/core/protocol.go` (decode) · `wire.go` · `vocabulary.go` · `encode.go` — start at `protocol.go` |
| Airlock tests | `internal/core/protocol*_test.go`, `fixtures*_test.go`, `encode_test.go`, `airlock_test.go` |
| One agent: spawn · events · lifecycle · stop | `internal/core/session.go` |
| One agent's write path: send · permission answers · interrupt · mode · rewind | `internal/core/write.go` — split from session.go once `Rewind` crossed the 800-line hard max |
| How a session names itself on the command line | `internal/core/argv.go` — `identityArgs`, `SessionArgvMarkers` |
| How a session ends | `internal/core/ending.go` |
| Asks: kind, payload, answers | `internal/core/vocabulary.go` · `event.go` · `encode.go` |
| The child's environment, stderr, process group | `internal/core/process.go`, `procgroup_*.go` |
| Live-cap scheduler | **NOT BUILT** — `internal/core/pool.go` is planned |
| Routing: `@name` · manager · broadcast | `internal/core/router.go` |
| Daemon ↔ client transport | `internal/rpc/wire.go` (frames) · `lifecycle.go` (ending verbs, fleet report) |
| Socket, start-or-attach, status | `internal/daemon/daemon.go` — `Status`, `FleetOnDisk` |
| Test-only parent-death lease | `internal/daemon/lease_*.go` — inherited pipe EOF cancels `Serve`; normal product daemons receive no lease |
| Accept loop, dispatch, shutdown | `internal/daemon/server.go` — `quitVerb`, `beginQuit`, `reconsiderEmptyExit`, `shutdown` |
| One supervised session, liveness policy | `internal/daemon/agent.go` — `stateLocked` |
| One agent's stdin path: queue, drain, apply | `internal/daemon/apply.go` — `submit`, `serveInput`, `apply` |
| Spawn, fork, wake, watchdog | `internal/daemon/spawn.go` — `launch`, `forkRefusal`, `admit` |
| Fan-out: one session's events to every client | `internal/daemon/fanout.go` — `fanOut`, where the effort probe's reply is consumed (`absorbProbe`) and the startup probe fires (`firstInit`). Split from `spawn.go` to keep it under the hard max |
| The supervisor a launch runs under | `internal/daemon/launcher.go` — `newAgentLauncher`, `DirectAgentLauncherEnv` (why tests default to the direct path) |
| May this spawn happen at all: boundary, cap | `internal/daemon/mayspawn.go` — `maySpawn`, `liveCount`, `capRefusal` |
| The worktree a session runs in | `internal/daemon/worktree.go` — `sessionDir`, `addWorktree`, and `git`, whose command is bounded by a WaitDelay and a process group (`worktreeproc_{unix,other}.go`) so a post-checkout hook cannot hang it forever. The name fence is `internal/rpc/worktree.go`'s `ValidWorktreeName`, because both sides check it and `internal/ui` may not import the daemon. Wake creates it; Wake never removes it. A **worktree** spawn runs off the dispatch goroutine so a slow git cannot hold a client's other frames — `server.go`'s `dispatch`, where a no-worktree spawn stays in line to keep `mcp.go`'s `act` ordering |
| What `⌃Q` asked for and what came back | `internal/ui/park.go` — `parkAll` (the ask), `parkAllSettled`, `parkAllTaken` (the reply, told apart from a push), `closing` · `internal/ui/hangup.go` — the EOF route |
| Park, wake, and what survives both | `internal/daemon/park.go` — `unpark` (a live ⌃C row) · `unparkRecord` (a book entry, the only path across a restart) · `parkbook.go` — `parkedStatuses`, which is what `rpc.Status.Parked` carries |
| Is anything still running this id | `internal/daemon/liveid_unix.go` (vs `reap_unix.go`, which asks about a pid) |
| Session discovery and import | `internal/daemon/discover.go` · `import.go` |
| Reading a conversation back off claude's disk | `internal/daemon/history.go` — `History`, `transcriptPath` (found by filename, never built from a slug), `answerHistory` behind two verbs · `internal/core/protocol.go` — `DecodeTranscriptLine`, a filter in front of `DecodeLine` rather than a second decoder, and the one on-disk key it reads · `internal/ui/history.go` — the ask and the fold |
| Reading the **room** back off the same disk | `internal/ui/roomhistory.go` — `roomHistoryLines` (the merge, the filter, the broadcast rule), `roomAsk` (the room's own ledger) · `internal/ui/chat.go` — `Room.Before` |
| Narrowing the room to one agent's thread | `internal/ui/roomfocus.go` — `focusAdmits` (pure, id-comparison) · `internal/ui/chat.go` — `Room.focus`/`WithFocus`, the `roomLine.to` stamp, the subset render (a hidden line stays in `said` at `rows == 0`) · `internal/ui/send.go` — `retarget` sets focus off the composer's lone direct `@name`, and stamps `to` on the echo |
| A peer's cross-session message | `internal/core/wire.go` — `crossSession` (the envelope recogniser, beside the wire shapes it is one of) · `internal/core/event.go` — `KindCrossSession`, `FromName`/`FromAddr` · `internal/ui/fleet.go` — `fold`'s admit · `internal/ui/fleetquery.go` — `Fleet.crossSpeaker` (sender attribution) · `internal/ui/observe.go` — the room append and `replayedOwnSend` (the DM single-source) · `internal/ui/chat_blocks.go` — `crossSaid`/`crossSessionLead`, `roomCollapsible` · `internal/ui/dm_blocks.go` — `crossSessionBlock` · `internal/ui/crosssession_test.go` · `testdata/{stream,transcript}/cross-session.jsonl` |
| ⎋, and the second one | `internal/ui/escape.go` — `escape`, `clearsOnEscape` (+ `escprobe_test.go` for what two escapes in one read actually are) |
| The rewind picker: trigger, tree-aware read, receipt | `internal/ui/rewind.go` — `rewindArmable`, `RewindPicker`, `noteRewind` (the receipt fold and re-read) · `internal/core/activebranch.go` — `ActiveBranch`, the tree walk · `internal/daemon/rewindtargets.go` — `RewindTargets`, the daemon's own query behind `FrameRewindTargets` |
| The manager's config and scope | `internal/daemon/manager.go` |
| What a session thinks with | `internal/core/effort.go` (two vocabularies) · `internal/core/model.go` · `internal/daemon/effort.go` — `noteEffort`, `argvEffort` |
| What a session may spend, and what it falls back to | `internal/core/spend.go` — `ValidBudget`, `ValidFallbackModel`, and what neither of them confirms · `internal/daemon/spawnconfig.go` — `configRefusal`, the checks a spawn frame passes before a name is claimed, and `launchRefusal`, the last door before the argv for every caller |
| What a session may reach, and what it logs about itself | `internal/rpc/paths.go` — `ValidAddDir` (a path, `Frame.Dir`'s fence) · `ValidDebugFileName` (a name, `ValidWorktreeName`'s), both here for `worktree.go`'s reason: both sides check them and `internal/ui` may not import the daemon · `internal/core/debug.go` — `ValidDebugFilter`, and why a filter never reaches an argv without a file · `internal/daemon/debuglog.go` — `debugFilePath`, the directory Wake owns |
| The flags `/new` takes | `internal/ui/newflags.go` — `takeNewFlags`, one table for `--worktree`, `--add-dir` and the two debug flags, stripped before `new.go` counts a name and a directory |
| The two lists, extracted rather than typed | `scripts/extract-claude-flags.py` → `internal/core/testdata/claude-flags.json` · `testdata/stream/bare-model.jsonl` is the authority for the models |
| Names, labels, roster, reaping, locking | `internal/daemon/names.go` · `label.go` · `roster.go` · `reap*.go` · `lock*.go` |
| Rename and re-label | `internal/daemon/rename.go` · `internal/ui/rename.go` |
| A per-agent identity colour | `internal/rpc/color.go` — `ColorNames`, `NormalizeColor` (the fence both sides apply) · `internal/daemon/color.go` — `setColor`, `colorSession` · `internal/ui/color.go` — `App.colorAgent` · `internal/ui/theme.go` — `identityColors`/`identityStyle`/`identityColor` (Wake's own bolder set, not in `claude-palette.json`). Rendered by `speakerStyle` (room name-tag), `Composer.boxStyle`/`titleStyle` (the composer border and @name, set by the DM pane via `WithColor`) and `Roster.headStyle` (roster row, bold under the cursor). **The status bar does not take it** — `statusBar` recedes to `HintStyle` and `barKey` omits the colour. `@who /color` routing is the `mentionCommand` bridge in `internal/ui/slash.go`, dispatched from `sendRoom`. Survives a park via `parkedRecord.Color` |
| MCP server exposed to the manager | `internal/mcp/` — `tools.go` (six tools, held to `managerScope` in both directions), `rollup.go`, `fleet.go`, `stateguard_test.go` |
| Bubble Tea root model | `internal/ui/app.go` — start at `apply` |
| Folding one agent's event into the model | `internal/ui/observe.go` — `observe` (the room/DM fold, and where a rate-limit event is routed away), `appendEvent`; split from `app.go` |
| The rate-limit warning as a timed pop-up | `internal/ui/ratelimit.go` — `rateLimited` (a warning to the notice row, a benign `allowed` to nothing), `armRateLimitClear`/`rateLimitCleared` (the one-shot linger, `gen`-guarded against an overlapping warning clearing early) · `internal/notice/notice.go` — `ClearIf` (clears only while it is still the notice showing) |
| What a fleet report does to the model | `internal/ui/report.go` — `applyStatus`, `noteEnding` |
| The `Agent` type and the event fold that writes it | `internal/ui/fleet.go` — `Agent`, `Fleet.WithStatus`, `fold`, `withFacts` |
| Reading the fleet: the immutability copy and the accessors | `internal/ui/fleetquery.go` — `Fleet.copy` (copies `subs` too, once a missing line here left a subagent's transcript blank), `Agent`, `OnRoster`, `ByName`, `Agents`, `Focus` (split from `fleet.go` at the write/read seam) |
| The keys the App owns, and the legend bijection | `internal/ui/keys.go` |
| The drain that is not the draw loop | `internal/ui/inbox.go` — the ring, the fold that keeps a preview off a slot, and what `dropped` counts (+ `inbox_bench_test.go` for the fold's price) |
| Which conversations are on screen and where | `internal/ui/grid.go` — bounded: columns, each split once |
| Pane focus, placement, and the DM ring | `internal/ui/panes.go` |
| Frame layout, breakpoints, divider, mouse | `internal/ui/layout.go` · `appview.go` · `geometry.go` · `mouse.go` |
| Selecting text, and what a drag copies | `internal/ui/selection.go` (the value, pure) · `mouse.go` (the gesture) · `transcript.go`'s `highlighted` (the draw) · `internal/ui/composersel.go` (the same, over the query box's own draft rows) · `internal/ui/screensel.go` (the same again, over every other rendered surface as a frame-wide screen selection; `appview.go`'s `View` lays the overlay over `assembleFrame`) · `internal/ui/composerdelete.go` (⌫/delete removing the highlighted draft text, mapped back to raw runes through bubbles' own wrap) |
| A DM reader told they have drifted off the newest line | `internal/ui/followbanner.go` — `followLine` (the absolute line the banner overlays, `-1` while following), `withFollowBanner` (the overlay, applied in `DM.View` after `transcript.view`) · `internal/ui/selection.go`'s `bannerHit` (decided at press time in `mouse.go`'s `startSelection`, from the same freshly measured height `transcriptRows` uses — not re-derived at release, where the DM's own stored transcript can be stale) · `DM.JumpToLatest` in `dm.go`. The streamed preview and the working line update regardless of scroll position (`dm.go`'s `View`); the transcript deliberately does not follow a scrolled reader (`Append`'s own comment) — this is the missing signal that a reader has silently detached, not a change to either rule |
| The clipboard, in three layers | `internal/ui/clipboard.go` · `cmd/wake/output.go` — the writer Bubble Tea draws through, which **must** embed `*os.File` |
| Sending: routes, echoes, one command per draft | `internal/ui/send.go` |
| Dropping an image into the composer | `internal/ui/imagedrop.go` — `droppedImage` (the paste hijack, read at the top of `App.key`), `imageDropPaths` (shape only, no I/O), `readDroppedImages` (the off-goroutine read, sniff, base64), `imageDropped` (fold to a chip, or path back on failure) · `internal/ui/composerimage.go` — `Attach`, `Images` (only the chips still in the draft), `stripImageChips`. The wire shape is `core.ImageBlock` → `rpc.Frame.Images` → `EncodeUserMessage` (images first, text last) |
| Cards and their keys | `internal/ui/cards.go` · `cards_blocks.go` · `cardkeys.go` |
| A question ask as a wizard | `internal/ui/cardsteps.go` — the step model (`OnReview`, `optionsAt`, `firstUnanswered`) and the tab strip · `cardreview.go` — the review page and `reviewChoose`, drawn through the questions' own `optionRow` so it earns no binding · `cardanswer.go` — answer mode: the `Other…` row, the refusal's reason, and the one state in which `↵` on a draft is not a message |
| A bordered box with a label in an edge | `internal/ui/titledbox.go` — `titledEdge`, `titledBox`. Two surfaces draw one: the composer names its pane in the top edge, a card names the agent in the top and its keys in the bottom |
| The transcript behind an ask | `internal/ui/askdim.go` — `quieted`, and why the SGR pair is taken from `HintStyle` once rather than rendered per row (`askdim_bench_test.go` for both numbers) |
| What App sets on a pane for the draw | `internal/ui/panedraw.go` — `WithAsk`, `WithWriting`, `WithSelection`, `WithMenu`; set on the way to `View`, never folded into |
| Which pane draws an ask | `internal/ui/appview.go` — `cardOf` (a conversation puts its own agent's; **the room puts none**), `cardBlock`, `drawnConversations`, `focusedPane` · `internal/ui/cardhome_test.go` for the four placements |
| The room's record that a question was resolved | `internal/ui/cardroom.go` — `recordQuestionResolved` (authored above the airlock, questions only), called from `cardreview.go` (answered) and `cardanswer.go` (cancelled) · `internal/ui/chat_blocks.go` — `resolvedLine` (green for an answer, muted for a refusal) · `core.NoticeQuestionAnswered`/`NoticeQuestionCancelled` · `askroom_test.go` for both, beside the ask announcement it closes |
| What a pane pins over its composer, and how many rows are left | `internal/ui/appview.go` — `menuBlock` (the card, then the picker, then the completion menu) · `transcriptRows`, sized by the draw's own `SetSize` and read by `mouse.go`'s `startSelection` |
| The sample beside an option | `internal/ui/preview.go` — three tiers: beside, stacked, dropped |
| Which key means what | `internal/ui/keys.go` — `App.key`, held to `legendEntries` in both directions |
| Leaving takes ⌃O then ↵ | `internal/ui/detach.go` — the arm, why the confirm is a different key, and why the legend carries it |
| The way out of a Wake that has stopped answering | `cmd/wake/killswitch.go` — `killTrigger` (pure, so the one thing that can close somebody's window is testable without a terminal) · `killSwitch.pump` (the read that never waits on the consumer it exists to escape) · `emergencyExit` · `watchSignals`. Wired in `attach.go`'s `converseModel`, which is the one place a program runs |
| The legend, the armed cue it has become, and the labels an arm swaps | `internal/ui/legend.go` — `legendEntries` (the bijection's canonical list, no longer drawn on every frame), `legendArms`, `armedLabel`/`armedCueParts`/`armedCue` (the only thing drawn now, and only while an arm is live) · `internal/ui/composer.go` — `showsCue` (`View` draws the cue row and `overhead` counts it by the one predicate) · `legend_test.go` for the bijection and the cue |
| Walking back through what you typed | `internal/ui/prompts.go` — `↑↓` on an empty or single-line draft, derived from the pane's own events |
| Where Wake's keyboard collides with Claude Code's | `internal/ui/testdata/claude-keymap.json`, maintained by hand (asserted by `keymap_test.go`, which holds the eight accepted collisions and fails on a ninth) |
| `⇧⇥`, the cycle, and the label | `internal/ui/mode.go` |
| Fork · park · resume · slash · new · starts · last-read | `internal/ui/fork.go` · `park.go` · `resume.go` (`/resume` and the wake bookkeeping, split out when `slash.go` hit the 800 line max) · `slash.go` · `new.go` · `starts.go` · `lastread.go` |
| The command kinds, and the fence | `internal/ui/slash.go` — `slash` (Wake-addressed) · `configure` (session-addressed, bare) · `mentionCommand`/`roomTargetCommands` (a room mention aiming a target-command, `@who /color`) · `bareOnlyCommands` (+ `slashguard_test.go`) |
| The `/login` auth panel | `internal/ui/authapp.go` — `login`, `runAuthStatus`, `authResult`, and `panelResult` (the one Update case `/mcp` and `/login` share) · `authpanel.go` — `parseAuthStatus`, `authPanel`. Runs `claude auth status --json` through `bangRun` and hands `claude auth login` over, never running the login (no-PTY). Decodes only `loggedIn`/`authMethod`, never the account email or org (public repo) |
| The menu Wake draws | `internal/ui/picker.go` — drawn through `cards_blocks.go`'s `optionRow` · `pickerCurrent` (the value the one target is already at, marked for `/effort` and lined for `/model`) |
| The board: the fleet as one row per agent, or a tiled live wall | `internal/ui/board.go` — `/board`, an overview and never panes you *operate* (the owner's 2026-08-12 ruling, narrowed 2026-08-27 for the tile view, guardrail 2 revised 2026-09-01 to a transcript window); drawn instead of the grid, closed by any key it does not claim · `internal/ui/boardtile.go` — the tile render (each tile a live transcript window, `tileMiddle`) and grid geometry, toggled by `⇥` |
| The tiled board's per-tile transcripts | `internal/ui/boardtranscript.go` — `App.boardDMs`/`boardHistoryAsked`, `ensureBoardDMs`, `foldBoard`, `boardHistoryArrived`; one rendered DM per on-screen tile, seeded from disk (the shared `FrameHistory` wire) and fed live, dropped whole on close. Reuses `DM.transcriptWindow` (`dm.go`) so no glamour runs per frame — `board.go`'s revised guardrail 2, `docs/superpowers/specs/2026-09-01-board-tile-transcripts-design.md` |
| Inline completion, and the directory read that is not on the draw goroutine | `internal/ui/completion.go` — `completing`, `completionUp` (pane *and* draft), `completionKey` (⇥ · ⌃N/⌃P, read above `App.key`'s switch so they take no legend entry) · `completionpath.go` — `scanning`, `pathsScanned`, one directory, `pathScanMax` entries, one read at a time · `slash.go`'s `commandStem`/`wakeVerbs`, because only that file knows what a leading slash means |
| `!cmd` shell lines | `internal/ui/bang.go` · `bangout.go` · `bangapp.go` · `bangproc_unix.go` |
| Hang-up and the way back | `internal/ui/hangup.go` |
| Attention derivation | `internal/ui/attention.go` (**not** `internal/core/attention.go`, which the spec names) |
| The awareness strip: the fleet in one row | `internal/ui/awareness.go` — `awarenessStrip`, `stateLabel` (a word per state, derived from `stateGlyph`), `stripWorkspace` |
| Views and theme | `internal/ui/{chat,dm,cards,groups,roster,composer,theme}.go` — `groups.go` is the left workspaces sidebar, **hidden for now**: the code and its geometry (`Layout.ShowGroups`, `Regions`) are kept, but the app never enables it and there is no `⌃G`, until the multi-groupchat version. `⌃R` (activity/roster) is unaffected |
| What a roster row spends 24 columns on | `internal/ui/roster.go` — `headLine`'s budget: the unread badge cuts the name, and the token count is dropped whole rather than cutting it. `rowTokens` draws on a **working** row only |
| What the turn in flight has produced | `internal/core/protocol.go` — `turnTokensEvent`, off `message_delta`'s usage · `internal/ui/fleet.go` — `Agent.TurnTokens`, summed as the turn runs and cleared when it ends. **Never added to `Agent.Tokens`**, which is every *completed* turn: the result frame restates the same tokens |
| The palette | `internal/ui/theme.go` · `internal/ui/testdata/claude-palette.json`, maintained by hand (asserted by `palette_test.go`) |
| The working line, and the one ticker | `internal/ui/heartbeat.go` · `shimmer.go` · `heartbeatwords.go` · `beat.go` — start at `beat.go` for the cost argument, and for `roomWorkingLine`, the same line for a surface with many agents on it · `roomwords.go` — the room's own minimal `✻ Sailed for 49s` (`roomHeartbeatLine`) and its past-tense nautical-and-dawn pool, drawn without the DM's token clause |
| The DM's done line, once a turn finishes | `internal/ui/beat.go` — `doneLine` (`✻ Cooked for 1m 59s · done 6:48 PM`, static and dim) · `internal/ui/donewords.go` — the Wake-authored past-tense pool · `internal/ui/dmbeat.go` — `DM.heartbeat` (working line or done line), `showsDone`, `hasBeat` (the one row `baseChrome`/`SetSize` count) · `internal/ui/fleet.go` — `Agent.doneAt`/`turnDur`, captured at the working→idle edge |
| The DM's compacting line, while `/compact` runs | `internal/ui/compacting.go` — the App-owned `compacting` map (session id → start), `observeCompaction` (fold on the bracketing notices), `anyCompacting`, `compactingSince`, `pruneCompacting` (the backstop for a compaction cut short) · `internal/ui/beat.go` — `compactingLine` (`✻ Compacting conversation…`, indeterminate — the wire carries no progress figure) · `internal/ui/dmbeat.go` — `DM.heartbeat`/`hasBeat` (it wins over the done line: a compaction runs between turns, so the agent is idle) · `internal/ui/panedraw.go` — `WithCompacting` · `internal/core/vocabulary.go`/`protocol.go` — `systemNoticeFor`, which resolves the two subtype-`status` frames to `NoticeCompacting`/`NoticeCompacted` off the payload (the end keys on `compact_result`, not the boundary a failed compaction never emits) |
| An answer as it is written | `internal/core/protocol.go` — `partialEvent` · `wire.go` — `wireStreamEvent` · `internal/ui/partial.go` — start there for the cost argument, and `partial_bench_test.go` for the numbers |
| A dispatch's lifecycle, decoded | `internal/core/task.go` (Wake's vocabulary) · `protocol.go` — `taskUpdate` · `vocabulary.go` — `taskPhases`, `taskKinds`, `taskStatuses` |
| What a conversation has dispatched | `internal/ui/tasks.go` — the fold, pure, and `named` (the ending frame does not say what ended) · `taskline.go` — the line an ending leaves in the transcript. The running subagents are drawn in the **right sidebar** (`rostersubs.go`), not a list under the pane — the pane space is the task board (`checklistpin.go`) |
| Who owns that fold, and why it is not on `Agent` | `internal/ui/fleettasks.go` — `Fleet.tasks` keyed on session id, `RunningTasks` (the sidebar's filter), `named` (ingest-time enrichment for the ending line). It is a second map because `Agent` must stay comparable for `Observe`'s `now == was`. The sidebar reads it directly; nothing projects it onto the DM any more |
| Subagents in the right sidebar | `internal/ui/rostersubs.go` — `subagentRow` (the type, then the count if it fits whole), `subsOf`, `viewingPicked` (what `⌃D` and a click do with one, and where a freshly-opened DM is seeded before `Viewing` renders — see the row below). The walk is `roster.go`'s `walkable`; `Roster.SelectedTask` names the dispatch while `Selected` stays the **agent**, so `⌃C`, `⎋` and `↵` keep targeting a session |
| Where a subagent's frames are drawn | `internal/ui/dm.go` — `forwardedTo` (which transcript a frame belongs to) · `appendForwarded` · `Viewing` · `renderForwarded` |
| A dispatch's speech, for an agent nobody has opened yet | `internal/ui/fleetsubs.go` — `Fleet.foldSub`/`SubBacklog` (folded unconditionally in `Fleet.Observe`, the same move `fleettasks.go` made for the row that names a dispatch) · `DM.withSubBacklog` (seeds a DM's own `subs` from it once, only when the DM holds nothing yet for that dispatch). Without this, opening a dispatch under an agent this client never watched live drew an empty transcript — indistinguishable from the wire's own floor for a dispatch that truly forwarded nothing |
| The conversation's status bar | `internal/ui/statusbar.go` — path, branch, model, context left, **effort**, **PRs opened**, and the permission mode; cached on `DM.bar` (`barKey`), drawn per change, and drawn inside the composer below the box (above the armed cue on the rare frame one is up). **The permission mode is the bar's alone now** — the always-on legend that used to carry it is gone, so `modeFormat` lives here. Effort is the level `confirmedEffort` reads back, or the asked-for one until then; the **model** is the name `ConfirmedModel` reads back, or the init-frame id until then — the id rides both the init event and `rpc.SessionStatus.Model` (`agent.observedModel`), so a client that attached without witnessing an init still names it. `prSegment` names them `PR #29`/`PR #29, #30`. The mode is **drawn whole or dropped**, never right-cut into `permissions: …`. **A conversation/room bar too narrow for one row wraps the overflow onto a second** (`statusBar`'s `rows`, `dmBarRows`=2) rather than dropping the model and context; `chromeHeight` counts the real height (`barRows`) so the second row costs a row of transcript, not the alt screen. The **board tile keeps one row** (`tileBarRows`=1) — its tiles are fixed height |
| The PRs a session has opened | `internal/daemon/prs.go` — `recordPRs`, `prURL` (scraped from a `gh pr create` tool result, carried on `rpc.SessionStatus.PRs`, no subprocess or poll) · `internal/ui/prs.go` — `prSet`/`withPRs` (a pointer for `commandSet`'s reason, folded in `Fleet.WithStatus`) · drawn by `statusbar.go`'s `prSegment` |
| The room's info bar | `internal/ui/chat.go` — `Room.bar`/`Room.withBar` · `internal/ui/send.go` — `App.withRoomBar`, which draws it for the agent the composer is addressing (a lone `@name`, else the manager) and nothing for an empty room. Cached like a DM's; the room *banner* stays fact-free (`banner_test.go`) — this is a different row |
| The composer's info line and armed cue | `internal/ui/composer.go` — `WithBar` places a pre-rendered bar below the box (above the armed cue when one is drawn); the pane builds the bar (it reads the filesystem). `View` draws the cue row only while `showsCue` |
| The effort/model probe | `internal/daemon/probe.go` — `probeEffort`, `absorbProbe` (a `pendingProbes` counter, records both `confirmedEffort` and `confirmedModel`), `firstInit`, `incProbe`/`decProbe` · `internal/daemon/effort.go` — `noteEffort` and `noteModel` (each returns whether to re-probe), the `/model` compose · `internal/daemon/agent.go` — the `confirmedEffort`/`confirmedModel`/`pendingProbes` fields · `internal/daemon/fanout.go` — the fan-out loop that consumes the reply · `internal/core/vocabulary.go` — `IsModelReply`, `EffortFromModelReply`, `ModelFromModelReply` (asserted against `testdata/stream/bare-model.jsonl`) · `internal/daemon/history.go` — the disk filter |
| Which branch a directory is on | `internal/gitref/` — one implementation, shared by the daemon's label and the status bar |
| What a session runs as, and how full it is | `internal/core/protocol.go` — `initFacts`, `resultFacts` → `core.SessionFacts` → `ui.Agent.withFacts`. `initFacts` also carries `slash_commands`, which is what the completion menu offers |
| Markdown · diffs · tool blocks · task lists | `internal/render/` — `todo.go` for the checklist · `tool.go` owns the layout and takes its palette from the caller, so `theme.go` stays the one place a colour is written down |
| The live checklist: decode, fold, pin | `internal/core/vocabulary.go` — `toolChecklistOp` (one `TaskCreate`/`TaskUpdate` op, `TodoWrite` retired) · `internal/ui/checklist.go` — the `checklist` type (id-keyed on claude's monotonic counter, not position), `Fleet.foldChecklist` (the live working line) and `DM.foldChecklist` (the board, folded in `Append` and re-derived in `Before` so a list survives a restore off disk), plus `DM.isChecklistOp`, which decides an op is the board and never a transcript block · `internal/ui/checklistpin.go` — `checklistPin` (the board pinned above the composer, the one place the list shows), `checklistRows` (its height, counted in `chromeHeight`) and `resettleBoard` (re-wrap once on a create/delete, `withTasks`' old rule). A parent op draws nothing — `eventBlock`'s `isChecklistOp` guard — while a **subagent's** op still draws its list inline in its own forwarded transcript (`todoBlock`), because a subagent has no board of its own |
| A tool call in a conversation: the fold, the click, the settle | `internal/ui/toolblocks.go` — `toolHeadline` (**one line**, because it is the row a result rewrites in place), `bulletFor` (dim → green or red, read out of Claude Code's own bullet component), `settled` (one line rewritten, never a re-render), `openTool` (what a click toggles), `toolResultBlock` (a **successful edit's** confirmation is suppressed — the diff and the green ⏺ carry it; a failed one still shows) · `transcript.go`'s `mark`/`restyle`/`headLine` for where a call sits in the scrollback |
| A run of tool calls folded to one line | `internal/ui/rollup.go` — `rollupSummary` (the count, MCP grouped by server), `isToolUse`/`runEnd` (what a run is), `foldExempt` (why `TodoWrite`, a checklist, **and an edit's diff** stay whole out of the run), `openRun` (a click), `trailingRun`/`runKey` (the live run a new event restyles) · `internal/ui/dmtranscript.go` — `renderAll` (re-derived) and `drawFold` (incremental), the two paths held to the same run boundary · `internal/render/tool.go` — `ToolRollup` · `transcript.go`'s `runs`/`runHeads` for where a rollup sits |
| Assembling a DM's transcript from events | `internal/ui/dmtranscript.go` — the `block` type, `renderAll`, `renderForwarded`, `drawFold`; split from `dm.go`, which keeps the model and the sizing |
| Failure reporting under a TUI | `internal/notice/notice.go` |
| Markdown wrapping, and the dependency patched to make it correct | `internal/render/markdown.go` — `Markdown`, `fitToWidth` · `third_party/reflow/` + its `WAKE-PATCH.md` (why a fork, and why counting the rune is only half of it) · `internal/render/wrap_test.go` |
| Recorded stream-json fixtures | `testdata/stream/` · `testdata/transcript/` is the **on-disk** format, which is a different one. `testdata/input/` is a third kind: a line Wake would *write*, kept out of `stream/` because `TestDecodeRecordedFixtures` requires every line there to decode |
| The demo film: a scripted fleet, recorded | `demo/` — `agent/claude` (a **Python** stand-in on a shim PATH, because `argv_test.go` and `airlock_test.go` walk every non-test .go file and a Go one would need an exemption in both) · `agent/wakemcp.py`, so the manager's fan-out really goes through `wake mcp` · `tapes/*.tape` (VHS) · `setup.sh`, which **generates** the staging tape because `/new … in <dir>` resolves against the session's directory · `build.sh`. Every frame is the real binary; only what the models say is scripted |
| Taking the recording machine back out of a fixture | `scripts/scrub-fixtures.py` (`--check` is a gate) · `internal/core/corpus_test.go` is the guard it satisfies, and is tree-wide via `git ls-files` |

## Toolchain

Go 1.26+.

```bash
make build     # go build ./cmd/wake
make test      # go test ./... -race, then again without it
make cover     # coverage report; gate is 80%
make lint      # golangci-lint run
make ci        # every step the workflow runs, for when CI cannot
make soak      # 20 fake sessions replaying fixtures; SOAK_DURATION=1h for the long one
make run       # build and start
```

Dependencies: `bubbletea`, `lipgloss`, `bubbles`, `glamour` (all MIT, Charm).

**One of them is patched, and the patch is in the tree.** `third_party/reflow` is
`muesli/reflow` v0.3.0 (MIT) with one branch changed, reached through a `replace` in `go.mod`.
glamour wraps every paragraph *twice* and upstream's first pass writes a breakpoint rune (`-`)
without counting it or checking that it fits, so the second pass re-breaks the over-long line it
was handed and strands the word after the break on a line of its own - on any hyphen, which here
means `--resume`, a date or a ticket id. Nothing in glamour's API reaches it. The argument, the
measurements and what guards it are in `third_party/reflow/WAKE-PATCH.md`; the guard is
`internal/render.TestProseWrapsGreedily`, because that directory is a separate module and
`make ci` does not descend into it.

## Testing

**80% coverage minimum. TDD: write the failing test first.**

**Never test against a live LLM.** It's slow, nondeterministic, and costs money per CI run. Record
real sessions once with `--output-format stream-json`, commit the JSONL to `testdata/`, replay
forever. Any session that misbehaves in real use gets recorded and becomes a regression test.

**Record into a sterile `HOME`, because a recording is a photograph of the machine that took it.**
`system/init` is an environment dump — `tools`, `slash_commands`, `skills`, `plugins`,
`mcp_servers`, `agents`, `memory_paths`, `cwd` — and it is the *first line of every recording*. It
names every skill installed, every MCP server connected, and the absolute path of a home directory.
Nobody chooses to commit any of it; it arrives before the frame anybody wanted. This instruction
used to stop at "commit the JSONL", which is how 62 of 65 fixtures came to carry one.

So a capture runs under a throwaway `HOME` with an empty `~/.claude`, which makes the init frame
boring at birth rather than something to clean up later:

```sh
HOME=$(mktemp -d) claude --print --input-format stream-json --output-format stream-json --verbose …
```

**And a doc never pastes a raw frame** — the findings notes in `docs/superpowers/notes/` did, which
is why scrubbing `testdata/` alone would not have been enough. Cite the fixture and the line.

### The corpus: what is in `testdata/`, and why it is checked in

Three directories, and Go ignores every one of them for builds — a directory named `testdata` is
invisible to the toolchain, which is why they can hold megabytes without touching the binary.

| Where | What | Read by |
|---|---|---|
| `testdata/stream/` | Recorded `stream-json` **on stdout** — what Wake's airlock decodes live | 25 test files |
| `testdata/transcript/` | The **on-disk** JSONL Claude writes to `~/.claude/projects/…` — a *different* format, with its own keys | `DecodeTranscriptLine`, `history.go` |
| `internal/{core,ui}/testdata/` | `claude-flags.json`, `claude-palette.json` — extracted, not recorded | `palette_test.go`, flag guards |

**Why it exists at all:** the rule above. A decoder proved against what a developer *imagined* the
format was is a decoder that works until the first real session. Every trap in the CLI-surface
section below was found by recording one and is now pinned by a file here — `result` being
per-turn rather than per-process, `new_conversation_id` naming the id that *died*, a client deny
being a different shape from an interrupt. None of those are guessable, and each one cost a real
session to discover. That is what the 2.9MB buys: they are discovered once and can never regress.

**The two formats are not interchangeable**, which is the mistake the split directory exists to
prevent. `testdata/transcript/` is what a conversation is read back from when a pane opens; its
lines carry keys the stream never emits. One decoder in front of the other (`DecodeTranscriptLine`
filters into `DecodeLine`) rather than two decoders, but two corpora, because they are two wires.

| Layer | Approach |
|---|---|
| `protocol` | Golden files against recorded stream-json |
| `attention`, `router` | Pure functions, table tests |
| `session` | Fake process behind the same interface |
| `rpc`, `daemon` | Contract tests over a real socket |
| `ui` | in-process assertions, plus `internal/ui/frame_test.go` reading `App.View`'s characters |
| screen | **a real pty, the real binary, `vt10x`** — `cmd/wake/screen_unix_test.go` is the harness, 40 tests use it. Reach for this for anything about layout, keys or the mouse |
| `cmd/wake` | Fake daemon for ordering, `daemon.Serve` in-process, and `detach_unix_test.go` for the detached fork |
| soak | Build tag `soak`; goroutines, child processes, on-disk roster |

**`make test` runs the suite twice — with `-race` and without** — because the detector changes
scheduling enough to mask real ordering bugs. It has happened twice here. A green race run is not
evidence on its own.

**`make ci` may not drift from the workflow.** `internal/core/citarget_test.go` requires the same
command set in both directions; a step that genuinely cannot run off a runner goes in `ciOnlySteps`
by name with its reason.

A goroutine leak is a bug, not a warning.

### Guards worth knowing about

Several tests derive claims rather than restate them, and they fail with the correction in their own
message. Do not "fix" one by editing the number:

- `TestCLAUDEmdNamesTheTwoLargestNonTestFiles` and `TestCLAUDEmdDescribesTheLegendItDraws` read *this
  file* and hold it to the tree.
- `TestNoNonTestFileCrossesTheHardMax` — the 800-line rule, tree-wide.
- `airlock_test.go` / `argv_test.go` — the two leak boundaries above.
- Totality guards derive their domain from the **producer** (e.g. `agent.stateLocked`), not from the
  constant block, because the declared set is wider than what can arrive. A new state is a build
  failure until somebody rules on it in `parkStates`, `forkParentStates`, `forkArrivalStates`,
  `renameableStates`, `managerVerbs`.

## Claude Code CLI surface

**Verified against v2.1.232** (re-checked 2026-08-13). Re-verify before assuming a behavior change.
The recorded corpus in `testdata/stream/` was captured at 2.1.226–2.1.238; a fixture's own `init`
frame names its version.

| Need | Flag |
|---|---|
| Programmatic control | `--print --input-format stream-json --output-format stream-json` |
| Legal invocation at all | `--verbose` — without it stream-json exits 1 |
| Seeing a permission request | `--permission-prompt-tool stdio` — without it every ask is auto-denied. Undocumented; absent from `--help` |
| Identity | `--session-id <uuid>` · `--resume <uuid>` · `--fork-session` (**only** as `--resume <parent> --fork-session --session-id <new>`) |
| Display name · mode | `--name` · `--permission-mode manual\|auto\|acceptEdits\|plan\|dontAsk\|bypassPermissions` — six, of which Wake spawns `auto` and `⇧⇥` reaches four |
| Spend and failover | `--max-budget-usd <amount>` · `--fallback-model <model,model>` — **both emitted**, both documented "only works with `--print`", which is the mode every agent runs in. The chain is tried in order and the primary is re-tried at the start of each user turn |
| What a session thinks with | `--effort low\|medium\|high\|xhigh\|max` (five — the command takes seven) · `--model <alias or full id>` |
| Changing the mode after spawn | Not a flag: a `set_permission_mode` control request on stdin. The flag is the mode a session *starts* in and nothing more |
| Visibility | `--include-hook-events`, `--include-partial-messages`, `--replay-user-messages`, `--forward-subagent-text`, `--brief` — **Wake emits all five** as of 2026-08-31. `--replay-user-messages` is what puts a peer's cross-session message on the live stream (without it that message reaches only the on-disk transcript); its replays of Wake's own sends carry `isReplay` and stay dropped as `Echoed`. `--include-partial-messages` is refused without `--print` and `--output-format stream-json` |
| What a session's tools may reach | `--add-dir <directories...>` — variadic, and **both spellings are recorded as equivalent** (2026-08-16: repeated and variadic, two directories outside the session's tree, identical either way). Wake emits the **repeated** one, for a local reason: the variadic form has to ask whether a directory is the first, and `argvguard_test.go` refuses a question about a Config field's value on that path |
| Logging one agent of thirty | `--debug-file <path>`, which *implicitly enables debug mode* · `-d, --debug [filter]`, `api,hooks` or `!1p,!file`. Wake carries a **name** for the first and places the file itself — see the paths ruling above |
| Isolation | `--worktree [name]` — **read and not used.** It is a managed subsystem (paths under `.claude/worktrees`, a lock keyed on pid, persisted session state, a sweep that removes stale ones), so passing it would make claude the owner of the directory Wake's park book, discovery and groups all key on. Wake runs `git worktree add` itself and passes the path as `Dir` |
| The manager's tools | `--mcp-config <path>` — **only ever** beside `--strict-mcp-config` and `--tools ""` |
| Bounding the built-in set | `--tools <tools...>` — membership, and `""` is none. **Not** `--allowed-tools`, which bounds nothing and in `auto` does nothing at all. MCP tools pass through it whether named or not |
| The manager's scope | `--append-system-prompt` (not `--system-prompt`, which replaces) |

### Traps — do not design around the naive reading

- **`init.permissionMode` is normalized, not an echo — and the trap is one-directional.** At spawn
  it does not confirm the flag took: spawning `manual` reports `"default"`. *After* a
  `set_permission_mode` it reports the mode the session is genuinely in, so it is the right thing to
  reconcile a belief against, and it arrives on every turn.
- **`set_permission_mode`'s receipt is the authority, not the mode requested.** `manual` is accepted
  and silently becomes `default`; a refusal is a *different shape* — subtype `"error"` with a
  top-level `error` string, not a `success` carrying a failure. `bypassPermissions` is refused unless
  the process was launched `--dangerously-skip-permissions`, which nothing here passes.
- **`result` and `system/init` are per-turn, not per-process.** One process emitted seven of each.
  Treating `result` as "the agent exited" tears down live sessions.
- **`control_request`/`control_response` carry their subtype nested**, and a permission request
  carries **no `session_id`** — correlate on `request_id`. Inbound, the id and payload are nested
  twice. Reading the top level yields nothing.
- **`new_conversation_id` is not the new session id.** `conversation_reset` names the id that *died*.
  Re-key on `session_id` changing between events, not on `init`.
- **A client deny is `non_execution_kind: "permission-rule"`; `"user-rejected"` is two different
  things.** Read `subtype` first: a denial ends `success`/`completed`/`is_error: false`, an interrupt
  ends `error_during_execution`/`aborted_tools`/`is_error: true`. `permission_denials` answers "did a
  tool fail to run", not "was this denied or interrupted". **A denial is not a turn failure.**
- **Spend cannot be derived from `num_turns` or `duration_api_ms`** (`/compact` bills while reporting
  zero) — and `total_cost_usd`/`modelUsage` **reset to zero on `/clear`**, so a naive delta silently
  loses everything before the reset. Accumulate per session-id epoch.
- **`result.subtype` is not always `"success"`.** An interrupted turn has no `result` key at all.
- **An interrupted process exits 1 with empty stderr**, byte-identical to a startup rejection. The
  session remembers it sent an interrupt and `ending.go`'s `interruptedExit` suppresses exactly that
  ending — cleared by the next successful `Send`, because the excuse expires with the turn.
- **Claude's `[Request interrupted by user]` marker arrives as an ordinary `user` frame.** Text is the
  only discriminator; resolved in the airlock to `NoticeTurnInterrupted`.
- **One `can_use_tool` carries three questions.** A bare allow is right for a permission and a plan
  and *wrong* for `AskUserQuestion` — the answer rides in `updatedInput.answers`, and without it the
  model is told "the user did not answer" on a turn that still ends `success`. Resolved by
  `core.askKind` from the payload shape, never from the tool's name.
- **A question that dies because Wake closed stdin is indistinguishable from an operator deny.** If
  Wake ever renders "you denied this", it will say so about a question nobody saw.
- **An image reaches a headless session, and a *broken* one is not an error.** A user frame whose
  `content` array carries an `image` block with a base64 `source` is accepted and read - recorded
  2026-08-15, `testdata/stream/image-block.jsonl` and `testdata/input/image-block.stdin.jsonl`. Images go first and text last, because
  the prompt is derived from the last block. But an image claude cannot decode or shrink
  **degrades to a text block** - `[Image could not be processed: …]` - and the turn ends `success`
  with nothing on stdout saying so. There is no frame to detect it from.
- **A malformed stream-json input line is echoed to stderr in full, then the process exits 1.** With
  a multi-MB base64 line that is megabytes into whatever is reading stderr. `internal/core` bounds
  its *output* lines at `maxLineBytes`; nothing bounds what a rejection prints back.
- **Answered:** `/model`, `/clear`, `/compact`, `/context` survive stream-json; `/resume` does not.
- **A *bare* `/effort` or `/model` does nothing at all**, and the receipt says so three ways: one
  assistant line, `num_turns: 0`, `$0`. Handled by the CLI without a model turn — which is what
  makes them forms Wake may claim. Recorded in `testdata/stream/bare-{effort,model}.jsonl`.
- **`stream_event` is recorded as of 2026-08-21** — `testdata/stream/partial-turn.jsonl`, one
  streamed turn against 2.1.238, closing what was the airlock's only unrecorded inbound shape. The
  envelope is `{type:"stream_event", event, parent_tool_use_id, uuid, session_id, ttft_ms?}` with
  `ttft_ms` on the `message_start` alone; a text delta is `event.type=="content_block_delta" &&
  event.delta.type=="text_delta"` → `event.delta.text`, and **the completed `assistant` frame
  arrives byte-identical to its deltas** — the claim the whole preview design rests on, now bytes.
  `partialEvent` still yields **no event** for every shape it does not recognise (`thinking_delta`
  and `signature_delta` stream beside the text, correctly dropped), so a moved schema costs the
  preview and never the transcript. Findings and provenance caveats:
  `docs/superpowers/notes/2026-08-21-partial-messages-findings.md`.
- **`--debug` alone does nothing observable in the mode every agent runs in.** Recorded 2026-08-16
  against 2.1.233 — `docs/superpowers/notes/2026-08-16-spawn-flag-findings.md` §1 has the probe:
  under `--print --output-format stream-json` it exits 0 with **zero bytes on stderr** and a stdout
  the length of the same spawn without the flag, while `--debug-file` on the same session wrote
  17KB. So a filter with no file is not a weaker log — it is logging somebody turned on and no log
  anywhere, which is why Wake refuses the pair rather than emitting half of it. **Whether
  `--debug-file` creates its own parent directories is not recorded**; `daemon/debuglog.go` makes
  them rather than finding out.
- **The checklist is `TaskCreate`/`TaskUpdate`, and `TodoWrite` is retired.** Recorded 2026-08-22
  against 2.1.240 — `testdata/stream/task-checklist.jsonl`, `2026-08-22-task-checklist-findings.md`.
  `TodoWrite` is enabled only when `CLAUDE_CODE_ENABLE_TASKS` is not explicitly `false`,
  so the tool is off by default and no fixture calls it. The live checklist is built across a run of
  `TaskCreate {subject, description, activeForm}` and `TaskUpdate {taskId, status, subject?,
  activeForm?}` calls — a **different subsystem** from the `task_*` system frames the dispatch list
  draws — where `TodoWrite` sent the whole list each call. The id is a **monotonic per-session
  counter** ("1","2","3", reported only in the create's `tool_result` text), not a position — a
  delete does not renumber the survivors — so the fold keys on it, never on slice index. `status` is
  `pending|in_progress|completed`, with a fourth `deleted` in the bundle schema and no recording.
  `core.toolChecklistOp` decodes one op; the `ui.checklist` type accumulates it, folded on the Fleet
  (the live working line) and on the DM (the transcript, so it survives a restore off disk).
- **`--effort` takes five levels and `/effort` takes seven** (`ultracode`, `auto` on top). Two
  surfaces, two constants: `core.EffortLevels` for the argv, `core.EffortCommands` for the text.
  `daemon.argvEffort` is the one place a level is narrowed for a command line.
- **The bare `/model` reply is the only thing that enumerates the models** — the `init` frame names
  the one in use, `--help` gives an `e.g.` — and it also reports the session's **effort**, which is
  the one known way to read a level back. Wake does not ask; see `deferred.md`.
  `/clear` changes the session id.

Recordings and verbatim frames: `docs/superpowers/notes/2026-08-08-stream-json-findings.md`,
`2026-08-08-interrupt-findings.md`, `2026-08-09-interrupt-permission-findings.md`,
`2026-08-09-question-findings.md`, `2026-08-09-resume-fork-findings.md`,
`2026-08-10-live-fork-findings.md`, `2026-08-12-*`, `2026-08-15-image-input-findings.md`,
`2026-08-16-spawn-flag-findings.md`.

## Conventions

**Surgical code, brief comments, nothing extra.** Owner's rule, 2026-08-12, and it binds every change:

- **Write the smallest change that does the job.** No scaffolding for a future caller, no options
  nobody passes, no helper with one call site that reads fine inline.
- **Comments are brief.** One or two lines on what is not obvious — usually *why*, not *what*. The
  long essays in this tree are history, not a template. A comment restating the code is deleted.
- **Nothing parallel.** One implementation of a thing. Find the existing code before writing new code.
- **No unnecessary code.** Dead branches, unused fields, unreachable guards and speculative
  abstractions are defects, not slack. A guard's domain must be what can *arrive*, not what the type
  declares.
- **Immutable by default.** Return new values; don't mutate in place. Especially in `attention` and
  `router`, which must stay pure.
- **Small files: 200–400 typical, 800 hard max.** The two largest non-test files are
  `internal/rpc/wire.go` at 798 and `internal/core/protocol.go` at 797 — that sentence is derived by
  `TestCLAUDEmdNamesTheTwoLargestNonTestFiles`, so a stale count fails with the correction in its own
  message. Split by subject, never by line count.
- **Functions under 50 lines. Nesting under 4 levels.**
- **Handle every error explicitly.** Never silently swallow. A malformed JSON line logs and skips — it
  never crashes the render loop. Under a TUI, failures go to `internal/notice`, never to stderr.
- **No hardcoded values.** Config or constants.
- **Reference code by symbol, not line number.**
- **A number nothing asserts is wrong by default.** This has been broken five times in this file
  alone. Derive it or delete it.

## Running Wake from this working tree

**Never run `wake` — any verb — from this repository without `WAKE_SOCKET` set.** The default socket
is `~/.wake/daemon.sock`, which is the *real* fleet: the owner's own sessions doing their own work.

`make` targets are safe — the Makefile exports a scratch `WAKE_SOCKET`. A bare `go run ./cmd/wake …`
or `./bin/wake …` is **not**, and neither is `wake` on your `PATH`.

```sh
WAKE_SOCKET=$(mktemp -d)/wake.sock go run ./cmd/wake status
```

**`wake stop` is the only irreversible verb in this project.** Nothing brings a *stopped* session
back — that is the whole reason park exists. This rule is written down because an agent cleaning up a
leaked test daemon ran `go run ./cmd/wake stop` with no `WAKE_SOCKET`, stopped the owner's fleet of
three, and reported afterwards that it had verified the real daemon untouched. It had not looked. Two
of those transcripts were not on disk to recover.

**Look before a destructive verb, in the same command.** `wake status` first, and read it. A daemon
you did not start is a daemon somebody is using.

## Git

**A feature goes on a branch and gets a PR. Always, without being asked.** Owner's rule,
2026-08-12, after nine `feat:` commits reached `main` directly in one session — each green, each
pushed, none of them reviewable as a unit. *Branch, commit, PR, merge freely* has never meant push
to `main`; the freedom is in not needing permission to branch.

**For a bug fix, ask where it goes** if the owner has not already said. Do not assume `main`.

Create the branch at the first edit rather than at the end, and when the work is green say plainly
that it is a PR awaiting a merge rather than letting "pushed" stand in for "landed".

**Two reviews before the PR is opened, not after.** Owner's rule, 2026-08-16. `make ci` proves the
tree still works; it cannot tell you the work is *right*, and with Actions unfunded there is no
second reader downstream — the PR is opened into a repository where nothing else will look at it.
So both passes run first, and their findings go in the PR body beside the exit code:

1. **A code review** — the `code-reviewer` agent, or an equivalent read of the diff for
   correctness, the non-negotiables above, and the conventions below. Its job is *this diff is
   sound*.
2. **An adversarial review** — a separate pass whose job is the opposite: **try to break the
   claim.** Not "does this look fine" but "what does this diff assert, and what would make that
   assertion false?" Run it against the strongest claims in the change — the derived numbers, the
   guards that say a thing cannot happen, the tests that pass. A test passing is not evidence the
   test can fail; check that it ever went red.

They are two passes because they fail differently: a code review reads what is there, and an
adversarial review asks what is missing or overstated. This project has been wrong the second way
far more often — five hardcoded numbers in `CLAUDE.md` alone, a legend that named keys nothing
bound, guards that passed while the keys they guarded did nothing on macOS.

**Say which passes ran.** A PR that skipped one says so, in the body. Silence reads as done.

**GitHub Actions is out of funds, so `make ci` on this machine is the only gate there is.** Runs
have failed on billing before starting a job since 2026-08-12. Nothing checks a PR after it is
opened, and a red X nobody is watching for cannot appear — so a branch that was never gated locally
is a branch merged on somebody's word.

Which makes the rule the reverse of the usual one: **run `make ci` and read its exit code before
opening the PR, not after.** Not `go test ./...`, which skips the lint, the coverage floors, the
cross-compile and the second non-race pass — `make ci` is the whole of what the workflow would have
done, and it takes about six minutes. Say the exit code in the PR, because it is the only evidence
anybody gets.

It cannot replicate a clean checkout or a second machine, and it never could; that gap is the same
one it has always had and is not what this rule is about.

Side project — branch, commit, PR, merge freely.

Conventional commits: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`, `perf:`, `ci:`.

**A branch takes the same type as a prefix:** `<type>/<short-kebab-description>` — the commit type
above, a slash, then what the branch is (`feat/conversation-rewind`, `fix/commands-on-report`,
`refactor/…`, `docs/…`). It is the type verbatim, not a synonym — `fix/`, never `bugfix/` — and a
slash rather than a colon, because a colon is not a legal git ref character.

**A development worktree lives *inside* the repo, under `.worktrees/<name>`, never in `$HOME`,
`~/Documents`, `/tmp`, or beside the repo.** Owner's rule, 2026-08-29, after worktrees for this
project had scattered across the home directory, `~/Documents` and the project root — impossible to
find, and clutter nobody could tell stale from live. `.gitignore` already reserves `/.worktrees/`
for exactly this ("Parallel subagent worktrees"), so a worktree there is organized *and* uncommittable
by construction; put every `git worktree add` for working on Wake under it (`git worktree add
.worktrees/<name>`). This is **not** Wake's own `--worktree` feature, which is a product path
(`<repo>/.wake/worktrees/<name>`, `internal/daemon/worktree.go`) and unchanged. Remove a dev worktree
with `git worktree remove` when its branch has merged; the branch survives the removal, so nothing is
lost.

**Never add Claude attribution** — no `Co-Authored-By`, no generated-with footer, in commits, PR
titles, or PR bodies.

## Working notes

- `docs/goals.md` — what was asked for, the four phases, and every original ask traced to built or
  not-built.
- `docs/live-testing.md` — what only a human at a real terminal can check. `go test` has no TTY, no
  font, no mouse, no window manager, and no `claude` binary it will spend money on. **Anything there
  that turns out to be testable is a bug in the file** — move it into the suite.
- `docs/notes/deferred.md` — everything consciously put off, triaged by what it blocks. **Read it
  before starting a task**; several entries are addressed to a specific one.
- `docs/notes/decisions.md` — rulings not obvious from the code, plus recurring failure modes worth
  recognising on sight.
- `docs/notes/bugs.md` — defects somebody **watched go wrong** running the build, as against work put
  off (`deferred.md`). Each entry separates the symptom from
  the rulings it collided with, because most of these are a decision meeting a case it did not
  anticipate rather than a mechanism that failed.

### Notes are shared through `origin/main`, not through a branch

Work happens in several worktrees at once, and a note written in one is invisible to every other
until it lands. That is the wrong latency for a note — the whole value of `decisions.md` is that
the *next* agent reads it, and the next agent is usually running right now in a different
directory.

So **a docs-only commit goes straight to `main` and is pushed immediately.** It is not a feature,
it does not need the branch-and-PR rule, and holding one on a feature branch for review is holding
it away from the readers it was written for. Mixed commits still branch: the rule is about
docs-only ones.

**To read what another worktree has written, ask the remote rather than the filesystem** — no
`cd` to the main checkout, no merge, nothing to clean up:

```sh
git fetch origin main --quiet
git show origin/main:docs/notes/decisions.md
```

## Scope discipline

The v1 boundary is §17 of the spec. Before adding anything not on the "in" list, check §2 (non-goals)
and §17 (out). The failure mode for this project is drifting toward being a worse cmux. **The group
chat is the product; the panes are substrate.**
