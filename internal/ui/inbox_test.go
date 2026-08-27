package ui

// The property C1 turns on: the socket is drained by something that is not the
// goroutine that draws.
//
// These go through a real connection and a real rpc.ReadFrames rather than
// through the inbox directly, because the failure being fixed is a socket
// filling up - and a test that pushes frames into a channel by hand cannot
// distinguish a drain that works from one that does not.

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// wired is an App attached to one end of a pipe, with the daemon's end
// returned so a test can write frames onto it.
//
// net.Pipe is synchronous and unbuffered, which is what makes it the right
// instrument here: a write to it completes only when somebody has read it. A
// model that has stopped draining cannot hide behind a socket buffer.
func wired(t *testing.T) (App, net.Conn) {
	t.Helper()
	fresh(t)

	mine, daemonEnd := net.Pipe()
	frames, errs := rpc.ReadFrames(mine)
	t.Cleanup(func() {
		_ = daemonEnd.Close()
		_ = mine.Close()
	})

	a := dmApp(nil, Stream{Frames: frames, Errs: errs}, "s1", "alex")
	m, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m.(App), daemonEnd
}

// writeFrames puts frames on the daemon's end, failing rather than hanging if
// a write does not complete. A stalled write is exactly the failure under test,
// and a test that hangs on it reports nothing at all.
func writeFrames(t *testing.T, conn net.Conn, fs ...rpc.Frame) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		for _, f := range fs {
			if err := rpc.WriteFrameTo(conn, f); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("writing to the client: %v", err)
		}
	case <-time.After(cmdTimeout):
		t.Fatalf("the daemon's write to this client did not complete within %v: nothing is draining the connection", cmdTimeout)
	}
}

// held is what the drain has in hand: how many frames, and whether it has seen
// the connection end. Defined here rather than in inbox.go because nothing but
// a test needs to look inside the buffer.
func (b *inbox) held() (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.n, b.ended
}

// settled waits until the drain holds what a test is about to fold.
//
// The pump is a goroutine - that is the entire point of it - so "the daemon
// wrote those frames" and "the model can fold those frames" are two different
// moments. A test that steps between them folds a batch that is short by
// whatever was still in flight, and then fails on an assertion about the
// missing half. Polling the drain rather than sleeping keeps that from being a
// timing guess.
func settled(t *testing.T, a App, frames int, ended bool) {
	t.Helper()
	deadline := time.Now().Add(cmdTimeout)
	for {
		n, done := a.in.held()
		if n >= frames && done == ended {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("within %v the drain held %d frames (ended=%v), want %d (ended=%v)", cmdTimeout, n, done, frames, ended)
		}
		time.Sleep(time.Millisecond)
	}
}

// The whole of C1's third link. Update is the goroutine that renders, and a
// re-wrap of a large transcript is a quarter of a second of it; the daemon
// hangs up on a client whose write blocks for five. So the drain may not be
// Update.
//
// Nothing here calls Update at all - the model is built and then ignored, which
// is a render that never finishes. Every frame must still leave the daemon.
//
// Mutation check: dropping the pump goroutine and reading the stream inside
// listen again fails this at "the daemon's write to this client did not
// complete within 2s", on the very first frame.
func TestTheConnectionIsDrainedWhileNothingIsDrawing(t *testing.T) {
	_, daemonEnd := wired(t)

	// Far more than any socket buffer would hold, and none of it is read by
	// anything that draws.
	fs := make([]rpc.Frame, 500)
	for i := range fs {
		fs[i] = eventFrame("s1", fmt.Sprintf("frame %d", i))
	}
	writeFrames(t, daemonEnd, fs...)
}

// And the frames survive the wait: a drain that kept the daemon happy by
// throwing everything away would pass the test above.
func TestFramesThatArrivedWhileNothingWasDrawingAreStillRendered(t *testing.T) {
	app, daemonEnd := wired(t)
	writeFrames(t, daemonEnd,
		eventFrame("s1", "said while busy one"),
		eventFrame("s1", "said while busy two"),
	)
	settled(t, app, 2, false)

	var m tea.Model = app
	m, _ = step(t, m, app.listen())

	for _, want := range []string{"said while busy one", "said while busy two"} {
		if !strings.Contains(shown(m), want) {
			t.Errorf("a frame that arrived while nothing was drawing was lost: %q\n%s", want, shown(m))
		}
	}
}

