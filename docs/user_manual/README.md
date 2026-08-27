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

You get a daemon, an agent, and an open conversation. Then:

- Type to talk to the agent whose pane has the keys.
- **`@name`** in the room talks to one agent; **`@all`** talks to everybody.
- **`⌃C`** parks the agent you are looking at — its process stops, its conversation is kept, and
  `/resume` brings it back.
- **`⌃O`** detaches. Wake closes, every agent keeps working. `wake` reopens.
- **`⌃Q`** parks the whole fleet and quits. Next `wake` offers it all back.

The distinction that matters most is in [chapter 4](04-lifecycle.md): **park is about the agent,
detach is about you.**

## What is not built

Named here so you do not go looking:

- **A manager session.** You cannot `@manager`. That is Phase 2's remaining work.
- **Creating or naming an agent from inside Wake.** No `/new`, no `+` button, no rename. Every
  session starts from the shell with `wake` or `wake new`, and its label comes from the git branch.
- **Subagents in the sidebar.** Deferred in favour of shipping the room.

## A warning worth reading once

**`wake stop` is the only irreversible verb.** It ends every session, and nothing brings a
*stopped* session back — that is precisely why park exists. If you want to shut down and come
back, use `⌃Q`.
