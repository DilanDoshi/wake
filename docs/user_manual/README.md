# Wake — user manual

Wake runs many Claude Code sessions at once and puts them in a room.

Everything here describes what the build **actually does today**. Where something is not built,
it says so rather than describing the plan — this project's rule is that a missing feature is not
trusted and a lying one is, and a manual is the easiest place to break it.

| | |
|---|---|
| **[1. Getting started](01-getting-started.md)** | Your first agent, and the four things to know before the second |
| **[2. The room](02-the-room.md)** | The group chat, `@name`, `@all`, and why agents stay quiet |
| **[3. Keyboard shortcuts](03-keyboard.md)** | Every key, what it does, and the ones that are deliberately absent |
| **[4. The lifecycle](04-lifecycle.md)** | Park, detach, stop, quit — four different things, and which are reversible |
| **[5. Forking](05-forking.md)** | Branching a conversation you want to take two directions |
| **[6. Commands](06-commands.md)** | `wake …` at the shell, `/…` in the composer, `!…` for a shell line |
| **[7. When something goes wrong](07-troubleshooting.md)** | Refusals, orphans, and what each message means |

## The one-minute version

```sh
make build && ./bin/wake
```

You get a daemon and the room, with the manager in it. Then:

- **`/new`** starts an agent. Anything you type in the room without an `@name` goes to the manager,
  which can start agents for you; in an open conversation, you talk to that agent.
- **`@name`** in the room talks to one agent; **`@all`** talks to everybody.
- **`⌃C`** parks the agent you are looking at — its process stops, its conversation is kept, and
  `/resume` brings it back.
- **`⌃O`** detaches. Wake closes, every agent keeps working. `wake --fleet <name>` reopens.
- **`⌃Q`** parks the whole fleet and quits. Next `wake --fleet <name>` offers it all back.

The distinction that matters most is in [chapter 4](04-lifecycle.md): **park is about the agent,
detach is about you.**

## What is not built

Named here so you do not go looking:

- **A key or a `+` button that creates an agent.** `/new` creates one, `wake new` does it from the
  shell, and `⌃F` forks the conversation you are in; `⌃D` opens a conversation rather than creating one.
- **A queue past the cap.** A spawn past `--max-agents` is refused rather than held until there is
  room; park an agent with `⌃C` first ([chapter 6](06-commands.md)).

## A warning worth reading once

**`wake stop` is the only irreversible verb.** It ends every session, and nothing brings a
*stopped* session back — that is precisely why park exists. If you want to shut down and come
back, use `⌃Q`.
