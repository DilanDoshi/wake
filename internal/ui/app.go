package ui

// The root model: the room, the DMs open beside it, the two sidebars, and the
// only thing in this package that holds a connection. Everything below it
// receives messages and draws.
//
// # What made this a room
//
// One line. daemon.fanOut already broadcasts every session's events to every
// attached client, and apply used to drop the ones whose SessionID was not its
// own. That discard *was* the room: turning it into a fold costs one consumer
// replacing one filter - no daemon change, no new frame kind, no second
// connection. See Fleet, which is the consumer.

import (
	"errors"
	"io"
	"net"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// noticeHeight is the one row App reserves for the error surface.
	//
	// Reserved unconditionally, and that is the point. Taking the row only
	// when there is something to say would change the panes' height at an
	// arbitrary moment, and SetSize returns the reader to the newest line -
	// so a failure arriving would silently end somebody's scrollback. One row
	// always costs less than that.
	noticeHeight = 1

	// noticePrefix marks the reserved row as a report about Wake rather than
	// something the agent said.
	noticePrefix = "! "

	// pageFraction is the share of the window a page key moves: half, so the
	// reader keeps context on both sides of the jump rather than landing in a
	// screenful of text with nothing recognisable in it.
	pageFraction = 2

	// detachAdvice is what to do about a conversation that cannot continue.
	//
	// An *ended* session is the one thing /resume cannot bring back - stop is
	// the deliberate ending and spec §2 makes it the one there is no way back
	// from - so the honest instruction is still to leave, and the key that
	// leaves is ⌃O. It was ⌃C, which parks now: a sentence naming ⌃C here would
	// send somebody to park a session that has already ended.
	detachAdvice = "⌃O to detach"
)

// errDaemonHungUp is the stream ending with nothing wrong on the wire - the
// daemon exited, or closed this client. It is not a read failure, and it is
// not silence either: every later keystroke goes nowhere, so it is reported.
var errDaemonHungUp = errors.New("the daemon hung up; this session is no longer attached")

// Stream is the daemon's half of an attached connection: the frames read off
// it, and the error that ended them.
//
// It is injected rather than opened here, and that is deliberate twice over.
//
// rpc.ReadFrames starts a goroutine and a scanner that buffers ahead of its
// caller, so calling it inside the tea.Cmd that waits for one frame would mean
// a new goroutine and a fresh 64KB scanner per received event, each abandoned
// after a single receive - an unbounded goroutine leak, plus silent frame loss
// from bytes stranded in the abandoned buffers. One reader, for the life of
// the connection.
//
// And the client must consume rpc.FrameHello before it may spawn anything: a
// dial to a daemon in graceful shutdown succeeds into the listen backlog and
// is never accepted, so the handshake is the only thing that tells an attached
// connection from a connection to a daemon that will never answer. That read
// happens before there is a model, which means the reader cannot belong to
// one. See cmd/wake.
//
// The zero value is a model with no daemon behind it, which renders and sends
// nothing - the shape the unit tests use.
type Stream struct {
	Frames <-chan rpc.Frame
	Errs   <-chan error
}

// Dialer opens a fresh connection to the same daemon and session, and reports
// that session and the whole fleet with it - the report reattached folds.
//
// It is injected rather than written here, for the reason Stream is: this
// package may not dial. Which socket, whether an unanswering daemon is shutting
// down or crashed, and whether the session still exists are all questions
// cmd/wake already answers (connect() and daemon.Status) - a second hello-or-EOF
// handshake inside a view is exactly the parallel implementation this forbids.
//
// Nil means "no way back": the hang-up is reported and nothing else happens,
// exactly as before - what the unit tests and any un-wired future caller get.
type Dialer func() (net.Conn, Stream, rpc.SessionStatus, *rpc.Status, error)

// eventMsg carries one session event into the Bubble Tea loop, addressed to
// the conversation that is open.
type eventMsg struct{ Event core.Event }

// errMsg carries a transport or session failure into the view.
type errMsg struct{ Err error }

// frameMsg is one frame off the daemon, undecided.
//
// Update decides what a kind means, not the reader. It is the single-frame form
// of streamMsg and folds through the same apply, so the two cannot drift.
type frameMsg struct{ Frame rpc.Frame }

