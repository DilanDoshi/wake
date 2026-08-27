// Telling a silent session from an idle one.
//
// This is the distinction nothing below the daemon can make. core reports a
// session *ending*; it has no way to report one that has stopped happening,
// and there is a real state - the agent exited while something it spawned
// kept its stdout - where Err() is nil, Events() is open and the pump is
// parked in Scan for good. An idle agent waiting for its next instruction
// looks exactly the same from outside.
//
// Every test here is built around that: two sessions equally quiet, and only
// one of them wrong.

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The whole policy in one test. Both sessions have produced no events for the
// same length of time; only one of them owes Wake a turn end.
//
// A policy of "quiet for too long is silent" passes half of this and fails
// the other half, which is the point of asserting both in one place.
func TestASilentSessionIsNotReportedAsIdleAndAnIdleOneIsNotReportedAsSilent(t *testing.T) {
	shortSilence(t, 150*time.Millisecond)
	fakeClaudeOnPath(t, "mute")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.spawn(idBeta, "alex")

	// Only one of them is asked for anything. The mute agent answers
	// neither, so from the event stream the two are indistinguishable.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "are you there?"})

	silent := c.pollState(idAlpha, rpc.StateSilent)
	if silent.QuietMS < silenceLimit.Milliseconds() {
		t.Errorf("reported silent after %dms, want at least %dms", silent.QuietMS, silenceLimit.Milliseconds())
	}

	// The comparison this test is named for, and it only means anything once
	// *both* are past the limit. They do not get there together: idBeta was
	// spawned second, so at the moment idAlpha crosses the line idBeta is
	// behind it by the gap between the two spawns, and asserting there
	// compares a session past the limit against one that has not reached it.
	// Waiting for the slower one is the precondition, established rather than
	// hoped for - pollQuietFor fails loudly if it never holds.
	untouched := c.pollQuietFor(idBeta, silenceLimit)
	if untouched.State != rpc.StateIdle {
		t.Errorf("the session nobody asked anything is %q after %dms quiet, want %q - it has been quiet at least as long as the silent one, and being quiet is not the fault", untouched.State, untouched.QuietMS, rpc.StateIdle)
	}
}

// The sharper half of the policy, and the case the brief is about: an agent
// whose process has already exited while a grandchild holds its stdout. core
// cannot see it - the pump is parked in Scan, Err() is nil, Events() is open -
// and no timer is needed either, because the next write to its stdin fails.
// That failure is proof rather than a heuristic.
func TestAnAgentWhoseProcessIsGoneIsReportedSilentRatherThanIdle(t *testing.T) {
	fakeClaudeOnPath(t, "hold")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	// The grandchild speaks only after its parent has gone, so this is the
	// proof that the agent has already exited - without it the send below
	// races the exit and lands in a pipe somebody is still reading.
	c.awaitEvent(idAlpha, "held")

	// It looks perfectly healthy right up to this point.
	if got := c.pollState(idAlpha, rpc.StateIdle); got.State != rpc.StateIdle {
		t.Fatalf("state = %q before anything was sent, want idle", got.State)
	}

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "still with us?"})

	silent := c.pollState(idAlpha, rpc.StateSilent)
	if silent.Error == "" {
		t.Error("a session reported silent said nothing about why")
	}
	// And the report is not the end of it: the operator has a verb that
	// works on it, which is the only reason making the distinction matters.
	c.send(rpc.Frame{Kind: rpc.FrameKill, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)
}

// A client should not have to poll to find out an agent stopped responding.
// Nothing in the fleet asks; the daemon has to say.
func TestTheDaemonAnnouncesAStateChangeWithoutBeingAsked(t *testing.T) {
	shortSilence(t, 150*time.Millisecond)
	fakeClaudeOnPath(t, "mute")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "anything"})

	// awaitState, not pollState: this is about the daemon pushing, so the
	// test must never ask.
	got := c.awaitState(idAlpha, rpc.StateSilent)
	if got.ID != idAlpha {
		t.Fatalf("announced %+v, want %s", got, idAlpha)
	}
}

