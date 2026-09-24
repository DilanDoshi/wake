# `/mcp` as Claude Code's menu — design

**Status:** built on `feat/mcp-menu`, 2026-09-24. Owner-approved mockups in the session that
produced it; rulings recorded in `docs/notes/decisions.md` (2026-09-24).

## Goal

Make Wake's `/mcp` as close as a headless fleet allows to Claude Code's interactive `/mcp`: the
same menu, the same actions, and — the part Claude does inside its own process — signing a server
in. Fleet-wide views are out of scope, except that one sign-in reconnects every agent stuck on the
same server.

## What Claude Code's `/mcp` does

A list titled *Manage MCP servers*, grouped by where each server is configured, each row a `✔ ⚠ ✘`
glyph and a status or tool count; `↵` opens a detail view (status, URL or command, config, tools,
error) with actions — View tools, Authenticate / Re-authenticate / Clear authentication, Reconnect,
Enable / Disable. Authenticate opens the browser at the server's authorization endpoint, listens for
the redirect on `localhost`, stores the token in the keychain and reconnects.

## What a headless Wake agent can do (probed 2026-09-23, 2.1.281, sterile HOME)

| Need | Headless | Recording |
|---|---|---|
| Live status: state, error, config, scope, tools | `mcp_status` control request, no turn | `testdata/stream/mcp-control.jsonl` |
| Reconnect one server | `mcp_reconnect {serverName}` | same |
| Enable / disable (persists in `~/.claude.json`) | `mcp_toggle {serverName, enabled}` | same |
| Tool descriptions | **not returned** | — |
| Sign-in | **no control request**; `claude mcp login` refuses a non-terminal stdin | probe |
| claude.ai connectors | **not loaded** by a headless session at all | probe |

## Design

**Wire.** `internal/core/encode.go` encodes the three requests byte-for-byte against
`testdata/input/mcp-control.stdin.jsonl` and decodes a status reply (known by its `mcpServers` key)
into `KindMCPReply`. Reconnect and toggle answer with a mode receipt's bare shape, so
`Session.answeredMCP` labels those by the request id it minted. Four socket kinds in
`internal/rpc/mcp.go`, the server in `Frame.Text`; the daemon writes them without the blocked-on-ask
refusal (nothing about a permission ask changes), and the answer reaches every client on the event
stream. The manager is refused all four.

**Menu.** `internal/ui/mcpmenu.go` + `mcpmenuview.go`: a modal box over the composer of the pane that
opened it (the `/resume` picker's placement and key routing, above `App.key`'s switch, no legend
entry). Three levels — list, detail, tools — and `esc` walks back out. Bare `/mcp` targets the
conversation you are in or the room's roster pick; `@who /mcp` aims it. Actions are offered only
where they help (Authenticate only for `needs-auth`, Enable only for `disabled`), and every state
change waits for the agent's reply; a success re-asks the list.

**Sign-in.** `internal/ui/mcpauth.go` runs `claude mcp login <server>` in the agent's directory through
a `HandOver` seam that `cmd/wake` supplies: Bubble Tea's `ExecProcess` stops drawing and reading, and
`cmd/wake/handover.go` pauses the kill switch — its pump reads through a cancellable reader, and
`suspend` stops it, restores cooked mode and mutes the signal watcher until the child exits. Handing
the terminal over emulates nothing, so the no-PTY non-negotiable stands. A finished sign-in reconnects
the asking agent, then asks every other live agent for its servers and reconnects those stuck on the
same server, reporting once. With no terminal (no kill switch), Authenticate prints the command.

## Known limits

- While the sign-in holds the terminal, Wake's screen is frozen (Bubble Tea's exec is synchronous).
  The socket drain keeps reading, but a very busy fleet can overflow its buffer during a long
  sign-in; the result is the ordinary "dropped N frames" gap, which re-derives what it lost.
- That a live session picks up a token another process wrote on `mcp_reconnect` is **unverified**
  (`docs/live-testing.md`).
- Re-authenticate and Clear authentication are not offered: `claude mcp logout` exists, but a
  connected server's re-sign-in has not been probed, and clearing a token is an irreversible verb this
  change did not need.
