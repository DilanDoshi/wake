# 2. The room

The room is a group chat over your whole fleet. It is the product's reason to exist, and the
thing that makes 30 agents possible rather than merely permitted.

## Why it is quiet

**You should only see a session's output if it needs something from you or is talking to you.**

That single rule is the difference between a room and a log. Thirty agents all narrating their
work is thirty agents you stop reading. So the room shows you:

- messages addressed to you,
- agents that are **blocked** and waiting on a decision,
- an agent's own closing words when it finishes,

and nothing else. Everything an agent does that is *not* addressed to you shows up in the right
sidebar as activity, not in the room as prose.

## Talking

| You type | Who hears it |
|---|---|
| `@sydney look at the retry logic` | just sydney |
| `@all pause what you are doing` | everybody, with a count of the turns it starts |
| anything with no `@` | **nobody** — the room refuses it and says so |

The room refuses an unaddressed message on purpose. In a group chat with thirty members,
"send to whoever" is not a thing you can mean.

**`@` is overloaded, exactly as in Claude Code.** `@sydney` is a session if a live session is
called that; otherwise `@src/main.go` is a file path and passes straight through to the agent.
Live names win, and Wake shows you what it resolved to.

### What you said from the room shows up in their conversation

A message you route from the room lands in the room *and* in the conversation of everyone it
reached, headed `› you · from the room` and still carrying the `@name` you typed:

```
› you · from the room
@sydney look at the retry logic
```

The mention is kept on purpose. What sydney actually receives is `look at the retry logic` — a
leading `@name` is stripped before sending, because Claude Code expands one before the model sees
it — so without it, reading that conversation back a day later, your line would be
indistinguishable from one you had typed into sydney's own composer. Those are different acts, and
in a conversation what you type is sent *verbatim*, so `@sydney …` typed there really does reach
her with the `@sydney` on it.

An `@all` lands in every conversation it reached, for the same reason: a pane that is missing it
shows an agent answering something that pane never mentions. The room still draws it once — one
broadcast is one thing you said.

**One limit worth knowing.** This applies to conversations Wake is holding — ones you have open,
or had open and closed with `⌃W`. A conversation you have *never* opened is filled from Claude's
own transcript the first time you open it, and that file records what the agent received, so that
one line comes back without the `@name` and without the label. Everything after you open the pane
is labelled.

## The panes

```
┌─────────────┬───────────────────────────────┬──────────┐
│ workspaces  │            the room           │ activity │
│   (⌃G)      │                               │   (⌃R)   │
│             ├───────────────────────────────┤          │
│             │        composer               │          │
└─────────────┴───────────────────────────────┴──────────┘
```

**Left — workspaces.** One row per directory you have agents in, with unread counts.

**Right — activity.** One row per agent, ordered by whether it needs you: blocked agents first
with the tool they are waiting on, then working agents stalest-first, then idle. At 30 agents in a
short terminal the list gets cut — and it is cut from the *idle* end, so what needs you is never
what disappears.

**Middle — the room**, and below it the composer, outlined in orange so you can find where you type.

The box **grows as you type**. An empty one is a single row; it gains a row for every line the draft
wraps onto or every newline you put in with `⌥↵`, up to ten — and less than ten in a pane too short
to spare them, because the conversation above keeps its floor. Past that the draft scrolls under the
cursor, so what you are typing now is always the part you can see. It shrinks back as you delete.

Open a conversation with `⌃D` and it appears **beside** the room, with a divider you can drag. The
right sidebar stays open — picking a row is a request to read that conversation, not to put the
fleet away, and `⌃R` is what closes it when you want the columns. Below 120 columns the
conversation takes the whole pane instead of splitting, because a split there leaves each side too
narrow to read.

## Answering a blocked agent

When an agent needs a decision it becomes a card at the top of the room rather than a line in it.
Three kinds arrive through one mechanism:

- **A permission request** — it wants to run a tool.
- **A plan** — it wants approval to proceed.
- **A question** — it is asking you to choose between options.

The third is why these are not one thing: a bare "allow" answers the first two, but a question
needs your *answer* carried with the approval. Approving a question without answering it tells the
model nobody answered, on a turn that otherwise looks successful.

`⎋` interrupts a blocked agent safely — the session survives, the ask is withdrawn, and the next
message works. No respawn.

## Reading long replies

The room is for what is addressed to you. A long answer belongs in the conversation, so a reply
that would fill more than about two dozen rows of the room collapses to a pointer and you open the
DM to read it. The measure is how tall the reply renders *at the room's current width*, so the same
message shows in full when the room has the whole terminal and folds to a pointer when it is a
narrow column beside a conversation — the room shows more when it has the space and defers to the
DM when it does not.

Inside a conversation, **the last-read marker** remembers where you stopped. Leave mid-way through
a 400-line answer, deal with another agent, come back — a rule sits where you left off, labelled
in the past tense.

Two things about it are deliberate and unlike every chat app:

- **The rules do not move.** A marker that relocates every time you leave erases the landmark
  exactly when you are using it.
- **You see the newest three.** Older boundaries are dropped, because each one is two lines in the
  loudest colour on screen and they all land in the band you scroll back through.

A window resize can remove surplus rules from the screen. That is expected: it only ever removes
ones the cap had already excluded, and it never moves one that survives.