// A session that is answering is working, not silent, however long the
// individual gaps are. The state exists to be actionable, and a state that
// fires on a healthy agent is worse than no state at all.
func TestAWorkingSessionIsNotReportedSilentWhileItIsStillTalking(t *testing.T) {
	shortSilence(t, 400*time.Millisecond)
	fakeClaudeOnPath(t, "slow")
	t.Setenv(fakeDelayEnv, "50ms")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	for i := range 8 {
		c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "tick"})
		c.awaitEvent(idAlpha, "tick")
		for _, s := range c.status().Sessions {
			if s.ID == idAlpha && s.State == rpc.StateSilent {
				t.Fatalf("turn %d: a session that just answered is reported silent (quiet %dms)", i, s.QuietMS)
			}
		}
	}
}

// A permission ask stops an agent dead until a human answers, and that is not
// a fault - it is the highest-priority thing in the fleet. Reporting it as
// silent would bury the one state that needs somebody.
func TestABlockedSessionIsBlockedRatherThanSilent(t *testing.T) {
	shortSilence(t, 150*time.Millisecond)
	fakeClaudeOnPath(t, "ask")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	blocked := c.pollState(idAlpha, rpc.StateBlocked)
	if got := soleAsk(t, blocked); got != askRequestID {
		t.Errorf("blocked session names request %q, want %q - a client that reattached needs it to answer at all", got, askRequestID)
	}

	// Well past the silence limit, and still blocked rather than silent.
	time.Sleep(3 * silenceLimit)
	for _, s := range c.status().Sessions {
		if s.ID == idAlpha && s.State != rpc.StateBlocked {
			t.Fatalf("state = %q after waiting, want it still blocked on a human", s.State)
		}
	}
}

// An interrupt landing on an outstanding ask **withdraws** it, and the daemon
// has to stop reporting an ask nobody will ever answer.
//
// This is the whole of the defect the recording found. Nothing wedges: the
// aborted turn still ends, so what the session owes still clears, and that
// ordering is the CLI's rather than Wake's. What was left was a lie - a client
// asking for status was told the agent was blocked on a request that was
// already dead, and rpc.SessionStatus.RequestIDs exist precisely so a client
// that reattached can answer that request, which here means writing an answer
// into the void.
//
// Asserted through a real socket and a real process because the frame arrives
// on the event stream and has to survive core's attribution, the daemon's
// fan-out, and the status report - none of which the unit test below reaches.
func TestAnInterruptedAskStopsBeingReportedAsOutstanding(t *testing.T) {
	fakeClaudeOnPath(t, "ask")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	if blocked := c.pollState(idAlpha, rpc.StateBlocked); soleAsk(t, blocked) != askRequestID {
		t.Fatalf("blocked session names request %q, want %q - there is nothing to withdraw otherwise", soleAsk(t, blocked), askRequestID)
	}

	c.send(rpc.Frame{Kind: rpc.FrameInterrupt, SessionID: idAlpha})

	// Retired on the withdrawal, and this is asserted inside the window the
	// withdrawal opens: the fake holds the aborted turn's result back until
	// the next line on stdin, and the daemon clears an outstanding ask on a
	// turn end as well - so without that gap this passes with nothing acting
	// on the withdrawal at all. See fakeWithdrawTheAsk.
	//
	// Both halves are asserted. A state that moved on while the id stayed
	// would leave every client that reads the id offering the same dead
	// prompt.
	settled := c.pollState(idAlpha, rpc.StateIdle)
	if got := soleAsk(t, settled); got != "" {
		t.Errorf("a session whose ask was withdrawn still names %q: a client that reattached would offer that prompt and its answer would go nowhere", got)
	}

	// And the turn still ends, so nothing wedged - the process took the next
	// message on the same session and answered it. That ordering is the CLI's
	// and TestAWithdrawalNamesAnEarlierAskAndATurnEndStillFollowsIt checks it
	// against the recorded bytes; this is the daemon end of the same fact.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "still there?"})
	c.awaitEvent(idAlpha, "interrupted")
}

