// Carrying a rewind from a client to a session's stdin.
//
// EncodeRewind has its own tests in internal/core; Session.Rewind has its own
// in internal/core/rewind_test.go. What is pinned here is the daemon's own
// half, over the same real-process harness mode_test.go uses: a FrameRewind
// reaches the agent's stdin through the same queue a send takes, and it is
// refused - the way FrameMode already is - while the session is stopped on a
// permission ask.

package daemon

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// TestFrameRewindWritesTheControlRequest pins that a FrameRewind reaches the
// agent's real stdin pipe rather than only a.sess.Rewind's own return value.
// The fake process reads its actual stdin and echoes back whatever line does
// not match a subtype it handles specially (main_test.go's fakeTurns), so the
// echoed text is proof the bytes crossed the OS pipe the daemon wrote them to
// - not merely that Session.Rewind built the right line in memory, which
// internal/core/rewind_test.go already covers.
func TestFrameRewindWritesTheControlRequest(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{
		Kind: rpc.FrameRewind, SessionID: idAlpha,
		RewindTarget: "T", RewindLastSeen: "S",
	})

	got := c.awaitEvent(idAlpha, "rewind_conversation")
	line := got.Event.Text
	if !strings.Contains(line, `"subtype":"rewind_conversation"`) ||
		!strings.Contains(line, `"target_message_uuid":"T"`) ||
		!strings.Contains(line, `"last_seen_user_message_uuid":"S"`) {
		t.Fatalf("stdin did not carry the rewind request: %s", line)
	}
}

// TestFrameRewindRefusedWhileBlockedOnAsk mirrors
// TestAModeFrameIsRefusedWhileAnAskIsOutstanding in mode_test.go: the daemon
// is holding the ask, so it can rule without asking anybody, and it refuses
// rather than writing a second control_request behind the client's back.
func TestFrameRewindRefusedWhileBlockedOnAsk(t *testing.T) {
	fakeClaudeOnPath(t, "ask")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.await("the permission ask", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindPermissionRequest
	})

	c.send(rpc.Frame{Kind: rpc.FrameRewind, SessionID: idAlpha, RewindTarget: "T", RewindLastSeen: "S"})

	f := c.await("a refusal", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "permission request") {
		t.Errorf("error = %q, want it to name the outstanding ask", f.Text)
	}
	st := c.status()
	if len(live(st)) != 1 {
		t.Fatalf("status = %+v, want the session untouched", st.Sessions)
	}
	// Untouched means still blocked, not merely still listed - mode_test.go's
	// own reasoning: the refusal above is the daemon's own sentence, nothing
	// was written to the process, so reading it as a failed write would mark a
	// healthy blocked agent unreachable.
	if got := stateOf(st, idAlpha); got != rpc.StateBlocked {
		t.Errorf("after the refusal the session reports %q, want %q: a refusal the daemon wrote itself is "+
			"not evidence the process is gone", got, rpc.StateBlocked)
	}
}
