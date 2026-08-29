package ui

// The right sidebar: what agents are doing.
//
// This is the structural half of the settled design. In a messaging platform
// you do not see what people are *doing* - you see what they said to you - and
// activity lives outside the message list. That split kills the clutter
// problem rather than tuning it: there is no threshold to pick, because tool
// calls, diffs and lifecycle never enter the conversation at all.
//
// It reads the Fleet's per-agent record, which internal/ui updates from the
// event stream rather than from status pushes. A push fires on a state
// *change* and an agent stays `working` across ten tool calls, so a sidebar
// driven by pushes would show a stale tool for the whole of a turn. The report
// carries the same fields for a different consumer - see
// rpc.SessionStatus.Tool.
//
// # Twenty columns is the design constraint, not a detail of it
//
// rosterWidth lives in layout.go, which is the only file that spends it. Three
// rules here follow from it and from nothing else: two lines per agent rather
// than one, an argument cut to its last path segment, and an unread count
// budgeted before the name rather than after it.
//
// The fourth is the load-bearing one. **Nothing here may be wider than the
// column it was given.** lipgloss joins columns on their widest line, so a
// sidebar that draws one row too wide does not overflow itself - it shoves the
// room and the DM sideways for as long as that row is on screen. Rows are
// therefore cut before they are joined rather than after: a lipgloss Width also
// *wraps*, so an unbounded row does not merely look wrong, it spends two or
// three of the column's rows and the agents below it fall off the bottom.

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// stateGlyph is one character of liveness. Block glyphs stand in for the pixel
// sprites §17 defers.
//
// A glyph per state and no more: the row's job is to be scanned, and the
// evidence behind a state - how long it has been quiet - is what ranked the row
// into the position it is in.
//
// One glyph per state *and one state per glyph*. Both directions are derived
// from rpc's own declaration in the test, because a glyph shared between two
// states is a sidebar that says an ended agent is working with nothing
// anywhere able to see it.
var stateGlyph = map[string]string{
	rpc.StateBlocked: "●",
	rpc.StateWorking: "◐",
	rpc.StateIdle:    "○",
	rpc.StateSilent:  "◌",
	// Parked is a filled square: solid, because the session is still there, and
	// square rather than round because every round glyph above it means a
	// process is doing something. Whether it renders as one cell in a real
	// terminal font is docs/live-testing.md's question; that it measures as one
	// here is TestEveryGlyphIsOneColumnWide's.
	rpc.StateParked:   "▪",
	rpc.StateEnded:    "·",
	rpc.StateOrphaned: "!",
}

// unknownGlyph is a state this build does not know. Drawn rather than blanked:
// a row with no glyph reads as an agent with no state.
//
// It is also what an agent gets before any report has described it, which is
// not an edge case - Fleet.Observe adds an agent the moment an event mentions
// one, and fan-out starts before the spawn confirmation is enqueued.
const unknownGlyph = "?"

// Roster is the sidebar and where the cursor is in it.
type Roster struct {
	// Selected is the agent the cursor is on, by id. Empty means none - which
	// is the honest state for a fleet with nothing in it, and for a jump that
	// found nothing to jump to.
	Selected string

	// SelectedTask is the dispatch the cursor is on under that agent, and ""
	// for the agent's own row.
	//
	// **Selected still names the agent when the cursor is on a subagent**, and
	// that is the whole reason this is a second field rather than a wider
	// meaning for the first. ⌃C parks, ⎋ interrupts and ↵ opens whatever
	// pickedAgent answers, and a subagent is not a session: it has no process,
	// no id the daemon knows and no lifecycle of its own. Widening Selected
	// would have made all three of those keys ask a question with no answer, on
	// the surface an operator stops a runaway agent from.
	SelectedTask string
}

