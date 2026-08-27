package daemon

// The model a session runs as, from the spawn frame to the argv and back out
// of a park.
//
// core.Config.Model has had an emitter in argv.go and no producer anywhere
// since it was declared. These are the producer's tests.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The book carries it for Effort's reason, and the argument is the same one:
// a model the operator chose and a wake dropped is a silent downgrade, and
// nothing about a name or a directory implies one.
func TestAParkedSessionKeepsTheModelItRanAs(t *testing.T) {
	path := filepath.Join(t.TempDir(), parkBookName)
	book := newParkBook(path)
	if err := book.add(parkedRecord{ID: idAlpha, Name: "alex", Dir: "/tmp/repo", Model: "opus"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the park book back: %v", err)
	}
	if !strings.Contains(string(data), `"model":"opus"`) {
		t.Errorf("the model is not on disk under the key another build would read:\n%s", data)
	}

	back := newParkBook(path)
	rows := back.records()
	if len(rows) != 1 || rows[0].Model != "opus" {
		t.Fatalf("the model did not survive the round trip: %+v", rows)
	}
}

// A session Wake chose no model for writes no key at all, so a book from this
// build is byte-identical to one from before the field existed.
func TestASessionWithNoModelWritesNoModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), parkBookName)
	if err := newParkBook(path).add(parkedRecord{ID: idAlpha, Name: "alex", Dir: "/tmp/repo"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "model") {
		t.Errorf("a session with no chosen model wrote the key anyway:\n%s", data)
	}
}

// A model survives the **daemon**, not just the park.
//
// This is the seam between two changes and the one place it could be lost. The
// park book stopped being restored into the fleet, so a session parked by ⌃Q
// comes back through unparkRecord - which builds its Config from a file on disk
// rather than from a row a previous daemon was holding. A field that unpark
// carries and unparkRecord forgets is a downgrade with nothing on screen saying
// so: the session comes back on claude's default model, answers perfectly well,
// and nobody finds out.
//
// TestAParkedSessionComesBackOnTheModelItWasStartedWith is the same claim inside
// one daemon's life, which is the other door and does not cover this one.
//
// Unlike effort there is no narrowing here: every non-empty model may go on a
// command line, because nothing can enumerate the set and a daemon-side
// allowlist would refuse names claude accepts.
func TestAModelSurvivesADaemonRestartAndNotOnlyAPark(t *testing.T) {
	for _, tc := range []struct{ name, stored string }{
		{"an alias", "opus"},
		{"a full name", "claude-fable-5"},
		{"a model this build has never heard of", "claude-not-invented-yet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeClaudeOnPath(t, "")
			socket := tempSocket(t)
			plantTranscript(t, idAlpha) // the session ran; a resumable record has a transcript

			first := startDaemonOn(t, socket)
			c := attach(t, socket)
			c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), Model: tc.stored})
			c.pollState(idAlpha, rpc.StateIdle)
			c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
			c.awaitState(idAlpha, rpc.StateParked)
			c.close()
			first.stop(t)

			// A different daemon, which knows this session only from the book.
			second := startDaemonOn(t, socket)
			back := attach(t, socket)
			back.pollState(idAlpha, rpc.StateParked)

			if got := wakeOutcome(back, idAlpha); !got.woke {
				t.Fatalf("a session resumed out of the park book did not come back: %s", got.why)
			}
			// Parked again, so what is read back was written by the woken
			// session out of the Config unparkRecord built for it.
			back.pollState(idAlpha, rpc.StateIdle)
			back.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
			back.awaitState(idAlpha, rpc.StateParked)

			if got := parkedModel(t, second, idAlpha); got != tc.stored {
				t.Errorf("a session resumed across a daemon restart runs as %q, want the %q its "+
					"record named: unparkRecord builds its Config from the book, so a field it "+
					"does not carry is silently gone", got, tc.stored)
			}
		})
	}
}

// The empty model is the absence of one and must not reach an argv as a flag
// with nothing after it.
func TestAnEmptyModelIsNotAModel(t *testing.T) {
	if core.ValidModel("") {
		t.Fatal("the daemon would carry an empty model onto a command line")
	}
}
