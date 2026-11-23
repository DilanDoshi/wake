# 4. The lifecycle

There are several ways to stop something in Wake and they are genuinely different. Getting them
confused is the one mistake here that costs work, so this chapter is short and worth reading once.

## The distinction that matters

> **Park is about the agent. Detach is about you.**

**Park** stops an agent's `claude` process. It stops using CPU, memory and battery. Its conversation
stays on disk, and `/resume` brings it back with full context. The agent is not running and not
thinking.

**Detach** closes Wake. Every agent keeps running exactly as it was — the daemon holds them. Close
the terminal, shut the lid, come back tomorrow, and the work is done and waiting. Nothing about the
fleet changes; only your view of it goes away.

Park pauses an employee. Detach is you going home while they keep working.

## The verbs

| | What it does | Recoverable |
|---|---|---|
| **`⌃C`** | Park the agent in front of you (`/park @who` or `/park @team` from the composer) | **Yes** — `/resume <name>` or `/resume <team>` |
| **`⌃Q⌃Q`** | Park the whole fleet, then quit Wake (the first `⌃Q` arms, the second confirms) | **Yes** — next `wake --fleet <name>` offers it back |
| **`⌃O`** | Detach: close Wake, leave everyone running | Nothing stopped |
| **`/quit`** | End the conversation you are in, and what it started | **No** — the live agent is gone; `/resume` can reopen its transcript |
| **`wake stop --fleet <name>`** | End every session and the daemon, forget the park book, remove the fleet | **No** — the same, for every agent, and nothing is parked for later |

`/quit` and `wake stop` end an agent for good: its `claude` process and what it started are gone,
and nothing parks it. What stays is the transcript on Claude's disk. `/resume` can find it and reopen the
conversation in a new process — but not what was running when it ended. That is the gap park was built
to close: a parked agent comes back as itself.

## Parking one agent

`⌃C` on the conversation with the keys. The process stops; the row stays in your roster, marked
parked; the transcript is untouched.

Bring it back with **`/resume sydney`** typed into the composer, or **`/resume all`**, or bare
**`/resume`** which lists what is parked.

**`⌃C` is refused on a blocked agent**, and the refusal points you at `⎋`. That is not fussiness:
parking closes the agent's stdin, and closing stdin while it is waiting on a permission question is
recorded by the CLI as *you denying it* — a denial you never made, which survives the wake and
cannot be told apart from a real one afterwards. Interrupt with `⎋` first, then park.

## Parking or quitting a team

**`/park @backend`** parks every live agent on the backend team at once. An agent blocked on a
permission question is skipped, for the reason above, and the notice names it. Once every one is
parked, the notice says `@backend is parked - /resume @backend brings it back`.

**`/resume backend`** (or `@backend`) wakes every parked member of the team, whether you parked it
in this daemon or it came back from a `⌃Q`.

**`/quit @backend`** ends the team's live agents the way `/quit` ends one, so it asks first. A menu over the composer
names who would end, with its cursor on **never mind**: `↓` then `↵` quits them, and `↵` alone or `esc`
does nothing. Members already parked stay parked. Each row leaves your roster once its ending lands.

In the room, `@backend /park` and `@backend /quit` do the same. A live agent that happens to share the
team's name wins it, as it does for `@backend` messages. The manager is never part of a team's verbs:
`/manager` parks it and `/manager-stop` ends it.

## Parking everything and quitting

`⌃Q` parks the fleet and exits, and it is armed: the first press arms it (the cue reads
`⌃Q park all & quit`), a second confirms. The next `wake --fleet <name>` sees what was parked and offers it back:

```
1 session parked: @kwame.
/resume <name> brings one back, /resume all brings them all back.
```

The offer is an offer, not an automatic restore. Waking N sessions the instant Wake starts would
put N process launches in front of everything else it needs to do.

**Anything still mid-turn when you press `⌃Q` is interrupted, as `⎋` would, and parked.** The turn
ends at once and the agent is in the offer; after `/resume`, tell it `continue` and it picks the turn
up. An agent waiting on a permission question has the question withdrawn, not denied. A message still
queued behind a turn is cancelled, and nothing can bring it back afterwards, so the line Wake leaves in
your terminal prints its words under the agent it was waiting at. A fleet whose daemon is still the
build before this one lets turns run to the end of a grace period instead, and the line says so.

## Detaching

