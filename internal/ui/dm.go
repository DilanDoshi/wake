package ui

// The DM view: a full 1:1 conversation with one agent. This file owns the
// model and how the pane is sized; dm_blocks.go owns what one event looks like;
// dmtranscript.go owns how those blocks become the scrollback, the run fold
// included.

import (
	"cmp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// minTranscriptHeight keeps a row of conversation on screen no matter how
	// short the pane is. Below the resulting floor the DM stops shrinking
	// rather than drawing a broken box - the discipline Composer applies to
	// width, applied to height.
	minTranscriptHeight = 1

	// minDMHeight is the shortest View the DM draws: Composer's rows and one row
	// of transcript. TestViewFloorsBelowItsMinimumSize pins it against the
	// composer it is derived from. The pane's name is no longer a row of its own
	// - it sits in the composer's top border, so it costs nothing.
	minDMHeight = composerViewHeight + minTranscriptHeight

	// composerViewHeight is what an unarmed Composer.View returns - the
	// three-row bordered box, and no legend row, since the always-on hints moved
	// to the status bar. View measures the real composer rather than trusting
	// this; it exists so minDMHeight can be a constant.
	composerViewHeight = 3

	// minBlockWidth is the narrowest width a block renderer is asked for.
	// Below it they degenerate: ToolCall truncates a line to an ellipsis and
	// glamour stops wrapping. The DM asks for a readable width and lets the
	// transcript clip to the real pane, which it does anyway.
	minBlockWidth = 20

	agentPrefix = "@"
)

// headerStyle names the agent at the top of the pane.
var headerStyle = lipgloss.NewStyle().Foreground(Accent).Bold(true)