// View draws one row per agent, in the order it was given them, scrolled so the
// cursor is always on screen.
//
// That order is attention order, because Fleet.Agents ranks before it returns.
// This does not rank again: a second sort here would be a second opinion about
// the same question, and the roster would disagree with the room about which
// agent is first.
//
// # Why it scrolls at all, and why it has no scroll position
//
// The column stopped drawing at the bottom and said nothing. At thirty agents
// on a forty-row terminal that is a third of the fleet invisible, in the one
// surface whose whole job is telling an operator who needs them - and ↑↓ could
// move the cursor onto a row nobody could see, so ⌃D opened a conversation with
// an agent the operator had never been shown.
//
// The window is **derived from the cursor** rather than stored beside it. A
// stored offset is a second piece of state that can disagree with the first:
// the ranking reorders between frames, so an offset that was right when it was
// written points at a different agent by the time it is drawn. Deriving it means
// the invariant - the cursor is on screen - is true by construction rather than
// by every writer remembering to maintain it.
func (r Roster) View(agents []Agent, subs subsOf, width, height int) string {
	if width <= 0 || height <= 0 {
		// A column nobody has is drawn as nothing at all, and "nothing" has to
		// be the empty string rather than an empty column: a caller composing
		// regions joins whatever it is handed. Without this, a zero width is
		// height rows of blank padding and a zero height is one row of it -
		// both of them a blank column drawn where Regions said to draw none.
		//
		// Both halves are separate failures and both are mutated: dropping
		// either comparison leaves the other's case producing padding.
		return ""
	}

	w := r.window(agents, subs, width, height)
	lines := make([]string, 0, height)
	for _, a := range agents[w.from:w.to] {
		lines = append(lines, r.rows(a, subsFor(subs, a.ID), width)...)
	}
	if hidden := len(agents) - (w.to - w.from); hidden > 0 {
		// Cut to make room for the count rather than letting MaxHeight cut the
		// count. window's fallback draws one agent taller than the whole column,
		// so without this the line saying the fleet is bigger is the first thing
		// off the bottom - at exactly the size where it is the only thing that
		// could tell an operator so.
		lines = append(lines, moreRow(hidden, width))
	}
	// Width pads every row out to the column and Height pads the column out to
	// its rows; MaxHeight is the cut, and the last agent to fit is the one that
	// straddles the boundary with a tool call under it. There is deliberately no
	// MaxWidth here - rows cuts each line already, and a second bound over the
	// same property is one that could be deleted with this package still green.
	return lipgloss.NewStyle().Width(width).Height(height).
		MaxHeight(height).Render(strings.Join(lines, "\n"))
}

// span is the slice of agents a column has room for: [from, to).
type span struct{ from, to int }

// moreLine is what the column says about the agents it did not draw.
//
// A count rather than a scrollbar, because the question it answers is "is the
// fleet bigger than this" and the answer has to survive a sixteen-column
// sidebar. Silence was the old answer and it is the defect: a roster that stops
// is indistinguishable from a fleet that ends.
func moreLine(n int) string { return "  +" + strconv.Itoa(n) + " more" }

// moreRow is that line as a sidebar draws it: styled, cut to the column and
// padded back out to it. Both sidebars call it, because a row narrower than the
// column makes the whole block narrower - lipgloss then joins the pane beside it
// several columns early while Layout.Hit goes on using the reserved width, and
// every click lands on the wrong region.
func moreRow(n, width int) string {
	line := clip(HintStyle.Render(moreLine(n)), width)
	return line + strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))
}

// window is which agents fit, chosen so the cursor is among them.
//
// The cursor's own agent is placed first and the window grows around it, which
// is what makes "the cursor is on screen" true by construction. The first draft
// grew forwards from the cursor and then backwards, measuring each range from
// scratch - and an adversarial review found that when the cursor's own agent was
// taller than the room, the forward loop could not place it and the backward
// loop then measured a range that did not contain it, so the column drew the
// agent *above* the cursor and ⌃D opened a conversation with an agent that was
// not on screen. That is the defect this whole file exists to remove, arriving
// through the fix for it.
//
// Heights come from rowsFor rather than from rendering, so choosing a window
// costs arithmetic rather than a lipgloss render per agent per step. The same
// review measured the rendering version at 833µs for one column against a
// ~250µs budget for the whole frame.
//
// The overflow line costs a row whenever anything is hidden, budgeted before
// the walk: a window sized as though it were free is one row too tall, and
// lipgloss wraps the row it cannot fit.
func (r Roster) window(agents []Agent, subs subsOf, width, height int) span {
	if len(agents) == 0 {
		return span{}
	}
	total := 0
	for _, a := range agents {
		total += rowsFor(a, subsFor(subs, a.ID))
	}
	if total <= height {
		return span{from: 0, to: len(agents)}
	}
	room := height - 1

	cursor := indexOf(agents, r.Selected)
	if cursor < 0 {
		cursor = 0
	}
	if rowsFor(agents[cursor], subsFor(subs, agents[cursor].ID)) > room {
		// No whole agent fits. Drawn as the count alone, which is the honest
		// answer: a cut agent implies the fleet is that agent, and the one thing
		// this column must never do is understate the fleet.
		return span{}
	}
	used, to := rowsFor(agents[cursor], subsFor(subs, agents[cursor].ID)), cursor+1
	for to < len(agents) && used+rowsFor(agents[to], subsFor(subs, agents[to].ID)) <= room {
		used += rowsFor(agents[to], subsFor(subs, agents[to].ID))
		to++
	}
	from := cursor
	for from > 0 && used+rowsFor(agents[from-1], subsFor(subs, agents[from-1].ID)) <= room {
		used += rowsFor(agents[from-1], subsFor(subs, agents[from-1].ID))
		from--
	}
	return span{from: from, to: to}
}

