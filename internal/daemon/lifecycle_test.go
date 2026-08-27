// The four verbs, driven end to end: a real daemon, a real client, a real
// socket, real processes. Spec §5 says these are four different intents and
// that conflating any two is how a fleet becomes unstoppable or a machine
// fills with orphans, so each one is tested for what it does *and* for what
// it must not do.

package daemon

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The whole reason the daemon exists: a client can leave and come back, and
// the session it left is still there and still talking.
func TestASessionOutlivesTheClientThatStartedIt(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)

	first := attach(t, d.socket)
	first.spawn(idAlpha, "sydney")
	first.awaitEvent(idAlpha, "ready")
	first.close() // detach: the TUI exits and nothing else should

	second := attach(t, d.socket)
	st := second.status()
	if got := live(st); len(got) != 1 || got[0].ID != idAlpha {
		t.Fatalf("after reattaching, status = %+v, want the session still running", st.Sessions)
	}

	// Still talking, not merely still listed.
	second.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "still there?"})
	second.awaitEvent(idAlpha, "still there?")
}

// Two clients on one daemon see the same stream, which is what makes
// reattaching from a second window work at all.
func TestEventsFanOutToEveryAttachedClient(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)

	a := attach(t, d.socket)
	b := attach(t, d.socket)
	a.spawn(idAlpha, "sydney")

	a.awaitEvent(idAlpha, "ready")
	b.awaitEvent(idAlpha, "ready")

	a.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hello"})
	// Sent by a, seen by b: the daemon fans out by session, not by whoever
	// happened to ask.
	b.awaitEvent(idAlpha, "hello")
}

// The same rule for the roster that the test above states for the stream: a
// room open in one terminal has to learn about an agent spawned from another.
//
// The spawn's own confirmation is a *reply* and reaches only the client that
// asked. Before this, the only unsolicited account of a new session was
// watchLiveness, whose tick lands on the 30-second clamp - so a group chat
// would show an empty roster for up to half a minute after somebody joined.
//
// Asserted on what the second client received rather than on what the daemon
// emitted: a push nobody is sent is not an announcement. The wait matches any
// push and the assertion reads its contents, so a daemon that announced the
// wrong roster fails here rather than satisfying the wait with it.
func TestASpawnIsAnnouncedToEveryClientAndNotOnlyToTheOneThatAskedForIt(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)

	// Attached before the spawn and it asks nothing at all: every helper that
	// sends a frame would make the answer a reply, which is the thing this
	// test must not accept.
	watcher := attach(t, d.socket)
	spawner := attach(t, d.socket)

	spawner.spawn(idAlpha, "sydney")

	push := watcher.await("an unasked-for status push", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameStatusPush && f.Status != nil
	})
	found := false
	for _, s := range push.Status.Sessions {
		if s.ID == idAlpha && s.State != rpc.StateEnded {
			found = true
		}
	}
	if !found {
		t.Fatalf("a client watching the fleet was pushed %+v when an agent started, which does not name %s: the room would show an empty roster until the liveness tick, up to 30s later", push.Status.Sessions, idAlpha)
	}
}

// Two sessions, one socket. A daemon that got this wrong would be unusable at
// the scale the product is for, and the failure is silent: the wrong agent
// answers.
func TestMessagesReachTheSessionTheyAreAddressedTo(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.spawn(idBeta, "alex")
	c.awaitEvent(idAlpha, "ready")
	c.awaitEvent(idBeta, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idBeta, Text: "for alex only"})
	got := c.awaitEvent(idBeta, "for alex only")

	// The event's own session id comes off the agent's frames, which is how
	// a client can tell a re-keyed session from a misrouted one.
	if got.Event.SessionID != idBeta {
		t.Errorf("event carried session %q, want %q - the agent stamped somebody else's id", got.Event.SessionID, idBeta)
	}
	for _, f := range c.seen {
		if f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && strings.Contains(f.Event.Text, "for alex only") {
			t.Fatal("the message reached the wrong agent as well")
		}
	}
}

