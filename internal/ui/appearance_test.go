package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// These pin two pieces of appearance the owner asked for by pointing at
// Claude Code: your own message sits on a shaded ground, and the composer -
// the one thing you look for when you come back to a pane - is outlined in
// the accent rather than in the same grey as everything else.
//
// Appearance is usually not worth a test. These two are, because both are a
// single style reference that a refactor can drop with nothing else changing:
// the shading would silently become plain text, and the composer would
// silently become another grey box. Neither failure is visible in any other
// assertion in this package.
//
// A colour profile is forced because `go test` is not a terminal and lipgloss
// correctly emits no escapes into a pipe - so without this every assertion
// below would pass against unstyled output and prove nothing.
func forceColour(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(2) // termenv.ANSI - any colour profile proves the style applied
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// background is the SGR sequence lipgloss emits for OwnStyle's ground at the
// forced profile. Derived rather than hard-coded: hard-coding it would pin
// the escape lipgloss happens to emit today, and this test is about whether
// the style is applied at all, not about how lipgloss spells it.
func background(t *testing.T) string {
	t.Helper()
	rendered := lipgloss.NewStyle().Background(Own).Render("x")
	esc, _, ok := strings.Cut(rendered, "x")
	if !ok || esc == "" {
		t.Fatalf("lipgloss emitted no escape for the shaded ground at this profile: %q", rendered)
	}
	return esc
}

func TestYourOwnMessageSitsOnAShadedRectangle(t *testing.T) {
	forceColour(t)
	const width = 40
	// Long enough to wrap, so the assertion covers every line and not just
	// the one case where the text happens to reach the edge by itself.
	out := userBlock(core.Event{
		Kind: core.KindUserText,
		Text: "does the retry header survive a 401 or does refresh drop it somewhere",
	}, width)

	body := strings.Split(out, "\n")[1:] // line 0 is the "› you" label
	if len(body) < 2 {
		t.Fatalf("wanted a message that wrapped, got %d body line(s): the fixture cannot show a ragged edge", len(body))
	}
	esc := background(t)
	for i, line := range body {
		if !strings.Contains(line, esc) {
			t.Errorf("body line %d is not shaded: your own message should sit on a ground, got %q", i, line)
		}
		if got := lipgloss.Width(line); got != width {
			t.Errorf("body line %d is %d cells against a width of %d: the ground is ragged, not a rectangle", i, got, width)
		}
	}
}

// Your own turn's words line up with the reply beneath them. Both sit at
// bodyIndent - glamour's two-column margin - so only the "› you" label is at the
// edge, the same rule thinkingBlock already follows. The room-routed turn shades
// through the same shadedOwn, so it is covered here too.
func TestYourOwnMessageAlignsWithTheReply(t *testing.T) {
	forceColour(t)
	const width = 40
	const text = "does the retry header survive a 401 or does refresh drop it somewhere"

	// Where the assistant's prose starts, measured off the real reply render so
	// the two surfaces cannot drift apart in the test either.
	reply := NewDM("s1", "sydney").kindBlock(core.Event{Kind: core.KindAssistantText, Text: text}, width)
	want := indentOf(stripANSI(reply), "does")
	if want != bodyIndent {
		t.Fatalf("precondition: the assistant reply starts at column %d, not bodyIndent (%d)", want, bodyIndent)
	}

	bg := background(t)
	for _, fromRoom := range []bool{false, true} {
		out := userBlock(core.Event{Kind: core.KindUserText, Text: text, FromRoom: fromRoom}, width)
		body := strings.Split(out, "\n")[1:] // line 0 is the label
		if len(body) < 2 {
			t.Fatalf("fromRoom=%v: fixture did not wrap (%d body line(s)), so a ragged edge cannot show", fromRoom, len(body))
		}
		if got := indentOf(stripANSI(out), "does"); got != want {
			t.Errorf("fromRoom=%v: your own turn starts at column %d, the reply at %d - they do not line up:\n%s", fromRoom, got, want, out)
		}
		// The indent is drawn inside the shade, so every line is still a solid
		// width-cell rectangle - wrapped lines included.
		for i, line := range body {
			if !strings.Contains(line, bg) {
				t.Errorf("fromRoom=%v: body line %d lost its shaded ground:\n%q", fromRoom, i, line)
			}
			if cells := lipgloss.Width(line); cells != width {
				t.Errorf("fromRoom=%v: body line %d is %d cells against width %d - the ground is ragged, not a rectangle", fromRoom, i, cells, width)
			}
		}
	}
}

// The shaded own turn is a rectangle bounded to its pane at every width,
// including the ones too narrow for the indent. PaddingLeft(bodyIndent) at
// width <= bodyIndent leaves Width zero cells to wrap into and spills the whole
// message onto one unbounded line - which in the room would shove the
// neighbouring columns out of place - so shadedOwn drops the indent below that
// width and the shade below one column. Unreachable today, since every caller
// floors at minBlockWidth, but the guard belongs on the function. youSaid shades
// through the same primitive, so one sweep covers both surfaces.
func TestAShadedOwnTurnNeverOverflowsItsPane(t *testing.T) {
	const breakable = "alpha beta gamma delta epsilon zeta eta theta iota kappa"
	for _, w := range []int{1, bodyIndent, bodyIndent + 1, 4, 8, 20, 40} {
		for _, out := range []string{shadedOwn(breakable, w), youSaid("@sydney "+breakable, w)} {
			for _, line := range strings.Split(out, "\n") {
				if cells := ansi.StringWidth(line); cells > w {
					t.Errorf("width %d: a shaded own-turn line is %d cells - the ground overflows the pane and shoves its neighbours out of place:\n%q", w, cells, line)
				}
			}
		}
	}

	// Below one column Width(0) is unbounded, so there is nothing to bound; the
	// turn falls back to plain text rather than an endless shaded line.
	if got, want := shadedOwn("hi", 0), TextStyle.Render("hi"); got != want {
		t.Errorf("shadedOwn lost its width<1 fallback: got %q, want %q", got, want)
	}
}

func TestAnAgentsWordsAreNotShaded(t *testing.T) {
	forceColour(t)
	// The shading means "you said this". An agent's turn wearing it would say
	// the opposite of what it is for, which is the failure worth guarding -
	// not the absence of the style, but its presence in the wrong place.
	// Through the real dispatch rather than a renderer picked by hand, so the
	// guard also catches an agent's turn being routed to the user's path.
	out := NewDM("s1", "sydney").kindBlock(core.Event{
		Kind: core.KindAssistantText,
		Text: "Confirmed - refresh rebuilds the request and never copies it.",
	}, 40)
	if esc := background(t); strings.Contains(out, esc) {
		t.Errorf("an agent's words are wearing the ground that means you typed them:\n%q", out)
	}
}

func TestTheComposerIsOutlinedInTheAccent(t *testing.T) {
	forceColour(t)
	out := NewComposer().View(40)
	accent := lipgloss.NewStyle().Foreground(Accent).Render("x")
	esc, _, ok := strings.Cut(accent, "x")
	if !ok || esc == "" {
		t.Fatalf("lipgloss emitted no escape for the accent at this profile: %q", accent)
	}
	// The border is the first thing drawn, so the accent must reach the
	// opening corner - checking the whole string would also pass if only the
	// hint line underneath happened to carry it.
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(first, esc) {
		t.Errorf("the composer's top border is not in the accent: it is the thing you look for to know where you type\n%q", first)
	}
}
