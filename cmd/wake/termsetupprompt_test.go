package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/termsetup"
)

func TestPromptTerminalSetupOnceFiresForAnUnconfiguredWritableTerminal(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	home := t.TempDir()
	t.Setenv("HOME", home)
	configHome := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	promptTerminalSetupOnce()

	n, ok := notice.Latest()
	if !ok {
		t.Fatal("no notice was reported")
	}
	if !strings.Contains(n.Text, "setup-terminal") {
		t.Errorf("the notice does not point at the verb: %q", n.Text)
	}
	if !termsetup.PromptSeen(configHome) {
		t.Error("the prompt fired but was not recorded as seen")
	}
}

func TestPromptTerminalSetupOnceNeverFiresTwice(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	promptTerminalSetupOnce()
	notice.Reset() // clear what the first call reported, so the second call's silence is unambiguous

	promptTerminalSetupOnce()
	if _, ok := notice.Latest(); ok {
		t.Error("the prompt fired a second time on the same machine")
	}
}

func TestPromptTerminalSetupOnceIsSilentWhenAlreadyConfigured(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	home := t.TempDir()
	t.Setenv("HOME", home)
	configHome := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	if _, err := termsetup.Apply(termsetup.Ghostty, configHome); err != nil {
		t.Fatal(err)
	}

	promptTerminalSetupOnce()
	if _, ok := notice.Latest(); ok {
		t.Error("the prompt fired for a terminal that was already configured")
	}
	if termsetup.PromptSeen(configHome) {
		t.Error("an already-configured terminal marked the prompt as seen, closing the gate for no reason")
	}
}

func TestPromptTerminalSetupOnceIsSilentForAManualOnlyTerminal(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	promptTerminalSetupOnce()
	if _, ok := notice.Latest(); ok {
		t.Error("the prompt fired for a terminal wake setup-terminal cannot write to")
	}
}

func TestPromptTerminalSetupOnceIsSilentForAnUnrecognisedTerminal(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)
	clearDetectionEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	promptTerminalSetupOnce()
	if _, ok := notice.Latest(); ok {
		t.Error("the prompt fired for a terminal wake could not identify")
	}
}
