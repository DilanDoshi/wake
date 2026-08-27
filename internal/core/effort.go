package core

// The reasoning effort a session runs at.
//
// This is the *only* place the levels are spelled. The permission-mode values
// are duplicated across internal/ui/composer.go and internal/daemon/spawn.go,
// which CLAUDE.md's airlock header names as a real second leak still waiting
// for its own ruling; effort does not repeat it. Every layer that needs to know
// whether a level is legal asks ValidEffort, and the flag itself is spelled in
// argv.go beside the other command-line words.
//
// The set is closed on purpose, and that is what makes carrying it on the wire
// safe. CLAUDE.md refuses a *path* on the socket - anything that can dial it
// would be choosing a session's command line - and an unvalidated string here
// would be the same hole one flag over. A value that is not one of these five
// never reaches an argv.
//
// Recorded rather than documented: claude 2.1.229 carries two arrays,
// ["low","medium","high","xhigh","max"] and the same without "max". Wake
// accepts all five and lets claude refuse the one its model does not support,
// because the narrower list is model-dependent and Wake cannot know which model
// a session will resolve to before it starts.

import "slices"

const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
	EffortXHigh  = "xhigh"
	EffortMax    = "max"
)

// EffortLevels is every level Wake will pass, weakest first. The order is the
// order a cycle walks them in, so it is a statement rather than a set.
var EffortLevels = []string{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}

// ValidEffort reports whether a level may reach a command line. The empty
// string is *not* valid: it means "nobody chose", which callers spell by not
// passing an effort at all rather than by passing one this would admit.
func ValidEffort(level string) bool {
	return slices.Contains(EffortLevels, level)
}

// EffortUltracode and EffortAuto are levels `/effort` takes and `--effort` does
// not. Verified at 2.1.232: --help prints the flag's set as "(low, medium,
// high, xhigh, max)", while the command's usage line names these two on top.
const (
	EffortUltracode = "ultracode"
	EffortAuto      = "auto"
)

// EffortCommands is every level `/effort` takes.
//
// Wider than EffortLevels because the flag and the command are different
// surfaces that happen to share a name - a line of text on stdin against an
// argv word. Keeping one set would put "ultracode" on a command line claude
// refuses; see daemon.bookEffort, which drops one on the way back from a park.
var EffortCommands = append(append([]string{}, EffortLevels...), EffortUltracode, EffortAuto)

// ValidEffortCommand reports whether a level may be typed at a session.
// ValidEffort is the argv predicate; callers may not swap them.
func ValidEffortCommand(level string) bool {
	return slices.Contains(EffortCommands, level)
}
