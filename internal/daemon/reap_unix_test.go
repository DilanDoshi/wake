//go:build unix

// The reaper, and `wake status` for a fleet whose daemon is gone.
//
// These tests are Unix-only because the observation is: they ask the OS
// whether a specific process is still alive after its daemon was SIGKILLed,
// which is what "did anything actually get reaped" means and the only thing
// that would catch trees accumulating.
//
// The daemon under test here is a *separate process* - the same fork
// EnsureRunning performs - because the scenario is a daemon that dies without
// running any cleanup, and a Serve running inside the test binary cannot be
// SIGKILLed without taking the test with it.

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The whole cost of Setpgid, and the whole reason this file exists. The agent
// survives its daemon - that is what makes detach mean anything - and nothing
// is left holding a handle to it.
func TestANewDaemonReapsTheFleetACrashedOneLeftRunning(t *testing.T) {
	fakeClaudeOnPath(t, "deaf")
	sock := tempSocket(t)
	t.Setenv(SocketEnv, sock)
	t.Setenv(fakeDaemonEnv, "1")

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, err := EnsureRunning(ctx, sock)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	c := attachConn(t, conn)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	st := c.status()
	if st.PID == 0 || st.PID == os.Getpid() {
		t.Fatalf("status reports pid %d, want a forked daemon's - this test is not driving a separate process", st.PID)
	}
	sessions := live(st)
	if len(sessions) != 1 || sessions[0].PID <= 0 {
		t.Fatalf("status = %+v, want one session with a process group recorded", st.Sessions)
	}
	agent := sessions[0].PID

	// No warning, no cleanup, no chance to stop anything: exactly what a
	// SIGKILL or a panic leaves behind.
	if err := syscall.Kill(st.PID, syscall.SIGKILL); err != nil {
		t.Fatalf("kill the daemon: %v", err)
	}
	waitForDaemonGone(t, sock)

	if !processAlive(agent) {
		t.Fatal("the agent died with its daemon, so there is nothing here to reap and this test proves nothing")
	}

	// `wake status` has to find it. A fleet nobody is holding is not
	// "nothing is running", and the session ids are on disk precisely so
	// this is answerable.
	orphaned, err := Status(sock)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if orphaned.Running {
		t.Error("Status says a daemon is running after it was killed")
	}
	if len(orphaned.Sessions) != 1 || orphaned.Sessions[0].State != rpc.StateOrphaned {
		t.Fatalf("Status = %+v, want the abandoned session reported as orphaned", orphaned.Sessions)
	}

	// Starting a daemon on the same socket is what cleans up.
	d := startDaemonOn(t, sock)
	waitForProcessGone(t, agent)
	if recs := loadRoster(rosterPath(d.socket)); len(recs) != 0 {
		t.Errorf("roster after reaping = %+v, want empty", recs)
	}
}

// The reaper reads process group ids off a file written by a process that has
// since died, and pids are recycled. Signalling a *group* on a stale id could
// take down a terminal job, a build, or a shell - so an entry is killed only
// if the live process still carries the session id Wake minted, which exists
// nowhere else on the machine.
//
// The bystander here is a real process with a real pid in the roster, and the
// only thing wrong with the entry is that the pid is somebody else's now.
func TestTheReaperWillNotSignalAPidThatIsNoLongerTheAgent(t *testing.T) {
	bystander := startLingererInItsOwnGroup(t)
	sock := tempSocket(t)

	// A roster the way a crashed daemon would have left it, except that the
	// pid has since been recycled into the process above.
	writeRoster(t, rosterPath(sock), []record{{
		ID:      "6a37f3a2-0000-4000-8000-000000000001",
		Name:    "sydney",
		PID:     bystander.Process.Pid,
		Started: time.Now().Add(-time.Hour),
	}})

	startDaemonOn(t, sock)

	// Give the reaper every chance to have got it wrong.
	select {
	case <-bystander.died:
		t.Fatalf("the reaper SIGKILLed a process group on a stale pid (%v): at 15-30 recorded sessions that is somebody's shell", bystander.why)
	case <-time.After(500 * time.Millisecond):
	}
}

