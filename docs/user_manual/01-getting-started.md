# 1. Getting started

## Install

Wake runs on your own Claude Code, so install that first and sign in
([Claude Code setup](https://code.claude.com/docs/en/setup)). Then:

```sh
curl -fsSL https://raw.githubusercontent.com/DilanDoshi/wake/main/scripts/install.sh | sh
```

That installs the latest release for your machine to `~/.local/bin`, checked against the release's
checksums, and offers to put that directory on your `PATH` if it is not there already. `wake
--version` says which build you have; [chapter 4](04-lifecycle.md#upgrading) covers upgrading.

## Your first agent

```sh
cd ~/your-project
wake
```

One command does four things: starts a background daemon, spawns a `claude` process, gives it a
name from a 64-name pool, and opens **the room** — the group chat over your whole fleet, which at
this point is one agent.

You will see it on the roster, on the right, as a row like `sydney <> dev-5748`. The first half is
the agent's handle — what you type after `@`. The second half is its **task label**, read from the
git branch checked out where the session started, falling back to the directory name. Set your own
with `/task <what it is on>`.

Press `↵` on that row and the conversation opens. Type and press `↵` again. You are in Claude
Code: the same rendering, the same `/commands`, the same `@file` completion. `⌃W` closes the pane
and leaves you in the room.

`wake` lands on the room rather than on the agent because the room is what it is a request about —
you will be running fifteen of these, not one. `wake new` is the verb that means *an agent, and
put me in it*.

## The four things to know before your second agent

**1. Wake is not a terminal.** No PTY, no shell panes, no VT100. Wake talks to `claude` processes
over structured JSON; it does not pretend to be a multiplexer. `!cmd` runs a shell line and shows
you the output, and that is the whole of it.

**2. The daemon outlives your terminal.** Agents are children of a background daemon, not of your
shell. `⌃O` closes Wake and leaves everything running. Close the terminal, shut the laptop lid,
come back tomorrow, run `wake` — the work is done and waiting.

**3. Every agent starts in `auto` permission mode.** It will act without asking for most things, and
ask you when it genuinely needs a decision. `⇧⇥` cycles it — `default` → `acceptEdits` → `plan` →
`auto` — for the agent the roster has selected.

**4. A name is not an address.** Names are for you. Wake routes on session ids internally, and a
name is released when its session ends and may be reissued to somebody else.

## A second agent

```sh
wake new                 # a name from the pool
wake new backend         # a name you choose
```

Both open a conversation with the new agent and put it in the room with the others.

You can also start one without leaving Wake: `/new [name]` in the composer spawns an agent from
inside the room, and takes the spawn flags too — `/new backend --worktree fix-42 in ~/api`.

## Finding your way back

```sh
wake --fleet <name>      # reopen a fleet you already have
wake fleets              # list every fleet on this machine
wake attach sydney       # open one conversation by name
wake attach 6c246eb1     # or by the first few characters of its id
wake status              # what is alive, without opening anything
```

The thing to know: a bare `wake` starts a *new* fleet each time and names it, so it is no longer the
way back. `wake --fleet <name>` reopens one you already have, and `wake fleets` lists them. On a
fresh machine with nothing running, a bare `wake` is still the right first command — it makes your
first fleet and spawns an agent to start with.

## Stopping

Read [chapter 4](04-lifecycle.md) before you stop anything, because the four ways to stop are
genuinely different and one of them cannot be undone. The short version:

- **`⌃O`** — you leave, they keep working.
- **`⌃C`** — park the agent in front of you. Recoverable with `/resume`.
- **`⌃Q⌃Q`** — park everything and quit (the first `⌃Q` arms, the second confirms). Next `wake` offers it back.
- **`wake stop`** — end the fleet. **Not recoverable.**

## Where your conversations actually live

Claude writes every session to `~/.claude/projects/<directory>/<session-id>.jsonl`. Wake stores
almost nothing of its own — a roster, a park book, and your layout. That is deliberate: Wake can
crash and lose nothing that matters.

It also means a session is located **by the directory it started in**. That is why a forked or
woken agent always runs where its parent ran, and why moving directories is not something you can
do to a running session.