// DM is a full 1:1 conversation with one agent - the unfiltered view.
// Everything the group chat hides shows here: thinking, every tool call, the
// diff an edit carries, and both halves of the conversation.
//
// Its methods take value receivers and return a new DM, so a caller holding an
// older copy keeps the transcript it had.
//
// It renders; it never touches a process, and it never de-duplicates. Whoever
// feeds it must pick one source for the user's turn - see userBlock.
//
// Call render.Prime before tea.NewProgram. The first markdown block otherwise
// pays for a blocking terminal handshake inside the draw loop.
type DM struct {
	// behindAsk dims this pane's transcript, so a framed card reads as modal
	// rather than as the newest block. Set for the draw; see WithAsk.
	behindAsk bool

	// writing titles the composer while it holds an answer rather than a
	// message. Set for the draw; see WithWriting.
	writing string

	SessionID string
	Name      string

	// Agent is the fleet's record of whose conversation this is, set by
	// App.dmPane on the copy it draws. One field rather than a copy of each
	// fact it holds: the pane needs the state to say a conversation is parked,
	// the turn's start for the heartbeat, and the directory, model and context
	// for the status bar - and six fields kept in step by hand is five chances
	// to forget one.
	//
	// A parked conversation is otherwise indistinguishable from a live one -
	// same transcript, same composer - so ⌃C read as having done nothing.
	Agent Agent

	// ParentName is the conversation this one was forked from, by name, or
	// empty. A name and never an id: this is the addressing surface, and eight
	// hex characters here are what names exist to replace. Set when the
	// conversation is opened, from the fleet report - see App.openDMWith.
	ParentName string

	// events is the transcript's source of truth; tr is the same content
	// rendered at width, as lines. The rendering is memoized because glamour
	// is far too slow to run on every frame, and re-derived on resize because
	// a block wrapped for a window that no longer exists is worse than the
	// cost of redoing it.
	//
	// Both grow for the life of the session and both are copied on every
	// append, which is why neither is a plain slice - see chunked.
	events chunked[core.Event]
	tr     transcript

	// subs is each dispatch's own frames, keyed by the dispatch id they carry,
	// and viewing is which of them this pane is drawing - "" for the
	// conversation itself.
	//
	// A subagent is a conversation rather than a paragraph: its frames used to
	// land in events, where they were 55-77% of a dispatching turn's transcript.
	// They are held unrendered because only one can be on screen at a time; see
	// appendForwarded for why that matters at fleet scale.
	subs    map[string]chunked[core.Event]
	viewing string

	// checklist is this conversation's own accumulation of TaskCreate/TaskUpdate
	// ops, so a list built before this client attached comes back off disk and
	// the live half continues from it - the Fleet fold only ever saw what
	// arrived live. Folded in Append and re-derived in Before. See checklist.go.
	checklist checklist

	// marks are the event indices the last-read boundaries sit above, ascending
	// and bounded by maxLastReadRules. Plural because the transcript holds one
	// rule per absence, and a single slot behind N artefacts is a re-wrap that
	// regenerates one of them and silently drops the rest. The zero value is a
	// conversation nobody has left. See lastread.go.
	marks []int

	// expanded is whether this conversation's tool results are drawn whole
	// rather than collapsed to their first lines. Per conversation and not per
	// App: thirty open DMs re-rendered on one keypress is thirty transcripts
	// back through glamour, which holds one process-global mutex for the whole
	// fleet. See expand.go.
	expanded bool

	// What is known about this conversation's tool calls, all keyed on a
	// tool_use id: the invocation itself, whether its result landed and
	// failed, and which the operator has clicked open. See toolblocks.go for
	// why the first two are shared rather than copied on write and the third
	// is not.
	calls    map[string]*core.ToolCall
	outcomes map[string]bool
	opened   map[string]bool

	// runOpen is which folded tool runs a click has opened, keyed on the run's
	// key - the first use's id. Copied on write like opened, for opened's
	// reason: a click may rewrite an entry already there. See rollup.go.
	runOpen map[string]bool

	// runKey is the key of the run at the tail of the transcript while it is
	// still folded and growing, and "" when the last block was not one. A new
	// tool event restyles that run's one summary line rather than adding a
	// second; it is recomputed from the events after any whole re-render, never
	// held as a line index, because a line renumbers on a re-wrap and an id does
	// not. See rollup.go's trailingRun.
	runKey string

	// runTally is that run's use count per category, maintained in O(1) as it
	// grows so a new event formats the summary from it rather than walking the
	// whole run each time. Reset when a run starts, rebuilt from the events only
	// where they change under it - a restore. See advanceRun.
	runTally rollupTally

	// partial is the block this agent is writing, if one is arriving. It is
	// deliberately beside events and tr rather than in either: it is a preview
	// that the completed block replaces, and storing it would put every
	// sentence in the transcript twice. See partial.go for the cost argument
	// that decides its shape.
	partial partial

	composer Composer
	width    int
	height   int

	// sel is the selection this pane is being drawn with, set for the draw by
	// App.dmPane and not held - App owns the one selection, and a copy kept
	// here would go stale against it.
	sel marked

	// csel is a selection taken inside the query box rather than the transcript,
	// set for the draw the same way. At most one of sel and csel is non-empty:
	// the one drag the app holds is in one surface or the other.
	csel marked

	// bar is the rendered status bar, and barFrom is what it was rendered
	// from. It is cached because it is the one thing in this pane that reads
	// the *filesystem* - gitref walks for a .git and reads HEAD - and
	// chromeHeight consults it on every View. At the shimmer's 20Hz that was
	// four stats a frame for as long as any agent anywhere was working, which
	// is the "work per frame that could be work per change" the first
	// non-negotiable forbids.
	//
	// State is part of the key so a branch the operator checks out mid-session
	// is picked up: a status push fires on every state change, so the bar is
	// re-read at each turn boundary rather than never.
	bar     string
	barFrom barKey

	// menu is everything pinned between the transcript and the composer - the
	// card, the picker and the completion menu, assembled by App.menuBlock and
	// handed over whole by App.dmPane, clipped here (see menuRows). Room.menu's
	// field for Room.menu's reason, and it joins the preview, the heartbeat and
	// the dispatch list below the transcript rather than above it, which is the
	// rule View's own comment already states for those three.
	menu string

	// chrome is what chromeHeight returned when the transcript was last sized.
	//
	// The transcript's height is height minus that, so anything which changes
	// the chrome has to re-size the pane. Width and height used to be the only
	// two, and both are arguments; the heartbeat's row and the status bar's are
	// not - they appear when an agent starts a turn and when the first fact
	// about a session arrives, neither of which is a resize. Without this the
	// pane draws one row more than it was given, and a frame one row too tall
	// scrolls the alt screen on every draw.
	chrome int
}

