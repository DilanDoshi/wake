package termsetup

// Multiplexer is a terminal multiplexer sitting between Wake and the real
// terminal - relevant here because it is another layer that can reshape or
// delay the raw bytes a Shift+Enter config produces, independently of
// whether the terminal underneath is configured correctly.
type Multiplexer int

const (
	NoMultiplexer Multiplexer = iota
	Tmux
	Screen
)

func (m Multiplexer) String() string {
	switch m {
	case Tmux:
		return "tmux"
	case Screen:
		return "screen"
	default:
		return ""
	}
}

// DetectMultiplexer reports whether Wake is running inside one, from the
// same env map Detect reads.
//
// Non-empty, not merely present - `TMUX= cmd` is the shell idiom for "treat
// this child as outside tmux", and hasAlacrittyEnv's header has the full
// argument for why a present-but-empty value must read as absent.
func DetectMultiplexer(env map[string]string) (Multiplexer, bool) {
	if env["TMUX"] != "" {
		return Tmux, true
	}
	if env["STY"] != "" {
		return Screen, true
	}
	return NoMultiplexer, false
}

// MultiplexerWarning is what to tell an operator running inside one: the
// terminal config this package writes is real, but the multiplexer sits
// between it and Wake and may need its own passthrough turned on.
func MultiplexerWarning(m Multiplexer) string {
	name := m.String()
	return "you're running inside " + name + ", which can buffer or delay a raw escape sequence " +
		"like the one Shift+Enter now sends, independently of the terminal config below - if it " +
		"still doesn't work after this, check " + name + "'s own passthrough of extended keys " +
		"(tmux: `set -s extended-keys on` and a short `escape-time`)."
}