// streamMsg is what the drain goroutine hands the draw loop: every frame that
// arrived while it was busy, how many were lost before them, and the end of the
// stream when it comes.
//
// A batch rather than a frame, and that is a cost decision with a measurement
// behind it. Bubble Tea calls View after every single message and View is a
// fixed ~400µs whatever the transcript holds, so one message per frame meant
// one whole frame drawn per event the fleet produced - at a busy fleet's rate
// that is more than a core spent drawing frames nobody could read. Folding a
// batch draws once for all of them.
type streamMsg struct {
	batch

	// gen identifies the connection this batch came off. A reattach replaces
	// the inbox, and a read still outstanding against the old one must not
	// re-arm the new one - two live reads would each take half the frames.
	gen uint64
}

// reattachedMsg is the connection and fleet report a reattach came back with.
type reattachedMsg struct {
	conn    net.Conn
	stream  Stream
	session rpc.SessionStatus
	fleet   *rpc.Status
}

// App is the root model. It owns the RPC connection; no view below it touches
// a socket or a process.
//
// Its methods take value receivers and return a new App, which is Bubble Tea's
// contract as well as this project's: a branch of Update that returns the
// receiver silently discards whatever it just did.
type App struct {
	conn net.Conn

	// fleet is every agent this client knows about, and the only thing that
	// inspects an event. See fleet.go.
	fleet Fleet

	room   Room
	cards  Cards
	roster Roster
	groups Groups

	// picker is the menu Wake draws for a bare /effort or /model, and the zero
	// value is "there is not one". Beside cards rather than in them: it is
	// Wake's own and appears in no fleet report. See picker.go.
	picker Picker
	rewind RewindPicker // esc esc's own picker, on an idle empty conversation; see rewind.go

	// completion is the menu under the focused draft: what could finish the
	// word at the cursor. Rebuilt per keystroke, never per frame, and its `@`
	// half is read off this goroutine. See completion.go.
	completion completion

	// dms are the conversations that are open, keyed by session id. A map
	// because multiple DMs open at once is a mode rather than a nice-to-have:
	// thinking *with* two agents is the case the design's amendment named, and
	// the DM a person returns to must still hold what it held.
	//
	// It is one an App copy shares rather than owns, like in, so it is copied on
	// write. Keyed on *DM: a write replaces the pointer, never mutating a value.
	dms map[string]*DM

	// boardDMs holds one rendered conversation per on-screen tile of the tiled
	// board, and boardHistoryAsked records which have a disk-history ask
	// outstanding (how many events each held at ask time - historyArrived's rule).
	// Both are built while board.Up && board.Tiled and dropped whole on close -
	// the board's own lifecycle - and isolated from the pane map (a.dms): the ⇥ ring,
	// App.wants and hideDM never see these, so a tile costs nothing where a pane
	// is counted. See boardtranscript.go.
	boardDMs          map[string]*DM
	boardHistoryAsked map[string]int

	// compacting is which sessions are mid-compaction and when each began, so a
	// DM can draw `✻ Compacting conversation…` while it runs. On App and keyed by
	// session id for tails' reason. Folded from two notices; see compacting.go.
	compacting map[string]time.Time

	// askedHistory is which sessions this client has already asked for a
	// transcript, and pendingHistory is the asks Update has not yet written.
	// Both are copied on write for dms' reason. See history.go.
	askedHistory   map[string]int
	pendingHistory []string

	// dmOrder is the order those conversations were opened, which is the order
	// ⇥ moves the keys through them. A map has no order - Go randomises the
	// iteration deliberately - so a ring derived from dms would put the same
	// three agents in a different sequence on every press, and attention rank
	// would reorder them between frames. See App.chats.
	//
	// Copied on write, like dms and for the reason Fleet copies its own order
	// slice: an append on a value receiver writes into a backing array a
	// discarded App still points at.
	dmOrder []string

	// grid is which of those conversations are on screen and where. See grid.go.
	grid Grid

	// board is the fleet overview, drawn instead of the grid while up. See
	// board.go.
	board Board

	// focus is the conversation with the keys, "" for the room.
	//
	// One field rather than a pane enum beside an id: with a grid there is no
	// bounded set of panes to enumerate, and two fields saying which pane is
	// live is the disagreement App.withFocus exists to prevent. Every pane in
	// the grid has a composer, so "which one takes a keystroke" is exactly
	// "which conversation".
	focus string

	// sessionID is the session this client attached to: the one a hang-up
	// reattaches to, and the one whose ending closes its composer. It is not
	// "the session this App is about" - the room is about all of them - which
	// is why apply no longer reads it.
	sessionID string

	// in is the drain: a goroutine filling a bounded buffer that Update reads
	// in batches. It is a pointer, and it is one of the two things in this
	// package a copy of an App shares rather than owns - because there is one
	// connection however many copies of the model exist. See inbox.go for why
	// the drain may not be the goroutine that draws.
	in *inbox

	// gen counts connections. It rises on every reattach so a read still
	// outstanding against the connection that hung up is discarded rather than
	// re-arming the new one.
	gen uint64

	// dial is how this model gets back to its session after the daemon hangs
	// up. Nil disables reattaching entirely.
	dial Dialer

	// sessions is how this model sees the claude sessions on this machine that
	// Wake did not start. Nil means it cannot look, and says so. See adopt.go.
	sessions Sessions

	// adoptOut keeps a stalled machine walk to one goroutine. See adopt.go.
	adoptOut bool

	// reattaching stops a second attempt starting while one is in flight. The
	// stream ends exactly once per connection so this is belt and braces, and
	// it is what makes "reattaching…" a state rather than a message.
	reattaching bool

	// layout is the geometry the panes are currently *laid out* for, and
	// pending is the newest one the terminal has reported or the operator has
	// dragged to. They differ only during a drag: re-wrapping is the expensive
	// thing this model does, so it happens once the drag stops, and until then
	// the frame is drawn at the old wrap and clipped to the new terminal.
	// Height needs no such treatment - it moves a window over lines that are
	// already rendered. See geometry.go.
	layout  Layout
	pending geometry

	// geoGen identifies the newest geometry change, so the timer belonging to
	// an older one is ignored when it lands. One counter, because there is one
	// pending geometry: a window drag and a divider drag are the same change to
	// the same value, and racing two of them is the defect this replaced.
	geoGen uint64

	// dragAt is which divider a drag is in flight on, noDrag for none: the
	// button went down on that divider's column and has not come up. Motion
	// means nothing without it - mouse mode 1002 reports motion whenever a
	// button is held, wherever the pointer is. An index rather than a flag
	// because a grid has one divider per pair of columns.
	dragAt int

	// dragRows says the hand is on a stacked column's rule rather than on the
	// divider right of it. Both are dragAt's column, and only one can be held,
	// so this is which - not a second drag that could race the first.
	dragRows bool

	// sel is the text a drag has taken, selTop the screen row that pane's
	// transcript starts at, selRows how many rows of it are drawn, and
	// selecting whether the button is still down. One selection for the whole
	// app. See mouse.go, which is all four.
	sel       selection
	selTop    int
	selRows   int
	selecting bool
	cdrag     composerDrag // query-box drag geometry, captured at its start; see composersel.go
	rosterHit rosterHit    // the roster row resolved at a press, opened on an empty release; see screensel.go

	// out is the terminal, for the one thing Wake writes that is not a frame.
	// Nil writes nowhere. See clipboard.go and cmd/wake/output.go.
	out io.Writer

	beating bool // a heartbeat tick is already scheduled; see beat.go

	// rl is the timed rate-limit notice: what it last showed and a generation
	// counter, so an overlapping warning's expiry cannot clear a newer one
	// early. See ratelimit.go.
	rl rateLimitState

	// ended records that the daemon has reported this client's own session
	// gone. It is what stops its composer from accepting messages nothing will
	// ever read, and it latches: a later report cannot un-end a session, and
	// re-reporting the same ending on every subsequent push would be a flood.
	ended bool

	// pendingStarts are the sessions this client has asked the daemon to start
	// and not seen arrive: a fork from ⌃F, a fresh agent from `/new`. It is how
	// either one opens its own conversation - the session does not exist until
	// the daemon has started it, so each minted id is remembered and the next
	// report that names one is the signal. A member leaves when it arrives and
	// when the daemon refuses it - a refusal is addressed to the *new* session's
	// own id, which is the property daemon.fork's comment is about.
	//
	// A set rather than one slot because two presses are the feature rather
	// than a slip, and one slot lost the first fork. Copied on write, like dms
	// and dmOrder; awaitingStart and startSettled are its only two writers and a
	// guard derives that. **One set for both verbs** and why: starts.go.
	pendingStarts map[string]struct{}

	// parking are the sessions this client asked to park and has not yet seen
	// parked. pendingStarts' shape, for pendingStarts' reason - see parkArrived.
	parking  map[string]struct{}
	quitting map[string]struct{} // asked to /quit, not yet ended; departedQuit (quit.go) drops each from the fleet on the confirming report

	// waking: asked to wake, not yet seen back. See wakeArrived.
	waking map[string]struct{}

	// modes is the permission mode this client believes each session is in,
	// written only by what the daemon reported - a receipt, or a turn's init.
	// Never by the keystroke that asked. Absent means the spawn mode. See
	// mode.go, which is the whole argument.
	modes map[string]string

	// asking is the mode this client last *requested* per session and has not
	// been answered about. A different question from modes, and deliberately a
	// different field: one is what the session is in, the other is what was
	// asked for. Only the cycle reads it. See mode.go's cyclingFrom.
	asking map[string]string

	// quit is ⌃Q: what it asked for, and what the daemon said back. park.go is
	// the whole of why it is a wait rather than a flag, and what may settle
	// one; nothing about it belongs in this file's line count.
	quit parkAll

	// mention is what a leading @name means, and its zero value is the cheap
	// reading. mention.go is the whole of why it is a mode and what it may not
	// touch; nothing about it belongs in this file's line count.
	mention MentionMode

	// detachArmed is a ⌃O pressed once and not yet confirmed. detach.go is why
	// leaving takes two, and App.disarmed takes this back with the card's arm.
	detachArmed bool

	// escArmed is whether the next ⎋ clears the draft or opens the rewind
	// picker, rather than stopping the turn again. See escape.go.
	escArmed bool

	// quitArmed is a ⌃Q pressed once and not yet confirmed. park.go is why the
	// fleet park takes two presses now, and App.disarmed takes this back with the
	// other arms. Removing ⌃Q from the emergency kill-switch is the matching half
	// - see cmd/wake/killswitch.go.
	quitArmed bool

	// roomAsk is the room's own history ledger, which is not the DM's. See
	// roomhistory.go.
	roomAsk roomAsk
}