// drawStatusBar is the seam the bar is rendered through, so a test can count
// how often it actually runs. Reach it through this, never by calling
// statusBar directly: drawing the bar reads the filesystem, and a direct call
// is invisible to the counter that keeps that off the draw loop.
var drawStatusBar = statusBar

// barKey is everything statusBar reads. A value type so "has anything changed"
// is one comparison rather than a list somebody has to keep in step.
//
// The identity colour is deliberately absent: the bar recedes in the muted grey
// every bar wears and does not take the hue (see statusBar), so a /color change
// moves nothing here and belongs in no key that would redraw for it.
type barKey struct {
	width  int
	dir    string
	model  string
	mode   string
	state  string
	used   int
	window int
	prs    *prSet // a PR arrives mid-turn with no other bar fact moving, so the key must carry it; prSet.same keeps the pointer stable so it does not redraw per frame
}

// withBar re-renders the status bar if anything it is drawn from has moved, and
// returns the receiver untouched otherwise.
//
// The mode comes off this pane's own composer, which App.dmFor has already set
// from App.modeOf - the same value the legend names, so the two lines of one
// pane cannot disagree about it. It is part of the key because the bar is drawn
// per change: a mode left out of it would be the one fact here that goes stale.
func (d DM) withBar(width int) DM {
	mode := d.composer.Mode()
	key := barKey{
		width: width, dir: d.Agent.Cwd, model: d.Agent.Model, mode: mode, state: d.Agent.State,
		used: d.Agent.ContextTokens, window: d.Agent.ContextWindow, prs: d.Agent.prs,
	}
	if key == d.barFrom {
		return d
	}
	d.bar, d.barFrom = drawStatusBar(d.Agent, mode, width), key
	return d
}

// withAgent folds this conversation's own agent in from the fleet, or leaves it
// alone when the fleet has no row for it yet - a session that has ended, or one
// whose first report has not landed.
//
// A method on DM rather than a write at the call site so the one field the
// pane's chrome is derived from has one setter. See App.refreshedAgents.
func (d DM) withAgent(f Fleet) DM {
	if agent, ok := f.Agent(d.SessionID); ok {
		d.Agent = agent
	}
	return d
}

// NewDM returns an empty conversation with a focused composer.
func NewDM(sessionID, name string) DM {
	return DM{
		SessionID: sessionID,
		Name:      name,
		composer:  NewComposer(),
	}
}

// SetSize re-lays the conversation for a pane of w by h.
//
// Only a change of width re-wraps the transcript; a change of height moves the
// window over lines that are already rendered. That asymmetry is the whole cost
// model of this type - see View.
func (d DM) SetSize(w, h int) DM {
	d = d.withBar(max(w, minComposerWidth))
	// Sampled before anything is re-laid, the same way Append does it.
	following := d.tr.atBottom()
	if w != d.width {
		d.width = w
		d.tr = d.tr.replace(renderTranscript(d))
		// A re-wrap invalidates the offset outright: it no longer points at
		// what the reader was reading, and restoring a stale one would be a
		// worse lie than returning to the newest message.
		following = true
	}
	d.height = h
	// The composer wraps the draft as keys arrive, so it is sized here rather
	// than only when it is drawn. See Composer.SetWidth.
	// The heartbeat's row and the status bar's are chrome the draft does not own
	// either, so they come out of its allowance the same way the transcript's
	// floor does - see composerRowsIn.
	// Re-wrapped before the chrome is measured: its rows are part of what the
	// transcript's height is taken out of, so a preview laid out for the old
	// width would size the pane against rows that no longer exist.
	d.partial = d.partial.sized(d.blockWidth())
	extra := d.partial.rows()
	if d.hasBeat() {
		extra++
	}
	if d.bar != "" {
		extra++
	}
	d.composer = d.composer.SetWidth(max(w, minComposerWidth)).
		WithMaxRows(composerRowsIn(h, d.composer.overhead()+extra))

	// A height change is not that. It moves a window over lines that already
	// exist, so a reader who has scrolled back keeps their place - which is the
	// rule Room.SetSize states at length, held here identically because the two
	// panes share this cost model and a divergence between them would be a
	// scrollback that behaves differently depending on which pane it is in.
	d.chrome = d.chromeHeight()
	d.tr = d.tr.sized(max(w, minComposerWidth), max(h-d.chrome, minTranscriptHeight))
	if following {
		d.tr = d.tr.toBottom()
	}
	return d
}

