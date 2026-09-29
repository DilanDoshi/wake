//go:build unix

package main

// A conversation's @ menu driven through the real binary: fleet peers, the
// machine's other Claude sessions from the daemon's bare one-shot, subagent
// types from the init, and the project's files from git. The unit tests decide
// what each half offers; this puts the key path, the one-shot, the peers round
// trip and the drawn labels through a real pty. ⇥ accepts; ↵ still sends.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	atMenuAgent = "sydney"
	atMenuPeer  = "wren"
	atMenuFile  = "@pkg/deep/main.go"
	atMenuDecoy = "@main/n.go"
)

// atMenuRepo is a git repository whose main.go sits deep, beside a decoy whose
// whole path matches "main.g" tighter than the file's does.
func atMenuRepo(t *testing.T) string {
	t.Helper()
	wakeBinary(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the repository: %v", err)
	}
	for _, f := range []string{"pkg/deep/main.go", "main/n.go", "README.md"} {
		path := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("make %s: %v", f, err)
		}
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	return dir
}

// rowsWith is the screen rows containing text.
func (s *screen) rowsWith(text string) []int {
	var rows []int
	for y, line := range s.lines() {
		if strings.Contains(line, text) {
			rows = append(rows, y)
		}
	}
	return rows
}

// The whole menu through one conversation, then the room's, which offers none
// of the conversation's extras.
func TestAtMenuOffersPeersSessionsAgentsAndFiles(t *testing.T) {
	withScriptedAgent(t, scriptAtMenu)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	t.Setenv("WAKE_PROJECTS", t.TempDir())

	s := startWakeIn(t, atMenuRepo(t), 160, 44, cmdNew, atMenuAgent)
	s.await("ready")

	// A second agent, the one fleet peer that starts with w.
	s.send("\x17") // ⌃W to the room
	s.await("group chat")
	s.send("/new " + atMenuPeer + "\r")
	s.await("@" + atMenuPeer)
	s.send("\x1b") // clears the mention /new drafted
	s.settle()
	s.openAgent(atMenuAgent)
	s.settle()

	// @w: the fleet peer, and an outside session with its directory.
	s.send("@w")
	s.await("@" + atMenuPeer)
	s.await("@wf-alpha (")
	if row := s.rowsWith("@wf-alpha ("); len(row) != 1 ||
		!regexp.MustCompile(`@wf-alpha \(\S+`).MatchString(s.lines()[row[0]]) {
		t.Fatalf("@wf-alpha is not offered once with its directory.\n%s", s.dump())
	}
	for _, y := range s.rowsWith("@" + atMenuAgent) {
		if !strings.Contains(s.lines()[y], "╭") {
			t.Fatalf("the conversation's own agent is offered to itself.\n%s", s.dump())
		}
	}

	// @gen: a subagent type.
	s.send("\x7f\x7f@gen")
	s.await("@agent-general-purpose (agent)")

	// ⇥ on a tagged offer inserts the mention without its tag.
	s.send("\t")
	s.awaitGone("(agent)")
	s.settle()
	if got := s.rowsWith("@agent-general-purpose"); len(got) != 1 {
		t.Fatalf("⇥ did not leave the mention in the composer.\n%s", s.dump())
	}
	s.send(strings.Repeat("\x7f", 30))
	s.settle()

	// @main.g: the file whose name matches ranks above the tighter path.
	s.send("@main.g")
	s.await(atMenuFile)
	s.await(atMenuDecoy)
	file, decoy := s.rowsWith(atMenuFile), s.rowsWith(atMenuDecoy)
	if len(file) != 1 || len(decoy) != 1 || file[0] >= decoy[0] {
		t.Fatalf("%s is not offered above %s.\n%s", atMenuFile, atMenuDecoy, s.dump())
	}

	// ⇥ inserts the insert, not the label; the menu closes.
	s.send("\t")
	s.awaitGone(atMenuDecoy)
	s.settle()
	if got := s.rowsWith(atMenuFile); len(got) != 1 || strings.Contains(s.lines()[got[0]], atMenuFile+" (") {
		t.Fatalf("⇥ did not leave %q alone in the composer.\n%s", atMenuFile+" ", s.dump())
	}

	// ↵ sends the draft.
	s.send("\r")
	s.await(heardPrefix + atMenuFile)

	// The room's @w: peers only, no outside session and no subagent type.
	s.send("\x17")
	s.await("group chat")
	s.send("@w")
	s.await("@" + atMenuPeer)
	s.settle()
	if got := s.rowsWith("@wf-alpha"); len(got) != 0 {
		t.Fatalf("the room offers an outside session.\n%s", s.dump())
	}
	s.send("\x7f\x7f@gen")
	s.settle()
	if got := s.rowsWith("@agent-"); len(got) != 0 {
		t.Fatalf("the room offers a subagent type.\n%s", s.dump())
	}
}
