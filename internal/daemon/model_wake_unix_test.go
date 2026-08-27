//go:build unix

package daemon

// The model from the spawn frame to the agent's record, and back out of a park.
//
// Asserted through parked.json rather than through a field: the book is what a
// successor daemon reads, so it is the observable that decides whether a woken
// session comes back as what it was started as. Effort's own tests take the
// same route for the same reason.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// parkedModel is what the book beside this daemon's socket says one session
// runs as.
func parkedModel(t *testing.T, d *testDaemon, id string) string {
	t.Helper()
	for _, rec := range newParkBook(parkBookPath(d.socket)).records() {
		if rec.ID == id {
			return rec.Model
		}
	}
	t.Fatalf("no park book row for %s", id)
	return ""
}

// The spawn frame is the producer core.Config.Model never had: the field has
// had an emitter in argv.go and nothing setting it since it was declared.
func TestASpawnCarriesItsModelOntoTheConfig(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), Model: "opus"})
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if got := parkedModel(t, d, idAlpha); got != "opus" {
		t.Errorf("the session parked as %q, want the model the spawn frame named", got)
	}
}

// A model does not survive a park by accident: a wake is served from the
// daemon's own row and the park book, and the client that chose it is gone.
func TestAParkedSessionComesBackOnTheModelItWasStartedWith(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), Model: "opus"})
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if got := wakeOutcome(c, idAlpha); !got.woke {
		t.Fatalf("the parked session was not woken: %s", got.why)
	}

	// Parked a second time, which is what makes this about the wake rather
	// than about the first record: the row now on disk was written by the
	// woken session out of the config unpark built for it.
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if got := parkedModel(t, d, idAlpha); got != "opus" {
		t.Errorf("a woken session runs as %q, want the %q it was started with", got, "opus")
	}
}

// Absent is not a model. It means Wake chose none, --model is left off the
// argv entirely, and claude applies its own default - the meaning "" already
// carries for effort. It is not refused either: absent and invalid are
// different, and this is the floor under that distinction.
func TestASpawnWithNoModelPutsNoFlagOnTheArgv(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir()})
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if got := parkedModel(t, d, idAlpha); got != "" {
		t.Errorf("a spawn that named no model recorded %q", got)
	}
}
