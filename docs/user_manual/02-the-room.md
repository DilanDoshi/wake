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
| anything with no `@` | the **manager** — the room seats one by default |

An unaddressed message goes to the **manager**, the session Wake seats in every room to field exactly
this (`@manager` reaches it by name too). Only when there is no manager at all does an unaddressed
message resolve to nobody — in a group chat with thirty members, "send to whoever" is not otherwise
a thing you can mean.

**`@` is overloaded, exactly as in Claude Code.** `@sydney` is a session if a live session is
called that; otherwise `@src/main.go` is a file path and passes straight through to the agent.
Live names win, and Wake shows you what it resolved to.

### Narrowing the room to one agent

Type a lone `@name` and the room **narrows to that agent's thread** — their lines, every broadcast,
your own messages to them, and the messages sent to or from them — for as long as it is what the
composer is addressing. The manager's own lines, and what you said to the manager, are not in it. It is a view filter, not a mode: `@sydney hi` still routes to sydney, and the room widens
again when you change or clear the target. `⌃A` **shows all** — it widens a narrowed room back to
every agent while still addressing `@sydney`, and a second `⌃A` re-narrows. `/groupchat-filter off`
flips the default so a lone `@name` no longer narrows (`⌃A` then narrows on demand); `on` restores
it, and a bare `/groupchat-filter` reports which is set.

### Channels: one team's room

The roster's team headers are **channels**. Put the cursor on one with `⇧↑↓` and press `↵`, or click
it, and the room shows only that team's thread — its members' lines, the turns you sent them, the
messages sent to or from them, every broadcast, and the manager, which has a seat in every channel —
titled with the team's name instead of `group chat`. It lasts until you pick another team or
`all`, the header pinned at the top of the sidebar, which is where every window starts.

Inside a channel the room works as it always does. A lone `@name` narrows further (`backend › @kit`),
`⌃A` widens back to the channel, and where a message goes does not change: a draft with no `@` still
goes to the manager. Conversations open as usual and the channel stays. A team that leaves the roster
takes its channel with it, and the room goes back to everyone.

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
┌───────────────────────────────────────────┬──────────┐
│                 the room                   │ activity │
│                                            │   (⌃R)   │
├───────────────────────────────────────────┤          │
│               composer                     │          │
└───────────────────────────────────────────┴──────────┘
```

**Left — workspaces (hidden for now).** There was a left sidebar with one row per directory you
have agents in, and its unread counts; it is hidden for now and returns with a future
multi-group-chat version. Until then the awareness strip's `#workspace` segment names the directory
of the conversation you are in.

**Right — activity.** One row per agent, ordered by whether it needs you: blocked agents first
with the tool they are waiting on, then working agents stalest-first, then idle. At 30 agents in a
short terminal the list gets cut — and it is cut from the *idle* end, so what needs you is never
what disappears. The wheel over the sidebar scrolls the rest into view.

At the top of the sidebar is a **search bar** (three rows). Click it and type a few letters of a name:
the sidebar narrows to the agents that match, in the order it already had, and a team with no match drops
out. The count beside what you typed says how many matched (`5/41`). `↑↓` or `⇧↑↓` move the cursor over
the matches, `↵` opens the one it is on exactly as clicking its row does (`⌃B` below, `⌃D` in the focused
pane), and `esc` clears the filter and gives the keys back to the composer, and `⌃C` only lets go of it without
parking anything. A click anywhere else or any
key the bar does not read lets go of it and leaves the filter standing, lit, until you clear it. `⌃F` is the
same search as a box over the composer; it never focuses the bar.

Each section of the roster is drawn as a **box**: a team's name is the top edge, its agents stand between
walls, and a closing row ends it; the agents above the first team sit in a box under the pinned `all`. A
row is four columns narrower inside its box, so a long name is cut at the right wall and the figures at its
end drop sooner. **Click an agent's row** and it opens in a new column at the right end of the grid, next to
the sidebar (in a window under 120 columns it takes the place of the pane on screen, as `⌃D` does); an agent
already on screen is only focused, and a click on a team's header still picks that channel. **Drag a row out
of the sidebar** to place it: the agent's name follows the pointer and the pane it will become is outlined.
Let go on the lower half of a pane and it opens below that pane (one split to a column, the room included);
let go anywhere else and it opens in a new column. `esc`, any other key, a resize or letting go back on the
sidebar cancels the drag, and `esc` is nothing else — it does not interrupt the agent in the pane with the
keys. A **Shift-click** on a row opens it below the focused pane, as `⌃B` does, in a terminal that delivers
one. A drag over a row's own name moves the agent instead of selecting its text; the headers and the blank
sidebar cells still select.

**Middle — the room**, and below it the composer, outlined in orange so you can find where you type.

The box **grows as you type**. An empty one is a single row; it gains a row for every line the draft
wraps onto or every newline you put in with `⌥↵`, up to ten — and less than ten in a pane too short
to spare them, because the conversation above keeps its floor. Past that the draft scrolls under the
cursor, so what you are typing now is always the part you can see; the wheel over the box scrolls
the rest into view without moving the cursor. It shrinks back as you delete.

Open a conversation with `⌃D` and it appears **beside** the room, with a divider you can drag. The
right sidebar stays open — picking a row is a request to read that conversation, not to put the
fleet away, and `⌃R` is what closes it when you want the columns. Below 120 columns the
conversation takes the whole pane instead of splitting, because a split there leaves each side too
narrow to read.

## What a working agent says it is doing

While an agent works, its conversation, the room and a `/board` row each name what it is on:

- **The working line** (`✻ Sailed for 49s` in the room, `✻ Sailing… (49s · ↓ 11k tokens)` in a conversation) carries
  the item the agent has marked in progress on its own task list, in its words, or by its subject when it gave none; an
  agent with no list gets a word from Wake's own pool (`✻ Sailing…`). **`/board`** shows that item, or else the tool
  running (`Bash`), then the agent's last line of prose.
- **In a conversation the list itself** is pinned above the composer: `☑` done, `■` in progress, `☐` to do.
- **Most agents keep no list.** Claude Code offers its task tools by default only on older models (the list is in
  "Task tool availability" in its tools reference, `code.claude.com/docs/en/tools-reference`), so an agent on a newer
  model shows a pool word and the running tool, and no list. Wake follows that default and does not turn the tools on.
- **To have every ordinary agent keep one**, export `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` before the first `wake` of a
  fleet. Wake passes your environment to the agents it starts (they take the one their fleet's daemon began with, so an
  already-running fleet keeps what it has). It can cost: the model may spend several extra calls building and ticking
  the list, and the tools' definitions sit in each agent's context. Claude Code's tools reference lists its other
  opt-ins; this variable is the one Wake passes through.

## Answering a blocked agent

When an agent needs a decision it becomes a card at the top of the room rather than a line in it.
Three kinds arrive through one mechanism:

- **A permission request** — it wants to run a tool.
- **A plan** — it wants approval to proceed.
- **A question** — it is asking you to choose between options. Its `Other…` row takes your own
  words: type them and press `↵` (over an open `@` or `/` menu it completes the word first), or press
  `↑` or `esc` to go back to the options with the words kept in that row, and choose it again to edit them. A question that takes several draws a box on each: `↵`, the
  option's digit or space ticks one without moving on (a filled `Other…` row unticks the same way), and
  the Next or Submit row under them walks on.

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