// The reaper's ids do not arrive through maySpawn. They come off a file that
// outlives the process, the build that wrote it, and any later build that
// relaxes the rule - so the invariant maySpawn establishes has to hold where
// the SIGKILL is, not merely where it was established.
//
// The roster here is one a pre-UUID daemon left behind: a session called "s1".
// The process it names is not that session and never was - it is an ordinary
// build job whose command line happens to contain the substring, which is what
// a two-character id does on a busy machine. The match is `strings.Contains`,
// and that is only sound for a UUID.
func TestTheReaperRefusesARosterEntryWhoseIDWakeCouldNotHaveMinted(t *testing.T) {
	bystander := startLingerer(t, "--log", "/tmp/build-logs1.txt")
	sock := tempSocket(t)

	writeRoster(t, rosterPath(sock), []record{{
		ID:      "s1",
		Name:    "sydney",
		PID:     bystander.Process.Pid,
		Started: time.Now().Add(-time.Hour),
	}})

	// The decision itself, asked directly so the answer does not depend on
	// timing: the next thing a true here does is SIGKILL that group. One probe
	// ctx serves both this and goneNow below, the way verifyAgent takes its
	// deadline from the caller now.
	pctx, pcancel := context.WithTimeout(context.Background(), probeTimeout)
	defer pcancel()
	if verifyAgent(pctx, bystander.Process.Pid, "s1") {
		t.Fatal(`verifyAgent said yes about a build job because its command line contains "s1": an id Wake could not have minted must never be matched against a live process`)
	}
	// The other half of the same match. goneNow's callers all come through
	// maySpawn today, so this cannot fire from inside the daemon - but it runs
	// the same strings.Contains against the same live argv, and an answer of
	// "gone" there is what marks an agent unreachable.
	//
	// The failing direction is the one worth naming: "s1" is not in that
	// bystander's argv either, and goneNow reads a missing id as a **recycled
	// pid** - so without the minted-by-Wake guard this would answer gone rather
	// than merely matching the wrong process.
	if got, err := goneNow(pctx, []watched{{id: "s1", pid: bystander.Process.Pid}}); err != nil || got["s1"] {
		t.Errorf(`goneNow = %v, %v for id "s1": an id Wake could not have minted must not be matched against a live `+
			`process at all, and the answer here is what marks an agent unreachable`, got, err)
	}

	// And the whole motion, because the guard is only worth having where the
	// reaper actually reads the roster.
	startDaemonOn(t, sock)
	select {
	case <-bystander.died:
		t.Fatalf("the reaper SIGKILLed a process group named by an id Wake could not have minted (%v)", bystander.why)
	case <-time.After(500 * time.Millisecond):
	}
}

// A roster entry with no process group is unreapable and must not become a
// signal to group 0, which means "the caller's own group" to kill(2) - the
// daemon, its terminal job and every session it had just started.
func TestTheReaperRefusesAnEntryWithNoProcessGroup(t *testing.T) {
	sock := tempSocket(t)
	writeRoster(t, rosterPath(sock), []record{
		{ID: "no-group", PID: 0, Started: time.Now()},
		{ID: "negative", PID: -1, Started: time.Now()},
	})

	// If either entry were signalled, this daemon would not survive its own
	// startup - it is in the group that would be hit.
	d := startDaemonOn(t, sock)
	c := attachConn(t, mustDial(t, d.socket))
	if st := c.status(); !st.Running {
		t.Fatal("the daemon did not survive reaping a roster it should have refused")
	}
}

// docs/notes/bugs.md BUG-28. A daemon that could not finish reaping the fleet
// its predecessor left running used to clear the whole roster regardless - so a
// transient ps failure left the orphan alive and its record gone, and no later
// daemon could ever retry. Up to 30 process trees permanently ownerless in one
// failed recovery.
//
// Here ps cannot answer and the orphan is real: a live group leader carrying
// the session id, which a working ps would verify and reap. The reaper cannot
// verify it, so it may not signal it - and it must not forget it either.
//
// RED on the unconditional clear, GREEN once a could-not-ask record is retained.
func TestAReapThatCouldNotCheckAnOrphanKeepsItToRetry(t *testing.T) {
	id := testSessionID("bbbb")
	orphan := startLingererCarrying(t, id)

	shortProbeTimeout(t, 200*time.Millisecond)
	brokenPsOnPath(t, psRefuses)

	sock := tempSocket(t)
	writeRoster(t, rosterPath(sock), []record{{
		ID:      id,
		Name:    "sydney",
		PID:     orphan.Process.Pid,
		Started: time.Now().Add(-time.Hour),
	}})

	newServer(sock).reapOrphans()

	// "I could not check" is not "it is gone": the record has to survive so a
	// later daemon whose ps works can finish the reap this one could not.
	if !rosterNames(sock, id) {
		t.Fatalf("reapOrphans cleared session %s though ps could not verify it: the process is still alive with no daemon holding it, and nothing on disk names it for a later daemon to reap", id)
	}
	// And it was never signalled - an unverifiable record must not be.
	if !processAlive(orphan.Process.Pid) {
		t.Fatal("reapOrphans SIGKILLed a group it could not verify: at 15-30 records that is somebody's shell")
	}
}

