package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Discovery reads each transcript whole, on purpose: a session's slug-matching
// cwd can sit deep in the file (measured 260KB-9MB into 18 of a 358-file
// corpus), so a head-only read would drop those sessions from the picker and
// resumeSource would then refuse them. This pins the whole-file read: a proving
// cwd far past any plausible head bound is still found.
func TestDiscoveryProvesADirectoryFromACwdDeepInTheTranscript(t *testing.T) {
	projects := t.TempDir()
	real := t.TempDir()

	id := "11111111-1111-4111-8111-111111111111"
	dir := filepath.Join(projects, slugOf(real))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var b strings.Builder
	line := func(v any) {
		enc, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		b.Write(enc)
		b.WriteByte('\n')
	}
	// Early frames carry no proving cwd; padding pushes the real one megabytes in.
	pad := strings.Repeat("z", 4096)
	for b.Len() < 2*1024*1024 {
		line(map[string]any{"type": "assistant", "text": pad})
	}
	// The frame that proves the directory, deep in the file.
	line(map[string]any{"type": "user", "cwd": real, "sessionId": id})

	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sessions, err := discover(projects)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got := found(t, sessions, id).Dir; got != real {
		t.Errorf("discovery proved dir %q, want %q from a cwd deep in the transcript", got, real)
	}
}
