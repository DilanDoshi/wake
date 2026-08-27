//go:build unix

package daemon

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Omitting failure restoration loses the record-only row and rewrites the
// in-memory row with a new park timestamp during shutdown. Weakening shutdown
// admission instead starts a process after quit committed and fails the same
// restart invariant.
func TestWakeRacingParkAllPreservesExactRecordAcrossRestart(t *testing.T) {
	for _, source := range []string{"record-only", "in-memory"} {
		t.Run(source, func(t *testing.T) {
			fakeClaudeOnPath(t, "")
			socket := tempSocket(t)
			s := newServer(socket)
			want := durableParkedRecord(t)
			installDurableParkSource(t, s, want, source)
			d := startControlledServer(t, s)
			waker := attach(t, socket)
			quitter := attach(t, socket)

			s.admitMu.Lock()
			locked := true
			defer func() {
				if locked {
					s.admitMu.Unlock()
				}
			}()
			waker.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: want.ID})
			waitForParkReservation(t, s, want.ID)

			quitter.send(rpc.Frame{Kind: rpc.FrameParkAll})
			<-s.quit // the admission refusal is now irrevocable
			s.admitMu.Unlock()
			locked = false
			d.waitForExit(t)

			assertExactRecordAfterRestart(t, socket, want)
		})
	}
}

// Deleting held/on-disk state at reserve loses this exact record when bounded
// shutdown exits the forked daemon before its blocked dispatch can restore.
func TestForkedDaemonKeepsWakeReservationDurablePastShutdownWait(t *testing.T) {
	socket := tempSocket(t)
	want := durableParkedRecord(t)
	if err := newParkBook(parkBookPath(socket)).add(want); err != nil {
		t.Fatalf("write initial park record: %v", err)
	}
	gatePath := tempSocket(t)
	gateListener, err := net.Listen("unix", gatePath)
	if err != nil {
		t.Fatalf("listen on admission gate: %v", err)
	}
	defer func() { _ = gateListener.Close() }()
	t.Setenv(SocketEnv, socket)
	t.Setenv(fakeDaemonEnv, "1")
	t.Setenv(fakeAdmitGateEnv, gatePath)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, err := EnsureRunning(ctx, socket)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	waker := attachConn(t, conn)
	pid := waker.status().PID
	gate, err := gateListener.Accept()
	if err != nil {
		t.Fatalf("accept admission gate: %v", err)
	}
	defer func() { _ = gate.Close() }()
	readGateByte(t, gate, 'L', "daemon holding admission")

	waker.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: want.ID})
	readGateByte(t, gate, 'R', "wake reserving the park record")
	quitter := attach(t, socket)
	if row := sessionRow(quitter.status(), want.ID); row.State != "" {
		t.Errorf("reserved wake still appears as parked status: %+v", row)
	}
	quitter.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: want.ID})
	refusal := quitter.await("duplicate wake refusal", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == want.ID
	})
	if !strings.Contains(refusal.Text, "already being brought back") {
		t.Errorf("duplicate wake refusal = %q, want the in-progress reservation named", refusal.Text)
	}
	quitter.send(rpc.Frame{Kind: rpc.FrameParkAll})

	if !daemonPIDExits(pid, shutdownWait+3*time.Second) {
		t.Fatalf("forked daemon pid %d did not exit after bounded shutdown", pid)
	}
	// The admission gate is intentionally still held. No dispatch restoration
	// can have run in the process that has now exited.
	got := loadParkBook(parkBookPath(socket))
	if len(got) != 1 || !sameParkedRecord(got[0], want) {
		t.Fatalf("park book after bounded forked shutdown = %+v, want exact durable record %+v", got, want)
	}
	assertExactRecordAfterRestart(t, socket, want)
}

func readGateByte(t *testing.T, gate net.Conn, want byte, what string) {
	t.Helper()
	if err := gate.SetReadDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatalf("set gate deadline: %v", err)
	}
	var got [1]byte
	if _, err := gate.Read(got[:]); err != nil {
		t.Fatalf("wait for %s: %v", what, err)
	}
	if got[0] != want {
		t.Fatalf("gate for %s = %q, want %q", what, got[0], want)
	}
}

// A successful process is the one outcome that commits deletion: current
// status and a successor must never offer its record while it is running.
func TestSuccessfulWakeCommitsDurableReservation(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	s := newServer(socket)
	want := durableParkedRecord(t)
	installDurableParkSource(t, s, want, "record-only")
	d := startControlledServer(t, s)
	c := attach(t, socket)
	if result := wakeOutcome(c, want.ID); !result.woke {
		t.Fatalf("wake failed: %s", result.why)
	}
	if _, held := s.parked.record(want.ID); held {
		t.Fatal("successful wake left its record offered in memory")
	}
	if got := loadParkBook(parkBookPath(socket)); len(got) != 0 {
		t.Fatalf("successful wake left park book %+v, want empty", got)
	}
	d.stop(t)
}