`⌃O`. Wake closes, the fleet keeps working, your terminal is returned to you sane. Come back with
`wake --fleet <name>`.

This is what the background daemon exists for. If you only ever detach, you never lose anything —
but you also leave 30 `claude` processes running, which is what park is for.

## Ending one agent

`/quit` ends the conversation you are in — it interrupts the agent as `esc` does, stops its process
and what it started, releases the name, and drops the row from your window. The turn in flight
ends at once, what you had typed ahead of it is cancelled, and a question it was waiting on is
withdrawn, not answered no — so `/quit` works on a blocked agent, where `⌃C` does not. A normal `/quit`
is done within a second or two; only an agent that ignores both the interrupt and the stop is killed,
and only after 30 seconds. What it started means the processes Wake can show are the agent's: its own
process group and the groups its background shells and hooks led while it ran. A command that left the
agent before the ending (`nohup cmd &` in a foreground call) is not reached.

A `/quit` also beats a `⌃C` park still waiting for the turn to end: the agent ends and is not parked.

That is the whole difference from `⌃C`: park pauses and `/resume` brings the agent back as itself;
`/quit` ends the live agent. Its transcript stays on disk, so `/resume` can still find the conversation
and open it in a new process. `@who /quit` from the room ends that agent instead. The manager is
refused and pointed at `/manager-stop`, its one ending.

## Ending the fleet

```sh
wake stop
```

Ends every session and the daemon. Each agent is interrupted, then stopped, as `/quit` does it; only
an agent that ignores both is killed, after the same 30 seconds. It refuses to claim success unless it
can prove the fleet is actually down.

**It also forgets the park book** — a deliberate ending ends the parked half too. So `wake stop`
does not leave you a fleet to resume tomorrow. `⌃Q` does.

**A stopped named fleet leaves the list.** Once the daemon is confirmed down, `wake stop` removes the
fleet's directory and its log, and says so (`Removed fleet dev…`); the name is free for a new fleet, and
the transcripts stay where Claude wrote them. With no daemon running, `wake stop --fleet <name>` does the
same, naming each parked session it forgets. It keeps a fleet it
cannot call done — a daemon running on it again, a crash its next daemon has yet to hand over, a file
Wake did not write — and says what kept it. Either way it exits 0, since the stop happened. The unnamed
fleet (`~/.wake` itself) and a socket set by hand have no directory to remove.

`wake fleets` shows what each fleet is, one row each: `running` (its agents and its build), `running · not
answering` (stopping, or wedged), `crashed` (`wake --fleet <name>` recovers its sessions), `parked` (the
agents a `⌃Q` left, or only the manager), or `stopped` (`wake stop --fleet <name>` removes it, or what it
keeps). It reads files, never takes a lock, so it cannot disturb a daemon starting on a fleet.

## Upgrading

```sh
wake upgrade
```

Installs the newest release over the `wake` you ran it as, checked against the release's checksums.
Wake also looks when the room opens, and again when you type in it an hour or more later - never
while it sits untouched - asking GitHub about once a day. A newer release is announced in the notice
row once a day and named at the end of the strip (`↑ wake 0.1.9`) for as long as that room runs, so a
room left open for days still finds out; `WAKE_NO_UPDATE_CHECK=1` turns that off.

**A fleet that is already running keeps the build it started from.** Its daemon is a process, and
replacing the file on disk does not change a process that is running. `wake upgrade` names the
fleets this applies to; a room opened over one says so, and so do `wake status` and `wake fleets`.
To move a fleet onto the new build:

1. `⌃Q⌃Q` in its room — parks everything and ends the daemon.
2. `wake --fleet <name>` — a fresh daemon on the new build.
3. `/resume` — bring back what you parked.

Nothing is lost on the way: parking keeps every transcript, and `/resume` picks each one up where it
stopped.

## What survives what

| | Transcript on disk | Resumable |
|---|---|---|
| Park (`⌃C`, `⌃Q`) | yes | yes |
| Detach (`⌃O`) | yes — nothing stopped | n/a |
| Interrupt (`⎋`) | yes — session lives | n/a |
| Stop (`wake stop`) | yes | **no** |

The last row is worth staring at. Your *conversation* survives `wake stop` — Claude wrote it to
`~/.claude/projects/…` — but Wake has no path back to it. What park adds is the bookkeeping that
makes a stopped process resumable.
