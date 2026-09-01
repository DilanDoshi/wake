# 7. When something goes wrong

Wake tries to make every refusal say **when** you can do the thing, rather than only that you
cannot. If you get a message that just says no, that is a bug worth reporting.

## "session … is parked, so its process has stopped"

You tried to `wake attach` a parked session. Parking stops the process, so there is no live
conversation to attach to.

Bring it back with `/resume <name>` from inside Wake, then attach.

## A fork or a wake was refused

Both check things first, and both name the condition:

- **"mid-turn / running a tool / blocked"** — only an idle agent whose turn has finished is recorded
  as safe to fork. Wait for it, or `⎋` to interrupt first.
- **"this daemon does not know where session … ran"** — Wake will not resume a session without
  knowing its directory. Claude locates a transcript by the directory it started in, so resuming in
  the wrong one produces an *empty* session under the right id with nothing saying the history is
  missing. Refusing is the safe answer.
- **"something already holds this session id"** — a process is still using that id. Wake will not
  start a second one, because two live processes on one id corrupt the transcript **silently**, with
  no error and no way to detect it afterwards.

## `⌃C` was refused on a blocked agent

Deliberate. Parking closes stdin, and closing stdin under an outstanding permission question is
recorded by the CLI as *you denying it* — a denial you never made, which survives the wake and
cannot be distinguished from a real one afterwards.

Press `⎋` to interrupt the ask, then `⌃C`.

## A key does nothing

Check it is on the list in [chapter 3](03-keyboard.md) — if it is not there, Wake does not bind it,
and the list is enforced by a test rather than maintained by hand.

If it *is* on the list and does nothing, the likely causes are your terminal or a multiplexer:

- **`⌃C` kills Wake instead of parking.** Your terminal is delivering it as a signal. Wake puts the
  terminal in raw mode to prevent that; under tmux or ssh it is worth checking first.
- **`⌃W`, `⌃R`** are claimed by some terminals and shells (delete-word, reverse-search).
- **`⇧⇥`** is the most likely of all to be intercepted.

## `wake status` says there is no daemon, but agents are running

That is the orphan case and `wake status` reports it on purpose: the daemon died — crashed, killed,
or the machine rebooted — and left its children behind.

Starting Wake again will reap them on the way up. Their conversations are on disk regardless.

## Wake is using CPU while nothing is happening

That is a bug, not tuning. Wake is meant to be cheap to leave open next to 15–30 `claude` processes;
anything that costs per frame rather than per change is a design regression. Report it with what was
on screen.

## The hint line looks cut off

It is. The legend is wider than most panes and truncates from the right, ordered so the keys you
need most survive. Note it is cut to the width of the **pane**, not the terminal — with a
conversation open beside the room, each side of a 120-column terminal gets about 50.

## Something looks wrong and you want to report it

The useful report is: **what you did, what you saw, what you expected**, your terminal, and
`claude --version`. A screenshot beats prose for anything visual.

`docs/live-testing.md` is the standing list of things only a person at a real terminal can check —
if what you hit is on it, saying so is genuinely useful, because none of that list has been
confirmed by a human yet.

## Recovering a conversation Wake cannot reach

Your conversations are Claude's, not Wake's: `~/.claude/projects/<directory>/<session-id>.jsonl`.
`wake status` prints the first characters of each id. Even if Wake has lost track of a session, the
transcript is a file you can read.
