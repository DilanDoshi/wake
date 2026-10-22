//go:build unix

// A daemon that dies without parking anything still hands its fleet on
// (docs/notes/bugs.md BUG-53). Unix-only because the subject is a real daemon
// SIGKILLed with real agents behind it, and the reaper's verdicts come from the
// OS.

package daemon

import (
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

var idEcho = testSessionID("e55e")

// deadPID is a pid that led a process group and has exited: what a recorded
// agent looks like after a reboot, when nothing at all holds its number.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	leaveProcessGroup(cmd)
	if err := cmd.Run(); err != nil {
		t.Fatalf("run a process to its end: %v", err)
	}
	return cmd.Process.Pid
}

// writeReady leaves a park-ready file the way a daemon that was writing one
// would have, keyed by the spawn id its roster row names.
func writeReady(t *testing.T, sock string, rows map[string]parkedRecord) {
	t.Helper()
	p := newParkReady(parkReadyPath(sock))
	for session, rec := range rows {
		if err := p.set(session, rec); err != nil {
			t.Fatalf("write the park-ready record for %s: %v", session, err)
		}
	}
}

// dialWhenUp dials a daemon that is still binding. A SIGKILLed one leaves its
// socket file behind, so startDaemonOn's wait for the file proves nothing about
// its successor; the reaper has finished by the time a dial is accepted.
func dialWhenUp(t *testing.T, sock string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		conn, err := Dial(sock)
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing began listening on %s: %v", sock, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func fileGone(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

// The whole of the bug in one table: a roster row the reaper finishes - its
// process gone, or its pid someone else's now - becomes a park book record
// carrying everything a wake needs. The /clear row is keyed by its spawn id and
// parks under the conversation it was writing; the manager keeps the name
// claude knew it by.
func TestAFleetACrashLeftBehindIsHandedToTheParkBookWithEverythingAWakeNeeds(t *testing.T) {
	stranger := startLingererInItsOwnGroup(t)
	sock := tempSocket(t)
	for _, id := range []string{idBeta, idGamma, idEcho} {
		plantTranscript(t, id)
	}

	alex := parkedRecord{ID: idBeta, Name: "alex", Label: "dev", Dir: "/tmp/repo", Effort: core.EffortMax,
		Model: "opus", MaxBudgetUSD: "5", FallbackModel: "sonnet", Color: "blue", Team: "api"}
	manager := parkedRecord{ID: idGamma, Name: core.ManagerName, Dir: "/tmp/repo", ClaudeName: "manager-ops"}
	recycled := parkedRecord{ID: idEcho, Name: "robin", Dir: "/tmp/repo", Team: "web"}
	writeReady(t, sock, map[string]parkedRecord{idAlpha: alex, idGamma: manager, idEcho: recycled})
	writeRoster(t, rosterPath(sock), []record{
		{ID: idAlpha, Name: "alex", PID: deadPID(t), Started: time.Now().Add(-time.Hour)},
		{ID: idGamma, Name: core.ManagerName, PID: deadPID(t), Started: time.Now().Add(-time.Hour)},
		{ID: idEcho, Name: "robin", PID: stranger.Process.Pid, Started: time.Now().Add(-time.Hour)},
	})

	s := newServer(sock)
	s.reapOrphans()

	for _, want := range []parkedRecord{alex, manager, recycled} {
		got, ok := s.parked.record(want.ID)
		if !ok {
			t.Fatalf("%s (%s) is not in the park book after the reaper finished its row: a crash lost it, and /resume all cannot find it", want.Name, want.ID)
		}
		if !sameWakeInputs(got, want) {
			t.Errorf("%s was handed over as %+v, want %+v: a wake would bring it back without what the operator set", want.Name, got, want)
		}
		if time.Since(got.Parked) > time.Minute {
			t.Errorf("%s parked at %v: the hand-over is when Wake parked it", want.Name, got.Parked)
		}
	}
	if got, _ := s.parked.record(idGamma); restoredClaudeName(got) != "manager-ops" {
		t.Errorf("the manager would come back as %q, want manager-ops: every agent that reached it by that name loses it", restoredClaudeName(got))
	}
	if onDisk := loadParkBook(parkBookPath(sock)); len(onDisk) != 3 {
		t.Errorf("the park book on disk holds %+v, want the three sessions: the next daemon reads this file", onDisk)
	}

	parked := s.fleet().Parked
	if len(parked) != 3 {
		t.Fatalf("Status.Parked = %+v, want all three", parked)
	}
	for _, p := range parked {
		if p.ID == idBeta && (p.Name != "alex" || p.Team != "api" || p.Color != "blue" || p.Effort != core.EffortMax) {
			t.Errorf("alex is offered back as %+v: its team, colour and effort are what the operator set", p)
		}
	}

	if recs := loadRoster(rosterPath(sock)); len(recs) != 0 {
		t.Errorf("the roster still holds %+v: a finished row left behind is a pgid a later reaper hunts for", recs)
	}
	if !fileGone(parkReadyPath(sock)) {
		t.Error("the park-ready file survives its hand-over, so the same sessions would be handed over again")
	}
	select {
	case <-stranger.died:
		t.Fatalf("the reaper SIGKILLed a recycled pid's group while handing the fleet over: %v", stranger.why)
	case <-time.After(200 * time.Millisecond):
	}
}

// BUG-28's rule holds: a row the reaper could not finish is neither forgotten
// nor parked. The process may be a live agent, and a wake of a live session
// branches its transcript.
func TestARecordTheReaperRetainsStaysOutOfTheParkBook(t *testing.T) {
	orphan := startLingererCarrying(t, idAlpha)
	shortProbeTimeout(t, 200*time.Millisecond)
	brokenPsOnPath(t, psRefuses)
	sock := tempSocket(t)
	writeReady(t, sock, map[string]parkedRecord{idAlpha: {ID: idAlpha, Name: "alex", Dir: "/tmp/repo"}})
	writeRoster(t, rosterPath(sock), []record{{ID: idAlpha, Name: "alex", PID: orphan.Process.Pid, Started: time.Now()}})

	s := newServer(sock)
	s.reapOrphans()

	if recs := s.parked.records(); len(recs) != 0 {
		t.Errorf("the park book holds %+v for a process ps could not rule out: a wake would start a second claude on a live session", recs)
	}
	if !rosterNames(sock, idAlpha) {
		t.Error("the roster row was cleared though ps could not check it")
	}
	if _, ok := loadParkReady(parkReadyPath(sock))[idAlpha]; !ok {
		t.Error("the park-ready record was dropped with nothing else naming this session: the next daemon, whose ps works, has nothing to hand over")
	}
}

// A book record already under that conversation is a newer fact than a crashed
// daemon's last snapshot of a session.
func TestAHandOverNeverReplacesARecordTheParkBookAlreadyHolds(t *testing.T) {
	sock := tempSocket(t)
	held := parkedRecord{ID: idBeta, Name: "parked-by-hand", Dir: "/tmp/repo", Parked: time.Now().Add(-time.Hour)}
	if err := newParkBook(parkBookPath(sock)).add(held); err != nil {
		t.Fatal(err)
	}
	writeReady(t, sock, map[string]parkedRecord{idAlpha: {ID: idBeta, Name: "alex", Dir: "/tmp/repo"}})
	writeRoster(t, rosterPath(sock), []record{{ID: idAlpha, Name: "alex", PID: deadPID(t), Started: time.Now()}})

	s := newServer(sock)
	s.reapOrphans()

	// Compared field by field: held carries a monotonic clock reading the file
	// it was read back from does not.
	if got, _ := s.parked.record(idBeta); got.Name != held.Name || !got.Parked.Equal(held.Parked) {
		t.Errorf("the hand-over replaced %+v with %+v", held, got)
	}
}

// Nothing to hand over is today's behaviour and must stay it: a roster row from
// a build that wrote no park-ready record, or whose write failed, is cleared.
func TestARosterRowWithNoParkReadyRecordIsForgottenAsBefore(t *testing.T) {
	sock := tempSocket(t)
	writeRoster(t, rosterPath(sock), []record{{ID: idAlpha, Name: "alex", PID: deadPID(t), Started: time.Now()}})

	s := newServer(sock)
	s.reapOrphans()

	if recs := s.parked.records(); len(recs) != 0 {
		t.Errorf("the park book holds %+v for a session nothing recorded a wake for", recs)
	}
	if rosterNames(sock, idAlpha) {
		t.Error("the row was retained with nothing to retry")
	}
}

// The roster is the proof a session was running when the daemon died. A
// park-ready record with no row beside it is one that ended, or parked, in the
// instant before the crash, and offering it back would undo a /quit.
func TestAParkReadyRecordWithNoRosterRowIsDroppedNotParked(t *testing.T) {
	sock := tempSocket(t)
	writeReady(t, sock, map[string]parkedRecord{idAlpha: {ID: idAlpha, Name: "alex", Dir: "/tmp/repo"}})

	s := newServer(sock)
	s.reapOrphans()

	if recs := s.parked.records(); len(recs) != 0 {
		t.Errorf("the park book holds %+v for a session whose roster row was already gone", recs)
	}
	if !fileGone(parkReadyPath(sock)) {
		t.Error("a stale park-ready file outlived the reaper")
	}
}

// A hand-over that could not be written keeps the row, so the next daemon asks
// again instead of forgetting a fleet because one write failed.
func TestAHandOverThatCouldNotBeWrittenKeepsTheRosterRow(t *testing.T) {
	sock := tempSocket(t)
	if err := os.Mkdir(parkBookPath(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	writeReady(t, sock, map[string]parkedRecord{idAlpha: {ID: idAlpha, Name: "alex", Dir: "/tmp/repo"}})
	writeRoster(t, rosterPath(sock), []record{{ID: idAlpha, Name: "alex", PID: deadPID(t), Started: time.Now()}})

	newServer(sock).reapOrphans()

	if !rosterNames(sock, idAlpha) {
		t.Error("the row was cleared though the park book could not be written: that fleet is now lost")
	}
	if _, ok := loadParkReady(parkReadyPath(sock))[idAlpha]; !ok {
		t.Error("the park-ready record was dropped with its row")
	}
}

// awaitReady polls the park-ready file until the record for session satisfies
// ok, which is how a test waits for a daemon to have written something down.
func awaitReady(t *testing.T, sock, session, what string, ok func(parkedRecord) bool) parkedRecord {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if rec, held := loadParkReady(parkReadyPath(sock))[session]; held && ok(rec) {
			return rec
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the park-ready file never recorded %s for %s: %+v", what, session, loadParkReady(parkReadyPath(sock)))
	return parkedRecord{}
}

// Every input a wake reads reaches the file when it changes, not only at spawn:
// a record frozen at launch is the operator's team, colour, name and /clear
// reverted by a crash.
func TestALiveAgentIsKeptParkReadyAsWhatAWakeNeedsChanges(t *testing.T) {
	fakeClaudeOnPath(t, "")
	sock := tempSocket(t)
	s := newServer(sock)
	startControlledServer(t, s)
	c := attach(t, sock)
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Effort: core.EffortLow})
	c.awaitState(idAlpha, rpc.StateIdle)

	rec := awaitReady(t, sock, idAlpha, "the spawn", func(r parkedRecord) bool { return r.Name == "alex" })
	if rec.ID != idAlpha || rec.Effort != core.EffortLow {
		t.Errorf("the spawn was written down as %+v", rec)
	}

	c.send(rpc.Frame{Kind: rpc.FrameRename, SessionID: idAlpha, Text: "alexis"})
	awaitReady(t, sock, idAlpha, "the rename", func(r parkedRecord) bool { return r.Name == "alexis" })
	c.send(rpc.Frame{Kind: rpc.FrameTeam, SessionID: idAlpha, Text: "api"})
	awaitReady(t, sock, idAlpha, "the team", func(r parkedRecord) bool { return r.Team == "api" })
	c.send(rpc.Frame{Kind: rpc.FrameColor, SessionID: idAlpha, Text: "blue"})
	awaitReady(t, sock, idAlpha, "the colour", func(r parkedRecord) bool { return r.Color == "blue" })

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/effort high"})
	awaitReady(t, sock, idAlpha, "the effort", func(r parkedRecord) bool { return r.Effort == core.EffortHigh })

	// A /clear moves the agent onto a conversation claude mints; a wake resumes
	// that one, so the file has to follow it.
	s.mu.Lock()
	a := s.agents[idAlpha]
	s.mu.Unlock()
	clearTo(a, idAlpha, idBeta)
	s.pushStatus()
	awaitReady(t, sock, idAlpha, "the conversation after a /clear", func(r parkedRecord) bool { return r.ID == idBeta })
}

// A session that ended or parked leaves nothing for a crash to hand over: its
// roster row is gone, and a stale park-ready record outliving it is the
// resurrection of something the operator ended.
func TestASessionThatEndedOrParkedLeavesNoParkReadyRecord(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.spawn(idBeta, "robin")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.awaitState(idBeta, rpc.StateIdle)
	awaitReady(t, d.socket, idAlpha, "the spawn", func(parkedRecord) bool { return true })
	awaitReady(t, d.socket, idBeta, "the spawn", func(parkedRecord) bool { return true })

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idBeta})
	c.awaitState(idBeta, rpc.StateEnded)
	c.status() // behind both retires' status pushes

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if len(loadParkReady(parkReadyPath(d.socket))) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("the file still holds %+v after both sessions left: a crash now would park a session the operator ended", loadParkReady(parkReadyPath(d.socket)))
}