// rowsFor is how many lines an agent draws, without drawing them.
//
// It is the one place that number is decided and rows() is held to it by
// TestRowsDrawsExactlyTheLinesRowsForPromises - a second answer here would be
// a window sized for a column somebody else is rendering.
func rowsFor(a Agent, subs []Task) int {
	n := 1
	if a.Tool != "" {
		n++
	}
	return n + len(subs)
}

// At is the agent whose rows include line y, counted the way View lays them.
//
// It starts from the same window View draws, so a click on a scrolled column
// lands on the agent under the pointer rather than on the one that would have
// been there unscrolled - and it walks rows() rather than restating how tall an
// agent is, so a row that grows a line stays clickable without this changing.
func (r Roster) At(agents []Agent, subs subsOf, width, height, y int) (Agent, string, bool) {
	if width <= 0 || y < 0 {
		return Agent{}, "", false
	}
	w := r.window(agents, subs, width, height)
	line := 0
	for _, a := range agents[w.from:w.to] {
		mine := subsFor(subs, a.ID)
		rows := r.rows(a, mine, width)
		if y >= line+len(rows) {
			line += len(rows)
			continue
		}
		// The subagent rows are the last len(mine) of them, so which one was hit
		// is measured from the bottom rather than from an offset that would have
		// to restate whether this agent drew a tool call.
		if from := len(rows) - len(mine); y-line >= from {
			return a, mine[y-line-from].Dispatch, true
		}
		return a, "", true
	}
	return Agent{}, "", false
}

// rows is one agent: its liveness and name, and under it what it is inside.
//
// Two lines rather than one, because twenty columns cannot hold
// "◐ sydney Edit(auth/token.go)" and the alternative to choosing is wrapping.
// The name goes first, so a column with room for an odd number of lines cuts a
// tool call rather than an agent.
//
// The tool goes through render.ToolCall for the same reason the DM's does - it
// measures the argument against the name, so an MCP tool name running past
// twenty columns truncates the argument rather than the line.
//
// Both lines are cut in one statement rather than each at its own Render. The
// tool's line is already bounded by render.ToolCall and the head's is not, so
// two separate bounds would mean one of them could be deleted with every test
// in this package still green - a bound whose failure nothing reaches, sitting
// beside the one thing this file must not get wrong.
func (r Roster) rows(a Agent, subs []Task, width int) []string {
	out := []string{r.headStyle(a).Render(headLine(a, width))}
	if a.Tool != "" {
		// Indented to sit under the name rather than under the glyph: the glyph
		// is the row's own column and what follows it is about the agent.
		// Unstyled, because the whole row is muted below: the roster is a
		// glance at what an agent is doing, not the conversation's own block.
		tool := render.ToolCall(render.Call{Name: a.Tool, Display: shortArg(a.ToolArg)}, render.ToolStyle{}, width-toolIndent)
		out = append(out, strings.Repeat(" ", toolIndent)+HintStyle.Render(tool))
	}
	// Under the tool call rather than above it: the tool is what this agent is
	// doing itself, and a dispatch is work it handed to somebody else.
	for _, t := range subs {
		out = append(out, r.subStyle(a.ID, t).Render(subagentRow(t, width)))
	}
	for i, line := range out {
		out[i] = clip(line, width)
	}
	return out
}