// NewRoomApp returns the root model: a room over the whole fleet, seeded with
// the report this client already holds.
//
// conn is written to and never read from - stream is the read side, opened
// once by whoever performed the handshake. Both may be zero, which gives a
// model that renders and does nothing else.
//
// seed is the fleet report the caller already has: the spawn's own confirmation
// carries every session, and a reattach fetched one on the way in, so the room
// opens with a roster rather than with an empty one that fills in over the next
// thirty seconds. Nil is legitimate and means "wait for the first push".
//
// The drain starts here rather than in Init, and the difference is not
// cosmetic: rpc.ReadFrames is already running by the time this is called, and
// every moment between the handshake and Bubble Tea's first message is a moment
// in which the daemon is writing to a socket nobody is reading. Init runs after
// the terminal handshake, which is not a bounded amount of time.
func NewRoomApp(conn net.Conn, stream Stream, seed *rpc.Status) App {
	a := App{
		conn:   conn,
		fleet:  NewFleet().WithStatus(seed),
		room:   NewRoom(),
		dms:    map[string]*DM{},
		grid:   NewGrid(),
		dragAt: noDrag,
		// The right sidebar starts open - what the room is for, who is there and
		// what they are doing; Layout hides it by width anyway. The left
		// workspaces sidebar (ShowGroups) is hidden for now - kept in the code
		// (groups.go, Regions) but never enabled and with no ⌃G. See groups.go.
		layout: Layout{ShowRoster: true},
	}
	// A blocked agent in the seed is answerable immediately rather than after
	// the next push: the report carries a request id for exactly the client
	// that was not there when the ask went past.
	a.cards = a.cards.Reconcile(seed)
	// The room over a fleet that has been talking since before this window
	// existed opens with nothing in it. This is where it asks; see
	// roomhistory.go for the other moment and for why there are only two.
	a = a.askRoomHistory(liveSessions(seed)...)
	if stream.Frames != nil {
		a.in = newInbox()
		go a.in.pump(stream)
	}
	return a.retarget()
}