// The withdrawal names one request, and only that one may be retired.
//
// The frame carries no session_id and need not name anything this agent is
// waiting on, so acting on it unconditionally is the failure above inverted:
// a prompt taken off the screen of an agent that is still blocked, with the
// operator given nothing to answer and the agent stopped dead until they do.
// The empty case is the sharper one - protocol.go decodes a withdrawal that
// names nothing rather than dropping it, so "" reaches here, and "" must not
// mean "all of them".
func TestAWithdrawalRetiresOnlyTheAskItNames(t *testing.T) {
	cases := []struct {
		name      string
		withdrawn string
		want      string
	}{
		{"the ask itself", askRequestID, ""},
		{"some other request", "req-2", askRequestID},
		{"a withdrawal naming nothing", "", askRequestID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newAgent(idAlpha, "sydney", "dev-5748", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
			a.observe(core.Event{Kind: core.KindPermissionRequest, RequestID: askRequestID})
			a.observe(core.Event{Kind: core.KindRequestWithdrawn, RequestID: tc.withdrawn})

			got := a.snapshot()
			if ask := soleAsk(t, got); ask != tc.want {
				t.Errorf("outstanding ask = %q, want %q", ask, tc.want)
			}
			wantState := rpc.StateIdle
			if tc.want != "" {
				wantState = rpc.StateBlocked
			}
			if got.State != wantState {
				t.Errorf("state = %q, want %q", got.State, wantState)
			}
		})
	}
}

// The report has to carry the two facts the room's sidebars draw and neither
// of which was on the wire: which workspace an agent is in, and which tool
// call it is currently inside.
func TestAStatusReportSaysWhereAnAgentIsAndWhatItIsDoing(t *testing.T) {
	a := newAgent("s1", "alex", "main", "/repo/api", "", nil, func() {})
	a.observe(core.Event{
		Kind: core.KindToolUse,
		Tool: &core.ToolCall{Name: "Edit", Display: "auth/token.go"},
	})

	st := a.snapshotFields()
	if st.Dir != "/repo/api" {
		t.Errorf("Dir = %q, want %q: the left sidebar groups by directory, because two repos on `main` are two workspaces", st.Dir, "/repo/api")
	}
	if st.Tool != "Edit" || st.ToolArg != "auth/token.go" {
		t.Errorf("Tool/ToolArg = %q/%q, want Edit/auth/token.go", st.Tool, st.ToolArg)
	}
}

// A tool result is the answer to the call above it, not a new activity.
// Blanking the row between the two makes the sidebar flicker at exactly the
// rate a busy agent works.
func TestAToolResultDoesNotEraseTheToolCallItAnswers(t *testing.T) {
	a := newAgent("s1", "alex", "main", "/repo/api", "", nil, func() {})
	a.observe(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Bash", Display: "go test ./..."}})
	a.observe(core.Event{Kind: core.KindToolResult, Text: "ok"})

	if st := a.snapshotFields(); st.Tool != "Bash" {
		t.Errorf("Tool = %q after its own result, want Bash: the sidebar shows what an agent is on, and a result is not a new activity", st.Tool)
	}
}

// The other half of the same rule: the turn ending is what does clear it. An
// idle agent still showing a tool call is a sidebar reporting work nobody is
// doing.
func TestATurnEndingClearsWhatAnAgentIsDoing(t *testing.T) {
	a := newAgent("s1", "alex", "main", "/repo/api", "", nil, func() {})
	a.observe(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Bash", Display: "go test ./..."}})
	a.observe(core.Event{Kind: core.KindTurnEnd})

	if st := a.snapshotFields(); st.Tool != "" {
		t.Errorf("Tool = %q after the turn ended, want empty: an idle agent is not still running a tool", st.Tool)
	}
}

// snapshotFields is snapshot() for an agent no test ever started.
//
// It **calls** snapshot rather than restating it, and that is the whole design
// of this helper. A helper that assembled its own rpc.SessionStatus from the
// same fields would be the shape docs/notes/decisions.md names first - a test
// restating a fact instead of deriving it - and deleting `Dir: a.dir` from
// snapshot would leave all three tests above green.
//
// All it supplies is the one thing snapshot needs and these tests have no use
// for: snapshot reads a.sess.Pgid(), which nil-dereferences on an agent
// nothing ever started. An unstarted core.Session answers it with no process
// behind it.
func (a *agent) snapshotFields() rpc.SessionStatus {
	if a.sess == nil {
		a.sess = core.NewSession(core.Config{SessionID: a.id})
	}
	return a.snapshot()
}

// Non-nil is not a crash. core's WaitDelay turns a clean exit 0 into an error
// whenever anything the agent spawned held stderr past the bound, so an
// ending has to be reported as an ending.
func TestAnEndingIsReportedAsAnEndingNotAsACrash(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})

	ended := c.pollState(idAlpha, rpc.StateEnded)
	if ended.Error != "" {
		t.Errorf("a session that exited cleanly reported %q", ended.Error)
	}
	for _, f := range c.seen {
		if f.Kind == rpc.FrameError && strings.Contains(strings.ToLower(f.Text), "crash") {
			t.Errorf("a clean ending was announced as a crash: %q", f.Text)
		}
	}
}