// subStyle is a subagent row in colour: the cursor, and nothing else.
//
// No warn arm, unlike headStyle. A subagent cannot block on its own account -
// its ask arrives against the parent session, which is the row above wearing
// the colour already.
//
// The agent is part of the match rather than the dispatch id alone, so the
// cursor is a property of *this row under this agent*. A bare id comparison is
// only as sound as the ids being unique across the fleet, which is a fact about
// claude's tool_use ids rather than anything this file can hold - and it made a
// cursor that named two different agents draw as one selection under the wrong
// one.
func (r Roster) subStyle(agentID string, t Task) lipgloss.Style {
	if r.SelectedTask != "" && r.Selected == agentID && t.Dispatch == r.SelectedTask {
		return AccentStyle
	}
	return HintStyle
}

// headStyle is the whole of what a row says in colour: where the cursor is,
// who is stopped waiting for you, and which agent this is when /color named one.
//
// Everything else is ordinary text. A sidebar in which every row is emphasised
// has emphasised nothing, and the states that are not blocked are already
// distinguished by their glyph.
//
// The cursor is matched on a non-empty id rather than on equality alone: a
// Roster selecting nothing and an agent whose id has not arrived are both "",
// and reading that as a match would draw every row as the cursor for as long as
// nothing was selected - which is the state a fleet starts in.
// SelectedTask is what keeps this to one row. Selected goes on naming the agent
// while the cursor is on one of its subagents - which is what ⌃C and ⎋ read -
// so without that clause both rows wear the accent and the sidebar shows two
// selections with nothing saying which one a key is about.
//
// # Blocked wins, then the cursor keeps the identity hue it used to hide
//
// Opening a conversation makes its agent the selected row (panes.go), so a
// coloured agent's row spent most of its life under the cursor - and the cursor
// used to win outright, drawing the accent and hiding the hue the operator set.
// So a coloured row keeps its hue under the cursor and shows the selection as
// **bold** instead; an uncoloured row still takes the accent, which is the whole
// cursor tell it ever had.
//
// Blocked is checked first, so a "waiting for you" is never painted over - by
// the accent, as it was before this reorder, or by an identity hue that reads
// far less urgent than warn. The selection rides along as bold when the blocked
// row is also the cursor, so nothing is lost by giving warn the top slot.
func (r Roster) headStyle(a Agent) lipgloss.Style {
	style, colored := identityStyleFor(a)
	selected := r.Selected != "" && a.ID == r.Selected && r.SelectedTask == ""
	switch {
	case a.State == rpc.StateBlocked:
		if selected {
			return warnStyle.Bold(true)
		}
		return warnStyle
	case selected:
		if colored {
			return style.Bold(true)
		}
		return AccentStyle
	default:
		if colored {
			return style
		}
		return TextStyle
	}
}

// headLine is a row's first line: liveness, name, the count of what you have
// not read, and what the agent has produced.
//
// # The budget, in the order it is spent
//
// The badge is budgeted before the name is cut rather than after, because the
// badge is why the row is worth looking at and a name stays recognisable three
// characters short. Truncating the assembled line would drop the badge first,
// every time, on exactly the rows that have one.
//
// The **token count is budgeted last and dropped whole**, which is the opposite
// rule and deliberately so: the name is how an agent is addressed - `@sydney`
// is typed at a composer - and the count is something to know. A name cut to
// make room for a figure buys the least urgent thing on the row with the one
// thing the row is for. So a wide enough sidebar shows both and a narrow one
// shows the name.
//
// Dropped rather than abbreviated further for the same reason `⌃Q` reads
// `quit & park all`: a partial `↓ 12` is a *different number*, and a wrong
// figure on screen is worse than no figure.
func headLine(a Agent, width int) string {
	head := rowGlyph(a) + " " + a.Name
	if a.Unread > 0 {
		badge := " " + unreadBadge(a.Unread)
		head = clip(head, width-lipgloss.Width(badge)) + badge
	}
	tokens := rowTokens(a)
	if tokens == "" || lipgloss.Width(head)+lipgloss.Width(tokens) > width {
		return head
	}
	return head + tokens
}

