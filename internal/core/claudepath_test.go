package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeOnPathSaysHowToInstallItWhenItIsMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	err := ClaudeOnPath()
	if err == nil || !strings.Contains(err.Error(), claudeInstallURL) {
		t.Fatalf("no claude on PATH: err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, claudeBinary), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ClaudeOnPath(); err != nil {
		t.Errorf("claude on PATH: err = %v", err)
	}
}

// A daemon started with a PATH that has no claude says how to install it at
// the spawn, on the direct path and the supervised one: the daemon's PATH is
// the one that counts, not the terminal's that asked for the agent.
func TestASpawnWithoutClaudeSaysHowToInstallIt(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := NewSession(Config{SessionID: "a11a0000-0000-4000-8000-00000000a11a", Dir: t.TempDir()})
	if err := s.Start(context.Background()); err == nil || !strings.Contains(err.Error(), claudeInstallURL) {
		t.Errorf("direct spawn: %v", err)
	}
}
