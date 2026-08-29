# 6. Commands

Three kinds, in three places.

## `wake …` — at the shell

```
wake                    reopen the room, or start an agent if nothing is running
wake new [name]         open a conversation with a new agent, with a name you choose
wake attach <who>       open a conversation with one already running, by name or id
wake fork <who> [name]  branch a conversation: a new agent with the same history so far
wake status             what is running
wake stop               stop every session and the daemon
```

**`wake` with no arguments is the one to remember.** It reopens the whole room on whatever is
running. If nothing is running it starts an agent as well, so it is also the right first command —
but the room is what opens either way, and that agent is a roster row you press `↵` on. `wake new`
is the verb that opens a conversation.

**`<who>`** is a name or a session id, and a unique prefix of either works — `wake attach syd`, or
the first few characters of the id `wake status` prints. If a prefix is ambiguous Wake lists the
candidates rather than guessing.

**`--max-budget-usd <amount>`** stops the agent once it has spent that much, on `new` and on
`manager`. `wake new alex --max-budget-usd 5` is an agent that cannot cost more than five dollars.
The cap survives a park, so an agent you `⌃C` and bring back is still capped — there is no way to
change one afterwards, so the alternative would be that parking quietly removed it. The conversation
banner names it beside the model.

What it does **not** do is tell you how close you are. Wake knows what it asked for and does not
report spend against it, so treat the cap as a stop and not as a gauge.

**`--fallback-model <model,model>`** is what the agent runs instead when its first choice is
overloaded, tried in order, with the first re-tried at the start of every turn. This matters at
fleet scale and hardly at all at one agent: an overloaded model does not stop the agent you are
watching, it stops every agent running that model at the same moment. `wake new alex --model opus
--fallback-model sonnet` keeps that agent working through it. Nothing announces that a failover
happened.

**`--worktree <name>`** puts the agent in its own git worktree, on `new` and on `manager`. Wake runs
`git worktree add` itself: the tree lands at `<repo>/.wake/worktrees/<name>` on a branch called
`wake/<name>`, anchored to the repository root however deep in it you were standing. Name a branch
that already exists and it is checked out rather than refused, which is how you put an agent back on
work you had started.

Two things worth knowing before you use it on real work.

**A worktree that cannot be created refuses the agent.** Not a git repository, a name already taken,
a branch checked out somewhere else — all of them stop the spawn and print git's own words. It never
quietly starts the agent in the shared tree, because that is the one outcome asking for a worktree
was meant to rule out.

**Wake never removes one.** A worktree holds uncommitted work, so removing it on park, on `⌃Q` or on
`wake stop` would be a second irreversible verb in a product whose whole lifecycle chapter turns on
there being exactly one. `git worktree remove` when you are done with it.

**An agent can also move itself.** `EnterWorktree` and `ExitWorktree` are ordinary tools every agent
has, so one may enter a worktree mid-conversation without being asked to. Wake follows it: the roster
row, the status bar's branch and — the one that matters — the directory a park writes down all track
where the session actually is, re-read from every turn.

**`--add-dir <dir>`** lets an agent's tools reach a directory outside the one it was started in, on
`new` and on `manager`. Repeat the flag for a second: `wake new alex --add-dir /repos/shared-lib
--add-dir /repos/docs`. Without it every agent is confined to its spawn directory, which is the right
default and the wrong one for an agent that has to read a sibling repository — and it matters more
now that an agent can move itself into a worktree.

The path has to be absolute. A shell has already expanded `~` and `$PWD` for you, so
`--add-dir "$PWD/../lib"` is the usual spelling and works; a bare `lib` is refused rather than
resolved, because the daemon that would resolve it is one process for the whole machine and is
almost never standing where you are. From `/new` in the composer, a relative path *is* resolved for
you — against Wake's own directory, the same way `in <dir>` is, `~` included.

One caveat on `manager`. That session runs with Claude Code's built-in tools removed — no shell, no
file access — so widening the directories it may reach has nothing to reach with. It is accepted
there because it may still affect which `CLAUDE.md` files that session loads, which is not something
this build has measured.

**`--debug-file <name>`** turns on Claude Code's debug logging for **that one agent** and writes it
to a file of that name. `wake new alex --debug-file alex` gives you `~/.wake/debug/alex.log` — in the
fleet's own directory, so a named fleet's logs are under that fleet's.

It is a **name and not a path**, and that is deliberate. The log is a file a `claude` process
creates and truncates, and unlike anything an agent writes there is no transcript line and no
permission ask to see it happen — so the socket carries what to call it and Wake decides where it
goes. Letters, digits, dot, dash and underscore; Wake adds the `.log`. Nothing stops you giving two
agents the same name, and `--debug-file` truncates, so give them different ones.

**`--debug <categories>`** narrows that log — `--debug api,hooks` keeps two, `--debug '!1p,!file'`
drops two. It needs `--debug-file` beside it and says so if it is missing: on its own, in the
headless mode every Wake agent runs in, `--debug` writes no log anywhere that can be read.