// --- the bound on the buffer ---------------------------------------------

// Unbounded buffering is the other way to kill this app, so the buffer drops.
// Dropping silently is the way to make it lie, so it says so - in the daemon's
// own vocabulary, because a reader who sees "dropped" twice should not have to
// learn two spellings of it.
func TestABacklogTooBigToHoldIsReportedAsAGap(t *testing.T) {
	b := newInbox()
	const over = 25
	for i := range inboxFrames + over {
		b.add(eventFrame("s1", fmt.Sprintf("frame %d", i)))
	}

	got := b.take(inboxFrames)
	if got.dropped != over {
		t.Errorf("the inbox reported %d dropped frames, want %d", got.dropped, over)
	}
	if len(got.frames) != inboxFrames {
		t.Fatalf("the inbox handed over %d frames, want the %d it can hold", len(got.frames), inboxFrames)
	}

	// The oldest went, not the newest. What a reader needs after an outage is
	// where the agent is now.
	newest := fmt.Sprintf("frame %d", inboxFrames+over-1)
	if last := got.frames[len(got.frames)-1].Event.Text; last != newest {
		t.Errorf("the newest frame held is %q, want %q: the buffer dropped the wrong end", last, newest)
	}
	oldestKept := fmt.Sprintf("frame %d", over)
	if first := got.frames[0].Event.Text; first != oldestKept {
		t.Errorf("the oldest frame held is %q, want %q", first, oldestKept)
	}
}

// And the gap reaches the person, not just the struct.
func TestAGapIsReportedOnTheNoticeRow(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(streamMsg{batch: batch{dropped: 12, frames: []rpc.Frame{eventFrame("s1", "after the hole")}}})

	if !strings.Contains(shown(m), "dropped 12 frames") {
		t.Errorf("the view lost frames and said nothing:\n%s", shown(m))
	}
}

// A frame taken out of the ring must not be held by it afterwards. The buffer
// is 2,048 frames and each one carries a whole assistant turn, so a ring that
// never lets go keeps megabytes alive for the life of a window that is meant to
// be cheap to leave open all day.
func TestTakenFramesAreReleased(t *testing.T) {
	b := newInbox()
	b.add(eventFrame("s1", "transient"))
	if got := b.take(takeLimit); len(got.frames) != 1 {
		t.Fatalf("took %d frames, want 1", len(got.frames))
	}
	for i, f := range b.ring {
		if f.Kind != "" || f.Event != nil {
			t.Fatalf("ring slot %d still holds %+v after the frame was taken", i, f)
		}
	}
}

// --- a preview may never cost the record a slot ----------------------------

// The finding: --include-partial-messages makes every output token an ordinary
// frame, so a stall spends the ring on previews and what gets evicted is
// completed blocks, permission requests, receipts and turn endings. Nothing on
// the permission wire times out, so an evicted ask is an agent blocked forever
// with nothing on screen to answer.
//
// Mutation check: take the partial arm out of add and this fails with the ask
// gone and a full ring's worth reported dropped.
func TestAFloodOfTokensCannotEvictAnAsk(t *testing.T) {
	b := newInbox()
	b.add(kindFrame("s0", core.KindPermissionRequest, "may I run this"))

	// Four ring-fulls of tokens across a working fleet - far past the ~1,455
	// frames a 250ms stall costs at the corpus's fastest recorded rate.
	for i := range inboxFrames * 4 {
		b.add(kindFrame(fmt.Sprintf("s%d", i%benchFleetSize), core.KindPartialText, "tok "))
	}

	got := b.take(inboxFrames)
	if got.dropped != 0 {
		t.Errorf("the inbox reported %d dropped frames after a flood of tokens, want 0: a preview is not a hole in the record", got.dropped)
	}
	if len(got.frames) == 0 {
		t.Fatal("the inbox handed over nothing")
	}
	if k := got.frames[0].Event.Kind; k != core.KindPermissionRequest {
		t.Fatalf("the oldest frame held is a %q, want the %q the tokens arrived behind: the ask was evicted", k, core.KindPermissionRequest)
	}
}