// Append renders one event into the transcript and returns a new DM. Events
// with no visual representation are dropped rather than drawn as noise.
//
// Nothing here costs what the conversation so far cost. Every session's events
// come through this method whether its DM is on screen or not, in Bubble Tea's
// single Update goroutine, so work proportional to the transcript's length
// makes the whole app slower the longer it runs - which is what rebuilding the
// transcript on every event did, and what dm_transcript_test.go keeps out.
//
// The view follows the newest message only for a reader who was already at the
// bottom. Someone who has scrolled back is reading; yanking them to the newest
// line every time the agent speaks makes an hour-long session unreadable, and
// is worse than having no scrollback at all. Sampling that before the content
// changes is what makes it true.
func (d DM) Append(ev core.Event) DM {
	// A /clear blanks the pane rather than adding to it, before the folds below
	// because nothing of the gone conversation has anywhere to land. See clear.go.
	if ev.Kind == core.KindSessionReset {
		return d.clearedBySessionReset()
	}

	// Before the guards below, because a tool result with an empty body draws
	// nothing and still settles the call above it, so a bullet left dim by an
	// early return is a call that never finishes on screen. A command that
	// printed nothing is the ordinary case of that. The dispatch fold that
	// used to sit beside this moved to Fleet - see fleettasks.go - so the rows
	// exist for agents nobody has opened.
	//
	// The checklist fold is the DM's own, so the board pinned over the composer
	// (checklistpin.go) comes back on a restore off disk and the live half
	// continues from it. A create/delete moves the board's height, so re-settle
	// the chrome once here, off the draw path: View compares chromeHeight against
	// DM.chrome, and a board that grew a row after the last resize leaves that
	// memo stale, so View re-sizes the pane on a throwaway copy every frame.
	d, ev = d.foldChecklist(ev)
	if ev.Tool != nil && ev.Tool.Checklist != nil && ev.Subagent == nil {
		d = d.resettleBoard()
	}
	d = d.observedTool(ev).settled(ev)

	// The preview, which is the one event kind that never reaches the
	// transcript. Returning here is what keeps the claim in partial.go true:
	// nothing below this line runs for a token.
	switch ev.Kind {
	case core.KindPartialText:
		d.partial = d.partial.sized(d.blockWidth()).add(ev.Text)
		return d
	case core.KindAssistantText, core.KindTurnEnd:
		// The block that supersedes it, or the turn that ended without ever
		// producing one - an interrupt, where nothing else would clear it.
		//
		// A turn end does *not* break the run: it draws nothing and is not
		// stored, so renderAll never sees it, and breaking a run on it live
		// would fold differently after a re-wrap than before. Two turns whose
		// tools are adjacent with no prose between them fold together, which is
		// the same thing they do on screen anyway.
		d.partial = d.partial.cleared()
	}

	if dispatch := forwardedTo(ev); dispatch != "" {
		return d.appendForwarded(dispatch, ev)
	}

	// The last-read boundary, if this is the first thing to arrive since somebody
	// left. It breaks a run, and it is read *before* the fold is classified: a
	// result that would have extended the run is orphaned by the break, so it
	// must be a stray drawn as itself rather than folded here and then dropped by
	// renderAll after a re-wrap. Asked here and again in renderAll, from the one
	// rule, so a re-wrap cannot put it somewhere else - see lastread.go.
	marker, breaksRun := d.markerBefore(d.events.len())

	// A tool run folds to one rollup line. A use starts or grows a run; a result
	// grows the live one. A result reaching here with no live run - or with the
	// run just broken by a boundary - is a stray: it falls through and draws as
	// itself, the way renderAll draws it too. See rollup.go.
	fold := isToolUse(ev) || (d.isToolBlock(ev) && d.runKey != "" && !breaksRun)
	var b block
	if !fold {
		b = d.renderEvent(ev)
		// A checklist op renders nothing but is stored all the same, so a restore
		// off disk can re-derive the board from it - the one invisible event this
		// path keeps. Every other empty render is a stray that consumes no slot.
		if b.text == "" && !d.isChecklistOp(ev) {
			return d
		}
		d.runKey, d.runTally = "", nil // a non-tool block, a checklist op or a stray ends the run
	}

	// Whether this pane is drawing the conversation at all. While a subagent is
	// open the conversation still accumulates - nothing is dropped for being off
	// screen - and the transcript it accumulates into is rebuilt on the way back.
	drawn := d.viewing == ""
	following := d.tr.atBottom()

	if breaksRun {
		if drawn {
			d.tr = d.tr.add(marker)
		}
		d.runKey, d.runTally = "", nil // the boundary closes the run above it
		// The mark has earned its rule, so the cap applies to it now - see
		// crossed for why not when it was set. Applied whether or not the rule
		// was drawn: the anchor is what a re-wrap regenerates it from, and
		// returning from a subagent is a re-wrap.
		d = d.crossed()
	}
	d.events = d.events.append(ev)
	if fold {
		// Advanced whether or not the pane draws it, so runKey and runTally track
		// the run even while a subagent is on screen - the same reason the
		// conversation keeps accumulating off screen.
		d = d.advanceRun(ev)
	}
	if !drawn {
		return d
	}
	if fold {
		d = d.drawFold(ev)
	} else if b.text != "" {
		// A checklist op stored above draws no line: it is the board pinned over
		// the composer, not a block. Everything else that reaches here draws.
		d.tr = d.tr.add(b)
	}
	if following {
		d.tr = d.tr.toBottom()
	}
	return d
}