**Neither survives a park.** An agent you `⌃C` and bring back comes back with no added directories
and no logging, so start it again with the flags if you still want either. That is the opposite of
`--max-budget-usd`, which does survive — a cap is a thing nothing can put back afterwards, and these
two you can simply ask for again.

**`wake status`** answers three different questions honestly: a daemon with a fleet, no daemon and
nothing alive, or — the interesting one — no daemon but processes it left behind, which it reports
as orphans rather than pretending they are gone.

**`wake stop` is irreversible.** See [chapter 4](04-lifecycle.md).

## `/…` — in the composer

Wake owns a short, closed list of slash commands:

```
/resume [<name>|all]           bring a parked session back
/new [<name>] [in <dir>]       start an agent without leaving the room; takes the spawn flags too
/name [@who] <new-name>        rename a session
/task [@who] <what it is on>   set the label beside its name
/mcp                           the MCP panel claude cannot draw headless
/effort                        pick a reasoning level — bare, no argument
/model                         pick a model — bare, no argument
```

`/new` takes six of the shell verb's flags — `--worktree`, `--add-dir`, `--debug-file`, `--debug`,
`--max-budget-usd` and `--fallback-model` — as in `/new sydney --worktree fix-42 --max-budget-usd 5
in ~/api`, in any order. The spend pair is here because it has no runtime command: an agent spawned
without a budget is uncapped for its whole life, where `/effort` and `/model` fix their spawn gap
one turn later — which is why those two stay shell-only, refused with the usage line rather than
ignored.

Two differences from the shell. A flag's value is a **single word** here, so a directory with a
space in it can only be given to `in <dir>`, which is the last thing on the line and takes the rest
of it. And a **relative** `--add-dir` is resolved for you, against Wake's own directory, with `~`
expanded — nothing else on the way from a composer would do it.

**Everything else beginning with `/` goes straight to the agent**, exactly as in Claude Code —
`/model opus`, `/clear`, `/compact`, `/context`, and every command you have in `.claude/commands/`.
Wake claims those words and passes the rest through untouched, and there is a guard whose whole job
is to keep it that way. If Wake ever swallowed one of yours you would lose a Claude Code feature
with no error, which is why the guard is over *what Wake must not claim* rather than over what it does.

`/effort` and `/model` are the two exceptions worth knowing: **bare**, they open a menu, because bare
is the form a headless `claude` does nothing with. **With an argument** — `/effort max`, `/model
opus` — they reach the agent byte for byte like anything else.

One consequence: if you have your own `.claude/commands/resume.md`, Wake's `/resume` shadows it.

Note `/clear` **changes the session id**, which Wake tracks — the conversation continues under a new
id and the roster follows it.

## Completion — Wake finishes the word for you

Start a draft with `/` or `@` and a menu appears above the conversation with what could finish it.

```
/re          →   /resume
@al          →   @alex          a live agent
@src/ma      →   @src/main.go   a file, relative to that session's directory
```

| Key | Does |
|---|---|
| `⇥` | Take the highlighted one |
| `⌃N` `⌃P` | Move down and up the list |
| anything else | Narrows it — the menu follows what you type |

`↵` still sends, always. The menu appears because you started typing rather than because you asked
for it, so it never changes what enter does — and it is about the word **under your cursor**, so
moving off that word closes it and hands `⌃N`/`⌃P` back to the draft as line keys.

**The `/` list is real, not a guess.** Every `claude` session tells Wake its whole command set when a
turn starts — its own, every `.md` file in your `.claude/commands/`, and your Claude Code **skills**
(they ride in the same list) — so the menu shows what *that agent* actually answers to, with the
session's own commands and skills **first** and Wake's own commands after. An agent that has not taken
a turn yet has told Wake nothing, so you get Wake's commands and no more until it does.

**The `@` list is names first, then paths**, which is the same order the router reads them in: a live
agent wins, and anything else is a file. Paths are relative to the session's own directory, one
directory at a time — `⇥` on a directory steps into it. A big directory is read up to a limit and
the menu says how many it left out; keep typing to narrow it. The names appear at once and the
paths a moment later, because the directory is read off the goroutine that draws: a file share that
has stopped answering costs you the file list and never the window.

## `!…` — a shell line

```
!git status
!ls -la
```

Runs the line and shows you the output in the conversation you are in. It is asked before anything
else, so `!ls` works in a conversation with an agent that has ended.

Output is capped so a runaway command cannot fill your transcript, and there is a deadline — but the
deadline signals the whole process group rather than one pid, so a command that spawned children
does not leave them behind.

**This is not a terminal.** No interactivity, no TTY, no shell session that persists between
commands. It is one line, run, and its output.

## `@…` — addressing

In the room, `@name` addresses one agent and `@all` addresses everybody. With no `@`, the room
refuses the message and says so — in a group chat with thirty members, "send to whoever" is not
something you can mean.

`@` is overloaded exactly as in Claude Code: a live session name wins, and anything else — like
`@src/main.go` — is a file path and passes through to the agent, which resolves it the way you are
used to. Wake completes both as you type them (above) and shows you what it resolved to.
