package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// writeHead makes dir look like a work tree whose HEAD holds line. The
// resolution itself is internal/gitref's and is tested there; this exists so
// the status bar's tests can put a branch somewhere for it to find.
func writeHead(t *testing.T, dir, line string) {
	t.Helper()
	git := filepath.Join(dir, ".git")
	if err := os.MkdirAll(git, 0o755); err != nil {
		t.Fatalf("making .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("writing HEAD: %v", err)
	}
}