// forwardedTo is the dispatch an event belongs to, and "" for one that belongs
// to the conversation itself.
//
// Two of the three shapes core.Subagent marks stay with the conversation, and
// each for its own reason:
//
//   - A **receipt** (Result set) is the agent reporting about a subagent
//     rather than the subagent speaking. It is the parent's own tool result.
//   - An **ask** carries an agent id and no dispatch, because the control
//     envelope names none. There is no transcript to file it under, and a
//     blocked agent reachable only by drilling into it is one nobody unblocks.
func forwardedTo(ev core.Event) string {
	if ev.Subagent == nil || ev.Subagent.Result != "" {
		return ""
	}
	return ev.Subagent.Dispatch
}

// appendForwarded files one of a subagent's frames under its dispatch, and
// draws it only if that is the transcript on screen.
//
// The events are kept unrendered. Only one subagent can be read at a time, so
// rendering every dispatch as it streams would be work per frame for panes
// nobody is looking at - and a fleet of thirty agents each running three
// subagents is what that multiplies by. Opening one renders it once, which is
// the same cost a width change already pays.
func (d DM) appendForwarded(dispatch string, ev core.Event) DM {
	subs := make(map[string]chunked[core.Event], len(d.subs)+1)
	for k, v := range d.subs {
		subs[k] = v
	}
	subs[dispatch] = subs[dispatch].append(ev)
	d.subs = subs
	if d.viewing != dispatch {
		return d
	}
	b := d.renderEvent(ev)
	if b.text == "" {
		return d
	}
	following := d.tr.atBottom()
	d.tr = d.tr.add(b)
	if following {
		d.tr = d.tr.toBottom()
	}
	return d
}

// Viewing swaps the pane onto one dispatch's transcript, or back to the
// conversation's own with "".
//
// The whole transcript is re-derived, which is what a width change already
// does and for the same reason: every line on screen has just been replaced.
// The reader lands on the newest line of whatever they opened.
//
// A dispatch nothing has forwarded draws an empty transcript rather than
// staying put. Task.Openable is what keeps the keys off those rows; this is
// the floor under it, because silently doing nothing reads as a broken key.
func (d DM) Viewing(dispatch string) DM {
	if dispatch == d.viewing {
		return d
	}
	d.viewing = dispatch
	d.tr = d.tr.replace(renderTranscript(d)).toBottom()
	return d
}

