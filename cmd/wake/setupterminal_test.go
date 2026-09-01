package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/termsetup"
)

// clearDetectionEnv neutralises every environment variable termsetup.Detect
// or DetectMultiplexer reads, so a test's outcome depends only on what it
// sets deliberately rather than on the terminal this suite happens to be
// running under (this machine's own dev shell has TERM_PROGRAM=ghostty).
func clearDetectionEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"TERM_PROGRAM", "TERM", "WEZTERM_PANE", "KITTY_WINDOW_ID",
		"ALACRITTY_WINDOW_ID", "ALACRITTY_SOCKET", "ALACRITTY_LOG",
		"TMUX", "STY",
	} {
		t.Setenv(k, "")
	}
}

func TestSetupTerminalFlagsParsesYesAndUndo(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want setupTerminalOpts
	}{
		{nil, setupTerminalOpts{}},
		{[]string{"--yes"}, setupTerminalOpts{Yes: true}},
		{[]string{"-y"}, setupTerminalOpts{Yes: true}},
		{[]string{"--undo"}, setupTerminalOpts{Undo: true}},
		{[]string{"--undo", "--yes"}, setupTerminalOpts{Undo: true, Yes: true}},
		{[]string{"-y", "--undo"}, setupTerminalOpts{Undo: true, Yes: true}},
	} {
		got, err := setupTerminalFlags(tc.args)
		if err != nil {
			t.Fatalf("setupTerminalFlags(%v): %v", tc.args, err)
		}
		if got != tc.want {
			t.Errorf("setupTerminalFlags(%v) = %+v, want %+v", tc.args, got, tc.want)
		}
	}
}

func TestSetupTerminalFlagsRefusesAnUnknownWord(t *testing.T) {
	_, err := setupTerminalFlags([]string{"--force"})
	if err == nil {
		t.Fatal("an unknown flag was accepted")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("the refusal does not name what was wrong: %v", err)
	}
}

// The verb is reachable through run()'s own dispatch, arity included -
// wired through main.go rather than a copy of the switch here.
func TestRunDispatchesSetupTerminal(t *testing.T) {
	clearDetectionEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	var out bytes.Buffer
	if err := run([]string{cmdSetupTerminal, "--yes"}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "detected:") {
		t.Errorf("output does not say what was detected: %q", out.String())
	}
}

func TestRunRefusesAnUnknownSetupTerminalFlag(t *testing.T) {
	if err := run([]string{cmdSetupTerminal, "--bogus"}, &bytes.Buffer{}); err == nil {
		t.Fatal("an unknown flag on wake setup-terminal was accepted")
	}
}

// --- confirmYesNo --------------------------------------------------------

func TestConfirmYesNo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  bool
	}{
		{"lowercase y", "y\n", true},
		{"uppercase Y", "Y\n", true},
		{"the word yes", "yes\n", true},
		{"YES with trailing space", "  YES  \n", true},
		{"empty line", "\n", false},
		{"no", "n\n", false},
		{"anything else", "sure\n", false},
		{"EOF with nothing typed", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := confirmYesNo(strings.NewReader(tc.input), &out, "apply? [y/N] ")
			if err != nil {
				t.Fatalf("confirmYesNo: %v", err)
			}
			if got != tc.want {
				t.Errorf("confirmYesNo(%q) = %v, want %v", tc.input, got, tc.want)
			}
			if !strings.Contains(out.String(), "apply?") {
				t.Errorf("the prompt was not written: %q", out.String())
			}
		})
	}
}

// --- the apply flow, end to end through runSetupTerminal -----------------

func TestSetupTerminalWritesOnConfirm(t *testing.T) {
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	err := runSetupTerminal(nil, strings.NewReader("y\n"), &out)
	if err != nil {
		t.Fatalf("runSetupTerminal: %v", err)
	}
	if !strings.Contains(out.String(), "wrote ") {
		t.Errorf("output does not confirm a write: %q", out.String())
	}
	path := termsetup.InfoFor(termsetup.Ghostty).ConfigPath(configHome)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the config file was not written: %v", err)
	}
}

func TestSetupTerminalDoesNotWriteOnDecline(t *testing.T) {
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	if err := runSetupTerminal(nil, strings.NewReader("n\n"), &out); err != nil {
		t.Fatalf("runSetupTerminal: %v", err)
	}
	if !strings.Contains(out.String(), "not applied") {
		t.Errorf("output does not say nothing was applied: %q", out.String())
	}
	path := termsetup.InfoFor(termsetup.Ghostty).ConfigPath(configHome)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a decline still wrote %s", path)
	}
}

