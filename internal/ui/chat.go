package ui

// The room: the group conversation, and the pane you return to.
//
// It is the DM's shape with a different filter and a different attribution.
// The transcript, the chunked backing and the width contract are all shared
// rather than reimplemented - a second scrollback here would be the parallel
// implementation this project forbids, and it would be the *expensive* kind,
// since transcript exists because rebuilding a conversation per event was
// 11.3ms of an 11.5ms append at 4,000 events.
//
// Room and DM are peers. The brief's body called the room the primary surface
// and its amendment supersedes that: thinking *with* an agent is a DM, open
// and persistent; *supervising* agents is the room. So the room is the pane
// you return to rather than the one you stare at, and nothing here assumes it
// owns the screen.

import (
	"cmp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// roomTitle heads the pane. It names the surface rather than an agent, which
// is the one visible difference from the DM's header.
const (
	roomTitle            = "group chat"
	roomRetentionEvents  = 20_000
	roomReclaimedHistory = "… older room history reclaimed"
)

// Room is the group conversation.
//
// Its methods take value receivers and return a new Room, so a caller holding
// an older copy keeps the transcript and the scroll position it had.
type Room struct {
	// behindAsk dims this pane's transcript, so a framed card reads as modal
	// rather than as the newest block. Set for the draw; see WithAsk.
	behindAsk bool

	// writing titles the composer while it holds an answer rather than a
	// message. Set for the draw; see WithWriting.
	writing string

	// said is what has been drawn, paired with who was speaking at the time.
	// The pairing is stored rather than re-derived on a re-wrap: an agent's
	// label changes when it changes branch, and re-deriving would silently
	// rewrite the attribution on every line of history at the next resize.
	said chunked[roomLine]
	tr   transcript

	// nextLineID gives every event block a stable identity across a Before
	// rebuild. lineMoves is the one-update translation that rebuild produced;
	// App.withRoom consumes and clears it when reconciling a selection.
	nextLineID uint64
	lineMoves  roomLineMoves

	// reclaimed is sticky once the combined history/live sequence has crossed
	// its cap. It keeps one fixed boundary at the oldest retained position.
	reclaimed bool

	// hist is how many of said's leading lines came out of a transcript rather
	// than off the socket. Before merges into exactly that range and leaves the
	// rest alone, which is what makes a session resumed an hour in land among
	// the history instead of under the newest thing anybody said.
	hist int

	// raw is every restored line, in time order and before the broadcast rule
	// has run - so said's leading `hist` lines are collapseBroadcasts(raw).
	//
	// Kept because the rule needs to see **several transcripts** and the daemon
	// answers **one per frame**: a copy of a broadcast arrives in its own reply,
	// so a collapse applied to each batch as it lands can never see a second
	// session and drops every restored turn you typed. Re-derived from raw on
	// each batch instead, which is also what makes the rule independent of the
	// order the replies happen to arrive in.
	raw []roomLine

	// expandAll is ⌃E's global toggle: while it is set, every response the room
	// would collapse into a pointer draws its full render instead. It is the
	// room learning the DM's ⌃E - expand-everything, then a second press
	// hides-everything, which is why toggling it off also drops the per-line
	// opens below (see toggleExpandAll). A bool rather than the DM's key, and it
	// survives a Before rebuild because it is not keyed on a block id.
	expandAll bool

	// expanded is the set of collapsed responses a click has opened one at a
	// time, keyed on roomLine.id. A line draws full while expandAll is set or
	// its id is here; a click flips one entry (toggleLine). Copied on write, so
	// a Room handed around by value cannot mutate a copy's opens. Keyed on the
	// block id rather than a position so it is stable across a re-wrap; a Before
	// that re-derives ids may drop an entry, which re-collapses that one line -
	// rare, and never wrong, the way the DM's per-item opens are transient.
	expanded map[uint64]bool

	composer Composer
	width    int
	height   int

	// bar is the info line drawn between the box and the legend, and barFrom is
	// what it was last rendered from. Unlike a DM's, the room has no agent of
	// its own - App.withRoomBar resolves the one the composer is addressing (the
	// manager, or a lone @name) and hands it here, cached so the filesystem read
	// statusBar does stays off the draw loop. Empty draws no bar - an empty room
	// has nobody to draw one for.
	bar     string
	barFrom barKey

	// sel is the selection the room is being drawn with, set for the draw by
	// App.roomPane and not held - App owns the one selection, and a copy kept
	// here would go stale against it.
	sel marked

	// csel is a selection taken inside the query box rather than the transcript,
	// set for the draw the same way. At most one of sel and csel is non-empty.
	csel marked

	// beat is the working line for the fleet, set for the draw by App.roomFor
	// the way sel and the composer's mode are: it is a fact about the *fleet*
	// and the room has no roster of its own. See roomWorkingLine.
	beat string

	// focus narrows the transcript to one agent's thread; "" is the unfocused
	// room. managerID and focusName ride with it - the predicate needs the
	// manager's id, the header needs the focused agent's name. Persistent, not
	// draw-only: every render path (a width re-wrap, a history merge) must apply
	// the same filter, and a hidden line stays in said at rows == 0. See
	// roomfocus.go and WithFocus.
	focus     string
	managerID string
	focusName string

	// menu is everything pinned between the transcript and the composer - the
	// card, the picker and the completion menu (App.menuBlock) - handed over
	// whole by App.roomPane and clipped here (see menuRows).
	//
	// Held rather than pinned above the pane because all three belong *next to
	// what is being typed*: through App.paneChrome they went above the
	// transcript, most of a pane away from the draft. Below the transcript they
	// also stop being rows mouse.go's startSelection has to know about.
	menu string

	// chrome is what chromeHeight returned when the transcript was last sized,
	// which is DM.chrome's rule read for this pane.
	//
	// The room's chrome moves for two reasons the DM's does not, and neither is
	// a resize: the composer grows a row per line of the draft, on a keystroke
	// that sizes no pane, and the working line appears the moment an agent
	// starts a turn. So the transcript stayed laid out for the box's old height
	// and the pane drew past the height it was given, which App.View cuts off
	// the bottom of the frame - the awareness strip and the notice row first,
	// the box's own last rows after them.
	chrome int
}

// WithAsk says an answerable card is drawn in this pane, which quiets the
// transcript behind it. For the draw only; see askdim.go for the cost.
func (r Room) WithAsk(v bool) Room {
	r.behindAsk = v
	return r
}

// WithWriting titles the composer for a draft that is an answer rather than a
// message, for the draw only. "" leaves the pane's own name in place.
//
// A field rather than a title set through WithComposer, because View sets the
// pane's own name unconditionally - and a title that has to win against that
// is a second claim on one string, which is how the two start disagreeing.
func (r Room) WithWriting(t string) Room {
	r.writing = t
	return r
}

// WithWorking is the working line this pane draws, for the draw only.
func (r Room) WithWorking(agents []Agent) Room {
	r.beat = roomWorkingLine(agents, r.blockWidth())
	return r
}

// WithSelection is the selection this pane draws, for the draw only.
func (r Room) WithSelection(m marked) Room {
	r.sel = m
	return r
}

// WithComposerSelection is the query-box selection this pane draws.
func (r Room) WithComposerSelection(m marked) Room {
	r.csel = m
	return r
}

// WithMenu is the menu block this pane draws, for the draw only. See Room.menu.
func (r Room) WithMenu(menu string) Room {
	r.menu = menu
	return r
}

// roomLine is one event and the speaker it is attributed to. id identifies its
// rendered block; broadcastID carries a proved cluster's identity on each raw
// physical copy so replies can be reordered without changing the block already
// on screen while that cluster remains retained.
type roomLine struct {
	ev core.Event
	by Agent
	// to is the agent id this user line was addressed to; "" is a broadcast or a
	// line that is not the operator's own. UI-only, set at creation (never on
	// core.Event) - the view filter reads it to tell "you → @iris" from a
	// broadcast and from "you → @john". See roomfocus.go.
	to          string
	id          uint64
	broadcastID uint64
	rows        int
}

// NewRoom returns an empty group conversation with a focused composer.
func NewRoom() Room { return Room{composer: NewComposer()} }

// SetSize re-lays the room for a pane of w by h. Only a width change re-wraps;
// height moves a window over lines that are already rendered.
//
// # What returns the reader to the bottom, and what does not
//
// A **width** change does, unconditionally: it re-wraps the text, so the line
// offset a scrolled reader was holding no longer points at what they were
// reading, and restoring a stale offset is a worse lie than the newest line.
//
// A **height** change does not, unless the reader was already following. It
// moves a window over lines that already exist and cannot invalidate an offset -
// and the pane's height moves for reasons that have nothing to do with reading:
// a card is pinned above the room whenever an agent is blocked, and App.roomPane
// then draws the room shorter by exactly that many rows. An unconditional
// return threw the offset away on every drawn frame for as long as a card was
// up, which is precisely when somebody is reading back to work out what the
// agent is asking about. The key moved the model and not the screen.
//
// Sampling atBottom *before* anything is re-laid is what makes it true, the
// same way Append does it.
func (r Room) SetSize(w, h int) Room {
	following := r.tr.atBottom()
	if w != r.width {
		r.width = w
		lines := r.said.slice(r.said.first(), r.said.len())
		blocks := renderRoom(r, lines)
		first := r.said.first()
		r.said = chunked[roomLine]{base: first, n: first}.append(lines...)
		r.tr = r.tr.replace(blocks)
		following = true
	}
	r.height = h
	// The composer wraps the draft as keys arrive, so it is sized here rather
	// than only when it is drawn. See Composer.SetWidth.
	r.composer = r.composer.SetWidth(max(w, minComposerWidth)).WithMaxRows(composerRowsIn(h, r.composer.overhead()))
	r.chrome = r.chromeHeight()
	r.tr = r.tr.sized(max(w, minComposerWidth), max(h-r.chrome, minTranscriptHeight))
	if following {
		r.tr = r.tr.toBottom()
	}
	return r
}

// WithFocus narrows the room to one agent's thread, or widens it when focus is
// "". A focus-id change re-renders the filtered subset and jumps to bottom - a
// width change's own semantics (SetSize sets following=true), and the natural
// read when you enter a focus. managerID and focusName ride on the struct for
// the predicate and the header to read, but a change in managerID *alone* does
// not re-render: while unfocused it changes nothing on screen, and at a manager
// start there are no past manager lines to reveal - the manager's own lines
// arrive later through Append with the id already updated here. So a fleet
// report that starts the manager never yanks a scrolled reader to the bottom.
// The caller clears any text selection on a focus change (App.retarget), because
// the re-render renumbers the lines a selection is anchored to - the reason a
// width change clears it too.
func (r Room) WithFocus(focus, focusName, managerID string) Room {
	r.focusName = focusName
	r.managerID = managerID
	if r.focus == focus {
		return r
	}
	r.focus = focus
	lines := r.said.slice(r.said.first(), r.said.len())
	blocks := renderRoom(r, lines)
	first := r.said.first()
	r.said = chunked[roomLine]{base: first, n: first}.append(lines...)
	r.tr = r.tr.replace(blocks)
	r.tr = r.tr.toBottom()
	return r
}

// Append draws one event, attributed to the agent that produced it.
//
// Nothing here costs what the conversation so far cost, and at 30 agents that
// matters more than it does in a DM: every session's room-worthy events come
// through this one method, in Bubble Tea's single Update goroutine.
//
// The view follows the newest line only for a reader who was already at the
// bottom. Someone who has scrolled back is reading, and at 30 agents a room
// that yanks them to the newest line every time anybody speaks is worse than
// one with no scrollback at all. Sampling that before the content changes is
// what makes it true.
func (r Room) Append(ev core.Event, by Agent) Room { return r.appendLine(ev, by, "") }

// appendUser draws the operator's own room echo, stamped with the agent it was
// addressed to (or "" for a broadcast). Only this path carries a recipient - an
// agent's own lines are told apart by session id, not by "to".
func (r Room) appendUser(ev core.Event, to string) Room { return r.appendLine(ev, Agent{}, to) }

func (r Room) appendLine(ev core.Event, by Agent, to string) Room {
	line := roomLine{ev: ev, by: by, to: to}
	// A room-worthy line the current focus hides is kept in said (canonical, so
	// unfocus restores it) at rows == 0 with no rendered block - and its render
	// is skipped, not done and dropped. A shown line renders as before; the
	// empty-block guard stays on that path (fold pre-drops blanks, so it is
	// defensive) and runs before an id is spent.
	hidden := r.focus != "" && !focusAdmits(line, r.focus, r.managerID)
	var b block
	if !hidden {
		// A new event carries no per-line open of its own, but expandAll is a
		// standing choice: while it is set, a long reply arriving lands expanded
		// too, so ⌃E's "show everything" keeps meaning everything.
		b = renderRoomBlock(ev, by, r.blockWidth(), r.expandAll)
		if b.text == "" {
			return r
		}
	}
	following := r.tr.atBottom()
	r.lineMoves = nil

	r.nextLineID++
	line.id = r.nextLineID
	if hidden {
		r.said = r.said.append(line)
	} else {
		var rows int
		r.tr, rows = r.tr.addMeasured(b)
		line.rows = rows
		r.said = r.said.append(line)
	}
	if drop := r.said.count() - roomRetentionEvents; drop > 0 {
		r = r.reclaimOldest(drop)
	}
	if following {
		r.tr = r.tr.toBottom()
	}
	return r
}

// Before folds a batch of restored lines into the room's history.
//
// A **merge** rather than a prepend, because the batches do not all arrive at
// once: the seed asks about every live session and a resume asks about one more
// an hour later. The new lines are merged into the leading `hist` lines by
// time; everything after that is live and is never re-ordered, which is what
// core.Event.At's zero value is for.
//
// The reader is returned to the newest line only if they were already there -
// Append's rule, and for Append's reason: somebody who has scrolled back is
// reading, and a room that yanks them to the bottom when a resume lands is
// worse than one with no scrollback.
func (r Room) Before(earlier []roomLine) Room {
	if len(earlier) == 0 {
		return r
	}
	following := r.tr.atBottom()
	old := r
	held := r.said.slice(r.said.first(), r.said.len())
	oldSpans := r.roomSpans(held)

	// Incoming batches are copied before they are identified: callers may keep
	// their slice, and Room's value contract does not let this write through it.
	earlier = append([]roomLine(nil), earlier...)
	for i := range earlier {
		if earlier[i].id == 0 {
			r.nextLineID++
			earlier[i].id = r.nextLineID
		}
	}

	// A new slice rather than an append onto r.raw: Room is handed around by
	// value, so appending in place would let a discarded copy write into the
	// backing array the live one is drawing from.
	r.raw = mergeByTime(r.raw, earlier)
	r.raw = trimRoomRaw(r.raw)
	// Re-derived from every transcript rather than from this batch. See raw.
	hist := collapseBroadcasts(r.raw, &r.nextLineID)

	live := held[min(r.hist, len(held)):]
	liveStart := r.liveLineStart(held)
	combined := append(append(make([]roomLine, 0, len(hist)+len(live)), hist...), live...)
	drop := max(len(combined)-roomRetentionEvents, 0)
	liveDrop := max(drop-len(hist), 0)
	combined = append([]roomLine(nil), combined[drop:]...)
	r.hist = max(len(hist)-drop, 0)
	r.reclaimed = r.reclaimed || drop > 0
	// A merge both drops the oldest lines and re-derives history ids, so an open
	// kept on a line that left - or whose id changed - would linger. Keep only
	// opens whose block is still in the room, which bounds the set to what is
	// retained. On the rebuild path, not the per-event one, so its cost rides
	// with the render already happening here.
	r.expanded = keptExpanded(r.expanded, combined)
	blocks := renderRoom(r, combined)

	base := 0
	if liveDrop < len(live) {
		base = liveStart + roomRenderedLines(live[:liveDrop]) - roomRenderedLines(combined[:r.hist])
		if !r.reclaimed {
			base -= len(blockLines(roomBanner(r.blockWidth()), true))
		}
	}
	r.said = chunked[roomLine]{}.append(combined...)
	prefix := ""
	if r.reclaimed {
		prefix = HintStyle.Render(roomReclaimedHistory)
	}
	r.tr = r.tr.replaceFrom(blocks, base, prefix)
	r.lineMoves = lineMovesBetween(old, oldSpans, r, r.roomSpans(combined))
	if following {
		r.tr = r.tr.toBottom()
	} else if moved, ok := r.lineMoves.translate(old.tr.scroll); ok {
		r.tr.scroll = moved
	} else {
		r.tr.scroll = r.tr.first()
	}
	return r
}

// reclaimOldest removes whole event blocks from both representations. The
// first reclamation replaces the opening banner with the fixed boundary; later
// ones advance the two chunked fronts without rebuilding retained content.
func (r Room) reclaimOldest(drop int) Room {
	drop = min(max(drop, 0), r.said.count())
	if drop == 0 {
		return r
	}
	oldFirst := r.tr.first()
	evicted := r.said.slice(r.said.first(), r.said.first()+drop)
	r = r.forgetExpanded(evicted)
	cut := r.tr.lines.first() + roomRenderedLines(evicted)
	if !r.reclaimed {
		cut += len(blockLines(roomBanner(r.blockWidth()), true))
	}
	// App.withRoom must distinguish the marker newly placed at cut-1 from the
	// evicted content row that previously occupied that absolute index. Retained
	// content keeps its indices; only an already-existing marker moves to the new
	// boundary.
	moves := roomLineMoves{{from: cut, to: cut, rows: r.tr.lines.len() - cut}}
	if r.reclaimed {
		moves = append(moves, roomLineMove{from: oldFirst, to: cut - 1, rows: 1})
	}
	r.said = r.said.trimBefore(r.said.first() + drop)
	r.hist = max(r.hist-drop, 0)
	if !r.reclaimed {
		r.reclaimed = true
		r.tr.prefix = HintStyle.Render(roomReclaimedHistory)
	}
	r.tr = r.tr.trimBefore(cut)
	r.tr.scroll = max(r.tr.scroll, r.tr.first())
	r.lineMoves = moves
	return r
}

// liveLineStart is the absolute line where the live tail begins in the current
// transcript. It is the anchor a late history merge must not move.
func (r Room) liveLineStart(held []roomLine) int {
	at := r.tr.lines.first()
	if !r.reclaimed {
		at += len(blockLines(roomBanner(r.blockWidth()), true))
	}
	return at + roomRenderedLines(held[:min(r.hist, len(held))])
}

func roomRenderedLines(lines []roomLine) int {
	n := 0
	for _, line := range lines {
		n += line.rows
	}
	return n
}

type roomSpan struct{ first, rows int }

type roomLineMove struct{ from, to, rows int }

type roomLineMoves []roomLineMove

func (moves roomLineMoves) translate(line int) (int, bool) {
	for _, move := range moves {
		if line >= move.from && line < move.from+move.rows {
			return move.to + line - move.from, true
		}
	}
	return 0, false
}

// roomSpans records each event block's absolute rendered range. IDs survive a
// history merge; positions deliberately do not.
func (r Room) roomSpans(lines []roomLine) map[uint64]roomSpan {
	spans := make(map[uint64]roomSpan, len(lines))
	at := r.tr.lines.first()
	if !r.reclaimed {
		at += len(blockLines(roomBanner(r.blockWidth()), true))
	}
	for _, line := range lines {
		if line.id != 0 && line.rows > 0 {
			spans[line.id] = roomSpan{first: at, rows: line.rows}
		}
		at += line.rows
	}
	return spans
}

// roomLineMoves translates every retained rendered row from one Before layout
// to the next. Boundary rows have identities too: the banner while it exists,
// and the reclamation marker after it replaces the banner.
func lineMovesBetween(old Room, oldSpans map[uint64]roomSpan, next Room, nextSpans map[uint64]roomSpan) roomLineMoves {
	moves := make(roomLineMoves, 0, len(oldSpans)+1)
	for id, before := range oldSpans {
		after, ok := nextSpans[id]
		if !ok {
			continue
		}
		moves = append(moves, roomLineMove{from: before.first, to: after.first, rows: min(before.rows, after.rows)})
	}
	switch {
	case old.reclaimed && next.reclaimed:
		moves = append(moves, roomLineMove{from: old.tr.first(), to: next.tr.first(), rows: 1})
	case !old.reclaimed && !next.reclaimed:
		rows := min(len(blockLines(roomBanner(old.blockWidth()), true)), len(blockLines(roomBanner(next.blockWidth()), true)))
		moves = append(moves, roomLineMove{from: old.tr.lines.first(), to: next.tr.lines.first(), rows: rows})
	}
	return moves
}

// mergeByTime interleaves two time-ordered runs of restored lines. Stable on a
// tie, with the run already on screen first, so a batch that re-reads a
// transcript cannot shuffle what is drawn.
func mergeByTime(held, arriving []roomLine) []roomLine {
	out := make([]roomLine, 0, len(held)+len(arriving)+1)
	i, j := 0, 0
	for i < len(held) && j < len(arriving) {
		if arriving[j].ev.At.Before(held[i].ev.At) {
			out = append(out, arriving[j])
			j++
			continue
		}
		out = append(out, held[i])
		i++
	}
	return append(append(out, held[i:]...), arriving[j:]...)
}

// View draws the header, the conversation and the composer, measuring exactly
// width by height.
//
// Pass the size SetSize was given. View re-lays for a size it was not given so
// an early frame is never wrong, but it has a value receiver and cannot
// memoize - a caller passing a width SetSize never saw re-runs the whole room
// through glamour on every frame, behind the render package's process-global
// mutex, and resets the scroll position on every frame as well.
func (r Room) View(width, height int) string {
	// The chrome is the third thing the transcript's height depends on and the
	// only one that is not an argument here: the composer grows with the draft
	// on a keystroke, and the working line appears when an agent starts a turn.
	// Neither is a resize. See Room.chrome.
	if width != r.width || height != r.height || r.chromeHeight() != r.chrome {
		r = r.SetSize(width, height)
	}
	w := max(width, minComposerWidth)
	transcript := r.tr.view(r.sel)
	if r.behindAsk {
		transcript = quieted(transcript)
	}
	rows := []string{transcript}
	// Between the transcript and the composer, where Claude Code puts it and
	// where the conversation pane already draws its own.
	if r.beat != "" {
		rows = append(rows, r.beat)
	}
	// Last before the composer: the card, picker or completion menu is answered
	// by typing, so it belongs at the query bar. Clipped to the same count
	// chromeHeight took out of the transcript, so the two cannot disagree.
	if menu := firstRows(r.menu, r.menuRows()); menu != "" {
		rows = append(rows, menu)
	}
	// The pane names the focused agent so the narrowing is discoverable; the
	// composer's own target line is the secondary tell.
	title := roomTitle
	if r.focus != "" && r.focusName != "" {
		title = roomTitle + " › @" + r.focusName
	}
	comp := r.composer.WithBar(r.bar).WithTitle(cmp.Or(r.writing, title)).View(w)
	comp = highlightComposerBlock(comp, r.csel, composerTextLeft, w-composerRightInset)
	return strings.Join(append(rows, comp), "\n")
}

// ScrollUp moves the reader lines back, or forward for a negative count.
func (r Room) ScrollUp(lines int) Room {
	r.tr = r.tr.scrolledUp(lines)
	return r
}

// withBar re-renders the room's info bar if the agent it draws, its mode, or
// the width has moved, and returns the receiver untouched otherwise. The agent
// is whoever the composer is addressing - App.withRoomBar resolves it - so a
// zero Agent draws no bar, which is how an empty room is spelled.
func (r Room) withBar(agent Agent, mode string, width int) Room {
	key := barKey{
		width: width, dir: agent.Cwd, model: agent.Model, mode: mode, state: agent.State,
		used: agent.ContextTokens, window: agent.ContextWindow, prs: agent.prs,
	}
	if key == r.barFrom {
		return r
	}
	r.bar, r.barFrom = drawStatusBar(agent, mode, width), key
	return r
}

// Composer is the input box. Hold the one the Room hands back rather than
// stashing copies: two Composers share one text area internally, so a snapshot
// is not independently renderable.
func (r Room) Composer() Composer { return r.composer }

// WithComposer returns a Room carrying c.
func (r Room) WithComposer(c Composer) Room { r.composer = c; return r }

// blockWidth is the width block renderers are asked for.
func (r Room) blockWidth() int { return max(r.width, minBlockWidth) }

// linesFor is how many of the room's lines belong to one session.
//
// Read at both ends of a history ask, and the pair is what says whether that
// session's turn could already be on screen. A scan rather than an index: it
// runs once per ask rather than per event, which is what the non-negotiable
// about work-per-change asks for, and the room is capped.
func (r Room) linesFor(id string) int {
	n := 0
	for _, l := range r.said.slice(0, r.said.len()) {
		if l.ev.SessionID == id {
			n++
		}
	}
	return n
}

// chromeHeight is the rows View spends on everything that is not conversation.
// The composer is measured rather than assumed so this cannot drift out of
// step with it; the working line is one row when there is one and none when
// there is not, which is what makes it safe to give and take back.
func (r Room) chromeHeight() int { return r.baseChrome() + r.menuRows() }

// baseChrome is the chrome that is not the menu, which is what the menu's own
// allowance is measured against. Split from chromeHeight so menuRows can ask
// for it without asking for itself.
func (r Room) baseChrome() int {
	rows := lipgloss.Height(r.composer.View(max(r.width, minComposerWidth)))
	if r.beat != "" {
		rows++
	}
	// The info bar's row. Counted separately because the composer measured here
	// carries no bar - WithBar is a draw-time overlay, like the DM's, so the
	// stored composer's height is bar-less and the row is added back here.
	if r.bar != "" {
		rows++
	}
	return rows
}

// menuRows is how many rows of the menu block this pane draws: all of it, or
// what is left after the rest of the chrome and one row of transcript.
//
// **The block never bounds itself**, which is the composer's rule for the
// composer's reason: a completion menu runs to completionRows+1 and a plan card
// to eight lines, more than a short pane or one of four grid panes has spare,
// and a frame one row taller than it was given scrolls the alt screen away on
// every draw. Bounded here rather than by App because only the pane knows what
// is left - the caller would have to measure this pane's chrome to find out,
// which is this arithmetic twice.
func (r Room) menuRows() int {
	if r.menu == "" {
		return 0
	}
	return max(min(lipgloss.Height(r.menu), r.height-r.baseChrome()-minTranscriptHeight), 0)
}

// minHeight is the shortest pane this room draws: its chrome, plus one row of
// conversation. Below it the room stops shrinking rather than drawing a broken
// box, which is the discipline the DM applies at minDMHeight.
//
// Measured rather than stated as a constant, because the room's composer is a
// row taller than a DM's whenever it is carrying a target line - and a floor
// that was one row out is a card drawn one row too tall, which scrolls the alt
// screen on every draw.
func (r Room) minHeight() int { return r.chromeHeight() + minTranscriptHeight }

// renderRoom is the seam renderAll is reached through, so a test can count how
// often the whole room goes back through glamour. Reach renderAll through
// this, never directly: a direct call is invisible to the counter and the
// fast-path tests keep passing while no longer discriminating.
// TestNothingReachesRenderAllExceptThroughItsCountingSeam holds that for both
// panes, which is what makes this comment checkable.
var renderRoom = Room.renderAll

// renderAll re-derives every line at the current width, each attributed to
// whoever said it, and writes the resulting row count into the caller-owned
// line copy. Before already owns its combined slice; SetSize passes said.slice.
// Call it through renderRoom, never directly.
func (r Room) renderAll(lines []roomLine) []block {
	blocks := make([]block, 0, len(lines)+1)
	// First, so it scrolls away as the room fills - see banner.go. Re-derived
	// here rather than stored, which is what makes a width change re-wrap it
	// with everything else.
	if !r.reclaimed {
		banner := roomBanner(r.blockWidth())
		banner.laidOut = blockLines(banner, true)
		blocks = append(blocks, banner)
	}
	for i := range lines {
		lines[i].rows = 0
		// A line the current focus hides keeps rows == 0 and contributes no
		// block, so the geometry (which sums rows) needs no notion of "shown vs
		// exists" - and its glamour render is skipped, not rendered then dropped.
		if r.focus != "" && !focusAdmits(lines[i], r.focus, r.managerID) {
			continue
		}
		b := renderRoomBlock(lines[i].ev, lines[i].by, r.blockWidth(), r.expandAll || r.expanded[lines[i].id])
		if b.text != "" {
			b.laidOut = blockLines(b, false)
			lines[i].rows = len(b.laidOut)
			blocks = append(blocks, b)
		}
	}
	return blocks
}
