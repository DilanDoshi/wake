package ui

import (
	"errors"
	"net"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Getting back into a conversation the daemon hung up on.
//
// The transport is not here and must not be: this package may not dial. What is
// asserted is what the model does with a Dialer - when it reaches for one, when
// it must not, and what it keeps when a new connection arrives. cmd/wake owns
// the dialing and tests it against a real daemon.

// stubDialer is a Dialer that hands back a stream a test controls, and records
// that it was asked.
type stubDialer struct {
	calls   int
	session rpc.SessionStatus
	fleet   *rpc.Status
	err     error
	frames  chan rpc.Frame
	errs    chan error
}

func (d *stubDialer) dial() (net.Conn, Stream, rpc.SessionStatus, *rpc.Status, error) {
	d.calls++
	if d.err != nil {
		return nil, Stream{}, rpc.SessionStatus{}, nil, d.err
	}
	return nil, Stream{Frames: d.frames, Errs: d.errs}, d.session, d.fleet, nil
}

// hungUpApp is a model whose connection has just ended, with a dialer wired up.
func hungUpApp(t *testing.T, d *stubDialer) (tea.Model, tea.Cmd) {
	t.Helper()
	frames, errs := closedStream(eventFrame("s1", "said before the outage"))
	a := sizedApp(t, frames, errs, "s1").WithDialer(d.dial)
	settled(t, a, 1, true)
	var m tea.Model = a
	return step(t, m, a.listen())
}

// C1's last link. The conversation is still alive on the other side of a
// hang-up - the agent keeps working and `wake status` still lists it - so the
// only thing that made the loss permanent was having no way back.
//
// Mutation check: returning a nil command from hungUp fails this at "the model
// did not try to reattach after the daemon hung up".
func TestAHangUpReattaches(t *testing.T) {
	next, nextErrs := openStream(t, eventFrame("s1", "still working on it"))
	d := &stubDialer{session: rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateWorking}, frames: next, errs: nextErrs}

	m, cmd := hungUpApp(t, d)
	if cmd == nil {
		t.Fatal("the model did not try to reattach after the daemon hung up")
	}
	m, cmd = step(t, m, cmd)
	if d.calls != 1 {
		t.Fatalf("the dialer was asked %d times, want 1", d.calls)
	}

	// And the new connection is live: frames on it reach the transcript.
	m, _ = step(t, m, cmd)
	if !strings.Contains(shown(m), "still working on it") {
		t.Errorf("the reattached connection is not being read:\n%s", shown(m))
	}

	// The conversation that was already on screen is still on screen. It is the
	// reason the model is reused rather than rebuilt: the daemon keeps no
	// replay buffer, so a fresh model would lose everything said before the
	// outage as well as everything said during it.
	if !strings.Contains(shown(m), "said before the outage") {
		t.Errorf("reattaching threw away the transcript it already had:\n%s", shown(m))
	}
}

// What a returning client is told it missed. It cannot say what - the daemon
// keeps no replay buffer, deliberately - so it says that there is a hole and
// where the agent is now, which is the question somebody staring at a
// reconnected window actually has.
func TestReattachingSaysWhatWasMissed(t *testing.T) {
	next, nextErrs := openStream(t)
	d := &stubDialer{session: rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateBlocked, RequestIDs: []string{"req-9"}}, frames: next, errs: nextErrs}

	m, cmd := hungUpApp(t, d)
	m, _ = step(t, m, cmd)

	// Asserted on the notice sink rather than on the frame. The row is one
	// row and lipgloss truncates it to the pane, so a Contains over an
	// 80-column view can only ever see the first half of the sentence - and
	// which half that is would then be what the test was pinning.
	n, ok := notice.Latest()
	if !ok {
		t.Fatal("reattaching reported nothing at all")
	}
	for _, want := range []string{"reattached", "blocked on a permission request", "is not in the conversation above"} {
		if !strings.Contains(n.Text, want) {
			t.Errorf("the reattach notice does not say %q: %q", want, n.Text)
		}
	}
	if !strings.Contains(shown(m), "reattached") {
		t.Errorf("the reattach notice never reached the view:\n%s", shown(m))
	}
}

