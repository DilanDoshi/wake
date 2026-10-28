package ui

import (
	"strings"
	"testing"
)

// Every friend is the same size, in every frame, so one placement and one
// transmission fit all of them.
func TestEveryFriendFrameIsTheArtsSize(t *testing.T) {
	for _, f := range cast {
		for n, frame := range f.art.frames {
			if len(frame) != friendArtHeight {
				t.Errorf("%s frame %d is %d rows, want %d", f.key, n, len(frame), friendArtHeight)
			}
			for y, row := range frame {
				if len(row) != friendArtWidth {
					t.Errorf("%s frame %d row %d is %d pixels, want %d", f.key, n, y, len(row), friendArtWidth)
				}
			}
		}
	}
}

// The half-block sprite is two pixels a cell over the same cells the image takes.
func TestEveryFriendHasAHalfBlockSpriteTheSizeOfItsCells(t *testing.T) {
	for _, f := range cast {
		if len(f.art.blocks) != 2*friendRows {
			t.Errorf("%s's sprite is %d rows, want %d", f.key, len(f.art.blocks), 2*friendRows)
		}
		for y, row := range f.art.blocks {
			if len(row) != friendCols {
				t.Errorf("%s's sprite row %d is %d pixels, want %d", f.key, y, len(row), friendCols)
			}
		}
	}
}

// Every pixel names a colour the palette has, and every colour is drawn
// somewhere, so the palette is the art's and nothing else.
func TestThePaletteIsExactlyTheColoursTheFriendsAreDrawnIn(t *testing.T) {
	used := map[byte]bool{}
	for _, f := range cast {
		grids := append(append([]string{}, f.art.blocks...), strings.Join(f.art.frames[0], ""))
		for _, frame := range f.art.frames[1:] {
			grids = append(grids, strings.Join(frame, ""))
		}
		for _, g := range grids {
			for i := 0; i < len(g); i++ {
				if g[i] == clearPixel {
					continue
				}
				if _, ok := friendPalette[g[i]]; !ok {
					t.Errorf("%s draws %q, which the palette does not hold", f.key, g[i])
				}
				used[g[i]] = true
			}
		}
	}
	for c := range friendPalette {
		if !used[c] {
			t.Errorf("the palette holds %q and no friend draws it", c)
		}
	}
}

// An animation whose frames are the same picture is a redraw for nothing.
func TestEachFriendsFramesDiffer(t *testing.T) {
	for _, f := range cast {
		for i := range f.art.frames {
			for j := i + 1; j < len(f.art.frames); j++ {
				if strings.Join(f.art.frames[i], "") == strings.Join(f.art.frames[j], "") {
					t.Errorf("%s's frames %d and %d are the same picture", f.key, i, j)
				}
			}
		}
	}
}
