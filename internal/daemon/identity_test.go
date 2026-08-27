package daemon

// Naming, through a real daemon on a real socket. names_test.go holds the
// registry to its own contract; this holds the daemon to using it - which is
// the half that was missing when every session on the machine was called alex.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// spawnFor starts a session and returns the row the daemon reports for it, so
// a test can assert on the name the daemon chose rather than on the one it was
// given.
//
// It waits for a status *reply*, which is the spawn's own confirmation, and
// excludes an ended row for the reason harness_test.go's spawn does: a
// remembered ending for this id would satisfy a bare id match and return before
// anything had started.
func spawnFor(c *testClient, id, requested, dir string) rpc.SessionStatus {
	c.t.Helper()

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id, Text: requested, Dir: dir})
	var got rpc.SessionStatus
	c.await("the spawned session in a status reply", func(f rpc.Frame) bool {
		if f.Kind != rpc.FrameStatusReply || f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == id && s.State != rpc.StateEnded {
				got = s
				return true
			}
		}
		return false
	})
	return got
}

// awaitRefusal waits for the daemon to say no to something, and returns why.
func awaitRefusal(c *testClient, id string) string {
	c.t.Helper()
	f := c.await("a refusal for "+id, func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == id
	})
	return f.Text
}

// The defect this whole change exists for: two independent sessions used to be
// called alex, because the name was a constant in the client.
func TestTwoSessionsAreNotGivenTheSameName(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	first := spawnFor(c, idAlpha, "", "")
	second := spawnFor(c, idBeta, "", "")

	switch {
	case first.Name == "":
		t.Fatal("the first session was left unnamed")
	case second.Name == "":
		t.Fatal("the second session was left unnamed")
	case first.Name == second.Name:
		t.Fatalf("two live sessions are both called %q", first.Name)
	}
	if !pooled(first.Name) || !pooled(second.Name) {
		t.Errorf("names %q and %q, want two drawn from the pool", first.Name, second.Name)
	}
}

// Naming one on purpose. `wake new sydney` is the client half; this is what it
// reaches.
func TestASessionCanBeNamedAtCreation(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	if got := spawnFor(c, idAlpha, "Sydney", ""); got.Name != "sydney" {
		t.Errorf("a session asked for as Sydney is called %q", got.Name)
	}
}

// The registry is the daemon's, so the refusal is the daemon's too - and it has
// to happen before a process exists. A name collision that cost a claude
// process, started and then torn down, would be a spawn the user did not ask
// for and a transcript on disk for a session that never ran.
//
// Mutation check: claiming the name after sess.Start leaves this failing at
// "the refused spawn left a session behind".
func TestANameAlreadyInUseIsRefusedBeforeAnythingIsStarted(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnFor(c, idAlpha, "sydney", "")

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idBeta, Text: "sydney"})
	why := awaitRefusal(c, idBeta)
	if !strings.Contains(why, "sydney") {
		t.Errorf("the refusal does not name the name: %q", why)
	}

	for _, s := range c.status().Sessions {
		if s.ID == idBeta {
			t.Fatalf("the refused spawn left a session behind: %+v", s)
		}
	}
}

// Identity is checked before display. A client that re-sent its own spawn -
// the same id and the same name - has a duplicate *id*, and being told its name
// was taken would send it looking for the wrong thing: the name is taken by
// itself.
//
// Mutation check: dropping maySpawn's holds check leaves this failing at "the
// refusal is about the name rather than the id". register still refuses the
// spawn, so nothing unsafe happens - only the reason is wrong, which is exactly
// the kind of defect nothing else here would have caught.
func TestRespawningAnIdUnderItsOwnNameBlamesTheIdRatherThanTheName(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnFor(c, idAlpha, "sydney", "")

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "sydney"})
	why := awaitRefusal(c, idAlpha)
	if !strings.Contains(why, "already exists") {
		t.Errorf("the refusal is about the name rather than the id: %q", why)
	}
	if n := len(live(c.status())); n != 1 {
		t.Errorf("status holds %d sessions, want the one that was already there", n)
	}
}

// A name is free again once its session ends, which is what keeps a daemon
// that has been up for a week from running out of names it is not using.
func TestANameIsReusableOnceItsSessionHasEnded(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnFor(c, idAlpha, "sydney", "")
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if got := spawnFor(c, idBeta, "sydney", ""); got.Name != "sydney" {
		t.Errorf("a name held only by an ended session was not reusable: the new session is %q", got.Name)
	}
}