// Stop is not kill, and this is the difference in one test: an agent that is
// mid-turn when the stop arrives finishes the turn. Stop closes stdin; the
// process exits on the EOF *after* the work it was doing.
//
// The delay is the whole discrimination. Against a kill, or against any stop
// that signalled, the reply never arrives.
func TestStopLetsTheInFlightTurnFinish(t *testing.T) {
	fakeClaudeOnPath(t, "slow")
	t.Setenv(fakeDelayEnv, "300ms")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "one last thing"})

	// Stopped while the turn is still running.
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})

	c.awaitEvent(idAlpha, "one last thing")
	c.pollState(idAlpha, rpc.StateEnded)

	if st := c.status(); len(live(st)) != 0 {
		t.Errorf("status = %+v, want the stopped session gone from the roster", st.Sessions)
	}
}

// The other half: a hard kill is available and it is a different verb.
// Whether an agent is killed mid-Edit must be a decision somebody made.
func TestKillEndsTheTurnStopWouldHaveWaitedFor(t *testing.T) {
	fakeClaudeOnPath(t, "slow")
	t.Setenv(fakeDelayEnv, "10s")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "a long one"})
	c.pollState(idAlpha, rpc.StateWorking)

	c.send(rpc.Frame{Kind: rpc.FrameKill, SessionID: idAlpha})

	// Ends long before the turn would have: a stop here would have waited
	// ten seconds, which is longer than this test's patience by design.
	ended := c.pollState(idAlpha, rpc.StateEnded)
	for _, f := range c.seen {
		if f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && strings.Contains(f.Event.Text, "a long one") {
			t.Fatal("the killed turn still completed, so this test did not kill anything")
		}
	}
	// Killing is not a clean ending and must not be reported as one - but it
	// is also not a crash, and nothing here calls it one.
	if ended.Error == "" {
		t.Error("a killed session ended with no account of why")
	}
}

// The agent this exists for: one whose process is already gone but whose
// stdout a grandchild still holds. core's pump is parked in Scan, Err() is
// nil and Events() is open, so nothing below the daemon can tell it from an
// agent thinking hard - and stop cannot end it, because there is no process
// left to read the EOF.
func TestKillEndsAWedgedAgentThatStopCannotReach(t *testing.T) {
	fakeClaudeOnPath(t, "hold")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	// Spoken by the grandchild after its parent exited, so the wedge is
	// established rather than assumed.
	c.awaitEvent(idAlpha, "held")

	// Stop first, and it changes nothing: the process that would have read
	// the EOF has already exited.
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	time.Sleep(300 * time.Millisecond)
	if st := c.status(); len(live(st)) != 1 {
		t.Fatalf("status = %+v, want the wedged session still held - if stop ended it, this test is not about a wedged agent", st.Sessions)
	}

	c.send(rpc.Frame{Kind: rpc.FrameKill, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if st := c.status(); len(live(st)) != 0 {
		t.Errorf("status = %+v, want the killed session gone", st.Sessions)
	}
}

// Quit is the fourth verb: every session stops, then the daemon exits. A quit
// that left agents running would be the orphan factory the reaper exists to
// clean up after.
func TestQuitStopsEverySessionAndEndsTheDaemon(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.spawn(idBeta, "alex")
	c.awaitEvent(idAlpha, "ready")
	c.awaitEvent(idBeta, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)

	// Nothing is listening any more, and the socket file is gone with it.
	if conn, err := Dial(d.socket); err == nil {
		_ = conn.Close()
		t.Fatal("the daemon is still listening after quit")
	}
	// And it left nothing on disk for a later daemon to hunt.
	if recs := loadRoster(rosterPath(d.socket)); len(recs) != 0 {
		t.Errorf("roster after quit = %+v, want empty", recs)
	}
}

// A permission ask blocks the agent until it is answered on stdin. Without a
// route back, Wake surfaces a blocked agent and cannot unblock it - which is
// the state Phase 1 must not ship.
func TestAPermissionAskRoutesAndTheAnswerUnblocksTheAgent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer rpc.Frame
		expect string
	}{
		{
			name:   "allow",
			answer: rpc.Frame{Kind: rpc.FrameAllow, SessionID: idAlpha, RequestID: askRequestID},
			expect: "permission allow",
		},
		{
			name:   "deny",
			answer: rpc.Frame{Kind: rpc.FrameDeny, SessionID: idAlpha, RequestID: askRequestID, Reason: "not that file"},
			expect: "permission deny",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeClaudeOnPath(t, "ask")
			d := startDaemon(t)
			c := attach(t, d.socket)

			c.spawn(idAlpha, "sydney")
			ask := c.await("the permission request", func(f rpc.Frame) bool {
				return f.Kind == rpc.FrameEvent && f.Event != nil &&
					f.Event.Kind == core.KindPermissionRequest
			})
			// A can_use_tool request carries no session_id on Claude's
			// wire; core stamps it from the pipe it arrived on, and that
			// stamp is the only thing that makes it routable at 30 agents.
			if ask.SessionID != idAlpha || ask.Event.SessionID != idAlpha {
				t.Fatalf("ask arrived as %+v, want it attributed to %s", ask, idAlpha)
			}
			if ask.Event.RequestID != askRequestID {
				t.Fatalf("ask RequestID = %q, want %q - the only correlator an answer has", ask.Event.RequestID, askRequestID)
			}
			// Blocked, not idle and not working: a human has to act.
			c.pollState(idAlpha, rpc.StateBlocked)

			c.send(tc.answer)
			c.awaitEvent(idAlpha, tc.expect)
		})
	}
}

