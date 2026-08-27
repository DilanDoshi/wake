package ui

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The conversation pane is 1:1 Claude Code in its colours, so every colour in
// theme.go is Claude's and this test is what says so. The fixture is
// maintained by hand against Claude Code; a palette that
// drifts from it fails here with the value it should have been.
const paletteFixture = "testdata/claude-palette.json"

type claudePalette struct {
	Source string            `json:"_source"`
	Light  map[string]string `json:"light"`
	Dark   map[string]string `json:"dark"`
}

func loadPalette(t *testing.T) claudePalette {
	t.Helper()
	raw, err := os.ReadFile(paletteFixture)
	if err != nil {
		t.Fatalf("reading %s: %v\nthe fixture is maintained by hand", paletteFixture, err)
	}
	var p claudePalette
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("parsing %s: %v", paletteFixture, err)
	}
	return p
}

// paletteBinding maps each colour Wake exports to the Claude theme key it
// mirrors. Adding a colour to theme.go without a row here is the drift this
// file exists to catch, so the table is also the enumeration.
func paletteBinding() map[string]struct {
	got lipgloss.AdaptiveColor
	key string
} {
	return map[string]struct {
		got lipgloss.AdaptiveColor
		key string
	}{
		"Accent":        {Accent, "claude"},
		"AccentShimmer": {AccentShimmer, "claudeShimmer"},
		"Warn":          {Warn, "warning"},
		"Text":          {Text, "text"},
		"Muted":         {Muted, "inactive"},
		"Border":        {Border, "promptBorder"},
		"Own":           {Own, "userMessageBackground"},
		"Mention":       {Mention, "suggestion"},
		"Subtle":        {Subtle, "subtle"},
		"Success":       {Success, "success"},
		"Error":         {Error, "error"},
		// Wake's own decision, held to Claude's palette anyway: there is no key
		// for "where you stopped reading" because Claude Code has no such
		// marker, so the colour is chosen rather than mirrored - but it is still
		// one of Claude's, and this is what stops it drifting to an invented hue.
		"LastRead": {LastRead, "effortUltra"},
	}
}

func TestPaletteIsClaudeCodes(t *testing.T) {
	p := loadPalette(t)

	for name, b := range paletteBinding() {
		wantLight, ok := p.Light[b.key]
		if !ok {
			t.Errorf("%s binds Claude key %q, which %s (from %s) does not define",
				name, b.key, paletteFixture, p.Source)
			continue
		}
		wantDark := p.Dark[b.key]

		if b.got.Light != wantLight {
			t.Errorf("%s.Light = %q, want %q (Claude's %s in %s)",
				name, b.got.Light, wantLight, b.key, p.Source)
		}
		if b.got.Dark != wantDark {
			t.Errorf("%s.Dark = %q, want %q (Claude's %s in %s)",
				name, b.got.Dark, wantDark, b.key, p.Source)
		}
	}
}

// The fixture is only evidence if it was read out of the version CLAUDE.md
// claims Wake was verified against. A palette extracted from some other build
// would pass every assertion above while describing a different Claude.
func TestPaletteFixtureNamesItsSource(t *testing.T) {
	p := loadPalette(t)
	if p.Source == "" {
		t.Fatal("fixture carries no _source; it is maintained by hand")
	}
	if len(p.Dark) < 40 {
		t.Errorf("fixture holds only %d dark keys, far short of a whole theme - extraction likely broke", len(p.Dark))
	}
}