// rowTokens is what a **working** agent has produced, and "" for every other
// row.
//
// The arrow and the abbreviation are the working line's own - shared rather than
// re-spelled, so the figure beside a name and the figure beside a conversation
// cannot read differently for the same agent. The word `tokens` is not: it is
// seven of the twenty-three columns this sidebar has, and the arrow already
// says what the number is.
//
// # Working only, and the turn's own figure
//
// A row that is not working says nothing, which is the owner's ruling: the
// count answers "what is this costing me right now" and an idle row spending a
// third of its width on a number that stopped moving is clutter on the surface
// whose whole job is to be scanned.
//
// **The figure is the turn in flight**, not the session - Agent.TurnTokens,
// summed from the message_delta frames as the turn runs. That is the count
// Claude Code's own bar shows, and it needed `--include-partial-messages`,
// which this build passes since PR #37. Before it there was no account of a
// turn's cost until the turn was over, so both this row and the working line
// showed a session total: during turn N they reported turns 1…N−1.
//
// It is the same value the working line draws for the same agent, read off the
// same field, because two renderings of one figure are two things free to
// disagree.
//
// **A build that receives no message_delta shows nothing here** rather than
// falling back to the session total. The two are different questions, and a row
// that answered whichever it could would be a number whose meaning depended on
// a flag the reader cannot see.
func rowTokens(a Agent) string {
	if a.State != rpc.StateWorking || a.TurnTokens <= 0 {
		return ""
	}
	return " " + tokenArrow + " " + humanTokens(a.TurnTokens)
}

// toolIndent is the glyph and its space, so the activity line starts where the
// name does.
const toolIndent = 2

// rowGlyph is the character of liveness a row opens with: the state's own,
// except for a turn in flight, which animates.
//
// A static ◐ says an agent is in the working state and cannot say it is still
// alive - a session thinking and a session wedged draw the same row, which is
// the question a sidebar of fifteen to thirty exists to answer. It animates
// through the *same* heartbeatGlyph the conversation pane uses, off the same
// wall-clock elapsed, so the glyph beside a name and the glyph on its working
// line are one character at any moment rather than two animations that drift.
//
// A working agent with no start time keeps the static glyph: that is a session
// already running when this client attached, and animating it from the zero
// time would freeze every such row on one frame.
func rowGlyph(a Agent) string {
	if a.State == rpc.StateWorking && !a.startedAt.IsZero() {
		return heartbeatGlyph(turnAge(a.State, a.startedAt))
	}
	return glyphOf(a.State)
}

// glyphOf is a state's glyph, or the unknown one for a state this build does
// not know.
func glyphOf(state string) string {
	if g, ok := stateGlyph[state]; ok {
		return g
	}
	return unknownGlyph
}

// shortArg is the part of a tool's argument worth twenty columns.
//
// A path is cut to its last segment, because at this width the head of a path
// is the part every agent in the fleet has in common and the tail is the part
// that says which file this one is on: `internal/ui/roster.go` cut from the
// right reads `internal/ui/…`, which names no file at all.
//
// An argument carrying a space is left whole for render.ToolCall to cut from
// the right, because it is not a path - a shell command line and a subagent's
// task description both put their meaning at the front, and the last thing
// after a slash in `go test ./internal/ui` is the least informative part of it.
//
// The rule reads the shape of the string and never the tool's name. Which tool
// carries which argument is Claude's vocabulary, resolved behind the airlock as
// core.ToolCall.Display; a switch on tool names here would be that vocabulary
// leaking two packages above it, which is what render/tool.go's own header
// describes having already been fixed once.
func shortArg(display string) string {
	if strings.ContainsAny(display, " \t") {
		return display
	}
	if i := strings.LastIndexByte(display, '/'); i >= 0 && i+1 < len(display) {
		return display[i+1:]
	}
	return display
}