// A successful wake whose durable deletion fails must keep the exact old
// generation hidden and supervised after the resumed process ends. A later
// lifecycle event retries the repaired write before empty exit, so restart can
// never resurrect the stale row.
func TestFailedParkDeletionStaysSupervisedUntilEventfulRetry(t *testing.T) {
	fakeClaudeOnPath(t, "")
	brokenPsOnPath(t, psPartial)
	socket := tempSocket(t)
	s := newServer(socket)
	want := durableParkedRecord(t)
	if err := s.parked.add(want); err != nil {
		t.Fatalf("write initial park record: %v", err)
	}
	goodParkPath := s.parked.path
	d := startControlledServer(t, s)
	c := attach(t, socket)

	// Only commit writes through the broken path. The initial durable file at
	// goodParkPath remains the stale generation a successor would otherwise
	// load after this daemon exits.
	s.parked.mu.Lock()
	s.parked.path = filepath.Join(t.TempDir(), "missing", parkBookName)
	s.parked.mu.Unlock()
	if result := wakeOutcome(c, want.ID); !result.woke {
		t.Fatalf("wake before delete failure was refused: %s", result.why)
	}
	if got := loadParkBook(goodParkPath); len(got) != 1 || !sameParkedRecord(got[0], want) {
		t.Fatalf("failed commit did not leave the old durable generation for the test: %+v", got)
	}
	if got := s.parked.records(); len(got) != 0 {
		t.Fatalf("failed deletion re-offered hidden generation in current status: %+v", got)
	}

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: want.ID})
	if row := c.awaitSettled(want.ID); row.State != rpc.StateEnded {
		t.Fatalf("resumed process settled as %q, want an ordinary ending", row.State)
	}
	c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: want.ID})
	if why := c.awaitErrorFor(want.ID); !strings.Contains(why, "unknown session") {
		t.Fatalf("hidden pending deletion wake refusal = %q, want no stale offer", why)
	}

	c.close()
	waitForServerClients(t, s, 0)
	s.reconsiderEmptyExit()
	select {
	case <-s.quit:
		t.Fatal("empty exit committed after failed durable deletion; restart can resurrect the stale park record")
	default:
	}

	// Repair the write boundary. A new real client's final disconnect is the
	// eventful retry; no timer or polling loop is involved.
	s.parked.mu.Lock()
	s.parked.path = goodParkPath
	s.parked.mu.Unlock()
	retry := attach(t, socket)
	retry.close()
	if !d.waitForExitWithin(t, 3*time.Second) {
		t.Fatal("daemon did not retry the repaired pending deletion on the next client lifecycle boundary")
	}
	if got := loadParkBook(goodParkPath); len(got) != 0 {
		t.Fatalf("eventful retry left stale durable generation %+v", got)
	}
	assertNoRecordAfterRestart(t, socket, want.ID)
}

// Context cancellation is a lifecycle boundary too. With zero clients there
// is no later dropClient to retry a repaired pending deletion, so shutdown must
// do it after tracked finalizers settle and before Serve releases the listener.
func TestZeroClientServeShutdownRetriesPendingParkDeletion(t *testing.T) {
	socket := tempSocket(t)
	s := newServer(socket)
	want, goodParkPath := pendingParkDeletion(t, s)
	d := startControlledServer(t, s)
	s.mu.Lock()
	clients := len(s.clients)
	s.mu.Unlock()
	if clients != 0 {
		t.Fatalf("zero-client shutdown fixture has %d clients", clients)
	}
	// TestFailedParkDeletionStaysSupervisedUntilEventfulRetry drives the real
	// wake, ordinary end, and last-client half. This fixture carries that exact
	// repaired pending state into a real Serve cancellation, where shutdown is
	// now the only remaining lifecycle boundary.
	d.stop(t)
	if got := loadParkBook(goodParkPath); len(got) != 0 {
		t.Fatalf("zero-client context shutdown left stale durable generation %+v", got)
	}
	assertNoRecordAfterRestart(t, socket, want.ID)
}

// A newer park generation supersedes an older pending deletion. Its own first
// publication fails too; the repaired retry must publish R2, make its in-memory
// row durable, and clear supervision without deleting or hiding it.
func TestNewParkGenerationSupersedesPendingDeletion(t *testing.T) {
	socket := tempSocket(t)
	s := newServer(socket)
	old := durableParkedRecord(t)
	if err := s.parked.add(old); err != nil {
		t.Fatalf("write old park record: %v", err)
	}
	reserved, ok, err := s.parked.reserve(old.ID)
	if err != nil || !ok {
		t.Fatalf("reserve old generation = %+v, %v, %v", reserved, ok, err)
	}
	goodParkPath := s.parked.path
	s.parked.mu.Lock()
	s.parked.path = filepath.Join(t.TempDir(), "missing", parkBookName)
	s.parked.mu.Unlock()
	if err := s.parked.commit(reserved); err == nil {
		t.Fatal("commit through missing parent unexpectedly succeeded")
	}

	newSeed := old
	newSeed.Label = "new park generation"
	newSeed.Dir = t.TempDir()
	a := parkedAgentRow(newSeed, newSeed.Name)
	if !s.register(a) {
		t.Fatal("register newer in-memory park generation")
	}
	s.completePark(a, nil)
	newer, held := s.parked.record(old.ID)
	if !held || newer == old || !newer.Parked.After(old.Parked) {
		t.Fatalf("new park generation = %+v, %v; want a newer exact record than %+v", newer, held, old)
	}
	select {
	case <-s.quit:
		t.Fatal("failed R2 publication lost supervision while old deletion remained pending")
	default:
	}
	s.parked.mu.Lock()
	s.parked.path = goodParkPath
	s.parked.mu.Unlock()
	s.reconsiderEmptyExit()
	select {
	case <-s.quit:
	default:
		t.Fatal("successful retry of R2 left superseded R pending")
	}
	assertExactRecord(t, s, newer)
	assertExactRecordAfterRestart(t, socket, newer)
}

