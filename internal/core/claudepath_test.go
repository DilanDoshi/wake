package core

import (
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
