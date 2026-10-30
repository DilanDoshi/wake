package ui

// The friends: a small character sitting on the session-usage bar, Skip, Ollie,
// Bo or Puff, chosen for the machine with /friends (owner's 2026-10-27 and
// 2026-10-29 picks; decisions.md). In Kitty and Ghostty a friend is an image
// (kittyplace.go); everywhere else it is a hand-drawn half-block sprite in the
// same cells, so the roster's height never depends on the terminal.

import (
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/notice"
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
	{"puff", "Puff", "a pufferfish puffed round as a ball", puffArt},
}

// friendPalette is every colour a friend is drawn in, one letter a colour, and
// clearPixel is a pixel that is not drawn. They are Wake's own choices, not
// Claude's, so claude-palette.json does not hold them; the grids are in
// friendsart_skip.go, friendsart_ollie.go, friendsart_bo.go and friendsart_puff.go.
var friendPalette = map[byte]string{
	'A': "#5a331c",          // coffee in shade
	'a': "#7a4a2b",          // coffee
	'b': "#f0a28c",          // blush
	'C': "#e2cfa8",          // Skip's roof
	'c': "#f6e7c9",          // Skip's wheelhouse
	'd': "#b8812f",          // Puff's spikes
	'e': "#fdf3d6",          // Puff's belly
	'F': "#c96f2c",          // Puff's fins in shade
	'f': "#f39c4f",          // Puff's fins
	'G': "#f0d2a8",          // Ollie's muzzle
	'H': "#c78c5c",          // Ollie's fur shine
	'h': AccentShimmer.Dark, // Skip's hull shine
	'i': "#f2e9dc",          // the mug
	'j': "#3a2418",          // Ollie's nose
	'k': "#2a1a12",          // ink: outlines, eyes
	'L': "#e6e6ec",          // smoke and steam
	'l': "#fde7a6",          // Puff's shine
	'm': "#5a6ad8",          // Skip's windows
	'N': "#a83a55",          // Puff's open mouth
	'n': "#e6738a",          // Puff's lips
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
	'Y': "#d39e40",          // Puff's body in shade
	'y': "#f4c95d",          // Puff's body
}

// Friends is cmd/wake's half of the corner: the friend this machine keeps,
// whether this terminal draws Kitty images, and how a new choice is kept.
type Friends struct {
	Pick  string
	Kitty bool
	Save  func(string) error
}

// corner is the friend on the usage bar. pick is a cast key or friendNone, and
// "" - a test's App - draws no friend at all. images are each friend's frames
// as placeholder cells (Kitty terminals only) and sprites its half-block cells,
// drawn once; the rows are those laid out under the roster at the sidebar's
// width, rebuilt with the bar's (withUsageRows), never per frame. sent says the
// images are on the terminal: until then a placeholder would name nothing, so
// the sprite is drawn (kittyplace.go's transmitFriends).
type corner struct {
	pick        string
	kitty, sent bool
	save        func(string) error
	images      map[string][friendFrames][]string
	sprites     map[string][]string
	imageRows   map[string][friendFrames][]string
	spriteRows  map[string][]string
}

// WithFriends puts the kept friend on the usage bar, or Skip when none was
// kept or the kept word names no friend. The images are encoded here, before
// the program starts, so the transmission is only a write.
func (a App) WithFriends(f Friends) App {
	pick := strings.ToLower(f.Pick)
	if !slices.Contains(friendOptions(), pick) {
		pick = defaultFriend
	}
	kitty := f.Kitty
	if kitty {
		if _, err := friendTransmission(); err != nil {
			notice.Report(friendsNotSent, err)
			kitty = false
		}
	}
	a.corner = corner{pick: pick, kitty: kitty, save: f.Save, images: friendImages(kitty), sprites: friendSprites()}
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

// friendImages is every friend's frames as placeholder cells naming each
// frame's image, and none for a terminal that draws no Kitty images.
func friendImages(kitty bool) map[string][friendFrames][]string {
	if !kitty {
		return nil
	}
	out := make(map[string][friendFrames][]string, len(cast))
	for i, f := range cast {
		var frames [friendFrames][]string
		for n := range frames {
			frames[n] = placeholderRows(friendImage(i, n))
		}
		out[f.key] = frames
	}
	return out
}

// friendSprites is every friend's half-block sprite as its five rows of cells.
func friendSprites() map[string][]string {
	out := make(map[string][]string, len(cast))
	for _, f := range cast {
		out[f.key] = halfBlockRows(f.art.blocks)
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

// at lays every friend's cells out under the roster: standing on the bar's
// rule over the rows under it, or alone in the corner before the first reading.
func (c corner) at(bar []string, width int) corner {
	c.imageRows, c.spriteRows = nil, nil
	if c.pick == "" || width < friendCols {
		return c
	}
	c.imageRows = make(map[string][friendFrames][]string, len(c.images))
	for key, frames := range c.images {
		var drawn [friendFrames][]string
		for n, cells := range frames {
			drawn[n] = blockWithFriend(bar, cells, width)
		}
		c.imageRows[key] = drawn
	}
	c.spriteRows = make(map[string][]string, len(c.sprites))
	for key, cells := range c.sprites {
		c.spriteRows[key] = blockWithFriend(bar, cells, width)
	}
	return c
}

// block is friend's block under the roster in this frame: its images once the
// terminal has them, its sprite otherwise; none for none.
func (c corner) block(friend string, frame int) []string {
	if c.kitty && c.sent {
		return c.imageRows[friend][frame]
	}
	return c.spriteRows[friend]
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
