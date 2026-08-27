# 5. Forking

Fork copies a **conversation**, not a process and not a directory.

## What it does

You are talking to `alex`. You have spent forty minutes explaining the codebase, the constraints,
and what you are actually trying to do. Press `⌃F` and you get a second agent — a new session that
already knows everything `alex` knows, and from that instant onward independent. Both keep running.
Nothing you say to one reaches the other.

```
wake fork alex           # from the shell, a name from the pool
wake fork alex backend   # a name you choose
⌃F                       # in the conversation with the keys
```

The fork's own conversation opens when the daemon confirms it, and its header names where it came
from: `@sydney  forked from @alex`.

## Why you would

**Context is the expensive thing.** Without fork, trying two approaches from one understanding means
losing one or re-explaining everything to a fresh agent and hoping you got the setup identical.

- **Branch a good conversation.** The agent has understood the problem and proposed something risky.
  Fork; let one pursue it, keep the other on the safe path.
- **Ask the expensive question twice**, same context, different framing, and compare.
- **Preserve a session before something destructive.** The fork is a snapshot; the parent is
  untouched by anything the fork does.
- **Fan out from one briefing** instead of briefing three agents separately.

Two presses of `⌃F` is the normal case, not a mistake — that is what "explore two approaches"
means, and both forks open.

## What a fork is and is not

**A fork is a snapshot, not a live subscription.** Its lineage is frozen the moment you press the
key. Anything the parent says afterwards never reaches the fork, and vice versa.

**You can fork a running agent.** The parent survives being forked and its next turn works normally;
its transcript is not touched. You can also fork an agent that has already ended, and you can fork a
fork — the chain carries.

**A fork runs in the same directory as its parent.** So two forks of an agent that is editing files
are two agents editing the same working tree, and they will step on each other. For reading,
research and design that is harmless. For "try two refactors" it is not. Giving a fork its own git
worktree is a real feature and is not built.

## What it costs

A fork is a new `claude` process with the parent's whole history as its starting context. It is not
free — you are paying for that context again — and it takes a second name and a roster row.

## When it is refused

Fork asks the daemon, and the daemon refuses what has not been proved safe. Every refusal tells you
**when** you can fork instead of only that you cannot:

- **The agent is mid-turn, running a tool, or blocked.** Only forking an idle agent whose turn has
  finished is recorded as safe. Wait, or `⎋` first.
- **The parent is a session this daemon never held.** Wake would not know which directory to run in,
  and a fork in the wrong directory silently produces an *empty* session with nothing saying the
  history is missing.

A parked agent can be forked — its process has exited, which is the recorded-safe case.
