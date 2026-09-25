# CLAUDE.md — Wake

**Status: Phases 1–3 partially complete.** Core, daemon, room, DM, park/wake, fork, and the
manager work end to end. Session importing and pool management remain; Phase 4 is next — see
`docs/goals.md`.

The design lives in `docs/superpowers/specs/2026-08-08-wake-design.md`. **The spec is the source of
truth for *what* Wake does; this file is the source of truth for *how we build it*.** When they
disagree, the spec wins and this file gets fixed.

This file is the operating manual, not the archive. Every ruling's full argument lives in the named
file's header or in `docs/notes/decisions.md`; read those before changing a rule below.

## Public repository hygiene

Wake is **public** and sits beside Anthropic's own product. Non-negotiable:

- **Never reproduce Claude Code's binary.** No `strings` on it, no pasted minified source, no byte
  offsets, no "read out of the binary". Document **behaviour**. A value that matches Claude Code is
  "matched against Claude Code, maintained by hand".
- **Never paste a raw Claude frame into a doc or comment.** `init` is an environment dump (skills,
  plugins, paths, home dir). Cite a fixture and a line.
- **Recordings:** capture into a sterile `HOME`, then run `scripts/scrub-fixtures.py`.
  `internal/core/corpus_test.go` fails CI on home-shaped paths, machine env keys, or unknown
  `slash_commands`. Fix a failure with the scrubber, never by editing the guard or allowlist.
- **Attribution:** Wake is not affiliated with Anthropic (`README.md`, `NOTICE`). No Anthropic logo,
  no Clawd, no `claude-*` project name.

