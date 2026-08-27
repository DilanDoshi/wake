//go:build unix

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// useRealSupervisor opts one test out of the direct-path default TestMain
// installs, so it drives the production launcher end to end. The supervisor
// re-exec of the test binary is handled by TestMain's launcher branch.
func useRealSupervisor(t *testing.T) {
	t.Helper()
	orig := newAgentLauncher
	newAgentLauncher = core.SelfAgentLauncher
	t.Cleanup(func() { newAgentLauncher = orig })
}

// Activation puts a supervisor between the daemon and every claude, so the
// daemon keeps a durable off-disk handle to each agent's whole process group.
// The proof it is present: the target runs as a non-leader child of the
// supervisor (pid != pgid), where the direct path makes the target its own
// group leader.
func TestActivatedAgentRunsUnderASupervisor(t *testing.T) {
	useRealSupervisor(t)
	fakeClaudeOnPath(t, "supervised")
	d := startDaemon(t)
	c := attach(t, d.socket)
	id := testSessionID("b016")
	c.spawn(id, "")
	ev := c.awaitEvent(id, "supervised:")
	if !strings.Contains(ev.Event.Text, "child") {
		t.Fatalf("agent reported %q, want it running as a child of a supervisor; "+
			"the direct path reports 'leader', so the launcher was not activated", ev.Event.Text)
	}
}

// The silent-agent reclaim (BUG-17) still works through the supervisor - the one
// behavioral seam activation creates. When the target exits but a descendant
// holds its stdout, the supervisor reports DONE and exits, the pump stays parked,
// and the watchdog, probing the now-gone supervisor leader, reclaims the whole
// group so the live-cap slot goes back without anybody asking.
func TestASupervisedAgentThatGoesSilentIsReclaimed(t *testing.T) {
	useRealSupervisor(t)
	shortSilence(t, 200*time.Millisecond)
	fakeClaudeOnPath(t, "hold")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	// Spoken by the grandchild once its parent has exited: the agent is gone and
	// the session is not.
	c.awaitEvent(idAlpha, "held")

	ended := c.pollState(idAlpha, rpc.StateEnded)
	if !strings.Contains(ended.Error, "stopped reading stdout") {
		t.Errorf("the ending reports %q, want the account of the wedge it was reclaimed out of", ended.Error)
	}
}

// BUG-16's empty exit closes over a supervised fleet: once its one supervised
// agent has been reclaimed and the last client leaves, nothing needs supervision
// and the daemon exits on its own. This is activation and the reclaim and the
// empty-exit fix all in one path.
func TestASupervisedFleetLetsAnEmptyDaemonExit(t *testing.T) {
	useRealSupervisor(t)
	shortSilence(t, 200*time.Millisecond)
	fakeClaudeOnPath(t, "hold")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.awaitEvent(idAlpha, "held")
	c.pollState(idAlpha, rpc.StateEnded) // the watchdog reclaims the supervised silent agent

	c.close()
	if !d.waitForExitWithin(t, 4*time.Second) {
		t.Fatal("the daemon did not exit after its one supervised agent was reclaimed and the client left")
	}
}

// A supervised session survives a park and a wake and still answers - the wake
// re-execs a fresh supervisor around a --resume, and this pins that a supervised
// resume delivers the message and its answer rather than intermittently losing
// them (which the cmd/wake pty lifecycle test flagged).
func TestASupervisedWokenSessionAnswers(t *testing.T) {
	useRealSupervisor(t)
	rememberingClaudeOnPath(t)
	d := startDaemon(t)
	c := attach(t, d.socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "remember " + passphrase})
	c.awaitEvent(idAlpha, "noted")
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	woken := wakeOutcome(c, idAlpha)
	if !woken.woke {
		t.Fatalf("the parked supervised session did not come back: %s", woken.why)
	}

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: recallWord})
	answer := c.awaitEvent(idAlpha, recalledPrefix)
	if answer.Event == nil || !strings.Contains(answer.Event.Text, passphrase) {
		t.Errorf("a woken supervised session answered %q, want it to contain %q", answer.Event.Text, passphrase)
	}
}