// The other direction, which the fix must not lose: a record the reaper proves
// is no longer its agent still clears. Retaining on anything unproven would make
// the roster grow without bound, re-probing and re-signalling a gone fleet on
// every restart - the failure mode opposite to the bug above.
//
// The pid here leads its own group but its command line carries no session id:
// the shape of a recycled pid, which a working ps verifies as "not our agent".
func TestAReapClearsAnOrphanItProvesIsNoLongerTheAgent(t *testing.T) {
	stranger := startLingererInItsOwnGroup(t)

	sock := tempSocket(t)
	id := testSessionID("cccc")
	writeRoster(t, rosterPath(sock), []record{{
		ID:      id,
		Name:    "sydney",
		PID:     stranger.Process.Pid,
		Started: time.Now().Add(-time.Hour),
	}})

	newServer(sock).reapOrphans()

	if rosterNames(sock, id) {
		t.Fatalf("reapOrphans kept session %s after proving its pid is something else now: a record that never clears grows the roster and re-signals a gone fleet every restart", id)
	}
	// The recycled pid is not ours and must be untouched.
	select {
	case <-stranger.died:
		t.Fatalf("reapOrphans SIGKILLed a recycled pid's group: %v", stranger.why)
	case <-time.After(200 * time.Millisecond):
	}
}

// FleetOnDisk is bare `wake`'s front door when the daemon is gone, and it walks
// the roster running one ps per record. A ps that hangs - a loaded machine, an
// implementation that wedges on the query - costs probeTimeout per record, so N
// records with no deadline over the loop is N×probeTimeout serial. One deadline
// over the whole sweep caps the total at a single probe's budget. This pins that
// bound, not a latency value: RED today at ~N×probeTimeout, GREEN once the loop
// shares one ctx.
func TestFleetOnDiskIsBoundedAcrossTheWholeLoop(t *testing.T) {
	brokenPsOnPath(t, psHangs)
	shortProbeTimeout(t, 250*time.Millisecond)
	shortFleetProbeBudget(t, 500*time.Millisecond)

	// The records must name a real group leader: verifyAgent checks that before
	// it ever reaches ps, so a pid leading no group is refused with no probe and
	// nothing hangs. One lingerer is enough - every record names its pid, and the
	// hanging ps behind it is what the deadline has to survive.
	agent := startLingererInItsOwnGroup(t)
	sock := tempSocket(t)

	const n = 20
	recs := make([]record, n)
	for i := range recs {
		recs[i] = record{
			ID:      testSessionID(fmt.Sprintf("%04x", i)),
			Name:    fmt.Sprintf("agent-%02d", i),
			PID:     agent.Process.Pid,
			Started: time.Now().Add(-time.Hour),
		}
	}
	writeRoster(t, rosterPath(sock), recs)

	done := make(chan struct{})
	go func() {
		FleetOnDisk(sock)
		close(done)
	}()

	// Generous slack over the budget: make test runs under -race, which slows
	// process exits, and the fix bounds the total to about one budget. The
	// unbounded shape is n×probeTimeout, well past this.
	bound := fleetProbeBudget + 2*time.Second
	select {
	case <-done:
	case <-time.After(bound):
		t.Fatalf("FleetOnDisk did not return within %v: %d records each hanging for probeTimeout with no deadline over the loop is the ~%v serial cost this bound exists to cap",
			bound, n, time.Duration(n)*probeTimeout)
	}
}