// Viewed is the dispatch this pane is drawing, or "" for the conversation.
func (d DM) Viewed() string { return d.viewing }

// View draws the header, the transcript, and the composer, measuring exactly
// width by height for any size at or above the floor.
//
// Call SetSize whenever the pane size changes, and pass View the same size.
// View re-lays for a size it was not given, so an early frame is never wrong -
// but it has a value receiver and cannot memoize the result, so a caller who
// keeps passing a width SetSize never saw re-runs the whole transcript through
// glamour on every single frame. That is not a local cost: glamour renders
// behind one process-global mutex shared by every session, so one DM in that
// state serializes the whole fleet's drawing behind it.
//
// Passing a new *height* is cheap and always has been - only width re-wraps.
// TestViewDoesNotReRenderAtASizeAlreadySet pins the fast path.
//
// The wiring requirement is not only about speed. SetSize returns the reader to
// the newest message, so a caller passing a size SetSize never saw resets the
// scroll position on *every frame* - scrollback there is not slow, it does not
// work at all. That is the more expensive half of getting this wrong.
func (d DM) View(width, height int) string {
	// The guard holds the scroll position as much as the render cost, and what
	// pins it is TestANewEventDoesNotYankAScrolledReaderToTheBottom rather than
	// any of the render counters: without it every frame re-enters SetSize and
	// its GotoBottom. Keep that test scrolled, or this guard comes unpinned.
	//
	// The chrome is the third thing the transcript's height depends on, and
	// unlike the other two it changes without anybody resizing anything: see
	// DM.chrome. It is compared rather than assumed for the same reason width
	// is - a pane drawn one row taller than it was given scrolls the alt screen
	// away on every frame, which is what it did.
	d = d.withBar(max(width, minComposerWidth))
	if width != d.width || height != d.height || d.chromeHeight() != d.chrome {
		d = d.SetSize(width, height)
	}
	w := max(width, minComposerWidth)
	transcript := d.tr.view(d.sel)
	if d.behindAsk {
		transcript = quieted(transcript)
	}
	rows := []string{transcript}
	// Between the transcript and the composer, where Claude Code puts it: the
	// newest thing said, then what is being said now, then what is happening,
	// then where you type.
	if d.partial.view != "" {
		rows = append(rows, d.partial.view)
	}
	if beat := d.heartbeat(); beat != "" {
		rows = append(rows, beat)
	}
	// Above the composer with the heartbeat, because both answer "what is
	// happening now" - the heartbeat for this agent, this board for the tasks it
	// set itself. Below the transcript rather than above it, which is also what
	// keeps mouse.go's startSelection correct: it measures the rows *above* a
	// transcript, and these are not among them.
	if board := d.checklistPin(w); board != "" {
		rows = append(rows, board)
	}
	// Last before the composer: the card, picker or completion menu is answered
	// by typing, so it belongs at the query bar. Clipped to the same count
	// chromeHeight took out of the transcript, so the two cannot disagree.
	if menu := firstRows(d.menu, d.menuRows()); menu != "" {
		rows = append(rows, menu)
	}
	// The bar rides inside the composer, drawn between the box and the legend -
	// the info row over the keys row. baseChrome still counts it separately,
	// because the composer it measures carries no bar (this WithBar is a draw-
	// time overlay, like WithTitle and WithColor), so the height stays right.
	comp := d.composer.
		WithBar(d.bar).
		WithColor(d.Agent.Color).
		WithTitle(cmp.Or(d.writing, agentPrefix+d.Name+d.ancestry()+d.standing())).
		View(w)
	comp = highlightComposerBlock(comp, d.csel, composerTextLeft, w-composerRightInset)
	rows = append(rows, comp)
	return strings.Join(rows, "\n")
}