// The case the policy was built for and could not see: an agent that dies
// *after* its last turn.
//
// Nothing is owed, so the silence timer never fires. Nobody writes to it, so
// no failed write proves anything. core cannot help - its pump is parked in
// Scan, Err() is nil and Events() is open. So it reads idle forever, holding a
// live-cap slot, and at 15-30 sessions the fleet degrades one dead slot at a
// time while status says everything is fine.
//
// The daemon already holds both halves of the answer: the process group core
// recorded at spawn, and a way to ask the OS about it.
func TestAnAgentThatDiesAfterItsLastTurnIsNotReportedIdleForever(t *testing.T) {
	shortSilence(t, 200*time.Millisecond)
	// This one holds a wedged agent at the end, which stop cannot reach - so
	// its shutdown spends the whole grace. It used to compress that itself;
	// testQuitGrace now does it for every test, because two others needed the
	// same thing and did not ask.
	fakeClaudeOnPath(t, "hold")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	// Spoken by the grandchild once its parent has exited: the agent is gone
	// and the session is not.
	c.awaitEvent(idAlpha, "held")

	// Nothing is sent. That is the whole point - every other route to
	// noticing needs somebody to write to it.
	//
	// And noticing is no longer where it stops: the daemon reclaims a session
	// it has proved is gone (docs/notes/bugs.md BUG-17), so the slot goes back
	// without anybody asking. This is the path that carries the descriptive
	// reason, because the probe knows what it proved.
	ended := c.pollState(idAlpha, rpc.StateEnded)
	if !strings.Contains(ended.Error, "stopped reading stdout") {
		t.Errorf("the ending reports %q, want core's own account of the wedge it was reclaimed out of", ended.Error)
	}
}

// The other half, and the one that would make the probe worse than useless: a
// healthy quiet agent must not be declared dead just for being quiet.
//
// Asserted over the whole window rather than by asking once at the end. This
// test slept and then called status(), and a status reply carries no
// correlator - so it read whichever reply was at the front of the queue, which
// was one the daemon had pushed before the state changed. Mutated to declare
// every quiet agent gone, it passed. See watchStates.
func TestAQuietButLivingAgentIsNotReportedSilent(t *testing.T) {
	shortSilence(t, 150*time.Millisecond)
	fakeClaudeOnPath(t, "") // stays alive on stdin, says nothing more
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	// Several probe ticks go by with the agent quiet and alive.
	c.stayedIn(idAlpha, rpc.StateIdle, 6*silenceLimit,
		"the agent is alive and nothing is owed, and being quiet is not the fault")
}

