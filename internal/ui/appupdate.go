package ui

// The message loop: Update folds one message and writes what opening a
// conversation queued, update dispatches it, stream folds a batch of frames.
// Split out of app.go at this seam - the loop is one subject, the model and its
// constructors another - when app.go reached the 800-line hard max.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

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
	next, linger := next.armNoticeLinger()
	next, upgrade := next.dueUpdateCheck(msg)
	next = next.helpLaidOut()
	if ask == nil && roomAsk == nil && blink == nil && linger == nil && upgrade == nil {
		return next, cmd
	}
	return next, tea.Batch(cmd, ask, roomAsk, blink, linger, upgrade)
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
		return a.bangResult(m)

	case adoptedMsg:
		return a.adoptArrived(m)
	case resumeReadyMsg:
		return a.resumeArrived(m)

	case pathScanMsg:
		return a.pathsScanned(m)

	case imageDropMsg:
		return a.imageDropped(m)

	case authResultMsg:
		return a.authResult(m).autoWakeRecovered()

	case mcpSignedInMsg:
		return a.mcpSignedIn(m)

	case frameMsg:
		// The frame is folded first, then two things read the result: the
		// heartbeat, which may need starting, and ⌃Q's ask, which this frame
		// may have settled. See park.go's closing.
		next, cmd := a.applied([]rpc.Frame{m.Frame}).settle()
		return next, tea.Batch(cmd, next.closing())

	case heartbeatMsg:
		return a.beatArrived()

	case noticeExpiredMsg:
		return a.noticeExpired(m), nil
	case edgeScrollMsg:
		return a.edgeTicked()

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
		return a.unrecalled(m.Unsent).sendRecalled()

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
		a.clicks = clickRun{} // every keystroke ends a click run; see multiclick.go
		// Before anything acts on a draft - the view hides its pane's. See workflowKey.
		next, cmd, took := a.workflowKey(m)
		if took {
			return next.cleared(), cmd
		}
		// And /wake-help, which takes ⌫ and typing before a hidden draft's selection can.
		a = next
		next, cmd, took = a.helpKey(m)
		if took {
			return next.cleared(), cmd
		}
		a = next
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
		a = a.disarmed().closePicker().closeBoard().closeRewind().closeResume()
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
	a = a.applied(m.frames)
	if m.done {
		// A held /rename is dequeued and drawn but not sent, and a gone agent's
		// queue dropped: the connection is gone, so the write is discarded.
		a, _ = a.flushQueued()
		return a.hungUp(m.err)
	}
	// Once per batch, not per frame: at most one held message goes out per agent
	// per read - two in one batch would race each other mid-turn. See queue.go.
	next, cmd := a.settle()
	// Re-armed unconditionally, unless one of those frames was ⌃Q's answer.
	return next, tea.Batch(cmd, next.reading())
}