// WithOpenDM opens a conversation with one agent beside the room and makes it
// this client's own session - the one a hang-up reattaches to.
//
// `wake` and `wake attach` both land here: they spawned or resolved exactly one
// session, and that conversation is what the operator asked for. The room is
// beside it rather than instead of it.
func (a App) WithOpenDM(sessionID, name string) App {
	if sessionID == "" {
		return a
	}
	a.sessionID = sessionID
	return a.openDMWith(sessionID, name)
}

// WithDialer returns an App that reattaches after a hang-up. Without one, a
// hang-up is reported and the conversation is over - which is what it was.
func (a App) WithDialer(d Dialer) App { a.dial = d; return a }

// Init starts the read loop and the cursor.
//
// The blink is scheduled here because Composer focuses itself in its
// constructor and has no Init of its own to return the command from; without
// this the cursor is drawn once and never moves again.
func (a App) Init() tea.Cmd { return tea.Batch(textarea.Blink, a.listen()) }

// listen waits for the drain to have something and takes everything it has.
//
// One read outstanding at a time, re-armed by Update. It never blocks Update:
// the wait happens on the tea.Cmd's own goroutine, and the frames it collects
// were taken off the socket by the pump long before this ran.
func (a App) listen() tea.Cmd {
	in, gen := a.in, a.gen
	if in == nil {
		return nil
	}
	return func() tea.Msg {
		<-in.ready
		return streamMsg{batch: in.take(takeLimit), gen: gen}
	}
}