// Every ending the daemon chooses leaves no file behind, so a file present at
// startup always means the last daemon died.
func TestAnOrderlyEndingLeavesNoParkReadyFile(t *testing.T) {
	for _, verb := range []string{rpc.FrameQuit, rpc.FrameParkAll} {
		t.Run(verb, func(t *testing.T) {
			fakeClaudeOnPath(t, "")
			d := startDaemon(t)
			c := attach(t, d.socket)
			c.spawn(idAlpha, "alex")
			c.awaitState(idAlpha, rpc.StateIdle)
			awaitReady(t, d.socket, idAlpha, "the spawn", func(parkedRecord) bool { return true })

			c.send(rpc.Frame{Kind: verb})
			d.waitForExit(t)

			if !fileGone(parkReadyPath(d.socket)) {
				t.Errorf("%s left a park-ready file: the next daemon would hand a fleet over that somebody ended on purpose", verb)
			}
		})
	}
}

// A SIGTERM is the machine going down or somebody ending the daemon, and
// nobody in that story chose to lose the fleet (owner's 2026-10-21 ruling,
// reversing quitVerb's "neither may park a fleet nobody asked to park"). The
// cancelled context Serve is given is what signal.NotifyContext delivers.
func TestADaemonThatWasSignalledParksItsFleet(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Effort: core.EffortMax})
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FrameTeam, SessionID: idAlpha, Text: "api"})
	awaitReady(t, d.socket, idAlpha, "the team", func(r parkedRecord) bool { return r.Team == "api" })
	c.close()

	d.stop(t)

	recs := loadParkBook(parkBookPath(d.socket))
	if len(recs) != 1 || recs[0].Name != "alex" || recs[0].Team != "api" || recs[0].Effort != core.EffortMax {
		t.Fatalf("the park book holds %+v after the daemon was signalled, want alex with the team and effort it ran with: "+
			"a clean OS shutdown loses the fleet otherwise", recs)
	}
	if !fileGone(parkReadyPath(d.socket)) || !fileGone(rosterPath(d.socket)) {
		t.Error("a parked fleet left its roster or park-ready file behind: the next daemon would hand it over a second time")
	}
}

