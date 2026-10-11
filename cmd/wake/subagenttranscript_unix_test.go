//go:build unix

package main

// A subagent this window never watched speak, opened from the sidebar on a real
// screen. The unit tests decide what each half does - the daemon reading the
// subagent's own file, the pane folding the answer - and this is the only thing
// that puts the click, the FrameSubagent round trip and the draw through the
// real binary and a real pty.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// dispatchDiskSaid is a line only the subagent's own file on disk says: the
// scripted agent forwards no frame of the dispatch's own.
const dispatchDiskSaid = "counted twelve lines before this window looked"

// writeSubagentTranscript lays the subagent's own sidechain transcript where
// claude's runtime writes it - <session>/subagents/agent-<id>.jsonl, beside the
// session's transcript, which has to exist for the daemon to find the
// directory.
func writeSubagentTranscript(projects, sid, task string) error {
	if err := writeFakeTranscript(projects, sid); err != nil {
		return err
	}
	dir := filepath.Join(projects, fakeProjectSlug, sid, "subagents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	head := `{"isSidechain":true,"agentId":"` + task + `",`
	lines := []string{
		head + `"type":"user","uuid":"sub-1","message":{"role":"user","content":"Count the lines in alpha.txt"}}`,
		head + `"type":"assistant","uuid":"sub-2","parentUuid":"sub-1","message":{"role":"assistant","content":[{"type":"text","text":"` + dispatchDiskSaid + `"}]}}`,
	}
	return os.WriteFile(filepath.Join(dir, "agent-"+task+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

// The bug: the row is there and clicking it opened a pane with nothing in it,
// because the words were claude's own file's and this client was never sent
// them. Clicking now draws them.
func TestClickingASubagentDrawsWhatItSaidBeforeThisWindowWatched(t *testing.T) {
	withScriptedAgent(t, scriptDispatchesLive)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	t.Setenv("WAKE_PROJECTS", t.TempDir())

	s := startWake(t, 160, 44)
	s.await("ready")
	s.send("go\r")
	s.await(heardPrefix + "dispatched")
	s.await(dispatchType)
	s.settle()

	row := s.rowOf(dispatchType)
	if row < 0 {
		t.Fatalf("no sidebar row names the subagent:\n%s", s.dump())
	}
	line := s.lines()[row]
	at := strings.Index(line, dispatchType)
	if at < sidebarBand {
		t.Fatalf("the subagent row is at column %d, not the right sidebar:\n%s", at, s.dump())
	}
	if strings.Contains(s.text(), dispatchDiskSaid) {
		t.Fatalf("the subagent's own line is on screen before it was opened:\n%s", s.dump())
	}

	s.click(utf8.RuneCountInString(line[:at]), row)
	s.await(dispatchDiskSaid)
}
