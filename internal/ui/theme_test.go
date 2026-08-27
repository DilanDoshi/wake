package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Both grounds must be defined and well-formed. Whether they *differ* is not
// asserted: this palette is Claude's, and Claude's accent is the same orange on
// light and dark. That used to be a rule here, and it was Wake's own invention -
// palette_test.go now decides every value against the extracted fixture.
func TestPaletteDefinesBothTerminalBackgrounds(t *testing.T) {
	for name, c := range map[string]lipgloss.AdaptiveColor{
		"Accent":        Accent,
		"AccentShimmer": AccentShimmer,
		"Warn":          Warn,
		"Text":          Text,
		"Muted":         Muted,
		"Border":        Border,
		"Subtle":        Subtle,
		"Success":       Success,
		"Mention":       Mention,
		"Own":           Own,
	} {
		for variant, hex := range map[string]string{"Light": c.Light, "Dark": c.Dark} {
			if len(hex) != 7 || !strings.HasPrefix(hex, "#") {
				t.Errorf("%s.%s = %q, want a #rrggbb hex color", name, variant, hex)
			}
		}
	}
}

func TestStylesDrawFromThePalette(t *testing.T) {
	for name, tc := range map[string]struct {
		got  lipgloss.TerminalColor
		want lipgloss.AdaptiveColor
	}{
		"HintStyle":   {HintStyle.GetForeground(), Muted},
		"AccentStyle": {AccentStyle.GetForeground(), Accent},
		"TextStyle":   {TextStyle.GetForeground(), Text},
		"BoxStyle":    {BoxStyle.GetBorderTopForeground(), Border},
	} {
		if tc.got != tc.want {
			t.Errorf("%s color = %v, want %v", name, tc.got, tc.want)
		}
	}
}

func TestBoxStyleRendersAPaddedRoundedBorder(t *testing.T) {
	out := BoxStyle.Render("hi")

	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(out, corner) {
			t.Errorf("BoxStyle is not a rounded border, missing %q:\n%s", corner, out)
		}
	}
	if !strings.Contains(out, " hi ") {
		t.Errorf("BoxStyle does not pad its content:\n%s", out)
	}
}