// standing is what the title says about an agent that is not running: nothing
// for a live one, because the ordinary case is not worth a word.
func (d DM) standing() string {
	switch d.Agent.State {
	case rpc.StateParked, rpc.StateEnded:
		return " · " + d.Agent.State
	case "":
		return ""
	default:
		// The level this session thinks at, when Wake chose one. Nothing says
		// it back on claude's wire, so an agent Wake chose nothing for shows
		// nothing rather than claude's default under Wake's name.
		if d.Agent.Effort != "" {
			return " · " + d.Agent.Effort
		}
		return ""
	}
}

// Before puts a conversation's earlier events under everything it already
// holds, and re-renders.
//
// It rebuilds rather than prepending into the transcript because the transcript
// is *rendered lines*, and there is no cheap way to splice a hundred blocks in
// at the top of one. That is affordable exactly here and nowhere else: this
// runs once per conversation, on a pane the operator has just opened, with at
// most a handful of live events already in it.
//
// The reader lands on the newest line, which is the same thing a width change
// does and for the same reason: every line the conversation had has just moved.
func (d DM) Before(earlier []core.Event) DM {
	// The restored half carries tool calls, and without them a call read back off
	// disk draws no receipt and a bullet that never settles. Filed first, over the
	// unfiltered history, so the filter below can classify a result by its use -
	// a TodoWrite's result is fold-exempt only through the stored call, and
	// getting that wrong is what would keep an empty one the live path drops.
	for _, ev := range earlier {
		d = d.observedTool(ev)
	}
	// Restored history then goes through the same filter the live path applies:
	// Append never stores an event that draws nothing, so a run's boundary can be
	// the structural "any non-tool block" only if d.events never holds an
	// invisible one either. A blank assistant block or a turn end read back off
	// disk would otherwise split a run here that the live path kept whole - the
	// resize-versus-restore disagreement this fold has to avoid. See renderAll.
	earlier = d.storable(earlier)
	if len(earlier) == 0 {
		return d
	}
	events := append(append([]core.Event(nil), earlier...), d.events.slice(0, d.events.len())...)
	// Re-derive the checklist over the whole restored-plus-live sequence, in
	// order, so a list built before this client attached comes back and the live
	// half continues from it. Reset first because the live fold counted only what
	// arrived live; re-folding is idempotent for that half - the same ops give
	// the same snapshot - and gives the restored half its only one.
	d.checklist = checklist{}
	d.events = chunked[core.Event]{}
	for i := range events {
		d, events[i] = d.foldChecklist(events[i])
		d.events = d.events.append(events[i])
	}
	// The marks index events and every index has moved by exactly len(earlier),
	// so they are shifted rather than dropped. A mark means "the reader had read
	// N events when they left", and that is still true at N+k - dropping them
	// loses the *pending* one, which is the rule for the absence in progress, on
	// the one pane where a landmark matters most: it has just grown by hundreds
	// of events.
	shifted := make([]int, len(d.marks))
	for i, m := range d.marks {
		shifted[i] = m + len(earlier)
	}
	d.marks = shifted
	// The trailing run may now be a restored one this DM never folded live, so
	// the next tool event finds its summary rather than starting a second. It is
	// the one re-render that changes the events - a resize and a toggle leave the
	// tail alone, so runKey and runTally survive them untouched. Rebuilt from the
	// tail once here, where the live path keeps them in step incrementally.
	if run := d.trailingRun(); len(run) > 0 {
		d.runKey, d.runTally = run[0].Tool.ID, tallyOf(run)
	} else {
		d.runKey, d.runTally = "", nil
	}
	d.tr = d.tr.replace(renderTranscript(d)).toBottom()
	// The restored ops may have grown the board, which is chrome the transcript's
	// height is taken out of. Re-settle so the viewport is sized for it - the same
	// thing Append does after a live checklist op, and the reason the old code did
	// not need it is that the checklist was drawn in the transcript, not pinned.
	return d.resettleBoard()
}

// ScrollUp moves the reader lines back through the conversation, or forward
// for a negative count, and stops at either end.
//
// It is the only way in to a scroll position transcript has tracked and Append
// has sampled since both were written: Append deliberately does not return a
// reader who has scrolled back to the newest line, which is a promise nothing
// could keep - or break - while no caller could scroll.
func (d DM) ScrollUp(lines int) DM {
	d.tr = d.tr.scrolledUp(lines)
	return d
}