// The bound above drops records to stay quick, and a drop is an under-count - so
// the sweep has to SAY it was cut short. `wake stop` reads FleetOnDisk when its
// dial fails and, via runningCount==0, would otherwise report "the fleet is
// down" and exit zero over a wedged ps that dropped every live record: the
// false-down `wake stop && rm -rf` is written to survive. So a probe the
// deadline interrupts sets ProbeIncomplete, and the dropped records leave
// Sessions empty - the flag is the only thing that tells the empty list from an
// answer. Same wedged-ps harness as the bound; RED until FleetOnDisk sets the
// flag on a genuine deadline-break.
func TestFleetOnDiskFlagsASweepTheBudgetCutShort(t *testing.T) {
	brokenPsOnPath(t, psHangs)
	shortProbeTimeout(t, 250*time.Millisecond)
	shortFleetProbeBudget(t, 500*time.Millisecond)

	agent := startLingererInItsOwnGroup(t)
	sock := tempSocket(t)

	const n = 20
	recs := make([]record, n)
	for i := range recs {
		recs[i] = record{
			ID:      testSessionID(fmt.Sprintf("%04x", i)),
			Name:    fmt.Sprintf("agent-%02d", i),
			PID:     agent.Process.Pid,
			Started: time.Now().Add(-time.Hour),
		}
	}
	writeRoster(t, rosterPath(sock), recs)

	st := FleetOnDisk(sock)

	if !st.ProbeIncomplete {
		t.Errorf("FleetOnDisk did not set ProbeIncomplete after a wedged ps cut its sweep short: `wake stop` would read the empty roster as \"the fleet is down\" and exit zero over live agents")
	}
	if len(st.Sessions) != 0 {
		t.Errorf("FleetOnDisk reported %d sessions from a sweep that verified none (every ps hung): %+v", len(st.Sessions), st.Sessions)
	}
}

// The deadline must never silently under-report. With a fast ps and every record
// naming a live group leader, FleetOnDisk has to return all of them: a status
// view that quietly loses a live orphan is worse than a slow one, and it is the
// failure the loop's new break could introduce if it fired while probes were
// still answering promptly.
func TestFleetOnDiskReportsEveryRecordWhoseProbeAnswers(t *testing.T) {
	sock := tempSocket(t)

	const n = 20
	ids := make([]string, n)
	for i := range ids {
		ids[i] = testSessionID(fmt.Sprintf("%04x", i))
	}
	// One real group leader whose command line carries every id, so the real ps
	// verifies each record against it. FleetOnDisk probes per record whether or
	// not the pids differ, so one process exercises the whole loop without
	// spawning n copies of the test binary. The default budget stands: fast
	// probes never approach it.
	agent := startLingerer(t, ids...)

	recs := make([]record, n)
	for i := range recs {
		recs[i] = record{
			ID:      ids[i],
			Name:    fmt.Sprintf("agent-%02d", i),
			PID:     agent.Process.Pid,
			Started: time.Now().Add(-time.Hour),
		}
	}
	writeRoster(t, rosterPath(sock), recs)

	st := FleetOnDisk(sock)
	if len(st.Sessions) != n {
		t.Fatalf("FleetOnDisk reported %d of %d live records; the budget must not drop a record whose probe answered promptly", len(st.Sessions), n)
	}
	// And a complete sweep of live records must NOT flag itself incomplete: an
	// alive final probe appends and the loop ends without ever checking ctx.Err(),
	// so a budget expiring right after it cannot over-report. This is the
	// over-report half of the fail-closed signal, authored to stay green. (A final
	// record answering *dead* in the instant the budget expires is the one boundary
	// ctx.Err() cannot separate from a cut-short probe; it errs to incomplete -
	// fail-safe, never a false-down - see the note in FleetOnDisk.)
	if st.ProbeIncomplete {
		t.Errorf("FleetOnDisk set ProbeIncomplete on a fully-verified fleet (fast ps, default budget): reporting a complete sweep as incomplete would make `wake stop` refuse to confirm a fleet that is genuinely down")
	}
	for _, s := range st.Sessions {
		if s.State != rpc.StateOrphaned {
			t.Errorf("session %s reported %v, want %v", s.ID, s.State, rpc.StateOrphaned)
		}
	}
}