// Occupancy is what closes it: a session's unconsumed tokens share one slot
// however fast it is talking, so the ring's headroom stops depending on the
// output token rate.
func TestOneSessionsTokensShareOneSlot(t *testing.T) {
	b := newInbox()
	for range inboxFrames * 2 {
		b.add(kindFrame("s1", core.KindPartialText, "tok "))
	}
	if held, _ := b.held(); held != 1 {
		t.Errorf("a session streaming %d tokens took %d slots, want 1", inboxFrames*2, held)
	}
}

// And the fold loses nothing a reader would have seen: the tokens arrive as one
// event carrying all of them, in the order they were written.
func TestAFoldedPartialCarriesEveryTokenItAbsorbed(t *testing.T) {
	b := newInbox()
	for _, tok := range []string{"Fix", "ed the ", "retry"} {
		b.add(kindFrame("s1", core.KindPartialText, tok))
	}

	got := b.take(takeLimit)
	if len(got.frames) != 1 {
		t.Fatalf("took %d frames, want the one slot three tokens shared", len(got.frames))
	}
	if text := got.frames[0].Event.Text; text != "Fixed the retry" {
		t.Errorf("the folded preview reads %q, want %q", text, "Fixed the retry")
	}
}

// A completed block closes the fold. Without that the next block's first tokens
// would be appended to a preview the block is about to clear, and the preview
// would stay blank for the rest of the turn.
func TestTokensAfterACompletedBlockStartANewPreview(t *testing.T) {
	b := newInbox()
	b.add(kindFrame("s1", core.KindPartialText, "first block"))
	b.add(eventFrame("s1", "first block"))
	b.add(kindFrame("s1", core.KindPartialText, "second"))

	got := b.take(takeLimit)
	if len(got.frames) != 3 {
		t.Fatalf("took %d frames, want 3: a block between two previews is what separates them", len(got.frames))
	}
	if text := got.frames[2].Event.Text; text != "second" {
		t.Errorf("the preview after the completed block reads %q, want %q: it folded into the one the block had already superseded", text, "second")
	}
}

// The ring full of record, and a token arriving: the token goes, not the
// record. This is the invariant in one line - a partial never evicts anything.
func TestAPreviewIsRefusedRatherThanEvictingTheRecord(t *testing.T) {
	b := newInbox()
	for i := range inboxFrames {
		b.add(eventFrame("s1", fmt.Sprintf("frame %d", i)))
	}
	b.add(kindFrame("s2", core.KindPartialText, "tok"))

	got := b.take(inboxFrames)
	if got.dropped != 0 {
		t.Errorf("the inbox reported %d dropped frames, want 0: the token that found no room is not a gap", got.dropped)
	}
	if len(got.frames) != inboxFrames {
		t.Fatalf("the inbox handed over %d frames, want the %d it can hold", len(got.frames), inboxFrames)
	}
	if first := got.frames[0].Event.Text; first != "frame 0" {
		t.Errorf("the oldest frame held is %q, want %q: a token pushed the record out", first, "frame 0")
	}
}

// A token that is evicted by the record is not confessed either. App.stream
// reports a drop as "the conversation above has a gap" and calls forgotModes on
// it; a lost preview is neither a hole nor a reason to forget a mode.
func TestALostTokenIsNotReportedAsAGap(t *testing.T) {
	b := newInbox()
	// One session's tokens, folded into one slot, then a ring's worth of record
	// behind them - so the slot the tokens hold reaches the head and is evicted.
	b.add(kindFrame("s1", core.KindPartialText, "tok"))
	for i := range inboxFrames {
		b.add(eventFrame("s2", fmt.Sprintf("frame %d", i)))
	}

	got := b.take(inboxFrames)
	if got.dropped != 0 {
		t.Errorf("the inbox reported %d dropped frames, want 0: what it lost was a preview", got.dropped)
	}
	if first := got.frames[0].Event.Text; first != "frame 0" {
		t.Errorf("the oldest frame held is %q, want %q", first, "frame 0")
	}
}