// --yes must skip the prompt entirely, which this proves by handing
// runSetupTerminal a reader that errors on any read at all: reading from it
// would be reading a question nobody was asked.
func TestSetupTerminalYesSkipsThePrompt(t *testing.T) {
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	if err := runSetupTerminal([]string{"--yes"}, explodingReader{t}, &out); err != nil {
		t.Fatalf("runSetupTerminal: %v", err)
	}
	if !strings.Contains(out.String(), "wrote ") {
		t.Errorf("--yes did not write: %q", out.String())
	}
}

type explodingReader struct{ t *testing.T }

func (e explodingReader) Read([]byte) (int, error) {
	e.t.Fatal("--yes still read from stdin")
	return 0, nil
}

// Already configured means no prompt either - there is nothing to confirm.
func TestSetupTerminalSkipsThePromptWhenAlreadyConfigured(t *testing.T) {
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	if _, err := termsetup.Apply(termsetup.Ghostty, configHome); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runSetupTerminal(nil, explodingReader{t}, &out); err != nil {
		t.Fatalf("runSetupTerminal: %v", err)
	}
	if !strings.Contains(out.String(), "already configured") {
		t.Errorf("output does not say already configured: %q", out.String())
	}
}

func TestSetupTerminalUndoRemovesWhatWasWritten(t *testing.T) {
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	if err := runSetupTerminal([]string{"--yes"}, nil, &bytes.Buffer{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var out bytes.Buffer
	if err := runSetupTerminal([]string{"--undo"}, nil, &out); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !strings.Contains(out.String(), "removed from") {
		t.Errorf("output does not confirm removal: %q", out.String())
	}
	path := termsetup.InfoFor(termsetup.Ghostty).ConfigPath(configHome)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "shift+enter") {
		t.Errorf("the snippet survived undo: %q", content)
	}
}

// A terminal this package will not write to must produce manual steps
// through the same command, with no prompt and no attempted write.
func TestSetupTerminalOnAManualOnlyTerminalPrintsSteps(t *testing.T) {
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	var out bytes.Buffer
	if err := runSetupTerminal(nil, explodingReader{t}, &out); err != nil {
		t.Fatalf("runSetupTerminal: %v", err)
	}
	if !strings.Contains(out.String(), "iTerm2") {
		t.Errorf("output does not mention iTerm2: %q", out.String())
	}
	if !strings.Contains(out.String(), "Preferences") {
		t.Errorf("output does not carry the manual steps: %q", out.String())
	}
}

// Running inside tmux must surface the passthrough warning alongside
// whatever the terminal itself needs.
func TestSetupTerminalWarnsInsideAMultiplexer(t *testing.T) {
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	var out bytes.Buffer
	if err := runSetupTerminal([]string{"--yes"}, nil, &out); err != nil {
		t.Fatalf("runSetupTerminal: %v", err)
	}
	if !strings.Contains(out.String(), "tmux") {
		t.Errorf("output does not carry the multiplexer warning: %q", out.String())
	}
}

// Outside a multiplexer there is nothing to warn about - the companion to
// the test above, so a warning that fired unconditionally would still be
// caught.
func TestSetupTerminalDoesNotWarnOutsideAMultiplexer(t *testing.T) {
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	var out bytes.Buffer
	if err := runSetupTerminal([]string{"--yes"}, nil, &out); err != nil {
		t.Fatalf("runSetupTerminal: %v", err)
	}
	if strings.Contains(out.String(), "tmux") || strings.Contains(out.String(), "screen") {
		t.Errorf("output warns about a multiplexer that was never set: %q", out.String())
	}
}

// The one verb that dials no socket must not create the fleet's state
// directory as a side effect of resolving one - daemon.FleetSocketPath's
// os.MkdirAll runs on every other verb because every other verb needs the
// path it returns; this one never asks for it.
func TestSetupTerminalCreatesNoFleetDirectory(t *testing.T) {
	clearDetectionEnv(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	// A short path rather than t.TempDir(): ~/.wake/daemon.sock would be long
	// enough, nested under this test's own name, to fail on the unix socket
	// path length check before ever reaching the MkdirAll this test exists to
	// catch - which is a false pass for an unrelated reason, not evidence the
	// fix works. tempSocket (main_test.go) documents the same constraint.
	home, err := os.MkdirTemp("", "waketh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(daemon.SocketEnv, "") // force the ~/.wake fallback FleetSocketPath would otherwise resolve

	if err := run([]string{cmdSetupTerminal, "--yes"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".wake")); !os.IsNotExist(err) {
		t.Errorf("wake setup-terminal created %s/.wake, which daemon.FleetSocketPath would have made", home)
	}
}