// streamEnd is why the daemon's stream ended.
func streamEnd(errs <-chan error) error {
	if errs == nil {
		return errDaemonHungUp
	}
	if err := <-errs; err != nil {
		return err
	}
	return errDaemonHungUp
}

// Update handles one message.
//
// Nothing here blocks. A write to the daemon is a command, not a call: the
// socket belongs to a daemon that may have stopped reading, and this is the
// goroutine that draws every frame.
// Update folds one message and then writes anything opening a conversation
// queued.
//
// The drain is here rather than at each site that opens one because there are
// seven of those - a key, a click, the ring, next-blocked, a fork arriving, a
// spawn arriving, and cmd/wake building the model - and only two are in a
// position to return a tea.Cmd. One seam instead of seven signatures.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := a.update(msg)
	next, ok := model.(App)
	if !ok {
		return model, cmd
	}
	// Seed a board DM for any newly-visible tile before its history ask is drained
	// below, so the reply comes back on the same round trip. A no-op off the tiled
	// board (ensureBoardDMs' own gate), so every other Update pays nothing.
	next = next.ensureBoardDMs()
	next, ask := next.takeHistoryAsks()
	next, roomAsk := next.takeRoomHistoryAsks()
	next, blink := next.refocusBlink(a.focus)
	if ask == nil && roomAsk == nil && blink == nil {
		return next, cmd
	}
	return next, tea.Batch(cmd, ask, roomAsk, blink)
}

