package daemon

// A fork's room history is its own turns only. Its transcript opens with a copy
// of the conversation it was taken from, under the same uuids
// (testdata/transcript/fork-child.jsonl), and the room draws that conversation
// under the parent; answered whole, the copy is drawn again under the fork -
// or, once the parent's 400-event tail has moved past it, only under the fork.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// recordLine is one on-disk record under a uuid, the way claude keeps it.
func recordLine(kind, uuid, parent, text string) string {
	p := `null`
	if parent != "" {
		p = `"` + parent + `"`
	}
	return `{"type":"` + kind + `","uuid":"` + uuid + `","parentUuid":` + p + `,"isSidechain":false,"timestamp":"2026-09-29T10:00:00Z","message":{"role":"` + kind + `","content":[{"type":"text","text":"` + text + `"}]}}`
}

func writeTranscriptLines(t *testing.T, dir, id string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

func historyTexts(c *testClient, kind, reply, id string) []string {
	c.t.Helper()
	c.send(rpc.Frame{Kind: kind, SessionID: id})
	f := c.await(reply+" for "+id, func(f rpc.Frame) bool { return f.Kind == reply && f.SessionID == id })
	out := make([]string, 0, len(f.Events))
	for _, ev := range f.Events {
		out = append(out, ev.Text)
	}
	return out
}

func TestAForksRoomHistoryIsItsOwnTurnsOnly(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)
	forkOf(c, idAlpha, idGamma, "")
	c.pollState(idGamma, rpc.StateIdle)

	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	inherited := []string{
		recordLine("user", "u-1", "", "the parent was asked"),
		recordLine("assistant", "u-2", "u-1", "the parent answered"),
	}
	// The parent's file holds the records the fork copied, however far its own
	// history has moved on since: the filter reads the file, not a tail.
	writeTranscriptLines(t, dir, idAlpha, inherited...)
	writeTranscriptLines(t, dir, idGamma, append(inherited,
		recordLine("user", "u-3", "u-2", "the fork was asked"),
		recordLine("assistant", "u-4", "u-3", "the fork answered"))...)

	want := "the fork was asked|the fork answered"
	if got := strings.Join(historyTexts(c, rpc.FrameRoomHistory, rpc.FrameRoomHistoryReply, idGamma), "|"); got != want {
		t.Errorf("the fork's room history is %q, want only its own turns %q", got, want)
	}
	// The conversation pane is the whole conversation, inherited half included,
	// the way Claude Code shows a fork.
	if got := historyTexts(c, rpc.FrameHistory, rpc.FrameHistoryReply, idGamma); len(got) != 4 {
		t.Errorf("the fork's conversation history is %v, want all four records", got)
	}

	// And a wake inside this daemon keeps the lineage: the woken fork is still
	// told apart from the conversation it copied.
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idGamma})
	c.pollState(idGamma, rpc.StateParked)
	c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: idGamma})
	c.pollState(idGamma, rpc.StateIdle)
	if got := strings.Join(historyTexts(c, rpc.FrameRoomHistory, rpc.FrameRoomHistoryReply, idGamma), "|"); got != want {
		t.Errorf("after a park and a wake the fork's room history is %q, want %q", got, want)
	}
}
