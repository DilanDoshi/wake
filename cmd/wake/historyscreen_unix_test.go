//go:build unix

// A conversation that comes back with what it already said, on a real screen.
//
// This is the end of the chain and the only test that walks all of it: a
// transcript on disk, the daemon reading it through core's airlock, a frame,
// and a pane. Every layer below has its own tests and none of them can see the
// seam to the next one.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// plantedTranscript writes a conversation where the daemon will look for one,
// and points the child at that directory.
//
// The id is not known until the daemon has assigned one, so the file is written
// after the agent exists - which is also what makes this honest: the transcript
// is found by the id Wake minted rather than by the test saying where it is.
func plantedTranscript(t *testing.T, projects, id string, lines ...string) {
	t.Helper()
	dir := filepath.Join(projects, "-planted")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

func plantedUser(text string) string {
	return `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]}}`
}

func plantedAssistant(text string) string {
	return `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}`
}

// Opening a conversation draws what it already said.
//
// The defect: `/resume` brought the process back and the pane opened with no
// account of itself. The model still had the whole conversation - claude keeps
// the transcript, which is why Wake can own almost no state - and the operator
// had none of it.
func TestAConversationOpensWithWhatItAlreadySaid(t *testing.T) {
	withScriptedAgent(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	// The id the daemon assigned, which is what the transcript is found by.
	id := liveSessionID(t, name)
	plantedTranscript(t, projects, id,
		plantedUser("what did we decide about the cap"),
		plantedAssistant("thirty, and a spawn past it is refused"),
	)

	// Detach and come back. A conversation this client has already opened has
	// already been read - asking twice would prepend a second copy of
	// everything - so the case the feature is for is a *new* window over a
	// fleet that was running before it existed.
	s.send("\x0f\r") // ⌃O arms the detach and ↵ confirms it
	s.await("Detached")

	again := startWake(t, 100, 30)
	again.await("group chat")
	// By name rather than ⌃D, which opens whoever ranks first. This window has
	// no cursor of its own - it opened no conversation - and the room seats a
	// manager beside this agent, so "first among equals" became whichever order
	// the daemon's map iterated in.
	again.openAgent(name)

	again.await("what did we decide about the cap")
	again.await("thirty, and a spawn past it is refused")
}

// A conversation with nothing on disk opens as it always did.
//
// The ordinary state of a session that has never taken a turn, and of one
// started outside a directory claude tracks. Nothing is drawn and nothing is
// said about it, because there is nothing to say.
func TestAConversationWithNoTranscriptOpensQuietly(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("\x04") // ⌃D
	s.await("@" + name)
	s.settle()

	if strings.Contains(s.text(), "could not read") {
		t.Fatalf("a conversation with no transcript reported a failure.\n%s", s.dump())
	}
}

// The past goes above the present.
//
// A live turn that landed while the disk was being read is newer than
// everything in the file, so the file goes under it - and a reader who scrolls
// back reads the conversation in the order it happened.
func TestTheTranscriptIsDrawnAboveWhatArrivedSince(t *testing.T) {
	withScriptedAgent(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()
	id := liveSessionID(t, name)
	plantedTranscript(t, projects, id, plantedUser("said before this window"))

	s.send("\x0f\r") // ⌃O arms the detach and ↵ confirms it
	s.await("Detached")

	s = startWake(t, 100, 30)
	s.await("group chat")
	s.openAgent(name) // by name, for the reason the test above gives
	s.await("said before this window")

	s.send("said after\r")
	s.await(heardPrefix + "said after")

	before, after := -1, -1
	for y, line := range s.lines() {
		if strings.Contains(line, "said before this window") {
			before = y
		}
		if strings.Contains(line, heardPrefix+"said after") {
			after = y
		}
	}
	if before < 0 || after < 0 {
		t.Fatalf("the conversation is missing a turn (before=%d after=%d).\n%s", before, after, s.dump())
	}
	if before > after {
		t.Fatalf("the transcript is drawn below what arrived since (rows %d and %d).\n%s", before, after, s.dump())
	}
}

// liveSessionID is the id the daemon gave an agent, read from the roster it
// writes beside its socket.
//
// Off disk rather than off the screen because the TUI never draws one - a
// handle is what names exist to replace - and the transcript is found by id.
// The roster is the daemon's own record of what it started, which is exactly
// the fact this needs.
func liveSessionID(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(os.Getenv("WAKE_SOCKET")), "sessions.json")

	deadline := time.Now().Add(screenTimeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			var rows []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if json.Unmarshal(raw, &rows) == nil {
				for _, r := range rows {
					if r.Name == name {
						return r.ID
					}
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no roster row for %q at %s", name, path)
	return ""
}