// Wake originates identity, and a session with no id it assigned cannot be
// resumed or reaped - the two things the id exists for. Spawning one anyway
// would be a process nobody can find.
func TestSpawnWithoutASessionIDIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, Text: "nameless"})
	f := c.await("an error", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "id") {
		t.Errorf("error = %q, want it to say the session needs an id", f.Text)
	}
	if st := c.status(); len(live(st)) != 0 {
		t.Errorf("status = %+v, want nothing spawned", st.Sessions)
	}
}

// Two processes sharing one --session-id would write one transcript twice.
func TestSpawningTheSameSessionTwiceIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "sydney2"})

	f := c.await("an error", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "already exists") {
		t.Errorf("error = %q, want it to say the session already exists", f.Text)
	}
	if st := c.status(); len(live(st)) != 1 {
		t.Errorf("status = %+v, want exactly one session", st.Sessions)
	}
}

// Interrupt is the verb in this file that must NOT end anything.
//
// Two things have to be true at once and they pull in opposite directions: the
// frame has to reach the agent's stdin, which is the same path stop takes, and
// the session has to still be there afterwards, which is the opposite of what
// stop does. A daemon that routed interrupt to the ending code would pass the
// first half and fail the second silently - the operator presses the key to
// stop a turn and loses the agent.
func TestInterruptStopsTheTurnAndLeavesTheSessionRunning(t *testing.T) {
	fakeClaudeOnPath(t, "interruptible")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameInterrupt, SessionID: idAlpha})

	// The receipt, and it must carry the request_id core minted: the frame
	// names no session and no subtype of its own, so an id-less receipt is one
	// nothing across a fleet can attribute.
	receipt := c.await("the interrupt receipt", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindControlReceipt
	})
	if receipt.Event.RequestID == "" {
		t.Error("the receipt carries no request id: the client sent none and core minted none")
	}
	if receipt.SessionID != idAlpha {
		t.Errorf("receipt arrived as %+v, want it attributed to %s", receipt, idAlpha)
	}
	c.awaitEvent(idAlpha, "interrupted")

	// The half a routing mistake would take: the agent is still there, and
	// still takes work.
	if st := c.status(); len(live(st)) != 1 {
		t.Fatalf("status = %+v, want the session still running - an interrupt ends a turn, not a session", st.Sessions)
	}
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "still there?"})
	c.awaitEvent(idAlpha, "echo:")
}

