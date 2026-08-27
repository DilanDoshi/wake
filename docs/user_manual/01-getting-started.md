# 1. Getting started

## Your first agent

```sh
make build
./bin/wake
```

One command does four things: starts a background daemon, spawns a `claude` process, gives it a
name from a 64-name pool, and opens **the room** — the group chat over your whole fleet, which at
this point is one agent.

You will see it on the roster, on the right, as a row like `sydney <> dev-5748`. The first half is
the agent's handle — what you type after `@`. The second half is its **task label**, read from the
git branch checked out where the session started, falling back to the directory name. You cannot
set it yet.

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

**3. Every agent runs in `auto` permission mode.** It will act without asking for most things, and
ask you when it genuinely needs a decision. You cannot change this yet.

**4. A name is not an address.** Names are for you. Wake routes on session ids internally, and a
name is released when its session ends and may be reissued to somebody else.

## A second agent

```sh
wake new                 # a name from the pool
wake new backend         # a name you choose
```

Both open a conversation with the new agent and put it in the room with the others.

There is no way to do this from inside Wake yet — no `/new`, no button. It is the largest gap
between what this app was asked to be and what it is, and it is written down as such.

## Finding your way back

```sh
wake                     # reopen the room on whatever is running
wake attach sydney       # open one conversation by name
wake attach 6c246eb1     # or by the first few characters of its id
wake status              # what is alive, without opening anything
```

`wake` with no arguments is the one to remember. It reopens the whole room — every agent, both
sidebars, everything as you left it. If nothing is running it starts an agent as well, so it is
also the right first command on a fresh machine; either way what opens is the room.

## Stopping

Read [chapter 4](04-lifecycle.md) before you stop anything, because the four ways to stop are
genuinely different and one of them cannot be undone. The short version:

- **`⌃O`** — you leave, they keep working.
- **`⌃C`** — park the agent in front of you. Recoverable with `/resume`.
- **`⌃Q`** — park everything and quit. Next `wake` offers it back.
- **`wake stop`** — end the fleet. **Not recoverable.**

## Where your conversations actually live

Claude writes every session to `~/.claude/projects/<directory>/<session-id>.jsonl`. Wake stores
almost nothing of its own — a roster, a park book, and your layout. That is deliberate: Wake can
crash and lose nothing that matters.

It also means a session is located **by the directory it started in**. That is why a forked or
woken agent always runs where its parent ran, and why moving directories is not something you can
do to a running session.
