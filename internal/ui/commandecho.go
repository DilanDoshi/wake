package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Drawing a slash command the operator typed as a called command rather than a
// turn of prose. It is the '/' twin of leadingMention - a display detail the
// router has no stake in, because a passthrough command has already been routed
// and sent by the time it reaches a transcript, so recognising one here swallows
// nothing and belongs in the render layer rather than in slash.go.

// commandGlyph heads a command the operator typed, distinct from the '›' a prose
// turn carries so a called command and a spoken sentence do not read alike.
const commandGlyph = "⌘"

// commandName is a leading command word: a slash, a letter, then name characters
// - letters, digits, '-', '_' and the ':' of a namespaced or plugin command -
// ending at whitespace or the end of the draft. A second slash is not a name
// character, so a path and any text that merely contains a slash are never
// mistaken for a command.
var commandName = regexp.MustCompile(`^/([A-Za-z][A-Za-z0-9_:-]*)(\s|$)`)

// commandInvocation reports whether a turn is a slash command the operator
// typed, returning the command word. The display counterpart to slash's
// commandStem, narrower on purpose: commandStem takes any word at the cursor to
// offer a completion, where this rejects a path and requires a real command
// name, since a false positive here relabels a line of prose.
func commandInvocation(text string) (string, bool) {
	m := commandName.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// commandLine draws that invocation as one compact accented row - the glyph and
// what was typed, whitespace flattened, cut with an ellipsis if it will not fit
// - rather than a shaded paragraph. It keeps the accent a user turn has, since
// it is the operator's own action, and is a line rather than a block, since it
// is not prose.
func commandLine(text string, width int) string {
	inv := commandGlyph + " " + collapseWhitespaceOneLine(strings.TrimSpace(text))
	return AccentStyle.MaxWidth(width).Render(ansi.Truncate(inv, width, ellipsis))
}