**Gate:** `make ci` exit 0 is the only gate. Run it from a checkout **under your home directory** —
the screen tests fail under `/tmp` or very long paths. **Releasing is manual** (`goreleaser release
--clean`, on the owner's command) — see `docs/RELEASING.md`.

## Project overview

A terminal app for developers running 15–30 Claude Code sessions at once. The fleet is a room: a
filtered group chat as the primary surface, `@name` routing, a manager session, an attention-ranked
roster. Any agent opens as a full 1:1 DM at Claude Code fidelity.

An agent is a headless `claude` in stream-json mode with a Wake-assigned session UUID. **Wake never
screen-scrapes** — all state comes from structured JSON on stdout.

### Surfaces (details in the named files)

- **Verbs** (`cmd/wake/main.go`): bare `wake` starts a new named fleet and opens the room (spawns one
  agent as a roster row if the machine has nothing); `wake --fleet <name>` returns to one
  (`default` = the unnamed fleet at `~/.wake`); `new`, `fork`, `import`, `attach`, `status`, `stop`
  (irreversible), `fleets`, `manager`, `setup-terminal`. `$WAKE_SOCKET` wins; naming a fleet beside
  it is refused. Each fleet is a directory under `~/.wake/fleets/`; per-fleet files are
  `filepath.Dir(socket)` plus a name.
- **Spawn flags** (`new`, `manager`, `/new`): `--effort`, `--model`, `--max-budget-usd`,
  `--fallback-model` (both survive a park), `--worktree <name>` (Wake runs `git worktree add`; never
  passes claude's `--worktree`), `--add-dir` (repeatable), `--debug-file <name>` / `--debug`
  (the daemon owns the directory; `--debug` without a file is refused).
- **Room keys:** `↵` send/open/confirm · `esc` interrupt · `esc esc` clear draft, or idle+empty →
  rewind picker · `↑↓` prompt history (or cursor on a multi-line draft) · `⇧↑↓` pick agent · `⌃O`
  arm detach (`↵` confirms, `⌃O` cancels) · `⌃C` park focused · `⌃Q` arm park-all & quit (second
  `⌃Q` confirms) · **`⌃C⌃C` emergency quit** (read off the tty before Bubble Tea) · `⇥` focus ·
  `⇧⇥` permission mode · `⌃X` next blocked · `⇧←→` move between drawn panes · `⌥↵`/`⌃J` newline ·
  `⌃F` fork · `⌃D` open here · `⌃Y` new column · `⌃B` open below · `⌃W` close pane · `⌃A` toggle the
  lone-`@name` narrowing · `⌃E` expand folded results / card descriptions.
- **Wake's slash commands** (`internal/ui/slash.go`): `/resume`, `/new`, `/name`, `/task`, `/color`,
  `/team`, `/quit`, `/adopt`, `/mcp`, `/login`, `/reauth`, `/manager`, `/manager-stop`, `/board`,
  `/groupchat-filter`, plus bare `/effort`/`/model` menus. Everything else passes to the agent byte
  for byte. `@who /cmd` in the room aims a target-command at that agent. A spaced `/team` or
  `/name` argument is hyphenated.
- **`/mcp`** draws Claude Code's own MCP menu for one agent, live from the running session
  (`mcp_status`/`mcp_reconnect`/`mcp_toggle`, replies sent only to the asking window). Authenticate
  hands the real terminal to `claude mcp login <server>`, then reconnects every live agent stuck on
  that server. claude.ai connectors aren't listed — headless sessions don't load them.
  `internal/ui/mcpmenu.go`, `mcpauth.go`.
- **Manager:** started by default by every verb that opens the room. `/manager` toggles
  (absent→spawn, parked→wake, running→park); `/manager-stop` ends it.
- **Rendering:** folded tool runs (`⌃E`/click opens), `Edit` diffs drawn whole, task board pinned
  above the composer, running subagents in the right sidebar, streamed preview tail, DM done line
  (`✻ Cooked for 1m 59s · done 6:48 PM`), compacting line, loop line, question cards as a wizard
  with a review step, drag-to-select-and-copy on every surface.

## Non-negotiables

Violating one is a design regression, not a style nit.

| Rule | Why |
|---|---|
| **Not a terminal emulator or multiplexer.** No PTY, no VT100, no browser panes, no arbitrary shells. | Chasing it is how this project dies at 40%. |
| **Cheap to leave open.** No per-frame work that could be per-change, no poll where a wait will do, no process on a timer. | A per-agent cost multiplies by 30. |
| **Only `internal/core`'s four airlock files know Claude's JSON** — `protocol.go`, `wire.go`, `vocabulary.go`, `encode.go`. | Stays Codex-ready. Enforced by `airlock_test.go`, which also pins the file set. |
| **Claude's CLI identity flags are spelled only in `internal/core/argv.go`** — `--session-id`, `--resume`, `--fork-session`, `--continue`. Use `core.SessionArgvMarkers`. | Enforced by `argv_test.go` tree-wide. |
| **`attention.go` stays a pure function.** | Hardest logic; testable without spawning. |
| **The UI never touches an agent's process.** | Keeps the daemon boundary real. |
| **Wake owns almost no state.** Transcripts are Claude's (`~/.claude/projects/…`); Wake reads them back (`internal/daemon/history.go`). Wake stores roster, park book, groups, layout. The park book holds id, directory, name, label, parked-at — never a PID or ParentID. | Wake can crash and lose nothing. |
| **Never copy cmux source.** CLI only. | cmux is GPL-3.0-or-later. |
| **No parallel implementations.** Extend in place, or delete and replace. | Grep first; name what you extend or remove. |

## Load-bearing design rules

One line each; the full argument is in the named file or `docs/notes/decisions.md`.

**Identity and lifecycle**
- A session's name comes from the daemon's 64-name pool. **A name is never an address** — frames
  carry `SessionID`; names are released and reissued, so a rename has no alias.
- **Park is recoverable; stop is not.** `⌃C` parks one, `⌃Q⌃Q` parks all and exits, `wake stop` ends
  everything and clears the park book (`parked.json` beside the socket).
- `⌃Q` waits for the daemon to confirm the park (a `FrameStatus` written behind `FrameParkAll`)
  before closing — `internal/ui/park.go`.
- **A daemon restores nothing.** ⌃Q then `wake` is a fresh room; parked sessions are addressable only
  via `/resume` (`rpc.Status.Parked`, disjoint from `Sessions`). `admit` still refuses a spawn under a
  parked id. `⌃C` refuses a blocked agent (closing stdin reads as an operator deny).
- **Two live processes on one session id branch silently.** Every check happens before the second
  process exists: `resumeSafe` asks the OS; `launch` takes the row before starting anything.
- **Forks and imports are snapshots** (`--resume <parent> --fork-session --session-id <new>`).
  Import is a fork, the only guaranteed-safe primitive for a hand-started `claude`.
- **`/resume` resumes in place and skips `resumeSafe`** — the one deliberate exception, matching
  Claude Code (owner's 2026-09-20 ruling). Parked rows keep `resumeSafe`. `internal/daemon/resume.go`.
- **Anything waiting on a spawn waits on the id it minted**, never the parent's.
- `/quit` stops one agent (`FrameStop`) and drops its row **per window** once the ending is
  confirmed; refuses the manager (use `/manager-stop`) and parked agents. `internal/ui/quit.go`.
- `/manager-stop` refuses a parked manager and a missing one; it does not borrow park's
  blocked-agent refusal (a stop has no wake).
- `/reauth` parks sessions marked by a 401 (upstream bug #48786, shared-OAuth refresh race) so
  `/resume` brings them back on a fresh login. Wake never runs `claude auth login`.
  `internal/ui/apierror.go`, `reauth.go`.

**Keys and the legend**
- **The legend is drawn only while an arm is live, and then it is only the armed cue:**
  `↵ detach` / `⌃O cancel`, `esc clear draft`, `esc rewind`, `⌃Q park all & quit`. `legendEntries`
  is still the canonical list and must be a bijection with `App.key`'s cases
  (`TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed`); this paragraph is held to the
  renderer by `TestCLAUDEmdDescribesTheLegendItDraws`. `Composer.showsCue` is the one predicate both
  sizing and drawing use.
- **The permission mode moves on the receipt, never the keystroke.** `⇧⇥` cycles
  `default → acceptEdits → plan → auto` (Claude Code's own order,
  `internal/ui/testdata/claude-mode-cycle.json`); `init` corrects stale beliefs; a mode does not
  survive a park. `internal/ui` never spells a mode word — use core's constants. `internal/ui/mode.go`.
- **Grid keys are single bytes (`⌃Y`, `⌃B`)** because `⇧↵`/`⌃⇧↵` are unnamed by bubbletea and macOS
  eats every `⌃`/`⌃⇧`+arrow (`TestNoKeyIsACtrlArrow`, `keyprobe_test.go`). `⇧←→` move among drawn
  panes only; vertical pane movement has no key.
- **Plain `↑↓`** recall prompt history unless the cursor can move within a multi-line draft —
  decided by simulating bubbles' move on a copy (`Composer.CanCursorUp/Down`), not by counting rows.
  The roster is `⇧↑↓`. `internal/ui/keys.go`, `composer.go`.
- **Claude Code's keymap is kept by hand** in `internal/ui/testdata/claude-keymap.json`;
  `keymap_test.go` fails on any unruled collision. Only ⌃O is destructive, hence armed.
- **Detach is armed by ⌃O, confirmed by ↵, cancelled by ⌃O**, and the cue stays drawn while armed.
  Only key/mouse input disarms (a frame arriving must not flip ↵ to *send*). `internal/ui/detach.go`.
- **`⎋⎋` clears a draft; two fast escapes arrive as one `alt+esc`** and are handled as both halves
  (`escprobe_test.go`). Idle+empty → rewind picker, gated fresh on every `esc`. `internal/ui/escape.go`.
- **The emergency exit is `⌃C⌃C`, read off the tty before Bubble Tea** (`cmd/wake/killswitch.go`),
  because a wedged renderer swallows SIGINT/SIGTERM. ⌃Q is deliberately not watched (it collided with
  the armed park). **It pauses for a terminal hand-over** (`cmd/wake/handover.go`): while
  `claude mcp login` owns the tty, the pump stops, cooked mode is restored and signals are muted —
  all or nothing. Handing the terminal over (like `git commit` to an editor) is not a PTY.
- **A pane that holds the keys is always drawn**; every focus change goes through `App.refocus`.

**Rewind**
- `esc esc` idle sends `rewind_conversation` (a `control_request`) with `target_message_uuid` and
  the mandatory `last_seen_user_message_uuid`. `session_id` never changes.
- The on-disk transcript is an append-only tree; `core.ActiveBranch` walks `parentUuid` from the live
  leaf. History, room restore and `RewindTargets` share that one reconstruction. On `rewound:true`
  the pane re-reads itself (`noteRewind`) — the only mechanism. The manager is refused both frames.

**Layout, mouse, selection**
- **The grid is bounded:** columns, each split once (spec §8). The room is `Cols[0]` and cannot be
  closed. Arbitrary tiling is out of scope.
- Dividers store fractions; widths allocate on a running total so a drag stays local. Width drags go
  through the 80ms settle; row drags don't. The wheel scrolls the pane under the pointer.
- **Drag selects, release copies**, on every surface: transcript (anchored to `transcript.lines`
  indices), query box (`composersel.go`), everything else as a frame-wide screen selection
  (`screensel.go`). Every keystroke clears the highlight *and* does its job; width change clears,
  height doesn't; a click copies nothing. Roster click targets are resolved at press.
- **Double-click selects a word, triple-click its row**, on any selectable surface; the first click
  still does its own job. A timer (`multiClickWindow`) counts clicks but never tells a click from a
  drag. `internal/ui/multiclick.go`.
- Clipboard: `pbcopy` → `tmux load-buffer` → OSC 52, through the writer Bubble Tea draws through,
  which **must embed `*os.File`** or colour silently disappears (`cmd/wake/output.go`).
- `App.transcriptRows` measures the pane as drawn (cards, menus and previews move chrome without a
  resize); `startSelection` fences on it.
- **Only a width change returns a reader to the newest line.** Last-read markers anchor to events.

**Rendering and cost**
- **The socket is drained by a goroutine that does not draw** (`internal/ui/inbox.go`); geometry goes
  through one 80ms settle (`geometry.go`).
- **A streamed preview never costs the record a slot**: partials fold into one slot and never evict,
  and a dropped partial is not a gap (`inbox.go`, daemon `client.go`'s `partialCeiling`).
- **A preview is never a record**: plain-text tail, bounded by the pane, never through glamour,
  accumulated only for panes on screen (`App.wants`), dropped on leave. No preview in the room or for
  subagents. `internal/ui/partial.go`.
- The composer grows with the draft; the pane bounds it (`composerRowsIn`), never itself. A pane's
  chrome height is re-checked in `View` (`DM.chrome`) — a frame one row too tall scrolls the alt
  screen.
- **The room's working line is one row**: oldest running turn, `+N more working`
  (`roomWorkingLine`, `roomwords.go`).
- **The DM's done line** is captured at the working→idle edge (`Fleet.WithStatus`), only for turns
  this client watched start; forgotten on park/end/gap, on new agent content (`notDone`), and hidden
  while a subagent runs (`subRunning`). `DM.hasBeat` is the one row predicate.
- **Compacting line** (`compacting.go`): indeterminate bar (the wire has no progress figure); end
  keys on `compact_result`, not the boundary. The `compact_boundary` metadata draws
  `✻ Compacted · A → B tokens · …`, live-only.

**Room and routing**
- **The room re-derives its history from claude's transcripts** (`FrameRoomHistory`,
  `roomhistory.go`). `core.Event.At` is set only by `DecodeTranscriptLine`; a batch is dropped whole
  if its session spoke since the ask; a typed turn returns only when two transcripts prove it was a
  broadcast; agent prose is restored only inside a public turn.
- A routed message is echoed into the room and into every *held* DM it reached, mention included,
  marked `FromRoom`.
- **A lone `@name` narrows the room** to that thread (`roomfocus.go`); `⌃A` overrides per target;
  `/groupchat-filter off` flips the default per window (`roomfilter.go`).
- **Peer cross-session messages** show in the room as `↪ sender → recipient`
  (`--replay-user-messages`; `core.KindCrossSession`). The envelope is recognised only on *string*
  content, so pasted text can't forge one. DM replays of Wake's own sends are dropped.
- **Slash commands resolve against a closed set Wake owns; anything else is text.** A word claude
  advertises may be claimed only in a form claude is recorded doing nothing with
  (`bareOnlyCommands` names the fixture). `/color` is the owner-claimed exception
  (`ownerClaimedCommands`). Only `slash.go` decides what a leading slash means.
- **Open mention mode widens a message, never a command**: `@john /anything` always goes to john
  alone (`mention.go`, `leadingCommand`).
- **Completion offers, never routes, never takes `↵`.** Session commands/skills first, then Wake's,
  agents, teams (live members only), paths. It belongs to a cursor and a pane; directory reads run off
  the draw goroutine (`completion.go`, `completionpath.go`). It must mirror `core.Resolve`.

**Cards and asks**
- **An ask belongs to its agent's conversation; the room draws none** (`Cards.For`, `App.cardOf`).
  The roster row, the strip's `N need you` and `⌃X` announce it. Nothing on that wire times out.
- Permissions/plans: rune then `↵`, read only on an empty composer; any other input disarms (key,
  composer, mouse, digit).
- Questions: a wizard with a review step (`cardsteps.go`, `cardreview.go`); `Other…`/`d` enter answer
  mode, where the card takes only `↵` (`cardanswer.go`). Arrows reach the card with or without a
  draft; `←→` only on an empty composer.
- A `Picker` is not a `Card` — `Cards.Reconcile` would delete it on the next report.

**Manager**
- **May send, interrupt, spawn (optionally named, under `daemon.liveCap`, into a directory the fleet
  already occupies), and group (`set_team`, `set_color`)** — nothing else. Rename, label, park,
  wake, fork, import, stop, allow/deny, mode and the four MCP frames are refused, each argued in
  `cmd/wake/mcpguard_test.go`. All tool output goes through `mcp.oneLine`.
- Its config is a function of its name, applied in `launch`: `--mcp-config` only ever beside
  `--strict-mcp-config` and `--tools ""` (not `--allowed-tools`, which bounds nothing).
- The daemon socket has no caller auth; `managerVerbs` bounds the manager's tool surface, not what
  the daemon accepts.

**Directories and paths**
- `a.cwd` (from `init.cwd`, absolute or refused) is where a session *runs*; `a.dir` is where it was
  *started* and never moves — park, wake and fork use `a.dir` because claude locates transcripts by
  the start directory. The manager's spawn bound uses `Dir`.
- Wake creates worktrees at `<repo>/.wake/worktrees/<name>` on `wake/<name>`, never removes one, and
  refuses the spawn if `git worktree add` fails.
- `--add-dir` gets `Frame.Dir`'s fence (absolute or refused). `--debug-file` is a **name**
  (`rpc.ValidDebugFileName`); the daemon places it. Checked at `configRefusal` and `launchRefusal`.
  Neither survives a park (`/add-dir` doesn't exist at runtime).
- **Discovery verifies a directory, never decodes one**: `slugOf` appears only as an operand of
  `==`/`!=`. Transcripts are read whole (cwd can sit megabytes in), fanned across workers.
- **`wake stop` never claims more than it can see**: it waits for the socket unlink, then asks
  `daemon.Status`.

**Effort and model confirmation**
- The daemon sends a bare `/model` probe on `init` and after `/effort`/`/model` changes; its reply
  names the effort and model (`core.EffortFromModelReply`, `ModelFromModelReply`). The reply is
  suppressed at `fanOut` (`absorbProbe`) and filtered from restored history. The status bar prefers
  confirmed values. `internal/daemon/probe.go`, `effort.go`.

## Key locations

Update this table in the same commit that creates a path. **A row naming a path that does not exist
yet says so in bold.**

| What | Where |
|---|---|
| Entrypoint, verbs | `cmd/wake/main.go` · bare `wake`: `openroom.go` · attach/detach: `attach.go` · `match.go` · `fork.go` · `import.go` · `status.go` · `stop.go` · `manager.go` · `mcp.go` · `ensuremanager.go` · `setupterminal.go` · `termsetupprompt.go` · `internal/termsetup/` |
| Emergency exit, terminal hand-over | `cmd/wake/killswitch.go` · `handover.go` |
| Claude JSON airlock | `internal/core/protocol.go` · `wire.go` · `vocabulary.go` · `encode.go` |
| One agent | `internal/core/session.go` · write path `write.go` · argv `argv.go` · ending `ending.go` · process `process.go` |
| Live-cap scheduler | **NOT BUILT** — `internal/core/pool.go` is planned |
| Routing | `internal/core/router.go` |
| Transport | `internal/rpc/wire.go` · `lifecycle.go` · fences: `worktree.go`, `paths.go`, `color.go`, `team.go` |
| Daemon | `internal/daemon/daemon.go` · `server.go` · `agent.go` · `agentask.go` · `apply.go` · `spawn.go` · `fanout.go` · `launcher.go` · `mayspawn.go` · `worktree.go` · `park.go`/`parkbook.go` · `resume.go` · `discover.go` · `history.go` · `rewindtargets.go` · `manager.go` · `probe.go`/`effort.go` · `prs.go` · `loop.go` · `askreplay.go` · `taskreplay.go` · `subagenttrack.go` · `names.go`, `rename.go`, `color.go`, `team.go` |
| MCP server for the manager | `internal/mcp/` — `tools.go`, `sendteam.go`, `grouping.go` · verdicts in `cmd/wake/mcpguard_test.go` |
| Bubble Tea root | `internal/ui/app.go` (start at `apply`) · `observe.go` · `report.go` · `keys.go` · `appview.go` · `panedraw.go` |
| Fleet model | `internal/ui/fleet.go` · `fleetquery.go` · `fleettasks.go` · `fleetsubs.go` · `sections.go` |
| Input drain, geometry | `internal/ui/inbox.go` · `geometry.go` · `layout.go` · `grid.go` · `panes.go` |
| Mouse, selection, clipboard | `internal/ui/mouse.go` · `selection.go` · `composersel.go` · `screensel.go` · `multiclick.go` · `composercursor.go` · `composerdelete.go` · `clipboard.go` · `cmd/wake/output.go` |
| `/mcp` menu | `internal/core/mcpcontrol.go` · `mcpask.go` · `encode.go`'s `EncodeMCP*` · `internal/rpc/mcp.go` · `internal/daemon/mcpask.go` · `internal/ui/mcpmenu.go` · `mcpmenuview.go` · `mcpauth.go` · `cmd/wake/handover.go` · `testdata/stream/mcp-control.jsonl` |
| Sending | `internal/ui/send.go` · `queue.go` (type-ahead) · `mention.go` · `imagedrop.go` |
| Slash commands | `internal/ui/slash.go` · `new.go`/`newflags.go` · `resume.go`/`resumepicker.go` · `quit.go` · `service.go` · `adopt.go` · `color.go` · `team.go` · `board.go` · `authapp.go` · `reauth.go` · `picker.go` |
| Legend, arms, escape, rewind | `internal/ui/legend.go` · `detach.go` · `escape.go` · `rewind.go` · `prompts.go` · `mode.go` |
| Cards | `internal/ui/cards.go` · `cards_blocks.go` · `cardkeys.go` · `cardsteps.go` · `cardreview.go` · `cardanswer.go` · `cardroom.go` |
| Room | `internal/ui/chat.go` · `chat_blocks.go` · `roomhistory.go` · `roomfocus.go` · `roomfilter.go` |
| DM | `internal/ui/dm.go` · `dm_blocks.go` · `dmtranscript.go` · `dmbeat.go` · `partial.go` · `toolblocks.go` · `rollup.go` · `checklist.go`/`checklistpin.go` · `followbanner.go` · `compacting.go` · `loop.go` |
| Working/done lines | `internal/ui/beat.go` (start here) · `heartbeat.go` · `shimmer.go` · `heartbeatwords.go` · `roomwords.go` · `donewords.go` |
| Roster, strip, status bar | `internal/ui/roster.go` · `rostersubs.go` · `rostersection.go` · `awareness.go` · `statusbar.go` · `attention.go` (not `internal/core/attention.go` as the spec says) |
| Completion | `internal/ui/completion.go` · `completionpath.go` |
| Board | `internal/ui/board.go` · `boardtile.go` · `boardtilesection.go` · `boardtranscript.go` |
| `!cmd` shell lines | `internal/ui/bang.go` · `bangout.go` · `bangapp.go` · `bangproc_unix.go` |
| Theme, palette | `internal/ui/theme.go` · `internal/ui/testdata/claude-palette.json` (maintained by hand) |
| Markdown, diffs, tools | `internal/render/` — `markdown.go`'s `reflowProse` holds the greedy-wrap fix |
| Notices under a TUI | `internal/notice/notice.go` |
| Git branch lookup | `internal/gitref/` |
| Fixtures | `testdata/stream/` (stdout) · `testdata/transcript/` (on-disk, a different format) · `testdata/input/` (lines Wake writes) |
| Demo film | `demo/` (Python stand-in agent, VHS tapes) |
| Fixture scrubber | `scripts/scrub-fixtures.py` · guard `internal/core/corpus_test.go` |

## Toolchain

Go 1.26+. Dependencies: `bubbletea`, `lipgloss`, `bubbles`, `glamour` (all MIT, Charm).

```bash
make build     # go build ./cmd/wake
make test      # go test ./... -race, then again without it
make cover     # coverage report; gate is 80%
make lint      # golangci-lint run
make ci        # every step the workflow runs
make soak      # 20 fake sessions replaying fixtures; SOAK_DURATION=1h for the long one
make run       # build and start
```

glamour's greedy-wrap defect (a word stranded after a hyphen) is fixed wake-side in
`internal/render`'s `reflowProse`, not via a `replace` — a `replace` breaks `go install`. Guard:
`TestProseWrapsGreedily`.

## Testing

**80% coverage minimum. TDD: write the failing test first. Never test against a live LLM** — record
real sessions once, commit the JSONL to `testdata/`, replay forever. A session that misbehaves gets
recorded and becomes a regression test.

Record into a sterile `HOME`:

```sh
HOME=$(mktemp -d) claude --print --input-format stream-json --output-format stream-json --verbose …
```

| Layer | Approach |
|---|---|
| `protocol` | Golden files against recorded stream-json |
| `attention`, `router` | Pure functions, table tests |
| `session` | Fake process behind the same interface |
| `rpc`, `daemon` | Contract tests over a real socket |
| `ui` | In-process assertions, plus `internal/ui/frame_test.go` reading `App.View` |
| screen | **A real pty, the real binary, `vt10x`** (`cmd/wake/screen_unix_test.go`) — use for layout, keys, mouse |
| `cmd/wake` | Fake daemon, in-process `daemon.Serve`, `detach_unix_test.go` |
| soak | Build tag `soak` |

`make test` runs twice (with and without `-race`) — the detector masks ordering bugs. `make ci` may
not drift from the workflow (`internal/core/citarget_test.go`). A goroutine leak is a bug.

**Guards that derive claims — never "fix" one by editing the number:**
`TestCLAUDEmdNamesTheTwoLargestNonTestFiles` and `TestCLAUDEmdDescribesTheLegendItDraws` read this
file; `TestNoNonTestFileCrossesTheHardMax` (800 lines); `airlock_test.go` / `argv_test.go`; totality
guards derive their domain from the producer (`parkStates`, `forkParentStates`, `forkArrivalStates`,
`renameableStates`, `managerVerbs`). Adding an `rpc.SessionStatus` field trips three reflective
guards (ui `WithStatus` fold, mcp `agentAuthored`, mcp `notInTheStatusReport`).

## Claude Code CLI surface

**Verified against v2.1.232** (re-checked 2026-08-13); corpus captured at 2.1.226–2.1.240. A
fixture's `init` names its version. Findings notes: `docs/superpowers/notes/`.

| Need | Flag |
|---|---|
| Programmatic control | `--print --input-format stream-json --output-format stream-json` + `--verbose` (required) |
| Permission requests | `--permission-prompt-tool stdio` (undocumented; without it every ask is auto-denied) |
| Identity | `--session-id` · `--resume` · `--fork-session` (only as `--resume <parent> --fork-session --session-id <new>`) |
| Name · mode | `--name` · `--permission-mode` (Wake spawns `auto`; ⇧⇥ reaches four). Changing mode later is a `set_permission_mode` control request |
| Spend, failover | `--max-budget-usd` · `--fallback-model` |
| Thinking | `--effort low\|medium\|high\|xhigh\|max` (the `/effort` command takes seven) · `--model` |
| Visibility | Wake emits all five: `--include-hook-events`, `--include-partial-messages`, `--replay-user-messages`, `--forward-subagent-text`, `--brief` |
| Tool reach | `--add-dir` (Wake emits the repeated form) |
| Debug | `--debug-file <path>`; `--debug` alone logs nothing observable headless |
| Isolation | `--worktree` — **not used**; Wake runs `git worktree add` itself |
| Manager | `--mcp-config` only beside `--strict-mcp-config` and `--tools ""`; `--append-system-prompt` |

### Traps

- `init.permissionMode` is normalized (`manual` → `default`); after `set_permission_mode` it is
  authoritative. The receipt, not the request, is the truth; a refusal is subtype `"error"`.
- **`result` and `system/init` are per-turn, not per-process.** Treating `result` as exit kills live
  sessions.
- `control_request`/`control_response` nest their subtype; a permission request has **no
  `session_id`** — correlate on `request_id`.
- `new_conversation_id` names the id that *died*. Re-key on `session_id` changing. `/clear` changes
  the session id.
- A client deny ends `success` with `non_execution_kind: "permission-rule"`; an interrupt ends
  `error_during_execution`. **A denial is not a turn failure.**
- Spend: `total_cost_usd`/`modelUsage` reset on `/clear` — accumulate per session-id epoch. Not
  derivable from `num_turns`/`duration_api_ms`.
- An interrupted turn has no `result` key; an interrupted process exits 1 with empty stderr
  (`interruptedExit` suppresses it). `[Request interrupted by user]` arrives as a `user` frame.
- One `can_use_tool` carries three shapes; `AskUserQuestion` answers ride in
  `updatedInput.answers` (`core.askKind`, decided by payload shape, never tool name).
- A question killed by closing stdin is indistinguishable from an operator deny.
- Images: first in the content array, text last. An undecodable image silently degrades to text.
- A malformed stdin line is echoed to stderr in full, then exit 1.
- `/model`, `/clear`, `/compact`, `/context` survive stream-json; `/resume` does not. Bare
  `/effort`/`/model` do nothing (`num_turns: 0`, `$0`).
- `stream_event` text deltas are byte-identical to the completed `assistant` block
  (`testdata/stream/partial-turn.jsonl`); unrecognised shapes yield no event.
- The checklist is `TaskCreate`/`TaskUpdate` keyed on a monotonic id; `TodoWrite` is retired.
- A headless session answers `mcp_status`/`mcp_reconnect`/`mcp_toggle` with no model turn
  (2.1.281, `testdata/stream/mcp-control.jsonl`). Reconnect/toggle reply with the bare `success` a
  mode change gets — only the request id says what it answers. `mcp_toggle` persists. The status
  carries no tool descriptions.
- `claude mcp login` refuses a non-terminal stdin and has no headless control request — hence the
  hand-over. **A headless session does not load claude.ai connectors**, even with
  `ENABLE_CLAUDEAI_MCP_SERVERS=true`.

## Conventions

**Surgical code, brief comments, nothing extra** (owner's rule, 2026-08-12):

- **Smallest change that does the job.** No scaffolding, no unused options, no one-call helpers.
- **Comments are brief** — one or two lines, usually *why*. The long essays in this tree are history,
  not a template.
- **Nothing parallel. No dead code.** A guard's domain is what can *arrive*.
- **Immutable by default**, especially `attention` and `router`.
- **Small files: 200–400 typical, 800 hard max.** The two largest non-test files are
  `internal/core/protocol.go` at 799 and `internal/core/vocabulary.go` at 799 — derived by
  `TestCLAUDEmdNamesTheTwoLargestNonTestFiles`. Split by subject, never by line count.
- **Functions under 50 lines. Nesting under 4 levels.**
- **Handle every error explicitly.** A malformed JSON line logs and skips. Under a TUI, failures go
  to `internal/notice`, never stderr.
- **No hardcoded values. Reference code by symbol, not line number.**
- **A number nothing asserts is wrong by default.** Derive it or delete it.

## Running Wake from this working tree

**Never run `wake` — any verb — from this repository without `WAKE_SOCKET` set.** The default
`~/.wake/daemon.sock` is the owner's real fleet. `make` targets are safe; `go run ./cmd/wake`,
`./bin/wake` and `wake` on `PATH` are not.

```sh
WAKE_SOCKET=$(mktemp -d)/wake.sock go run ./cmd/wake status
```

**`wake stop` is the only irreversible verb.** An agent once stopped the owner's fleet with it and
reported it had checked. **Look before a destructive verb, in the same command** — `wake status`
first, and read it.

## Git

- **A feature goes on a branch and gets a PR, always.** For a bug fix, ask where it goes. Branch at
  the first edit; when green, say it is a PR awaiting merge.
- **Two reviews before opening the PR:** a code review (*is this diff sound*) and an adversarial one
  (*what would make its claims false* — check that a passing test can go red). Say which ran in the
  PR body.
- **Actions is unfunded: `make ci` on this machine is the only gate.** Run it before opening the PR
  and put the exit code in the body.
- **Every PR carries before/after screenshots of the real binary** (owner's rule, 2026-09-23) in a
  `## Screenshots` section — `main` build vs branch build doing the same thing. A change with nothing
  visible says so, with the reason.
  - Record with VHS against the real `wake` and a scripted fake `claude` on a shim `PATH`
    (`demo/agent/claude`). Never a live LLM or the owner's fleet: scratch `HOME`, and a **fresh
    `WAKE_SOCKET` directory per take** (a reused one inherits orphans and hangs `wake new`).
  - Use a neutral project path (e.g. `/tmp/<name>`) — a home path puts the operator's name in the image.
  - Host images on an orphan branch `pr-assets/<head-branch>` (head branch verbatim, one parentless
    commit, linked by `raw.githubusercontent.com`). Never commit a PNG to the feature branch. To
    replace an image, force-push a fresh parentless commit.
  - Before pushing a new one, delete assets branches whose PR merged or closed (report, don't
    delete, a head with no PR):

    ```sh
    git ls-remote --heads origin 'pr-assets/*' | sed 's#.*refs/heads/pr-assets/##' |
    while read -r b; do
      s=$(gh pr list --head "$b" --state all --json state --jq '.[0].state // "NONE"')
      case "$s" in MERGED|CLOSED) git push -q origin --delete "pr-assets/$b" ;; esac
    done
    ```
- Conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`, `perf:`, `ci:`);
  branches are `<type>/<kebab-description>` (`fix/`, never `bugfix/`).
- **Dev worktrees live in `.worktrees/<name>`** inside the repo (gitignored) — never `$HOME`,
  `~/Documents`, `/tmp` or beside the repo. Remove with `git worktree remove` after merge.
- **Never add Claude attribution** in commits, PR titles or bodies.
- **Docs-only commits go straight to `main` and are pushed immediately** so other worktrees see them.
  Read another worktree's notes via `git fetch origin main && git show origin/main:<path>`.

## Working notes

- `docs/goals.md` — the asks, the four phases, built vs not-built.
- `docs/live-testing.md` — what only a human at a real terminal can check. Anything testable there
  is a bug in that file.
- `docs/notes/deferred.md` — consciously put off. **Read it before starting a task.**
- `docs/notes/decisions.md` — rulings not obvious from the code, and recurring failure modes.
- `docs/notes/bugs.md` — defects somebody watched go wrong.

## Scope discipline

The v1 boundary is spec §17. Before adding anything not on the "in" list, check §2 (non-goals) and
§17 (out). The failure mode is drifting toward a worse cmux. **The group chat is the product; the
panes are substrate.**
