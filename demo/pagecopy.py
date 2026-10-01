"""The words on the landing site: one entry per recorded beat, and the notes
for what landed with no clip of its own. page.py lays them out; nothing here
knows about HTML beyond the inline <code> the prose uses."""

from collections import namedtuple

# glyph/who/label is the room's own grammar for attributing a line, which is
# what each section is headed in. home: whether the front page carries it too;
# the features page carries every shot.
Shot = namedtuple("Shot", "clip start dur glyph who label heading prose home")

SHOTS = [
    Shot(
        "03-broadcast",
        9.5,
        13,
        "●",
        "you",
        "the room",
        "One message. Six agents.",
        "<code>@all</code> reaches every agent in the room at once. They pick up "
        "their own piece of the work and answer in one thread — and they stay "
        "quiet unless they have something to say, which is the only reason a room "
        "of thirty is readable at all. An unaddressed message goes to the manager "
        "instead, deliberately: a manager told to report on the fleet by a message "
        "it also received has been given the same job twice.",
        True,
    ),
    Shot(
        "24-teams",
        1.0,
        33,
        "▪",
        "backend",
        "maya · nora · alex",
        "Name a team. Talk to all of it.",
        "<code>/team</code> files an agent under a name you choose. The roster "
        "sections itself by team, the board does the same, and "
        "<code>@backend</code> reaches every live member at once — only they "
        "receive it. A team is a tag, not a directory: agents in three repos can "
        "be one team, and moving one is a single command. The manager can sort "
        "the fleet into teams and address one with its own tool.",
        True,
    ),
    Shot(
        "25-done",
        0.5,
        30,
        "✔",
        "omar",
        "web · 429 client state",
        "Finished is a state you can see.",
        "When an agent finishes a turn you watched, its roster row gets a "
        "<code>✔</code> and the strip counts how many are done — so the fleet's "
        "progress is legible at a glance rather than inferred from who went "
        "quiet. The conversation keeps Claude Code's own done line, "
        "<code>✻ Cooked for 1m 59s · done 6:48 PM</code>. Send it more work and "
        "the mark clears until that turn ends too: it is a note about what you "
        "saw, never a state the daemon invents.",
        True,
    ),
    Shot(
        "12-filter",
        1.5,
        21,
        "●",
        "you",
        "group chat › @omar",
        "One name narrows the room.",
        "A lone <code>@omar</code> in the composer narrows the group chat to omar's "
        "thread — his lines, the manager's, every broadcast, and what you said to "
        "him — for as long as it is the target. It is a view, not a mode: backspace "
        "the name and the room widens again; <code>@omar hi</code> still routes to "
        "omar alone, and <code>@omar /model</code> still configures him. Nothing "
        "leaves the record, only the frame, and the header says so while it is "
        "narrowed.",
        False,
    ),
    Shot(
        "05-blocked",
        0,
        13,
        "▲",
        "omar",
        "web · 429 client state",
        "The one that needs you, found for you.",
        "Agents rank themselves by whether they need you. When one blocks on a "
        "permission, it goes to the top of the roster and turns amber without "
        "anyone watching for it — and <code>⌃X</code> jumps straight to whoever is "
        "blocked. The ask is answered <em>in the pane that raised it</em>, with the "
        "turn that led to it still on screen, never in a modal thrown over "
        "everything.",
        True,
    ),
    Shot(
        "26-question",
        1.0,
        26,
        "?",
        "priya",
        "cli · --rate-limit flag",
        "A question gets answered where it was asked.",
        "When an agent asks you something, the room says so in one yellow line, "
        "and <code>⌃X</code> takes you to the card in that agent's own "
        "conversation — a wizard with a review step before anything is sent. "
        "Answer it and the room's line resolves in place: purple, with your "
        "answer folded under it, instead of a stale warning sitting above a "
        "second message.",
        False,
    ),
    Shot(
        "07-grid",
        1.0,
        22,
        "○",
        "priya",
        "cli · --rate-limit flag",
        "Open one and you're in Claude Code.",
        "A conversation is the real thing: the same slash commands (the ones that "
        "session advertised, not a guess), the same rendering, and a status bar "
        "naming the branch, the model, the effort and the context left — each read "
        "back off the wire rather than assumed. Panes are columns, each splittable "
        "once — bounded on purpose. Wake is not trying to become a multiplexer; the "
        "group chat is the product and the panes are substrate.",
        True,
    ),
    Shot(
        "09-manager",
        13,
        15,
        "◐",
        "manager",
        "feat/rate-limit",
        "Tell the agents working on the api to…",
        "Every room seats a manager with a deliberately short list of verbs: send, "
        "interrupt, spawn, and sort agents into teams. “Tell the agents working on "
        "the api” is the manager listing the fleet, deciding which rows match by "
        "their labels, and sending to each one — real tool calls against a real "
        "server, which is why the roster rows light up underneath them. When the "
        "agents are already a team, it can address the team in one call.",
        False,
    ),
    Shot(
        "10-board",
        1.0,
        15,
        "▪",
        "the fleet",
        "one row each",
        "Thirty agents don't fit in panes.",
        "<code>/board</code> is the overview: one row per agent carrying its state, "
        "its label and its own last line, sectioned by team. <code>⇥</code> flips "
        "the same overview into a tiled wall — every agent's live transcript at "
        "once, shelved by team — and both stay view-only. The board is for "
        "triage, so it carries the triage verbs and nothing else: jump to one, "
        "park one, leave.",
        False,
    ),
    Shot(
        "14-workflow-view",
        0.5,
        30,
        "◈",
        "iris",
        "count-lines",
        "A workflow, drawn.",
        "When an agent runs a dynamic workflow, it becomes one row under that "
        "agent — agents done out of started. <code>↵</code> opens Wake's own view "
        "of it, since headless Claude Code cannot draw its <code>/workflows</code> "
        "menu: the run's phases and agents, each agent's prompt, and what it did, "
        "read off its own transcript. A run can be stopped from there, or its "
        "script saved as a slash command.",
        False,
    ),
    Shot(
        "11-leaving",
        1.5,
        17,
        "·",
        "you",
        "leaving",
        "Close the terminal. They keep working.",
        "<code>⌃O</code> arms the detach and <code>↵</code> confirms it — a "
        "different key, because a same-key confirm fires on exactly the reflex the "
        "arm exists to catch. Then the client is gone and the fleet is not: "
        "<code>wake status</code> from a bare shell still lists every agent, "
        "including the one still blocked. Run <code>wake</code> again and the room "
        "comes back, re-derived from Claude's own transcripts rather than from "
        "anything Wake kept. When you do want it all to stop, <code>⌃Q⌃Q</code> "
        "parks the whole fleet — and waits for the daemon to say it did before the "
        "window closes.",
        False,
    ),
]

