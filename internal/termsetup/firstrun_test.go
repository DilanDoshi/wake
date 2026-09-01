package termsetup

import "testing"

func TestPromptSeenIsFalseUntilMarked(t *testing.T) {
	home := t.TempDir()
	if PromptSeen(home) {
		t.Fatal("PromptSeen is true before MarkPromptSeen was ever called")
	}
	if err := MarkPromptSeen(home); err != nil {
		t.Fatalf("MarkPromptSeen: %v", err)
	}
	if !PromptSeen(home) {
		t.Fatal("PromptSeen is false right after MarkPromptSeen")
	}
}

func TestMarkPromptSeenIsIdempotent(t *testing.T) {
	home := t.TempDir()
	if err := MarkPromptSeen(home); err != nil {
		t.Fatalf("first MarkPromptSeen: %v", err)
	}
	if err := MarkPromptSeen(home); err != nil {
		t.Fatalf("second MarkPromptSeen: %v", err)
	}
	if !PromptSeen(home) {
		t.Fatal("PromptSeen is false after two marks")
	}
}

// Two different config homes - the shape of two different machines, or two
// different $XDG_CONFIG_HOME overrides - must not share the marker.
func TestPromptSeenIsPerConfigHome(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := MarkPromptSeen(a); err != nil {
		t.Fatal(err)
	}
	if PromptSeen(b) {
		t.Fatal("marking one config home's prompt as seen leaked into another")
	}
}
