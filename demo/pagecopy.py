"""The words on the landing site: one entry per recorded beat, and the notes
for what landed with no clip of its own. page.py lays them out; nothing here
knows about HTML beyond the inline <code> the prose uses."""

from collections import namedtuple

# glyph/who/label is the room's own grammar for attributing a line, which is
# what each section is headed in. home: whether the front page carries it too;
# the features page carries every shot, in this order.
Shot = namedtuple("Shot", "clip start dur glyph who label heading prose home")

# The friends on the usage bar: a short loop cut down to the bottom of the window,
# where the picker and the corner are. crop is ffmpeg's w:h:x:y in the
# recording's own pixels.
Friend = namedtuple("Friend", "clip start dur crop caption")

# Each page's <title> and its search-result description, kept short enough for a
# results page to show whole. The keys are the pages, in sitemap order.
META = {
    "index.html": (
        "Wake — run a fleet of Claude Code sessions as a group chat",
        "Wake is a terminal app for running up to 50 Claude Code sessions at once: one "
        "group chat, @name routing, team channels, a manager agent and an "
        "attention-ranked roster.",
    ),
    "features.html": (
        "Wake features — a group chat for your Claude Code agents",
        "Everything Wake does, recorded from the real binary: teams and team channels, "
        "@all broadcasts, a manager agent, a board of live tiles, question cards and a "
        "workflow view.",
    ),
    "releases.html": (
        "Wake releases — release notes for every version",
        "What changed in each Wake release, the terminal for a fleet of Claude Code "
        "sessions, with the one-line installer.",
    ),
}

