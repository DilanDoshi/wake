//go:build unix

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// forgottenPhrase is said before a /clear, so a woken session that holds it came
// back into the conversation the clear left behind.
const forgottenPhrase = "before-the-clear-0042"

// clearSession sends /clear and waits for the report to name the conversation
// claude moved on to, which is the id a park now records.
func clearSession(c *testClient, id string) string {
	c.t.Helper()
	was := sessionRow(c.status(), id).Conversation
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: clearCommand})
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if conv := sessionRow(c.status(), id).Conversation; conv != "" && conv != was {
			return conv
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.t.Fatalf("session %s was cleared and the report never named a new conversation\nsaw: %s", id, c.transcript())
	return ""
}

// wakeInto wakes a parked row and waits for the session that comes back, which
// after a /clear is filed under the conversation it resumed.
func wakeInto(c *testClient, id, conversation string) rpc.SessionStatus {
	c.t.Helper()
	c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: id})
	var row rpc.SessionStatus
	f := c.await("the woken conversation "+conversation, func(f rpc.Frame) bool {
		if f.Kind == rpc.FrameError && f.SessionID == id {
			return true
		}
		if f.Kind != rpc.FrameStatusPush || f.Status == nil {
			return false
		}
		row = sessionRow(*f.Status, conversation)
		return row.State != "" && row.State != rpc.StateParked
	})
	if f.Kind == rpc.FrameError {
		c.t.Fatalf("the wake of %s was refused: %s", id, f.Text)
	}
	return row
}

// recalled asks a session what it holds and checks it holds what was said after
// the last /clear and nothing from before it.
func recalled(t *testing.T, c *testClient, id string) {
	t.Helper()
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: recallWord})
	got := eventText(c.awaitEvent(id, recalledPrefix))
	if !strings.Contains(got, passphrase) {
		t.Errorf("the woken session answered %q, which does not hold %q: it came back without the conversation it was cleared into", got, passphrase)
	}
	if strings.Contains(got, forgottenPhrase) {
		t.Errorf("the woken session answered %q, which holds %q from before the /clear: the wake resumed the conversation the clear left behind", got, forgottenPhrase)
	}
}

// A /clear starts a new claude conversation in the same process, so a ⌃C park
// and wake must bring back that conversation - under its id, keeping the name.
func TestAWokenSessionComesBackIntoTheConversationItWasClearedInto(t *testing.T) {
	rememberingClaudeOnPath(t)
	d := startDaemon(t)
	c := attach(t, d.socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "remember " + forgottenPhrase})
	c.awaitEvent(idAlpha, notedPrefix)
	conv := clearSession(c, idAlpha)
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "remember " + passphrase})
	c.awaitEvent(idAlpha, notedPrefix)

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	row := wakeInto(c, idAlpha, conv)
	if row.Name != "alex" {
		t.Errorf("the woken session is named %q, want the name it parked with", row.Name)
	}
	if old := sessionRow(c.status(), idAlpha); old.State != "" {
		t.Errorf("the pre-clear row %s is still reported (%s) beside the session that replaced it", idAlpha, old.State)
	}
	recalled(t, c, conv)
}

// The incident this was found from: an agent cleared twice, parked, and woken by
// a second daemon that knows it only from the park book.
func TestASessionClearedTwiceIsWokenIntoItsLastConversationAcrossADaemon(t *testing.T) {
	rememberingClaudeOnPath(t)
	socket := tempSocket(t)
	first := startDaemonOn(t, socket)
	c := attach(t, socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "remember " + forgottenPhrase})
	c.awaitEvent(idAlpha, notedPrefix)
	clearSession(c, idAlpha)
	last := clearSession(c, idAlpha)
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "remember " + passphrase})
	c.awaitEvent(idAlpha, notedPrefix)
	plantTranscript(t, last) // the conversation ran; a resumable record has a transcript

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	c.close()
	first.stop(t)

	startDaemonOn(t, socket)
	back := attach(t, socket)
	back.pollState(last, rpc.StateParked)
	woken := wakeOutcome(back, last)
	if !woken.woke {
		t.Fatalf("the parked conversation %s did not come back: %s", last, woken.why)
	}
	recalled(t, back, last)
}