// A fold may not outlive the slot it names. take hands frames over in batches,
// so a preview leaves the buffer while its session is still streaming - and a
// fold still pointing at that slot appends the next token to whatever is
// written there afterwards, which is a cleared slot and a nil Event.
//
// Mutation check: drop the unfold from take and this panics.
func TestAFoldDoesNotOutliveTheSlotItNames(t *testing.T) {
	b := newInbox()
	b.add(kindFrame("s1", core.KindPartialText, "first"))
	if got := b.take(takeLimit); len(got.frames) != 1 {
		t.Fatalf("took %d frames, want 1", len(got.frames))
	}

	b.add(eventFrame("s2", "somebody else's turn"))
	b.add(kindFrame("s1", core.KindPartialText, "second"))

	got := b.take(takeLimit)
	if len(got.frames) != 2 {
		t.Fatalf("took %d frames, want 2", len(got.frames))
	}
	if text := got.frames[0].Event.Text; text != "somebody else's turn" {
		t.Errorf("the frame in the reused slot reads %q: a stale fold wrote over it", text)
	}
}

// And end to end, through a real connection: tokens that shared a slot still
// draw as the sentence they were written as.
//
// The trailing hello is how this waits for both of them - with the fold they
// are one slot, so "the drain holds two frames" is the only thing that says the
// second token has landed, and frames arrive on one connection in order.
func TestAFoldedPreviewStillDrawsEveryToken(t *testing.T) {
	app, daemonEnd := wired(t)
	writeFrames(t, daemonEnd,
		kindFrame("s1", core.KindPartialText, "Fixed the "),
		kindFrame("s1", core.KindPartialText, "retry loop"),
		rpc.Frame{Kind: rpc.FrameHello},
	)
	settled(t, app, 2, false)

	var m tea.Model = app
	m, _ = step(t, m, app.listen())

	if !strings.Contains(shown(m), "Fixed the retry loop") {
		t.Errorf("the preview lost tokens the fold absorbed:\n%s", shown(m))
	}
}

// The bound on a folded preview. A draw loop that stalls for a minute would
// otherwise grow one string per session for as long as it stalls, and appending
// to a string costs its length.
func TestAFoldedPreviewIsBounded(t *testing.T) {
	b := newInbox()
	for range (foldChars / 4) + 100 {
		b.add(kindFrame("s1", core.KindPartialText, "tok "))
	}

	got := b.take(takeLimit)
	if n := len(got.frames[0].Event.Text); n > foldChars {
		t.Errorf("a folded preview holds %d characters, want at most %d", n, foldChars)
	}
	// It keeps the newest, the way partial.add does - the tail is what a pane
	// draws.
	if !strings.HasSuffix(got.frames[0].Event.Text, "tok ") {
		t.Error("the folded preview was cut at the wrong end: a pane draws its tail")
	}
	// And the bound is above what any pane can draw, so nothing drawable is cut.
	if foldChars < previewChars(foldWidth) {
		t.Errorf("foldChars = %d, want at least previewChars(%d) = %d", foldChars, foldWidth, previewChars(foldWidth))
	}
}

// --- the end of the stream ------------------------------------------------

// The hang-up is the last thing the reader hears, never the first. A batch that
// reported the ending before handing over the frames that preceded it would
// lose the agent's final words behind the notice about losing it.
func TestTheLastFramesArriveBeforeTheHangUp(t *testing.T) {
	frames, errs := closedStream(eventFrame("s1", "the last thing it said"))
	a := sizedApp(t, frames, errs, "s1")
	settled(t, a, 1, true)

	var m tea.Model = a
	m, cmd := step(t, m, a.listen())

	if !strings.Contains(shown(m), "the last thing it said") {
		t.Errorf("the final frame was lost behind the hang-up:\n%s", shown(m))
	}
	if notice.Count(errDaemonHungUp.Error()) != 1 {
		t.Errorf("the hang-up was not reported once:\n%s", shown(m))
	}
	if cmd != nil {
		t.Error("the model re-armed a read on a connection that has ended")
	}
}
