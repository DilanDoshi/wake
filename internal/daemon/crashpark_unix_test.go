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
	d := startDaemonOn(t, sock)
	c := attach(t, d.socket)
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
}

// A /clear moves the agent onto a conversation claude mints, and a wake resumes
// that one, so the file has to follow it - through a real /clear, with nobody
// pushing a status by hand.
func TestAClearReachesTheParkReadyFile(t *testing.T) {
	rememberingClaudeOnPath(t)
	d := startDaemon(t)
	c := attach(t, d.socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	conversation := clearSession(c, idAlpha)

	awaitReady(t, d.socket, idAlpha, "the conversation after a /clear", func(r parkedRecord) bool { return r.ID == conversation })
}

// Nothing promises a status push after a /clear's successor speaks, and with
// none the file is told by the event that names the new conversation.
func TestAClearWithNoPushAfterItStillReachesTheParkReadyFile(t *testing.T) {
	fakeClaudeOnPath(t, "clearquiet")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "done")

	conversation := clearSession(c, idAlpha)

	awaitReady(t, d.socket, idAlpha, "the conversation after a quiet /clear", func(r parkedRecord) bool { return r.ID == conversation })
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
	// A kill, not a stop: a stop drops the row itself (TestAStopDrops...), and
	// this is the ending that leaves it to retire.
	c.send(rpc.Frame{Kind: rpc.FrameKill, SessionID: idBeta})
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

// A force restart or a power cut takes the daemon and every agent in the same
// instant, so the next daemon finds nothing alive at all: every row is a gone
// pid rather than a group to signal, and every session still comes back.
func TestAFleetWhoseDaemonAndAgentsAllDiedAtOnceComesBackParked(t *testing.T) {
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
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")
	c.spawn(idBeta, "robin")
	c.awaitEvent(idBeta, "ready")
	c.send(rpc.Frame{Kind: rpc.FrameTeam, SessionID: idAlpha, Text: "api"})
	awaitReady(t, sock, idAlpha, "the team", func(r parkedRecord) bool { return r.Team == "api" })
	awaitReady(t, sock, idBeta, "the spawn", func(parkedRecord) bool { return true })

	daemon := c.status().PID
	groups := loadRoster(rosterPath(sock))
	if len(groups) != 2 {
		t.Fatalf("the roster holds %+v, want both agents' groups", groups)
	}
	if err := syscall.Kill(daemon, syscall.SIGKILL); err != nil {
		t.Fatalf("kill the daemon: %v", err)
	}
	for _, r := range groups {
		if err := syscall.Kill(-r.PID, syscall.SIGKILL); err != nil {
			t.Fatalf("kill session %s's group %d: %v", r.ID, r.PID, err)
		}
	}
	waitForDaemonGone(t, sock)
	for _, r := range groups {
		waitForProcessGone(t, r.PID)
	}

	startDaemonOn(t, sock)
	byID := map[string]rpc.SessionStatus{}
	for _, p := range attachConn(t, dialWhenUp(t, sock)).status().Parked {
		byID[p.ID] = p
	}
	if alex := byID[idAlpha]; alex.Name != "alex" || alex.Team != "api" {
		t.Errorf("alex comes back as %+v, want its name and team", alex)
	}
	if robin := byID[idBeta]; robin.Name != "robin" {
		t.Errorf("robin comes back as %+v", robin)
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

// The roster is the proof, and the proof is per session: a park-ready record
// beside a roster that names somebody else is one that ended before the crash.
func TestAStaleParkReadyRecordBesideAnotherSessionsRosterRowIsDropped(t *testing.T) {
	sock := tempSocket(t)
	writeReady(t, sock, map[string]parkedRecord{
		idAlpha: {ID: idAlpha, Name: "alex", Dir: "/tmp/repo"},
		idBeta:  {ID: idBeta, Name: "robin", Dir: "/tmp/repo"},
	})
	writeRoster(t, rosterPath(sock), []record{{ID: idBeta, Name: "robin", PID: deadPID(t), Started: time.Now()}})

	s := newServer(sock)
	s.reapOrphans()

	if _, held := s.parked.record(idAlpha); held {
		t.Error("a session with no roster row was parked from its park-ready record: that offers back one the operator ended")
	}
	if _, held := s.parked.record(idBeta); !held {
		t.Error("the session the roster names was not parked")
	}
	if !fileGone(parkReadyPath(sock)) {
		t.Error("the stale record outlived the reaper")
	}
}

// A park-ready file is a file somebody may have edited, and its conversation id
// reaches --resume.
func TestAParkReadyRecordNamingAnIdWakeDidNotMintIsNotParked(t *testing.T) {
	sock := tempSocket(t)
	writeReady(t, sock, map[string]parkedRecord{idAlpha: {ID: "not-an-id-wake-minted", Name: "alex", Dir: "/tmp/repo"}})
	writeRoster(t, rosterPath(sock), []record{{ID: idAlpha, Name: "alex", PID: deadPID(t), Started: time.Now()}})

	s := newServer(sock)
	s.reapOrphans()

	if recs := s.parked.records(); len(recs) != 0 {
		t.Errorf("the park book holds %+v, which no wake could resume", recs)
	}
	if rosterNames(sock, idAlpha) {
		t.Error("the row was retained with nothing that could ever be handed over")
	}
}

// A wake whose book deletion failed leaves the old row behind, and a crash then
// finds it beside a roster row and a newer record. The newer one is the truth.
func TestAHandOverReplacesAParkBookRowOlderThanTheLastChange(t *testing.T) {
	sock := tempSocket(t)
	stale := parkedRecord{ID: idBeta, Name: "old", Dir: "/tmp/repo", Parked: time.Now().Add(-time.Hour)}
	if err := newParkBook(parkBookPath(sock)).add(stale); err != nil {
		t.Fatal(err)
	}
	writeReady(t, sock, map[string]parkedRecord{idAlpha: {ID: idBeta, Name: "alex", Team: "api", Dir: "/tmp/repo", Parked: time.Now()}})
	writeRoster(t, rosterPath(sock), []record{{ID: idAlpha, Name: "alex", PID: deadPID(t), Started: time.Now()}})

	s := newServer(sock)
	s.reapOrphans()

	if got, _ := s.parked.record(idBeta); got.Name != "alex" || got.Team != "api" {
		t.Errorf("the book holds %+v: the stale row from before the wake beat what the session was set to since", got)
	}
}

// A failed start takes its roster row back, and its park-ready record with it.
func TestAFailedStartLeavesNoParkReadyRecord(t *testing.T) {
	sock := tempSocket(t)
	s := newServer(sock)
	a := parkedRow(t, s, idAlpha, "alex")
	if err := s.ready.set(idAlpha, recordFor(a)); err != nil {
		t.Fatal(err)
	}

	s.withdraw(a, nil, nil)

	if _, ok := loadParkReady(parkReadyPath(sock))[idAlpha]; ok {
		t.Error("a start that never happened left a record a crash would park")
	}
}

// A stop is the operator ending a session, and its turn may take a while to
// finish: dying in that wait must not offer the session back. The deaf agent
// outlives its closed stdin, so the session is still there when this looks.
func TestAStopDropsTheParkReadyRecordBeforeTheSessionHasEnded(t *testing.T) {
	fakeClaudeOnPath(t, "deaf")
	shortQuitGrace(t, 2*time.Second)
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")
	awaitReady(t, d.socket, idAlpha, "the spawn", func(parkedRecord) bool { return true })

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})

	deadline := time.Now().Add(testTimeout)
	for len(loadParkReady(parkReadyPath(d.socket))) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("a stopped session kept its park-ready record: a crash during its last turn would park what the operator ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state := stateOf(c.status(), idAlpha); state == rpc.StateEnded {
		t.Fatal("the session had already ended, so this does not show the wait it is about")
	}
	// And a status push does not offer it again: a rename publishes one.
	c.send(rpc.Frame{Kind: rpc.FrameRename, SessionID: idAlpha, Text: "alexis"})
	c.status()
	if rows := loadParkReady(parkReadyPath(d.socket)); len(rows) != 0 {
		t.Errorf("a push wrote %+v back for a session that was asked to stop", rows)
	}
}

// wake stop is irreversible, so a daemon dying in its grace must leave nothing
// for the next one to hand over - while the agents are still ending.
func TestAFleetBeingStoppedDropsTheParkReadyFileBeforeItsAgentsHaveEnded(t *testing.T) {
	fakeClaudeOnPath(t, "deaf")
	shortQuitGrace(t, 3*time.Second)
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")
	awaitReady(t, d.socket, idAlpha, "the spawn", func(parkedRecord) bool { return true })

	c.send(rpc.Frame{Kind: rpc.FrameQuit})

	deadline := time.Now().Add(time.Second)
	for !fileGone(parkReadyPath(d.socket)) {
		if time.Now().After(deadline) {
			t.Fatal("the park-ready file outlived the start of a wake stop: a crash in the grace would hand the stopped fleet to the next daemon")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if d.waitForExitWithin(t, 10*time.Millisecond) {
		t.Fatal("the daemon had already exited, so this does not show the grace it is about")
	}
}

// retire removes a row after finish, and a write that took its record before
// finish must not put it back.
func TestARowWrittenAfterItsAgentDepartedDoesNotStay(t *testing.T) {
	s := newServer(tempSocket(t))
	a := newAgent(idAlpha, "alex", "dev", "/tmp/repo", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	rec, running := a.runningRecord()
	if !running {
		t.Fatal("a fresh agent does not read as running")
	}

	a.finish(nil)
	s.forgetReady(a.id)
	if err := s.writeReady(a, rec); err != nil {
		t.Fatal(err)
	}

	if rows := loadParkReady(parkReadyPath(s.socket)); len(rows) != 0 {
		t.Errorf("the file holds %+v for a session that had already ended: a crash would park it", rows)
	}
}

// A session being parked keeps its row until completePark takes it, whatever
// state its process is in: a crash in a quit park's grace has to find it.
func TestAParkingAgentDoesNotReadAsDeparted(t *testing.T) {
	a := newAgent(idAlpha, "alex", "dev", "/tmp/repo", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	a.beginPark()
	if err := a.stop(); err != nil {
		t.Logf("stop: %v", err) // nothing was started, so there is nothing to close
	}
	a.finish(nil)

	if a.departed() {
		t.Error("an agent that is parking read as departed, so its record would be dropped while its roster row still stands")
	}
	if _, running := a.runningRecord(); !running {
		t.Error("a parking agent offered no record")
	}
}

func bareAgent() *agent {
	return newAgent(idAlpha, "alex", "dev", "/tmp/repo", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
}

// A stop the agent's queue refused did not happen, and the agent goes on
// running: taking its only crash record with it would lose it to a crash.
func TestARejectedStopKeepsTheParkReadyRecord(t *testing.T) {
	sock := tempSocket(t)
	s := newServer(sock)
	a := bareAgent()
	if !s.register(a) {
		t.Fatal("the agent could not be put in the fleet")
	}
	s.noteReady(a)
	for len(a.in) < cap(a.in) {
		a.in <- pending{}
	}
	conn, peer := net.Pipe()
	t.Cleanup(func() { _ = conn.Close(); _ = peer.Close() })

	s.submit(newClient(conn), rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})

	if _, ok := loadParkReady(parkReadyPath(sock))[idAlpha]; !ok {
		t.Error("a stop that was refused removed the record of an agent that is still running")
	}
	if a.departed() {
		t.Error("a stop that was refused left the agent fenced out of the park-ready file")
	}
}

// The fence goes up when the operator asks, not when the stop is carried out
// behind whatever is queued first, and a row taken before it cannot land after.
func TestAQueuedStopFencesTheParkReadyWriters(t *testing.T) {
	s := newServer(tempSocket(t))
	a := bareAgent()
	before, _ := a.runningRecord()

	a.markStopQueued(true)
	s.forgetReady(a.id)

	if err := s.keepReady(a); err != nil {
		t.Fatal(err)
	}
	if err := s.writeReady(a, before); err != nil {
		t.Fatal(err)
	}
	if rows := loadParkReady(parkReadyPath(s.socket)); len(rows) != 0 {
		t.Errorf("the file holds %+v for a session whose stop was already asked for", rows)
	}
}

// A wake stop that has begun leaves nothing, whichever writer is still in
// flight: the file is not written again, so the grace cannot recreate it.
func TestAStoppedFleetsParkReadyFileStaysGoneUnderALateWriter(t *testing.T) {
	s := newServer(tempSocket(t))
	a := bareAgent()
	rec, _ := a.runningRecord()

	s.dropReady()
	if err := s.ready.set(a.id, rec); err != nil {
		t.Fatal(err)
	}
	if err := s.keepReady(a); err != nil {
		t.Fatal(err)
	}

	if !fileGone(parkReadyPath(s.socket)) {
		t.Error("a writer in flight brought the park-ready file back after a wake stop began")
	}
}

// Taking a row and writing it are two steps, so an older row can land after a
// newer one - a /clear's successor, a team - and must not be what a crash finds.
func TestAnOlderRowLandingLateDoesNotUndoANewerOne(t *testing.T) {
	s := newServer(tempSocket(t))
	a := bareAgent()
	old, _ := a.runningRecord()
	if err := a.setTeam("api"); err != nil {
		t.Fatal(err)
	}
	if err := s.keepReady(a); err != nil {
		t.Fatal(err)
	}

	if err := s.writeReady(a, old); err != nil {
		t.Fatal(err)
	}

	if got := loadParkReady(parkReadyPath(s.socket))[idAlpha]; got.Team != "api" {
		t.Errorf("the file holds %+v: an older snapshot overwrote what the operator set since", got)
	}
}

// The stop is queued behind whatever the agent was sent first and may sit there
// a while; the fence is up from the moment it was accepted, not when it is
// carried out. No input goroutine runs here, so the stop is never carried out.
func TestAQueuedStopIsFencedFromTheMomentItIsAccepted(t *testing.T) {
	s := newServer(tempSocket(t))
	a := bareAgent()
	if !s.register(a) {
		t.Fatal("the agent could not be put in the fleet")
	}
	s.noteReady(a)
	conn, peer := net.Pipe()
	t.Cleanup(func() { _ = conn.Close(); _ = peer.Close() })

	s.submit(newClient(conn), rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})

	if !a.departed() {
		t.Fatal("an accepted stop left the agent offering its record to every status push until it is carried out")
	}
	s.syncReady()
	if rows := loadParkReady(parkReadyPath(s.socket)); len(rows) != 0 {
		t.Errorf("the file holds %+v for a session whose stop was accepted", rows)
	}
}