func writeRoster(t *testing.T, path string, recs []record) {
	t.Helper()

	data, err := json.Marshal(recs)
	if err != nil {
		t.Fatalf("encode roster: %v", err)
	}
	if err := os.WriteFile(path, data, rosterPerm); err != nil {
		t.Fatalf("write roster: %v", err)
	}
}

// lingerer is a process that leads its own group and stays alive, plus the
// only reliable way to find out that it stopped.
//
// died is not a convenience. A child this process has not reaped is a zombie,
// and kill(pid, 0) succeeds on a zombie - so "is it still alive" answered that
// way says yes about a process that was SIGKILLed a moment ago. The first
// version of this test asked exactly that and passed against a reaper with its
// verification deleted. Wait is what actually distinguishes them.
//
// It is *closed* rather than sent to, and the reason cost a mutation run. A
// one-shot channel can be read once: a test that took the value and then
// failed left its own cleanup parked on a second receive that would never
// arrive, so the mutation it was written to catch surfaced as a package
// timeout instead of an assertion. A closed channel answers everyone.
type lingerer struct {
	*exec.Cmd
	died <-chan struct{}

	// why it died, valid once died is closed - the close is what publishes it.
	why error
}

// startLingererInItsOwnGroup spawns one. Its command line carries no session
// id, which is the whole point: it looks like a recycled pid.
func startLingererInItsOwnGroup(t *testing.T) *lingerer {
	t.Helper()
	return startLingerer(t)
}

// startLingererCarrying spawns one whose command line *does* carry a session
// id, so verifyAgent says yes about it. It is a stand-in for a live agent a
// crashed daemon left behind - the thing a reaper that ran when it should not
// have would kill.
//
// The flags are never parsed: TestMain branches on the linger marker before
// the testing package looks at a single argument.
func startLingererCarrying(t *testing.T, sessionID string) *lingerer {
	t.Helper()
	return startLingerer(t, "--session-id", sessionID)
}

func startLingerer(t *testing.T, args ...string) *lingerer {
	t.Helper()

	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), fakeLingerEnv+"=1")
	leaveProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lingerer: %v", err)
	}

	l := &lingerer{Cmd: cmd}
	died := make(chan struct{})
	l.died = died
	go func() {
		l.why = cmd.Wait()
		close(died)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-died
	})
	return l
}