// The scenario from the bug, with a real daemon process: a fleet with teams, a
// colour, an effort and a name is SIGKILLed with its roster left behind. The
// next daemon on that socket reports every session parked with those fields,
// and a wake brings one back.
func TestAFleetWhoseDaemonWasKilledComesBackParkedWithItsConfiguration(t *testing.T) {
	fakeClaudeOnPath(t, "deaf")
	plantTranscript(t, idAlpha)
	plantTranscript(t, idBeta)
	sock := tempSocket(t)
	t.Setenv(SocketEnv, sock)
	t.Setenv(fakeDaemonEnv, "1")

	conn, err := EnsureRunning(t.Context(), sock)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	c := attachConn(t, conn)
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Effort: core.EffortMax})
	c.awaitEvent(idAlpha, "ready")
	c.spawn(idBeta, "robin")
	c.awaitEvent(idBeta, "ready")
	c.send(rpc.Frame{Kind: rpc.FrameTeam, SessionID: idAlpha, Text: "api"})
	c.send(rpc.Frame{Kind: rpc.FrameColor, SessionID: idAlpha, Text: "blue"})
	c.send(rpc.Frame{Kind: rpc.FrameTeam, SessionID: idBeta, Text: "web"})
	awaitReady(t, sock, idAlpha, "the team and colour", func(r parkedRecord) bool { return r.Team == "api" && r.Color == "blue" })
	awaitReady(t, sock, idBeta, "the team", func(r parkedRecord) bool { return r.Team == "web" })

	daemon := c.status().PID
	if err := syscall.Kill(daemon, syscall.SIGKILL); err != nil {
		t.Fatalf("kill the daemon: %v", err)
	}
	waitForDaemonGone(t, sock)

	startDaemonOn(t, sock)
	c2 := attachConn(t, dialWhenUp(t, sock))
	byID := map[string]rpc.SessionStatus{}
	for _, p := range c2.status().Parked {
		byID[p.ID] = p
	}
	alex, robin := byID[idAlpha], byID[idBeta]
	if alex.Name != "alex" || alex.Team != "api" || alex.Color != "blue" || alex.Effort != core.EffortMax {
		t.Errorf("alex comes back as %+v, want its name, team, colour and effort: the next daemon reported a room holding nothing", alex)
	}
	if robin.Name != "robin" || robin.Team != "web" {
		t.Errorf("robin comes back as %+v", robin)
	}

	if got := wakeOutcome(c2, idAlpha); !got.woke {
		t.Fatalf("the crashed session could not be woken: %s", got.why)
	}
}