func (a App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		return a.resized(m.Width, m.Height)

	case geometrySettledMsg:
		return a.settled(m.gen), nil

	case streamMsg:
		return a.stream(m)

	case bangResultMsg:
		return a.bangResult(m), nil

	case adoptedMsg:
		return a.adoptArrived(m)

	case pathScanMsg:
		return a.pathsScanned(m)

	case imageDropMsg:
		return a.imageDropped(m)

	case mcpResultMsg, authResultMsg:
		return a.panelResult(m), nil

	case frameMsg:
		// The frame is folded first, then two things read the result: the
		// heartbeat, which may need starting, and ⌃Q's ask, which this frame
		// may have settled. See park.go's closing.
		next := a.apply(m.Frame)
		next, cmd := next.beat()
		next, rl := next.armRateLimitClear()
		return next, tea.Batch(cmd, rl, next.closing())

	case heartbeatMsg:
		return a.beatArrived()

	case rateLimitClearMsg:
		return a.rateLimitCleared(m), nil

	case parkAllMsg:
		return a.parkAllSettled(m.err)

	case eventMsg:
		return a.appendEvent(m.Event), nil

	case reattachedMsg:
		return a.reattached(m)

	case errMsg:
		// Not a hang-up: those arrive as a streamMsg and go through reattach.
		// This is a write that failed, or a reattach that could not connect.
		a.reattaching = false
		notice.Report("%v", m.Err)
		return a, nil

	case tea.MouseMsg:
		// A wheel or a click is the operator plainly doing something else, and
		// it never reaches App.key - so the disarm there cannot see it. It is
		// also the likeliest thing between an accidental arm and the retype
		// that follows: the mouse is what you reach for to scroll the room and
		// read the card you are being asked about.
		return a.disarmed().mouse(m)

	case copiedMsg:
		return a.copied(m)

	case tea.KeyMsg:
		// A live query-box selection turns ⌫/delete into "remove what is
		// highlighted" - read before cleared() wipes the selection, since that
		// runs on every keystroke. See deleteSelectedDraft.
		if next, cmd, handled := a.deleteSelectedDraft(m); handled {
			return next, cmd
		}
		// Clears the highlight and then does its own job - see cleared.
		a = a.cleared()
		if model, cmd, handled := a.key(m); handled {
			return model, cmd
		}
		// A key the App did not take still takes back an armed card settle, so
		// this is not the same disarm as key's: that one covers the keys the
		// switch above claims, this one the keys that go on to the composer.
		// ⌫ is the likeliest of them, because it is what somebody presses when
		// a character they typed did not appear.
		//
		// It closes an open picker for the same reason and in the same place: a
		// key that reaches the composer is somebody who has stopped choosing,
		// and a menu left up over a draft would take the ↵ meant to send it.
		// The board goes with them: a rune is somebody typing, and boardKey's
		// own close on this path is discarded with the rest of the not-handled
		// model - the disarm comment above is about exactly that.
		a = a.disarmed().closePicker().closeBoard().closeRewind()
	}

	// Whatever the App did not take goes to the pane that has the focus.
	c, cmd := a.composer().Update(msg)
	a = a.withComposer(c)
	if _, typed := msg.(tea.KeyMsg); !typed {
		// Only a keystroke can have changed where ↵ would send. This arm also
		// takes the cursor's blink, twice a second forever, and retargeting on
		// one is a fleet-sized name scan for a draft nobody touched - which is
		// the "work per frame that could be work per change" the
		// non-negotiables forbid, arriving on a timer instead of a frame.
		return a, cmd
	}
	next, scan := a.retarget().recompleted().scanning()
	return next, tea.Batch(cmd, scan)
}

// stream folds one batch of frames and re-arms the read.
//
// The gap is reported before the frames that follow it, which is the ordering
// the daemon's own flush uses and for the same reason: a reader learns its
// transcript has a hole *before* rendering what comes after the hole, rather
// than being told about one they have already read past.
func (a App) stream(m streamMsg) (tea.Model, tea.Cmd) {
	if m.gen != a.gen {
		// A read left over from the connection that hung up. Its frames were
		// already accounted for by whoever replaced the inbox; re-arming on it
		// would put two live reads on one buffer.
		return a, nil
	}
	if m.dropped > 0 {
		// Counted at the buffer, and previews are not in it: a lost token is
		// replaced by the block behind it, so notedGap fires only when the record
		// itself lost something. See inbox.go, and notedGap for the invalidation
		// this shares with the daemon's own gap.
		a = a.notedGap(m.dropped)
	}
	for _, f := range m.frames {
		a = a.apply(f)
	}
	if !m.done {
		// The heartbeat starts here because this is the path production frames
		// take: a status that put an agent into a turn schedules the first
		// tick. frameMsg's own beat covers only the single-frame form.
		next, tick := a.beat()
		next, rl := next.armRateLimitClear()
		// Re-armed unconditionally, unless one of those frames was ⌃Q's answer.
		return next, tea.Batch(tick, rl, next.reading())
	}
	return a.hungUp(m.err)
}