// A name is claimed before the process starts, so a spawn that fails to start
// has to give it back. Without that, every failed spawn burns a name until the
// daemon is restarted - and `wake new sydney`, tried twice after a typo in the
// directory, would be refused the second time for a session that never existed.
//
// The failure is reached through a directory that is absolute (so maySpawn
// admits it) and is not there (so the exec fails), which is the shape of a
// client started in a directory that has since been deleted.
//
// Mutation check: dropping the release on the start-failure path leaves this
// failing at "a name was burned by a spawn that never started".
func TestANameIsGivenBackWhenTheProcessFailsToStart(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	gone := filepath.Join(t.TempDir(), "deleted", "repo")
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "sydney", Dir: gone})
	awaitRefusal(c, idAlpha)

	if got := spawnFor(c, idBeta, "sydney", ""); got.Name != "sydney" {
		t.Errorf("a name was burned by a spawn that never started: asking for sydney again got %q", got.Name)
	}
}

// The name has to reach the process, not merely the daemon's own map. Spec §7
// routes on `@name`, so a name the agent has never heard of is a name nothing
// can be addressed to.
func TestTheAssignedNameReachesTheAgent(t *testing.T) {
	fakeClaudeOnPath(t, "name")
	d := startDaemon(t)
	c := attach(t, d.socket)

	got := spawnFor(c, idAlpha, "", "")
	if got.Name == "" {
		t.Fatal("the session was left unnamed, so there is nothing to look for on the command line")
	}
	c.awaitEvent(idAlpha, "name: "+got.Name)
}

// A name a person typed reaches the same place, and this is the one that could
// have been dropped silently: the daemon used to pass Frame.Text straight
// through, so a build that claimed a name and then spawned with the requested
// one would look right in `wake status` and be wrong on the wire.
func TestAChosenNameReachesTheAgentToo(t *testing.T) {
	fakeClaudeOnPath(t, "name")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnFor(c, idAlpha, "SYDNEY", "")
	c.awaitEvent(idAlpha, "name: sydney")
}

// --- the label --------------------------------------------------------------

// The label the fleet is actually read by: which branch this agent is on.
func TestASessionIsLabelledWithTheBranchItWasStartedOn(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := repoAt(t, t.TempDir(), "ref: refs/heads/dev-5748\n")
	if got := spawnFor(c, idAlpha, "", dir); got.Label != "dev-5748" {
		t.Errorf("session label = %q, want the branch dev-5748", got.Label)
	}
}

// And it survives to disk, because `wake status` on a machine whose daemon died
// assembles its report from the roster and would otherwise lose half of every
// session's identity at the moment it is hardest to work out what is running.
func TestTheLabelIsWrittenDownWithTheSession(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := repoAt(t, t.TempDir(), "ref: refs/heads/dev-5748\n")
	want := spawnFor(c, idAlpha, "", dir)

	data, err := os.ReadFile(rosterPath(d.socket))
	if err != nil {
		t.Fatalf("read the roster: %v", err)
	}
	var recs []record
	if err := json.Unmarshal(data, &recs); err != nil {
		t.Fatalf("decode the roster %s: %v", data, err)
	}
	if len(recs) != 1 {
		t.Fatalf("the roster holds %d records, want 1: %s", len(recs), data)
	}
	if recs[0].Name != want.Name || recs[0].Label != want.Label {
		t.Errorf("the roster says %q/%q, want %q/%q", recs[0].Name, recs[0].Label, want.Name, want.Label)
	}
}

// A roster written before labels existed still loads. It is a bare JSON array
// on disk and the next daemon reads it to decide what to reap, so a format
// change that made an old file unreadable would orphan a live fleet.
func TestARosterWrittenWithoutALabelStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), rosterFileName)
	writeFile(t, path, `[{"id":"`+idAlpha+`","name":"alex","pid":4242,"started":"2026-08-08T00:00:00Z"}]`)

	recs := loadRoster(path)
	if len(recs) != 1 {
		t.Fatalf("loadRoster returned %d records, want 1", len(recs))
	}
	if recs[0].ID != idAlpha || recs[0].Name != "alex" || recs[0].PID != 4242 {
		t.Errorf("loadRoster returned %+v, want the record as written", recs[0])
	}
	if recs[0].Label != "" {
		t.Errorf("a record with no label decoded with one: %q", recs[0].Label)
	}
}

// A name the daemon cannot use is refused with a reason, rather than folded
// into something else. Somebody who typed `wake new "two words"` gets told.
func TestANameTheDaemonCannotUseIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "two words"})
	if why := awaitRefusal(c, idAlpha); !strings.Contains(why, "letters") {
		t.Errorf("the refusal does not say what a name may hold: %q", why)
	}
	for _, s := range c.status().Sessions {
		if s.ID == idAlpha {
			t.Fatalf("a spawn refused for its name started anyway: %+v", s)
		}
	}
}