// Composer is the input box. Hold the one the DM hands back rather than
// stashing copies: two Composers share one text area internally, so a snapshot
// is not independently renderable.
func (d DM) Composer() Composer { return d.composer }

// WithComposer returns a DM carrying c.
func (d DM) WithComposer(c Composer) DM { d.composer = c; return d }

// WithParentName names the conversation this one was branched from, for the
// header. Empty for a session that is nobody's fork, which is every session
// this build had before fork existed.
func (d DM) WithParentName(name string) DM { d.ParentName = name; return d }

// WithName is a rename reaching the header.
//
// It exists because this header is the one surface in the build where a handle
// is read **in order to be typed**, rather than resolved. Everywhere else a name
// that has moved on produces a refusal; here it produces a message to whoever
// drew that name out of the pool next. A DM held the name it was opened under
// for the life of the client - `openDMWith` builds one only when the map has
// none, and `hideDM` keeps the map entry so ⌃W is reversible - so nothing about
// reopening would have cleared it either. App.renamed is the single caller and
// a guard holds every open conversation to the report.
func (d DM) WithName(name string) DM { d.Name = name; return d }

// ancestry is the header's second half. It is words rather than a glyph
// deliberately: block glyphs stand in for §17's sprites throughout this UI, but
// a rare typographic fork mark is one more thing only a human at a real
// terminal could check, and this sentence needs no font.
func (d DM) ancestry() string {
	if d.ParentName == "" {
		return ""
	}
	return forkedFromLabel + agentPrefix + d.ParentName
}

// forkedFromLabel is the separator and the words. Two spaces, like the roster's
// own gaps, so the handle still reads as a handle at a glance.
const forkedFromLabel = "  forked from "

// chromeHeight is the rows View spends on everything that is not transcript.
// The composer is measured rather than assumed so this cannot drift out of
// step with it.
func (d DM) chromeHeight() int { return d.baseChrome() + d.menuRows() }

// menuRows is how many rows of the menu block this pane draws. Room's own
// method, for Room's reason: the block never bounds itself, and only the pane
// knows what is left after the rest of its chrome.
func (d DM) menuRows() int {
	if d.menu == "" {
		return 0
	}
	return max(min(lipgloss.Height(d.menu), d.height-d.baseChrome()-minTranscriptHeight), 0)
}

// baseChrome is the chrome that is not the menu, which is what the menu's own
// allowance is measured against.
func (d DM) baseChrome() int {
	h := lipgloss.Height(d.composer.View(max(d.width, minComposerWidth)))
	// The preview's rows. Counted off the wrap the draw will use rather than
	// re-wrapped here, for the reason the bar below is read from its cache:
	// this runs inside SetSize and on every View.
	h += d.partial.rows()
	// The heartbeat's row, on the same condition heartbeat() draws one - the
	// working line or the done line. Asked as the predicate rather than by
	// rendering it: this runs inside SetSize, and the line costs a shimmer across
	// its own width to produce.
	if d.hasBeat() {
		h++
	}
	// The status bar's row. Read from the cache rather than rendered: this runs
	// inside SetSize, and drawing the bar reads the filesystem.
	if d.bar != "" {
		h++
	}
	// The task board. Counted rather than drawn, for the same reason: rendering
	// it costs a truncation per row and this runs on every re-lay.
	h += d.checklistRows()
	return h
}

// minHeight is the shortest pane this conversation draws: its chrome, plus one
// row of transcript. Below it the DM stops shrinking rather than drawing a
// broken box, which is the discipline the room applies at Room.minHeight.
//
// Measured rather than read off minDMHeight, for the reason chromeHeight is
// measured at all: the heartbeat's row and the status bar's come and go with
// the agent, and a floor a row out is a pinned card drawn a row too tall -
// which scrolls the alt screen away on every draw.
func (d DM) minHeight() int { return d.chromeHeight() + minTranscriptHeight }

// blockWidth is the width block renderers are asked for.
func (d DM) blockWidth() int { return max(d.width, minBlockWidth) }
