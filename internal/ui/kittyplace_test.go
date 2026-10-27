package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// placeholderCell is one cell as kittyplace.go draws it: its own colour, the
// placeholder, then the row and column diacritics.
var placeholderCell = regexp.MustCompile("\x1b\\[38;2;(\\d+);(\\d+);(\\d+)m\U0010EEEE(.)(.)")

func cellID(m []string) int {
	r, _ := strconv.Atoi(m[1])
	g, _ := strconv.Atoi(m[2])
	b, _ := strconv.Atoi(m[3])
	return r<<16 | g<<8 | b
}

// The image's rows are text the width of the box: one placeholder a cell, each
// naming its image, its row and its column, so Bubble Tea's line diff and every
// width measure treat it as nine ordinary cells.
func TestThePlaceholderRowsAreNineCellsNamingTheImageRowAndColumn(t *testing.T) {
	const id = 0x574b07
	rows := placeholderRows(id)
	if len(rows) != friendRows {
		t.Fatalf("%d placeholder rows, want %d", len(rows), friendRows)
	}
	for y, row := range rows {
		if w := ansi.StringWidth(row); w != friendCols {
			t.Errorf("row %d measures %d columns, want %d", y, w, friendCols)
		}
		cells := placeholderCell.FindAllStringSubmatch(row, -1)
		if len(cells) != friendCols {
			t.Fatalf("row %d has %d placeholder cells, want %d: %q", y, len(cells), friendCols, row)
		}
		for x, c := range cells {
			if got := cellID(c); got != id {
				t.Errorf("row %d cell %d names image %#x, want %#x", y, x, got, id)
			}
			if c[4] != string(placeDiacritics[y]) || c[5] != string(placeDiacritics[x]) {
				t.Errorf("row %d cell %d carries diacritics %q %q, want row %d and column %d", y, x, c[4], c[5], y, x)
			}
		}
	}
}

// A row cut anywhere - a selection's edge, a drag's clip - keeps every cell it
// keeps pointed at the image, because each cell carries its own colour.
func TestACutPlaceholderRowKeepsEveryCellsImage(t *testing.T) {
	const id = 0x574b01
	row := placeholderRows(id)[2]
	for from := 0; from < friendCols; from++ {
		for to := from + 1; to <= friendCols; to++ {
			cells := placeholderCell.FindAllStringSubmatch(ansi.Cut(row, from, to), -1)
			if len(cells) != to-from {
				t.Fatalf("cut [%d,%d) kept %d whole cells, want %d", from, to, len(cells), to-from)
			}
			for _, c := range cells {
				if cellID(c) != id {
					t.Errorf("cut [%d,%d) has a cell naming %#x", from, to, cellID(c))
				}
			}
		}
	}
}

var (
	apc      = regexp.MustCompile("\x1b_G([^;\x1b]*);([^\x1b]*)\x1b\\\\")
	apcSplit = regexp.MustCompile("\x1b_G")
)

