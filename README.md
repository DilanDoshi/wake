# Wake

A terminal app for developers running many Claude Code sessions at once. 

**Current version:** 0.1.6 · **[Download the latest release →](https://github.com/DilanDoshi/wake/releases/latest)**

**Website:** [wake-landing-rouge.vercel.app](https://wake-landing-rouge.vercel.app/)

Wake turns your fleet into a room. A group chat is the primary surface: `@name` an individual,
or broadcast to everyone. Agents stay quiet unless they're blocked or have something to report.
A roster ranks every agent by whether it needs you. Any agent opens as a full 1:1 conversation
at Claude Code fidelity.

**Wake is Claude Code plus a room** — not a replacement. Close the sidebars and open a DM and
you're in Claude Code: the same `/commands`, the same `@file`, the same rendering. Open the room
and you're running a fleet.

Wake never screen-scrapes. Every agent is a headless `claude` process in stream-json mode with a
Wake-assigned session id, and all state comes from structured JSON on stdout.

## Install

Wake runs on your own Claude Code, so `claude` has to be installed, signed in and on your `PATH`
first. Then paste this into a terminal:

```sh
curl -fsSL https://raw.githubusercontent.com/DilanDoshi/wake/main/scripts/install.sh | sh
```

It downloads the latest build for your Mac or Linux machine from the
**[releases page](https://github.com/DilanDoshi/wake/releases/latest)**, checks it against the
release's checksums, and installs it to `~/.local/bin`. If that directory is not on your `PATH` it
offers to add it (Claude Code's own installer uses it, so it usually is already). It also tells you
if `claude` is missing.

**Upgrading:** `wake upgrade` installs the newest release over the one you have (a build older than
the verb upgrades by re-running the install line). Wake checks once a day and says in the room when
a newer release is out; `WAKE_NO_UPDATE_CHECK=1` turns that off.
Fleets that are already running keep the old build until you `⌃Q⌃Q` and reopen them — see
[the lifecycle chapter](docs/user_manual/04-lifecycle.md#upgrading).

With Go 1.26+ you can instead run `go install github.com/DilanDoshi/wake/cmd/wake@latest`, which
puts `wake` in `$(go env GOPATH)/bin`. The binaries are not signed: one downloaded in a browser is
blocked by macOS until you run `xattr -d com.apple.quarantine <path-to-wake>`.

## Getting started

```sh
cd ~/your-project
wake
```

That starts a daemon, spawns an agent, and opens **the room** — the agent is a roster row, and `↵`
on it opens the conversation. (`wake new` is the verb that opens one for you.) `⌃O` detaches and
leaves the fleet working — close the terminal entirely and they keep going. Run `wake` again to
come back.

**→ [The user manual](docs/user_manual/) — start at [Getting started](docs/user_manual/01-getting-started.md).**

## The keys

| | | | |
|---|---|---|---|
| `↵` send | `⎋` interrupt | `⎋⎋` clear draft | `⌃O` detach |
| `⌃C` park | `⌃Q⌃Q` quit & park all | `⇥` next chat | `⇧⇥` permissions |
| `↑↓` prompt history | `⇧↑↓` pick agent | `⇧←→` move focus | `⌃X` next blocked |
| `⌃D` open DM | `⌃Y` open right | `⌃B` open below | `⌃W` close pane |
| `⌃R` activity | `⇞⇟` scroll | `⌃E` expand | `⌃F` fork |
| `⌃T` mention mode | `⌃A` show all | `⌥↵`/`⌃J` newline | |

`↑↓` recall your previous prompts into the query bar (Claude Code's own history keys); `⇧↑↓` walk the
roster instead — including a conversation's running subagents in the right sidebar — and `↵` or `⌃D`
opens the one the cursor is on. `⌃O` arms the detach and `↵` finishes it; a second `⌃O` cancels.
While a completion menu is up, `⌃N`/`⌃P` walk it and `⇥` completes.

Full reference: [Keyboard shortcuts](docs/user_manual/03-keyboard.md).

## Commands

```
wake                    start a new fleet, name it, and open it
wake --fleet <name>     open one that already exists (a bare `wake` always makes a new one)
wake fleets             every fleet on this machine
wake new [name]         open a conversation with a new agent, with a name you choose
wake attach <who>       open a conversation with one already running, by name or id
wake fork <who> [name]  branch a conversation: a new agent with the same history so far
wake import [<id>]      adopt a claude session this machine already has
wake setup-terminal     configure your terminal: Shift+Enter → a newline, Cmd+←/→ → line start/end
wake upgrade            install the newest release over this one
wake --version          which build you have
wake manager            start the manager from a shell (the room seats one by default)
wake status             what is running
wake stop               stop every session and the daemon — the one irreversible verb
```

Flags on the verbs that start a session (`new`, `manager`):

```
--effort <level>          low | medium | high | xhigh | max
--model <model>           what it thinks with
--max-budget-usd <amt>    what it may spend
--fallback-model <m,m>    what it fails over to when a model is overloaded
--worktree <name>         run it in a fresh git worktree of that name
--add-dir <dir>           a directory outside its own its tools may reach (repeatable)
--debug-file <name>       per-session debug log, placed beside the socket
--debug <categories>      narrow those categories (refused without --debug-file)
```

Inside the room, `/new` takes `--worktree`, `--add-dir`, `--debug-file` and `--debug` too. The other
slash commands are `/resume`, `/name`, `/task`, `/color`, `/team`, `/quit`, `/adopt`, `/mcp`, `/login`,
`/reauth`, `/manager`, `/manager-stop`, `/board` and `/groupchat-filter`; `/effort` and `/model`
configure the session they are addressed to. Everything else you type is passed to the agent byte for
byte. A lone `@name` narrows the room to that agent's thread; `⌃A` widens it back to everyone while
still addressing them, and `/groupchat-filter off` flips that default. `/team <name>` groups an agent
under an operator-named team — heading a roster/board section, addressed as `@team` — and the manager
can fan a message out to one with its own tool.

## Development

```sh
make build   # from source: ./bin/wake
make test    # the suite, twice: with and without -race
make lint    # golangci-lint
make cover   # coverage gate, 80% per package
make soak    # 20 fake sessions through a real daemon
```

`CLAUDE.md` is the source of truth for how this is built and why. Read it before changing
anything — most of the surprising decisions have an argument written next to them.

**Never run `wake` from this working tree without `WAKE_SOCKET` set.** The default socket is a
real fleet's. `make` targets are safe; a bare `go run ./cmd/wake …` is not.

## Notice

Wake is not affiliated with, endorsed by, or sponsored by Anthropic. Claude and Claude Code are trademarks of Anthropic.