SHOTS = [
    Shot(
        "24-teams",
        1.0,
        34,
        "▪",
        "backend",
        "maya · nora · alex",
        "Name a team. Talk to all of it.",
        "<code>/team</code> files an agent under a name you choose. The roster "
        "sections itself by team, the board does the same, and "
        "<code>@backend</code> reaches every live member at once — only they "
        "receive it. A lone <code>@backend</code> in the composer narrows the room "
        "to the team's thread while you write to it. A team is a tag, not a "
        "directory: agents in three repos can be one team, and moving one is a "
        "single command. The manager can sort the fleet into teams and address "
        "one with its own tool.",
        True,
    ),
    Shot(
        "29-channels",
        1.5,
        34,
        "▪",
        "backend",
        "channel",
        "Pick a team. The room is theirs.",
        "Every team header on the roster is a channel. Put the cursor on "
        "<code>backend</code> and press <code>↵</code>, or click it, and the group "
        "chat shows only that team's messages, titled <code>backend</code> instead "
        "of <code>group chat</code>, until you pick <code>── all ──</code> or "
        "another team. The manager has a seat in every channel, so you can ask it "
        "from whichever one you are in. Inside a channel the room works as it "
        "always does: a lone <code>@name</code> narrows further, <code>⌃A</code> "
        "widens back, and routing is unchanged. A channel belongs to the window, "
        "so two windows can watch two teams.",
        True,
    ),
    Shot(
        "03-broadcast",
        9.5,
        18,
        "●",
        "you",
        "the room",
        "One message. Six agents.",
        "<code>@all</code> reaches every agent in the room at once. They pick up "
        "their own piece of the work and answer in one thread — and they stay "
        "quiet unless they have something to say, which is the only reason a room "
        "of fifty is readable at all. An unaddressed message goes to the manager "
        "instead, deliberately: a manager told to report on the fleet by a message "
        "it also received has been given the same job twice.",
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
        "Every room seats a manager, and a bare <code>wake</code> opens with it "
        "alone — ask it for agents and it starts them. Its verbs are deliberately "
        "few: send, interrupt, spawn, and sort agents into teams. “Tell the agents "
        "working on the api” is the manager listing the fleet, deciding which rows "
        "match by their labels, and sending to each one — real tool calls against "
        "a real server, which is why the roster rows light up underneath them. "
        "When the agents are already a team, it addresses the team in one call. "
        "Other Claude sessions know it as <code>manager-&lt;fleet&gt;</code>, and "
        "the end of that name is yours.",
        True,
    ),
    Shot(
        "07-grid",
        1.0,
        38,
        "○",
        "priya",
        "cli · --rate-limit flag",
        "Mention one. Open one, and you're in Claude Code.",
        "<code>@priya</code> reaches one agent, and the reply comes back into the "
        "room. Open that agent and the conversation is the real thing: the same "
        "slash commands (the ones that session advertised, not a guess), the same "
        "rendering, and a status bar naming the branch, the model, the effort and "
        "the context left — each read back off the wire rather than assumed. Panes "
        "are columns, each splittable once — bounded on purpose. Wake is not trying "
        "to become a multiplexer; the group chat is the product and the panes are "
        "substrate.",
        True,
    ),
    Shot(
        "05-blocked",
        0,
        23,
        "▲",
        "omar",
        "web · 429 client state",
        "The one that needs you, found for you.",
        "Agents rank themselves by whether they need you. When one blocks on a "
        "permission or a question, it goes to the top of the roster and turns amber "
        "without anyone watching for it. <code>⌃X</code> jumps to the next agent "
        "that needs you, wherever it is — press it again for the one after. The ask "
        "is answered <em>in the pane that raised it</em>, with the turn that led to "
        "it still on screen, never in a modal thrown over everything.",
        True,
    ),
    Shot(
        "10-board",
        1.0,
        23,
        "▪",
        "the fleet",
        "live tiles",
        "Fifty agents don't fit in panes.",
        "<code>/board</code> is the overview: one row per agent with its state, its "
        "label and its own last line, sectioned by team. <code>⇥</code> turns it "
        "into a wall of tiles, three across, showing every agent's live transcript "
        "at once, shelved by team. A fleet taller than the screen scrolls instead "
        "of shrinking the tiles, and the selection always stays in view. Both views "
        "are view-only. The board is for triage, so it carries the triage verbs and "
        "nothing else: jump to one, park one, leave.",
        True,
    ),
    Shot(
        "08-fork",
        0.5,
        17,
        "○",
        "juno",
        "fork of maya",
        "Branch a conversation. Keep both.",
        "<code>⌃F</code> forks an agent: a new session with the same history so "
        "far, working beside the original — a second approach without losing the "
        "first. A fork is a snapshot made with Claude's own fork, the one way to "
        "branch that never leaves two processes writing one conversation, and "
        "<code>wake import</code> adopts a session you started by hand the same "
        "way.",
        True,
    ),
    Shot(
        "11-leaving",
        1.5,
        19.5,
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
        "window closes. A daemon that is signalled parks the fleet the same way, "
        "and one that crashes leaves a record the next one parks from, so "
        "<code>/resume</code> brings the fleet back either way.",
        True,
    ),
    Shot(
        "12-filter",
        1.5,
        21.5,
        "●",
        "you",
        "group chat › @omar",
        "One name narrows the room.",
        "A lone <code>@omar</code> in the composer narrows the group chat to "
        "omar's thread — omar's lines, every broadcast, what you said to omar, and "
        "what was sent to or from omar — for as long as it is the target, and a "
        "lone <code>@team</code> does the same for a team's live members. It is a "
        "view, not a mode: backspace the name and the room widens again; "
        "<code>@omar hi</code> still routes to omar alone, and <code>@omar "
        "/model</code> still configures it. <code>⌃A</code> widens the room while "
        "still addressing them, and <code>/groupchat-filter off</code> flips the "
        "default. Nothing leaves the record, only the frame, and the header says so "
        "while it is narrowed.",
        False,
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
        False,
    ),
    Shot(
        "26-question",
        1.0,
        30,
        "?",
        "priya",
        "cli · --rate-limit flag",
        "A question gets answered where it was asked.",
        "When an agent asks you something, the room says so in one yellow line, "
        "and <code>⌃X</code> takes you to the card in that agent's own "
        "conversation — a wizard with a review step before anything is sent. A "
        "question that takes several answers draws boxes to tick, and sends them "
        "the way Claude Code does. A card taller than its pane scrolls with it "
        "under the wheel. Answer it and the room's line resolves in place: purple, "
        "with your answer folded under it, instead of a stale warning sitting above "
        "a second message.",
        False,
    ),
    Shot(
        "30-bang",
        0.5,
        18.5,
        "●",
        "you",
        "@omar !sh statuses.sh",
        "Run it, and the agent sees it.",
        "<code>!cmd</code> in a conversation runs the command and draws its "
        "output, then hands both to that agent as Claude Code's own <code>!</code> "
        "does — a turn it answers. From the room, <code>@omar !sh "
        "statuses.sh</code> runs it in omar's directory, draws it in omar's thread "
        "and sends it there, so the output and the agent's read on it arrive "
        "together. A shell line has one directory, so <code>@team</code> and "
        "<code>@all</code> are refused with the draft kept.",
        False,
    ),
    Shot(
        "31-wake-help",
        0.5,
        17.5,
        "·",
        "you",
        "/wake-help",
        "Every key, one keystroke away.",
        "<code>/wake-help</code> lists every key Wake binds, every command it owns, "
        "the addressing forms and the shell verbs, in the pane you typed it in. "
        "Typing narrows it, and <code>/wake-help park</code> opens already "
        "narrowed. None of it is copied by hand: it is read from the bindings the "
        "build runs on, and a command that ships without a help line fails the "
        "build. Nothing is sent, so it costs no turn.",
        False,
    ),
    Shot(
        "14-workflow-view",
        0.5,
        27.5,
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
]

# The friends on the usage bar: minor, so near the bottom of both pages.
FRIENDS_HEADING = "A friend on the usage bar."
FRIENDS_PROSE = (
    "Under the roster sits the account's five-hour session usage — what Claude "
    "Code's <code>/usage</code> calls Current session — and a friend stands on it: "
    "Skip the tug, Ollie the otter, or Bo the whale. <code>/friends</code> opens a "
    "picker whose cursor previews each one in the corner, and <code>↵</code> keeps "
    "it for this machine, or none. In Kitty, Ghostty and cmux the friend is a picture "
    "that moves while an agent works, on the same heartbeat as the working line, so "
    "an idle fleet schedules nothing."
)
FRIENDS = [
    Friend(
        "32-friends",
        1.0,
        14,
        "3200:440:0:1300",
        "<code>/friends</code>, its cursor previewing each friend in the corner. Filmed "
        "in a terminal without Kitty graphics, so these are the hand-drawn block sprites "
        "every other terminal draws, which keep still; the usage figure is the stand-in "
        "agent's, not a real account's.",
    ),
]

# (title, prose) — what landed with no clip of its own. One sentence each.
ALSO = [
    (
        "/wake-name",
        "Rename the fleet you are in: everything started before the rename keeps "
        "reaching it, and every open window says so.",
    ),
    (
        "Up to fifty",
        "<code>--max-agents &lt;n&gt;</code> caps a fleet at 1 to 50 sessions, 50 "
        "unless set, and reaches one already open; lowering it stops nothing.",
    ),
    (
        "Usage limits, resumed",
        "Agents a usage limit stopped are resumed once — when you sign in to another "
        "account anywhere, or at the reset — and drawn as Wake's, never as your words.",
    ),
    (
        "A dropped connection, resumed",
        "A turn an unreachable API ended is resumed once another agent's turn goes "
        "through or the network changes, never on a timer.",
    ),
    (
        "/status",
        "One agent's own facts — state, ids, model, effort, context, mode, team, "
        "builds — in a menu over the composer, without sending it anything.",
    ),
    (
        "Narrow tables",
        "A table too narrow for its pane is drawn as records, <code>Header: "
        "value</code> a line, the way Claude Code draws one.",
    ),
    (
        "/clear in the room",
        "Empties this window's group chat and reaches no agent — for a room grown "
        "heavy enough to slow the machine.",
    ),
    (
        "The manager's own name",
        "Claude sessions know each fleet's manager as <code>manager-&lt;fleet&gt;</code>; "
        "<code>@manager /rename ops</code> makes it <code>manager-ops</code>.",
    ),
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
        "A message one Claude session sends another shows as <code>↪ sender → "
        "recipient</code>, folded at three rows, and a conversation draws its own "
        "agent's sends the same way — none can be forged from a pasted envelope.",
    ),
]