// notedGap reports a frame gap and drops the per-turn beliefs a missing frame
// could have staled: the permission mode, and the turn's tool and counts.
//
// One helper for the two gap producers, so their invalidation cannot drift: the
// window's own ring counts a drop onto streamMsg.dropped, and the daemon's client
// queue reports its overflow as a FrameError carrying rpc.Frame.Dropped. Both
// mean the same thing - the record has a hole - and both demand the same two
// forgettings. A permission-mode receipt may be in the hole, and a mode kept
// across one is a mode this window cannot vouch for in the unsafe direction (see
// forgotModes); a turn's result may be in it too, and its boundary is what would
// have cleared the turn state the roster draws (see Fleet.ForgetTurns).
//
// Not inDM: a gap is not itself a turn-end, and clearing it blindly here would
// leak an in-flight DM turn's remaining prose into the room. It is reconciled
// instead at the report's own working→idle edge (Fleet.WithStatus), the
// gap-robust second observable of the turn-end fold clears it on. See bugs.md.
func (a App) notedGap(n int) App {
	notice.Report("dropped %d frames: this window fell behind, so the conversation above has a gap", n)
	a = a.forgotModes()
	a.fleet = a.fleet.ForgetTurns()
	return a
}

// apply folds one frame into the model.
//
// # The discard that was the room
//
// Every session's events reach every attached client - that is what makes a
// reattaching TUI join the stream mid-conversation - and this used to drop the
// ones whose SessionID was not its own. Nothing new crosses the socket to make
// a group chat: one consumer replaces one filter, and Fleet.Observe is that
// consumer. It updates the record the right sidebar draws, the counts the left
// sidebar draws, and it returns the events the room draws - one pass, so the
// three cannot disagree about what arrived.
//
// The frame's SessionID is the id Wake spawned under and is the right one to
// attribute by: an event's own id changes under the session after a /clear, and
// the frame's does not.
func (a App) apply(f rpc.Frame) App {
	switch f.Kind {
	case rpc.FrameEvent:
		if f.Event == nil {
			return a
		}
		return a.observe(f.SessionID, *f.Event)

	case rpc.FrameHistoryReply:
		return a.historyArrived(f)
	case rpc.FrameRoomHistoryReply:
		return a.roomHistoryArrived(f)
	case rpc.FrameRewindTargetsReply:
		return a.rewindTargetsArrived(f)

	case rpc.FrameError:
		// The daemon's own queue overflowed: it dropped frames for this client
		// and says so with a typed count. That is the same hole the window's ring
		// produces one layer up, so it routes through the one invalidation rather
		// than being recognised by the daemon's sentence - the text-matching
		// client.go's gapNotice already complains two other callers do. See
		// notedGap. (This frame carries no SessionID, so nothing below is owed it.)
		if f.Dropped > 0 {
			return a.notedGap(f.Dropped)
		}

		// A refused spawn arrives this way rather than as a dropped
		// connection, so a client that ignored these would show an empty
		// conversation for a session that never started.
		//
		// Every one of them is reported now, not only this client's own. A DM
		// filtered these because it was 1:1 and another agent's failure was
		// somebody else's window; the room is every agent, so an error about
		// any of them is about something on this screen.
		// If this refusal names a fork, stop waiting for a conversation that
		// will never arrive - most often because the parent is mid-turn. Keyed
		// on the id and nothing else: an error about *another* agent must leave
		// the wait alone, or one unrelated crash while a fork is starting
		// cancels it and the fork then opens nothing. startSettled is a no-op
		// for an id nothing is waiting on, which is most error frames.
		//
		// The text says when it could be forked instead; that is the daemon's
		// sentence and it is reported below unchanged.
		a = a.startSettled(f.SessionID)
		notice.Report("%s", a.errorText(f))
		return a

	case rpc.FrameStatusPush, rpc.FrameStatusReply:
		return a.applyStatus(f.Status).parkAllTaken(f.Kind)

	case rpc.FrameHello:
		// Consumed before this model existed - whoever opened the connection
		// had to see it to know the connection was accepted at all.
		return a

	default:
		// Not silence: the daemon and this binary ship together, so a kind
		// this build does not know is a version skew worth seeing once.
		notice.Report("the daemon sent a frame this build does not understand: %q", f.Kind)
		return a
	}
}
