// Package ui renders Wake's terminal interface. It receives messages and
// draws them; it never touches a process.
package ui

import "github.com/charmbracelet/lipgloss"

// Claude Code's own palette. §8's "full Claude Code fidelity" is a standard
// about colour as much as features: an agent answering in a DM should look
// like Claude Code answering, not like Wake reporting what it said.
//
// Every value below is Claude's, matched against Claude Code by hand and kept in
// internal/ui/testdata/claude-palette.json;
// palette_test.go holds this file to it, so a drifted colour fails with the
// value it should have been. Wake picks Claude's light and dark themes; the
// ansi and daltonized variants are accessibility modes Wake has no setting to
// select. Accent is deliberately identical on both grounds, because Claude's
// is.
var (
	Accent        = lipgloss.AdaptiveColor{Light: "#d77757", Dark: "#d77757"} // claude
	AccentShimmer = lipgloss.AdaptiveColor{Light: "#f59575", Dark: "#eb9f7f"} // claudeShimmer
	Warn          = lipgloss.AdaptiveColor{Light: "#966c1e", Dark: "#ffc107"} // warning
	Text          = lipgloss.AdaptiveColor{Light: "#000000", Dark: "#ffffff"} // text
	Muted         = lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"} // inactive
	Border        = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#888888"} // promptBorder
	Success       = lipgloss.AdaptiveColor{Light: "#2c7a39", Dark: "#4eba65"} // success
	Error         = lipgloss.AdaptiveColor{Light: "#ab2b3f", Dark: "#ff6b80"} // error

	// Subtle is dimmer than Muted and sits below it: Claude uses it for rules
	// and separators, things present but not read.
	Subtle = lipgloss.AdaptiveColor{Light: "#afafaf", Dark: "#505050"} // subtle

	// Mention is the periwinkle Claude uses for a handle - its `suggestion`,
	// which is also the colour of inline code. An address is not the message.
	Mention = lipgloss.AdaptiveColor{Light: "#5769f7", Dark: "#b1b9f9"} // suggestion

	// Own is the ground behind the operator's own message, the way Claude Code
	// shades a turn you typed.
	Own = lipgloss.AdaptiveColor{Light: "#f0f0f0", Dark: "#373737"} // userMessageBackground

	// LastRead is the boundary rule, and it is the one colour on this surface
	// that is **Wake's own decision rather than Claude's**.
	//
	// Claude Code has no last-read marker, so no key here means "the thing that
	// says where you stopped reading". The purple is Claude's - `effortUltra`,
	// which it also spells `skill` and `merged` - chosen because it is the only
	// hue in the palette that nothing else in Wake's chrome uses, so the rule
	// cannot be mistaken for an accent, a mention, a warning or an ask. Being
	// unlike everything else is the whole job: it is scanned for, not read.
	LastRead = lipgloss.AdaptiveColor{Light: "#8700ff", Dark: "#af87ff"} // effortUltra
)

// identityColors are the hues /color paints an agent in, and they are **Wake's
// own decision rather than Claude's** - LastRead's footing, and for a related
// reason. Claude's palette carries a `rainbow_*` family, but those are pastel
// mid-tones tuned to sit *behind* text; an identity colour is the foreground of
// a name read at a glance across thirty of them, so these are deliberately
// bolder and more saturated than the rainbow set. Each is a light/dark pair
// picked to stay legible on both grounds - yellow is a dark gold on the light
// theme, because a bright yellow name is unreadable on white.
//
// They are not in claude-palette.json and palette_test.go does not cover them,
// which is the whole point: a value matched against Claude is held to that file,
// and a value Wake chose is not. The keys are rpc.ColorNames, held there by
// TestIdentityColorsAreABijectionWithTheFence.
var identityColors = map[string]lipgloss.AdaptiveColor{
	"blue":   {Light: "#1f6feb", Dark: "#58a6ff"},
	"green":  {Light: "#1a7f37", Dark: "#3fb950"},
	"indigo": {Light: "#4f46e5", Dark: "#818cf8"},
	"orange": {Light: "#d4572a", Dark: "#ff8c42"},
	"red":    {Light: "#cf222e", Dark: "#ff7b72"},
	"violet": {Light: "#a21caf", Dark: "#e879f9"},
	"yellow": {Light: "#9a6700", Dark: "#f2cc60"},
}