func waitForProcessGone(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for {
		if !processAlive(pid) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process group %d outlived the daemon that recorded it and the daemon that was supposed to reap it", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForDaemonGone waits until nothing answers on the socket. It is for a
// daemon that was killed, which leaves its socket file behind - see
// waitForCleanExit for one that was asked to leave.
func waitForDaemonGone(t *testing.T, sock string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for {
		conn, err := Dial(sock)
		if err != nil {
			return
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("something is still listening after the daemon was killed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForCleanExit waits until a daemon in another process has finished, not
// merely stopped listening.
//
// The gap between those is real and cost a flaky test to find. Serve closes
// its listener first, and closing a net.UnixListener *unlinks the socket
// file* - so the socket disappearing marks the beginning of the shutdown, not
// the end, and a test that stopped there deleted the directory out from under
// a daemon still writing to it.
//
// The roster is the honest marker: removing it is the last thing a daemon
// does before returning, and it is the last thing it writes to disk.
func waitForCleanExit(t *testing.T, sock string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for {
		_, sockErr := os.Stat(sock)
		_, rosterErr := os.Stat(rosterPath(sock))
		if os.IsNotExist(sockErr) && os.IsNotExist(rosterErr) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon on %s never finished shutting down", sock)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// "Quit, then start again" is the most ordinary thing a user does, and until
// this was fixed it SIGKILLed the fleet the old daemon was letting finish.
//
// Closing a net.UnixListener unlinks the socket file, and shutdown then takes
// up to quitGrace letting in-flight turns end - while the roster still names
// every one of those agents. In that window a new daemon finds no socket,
// starts, reads that roster and reaps a fleet that is very much alive: 15-30
// agents killed mid-Edit, by the four-verb design's own cleanup path.
//
// The agent here never reads its stdin, so stopping it cannot end it and
// shutdown must sit out the whole grace - which is the window, held open.
//
// # Why the barrier is a frame and not a poll
//
// This test used to enter the window by waiting for the roster to name the
// agent. That is not a synchronisation point at all: s.record writes the entry
// during spawn, strictly before the status frame c.spawn already waited for -
// so the predicate was true before FrameQuit was even sent and the wait
// returned on its first iteration. Nothing made the daemon have processed the
// quit, and under the mutation this test exists to catch the two outcomes were
// "passes with the bug present" and "hangs to the package timeout". Neither
// assertion below was reachable.
//
// enterShutdownWindow is proof rather than a hope: s.quit is closed by
// beginQuit before that refusal can be produced, and serveClient keeps
// dispatching for the whole grace.
func TestARestartDuringShutdownDoesNotReapTheFleetItIsWaitingFor(t *testing.T) {
	fakeClaudeOnPath(t, "deaf")
	// The grace has to outlast the assertions below and nothing more, and they
	// are all sub-100ms now that entering the window is a frame rather than a
	// race. It is kept short for headroom rather than for speed: this daemon's
	// worst legitimate shutdown is the grace plus shutdownWait, and
	// waitForExit gives it testTimeout - at 6s that left four seconds of slack
	// on a machine that might be running the soak in another terminal, which
	// is not enough margin for a bound whose job is to catch a daemon that
	// cannot exit at all. TestTheShutdownBudgetKeepsItsMargin holds that slack
	// to the number this const names.
	shortQuitGrace(t, restartGuardGrace)

	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	st := c.status()
	sessions := live(st)
	if len(sessions) != 1 || sessions[0].PID <= 0 {
		t.Fatalf("status = %+v, want one session with a process group", st.Sessions)
	}
	agent := sessions[0].PID

	c.enterShutdownWindow()

	// The precondition, asserted rather than waited for: the roster still
	// names a live agent, which is the state a second daemon's reaper acts on.
	// Without it the rest of this test is about nothing.
	if !rosterNames(d.socket, idAlpha) {
		t.Fatalf("the roster does not name %s inside the shutdown window, so there is nothing here for a reaper to get wrong", idAlpha)
	}

	// C1's own mechanism, isolated. The listener is held bound through the
	// whole grace precisely so this succeeds; closing it instead unlinks the
	// socket, and a `wake` run in this window finds nothing and forks.
	conn, err := Dial(d.socket)
	if err != nil {
		t.Fatalf("Dial during the shutdown window: %v - the socket was unlinked while the fleet was still up, which is what sends the next `wake` off to fork a second daemon", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("close: %v", err)
	}

	// The motion under test. A second daemon must refuse to start, which is
	// what keeps its reaper away from a fleet somebody else is holding.
	// Bounded, so a Serve that wrongly starts fails here rather than blocking
	// in its accept loop on a context nobody cancels.
	sctx, scancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer scancel()
	serveErr := Serve(sctx, d.socket)

	// The harm first, because it is the harm: whatever the second daemon did,
	// the agent the first one is waiting for must still be running.
	//
	// Asked of the OS through goneNow rather than with kill(pid, 0), which
	// succeeds on a zombie - and a SIGKILLed agent of an in-process daemon is
	// a zombie until core's pump unparks and reaps it, so signal 0 would say
	// "alive" about exactly the corpse this is looking for. goneNow treats a
	// zombie as gone and says when it could not tell.
	gctx, gcancel := context.WithTimeout(context.Background(), probeTimeout)
	defer gcancel()
	gone, gerr := goneNow(gctx, []watched{{id: idAlpha, pid: agent}})
	if gerr != nil {
		t.Fatalf("could not establish whether agent %d survived, so this proves nothing either way: %v", agent, gerr)
	}
	if gone[idAlpha] {
		t.Fatalf("agent %d was killed during the shutdown window: 15-30 of these, mid-Edit, by the cleanup path", agent)
	}

	if serveErr == nil {
		t.Fatal("a daemon started while another was shutting down: its reaper is about to SIGKILL a fleet mid-turn")
	}
	if !strings.Contains(serveErr.Error(), "already running") {
		t.Errorf("Serve = %v, want it to say a daemon is already running", serveErr)
	}

	d.waitForExit(t)
}

// rosterNames reports whether the roster on disk carries an id right now. It
// is a question, not a wait: everything this file needs to be true about the
// roster is already true by the time the daemon has answered a frame.
func rosterNames(sock, id string) bool {
	for _, r := range loadRoster(rosterPath(sock)) {
		if r.ID == id {
			return true
		}
	}
	return false
}
