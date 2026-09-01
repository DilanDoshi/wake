package termsetup

import "testing"

// Detect is a pure function of the environment, so every case is a table
// entry rather than a real terminal - the whole point of injecting the map
// instead of reading os.Environ() here.
func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want Emulator
	}{
		{"ghostty by TERM_PROGRAM", map[string]string{"TERM_PROGRAM": "ghostty"}, Ghostty},
		{"ghostty is case-insensitive", map[string]string{"TERM_PROGRAM": "Ghostty"}, Ghostty},
		{"iterm2", map[string]string{"TERM_PROGRAM": "iTerm.app"}, ITerm2},
		{"apple terminal", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, AppleTerminal},
		{"vscode", map[string]string{"TERM_PROGRAM": "vscode"}, VSCode},
		{"wezterm by TERM_PROGRAM", map[string]string{"TERM_PROGRAM": "WezTerm"}, WezTerm},
		{"wezterm by pane var", map[string]string{"WEZTERM_PANE": "0"}, WezTerm},
		{"kitty by window id", map[string]string{"KITTY_WINDOW_ID": "1"}, Kitty},
		{"kitty by TERM", map[string]string{"TERM": "xterm-kitty"}, Kitty},
		{"alacritty by window id", map[string]string{"ALACRITTY_WINDOW_ID": "1"}, Alacritty},
		{"alacritty by socket", map[string]string{"ALACRITTY_SOCKET": "/tmp/a.sock"}, Alacritty},
		{"alacritty by log", map[string]string{"ALACRITTY_LOG": "/tmp/a.log"}, Alacritty},
		{"nothing recognisable", map[string]string{"TERM": "xterm-256color"}, Unknown},
		{"empty environment", map[string]string{}, Unknown},
		{
			"TERM_PROGRAM wins over a kitty variable",
			map[string]string{"TERM_PROGRAM": "ghostty", "KITTY_WINDOW_ID": "1"},
			Ghostty,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.env); got != tc.want {
				t.Errorf("Detect(%v) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

// A cmux-embedded Ghostty still reports TERM_PROGRAM=ghostty, and the
// owner's own instruction says the Ghostty config path still applies there -
// so this is the same case as the plain-Ghostty one above, asserted
// separately because it is the one this package exists to get right for the
// project's own daemon-under-cmux users.
func TestDetectGhosttyUnderCmux(t *testing.T) {
	env := map[string]string{"TERM_PROGRAM": "ghostty", "CMUX": "1"}
	if got := Detect(env); got != Ghostty {
		t.Errorf("Detect under cmux = %v, want Ghostty", got)
	}
}

func TestEnvMapSplitsOnFirstEquals(t *testing.T) {
	env := EnvMap([]string{"HOME=/Users/someone", "FOO=bar=baz", "NOEQUALS"})
	if env["HOME"] != "/Users/someone" {
		t.Errorf("HOME = %q", env["HOME"])
	}
	if env["FOO"] != "bar=baz" {
		t.Errorf("FOO = %q, want the value to keep its own embedded '='", env["FOO"])
	}
	if _, ok := env["NOEQUALS"]; ok {
		t.Error("a word with no '=' was recorded as a key")
	}
}

func TestEmulatorStringNamesEveryKnownTerminal(t *testing.T) {
	for _, e := range []Emulator{Ghostty, Kitty, Alacritty, ITerm2, AppleTerminal, VSCode, WezTerm} {
		if got := e.String(); got == "" || got == "your terminal" {
			t.Errorf("%v.String() = %q, want a real name", int(e), got)
		}
	}
	if got := Unknown.String(); got == "" {
		t.Error("Unknown.String() is empty")
	}
}
