package render

// Claude Code's own markdown rendering, as a glamour style.
//
// # Why this file exists
//
// The conversation pane is 1:1 Claude Code, and until now that was true of the
// chrome and false of the thing the chrome is wrapped around. The renderer was
// built with glamour.WithStandardStyle, so every word an agent wrote came out
// in glamour's stock theme: headings in ANSI 39 (a hard blue), inline code in
// 203 (salmon) on a 236 background, links in 30 and 35 (two different teals).
// None of those is a Claude colour and none of them is close to one, which is
// why a reply in Wake and the same reply in Claude Code did not look related.
//
// # The rule this style is built on
//
// **Claude Code renders markdown plainly.** It does not tint headings, it does
// not put a coloured block behind an H1, and it does not indent the document.
// Emphasis is bold and italic - the things the markup already means - and the
// colour budget is spent on the two spans where colour carries information a
// reader cannot get from position: inline code, and a link.
//
// So the style below is mostly *absence*. Where glamour sets a colour, this
// leaves one unset, and an unset foreground inherits the terminal's own - which
// is what makes prose here match prose in the surrounding terminal instead of
// approximating it. The palette is consulted only where Claude genuinely uses
// one, and each of those carries the key it came from, the way theme.go does.
//
// # Where the colours come from
//
// internal/ui/testdata/claude-palette.json, Claude Code's own palette kept by
// hand. They are spelled here rather than
// imported because internal/ui imports this package and not the other way
// round; markdownstyle_test.go holds both files to the same fixture, so the two
// copies cannot drift.

import (
	gansi "github.com/charmbracelet/glamour/ansi"
)

// Claude's palette, for the three spans this style actually colours. Each name
// is the key in claude-palette.json, and markdownstyle_test.go checks it.
const (
	// suggestion is Claude's periwinkle, and theme.go already records what it
	// is for: it is the colour of a handle *and* the colour of inline code.
	suggestionDark  = "#b1b9f9"
	suggestionLight = "#5769f7"

	// subtle is what Claude draws rules and separators with - present, not read.
	subtleDark  = "#505050"
	subtleLight = "#afafaf"

	// inactive is Claude's muted text, for the things that are context rather
	// than content: a block quote, the alt text of an image.
	inactiveDark  = "#999999"
	inactiveLight = "#666666"
)

// defaultMargin is glamour's own, kept rather than chosen - see Document.
const defaultMargin uint = 2

// bullet is the list glyph. One cell wide in every font this project has been
// run in, unlike the box-drawing characters glamour's stock styles reach for.
const bullet = "• "

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }
func uintPtr(u uint) *uint       { return &u }

// claudeStyle is the markdown style for a dark or light terminal.
//
// Built from scratch rather than by copying a stock style and overriding it.
// An override leaves every field nobody thought about still set to glamour's
// value, and those fields are exactly the ones that were wrong here - the H1
// background, the two link colours and the code background were all defaults
// nobody had chosen.
func claudeStyle(dark bool) gansi.StyleConfig {
	code, rule, muted := suggestionLight, subtleLight, inactiveLight
	if dark {
		code, rule, muted = suggestionDark, subtleDark, inactiveDark
	}

	// Headings: bold, and no colour at all. Every level is the same, because
	// Claude Code does not shade a document by depth, and it strips the `#`
	// markers rather than drawing them - so no level carries a Prefix (owner
	// observation, 2026-08-29). glamour's own default strips them too; the
	// earlier Prefix here was Wake adding them back.
	heading := func() gansi.StyleBlock {
		return gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{
			Bold: boolPtr(true),
		}}
	}

	return gansi.StyleConfig{
		Document: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{
				BlockPrefix: "\n",
				BlockSuffix: "\n",
			},
			// The margin stays at glamour's two columns, and that is a
			// deliberate non-change: this file is about *colour*. The DM
			// already aligns everything else to it - dm_blocks.go indents a
			// plain body by two so thinking lines up with prose, and a
			// subagent's `│ ` gutter is drawn inside the same budget - so
			// removing it here moves every one of those instead, and the two
			// tests that caught it were right to.
			Margin: uintPtr(defaultMargin),
		},

		BlockQuote: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{
				Color:  stringPtr(muted),
				Italic: boolPtr(true),
			},
			Indent:      uintPtr(1),
			IndentToken: stringPtr("│ "),
		},

		Paragraph: gansi.StyleBlock{},

		// One heading style, all six levels the same: bold text with the `#`
		// markers stripped (owner observation, 2026-08-29). The blank line before
		// a heading is BlockPrefix on Heading itself so it applies to all of them.
		Heading: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{
			BlockSuffix: "\n",
			Bold:        boolPtr(true),
		}},
		H1: heading(),
		H2: heading(),
		H3: heading(),
		H4: heading(),
		H5: heading(),
		H6: heading(),

		Text:           gansi.StylePrimitive{},
		Strong:         gansi.StylePrimitive{Bold: boolPtr(true)},
		Emph:           gansi.StylePrimitive{Italic: boolPtr(true)},
		Strikethrough:  gansi.StylePrimitive{CrossedOut: boolPtr(true)},
		HorizontalRule: gansi.StylePrimitive{Color: stringPtr(rule), Format: "\n─────\n"},

		List: gansi.StyleList{
			StyleBlock:  gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{}},
			LevelIndent: 2,
		},
		Item: gansi.StylePrimitive{BlockPrefix: bullet},
		Enumeration: gansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: gansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},

		// A link is underlined and carries the one colour that says "this is
		// addressable". LinkText is left plain: glamour's stock style gives the
		// text one colour and the URL another, so a single link came out in two,
		// which is the effect the sources block at the end of a reply was
		// showing.
		Link:     gansi.StylePrimitive{Color: stringPtr(code), Underline: boolPtr(true)},
		LinkText: gansi.StylePrimitive{},

		Image:     gansi.StylePrimitive{Color: stringPtr(code), Underline: boolPtr(true)},
		ImageText: gansi.StylePrimitive{Color: stringPtr(muted), Format: "Image: {{.text}} →"},

		// Inline code: Claude's periwinkle and **no background**. The stock
		// style paints a 236 block behind every span, which at the density an
		// agent writes code in turns a paragraph into a row of grey boxes.
		Code: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{
			Color: stringPtr(code),
		}},

		// A fenced block keeps syntax highlighting, because Claude Code
		// highlights one too, and keeps the margin for Document's reason.
		CodeBlock: gansi.StyleCodeBlock{
			StyleBlock: gansi.StyleBlock{
				StylePrimitive: gansi.StylePrimitive{Color: stringPtr(muted)},
				Margin:         uintPtr(defaultMargin),
			},
			Theme: chromaTheme(dark),
		},

		Table: gansi.StyleTable{
			StyleBlock: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{}},
		},

		DefinitionDescription: gansi.StylePrimitive{BlockPrefix: "\n" + bullet},
	}
}

// chromaTheme is the syntax highlighting a fenced code block gets.
//
// Chroma's own names rather than Claude's palette: highlighting is a token
// classifier with dozens of classes, and hand-mapping those onto seven Claude
// colours would be inventing a theme rather than matching one. These two are
// the closest stock pair to what Claude Code shows.
func chromaTheme(dark bool) string {
	if dark {
		return "catppuccin-mocha"
	}
	return "catppuccin-latte"
}
