package termsetup

import "strings"

// Multiplexer is a terminal multiplexer sitting between Wake and the real
// terminal - relevant here because it is another layer that can reshape or
// delay the raw bytes a Shift+Enter config produces, independently of
// whether the terminal underneath is configured correctly.
type Multiplexer int

const (
	NoMultiplexer Multiplexer = iota
	Tmux
	Screen
	// Cmux is not a passthrough layer like the other two: it embeds Ghostty
	// and reads the same ~/.config/ghostty/config this package writes, so a
	// session inside it detects as Ghostty and the write lands correctly. What
	// it does not do is auto-reload that file - only `cmux reload-config` (or a
	// restart) loads a change - so its whole reason to be detected is to
	// correct the reload advice, not to warn about passthrough.
	Cmux
)

func (m Multiplexer) String() string {
	switch m {
	case Tmux:
		return "tmux"
	case Screen:
		return "screen"
	case Cmux:
		return "cmux"
	default:
		return ""
	}
}

// DetectMultiplexer reports whether Wake is running inside one, from the
// same env map Detect reads.
//
// Non-empty, not merely present - `TMUX= cmd` is the shell idiom for "treat
// this child as outside tmux", and hasAlacrittyEnv's header has the full
// argument for why a present-but-empty value must read as absent. tmux and
// screen are checked first: they are the passthrough layers, and one of them
// nested inside cmux is the case where the passthrough advice is the actionable
// one.
func DetectMultiplexer(env map[string]string) (Multiplexer, bool) {
	if env["TMUX"] != "" {
		return Tmux, true
	}
	if env["STY"] != "" {
		return Screen, true
	}
	if hasCmuxEnv(env) {
		return Cmux, true
	}
	return NoMultiplexer, false
}

// hasCmuxEnv is whether any of cmux's own variables is set to a non-empty
// value. Like Alacritty, cmux has no single canonical one - CMUX_PANEL_ID,
// CMUX_SOCKET_PATH and their siblings are all set together - so this checks
// the family rather than betting on one surviving a version bump.
func hasCmuxEnv(env map[string]string) bool {
	for k, v := range env {
		if v != "" && strings.HasPrefix(k, "CMUX_") {
			return true
		}
	}
	return false
}

// MultiplexerWarning is what to tell an operator running inside one.
//
// tmux and screen sit between a real terminal's config and Wake and may need
// their own passthrough turned on. cmux is the different case: the config
// below is the right file, but cmux loads a change to it only on `cmux
// reload-config`, so the warning is where its reload advice lives - the
// verb suppresses Ghostty's own "reloads automatically" line when inside it.
func MultiplexerWarning(m Multiplexer) string {
	if m == Cmux {
		return "you're running inside cmux, which uses Ghostty's config for keybinds - the file below " +
			"is the right one - but loads a change to it only on `cmux reload-config` or a restart, not " +
			"automatically. Run `cmux reload-config` after applying this, then Shift+Enter sends the newline."
	}
	name := m.String()
	return "you're running inside " + name + ", which can buffer or delay a raw escape sequence " +
		"like the one Shift+Enter now sends, independently of the terminal config below - if it " +
		"still doesn't work after this, check " + name + "'s own passthrough of extended keys " +
		"(tmux: `set -s extended-keys on` and a short `escape-time`)."
}
