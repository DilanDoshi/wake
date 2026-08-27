# Features to add

Things Wake should *do* that it does not yet — as opposed to defects, deferrals, or Claude Code
parity gaps. The fourth question alongside the other three notes files:

| File | Asks |
|---|---|
| `deferred.md` | what did we consciously put off |
| `bugs.md` | what did somebody watch go wrong |
| `claude-code-gap.md` | what does Claude Code do that we do not |
| **this file** | what should Wake do that nobody has built |

An entry is a *wish*, not a plan — it names the behaviour and, where known, the code it would touch,
so the next person starts from a seam rather than a blank page. Same rule as the others: an entry
leaves only when it ships or when somebody decides it never will, written down. And **check it still
describes the tree before building** — the room, the router and the status bar all move.

---

## The room's status bar follows the agent a draft addresses

When a room draft begins `@<agent>` (or `@manager`), the bottom status bar should switch to *that*
agent's — its directory, model, context left, branch — the same bar a DM draws
(`internal/ui/statusbar.go`, cached on `DM.bar`). Today the room's chrome says nothing about who a
message is aimed at, so you address an agent blind to which directory / model / context it is in
without opening its pane.

The seam: the room already resolves a draft's target (`internal/ui/send.go`'s `roomRoute`). Draw the
resolved agent's status bar in the room's chrome while the draft names one, and fall back to the
room's own line when it does not.

Open questions: what the bar shows for a **broadcast** or a **multi-recipient** draft (see below) —
the oldest addressee, a count, or nothing; and whether it follows the `@` at the cursor or the first
`@` in the draft.

## More than one `@<agent>` per message

Address several agents in one room message — `@alice @bob do the thing` routes to both, echoed into
each of their conversations, without being a fleet-wide broadcast. Today `internal/core/router.go`
resolves a message to a single `@name`, the manager, or a broadcast, and `internal/ui/send.go`'s
`echoToRouted` already echoes to the *routed set* — so the echo half thinks in a set; the routing
half is what collapses to one target. The change is letting the router return the set of names a
draft names and routing to each, kept distinct from a broadcast (which reaches the whole fleet and
keeps the names in the text, per open-mention mode).

Pairs with the status-bar item above: a multi-recipient draft is exactly the case its "what does the
bar show" question has to answer.

## The manager can orchestrate the fleet, not just dispatch to it

*Raised 2026-08-25, for after this version ships.* The manager is a full `claude` session but it is
**not** a general Claude Code orchestrator, and cannot be made one by configuration. `argv.go` emits
`--tools ""` beside `--mcp-config`, so every built-in is gone — no `Read`/`Grep`/`Glob`, no
`Bash`/`Write`/`Edit`, and **no `Task` (subagent dispatch)**. Its six tools
(`internal/mcp/tools.go`) are Wake's own MCP tools over the fleet. So today the manager cannot read
the corpus, cannot fan out explore subagents, and cannot even see what an agent *produced*: the read
tools return metadata (state, the tool call it is inside, how it ended), never an agent's output. It
can dispatch blind; it cannot collate results the way a classic orchestrator synthesizes subagent
reports.

The wish: a manager that can read the fleet's *output* and dispatch/synthesize real work — read the
corpus, spawn explore agents, gather their findings — the orchestrator role people expect from a
Claude Code session.

**The tension this entry exists to hold.** The bound is load-bearing, not an omission. Everything the
manager reads is untrusted text an agent's model wrote, and it holds `send_to_agent`, which starts a
turn on an agent running in `auto`. `--tools ""` was added 2026-08-12 precisely to close the
*injected-text → manager → shell → machine* escalation path
(`docs/superpowers/notes/2026-08-12-tool-bounding-findings.md` §4), and the current non-negotiable is
"the manager can send, interrupt and spawn, and nothing else." So this is a security-posture decision,
not a feature toggle.

The seam and the three directions, to be chosen explicitly:

1. **Leave it, and use an ordinary agent as the orchestrator seat.** A normal Wake agent already has
   the full toolset, its own subagents and file access — talk to it in its DM. Cheapest; the
   injection bound stays intact. This may simply be the answer.
2. **A second, un-bounded manager-like role** that keeps the fleet MCP tools *and* Claude Code's
   built-ins. This is the escalation `--tools ""` closed, so it needs its own answer to "what stops
   injected agent text from driving its shell," and probably should not be the room's default
   addressee.
3. **Widen the manager's MCP surface** (`internal/mcp/tools.go`, `internal/daemon/manager.go`) so it
   can read fleet output and dispatch read-only work — without ever handing it a shell. Middle path:
   keeps the machine bound while making it a real orchestrator over the fleet. Most design work.

**Operational prerequisite, verified 2026-08-25.** A long-lived manager will need `/compact` or
`/clear`. Both survive stream-json and are in-process, so the manager's argv — `--tools ""`,
`--mcp-config`, its scope prompt, its `manager` name — persists across them: it stays bounded and
stays the manager. `/compact` is clean (same id; bills ~$0.04 while reporting `num_turns: 0`).
`/clear` mints a new claude session id, which the daemon already re-keys (`agent.go`, on
`ev.SessionID != a.claudeID`; `KindSessionReset` clears it). **Untested, worth a live check before
relying on it:** `/clear` then **park → wake** — a wake does `--resume <claude-id>` and `/clear`
moved that id; the stream-json findings note lists "what `--resume <original-uuid>` does after a
`/clear`" as *not tested*.