# (title, prose) — what landed with no clip of its own. One sentence each.
ALSO = [
    (
        "/color",
        "Seven named hues. An agent's turns in the room, the composer it types "
        "into and its roster row are told apart by more than name text.",
    ),
    (
        "/mcp, live",
        "Claude Code's own MCP menu for one agent, read from the running session: "
        "each server's status, with Authenticate, Reconnect and Enable where they apply.",
    ),
    (
        "Your claude.ai connectors",
        "Agents open with Claude's <code>initialize</code> handshake, so the "
        "connectors you signed in to on claude.ai reach headless sessions too.",
    ),
    (
        "@ in a conversation",
        "The fleet's live peers, the machine's other Claude sessions, "
        "<code>@agent-&lt;type&gt;</code> subagents, then files by fuzzy search.",
    ),
    (
        "/resume, searchable",
        "A type-to-search picker over parked and on-disk sessions. A resumed "
        "session comes back under the name it had.",
    ),
    (
        "↑↓ prompt history",
        "Claude Code's own recall key, per pane — derived from the transcript, so "
        "it works on a reattach and in a conversation this window never opened.",
    ),
    (
        "Drag, double- and triple-click",
        "Wake owns the mouse, so it owns selection: a drag, a word or a whole row, "
        "on every surface it draws, copied on release.",
    ),
    (
        "esc esc rewind",
        "Idle and empty, a second <code>⎋</code> opens a picker of earlier prompts "
        "— Claude Code's own rewind, read tree-aware off its own transcript.",
    ),
    (
        "Subagents in the sidebar",
        "Running dispatches list under the agent that started them, with what each "
        "has spent; <code>⌃D</code> opens one's transcript in the pane.",
    ),
    (
        "The task board",
        "An agent's own <code>TaskCreate</code> checklist, pinned above the "
        "composer where Claude Code draws it, folded live from the ops.",
    ),
    (
        "Named fleets",
        "<code>wake --fleet &lt;name&gt;</code>: several fleets in one directory, "
        "each on its own socket, listed by <code>wake fleets</code>.",
    ),
    (
        "--worktree · --add-dir",
        "Start an agent in a fresh git worktree Wake creates and never removes, or "
        "widen what its tools may reach beyond its own directory.",
    ),
    (
        "Budget and failover",
        "<code>--max-budget-usd</code> and <code>--fallback-model</code> — thirty "
        "unbudgeted agents and one overloaded model both matter only at fleet scale.",
    ),
    (
        "/compact, drawn",
        "Compaction draws its own line while it runs, then the token counts it "
        "went from and to.",
    ),
    (
        "/quit · /login",
        "End one agent and drop its row from this window; check auth status and "
        "hand sign-in to a terminal.",
    ),
    (
        "Peer messages",
        "A cross-session message from one agent to another shows in the room, "
        "headed by the sender — and cannot be forged from a pasted envelope.",
    ),
]
