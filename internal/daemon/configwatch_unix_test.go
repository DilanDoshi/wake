//go:build darwin || linux

package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// awaitChange fails unless the watch signals within the test bound.
func awaitChange(t *testing.T, changes <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-changes:
	case <-time.After(testTimeout):
		t.Fatalf("no change signalled after %s", what)
	}
}

// drainChanges empties a coalesced signal left by an earlier step.
func drainChanges(changes <-chan struct{}) {
	for {
		select {
		case <-changes:
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}

// Claude's config file changes three ways and the watch sees each: a write in
// place, a replacement by rename (how claude writes it), and a delete then a
// recreate. Stopping it leaves nothing running.
func TestTheConfigWatchSeesEveryWayTheFileChanges(t *testing.T) {
	base := settledGoroutines()
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, stop := watchConfig(path)
	if changes == nil {
		t.Fatal("no watch on a platform that has one")
	}

	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, changes, "a write in place")
	drainChanges(changes)

	tmp := filepath.Join(dir, ".claude.json.tmp")
	if err := os.WriteFile(tmp, []byte(`{"a":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	drainChanges(changes) // the temporary file's own entry
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, changes, "a replacement by rename")
	drainChanges(changes)

	// The replaced file is watched too: a write in place to the new one.
	if err := os.WriteFile(path, []byte(`{"a":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, changes, "a write to the file that replaced it")
	drainChanges(changes)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	drainChanges(changes)
	if err := os.WriteFile(path, []byte(`{"a":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, changes, "a recreate after a delete")

	stop()
	waitForGoroutines(t, base)
}