// An unrecognized kind does nothing at all, which is the only safe end of
// that failure: every default here destroys something.
func TestAnUnknownFrameKindIsRefusedRatherThanGuessedAt(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	c.send(rpc.Frame{Kind: "terminate", SessionID: idAlpha})
	f := c.await("an error", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "unknown frame kind") {
		t.Errorf("error = %q, want it to name the unknown kind", f.Text)
	}
	if st := c.status(); len(live(st)) != 1 {
		t.Fatalf("status = %+v, want the session untouched by a verb nobody defined", st.Sessions)
	}
}

// Ending a session nobody has is an answer, not silence: a client that
// reattached after a session died would otherwise wait forever for something
// to happen.
func TestEndingAnUnknownSessionIsReported(t *testing.T) {
	d := startDaemon(t)
	c := attach(t, d.socket)

	for _, kind := range []string{rpc.FrameStop, rpc.FrameKill, rpc.FrameSend, rpc.FrameInterrupt} {
		c.send(rpc.Frame{Kind: kind, SessionID: "ghost", Text: "hello"})
		f := c.await("an error for "+kind, func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
		if !strings.Contains(f.Text, "unknown session") {
			t.Errorf("%s of an unknown session = %q, want it named", kind, f.Text)
		}
	}
}

// An ending has to survive the client missing the frame that announced it.
//
// The announcement goes through the same fan-out as everything else and is
// dropped for a lagging client by design, and no later question could recover
// it: the row simply vanished, with no reason, permanently. So a status report
// carries the recent endings too.
func TestAnEndingCanStillBeLearnedAfterTheAnnouncementIsMissed(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	// A client that was not even connected when it ended - the strongest
	// version of having missed the announcement.
	later := attach(t, d.socket)
	var found bool
	for _, s := range later.status().Sessions {
		if s.ID == idAlpha {
			found = true
			if s.State != rpc.StateEnded {
				t.Errorf("session %s reported %q, want %q", s.ID, s.State, rpc.StateEnded)
			}
		}
	}
	if !found {
		t.Fatal("a session that ended is absent from the next status: a client that missed one frame can never learn why a row disappeared")
	}
	if len(live(later.status())) != 0 {
		t.Error("the ended session is still counted among the running ones")
	}
}

// A respawned id must not be reported alive and dead at once.
func TestASessionThatComesBackReplacesItsOwnEnding(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	c.spawn(idAlpha, "sydney2")
	c.awaitEvent(idAlpha, "ready")

	var rows int
	for _, s := range c.status().Sessions {
		if s.ID == idAlpha {
			rows++
			if s.State == rpc.StateEnded {
				t.Errorf("session %s is still reported ended after being restarted", s.ID)
			}
		}
	}
	if rows != 1 {
		t.Errorf("session %s appears %d times in one status", idAlpha, rows)
	}
}

// The same forbidden state, reached through the other door.
//
// register takes an id out of the recent endings and into the live map in one
// locked step. retire has to do the reverse in one step too, and it did not: it
// deleted from s.agents under one lock and recorded the ending under a later
// one, with a roster *file write* in between. In that gap the id is in neither
// place, and a respawn landing there finds nothing to reconcile - register
// succeeds, forgetLocked clears nothing because the ending is not recorded yet,
// and then remember puts it back. fleet() reads both halves under one lock, so
// the result is atomically observable: one session reported alive and ended in
// the same reply, which is what the test above forbids.
//
// The barrier is the roster's own mutex rather than timing, and that is what
// makes this fail on demand instead of once in n runs. Holding it parks retire
// at precisely the point the gap used to open; whatever state it is holding
// when it parks is the entire question, and it cannot move on until this test
// says so.
func TestRetireLeavesTheMapAndEntersTheEndingsInOneStep(t *testing.T) {
	s := newServer(tempSocket(t))
	a := newAgent(idAlpha, "sydney", "dev-5748", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	if !s.register(a) {
		t.Fatal("register refused an id nothing else holds")
	}

	// Blocks the roster write retire performs, which is what used to sit
	// between the two halves.
	s.roster.mu.Lock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.retire(a)
	}()

	// Not a sleep. retire cannot reach the roster without having finished with
	// s.agents, so the id leaving the live map is exactly the moment its
	// bookkeeping is over - and the lock above then holds it there.
	waitUntilRetired(t, s, idAlpha)
	rows := statusRows(s.statusReply(), idAlpha)

	s.roster.mu.Unlock()
	<-done

	if rows != 1 {
		t.Fatalf("a session that had left the live map appears %d times in a status reply, want exactly 1: it is in neither place, so a respawn landing here is reported alive and ended at once", rows)
	}
}

// waitUntilRetired waits for an id to leave the live map.
func waitUntilRetired(t *testing.T, s *server, id string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if _, held := s.agent(id); !held {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("session %s never left the live map, so this test never reached the state it is about", id)
}

// statusRows is how many times an id appears in one report. Anything but one,
// for a session this daemon has started, is a bookkeeping bug.
func statusRows(f rpc.Frame, id string) int {
	if f.Status == nil {
		return 0
	}
	var rows int
	for _, s := range f.Status.Sessions {
		if s.ID == id {
			rows++
		}
	}
	return rows
}

// One daemon, a user whose work is spread across repositories: an agent asked
// for from one repo must not run in another. It decides which tree gets
// edited, and where claude persists the transcript, so a later --resume
// inherits whatever this gets wrong.
func TestASessionRunsInTheDirectoryTheClientAsksFor(t *testing.T) {
	fakeClaudeOnPath(t, "cwd")
	dir := t.TempDir()
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "sydney", Dir: dir})
	got := c.awaitEvent(idAlpha, "cwd:")

	// Resolved, because a temp directory on darwin is reached through a
	// symlink and the agent reports where it actually is.
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if !strings.Contains(got.Event.Text, want) {
		t.Errorf("the agent is running in %q, want %q - it is editing the wrong tree", got.Event.Text, want)
	}

	// And the report names the same directory the process got. Asserted here
	// rather than in a test of its own because it is one fact: the workspace
	// the room's left sidebar groups this agent under is the tree the agent is
	// editing, and splitting the two apart is how they would drift. What the
	// client asked for, unresolved - the sidebar groups on the string somebody
	// typed, and two clients naming a directory two ways is a Phase 3 problem
	// with a real answer rather than an EvalSymlinks in a daemon.
	if st := session(t, c.status(), idAlpha); st.Dir != dir {
		t.Errorf("the report says the session is in %q, want %q - the agent is in the right tree and the room would file it under the wrong workspace", st.Dir, dir)
	}
}

