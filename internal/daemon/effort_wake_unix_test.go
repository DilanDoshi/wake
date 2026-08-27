//go:build unix

package daemon

// A level `/effort` takes and `--effort` does not must not strand a session.
//
// The two sets diverged when the watcher started recording all seven, and the
// rule that keeps a non-argv level off a command line existed in only one of
// the two places a session can come back through. parkedAgent has it, so a
// restart was safe; unpark copied the live agent's own record straight into the
// launch config, and launch refuses a level it cannot pass. The session was
// then unwakeable until the daemon restarted - a park that is recoverable only
// by luck, which is the one property park exists to provide.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestASessionSetToACommandOnlyLevelStillWakes(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	// Through a send, because that is the only way this level is ever reached:
	// noteEffort watches the message go past. A test that set a.effort by hand
	// would assert against a state no operator can produce.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/effort " + core.EffortUltracode})
	c.pollState(idAlpha, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if got := wakeOutcome(c, idAlpha); !got.woke {
		t.Fatalf("a session set to %q could not be woken: %s\n\n"+
			"/effort takes seven levels and --effort takes five, so a level the operator really typed "+
			"cannot go back on a command line. parkedAgent already drops one; this is the other door, "+
			"and refusing here makes the park unrecoverable until the daemon restarts",
			core.EffortUltracode, got.why)
	}
}

// And the level does not silently survive onto the argv either: the session
// comes back at claude's default rather than at a level the CLI would refuse.
func TestACommandOnlyLevelIsNotWhatTheWokenSessionRunsAt(t *testing.T) {
	for _, level := range []string{core.EffortUltracode, core.EffortAuto} {
		if core.ValidEffort(argvEffort(level, idAlpha)) {
			continue
		}
		if got := argvEffort(level, idAlpha); got != "" {
			t.Errorf("argvEffort(%q) = %q; a level --effort refuses must become no flag at all", level, got)
		}
	}
	for _, level := range core.EffortLevels {
		if got := argvEffort(level, idAlpha); got != level {
			t.Errorf("argvEffort(%q) = %q; a level the flag takes must survive", level, got)
		}
	}
}
