// Package termsetup detects the host terminal Wake is running inside and
// knows how to make it send ESC CR (the two bytes \x1b\r) for Shift+Enter -
// the sequence bubbletea already reads as alt+enter, which
// internal/ui/composer.go already binds to InsertNewline. Wake cannot change
// what bytes a terminal sends for a chord; this package edits the terminal's
// own config instead, the same behaviour Claude Code's own /terminal-setup
// ships (docs/notes/deferred.md, "⇧↵ for a newline in the composer").
//
// It touches none of internal/core, internal/rpc or internal/daemon: this is
// a client-side, local-machine concern with nothing on the wire.
package termsetup

import "strings"

// Emulator is a host terminal Wake recognises.
type Emulator int

const (
	Unknown Emulator = iota
	Ghostty
	Kitty
	Alacritty
	ITerm2
	AppleTerminal
	VSCode
	WezTerm
)

// String is the name a person reads, used in every message this package
// produces.
func (e Emulator) String() string {
	switch e {
	case Ghostty:
		return "Ghostty"
	case Kitty:
		return "Kitty"
	case Alacritty:
		return "Alacritty"
	case ITerm2:
		return "iTerm2"
	case AppleTerminal:
		return "Terminal.app"
	case VSCode:
		return "VS Code"
	case WezTerm:
		return "WezTerm"
	default:
		return "your terminal"
	}
}

// EnvMap turns os.Environ()'s "KEY=VALUE" lines into a map, so detection
// stays a pure function of data instead of a reader of the process
// environment. cmd/wake is the one caller that passes the real thing;
// everything else in this package takes the map so a test can inject one.
func EnvMap(environ []string) map[string]string {
	env := make(map[string]string, len(environ))
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}
	return env
}

// Detect names the host terminal from its environment, or Unknown when
// nothing recognisable is set.
//
// TERM_PROGRAM is checked first - most of these terminals set it to their own
// name, and it is the most direct signal available. The terminal-specific
// variables after it catch Kitty and Alacritty, which do not reliably set
// TERM_PROGRAM to a value this package can rely on across versions.
func Detect(env map[string]string) Emulator {
	switch strings.ToLower(env["TERM_PROGRAM"]) {
	case "ghostty":
		return Ghostty
	case "iterm.app":
		return ITerm2
	case "apple_terminal":
		return AppleTerminal
	case "vscode":
		return VSCode
	case "wezterm":
		return WezTerm
	}
	if env["WEZTERM_PANE"] != "" {
		return WezTerm
	}
	if env["KITTY_WINDOW_ID"] != "" {
		return Kitty
	}
	if env["TERM"] == "xterm-kitty" {
		return Kitty
	}
	if hasAlacrittyEnv(env) {
		return Alacritty
	}
	return Unknown
}

// hasAlacrittyEnv is whether any of Alacritty's own variables are set to a
// non-empty value. Unlike the others, Alacritty has no single canonical one -
// ALACRITTY_LOG, ALACRITTY_SOCKET and ALACRITTY_WINDOW_ID have each been the
// one actually present depending on platform and version - so this checks the
// family.
//
// **Non-empty, not merely present.** `VAR= cmd` is the standard shell idiom
// for "unset this for the child", and a value's presence in a map built from
// `KEY=VALUE` pairs does not distinguish that from `VAR=something` - a test
// harness clearing detection this way, or an operator's own script, must
// reliably read as absent rather than as the terminal it happens to name.
func hasAlacrittyEnv(env map[string]string) bool {
	for k, v := range env {
		if v != "" && strings.HasPrefix(k, "ALACRITTY_") {
			return true
		}
	}
	return false
}
