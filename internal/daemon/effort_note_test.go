package daemon

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The daemon keeps its record true by watching, never by claiming: the command
// is claude's and passes through byte for byte. These are the cases the watcher
// must and must not act on.
func TestNoteEffortRecordsOnlyAnExactCommand(t *testing.T) {
	for _, tc := range []struct {
		name, text, was, want string
	}{
		{"the command", "/effort max", "", core.EffortMax},
		{"leading and trailing space", "  /effort high  ", "", core.EffortHigh},
		{"a level replaces the last", "/effort low", core.EffortMax, core.EffortLow},

		// Everything below leaves the record alone. A bare /effort names no
		// level - in stream-json it prints a usage line and changes nothing -
		// so guessing would make the record a claim rather than a memory.
		{"no level", "/effort", core.EffortMax, core.EffortMax},
		{"an unknown level", "/effort ludicrous", core.EffortMax, core.EffortMax},
		{"a level with trailing words", "/effort max please", core.EffortMax, core.EffortMax},
		{"a different command", "/model opus", core.EffortMax, core.EffortMax},
		{"prose that mentions it", "please /effort max", core.EffortMax, core.EffortMax},
		{"ordinary text", "run the tests", core.EffortMax, core.EffortMax},
		{"empty", "", core.EffortMax, core.EffortMax},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &agent{effort: tc.was}
			a.noteEffort(tc.text)
			if a.effort != tc.want {
				t.Errorf("after %q the record is %q, want %q", tc.text, a.effort, tc.want)
			}
		})
	}
}

// Every level core admits is one the watcher recognises, so a level the CLI
// accepts at spawn cannot be one the daemon fails to notice mid-session.
func TestNoteEffortRecognisesEveryLevel(t *testing.T) {
	for _, level := range core.EffortLevels {
		a := &agent{}
		a.noteEffort("/effort " + level)
		if a.effort != level {
			t.Errorf("level %q was not recorded, got %q", level, a.effort)
		}
	}
}

// The watcher reads a line of text, so the set it recognises is the command's
// seven and not the flag's five. Without this a session set to ultracode
// reports the level it was spawned at for the rest of its life.
func TestTheWatcherRecordsALevelOnlyTheCommandTakes(t *testing.T) {
	for _, level := range core.EffortCommands {
		a := &agent{}
		a.noteEffort("/effort " + level)
		if got := a.currentEffort(); got != level {
			t.Errorf("effort is %q after /effort %s; the watcher missed a level the command accepts", got, level)
		}
	}
}

// Widening the watcher does not widen it to everything. A word claude will
// refuse must not overwrite what Wake asked for.
func TestTheWatcherStillRefusesAWordThatIsNotALevel(t *testing.T) {
	for _, word := range []string{"enormous", "ultra", "AUTO", "ultracode!", "--model"} {
		a := &agent{effort: core.EffortHigh}
		a.noteEffort("/effort " + word)
		if got := a.currentEffort(); got != core.EffortHigh {
			t.Errorf("/effort %s was recorded as %q; it is not a level", word, got)
		}
	}
}

// The command has to end where the word does.
//
// "/effortmax" is not claude's command - claude does not recognise it - so
// recording it would make Wake report a level the session was never set to.
// A stale record is a memory; this one would be a claim.
func TestNoteEffortRequiresTheCommandToEndAtTheWord(t *testing.T) {
	for _, text := range []string{
		"/effortmax", "/effortlow", "/effort-max", "/effort=max", "/effortsmax",
	} {
		a := &agent{effort: core.EffortHigh}
		a.noteEffort(text)
		if a.effort != core.EffortHigh {
			t.Errorf("%q was recorded as %q; it is not the command", text, a.effort)
		}
	}
	// The real thing, with the separators claude accepts.
	for _, text := range []string{"/effort max", "/effort  max", "  /effort max  ", "/effort\tmax"} {
		a := &agent{}
		a.noteEffort(text)
		if a.effort != core.EffortMax {
			t.Errorf("%q was not recorded, got %q", text, a.effort)
		}
	}
}

// noteModel recognises a /model that carries a model, so apply can fire a
// confirming probe on it - and nothing else, so the probe's own bare /model
// (and the UI's picker) does not loop it.
func TestNoteModelFiresOnlyOnAModelWithAnArgument(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       bool
	}{
		{"an alias", "/model opus", true},
		{"a full id", "/model claude-opus-5", true},
		{"leading and trailing space", "  /model sonnet  ", true},

		// Everything below fires nothing. A bare /model names no model - it is
		// the probe's own form and the picker's - so a re-probe there would loop.
		{"bare", "/model", false},
		{"bare with trailing space", "/model  ", false},
		{"a different command", "/effort max", false},
		{"not the command", "/modelish opus", false},
		{"prose that mentions it", "please /model opus", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &agent{}
			if got := a.noteModel(tc.text); got != tc.want {
				t.Errorf("noteModel(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// A park book is a file on disk. A row that has been edited or corrupted must
// not put an arbitrary string on a command line - and must still come back,
// because refusing to wake a session over a bad decoration is worse than
// waking it without one.
func TestARestoredSessionDropsAnEffortThatIsNotALevel(t *testing.T) {
	for _, tc := range []struct {
		name, stored, want string
	}{
		{"a level survives", core.EffortMax, core.EffortMax},
		// A record of what somebody typed, so the command's set decides it.
		// Dropping this would erase a real choice on every daemon restart; what
		// may go on a command line is argvEffort's separate question.
		{"a level only the command takes survives", core.EffortUltracode, core.EffortUltracode},
		{"an injected argument is dropped", "; touch /tmp/pwned #", ""},
		{"an unknown level is dropped", "ludicrous", ""},
		{"nothing stays nothing", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := parkedRecord{ID: idAlpha, Name: "alex", Dir: "/tmp/repo", Effort: tc.stored}
			if got := bookEffort(rec); got != tc.want {
				t.Errorf("a book naming %q resumed at %q, want %q", tc.stored, got, tc.want)
			}
		})
	}
}

// launch is the one door, so the check is on it.
//
// This is the finding a review caught. Every check in the feature lived at the
// spawn *frame*, and three of the four ways a session starts do not go through
// one: a fork, an import and a **wake** all build their own Config. The wake
// path builds it from parked.json, a file on disk, and a hand-edited row put an
// arbitrary string on a live process's command line with the frame check still
// in place and passing.
//
// Driven through launch rather than through a frame because that is the claim:
// not "the spawn frame is checked" but "nothing this daemon starts can carry a
// level that is not one".
func TestLaunchRefusesAnEffortThatIsNotALevel(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	// A spawn frame is the reachable half; the unreachable halves are covered
	// by TestARestoredSessionDropsAnEffortThatIsNotALevel one layer down.
	for _, level := range []string{"ludicrous", "; touch /tmp/pwned #", "--model=evil"} {
		c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "", Dir: t.TempDir(), Effort: level})
		c.await("a refusal naming "+level, func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameError && strings.Contains(f.Text, level)
		})
	}

	// And nothing started under any of them.
	for _, row := range statusOf(t, c).Sessions {
		if row.ID == idAlpha && row.State != rpc.StateEnded {
			t.Errorf("a refused effort left a session running: %+v", row)
		}
	}
}

// statusOf asks the daemon what it is holding.
func statusOf(t *testing.T, c *testClient) *rpc.Status {
	t.Helper()
	c.send(rpc.Frame{Kind: rpc.FrameStatus})
	f := c.await("a status reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameStatusReply && f.Status != nil
	})
	return f.Status
}