// The window those two are watched over has to be evidence on its own.
//
// The test above and its broken-ps twin in inspect_unix_test.go both assert
// that a state does not move, and the daemon announces only movement - so
// before watchStates asked for status, the only thing that could put a frame
// in their window was an announcement left over from their own setup. Whether
// it landed inside the window or before it was decided by whether the agent's
// output beat the fan-out to the client, which is how CI failed both broken-ps
// subtests on 2026-08-10 while twelve other runs of the same commit passed.
// Both were green by luck, and a test that is green by luck is worth what the
// luck is worth: the mutation each exists to catch was never held at all on
// the runs where the window happened to be empty, because an empty window is
// only ever a Fatal about the harness.
//
// This makes that precondition deliberate rather than waiting for a loaded
// machine to produce it. The socket is drained until the daemon has been quiet
// for several liveness ticks, at which point its state is settled and recorded
// as announced (agent.changed) and it has nothing left to volunteer. Under a
// harness that only listens this fails every single run, with the message CI
// printed.
//
// The drain is a wall clock, and it is a floor rather than a ceiling: if load
// delays an announcement past it, that announcement lands in the window and
// this passes for the weaker reason the other two used to. It cannot fail for
// that reason, which is the only direction a guard is allowed to be wrong in.
func TestAWatchedWindowIsEvidenceWhenTheDaemonHasNothingToAnnounce(t *testing.T) {
	shortSilence(t, 150*time.Millisecond)
	fakeClaudeOnPath(t, "") // stays alive on stdin, says nothing more
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	quiet := 4 * livenessInterval()
drained:
	for {
		select {
		case f, open := <-c.frames:
			if !open {
				t.Fatalf("the daemon hung up while it was settling\nsaw: %s", c.transcript())
			}
			c.seen = append(c.seen, f)
		case err := <-c.errs:
			t.Fatalf("read while the daemon was settling: %v\nsaw: %s", err, c.transcript())
		case <-time.After(quiet):
			break drained
		}
	}

	c.stayedIn(idAlpha, rpc.StateIdle, 6*silenceLimit,
		"the agent is alive and quiet and the daemon has nothing left to announce about it")
}

// docs/notes/bugs.md BUG-17. The daemon proves the process is gone and then
// keeps everything the session was holding: five goroutines, two descriptors, a
// zombie, and one of the thirty live-cap slots - because liveCount switches on
// state and silent is neither parked nor ended.
//
// The detection was built, measured and commented at length; only the
// reclamation was never wired. The test below the original one has always sent
// rpc.FrameKill by hand to finish the job, and grepping the tree for a producer
// of that frame finds none: no key, no slash command, no CLI verb, no MCP tool.
// So the operator had no way to do by hand what this now does unasked.
func TestAFailedWriteReportsAnAgentWithoutKillingIt(t *testing.T) {
	fakeClaudeOnPath(t, "hold")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.awaitEvent(idAlpha, "held")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "still with us?"})

	silent := c.pollState(idAlpha, rpc.StateSilent)
	if silent.Error == "" {
		t.Fatal("a session reported silent said nothing about why")
	}
	// And it stays there. The first version of BUG-17's fix reclaimed on this
	// path too, and an adversarial review found the hole: EPIPE proves stdin
	// has no reader, not that the process is gone, so a child that closed stdin
	// while still finishing its output would have had its group killed under it.
	// The watchdog asks the OS; this does not, so this only reports.
	//
	// The window is under core's waitDelay, and that bound is new. This fake is
	// a genuine wedge - the leader exited and a grandchild holds its stdout - so
	// core now self-detects it and ends the session cleanly one waitDelay after
	// the exit. That ending is correct and is a different mechanism from the
	// failed write; what this pins is that the failed write does not reclaim
	// *before* then, since the reclaim bug fired in about 40ms. No shortSilence,
	// so the five-minute watchdog stays out of the measurement too.
	c.stayedIn(idAlpha, rpc.StateSilent, 1*time.Second,
		"a failed write reclaimed a session the OS was never asked about: EPIPE says stdin has no reader, not that the process is gone")
}

// ⌃C on an agent that is already silently wedged, which is the first thing an
// operator does to one - and it used to hang forever.
//
// quietAndDue does not exclude a stopped session, so the probe still reaches a
// parked one and the OS still says its process is gone. What refused was the
// mark: noteUnreachable excluded anything stopped, so lostProcess never killed,
// the session never ended, completePark never ran, and the row sat at idle
// holding its live-cap slot. Before BUG-16 that lasted until explicit shutdown;
// after BUG-16 the row itself keeps the live count nonzero and prevents empty
// exit. Park works by ending the process, so a parked session the OS reports
// gone is precisely the case park cannot finish on its own.
func TestParkingASilentlyWedgedAgentStillCompletes(t *testing.T) {
	fakeClaudeOnPath(t, "hold")
	shortSilence(t, 200*time.Millisecond)
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	// The grandchild speaks once its parent is gone: the agent is wedged.
	c.awaitEvent(idAlpha, "held")

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})

	parked := c.pollState(idAlpha, rpc.StateParked)
	if parked.State != rpc.StateParked {
		t.Fatalf("state = %q after parking a wedged agent, want parked: the park never completed", parked.State)
	}
}
