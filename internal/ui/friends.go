package ui

// The friends: a small character sitting on the session-usage bar, Skip, Ollie
// or Bo, chosen for the machine with /friends (owner's 2026-10-27 picks;
// decisions.md). In Kitty and Ghostty a friend is an image (kittyplace.go);
// everywhere else it is a hand-drawn half-block sprite in the same cells, so
// the roster's height never depends on the terminal.

import (
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	// friendCols and friendRows are the cells a friend takes at the sidebar's
	// right edge, its feet on the bar's rule; friendArtWidth and
	// friendArtHeight are its pixels as an image.
	friendCols, friendRows          = 9, 5
	friendArtWidth, friendArtHeight = 51, 60

	// friendFrames is one animation's frames, stepped every friendFrameEvery
	// while an agent works, on the heartbeat beat.go already runs.
	friendFrames     = 3
	friendFrameEvery = 400 * time.Millisecond

	clearPixel    = '.'
	friendNone    = "none"
	defaultFriend = "skip"

	// upperHalf and lowerHalf draw two of a sprite's pixels in one cell.
	upperHalf, lowerHalf = "▀", "▄"
)

// friendArt is a friend's pictures: its animation as images, and the one
// half-block sprite a terminal without Kitty graphics draws.
type friendArt struct {
	frames [friendFrames][]string
	blocks []string
}

// friend is one of the cast: the word /friends takes, the name and line its
// menu draws, and its art.
type friend struct {
	key, name, blurb string
	art              friendArt
}

var cast = []friend{
	{"skip", "Skip", "the tug that leaves a wake", skipArt},
	{"ollie", "Ollie", "an otter afloat, coffee in paw", ollieArt},
	{"bo", "Bo", "a baby whale whose spout puffs", boArt},
}

// friendPalette is every colour a friend is drawn in, one letter a colour, and
// clearPixel is a pixel that is not drawn. They are Wake's own choices, not
// Claude's, so claude-palette.json does not hold them; the grids are in
// friendsart_skip.go, friendsart_ollie.go and friendsart_bo.go.
var friendPalette = map[byte]string{
	'A': "#5a331c",          // coffee in shade
	'a': "#7a4a2b",          // coffee
	'b': "#f0a28c",          // blush
	'C': "#e2cfa8",          // Skip's roof
	'c': "#f6e7c9",          // Skip's wheelhouse
	'G': "#f0d2a8",          // Ollie's muzzle
	'H': "#c78c5c",          // Ollie's fur shine
	'h': AccentShimmer.Dark, // Skip's hull shine
	'i': "#f2e9dc",          // the mug
	'j': "#3a2418",          // Ollie's nose
	'k': "#2a1a12",          // ink: outlines, eyes
	'L': "#e6e6ec",          // smoke and steam
	'm': "#5a6ad8",          // Skip's windows
	'O': "#b1583a",          // Skip's hull in shade
	'o': Accent.Dark,        // Skip's hull
	'P': "#7f88d4",          // water in shade
	'p': UsageFill.Dark,     // water
	'R': "#d44c60",          // Skip's funnel
	'r': "#ff6b80",          // Skip's funnel band
	'S': "#7d4b2a",          // Ollie's fur in shade
	'T': "#a86c40",          // Ollie's fur
	't': "#d9b48a",          // Ollie's belly
	'U': "#5f79d8",          // Bo's body in shade
	'u': "#8fa7f0",          // Bo's body
	'v': "#d3dcff",          // a window's glint, Bo's belly and spout
	'w': "#ffffff",          // white: catchlights, crests, whiskers
}

// Friends is cmd/wake's half of the corner: the friend this machine keeps,
// whether this terminal draws Kitty images, and how a new choice is kept.
type Friends struct {
	Pick  string
	Kitty bool
	Save  func(string) error
}

// corner is the friend on the usage bar. pick is a cast key or friendNone, and
// "" - a test's App - draws no friend at all. cells are each friend's frames
// drawn once; rows are each friend's block under the roster at the sidebar's
// width, rebuilt with the bar's (withUsageRows), never per frame.
type corner struct {
	pick  string
	kitty bool
	save  func(string) error
	cells map[string][friendFrames][]string
	rows  map[string][friendFrames][]string
}