// The report the dialer already fetched is folded, so an ask that arrived while
// this client was gone comes back.
//
// serveClient enqueues one frame to a new connection - FrameHello - and never a
// fleet report, and ui.App writes FrameStatus exactly once (⌃Q's). So nothing
// reconciles what changed during the outage unless the reattach folds the report
// redial took on the way in. The bad case is the one nothing times out: a
// permission request raised while the client was away is on no surface -
// Cards.Reconcile needs a report to mint the card, and the roster and the
// awareness strip are drawn from the stale pre-outage fleet - and the agent is
// blocked with nobody told. This is the room's worst case, where the whole fleet
// is on screen.
//
// Mutation check: dropping the applyStatus fold from reattached fails this at
// "the ask that arrived during the outage is on no surface".
func TestAReattachFoldsTheFleetSoAnAskFromTheOutageComesBack(t *testing.T) {
	next, nextErrs := openStream(t)
	// The fleet as it is now: s1 blocked on a permission request that arrived
	// during the outage. redial/redialRoom hand this back rather than discarding
	// it - it is cmd/wake's own daemon.Status read, not a FrameStatus.
	blocked := &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateBlocked, RequestIDs: []string{"req-9"}, Tool: "Bash", ToolArg: "ls"},
	}}
	d := &stubDialer{
		session: rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateBlocked, RequestIDs: []string{"req-9"}},
		fleet:   blocked,
		frames:  next,
		errs:    nextErrs,
	}

	m, cmd := hungUpApp(t, d)
	// Baseline: nothing has folded a report yet, so the ask is on no surface.
	// Without this the test could pass on a card that was never missing.
	if _, ok := m.(App).cards.For("s1"); ok {
		t.Fatal("a card for s1 existed before the reattach folded any report, so this test proves nothing")
	}

	m, _ = step(t, m, cmd)

	// The card is back, which is what ⌃X and App.cardOf read: the ask is
	// answerable again rather than blocking the agent invisibly.
	if _, ok := m.(App).cards.For("s1"); !ok {
		t.Errorf("the reattach did not fold the fleet, so the ask that arrived during the outage is on no surface and nothing on that wire times out:\n%s", shown(m))
	}
	// And the roster row and the awareness strip, which read the fleet rather
	// than the cards, see the block too.
	if agent, ok := m.(App).fleet.Agent("s1"); !ok || agent.State != rpc.StateBlocked {
		t.Errorf("the reattached fleet does not show s1 blocked (ok=%v state=%q); the roster, the strip and ⌃X are drawn from the stale pre-outage report", ok, agent.State)
	}
}

// The fold runs before ForgetTurns, so a reattach does not come back believing a
// turn it cannot vouch for is mid-flight.
//
// WithStatus stamps a clock onto a working agent it did not already hold
// mid-turn - a fresh turn owns its clock and tokens. On a reattach that is
// exactly wrong: the boundary that would have timed the turn may have been in the
// gap, so ForgetTurns has to win, which it does only by running after the fold.
// s2 is absent from the pre-outage fleet and working in the report, which is the
// one shape where the two orders differ - a working agent already held mid-turn
// is not re-stamped either way.
//
// Mutation check: moving applyStatus after Fleet.ForgetTurns in reattached fails
// this at "adopted the report's turn clock".
func TestAReattachDoesNotAdoptTheReportsTurnClock(t *testing.T) {
	next, nextErrs := openStream(t)
	working := &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s2", Name: "robin", State: rpc.StateWorking},
	}}
	d := &stubDialer{
		session: rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle},
		fleet:   working,
		frames:  next,
		errs:    nextErrs,
	}

	m, cmd := hungUpApp(t, d)
	m, _ = step(t, m, cmd)

	agent, ok := m.(App).fleet.Agent("s2")
	if !ok {
		t.Fatal("the reattach did not fold the report, so this test proves nothing")
	}
	if !agent.startedAt.IsZero() {
		t.Error("a reattach adopted the report's turn clock; ForgetTurns must win over WithStatus's re-stamp, which needs the fold to run first")
	}
	if agent.TurnTokens != 0 {
		t.Errorf("a reattach came back holding %d turn tokens it never saw produced", agent.TurnTokens)
	}
}

