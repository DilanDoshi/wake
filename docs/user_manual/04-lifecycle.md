# 4. The lifecycle

There are four ways to stop something in Wake and they are genuinely different. Getting them
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

## The four verbs

| | What it does | Recoverable |
|---|---|---|
| **`⌃C`** | Park the agent in front of you | **Yes** — `/resume <name>` |
| **`⌃Q`** | Park the whole fleet, then quit Wake | **Yes** — next `wake` offers it back |
| **`⌃O`** | Detach: close Wake, leave everyone running | Nothing stopped |
| **`wake stop`** | End every session and the daemon | **No** |

`wake stop` is the only irreversible one. Nothing brings a *stopped* session back — that is exactly
the gap park was built to close.

## Parking one agent

`⌃C` on the conversation with the keys. The process stops; the row stays in your roster, marked
parked; the transcript is untouched.

Bring it back with **`/resume sydney`** typed into the composer, or **`/resume all`**, or bare
**`/resume`** which lists what is parked.

**`⌃C` is refused on a blocked agent**, and the refusal points you at `⎋`. That is not fussiness:
parking closes the agent's stdin, and closing stdin while it is waiting on a permission question is
recorded by the CLI as *you denying it* — a denial you never made, which survives the wake and
cannot be told apart from a real one afterwards. Interrupt with `⎋` first, then park.

## Parking everything and quitting

`⌃Q` parks the fleet and exits. The next `wake` sees what was parked and offers it back:

```
1 session parked: @kwame.
/resume <name> brings one back, /resume all brings them all back.
```

The offer is an offer, not an automatic restore. Waking N sessions the instant Wake starts would
put N process launches in front of everything else it needs to do.

**Anything still mid-turn when you press `⌃Q` gets a grace period to finish.** If it does not finish
in time it is killed rather than parked, and it will not be in the offer.

## Detaching

`⌃O`. Wake closes, the fleet keeps working, your terminal is returned to you sane. Come back with
`wake`.

This is what the background daemon exists for. If you only ever detach, you never lose anything —
but you also leave 30 `claude` processes running, which is what park is for.

## Ending the fleet

```sh
wake stop
```

Ends every session and the daemon. It waits for turns to finish rather than killing mid-thought,
and it refuses to claim success unless it can prove the fleet is actually down.

**It also forgets the park book** — a deliberate ending ends the parked half too. So `wake stop`
does not leave you a fleet to resume tomorrow. `⌃Q` does.

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