// clip cuts a line to a number of columns, ANSI-aware.
//
// It exists because lipgloss's own MaxWidth reads a non-positive maximum as "no
// maximum" and returns the line whole, which is the opposite of what every
// caller here means by a column count of zero.
func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// Move steps the cursor through the list as it is currently ordered.
//
// The arithmetic normalises twice on purpose. Go's % keeps the sign of its left
// operand, so a single `(at+delta+len)%len` still yields a negative index for
// any delta below -len - and this is an exported method taking an int, where a
// negative index is a panic in the draw loop rather than a wrong row.
func (r Roster) Move(agents []Agent, subs subsOf, delta int) Roster {
	if len(agents) == 0 {
		return Roster{}
	}
	stops := walkable(agents, subs)
	at := stopIndex(stops, r)
	if at < 0 && r.SelectedTask != "" {
		// The dispatch the cursor named has finished, so its row is simply gone
		// - walkable is rebuilt from what is running. The cursor falls back to
		// the agent it was under rather than to the top of the sidebar, which
		// at thirty agents is somewhere nobody was looking.
		at = stopIndex(stops, Roster{Selected: r.Selected})
	}
	if at < 0 {
		// The cursor is on nobody, or on an agent that has left the roster. The
		// first move lands on the first row rather than counting from a
		// position that no longer exists.
		at, delta = 0, 0
	}
	n := len(stops)
	return stops[((at+delta)%n+n)%n]
}

// walkable is every row the cursor can land on, in the order they are drawn:
// each agent, then the dispatches running under it.
//
// Built rather than indexed into, because the two are different shapes - an
// agent is a row and a dispatch is a row under one - and the arithmetic for
// "which agent does index 7 belong to" is the kind that goes wrong once and
// silently thereafter. A slice of the answers cannot.
func walkable(agents []Agent, subs subsOf) []Roster {
	out := make([]Roster, 0, len(agents))
	for _, a := range agents {
		out = append(out, Roster{Selected: a.ID})
		for _, t := range subsFor(subs, a.ID) {
			out = append(out, Roster{Selected: a.ID, SelectedTask: t.Dispatch})
		}
	}
	return out
}

// stopIndex is where the cursor sits among the walkable rows, or -1.
//
// An empty Selected is nobody rather than the first row, for indexOf's reason:
// "" is how "no selection" is spelled, and an agent seen through an event
// before any report described it has an empty id too.
func stopIndex(stops []Roster, at Roster) int {
	if at.Selected == "" {
		return -1
	}
	for i, s := range stops {
		if s == at {
			return i
		}
	}
	return -1
}

// Next jumps to the next agent that needs you, in the order it was given them,
// and selects nothing when nothing does.
//
// # The key this is the behaviour of, and why it is still not bound
//
// Spec §6 gives this to ⌃⇧A, and Phase 1 shipped that key bound to nothing at
// all - which is why it was taken out of the legend. This is the half that can
// exist now; the binding is not, and that is a fact about the terminal rather
// than a choice deferred for tidiness. bubbletea v1.3.10 names no
// ctrl+shift+<letter> at all - KeyCtrlShift{Up,Down,Left,Right,Home,End} is the
// whole set - and a terminal without the Kitty keyboard protocol sends ⌃⇧A as
// the byte it sends for ⌃A, so nothing in this build can tell the two apart.
// The four arrows in that set are no way out: macOS takes every one of them for
// spaces and Mission Control before a terminal sees it, which is what cost the
// grid its first pair of keys.
//
// So the legend gains nothing here, in either direction: no glyph without a
// binding, and no binding without a glyph. Task 10 probes the chord under the
// target terminal before either is added, and this is reachable meanwhile by
// putting the cursor on a row.
func (r Roster) Next(agents []Agent) Roster {
	blocked := make([]Agent, 0, len(agents))
	for _, a := range agents {
		if a.State == rpc.StateBlocked {
			blocked = append(blocked, a)
		}
	}
	if len(blocked) == 0 {
		// Nothing needs you, so the cursor goes nowhere and says so. A key that
		// moves a cursor when there is nothing to move it to is a key that lies
		// about the fleet.
		return Roster{}
	}
	// A cursor that is not among them - answered since, or never set - reads as
	// -1 and lands on the first, rather than counting from a row that is no
	// longer in the list.
	at := indexOf(blocked, r.Selected)
	return Roster{Selected: blocked[(at+1)%len(blocked)].ID}
}

// indexOf is where an id sits in a list, or -1.
//
// An empty id is nobody rather than the first agent whose id has not arrived
// yet: "" is how "no selection" is spelled on Roster.Selected, and Agent.ID is
// empty for an agent seen through an event before any report described it.
func indexOf(agents []Agent, id string) int {
	if id == "" {
		return -1
	}
	for i, a := range agents {
		if a.ID == id {
			return i
		}
	}
	return -1
}
