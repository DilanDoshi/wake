package ui

// A friend as an image, through Kitty's graphics protocol and its unicode
// placeholders - the method the protocol made for TUIs.
//
// Every frame is transmitted once, as a virtual placement the friend's box in
// size (friendTransmission). From then on the friend is text: a placeholder
// rune a cell, whose colour names the image and whose diacritics name the row
// and column. Bubble Tea's line diff redraws it like any row, a resize moves it
// like any row, and an animation frame is a change of colour - never pixels.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
)

const (
	placeholderRune = '\U0010EEEE'

	// kittyChunk is the protocol's most base64 in one escape.
	kittyChunk = 4096

	// friendScale is how many times larger the image is sent than its art, by
	// nearest neighbour; the terminal fits it to the box.
	friendScale = 4

	// friendImageBase starts the friends' image ids ("WK"), one per friend and
	// frame; a cell's 24-bit colour carries the id.
	friendImageBase = 0x574b00

	friendsNotSent = "friends: the picture did not reach the terminal: %v"

	// defaultFG ends a placeholder cell's colour, which is its image's id.
	defaultFG = "\x1b[39m"
)

// placeDiacritics are the first entries of Kitty's rowcolumn-diacritics table:
// the nth names row or column n, which is as far as a friend's box reaches.
var placeDiacritics = []rune{'̅', '̍', '̎', '̐', '̒', '̽', '̾', '̿', '͆'}

// friendImage is the id of cast[friend]'s frame.
func friendImage(friend, frame int) int { return friendImageBase + friend*friendFrames + frame + 1 }

// placeholderRows are the box's rows drawing image id. Each cell carries its
// own colour, so a row cut anywhere - a selection's edge, a drag's clip - keeps
// every remaining cell pointed at the image.
func placeholderRows(id int) []string {
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&0xff, id>>8&0xff, id&0xff)
	rows := make([]string, friendRows)
	for y := range rows {
		var b strings.Builder
		for x := range friendCols {
			b.WriteString(fg)
			b.WriteRune(placeholderRune)
			b.WriteRune(placeDiacritics[y])
			b.WriteRune(placeDiacritics[x])
		}
		b.WriteString(defaultFG)
		rows[y] = b.String()
	}
	return rows
}

// friendTransmission is every friend's every frame, each one quiet (q=2, so no
// reply reaches Bubble Tea's input) transmit-and-place escape chunked as the
// protocol requires. Built once: the art never changes while Wake runs.
var friendTransmission = sync.OnceValues(func() (string, error) {
	var b strings.Builder
	for i, f := range cast {
		for frame, grid := range f.art.frames {
			pic, err := friendPNG(grid)
			if err != nil {
				return "", fmt.Errorf("%s frame %d: %w", f.key, frame, err)
			}
			writeImage(&b, friendImage(i, frame), pic)
		}
	}
	return b.String(), nil
})

// writeImage puts one image on b as a virtual placement the friend's box in
// size: the first chunk carries the keys, the rest only m and q.
func writeImage(b *strings.Builder, id int, pic []byte) {
	data := base64.StdEncoding.EncodeToString(pic)
	for at := 0; at < len(data); at += kittyChunk {
		end := min(at+kittyChunk, len(data))
		more := 0
		if end < len(data) {
			more = 1
		}
		if at == 0 {
			fmt.Fprintf(b, "\x1b_Ga=T,U=1,f=100,i=%d,c=%d,r=%d,q=2,m=%d;%s\x1b\\", id, friendCols, friendRows, more, data[at:end])
		} else {
			fmt.Fprintf(b, "\x1b_Gq=2,m=%d;%s\x1b\\", more, data[at:end])
		}
	}
}

// friendPNG draws one frame friendScale times its size.
func friendPNG(grid []string) ([]byte, error) {
	pic := image.NewNRGBA(image.Rect(0, 0, friendArtWidth*friendScale, friendArtHeight*friendScale))
	for y, row := range grid {
		for x := 0; x < len(row); x++ {
			if row[x] == clearPixel {
				continue
			}
			c, err := pixelColor(row[x])
			if err != nil {
				return nil, err
			}
			at := image.Rect(x*friendScale, y*friendScale, (x+1)*friendScale, (y+1)*friendScale)
			draw.Draw(pic, at, image.NewUniform(c), image.Point{}, draw.Src)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, pic); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// pixelColor is a palette letter's colour, opaque.
func pixelColor(p byte) (color.NRGBA, error) {
	hex, ok := friendPalette[p]
	if !ok || len(hex) != len("#rrggbb") {
		return color.NRGBA{}, fmt.Errorf("no colour for %q", p)
	}
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("colour %q: %w", hex, err)
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, nil
}

// transmitFriends puts every friend's images on the terminal between frames,
// when this window draws them: from Init, which runs after Bubble Tea has
// entered the alt screen, and again whenever a hand-over re-enters it, since
// that clears the screen's images (mcpauth.go's mcpSignedIn). It answers
// friendsSentMsg only once they are written, which is when the corner may
// start naming them; a failed write leaves the sprite.
func (a App) transmitFriends() tea.Cmd {
	if !a.corner.kitty || a.out == nil {
		return nil
	}
	out := a.out
	return func() tea.Msg {
		seq, err := friendTransmission()
		if err == nil {
			_, err = io.WriteString(out, seq)
		}
		if err != nil {
			notice.Report(friendsNotSent, err)
			return nil
		}
		return friendsSentMsg{}
	}
}

// friendsSentMsg says the friends' images are on the terminal.
type friendsSentMsg struct{}

func (a App) friendsSent() App {
	a.corner.sent = true
	return a
}

var (
	// drawnPlaceholder is one placeholder cell as a row carries it, its colour
	// first; copiedPlaceholder is one with the styling stripped, as a copy reads it.
	drawnPlaceholder  = regexp.MustCompile("\x1b\\[38;2;[0-9;]+m\U0010EEEE[\u0300-\u036F]*")
	copiedPlaceholder = regexp.MustCompile("\U0010EEEE[\u0300-\u036F]*")
)

// selected is a stretch of a row as a selection draws it: restyled, except a
// friend's placeholder cells, which keep their bytes - the selection's own
// colour on them would name another image.
func selected(seg string) string {
	cells := drawnPlaceholder.FindAllStringIndex(seg, -1)
	if cells == nil {
		return SelectionStyle.Render(ansi.Strip(seg))
	}
	var b strings.Builder
	at := 0
	for i, c := range cells {
		if text := ansi.Strip(seg[at:c[0]]); text != "" {
			b.WriteString(SelectionStyle.Render(text))
		}
		b.WriteString(seg[c[0]:c[1]])
		if i+1 == len(cells) || cells[i+1][0] != c[1] {
			b.WriteString(defaultFG) // a run of cells ends its image's colour, as the row did
		}
		at = c[1]
	}
	if text := ansi.Strip(seg[at:]); text != "" {
		b.WriteString(SelectionStyle.Render(text))
	}
	return b.String()
}

// withoutPlaceholders is copied text with a friend's cells taken out: they
// are a picture, not words.
func withoutPlaceholders(text string) string {
	if !strings.ContainsRune(text, placeholderRune) {
		return text
	}
	return copiedPlaceholder.ReplaceAllString(text, "")
}
