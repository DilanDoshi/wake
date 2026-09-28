//go:build unix

package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A rename still waiting for idle when its agent is parked does not follow it
// into the woken process: the wake starts claude as --name <Wake's name>, so
// it is in step already. Unix-only for wakeOutcome, which reads ps.
func TestARenamePendingAtParkIsNotSentToTheWokenProcess(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hold"})
	renameTo(c, idAlpha, "bob")
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if got := wakeOutcome(c, idAlpha); !got.woke {
		t.Fatalf("the parked session was not woken: %s", got.why)
	}
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); got != "renames: 0 [] name=bob" {
		t.Fatalf("the woken process reports %q, want no /rename and --name bob", got)
	}
}

// The same for a held want: a mirror lands while the agent works and it parks
// before claude's reply. The woken process is sent nothing.
func TestAHeldRenameAtParkIsNotSentToTheWokenProcess(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hold"})
	mirrorTo(c, idAlpha, "bob")
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if got := wakeOutcome(c, idAlpha); !got.woke {
		t.Fatalf("the parked session was not woken: %s", got.why)
	}
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); got != "renames: 0 [] name=bob" {
		t.Fatalf("the woken process reports %q, want no /rename and --name bob", got)
	}
}
