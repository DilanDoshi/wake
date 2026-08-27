//go:build unix

package daemon

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The one thing park/wake promises, and the one thing nothing asserted.
//
// Every other test in this package proves a woken session is the *same session*
// - same id, same name, same label, same directory - and that it is *alive*,
// because it answers. None of them proves it is holding the conversation it
// parked with, which is the whole reason anybody parks instead of stopping.
//
// The gap was structural rather than an oversight: the fake `claude` kept no
// transcript, so there was nothing for a resume to be wrong about. A fake that
// remembers is the fix, and `memory` is it - it appends every turn to a file
// keyed by session id and reads that file back when it is started with
// `--resume`.
//
// # Why the recall message may not contain the answer
//
// The passphrase is told to the agent *before* the park and asked for *after*
// the wake, in a message that does not contain it. That is the whole design: an
// echoing fake, a fake that reads the wrong file, and a fake that starts empty
// on resume all produce an answer without it, and each is a way this could pass
// while meaning nothing.
//
// It also has to survive the daemon, not just the process - see
// TestAWokenSessionAnswersFromTheConversationItParkedWithAcrossADaemon.
func TestAWokenSessionAnswersFromTheConversationItParkedWith(t *testing.T) {
	rememberingClaudeOnPath(t)
	d := startDaemon(t)
	c := attach(t, d.socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	// Said once, to the process that is about to stop existing.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "remember " + passphrase})
	c.awaitEvent(idAlpha, "noted")

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	woken := wakeOutcome(c, idAlpha)
	if !woken.woke {
		t.Fatalf("the parked session did not come back, so there is no continuity to test: %s", woken.why)
	}

	// Asked of the process `--resume` started. The word is not in the question.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: recallWord})
	answer := c.awaitEvent(idAlpha, recalledPrefix)

	if answer.Event == nil {
		t.Fatalf("the recall produced a frame with no event: %+v", answer)
	}
	if !strings.Contains(answer.Event.Text, passphrase) {
		t.Errorf("a woken session was asked to recall and answered %q, which does not contain %q.\n"+
			"The process is alive and answering, and it is not holding the conversation it parked with - "+
			"which is the only reason to park rather than stop.", answer.Event.Text, passphrase)
	}
}

// The same claim across a daemon restart, which is the shape an operator
// actually produces: ⌃Q, close the terminal, come back tomorrow, /resume.
//
// It is a separate test rather than a longer one because it fails for a
// different reason - the park book, not the transcript - and a single test
// covering both would report the wrong cause for half its failures.
func TestAWokenSessionAnswersFromTheConversationItParkedWithAcrossADaemon(t *testing.T) {
	rememberingClaudeOnPath(t)
	socket := tempSocket(t)
	plantTranscript(t, idAlpha) // the session ran; a resumable record has a transcript

	first := startDaemonOn(t, socket)
	c := attach(t, socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "remember " + passphrase})
	c.awaitEvent(idAlpha, "noted")

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	c.close()
	first.stop(t)

	// A different daemon, which knows this session only from the park book.
	startDaemonOn(t, socket)
	back := attach(t, socket)
	back.pollState(idAlpha, rpc.StateParked)

	woken := wakeOutcome(back, idAlpha)
	if !woken.woke {
		t.Fatalf("a restored session did not come back: %s", woken.why)
	}

	back.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: recallWord})
	answer := back.awaitEvent(idAlpha, recalledPrefix)

	if answer.Event == nil || !strings.Contains(answer.Event.Text, passphrase) {
		t.Errorf("a session restored from the park book and woken by a *second* daemon answered %q, "+
			"which does not contain %q. The book carried the id and the directory and the conversation "+
			"did not come with them.", eventText(answer), passphrase)
	}
}

// eventText is the answer's text, or a description of why there is none, so a
// failure message never reads "answered \"\"" about a frame that had no event.
func eventText(f rpc.Frame) string {
	if f.Event == nil {
		return "<a frame carrying no event>"
	}
	return f.Event.Text
}