// A session that has ended has nothing to reattach to, and trying would spend a
// dial to be told so.
func TestAnEndedSessionIsNotReattachedTo(t *testing.T) {
	d := &stubDialer{}
	frames, errs := openStream(t)
	a := sizedApp(t, frames, errs, "s1").WithDialer(d.dial)

	// The two arrive in that order and separately, which is how they arrive:
	// the daemon announces the ending on retire and hangs up later.
	var m tea.Model = a
	m, _ = m.Update(frameMsg{Frame: endedPush(rpc.FrameStatusPush, "s1", "exit status 1")})
	if notice.Count(endedText(rpc.SessionStatus{Error: "exit status 1"})) != 1 {
		t.Fatalf("the ending was not reported, so this test proves nothing:\n%s", shown(m))
	}
	m, cmd := m.Update(streamMsg{batch: batch{done: true, err: errDaemonHungUp}})

	if d.calls != 0 {
		t.Errorf("the model tried to reattach to a session that had ended (%d dials)", d.calls)
	}
	if cmd != nil {
		t.Error("the model scheduled work after an ended session hung up")
	}
	_ = m
}

// When reattaching cannot work, the way back has to be a command that exists.
// This is the whole of what C1 was missing, and a message naming a verb that is
// not implemented would be worse than none.
func TestAFailedReattachNamesTheWayBack(t *testing.T) {
	d := &stubDialer{err: errors.New("no daemon is running on that socket")}

	m, cmd := hungUpApp(t, d)
	m, _ = step(t, m, cmd)

	out := shown(m)
	if !strings.Contains(out, "wake attach s1") {
		t.Errorf("a failed reattach did not name the verb that gets back in:\n%s", out)
	}
	if !strings.Contains(out, "no daemon is running") {
		t.Errorf("a failed reattach did not say why it failed:\n%s", out)
	}
}

// Without a dialer nothing changes: the hang-up is reported and the
// conversation is over, which is what it was before there was a way back.
func TestWithoutADialerAHangUpIsOnlyReported(t *testing.T) {
	frames, errs := closedStream()
	a := sizedApp(t, frames, errs, "s1")

	var m tea.Model = a
	m, cmd := step(t, m, a.listen())

	if cmd != nil {
		t.Error("a model with no dialer scheduled work after the hang-up")
	}
	if !strings.Contains(shown(m), "hung up") {
		t.Errorf("the hang-up left no trace:\n%s", shown(m))
	}
}

// --- the room, which is attached to nobody -------------------------------

// Bare `wake` runs a model with a dialer and **no session**, so every sentence
// on this path has to survive having nobody to name.
//
// # Why this is not hypothetical
//
// `cmd/wake.conversationRoom` wires up a dialer (`redialRoom`) and never calls
// WithOpenDM, so App.dial is set, App.sessionID is "" and App.dms is empty. The
// hang-up itself is the documented origin story of internal/ui/inbox.go: a
// window drag re-wraps the transcript, the daemon hangs up on a client whose
// write blocks for five seconds, and this path runs.
//
// # What it must not say
//
// A bare `@`. `cmd/wake.reattach`'s own comment records that failure the last
// time it happened - an unnamed session announced as `@<unnamed>` on the notice
// row and drawn as a bare `@` in the header, *"which reads as two agents"* - and
// interpolating an empty name into these three sentences reproduces it exactly.
//
// Both sentences are asserted, because they are two format strings and fixing
// one is what leaves the other.
func TestARoomThatHangsUpNamesNobodyRatherThanABareHandle(t *testing.T) {
	next, nextErrs := openStream(t)
	// A zero SessionStatus, which is what redialRoom hands back: there is no one
	// agent in a room whose state is the thing to report.
	d := &stubDialer{frames: next, errs: nextErrs}

	frames, errs := closedStream()
	// No session id and no DM - the room-only model, built the way
	// conversationRoom builds it.
	a := sizedApp(t, frames, errs, "").WithDialer(d.dial)
	settled(t, a, 0, true)
	var m tea.Model = a
	m, cmd := step(t, m, a.listen())

	reattaching, ok := notice.Latest()
	if !ok {
		t.Fatal("a room whose daemon hung up said nothing at all")
	}
	assertNamesNobody(t, "the reattaching notice", reattaching.Text)

	m, _ = step(t, m, cmd)
	reattached, ok := notice.Latest()
	if !ok {
		t.Fatal("reattaching a room reported nothing")
	}
	assertNamesNobody(t, "the reattached notice", reattached.Text)
	if !strings.Contains(reattached.Text, "is not in the conversation above") {
		t.Errorf("the reattached notice lost the thing it is for - saying there is a hole: %q", reattached.Text)
	}
	_ = m
}