// session finds one row in a status report and fails the test if it is absent,
// so a caller reads a field off a row that is provably there rather than off a
// zero value that agrees with anything.
func session(t *testing.T, st rpc.Status, id string) rpc.SessionStatus {
	t.Helper()
	for _, s := range st.Sessions {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("session %s is in no status report: %+v", id, st.Sessions)
	return rpc.SessionStatus{}
}

// A relative directory would resolve against the daemon's own working
// directory, which is the confusion the field exists to end.
func TestARelativeSpawnDirectoryIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "sydney", Dir: "../elsewhere"})
	f := c.await("an error", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "absolute") {
		t.Errorf("error = %q, want it to say the directory must be absolute", f.Text)
	}
	if len(live(c.status())) != 0 {
		t.Error("a session was spawned with a relative directory")
	}
}

// The reaper SIGKILLs a process group when a live process's command line
// contains the session id. That is only safe if the id is a UUID, and this is
// the only place that can make it true.
func TestASessionIDThatIsNotAUUIDIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	for _, id := range []string{"s1", "build", "sydney", "0000", "not-a-uuid"} {
		c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id, Text: "sydney"})
		f := c.await("an error for "+id, func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
		if !strings.Contains(f.Text, "UUID") {
			t.Errorf("spawning %q = %q, want it refused for not being a UUID - the reaper matches this against live command lines", id, f.Text)
		}
	}
	if len(live(c.status())) != 0 {
		t.Error("a session was spawned with an id the reaper cannot safely match")
	}
}