func TestFrameQuitClearsPendingParkDeletion(t *testing.T) {
	socket := tempSocket(t)
	s := newServer(socket)
	_, goodParkPath := pendingParkDeletion(t, s)
	s.beginQuit(quitStop)
	if err := s.shutdown(); err != nil {
		t.Fatalf("FrameQuit shutdown: %v", err)
	}
	if s.parked.hasPending() {
		t.Fatal("FrameQuit left hidden pending deletion in memory")
	}
	if got := loadParkBook(goodParkPath); len(got) != 0 {
		t.Fatalf("FrameQuit left stale durable generation %+v", got)
	}
}

func TestParkAllShutdownRetriesPendingParkDeletion(t *testing.T) {
	socket := tempSocket(t)
	s := newServer(socket)
	_, goodParkPath := pendingParkDeletion(t, s)
	s.beginQuit(quitPark)
	if err := s.shutdown(); err != nil {
		t.Fatalf("FrameParkAll shutdown: %v", err)
	}
	if s.parked.hasPending() {
		t.Fatal("FrameParkAll left repaired pending deletion in memory")
	}
	if got := loadParkBook(goodParkPath); len(got) != 0 {
		t.Fatalf("FrameParkAll shutdown left stale durable generation %+v", got)
	}
}

func pendingParkDeletion(t *testing.T, s *server) (parkedRecord, string) {
	t.Helper()
	want := durableParkedRecord(t)
	if err := s.parked.add(want); err != nil {
		t.Fatalf("write old park record: %v", err)
	}
	reserved, ok, err := s.parked.reserve(want.ID)
	if err != nil || !ok {
		t.Fatalf("reserve old generation = %+v, %v, %v", reserved, ok, err)
	}
	goodParkPath := s.parked.path
	s.parked.mu.Lock()
	s.parked.path = filepath.Join(t.TempDir(), "missing", parkBookName)
	s.parked.mu.Unlock()
	if err := s.parked.commit(reserved); err == nil {
		t.Fatal("commit through missing parent unexpectedly succeeded")
	}
	if !s.needsSupervision() {
		t.Fatal("failed deletion did not become supervised pending state")
	}
	s.parked.mu.Lock()
	s.parked.path = goodParkPath
	s.parked.mu.Unlock()
	return want, goodParkPath
}

// Unconditional commit deletion lets an older successful wake erase a newer
// completePark generation for the same id.
func TestOldWakeCommitDoesNotDeleteNewParkGeneration(t *testing.T) {
	socket := tempSocket(t)
	s := newServer(socket)
	old := durableParkedRecord(t)
	if err := s.parked.add(old); err != nil {
		t.Fatalf("write old park record: %v", err)
	}
	reserved, ok, err := s.parked.reserve(old.ID)
	if err != nil || !ok {
		t.Fatalf("reserve old record = %+v, %v, %v", reserved, ok, err)
	}

	newSeed := old
	newSeed.Label = "new park generation"
	newSeed.Dir = t.TempDir()
	a := parkedAgentRow(newSeed, newSeed.Name)
	a.mu.Lock()
	a.effort, a.model = newSeed.Effort, newSeed.Model
	a.budget, a.fallback = newSeed.MaxBudgetUSD, newSeed.FallbackModel
	a.mu.Unlock()
	s.completePark(a, nil)
	newer, held := s.parked.record(old.ID)
	if !held || newer == old || !newer.Parked.After(old.Parked) {
		t.Fatalf("completePark generation = %+v, %v; want a newer exact record than %+v", newer, held, old)
	}

	s.settleParkReservation(reserved, true, true)
	assertExactRecord(t, s, newer)
	assertExactRecordAfterRestart(t, socket, newer)
}