// assertNamesNobody fails on a handle with nothing after it.
//
// It looks for the prefix followed by a non-name rather than for the exact
// string `"@"`, so `@…` and `@;` and a trailing `@` are all caught: the defect
// is a name that was interpolated as empty, and which punctuation happens to
// follow it is the format string's business rather than this rule's.
func assertNamesNobody(t *testing.T, what, text string) {
	t.Helper()

	for _, bare := range []string{agentPrefix + "…", agentPrefix + ";", agentPrefix + ",", agentPrefix + " "} {
		if strings.Contains(text, bare) {
			t.Errorf("%s renders a handle with no name after it (%q): %q. A bare @ reads as an agent, "+
				"and there is no agent here - the room is attached to nobody", what, bare, text)
		}
	}
	if strings.HasSuffix(text, agentPrefix) {
		t.Errorf("%s ends in a bare handle: %q", what, text)
	}
}

// And when reattaching a room fails, the way back is a command that works.
//
// `wake attach` with nothing after it is refused by cmd/wake.checkArity, so the
// advice a room gave was an invocation the shell rejects - on the failure path,
// which is the one place somebody is reading it because nothing else worked.
// Bare `wake` is the verb that reopens a room, and it is the verb this client
// already is.
func TestAFailedRoomReattachNamesAVerbTheShellAccepts(t *testing.T) {
	d := &stubDialer{err: errors.New("no daemon is running, so there is no room to get back to")}

	frames, errs := closedStream()
	a := sizedApp(t, frames, errs, "").WithDialer(d.dial)
	settled(t, a, 0, true)
	var m tea.Model = a
	m, cmd := step(t, m, a.listen())
	m, _ = step(t, m, cmd)

	out := shown(m)
	if strings.Contains(out, "wake attach") {
		t.Errorf("a room was told to run `wake attach` with no session after it, which cmd/wake "+
			"refuses with \"takes one session id or name\":\n%s", out)
	}
	if !strings.Contains(out, "`wake`") {
		t.Errorf("a failed room reattach does not name the verb that reopens a room:\n%s", out)
	}
	if !strings.Contains(out, "no daemon is running") {
		t.Errorf("a failed room reattach did not say why it failed:\n%s", out)
	}
}

// A read left over from the connection that hung up must not re-arm the one
// that replaced it: two live reads on one buffer each take half the frames, and
// the transcript loses every other event with nothing reported.
func TestAReadFromTheOldConnectionIsDiscarded(t *testing.T) {
	next, nextErrs := openStream(t)
	d := &stubDialer{session: rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle}, frames: next, errs: nextErrs}

	m, cmd := hungUpApp(t, d)
	m, _ = step(t, m, cmd)

	stale := streamMsg{batch: batch{frames: []rpc.Frame{eventFrame("s1", "from the dead connection")}}, gen: 0}
	m, cmd = m.Update(stale)

	if cmd != nil {
		t.Error("a read from the connection that hung up re-armed the new one")
	}
	if strings.Contains(shown(m), "from the dead connection") {
		t.Errorf("a frame from the connection that hung up was rendered:\n%s", shown(m))
	}
}