// WithFriends puts the kept friend on the usage bar, or Skip when none was
// kept or the kept word names no friend.
func (a App) WithFriends(f Friends) App {
	pick := strings.ToLower(f.Pick)
	if !slices.Contains(friendOptions(), pick) {
		pick = defaultFriend
	}
	a.corner = corner{pick: pick, kitty: f.Kitty, save: f.Save, cells: friendCells(f.Kitty)}
	return a.withUsageRows()
}

// friendOptions is what /friends takes: the cast, then none.
func friendOptions() []string {
	out := make([]string, 0, len(cast)+1)
	for _, f := range cast {
		out = append(out, f.key)
	}
	return append(out, friendNone)
}

// friendCells is every friend's frames as the five rows of cells they draw:
// placeholders naming each frame's image, or the one half-block sprite.
func friendCells(kitty bool) map[string][friendFrames][]string {
	out := make(map[string][friendFrames][]string, len(cast))
	for i, f := range cast {
		var frames [friendFrames][]string
		sprite := halfBlockRows(f.art.blocks)
		for n := range frames {
			frames[n] = sprite
			if kitty {
				frames[n] = placeholderRows(friendImage(i, n))
			}
		}
		out[f.key] = frames
	}
	return out
}

// halfBlockRows draws a sprite two pixels to a cell, upper over lower.
func halfBlockRows(sprite []string) []string {
	rows := make([]string, friendRows)
	for y := range rows {
		var b strings.Builder
		for x := range friendCols {
			b.WriteString(halfBlock(sprite[2*y][x], sprite[2*y+1][x]))
		}
		rows[y] = b.String()
	}
	return rows
}

func halfBlock(top, bottom byte) string {
	ink := func(p byte) lipgloss.Color { return lipgloss.Color(friendPalette[p]) }
	switch {
	case top == clearPixel && bottom == clearPixel:
		return " "
	case bottom == clearPixel:
		return lipgloss.NewStyle().Foreground(ink(top)).Render(upperHalf)
	case top == clearPixel:
		return lipgloss.NewStyle().Foreground(ink(bottom)).Render(lowerHalf)
	}
	return lipgloss.NewStyle().Foreground(ink(top)).Background(ink(bottom)).Render(upperHalf)
}

// rowsAt is every friend's block under the roster: standing on the bar's rule,
// over the rows under it, or alone in the corner before the first reading.
func (c corner) rowsAt(bar []string, width int) map[string][friendFrames][]string {
	if c.pick == "" || width < friendCols {
		return nil
	}
	out := make(map[string][friendFrames][]string, len(c.cells))
	for key, frames := range c.cells {
		var drawn [friendFrames][]string
		for n, cells := range frames {
			drawn[n] = blockWithFriend(bar, cells, width)
		}
		out[key] = drawn
	}
	return out
}

// blockWithFriend lays a friend's cells at the sidebar's right edge, its last
// row in place of the rule's end. bar is usageRows' block, the rule first.
func blockWithFriend(bar, cells []string, width int) []string {
	pad := strings.Repeat(" ", width-friendCols)
	out := make([]string, 0, friendRows+len(bar))
	if len(bar) == 0 {
		for _, c := range cells {
			out = append(out, pad+c)
		}
		return out
	}
	for _, c := range cells[:friendRows-1] {
		out = append(out, pad+c)
	}
	feet := usageRuleStyle.Render(strings.Repeat(sectionRule, width-friendCols)) + cells[friendRows-1]
	return append(append(out, feet), bar[1:]...)
}

// drawnFriend is who the corner shows: the /friends menu's cursored friend
// while it is open, so moving the cursor previews and closing it puts the
// kept one back, and the kept one otherwise.
func (a App) drawnFriend() string {
	if p := a.picker; p.Word == friendsCommand && p.Cursor >= 0 && p.Cursor < len(p.Options) {
		return p.Options[p.Cursor]
	}
	return a.corner.pick
}

// friendFrame is the animation's frame: off the wall clock while an agent
// works, so it rides the heartbeat beat.go already runs, and the first frame
// otherwise.
func (a App) friendFrame() int {
	if !a.fleet.anyWorking() {
		return 0
	}
	return int(clock().UnixNano()/int64(friendFrameEvery)) % friendFrames
}