// Empty exit must supervise the process-to-durable-record transition rather
// than reuse liveCount's process-cap answer. A parked process is already gone,
// but until its old roster row is removed and its park record is durable the
// daemon is still the only owner that can finish that work.
func TestLastClientDisconnectWaitsForParkFinalization(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	s := newServer(socket)
	d := startControlledServer(t, s)
	c := attach(t, socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	a, ok := s.agent(idAlpha)
	if !ok {
		t.Fatal("spawned agent is missing from the server")
	}

	s.roster.mu.Lock()
	rosterLocked := true
	defer func() {
		if rosterLocked {
			s.roster.mu.Unlock()
		}
	}()
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	waitForAgentState(t, a, rpc.StateParked)
	if got := loadRoster(rosterPath(socket)); len(got) != 1 || got[0].ID != idAlpha {
		t.Fatalf("park finalizer passed the blocked roster storage: %+v", got)
	}

	c.close()
	waitForServerClients(t, s, 0)
	// dropClient also calls this. Calling it after the observable deletion
	// makes completion of the decision deterministic instead of sampling the
	// gap between delete(s.clients) and its following call.
	s.reconsiderEmptyExit()
	select {
	case <-s.quit:
		t.Fatal("empty exit committed while the parked generation was still blocked before durable finalization")
	default:
	}

	s.roster.mu.Unlock()
	rosterLocked = false
	if !d.waitForExitWithin(t, 3*time.Second) {
		t.Fatal("daemon did not exit after the parked generation became wakeable and durable")
	}
}

// A failed park-book write leaves this daemon's in-memory parked row as the
// only route to /resume. Empty exit must retain that row until a wake and a
// later successful park publish a durable generation.
func TestFailedParkPublicationKeepsDaemonForAnInMemoryWake(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	s := newServer(socket)
	goodParkPath := s.parked.path
	s.parked.mu.Lock()
	s.parked.path = filepath.Join(t.TempDir(), "missing", parkBookName)
	s.parked.mu.Unlock()
	d := startControlledServer(t, s)
	c := attach(t, socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	if row := c.awaitSettled(idAlpha); row.State != rpc.StateParked {
		t.Fatalf("failed publication settled the in-memory row as %q, want parked", row.State)
	}
	if got := loadParkBook(goodParkPath); len(got) != 0 {
		t.Fatalf("park publication fixture unexpectedly wrote durable records: %+v", got)
	}

	c.close()
	waitForServerClients(t, s, 0)
	s.reconsiderEmptyExit()
	select {
	case <-s.quit:
		t.Fatal("empty exit discarded the only in-memory route to a park whose durable write failed")
	default:
	}

	// Repair the storage boundary, then prove the retained in-memory row can
	// really wake through the client surface. Its next successful park may be
	// left to a new daemon, so empty exit becomes eligible again.
	s.parked.mu.Lock()
	s.parked.path = goodParkPath
	s.parked.mu.Unlock()
	waker := attach(t, socket)
	if result := wakeOutcome(waker, idAlpha); !result.woke {
		t.Fatalf("in-memory wake after failed publication was refused: %s", result.why)
	}
	waker.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	if row := waker.awaitSettled(idAlpha); row.State != rpc.StateParked {
		t.Fatalf("successful re-park settled as %q, want parked", row.State)
	}
	if got := loadParkBook(goodParkPath); len(got) != 1 || got[0].ID != idAlpha {
		t.Fatalf("successful re-park did not publish its durable generation: %+v", got)
	}
	waker.close()
	if !d.waitForExitWithin(t, 3*time.Second) {
		t.Fatal("daemon stayed alive after the retained row successfully re-parked durably")
	}
}

// A failed addition is dirty durable state, not merely an in-memory held row.
// ParkAll sees held IDs as protected, so its shutdown retry is the only writer
// that can publish this exact record before the daemon releases the listener.
func TestParkAllRetriesDirtyParkAdditionBeforeRestart(t *testing.T) {
	socket := tempSocket(t)
	s := newServer(socket)
	_, want, goodParkPath := dirtyParkAddition(t, s, idAlpha, "alex")
	if got := loadParkBook(goodParkPath); len(got) != 0 {
		t.Fatalf("failed-add fixture unexpectedly wrote durable records: %+v", got)
	}

	s.beginQuit(quitPark)
	if err := s.shutdown(); err != nil {
		t.Fatalf("FrameParkAll shutdown: %v", err)
	}
	assertExactRecord(t, s, want)
	assertExactRecordAfterRestart(t, socket, want)
}

// A successful add for B rewrites the whole book and incidentally includes
// dirty A, but only an eventful retry can publish that fact back to A's agent.
// Clearing every dirty marker in add(B) leaves A nondurable in memory forever.
func TestIncidentalFullBookWriteDoesNotStrandDirtyParkAgent(t *testing.T) {
	s := newServer(tempSocket(t))
	_, dirty, goodParkPath := dirtyParkAddition(t, s, idAlpha, "alex")
	other := durableParkedRecord(t)
	other.ID = idBeta
	other.Name = "sydney"
	other.Dir = t.TempDir()
	if err := s.parked.add(other); err != nil {
		t.Fatalf("write unrelated durable record: %v", err)
	}
	if !s.parked.hasPending() {
		t.Fatal("successful full-book write for another id silently cleared dirty park addition before its agent was marked durable")
	}
	if got := loadParkBook(goodParkPath); len(got) != 2 {
		t.Fatalf("incidental full-book write = %+v, want dirty and unrelated records", got)
	}

	s.reconsiderEmptyExit()
	select {
	case <-s.quit:
	default:
		t.Fatal("eventful retry wrote dirty agent but left it nondurable under empty-exit supervision")
	}
	if s.parked.hasPending() {
		t.Fatal("successful eventful retry left dirty park addition pending")
	}
	got, held := s.parked.record(dirty.ID)
	if !held || !sameParkedRecord(got, dirty) {
		t.Fatalf("dirty generation after retry = %+v, %v; want exact %+v", got, held, dirty)
	}
}

// retryPending may make old R durable while a wake successor occupies the map.
// If Start then fails, withdraw restores the old nondurable agent after pending
// was cleared. The failure outcome must acknowledge exact durable R on that
// restored pointer or empty exit remains pinned forever.
func TestWakeRollbackAcknowledgesDurabilityPublishedWhileSuccessorWasAdmitted(t *testing.T) {
	s := newServer(tempSocket(t))
	old, rec, _ := dirtyParkAddition(t, s, idAlpha, "alex")
	reserved, ok, err := s.parked.reserve(rec.ID)
	if err != nil || !ok || !sameParkedRecord(reserved, rec) {
		t.Fatalf("reserve dirty generation = %+v, %v, %v; want exact %+v", reserved, ok, err, rec)
	}
	successor := newAgent(rec.ID, rec.Name, rec.Label, rec.Dir, "",
		core.NewSession(core.Config{SessionID: rec.ID}), func() {})
	if !s.replaceParked(successor, old) {
		t.Fatal("admit wake successor before Start failure")
	}
	outcome := s.parkLaunchOutcome(rec, true)

	// The retry lands after admission but before Start reports failure. It sees
	// the successor, which is not the parked generation it just published.
	s.reconsiderEmptyExit()
	if s.parked.hasPending() {
		t.Fatal("retry did not publish dirty old generation")
	}
	s.withdraw(successor, old, nil)
	outcome(false) // release reservation after the old pointer is restored
	if got, held := s.agent(rec.ID); !held || got != old {
		t.Fatalf("failed wake restored agent %p, %v; want old parked pointer %p", got, held, old)
	}

	s.reconsiderEmptyExit()
	select {
	case <-s.quit:
	default:
		t.Fatal("withdraw restored exact durable park generation as nondurable and pinned empty exit")
	}
}

// An acknowledgement for R must not make a different parked R2 durable merely
// because it currently occupies the same session-id row.
func TestDurabilityAcknowledgementRequiresExactParkGeneration(t *testing.T) {
	for _, tc := range []struct {
		name string
		zero bool
	}{
		{name: "same nonzero timestamp"},
		{name: "same legacy zero timestamp", zero: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(tempSocket(t))
			old := durableParkedRecord(t)
			if tc.zero {
				old.Parked = time.Time{}
			}
			goodParkPath := s.parked.path
			s.parked.mu.Lock()
			s.parked.path = filepath.Join(t.TempDir(), "missing", parkBookName)
			s.parked.mu.Unlock()
			if err := s.parked.add(old); err == nil {
				t.Fatal("dirty old generation unexpectedly reached disk")
			}
			s.parked.mu.Lock()
			s.parked.path = goodParkPath
			s.parked.mu.Unlock()

			newer := old // deliberately the same ID and Parked timestamp
			newer.Label = "different generation"
			newer.Dir = t.TempDir()
			newer.Model = "different-model"
			newer.MaxBudgetUSD = "99.00"
			newer.FallbackModel = "different-fallback"
			a := parkedAgentRow(newer, newer.Name)
			a.mu.Lock()
			a.parkDurable = false
			a.mu.Unlock()
			if !s.register(a) {
				t.Fatal("register newer parked generation")
			}

			s.reconsiderEmptyExit()
			select {
			case <-s.quit:
				t.Fatal("old generation durability acknowledgement marked different same-timestamp park generation durable")
			default:
			}
		})
	}
}

func dirtyParkAddition(t *testing.T, s *server, id, name string) (*agent, parkedRecord, string) {
	t.Helper()
	a := newAgent(id, name, "dirty park", t.TempDir(), "",
		core.NewSession(core.Config{SessionID: id}), func() {})
	a.beginPark()
	a.finish(nil)
	if !s.register(a) {
		t.Fatalf("register dirty park agent %s", id)
	}
	goodParkPath := s.parked.path
	s.parked.mu.Lock()
	s.parked.path = filepath.Join(t.TempDir(), "missing", parkBookName)
	s.parked.mu.Unlock()
	s.completePark(a, nil)
	want, held := s.parked.record(id)
	if !held {
		t.Fatalf("failed add did not retain in-memory record for %s", id)
	}
	s.parked.mu.Lock()
	s.parked.path = goodParkPath
	s.parked.mu.Unlock()
	return a, want, goodParkPath
}

// OS proof may race ParkAll after beginReclaim but before retire observes the
// process ending. ParkAll's label must survive that barrier: reclaim completes
// the process idempotently, and completePark records the exact resumable row.
func TestParkAllPreservesIntentDuringOSConfirmedReclaim(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	s := newServer(socket)
	d := startControlledServer(t, s)
	c := attach(t, socket)
	c.spawn(idAlpha, "alex")
	before := c.awaitState(idAlpha, rpc.StateIdle)
	a, held := s.agent(idAlpha)
	if !held {
		t.Fatal("spawned reclaim barrier agent is missing")
	}
	if !a.beginReclaim(errors.New("OS confirmed the process is gone")) {
		t.Fatal("could not establish OS-confirmed reclaim before retire")
	}
	if a.finished() {
		t.Fatal("reclaim barrier agent retired before ParkAll")
	}

	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	d.waitForExit(t)
	records := loadParkBook(parkBookPath(socket))
	if len(records) != 1 || records[0].ID != before.ID || records[0].Name != before.Name || records[0].Dir != before.Dir {
		t.Fatalf("park book after ParkAll during reclaim = %+v, want exact identity %+v", records, before)
	}
	assertExactRecordAfterRestart(t, socket, records[0])
}

// The post-grace decision used to check reclaiming and clear parking in two
// different critical sections. Blocking its own log line opens that exact gap:
// OS proof lands after the check and before kill. The final decision must see
// the proof atomically and preserve the exact park.
func TestPostGraceParkAllAtomicallyPreservesConcurrentReclaim(t *testing.T) {
	fakeClaudeOnPath(t, "deaf")
	socket := tempSocket(t)
	s := newServer(socket)
	d := startControlledServer(t, s)
	c := attach(t, socket)
	c.spawn(idAlpha, "alex")
	before := c.awaitState(idAlpha, rpc.StateIdle)
	a, held := s.agent(idAlpha)
	if !held {
		t.Fatal("spawned post-grace barrier agent is missing")
	}

	blocked := newBlockingWriter()
	t.Cleanup(blocked.release)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	log.SetOutput(blocked)
	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	select {
	case <-blocked.entered:
	case <-time.After(testTimeout):
		t.Fatal("shutdown never reached the post-grace kill decision")
	}
	if !a.beginReclaim(errors.New("OS proof landed in the post-grace decision")) {
		blocked.release()
		t.Fatal("could not place OS proof between post-grace check and kill")
	}
	blocked.release()
	d.waitForExit(t)

	records := loadParkBook(parkBookPath(socket))
	if len(records) != 1 || records[0].ID != before.ID || records[0].Name != before.Name || records[0].Dir != before.Dir {
		t.Fatalf("park book after post-grace reclaim race = %+v, want exact identity %+v", records, before)
	}
	assertExactRecordAfterRestart(t, socket, records[0])
}

func waitForAgentState(t *testing.T, a *agent, want string) {
	t.Helper()
	deadline := time.NewTimer(testTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for a.snapshot().State != want {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("agent never reached state %q; last snapshot: %+v", want, a.snapshot())
		}
	}
}

func waitForServerClients(t *testing.T, s *server, want int) {
	t.Helper()
	deadline := time.NewTimer(testTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		s.mu.Lock()
		got := len(s.clients)
		s.mu.Unlock()
		if got == want {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("server client count = %d, want %d", got, want)
		}
	}
}

// Publishing parked before the old generation has finished its roster and
// park-book writes lets a wake replace it while those writes are still in
// flight. The old finalizer can then remove the successor's roster ownership
// or overwrite the record written by a successor that has already re-parked.
func TestParkIsNotWakeableUntilItsOldGenerationFinishesStorage(t *testing.T) {
	fakeClaudeOnPath(t, "")
	// A well-formed non-empty listing with no session marker makes resumeSafe's
	// answer deterministic in the sandbox and lets the old ordering reach the
	// real process start instead of being refused by the host's ps policy.
	brokenPsOnPath(t, psPartial)
	socket := tempSocket(t)
	s := newServer(socket)
	// Keep empty-exit from committing while this test deliberately holds the
	// old generation between its process transition and storage finalization.
	s.addClient(newClient(nil))

	old := newAgent(idAlpha, "alex", "old generation", t.TempDir(), "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	old.beginPark()
	old.finish(nil)
	if _, err := s.names.claim(old.name); err != nil {
		t.Fatalf("claim old generation name: %v", err)
	}
	if !s.register(old) {
		t.Fatal("register old generation")
	}
	if err := s.roster.add(old.rosterRecord(4242)); err != nil {
		t.Fatalf("record old generation: %v", err)
	}
	// shutdown's bookParked may already have installed this generation before
	// retire reaches completePark. Keeping that real shape in the fixture makes
	// the publish-first mutation perform an observable durable overwrite after
	// it starts the successor, instead of failing only on an in-memory flag.
	seed := parkedRecord{
		ID: old.id, Name: old.name, Label: old.label, Dir: old.dir,
		Parked: time.Date(2026, time.August, 23, 12, 34, 56, 789, time.UTC),
	}
	if err := s.parked.add(seed); err != nil {
		t.Fatalf("seed the shutdown-written park generation: %v", err)
	}

	// completePark changes the reported state first, then reaches this lock to
	// remove the old PID. Holding it gives the concurrent wake the exact old
	// publish-first window without sleeps or a production test hook.
	s.roster.mu.Lock()
	rosterLocked := true
	defer func() {
		if rosterLocked {
			s.roster.mu.Unlock()
		}
	}()
	finalized := make(chan struct{})
	go func() {
		s.completePark(old, nil)
		close(finalized)
	}()

	stateDeadline := time.NewTimer(testTimeout)
	defer stateDeadline.Stop()
	stateTick := time.NewTicker(time.Millisecond)
	defer stateTick.Stop()
	for old.snapshot().State != rpc.StateParked {
		select {
		case <-stateTick.C:
		case <-stateDeadline.C:
			t.Fatal("old generation never reached the parked state candidate before its blocked roster removal")
		}
	}
	if got := loadRoster(rosterPath(socket)); len(got) != 1 || got[0].PID != 4242 {
		t.Fatalf("old roster storage changed before the barrier: %+v", got)
	}

	premature := newClient(nil)
	prematureDone := make(chan struct{})
	go func() {
		s.unpark(context.Background(), premature, rpc.Frame{Kind: rpc.FrameWake, SessionID: idAlpha})
		close(prematureDone)
	}()

	// A correct wake is refused before it reaches storage. The old ordering
	// replaces the row and starts a process, then blocks trying to record that
	// successor behind the same roster lock.
	var startedEarly *agent
	wakeDeadline := time.NewTimer(testTimeout)
	defer wakeDeadline.Stop()
	wakeTick := time.NewTicker(time.Millisecond)
	defer wakeTick.Stop()
observeWake:
	for {
		select {
		case <-prematureDone:
			break observeWake
		case <-wakeTick.C:
			if got, ok := s.agent(idAlpha); ok && got != old && got.sess.Pgid() != 0 {
				startedEarly = got
				break observeWake
			}
		case <-wakeDeadline.C:
			t.Fatal("wake neither returned a refusal nor published the process it started")
		}
	}

	s.roster.mu.Unlock()
	rosterLocked = false
	select {
	case <-finalized:
	case <-time.After(testTimeout):
		t.Fatal("old generation did not finish after the roster barrier was released")
	}
	select {
	case <-prematureDone:
	case <-time.After(testTimeout):
		t.Fatal("premature wake did not finish after the roster barrier was released")
	}
	if startedEarly != nil {
		stale, held := s.parked.record(idAlpha)
		startedEarly.kill()
		if !waitFor(&s.wg, testTimeout) {
			t.Fatal("early successor did not retire after cleanup")
		}
		if !held || sameParkedRecord(stale, seed) || !stale.Parked.After(seed.Parked) {
			t.Fatalf("wake started successor pgid %d before finalization, but the delayed old storage step did not overwrite seed %+v: got %+v, %v",
				startedEarly.sess.Pgid(), seed, stale, held)
		}
		t.Fatalf("old finalizer overwrote durable generation %+v with %+v after successor pgid %d was already live",
			seed, stale, startedEarly.sess.Pgid())
	}
	if why := firstRefusal(t, premature).Text; !strings.Contains(why, "not parked") {
		t.Fatalf("wake during finalization was refused with %q, want the not-parked boundary named", why)
	}
	if got, ok := s.agent(idAlpha); !ok || got != old {
		t.Fatalf("refused wake changed the old generation row to %p, want %p", got, old)
	}

	// Once the finalizer publishes wakeability, no old storage step remains.
	// Wake a real successor, prove its live PGID is the roster owner, then park
	// it again and prove the exact newer generation survives a daemon restart.
	oldRecord, held := s.parked.record(idAlpha)
	if !held {
		t.Fatal("finalized old generation is missing from the park book")
	}
	waker := newClient(nil)
	s.unpark(context.Background(), waker, rpc.Frame{Kind: rpc.FrameWake, SessionID: idAlpha})
	successor, ok := s.agent(idAlpha)
	if !ok || successor == old || successor.sess.Pgid() == 0 {
		why := ""
		select {
		case f := <-waker.out:
			why = f.Text
		default:
		}
		t.Fatalf("finalized generation did not wake a live successor (agent=%p old=%p refusal=%q)", successor, old, why)
	}
	pgid := successor.sess.Pgid()
	if got := loadRoster(rosterPath(socket)); len(got) != 1 || got[0].ID != idAlpha || got[0].PID != pgid {
		successor.kill()
		t.Fatalf("live successor pgid %d does not own the exact roster row: %+v", pgid, got)
	}

	s.submit(newClient(nil), rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	parkDeadline := time.NewTimer(testTimeout)
	defer parkDeadline.Stop()
	parkTick := time.NewTicker(time.Millisecond)
	defer parkTick.Stop()
	for !successor.isParked() {
		select {
		case <-parkTick.C:
		case <-parkDeadline.C:
			successor.kill()
			t.Fatal("successor never completed its re-park generation")
		}
	}
	newer, held := s.parked.record(idAlpha)
	if !held || !newer.Parked.After(oldRecord.Parked) {
		t.Fatalf("successor park generation = %+v, %v; want a newer exact record than %+v", newer, held, oldRecord)
	}
	if got := loadRoster(rosterPath(socket)); len(got) != 0 {
		t.Fatalf("re-parked successor left live roster ownership behind: %+v", got)
	}
	assertExactRecord(t, s, newer)
	assertExactRecordAfterRestart(t, socket, newer)
}

// Failure restoration must not make quitStop reversible: shutdown clears the
// reservation and any restored row before a successor can read the book.
func TestWakeRacingFrameQuitStillClearsParkBook(t *testing.T) {
	for _, source := range []string{"record-only", "in-memory"} {
		t.Run(source, func(t *testing.T) {
			fakeClaudeOnPath(t, "")
			socket := tempSocket(t)
			s := newServer(socket)
			want := durableParkedRecord(t)
			installDurableParkSource(t, s, want, source)
			d := startControlledServer(t, s)
			waker := attach(t, socket)
			quitter := attach(t, socket)

			s.admitMu.Lock()
			locked := true
			defer func() {
				if locked {
					s.admitMu.Unlock()
				}
			}()
			waker.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: want.ID})
			waitForParkReservation(t, s, want.ID)
			quitter.send(rpc.Frame{Kind: rpc.FrameQuit})
			<-s.quit
			s.admitMu.Unlock()
			locked = false
			d.waitForExit(t)

			assertNoRecordAfterRestart(t, socket, want.ID)
		})
	}
}

// A failed exec is the non-racing half of the same promise: no process ever
// ran, so removing the only durable route back cannot be committed.
func TestWakeLaunchFailureRestoresExactParkRecord(t *testing.T) {
	for _, source := range []string{"record-only", "in-memory"} {
		t.Run(source, func(t *testing.T) {
			onlyPsOnPath(t)
			socket := tempSocket(t)
			s := newServer(socket)
			want := durableParkedRecord(t)
			installDurableParkSource(t, s, want, source)
			d := startControlledServer(t, s)
			c := attach(t, socket)

			result := wakeOutcome(c, want.ID)
			if result.woke {
				t.Fatalf("wake unexpectedly started a process: %+v", result.row)
			}
			assertExactRecord(t, s, want)
			d.stop(t)
		})
	}
}

func durableParkedRecord(t *testing.T) parkedRecord {
	t.Helper()
	return parkedRecord{
		ID: idAlpha, Name: "alex", Label: "DEV-16", Dir: t.TempDir(),
		Parked:        time.Date(2026, time.August, 23, 12, 34, 56, 789, time.UTC),
		Effort:        core.EffortMax,
		Model:         "sonnet",
		MaxBudgetUSD:  "12.50",
		FallbackModel: "opus,sonnet",
	}
}

func installDurableParkSource(t *testing.T, s *server, rec parkedRecord, source string) {
	t.Helper()
	if err := s.parked.add(rec); err != nil {
		t.Fatalf("write park record: %v", err)
	}
	if source != "in-memory" {
		return
	}
	a := parkedAgentRow(rec, rec.Name)
	a.mu.Lock()
	a.effort, a.model = rec.Effort, rec.Model
	a.budget, a.fallback = rec.MaxBudgetUSD, rec.FallbackModel
	a.mu.Unlock()
	if !s.register(a) {
		t.Fatal("register in-memory parked row")
	}
}

func startControlledServer(t *testing.T, s *server) *testDaemon {
	t.Helper()
	ln, err := listen(s.socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		runErr := s.run(ctx, ln)
		shutdownErr := s.shutdown()
		closeErr := ln.Close()
		errCh <- errors.Join(runErr, shutdownErr, closeErr)
	}()
	d := &testDaemon{socket: s.socket, err: errCh, cancel: cancel}
	t.Cleanup(func() { d.stop(t) })
	d.waitForListening(t)
	return d
}

func waitForParkReservation(t *testing.T, s *server, id string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		s.parked.mu.Lock()
		_, reserved := s.parked.reserved[id]
		s.parked.mu.Unlock()
		if reserved {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("park record %s was never reserved, so the wake did not reach launch", id)
}

// seedParkTranscript makes transcriptPath(id) succeed so parkedStatuses' PR #109
// filter (a record with no transcript on disk is not offered) keeps a park record
// whose fake claude wrote no real transcript. transcriptPath scans every slug
// under ProjectsDir for <id>.jsonl, so one seed directory under a temp
// WAKE_PROJECTS serves every id a test parks. A real parked session always has a
// transcript; only the fake-claude harness needs this.
func seedParkTranscript(t *testing.T, id string) {
	t.Helper()
	projects := os.Getenv("WAKE_PROJECTS")
	if projects == "" {
		projects = t.TempDir()
		t.Setenv("WAKE_PROJECTS", projects)
	}
	writeTranscript(t, projects, "seeded", id, "/seeded")
}

func assertExactRecordAfterRestart(t *testing.T, socket string, want parkedRecord) {
	t.Helper()
	seedParkTranscript(t, want.ID)
	second := startDaemonOn(t, socket)
	c := attach(t, socket)
	if row := sessionRow(c.status(), want.ID); row.State != rpc.StateParked {
		t.Fatalf("restart reports %+v, want exact parked session %s available", row, want.ID)
	}
	got := loadParkBook(parkBookPath(socket))
	if len(got) != 1 || !sameParkedRecord(got[0], want) {
		t.Fatalf("park book after restart = %+v, want exact record %+v", got, want)
	}
	second.stop(t)
}

func assertExactRecord(t *testing.T, s *server, want parkedRecord) {
	t.Helper()
	got, ok := s.parked.record(want.ID)
	if !ok || !sameParkedRecord(got, want) {
		t.Fatalf("park record = %+v, %v; want exact %+v", got, ok, want)
	}
	onDisk := loadParkBook(parkBookPath(s.socket))
	if len(onDisk) != 1 || !sameParkedRecord(onDisk[0], want) {
		t.Fatalf("park book on disk = %+v, want exact %+v", onDisk, want)
	}
}

func sameParkedRecord(a, b parkedRecord) bool {
	aTime, bTime := a.Parked, b.Parked
	a.Parked, b.Parked = time.Time{}, time.Time{}
	return a == b && aTime.Equal(bTime)
}

func assertNoRecordAfterRestart(t *testing.T, socket, id string) {
	t.Helper()
	second := startDaemonOn(t, socket)
	c := attach(t, socket)
	if row := sessionRow(c.status(), id); row.State != "" {
		t.Fatalf("restart after FrameQuit reports %+v, want no parked session", row)
	}
	if got := loadParkBook(parkBookPath(socket)); len(got) != 0 {
		t.Fatalf("park book after FrameQuit and restart = %+v, want empty", got)
	}
	second.stop(t)
}