// keysOf is an APC's control keys as a map.
func keysOf(s string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

// sentImage is one image the transmission carries: its first chunk's keys and
// its PNG, reassembled from every chunk.
type sentImage struct {
	keys map[string]string
	png  []byte
}

// imagesIn reads a transmission back the way a terminal would, failing on a
// chunk the protocol does not allow.
func imagesIn(t *testing.T, seq string) []sentImage {
	t.Helper()
	if n, m := len(apcSplit.FindAllStringIndex(seq, -1)), len(apc.FindAllStringIndex(seq, -1)); n != m {
		t.Fatalf("%d APC starts but %d whole APCs", n, m)
	}
	var out []sentImage
	var b64 strings.Builder
	var cur map[string]string
	for _, m := range apc.FindAllStringSubmatch(seq, -1) {
		keys := keysOf(m[1])
		if len(m[2]) > kittyChunk {
			t.Errorf("a chunk carries %d bytes, past the protocol's %d", len(m[2]), kittyChunk)
		}
		if cur == nil {
			cur = keys
		} else if len(keys) > 2 || keys["m"] == "" {
			t.Errorf("a continuation chunk carries %q; only m and q may follow the first", m[1])
		}
		b64.WriteString(m[2])
		if keys["m"] == "1" {
			if len(m[2])%4 != 0 {
				t.Errorf("a chunk before the last is %d bytes, not a multiple of four", len(m[2]))
			}
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(b64.String())
		if err != nil {
			t.Fatalf("image %s: %v", cur["i"], err)
		}
		out = append(out, sentImage{keys: cur, png: raw})
		cur = nil
		b64.Reset()
	}
	if cur != nil {
		t.Fatalf("image %s never sent its last chunk", cur["i"])
	}
	return out
}

// transmission is every friend's images, or the test's end.
func transmission(t *testing.T) string {
	t.Helper()
	seq, err := friendTransmission()
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

// One image per friend and frame, each a quiet transmit-and-place under its own
// id, a virtual placement the box's size, and the friend's own pixels.
func TestTheTransmissionIsEveryFriendsFramesAsQuietVirtualPlacements(t *testing.T) {
	images := imagesIn(t, transmission(t))
	if want := len(cast) * friendFrames; len(images) != want {
		t.Fatalf("%d images transmitted, want %d", len(images), want)
	}
	seen := map[string]bool{}
	for i, img := range images {
		f, frame := cast[i/friendFrames], i%friendFrames
		want := map[string]string{"a": "T", "U": "1", "f": "100", "q": "2",
			"c": strconv.Itoa(friendCols), "r": strconv.Itoa(friendRows), "i": strconv.Itoa(friendImage(i/friendFrames, frame))}
		for k, v := range want {
			if img.keys[k] != v {
				t.Errorf("image %d (%s frame %d) has %s=%q, want %q", i, f.key, frame, k, img.keys[k], v)
			}
		}
		if seen[img.keys["i"]] {
			t.Errorf("image id %s is sent twice", img.keys["i"])
		}
		seen[img.keys["i"]] = true
		pic, err := png.Decode(bytes.NewReader(img.png))
		if err != nil {
			t.Fatalf("image %d: %v", i, err)
		}
		if b := pic.Bounds(); b.Dx() != friendArtWidth*friendScale || b.Dy() != friendArtHeight*friendScale {
			t.Errorf("image %d is %dx%d, want the art at x%d", i, b.Dx(), b.Dy(), friendScale)
		}
		for y, row := range f.art.frames[frame] {
			for x := 0; x < len(row); x++ {
				_, _, _, alpha := pic.At(x*friendScale+friendScale/2, y*friendScale+friendScale/2).RGBA()
				if clear := row[x] == clearPixel; clear != (alpha == 0) {
					t.Fatalf("%s frame %d pixel (%d,%d) is %q but drew alpha %d", f.key, frame, x, y, row[x], alpha)
				}
			}
		}
	}
}

// A friend's id is the colour its cells wear, so the transmission and the rows
// must agree on it for every friend and frame.
func TestTheRowsNameTheImagesTheTransmissionSends(t *testing.T) {
	for i := range cast {
		for frame := range friendFrames {
			id := friendImage(i, frame)
			c := placeholderCell.FindStringSubmatch(placeholderRows(id)[0])
			if c == nil || cellID(c) != id {
				t.Errorf("%s frame %d's rows do not name image %d", cast[i].key, frame, id)
			}
			if !strings.Contains(transmission(t), fmt.Sprintf("i=%d,", id)) {
				t.Errorf("image %d is drawn but never sent", id)
			}
		}
	}
}

// A picture past one escape's worth of base64 - Ollie's frames sit just under
// it - goes out in chunks the protocol allows and arrives whole.
func TestAnImagePastOneEscapeIsSentInChunksAndArrivesWhole(t *testing.T) {
	pic := make([]byte, 3*kittyChunk)
	for i := range pic {
		pic[i] = byte(i * 7)
	}
	var b strings.Builder
	writeImage(&b, friendImage(1, 2), pic)
	if n := strings.Count(b.String(), "\x1b_G"); n < 3 {
		t.Fatalf("a %d-byte picture went out in %d escapes, want several", len(pic), n)
	}
	got := imagesIn(t, b.String())
	if len(got) != 1 || !bytes.Equal(got[0].png, pic) || got[0].keys["i"] != strconv.Itoa(friendImage(1, 2)) {
		t.Errorf("the chunks reassembled as %d images, the first %d bytes", len(got), len(got[0].png))
	}
}