// identityStyle is the foreground style for an agent's colour, and false when it
// has none or names a hue this build does not have. The caller draws its own
// default on false, so an unknown colour never draws a wrong one.
func identityStyle(name string) (lipgloss.Style, bool) {
	c, ok := identityColors[name]
	if !ok {
		return lipgloss.Style{}, false
	}
	return lipgloss.NewStyle().Foreground(c), true
}

var (
	// BoxStyle frames a pane: rounded border, one column of side padding.
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Border).
			Padding(0, 1)

	// ComposerStyle frames the thing you type into, and it is the one border
	// that carries the accent rather than receding. The composer is where
	// the operator's attention rests between turns, and at 15-30 sessions a
	// grey box on a grey pane is hard to find again after reading; the
	// accent is what makes "where do I type" answerable without looking.
	ComposerStyle = BoxStyle.BorderForeground(Accent)

	// cardBoxStyle frames an answerable card. It wears the warn colour rather
	// than the composer's accent or a pane's grey: a stopped agent waiting on
	// somebody is the one thing on screen that is neither conversation nor
	// where you type, and the ⚠ in its top edge is the same glyph.
	cardBoxStyle = BoxStyle.BorderForeground(Warn)

	// OwnStyle shades the operator's own message. Background only - the text
	// keeps TextStyle's foreground, so the shading distinguishes without
	// making your own words a different colour from the conversation.
	OwnStyle = lipgloss.NewStyle().Background(Own).Foreground(Text)

	// MentionStyle colours an @handle inside a message you typed, so the room
	// reads as a record of who you addressed. It carries Own's background
	// because the only place it is used is inside that shaded block, and a span
	// whose style ends mid-line takes the background with it.
	MentionStyle = lipgloss.NewStyle().Foreground(Mention).Background(Own)

	// HintStyle is for key hints and other chrome that must recede.
	HintStyle = lipgloss.NewStyle().Foreground(Muted)

	// AccentStyle marks the active or attention-worthy element.
	AccentStyle = lipgloss.NewStyle().Foreground(Accent)

	// CompletionStyle marks the matching command or mention in the completion
	// menu. Claude's effortUltra purple rather than the orange accent cards and
	// the picker wear, so a word being completed reads as its own kind of thing.
	// It shares LastRead's colour and never its surface - one is chrome under a
	// draft, the other a marker in the transcript.
	CompletionStyle = lipgloss.NewStyle().Foreground(LastRead)

	// SelectionStyle marks the cells a drag has taken.
	//
	// Own's ground rather than a new colour: it already means "this part of the
	// surface is yours" behind a message you typed, and a selection is the same
	// claim made with the mouse. Text over it because the selected part is
	// stripped before it is restyled - see highlighted - so it carries no
	// foreground of its own.
	SelectionStyle = lipgloss.NewStyle().Background(Own).Foreground(Text)

	// TextStyle is ordinary body text.
	TextStyle = lipgloss.NewStyle().Foreground(Text)
)

// A tool block's own styles, which are Claude Code's rather than chosen.
//
// The bullet says what became of the call, matched against Claude Code 2.1.233:
// its bullet component is `color: isUnresolved ? undefined : (isError ?
// "error" : "success")` with `dimColor` on the unresolved arm. Wake draws the
// unresolved one steady where Claude Code blinks it - which is Claude Code's
// own no-animation rendering, and keeps a call in flight off the draw loop.
//
// The name is bold in the ordinary text colour and the argument is plain -
// neither is dim, and neither is the accent. Wake drew the whole header in
// Claude's orange, which is the one part of this that was never Claude Code's.
var (
	ToolRunningStyle = lipgloss.NewStyle().Foreground(Muted)
	ToolOkStyle      = lipgloss.NewStyle().Foreground(Success)
	ToolFailStyle    = lipgloss.NewStyle().Foreground(Error)

	ToolNameStyle = lipgloss.NewStyle().Foreground(Text).Bold(true)
	ToolArgStyle  = lipgloss.NewStyle().Foreground(Text)

	// ErrorStyle draws a result that failed. HintStyle draws every other
	// result body: a tool's output recedes behind the conversation, which is
	// the dark grey against the header's white.
	ErrorStyle = lipgloss.NewStyle().Foreground(Error)
)
