package ui

import (
	"fmt"
	"testing"

	"github.com/DilanDoshi/wake/internal/render"
)

// The widths one frame renders markdown at, and the count render.CachedWidths
// is derived from. Measured rather than stated: a layout change that draws
// another width fails here instead of quietly thrashing the cache.

const (
	// frameWidth and frameHeight are the suite's standard wide geometry, the one
	// the benchmarks and the frame budget are measured at.
	frameWidth, frameHeight = 200, 40

	// boardAgents is enough agents that the tiled board draws its most tile
	// columns at frameWidth, which is its narrowest tile.
	boardAgents = 49

	// copyWidths is the renders at a fixed width the clipboard path will take
	// (a copy re-rendering what it rejoins), reserved ahead of it.
	copyWidths = 1
)

// widthsDrawn is every width a render asked internal/render for while f ran.
func widthsDrawn(f func()) map[int]bool {
	was := renderMarkdown
	seen := map[int]bool{}
	renderMarkdown = func(src string, w int) string {
		seen[w] = true
		return was(src, w)
	}
	defer func() { renderMarkdown = was }()
	f()
	return seen
}

// agentNames is n roster names, s1 onwards once withAgents seats them.
func agentNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("agent%d", i+1)
	}
	return names
}

// distinctGrid is the room and n-1 conversations, every column a different
// width from the floor upward, a plan card up in the last one, laid out one
// column narrower than the frame so the widening re-renders every pane at the
// width it keeps. False when n columns cannot all be drawn at distinct widths.
func distinctGrid(t *testing.T, n int) (App, bool) {
	t.Helper()
	names := agentNames(n)
	a := newRoomApp(t).withSize(frameWidth-1, frameHeight).withAgents(names...)
	for i := 1; i < n; i++ {
		id := fmt.Sprintf("s%d", i)
		a = pick(a, id).openRight(id, names[i-1])
	}
	for i := 1; i < n; i++ {
		a = said(a, fmt.Sprintf("s%d", i), "a reply long enough to wrap in any of these panes, once or twice")
	}
	if n > 1 {
		a.cards = a.cards.Add(fmt.Sprintf("s%d", n-1), planAsk(t))
	}
	l := a.layout
	l.Width = frameWidth
	cols := l.Regions(n, n-1).Cols
	space := 0
	for _, w := range cols {
		space += w
	}
	weights := make([]float64, n)
	for i := range n - 1 {
		weights[i] = float64(minPaneWidth + i)
		space -= minPaneWidth + i
	}
	if weights[n-1] = float64(space); n > 1 && space <= minPaneWidth+n-2 {
		return a, false
	}
	a.pending.weights = weights
	return a, true
}

// TestTheRendererCacheHoldsEveryWidthOneFrameDraws derives render.CachedWidths:
// the most widths any frame at frameWidth renders at - the most conversation
// columns the layout draws at distinct widths, with a plan card up - plus the
// tiled board's own tile width, which renders beside them while it is up, plus
// the copy path's reserved width.
func TestTheRendererCacheHoldsEveryWidthOneFrameDraws(t *testing.T) {
	most := 0
	for n := 1; ; n++ {
		a, ok := distinctGrid(t, n)
		if !ok {
			break
		}
		seen := widthsDrawn(func() {
			a = a.withSize(frameWidth, frameHeight).settled(a.geoGen + 1)
			_ = a.View()
		})
		if drawn := a.regions().Drawn(); drawn != n {
			t.Fatalf("%d columns asked for and %d drawn: the sweep is not measuring the grid it names", n, drawn)
		}
		most = max(most, len(seen))
	}
	if most < 2 {
		t.Fatalf("the widest grid drew %d widths: the sweep measured nothing", most)
	}

	b := newRoomApp(t).withSize(frameWidth, frameHeight).withAgents(agentNames(boardAgents)...)
	m, _ := typeAndSubmit(b, boardVerb)
	b = m.(App)
	b.board.Tiled = true
	b = b.ensureBoardDMs()
	// A tile renders a block as it folds in, at the tile's width, and draws the
	// stored lines after.
	tiles := widthsDrawn(func() {
		for _, ag := range b.visibleBoardAgents() {
			b = b.foldBoard(ag.ID, assistantBlock("a tile's worth of transcript, long enough to wrap in it"))
		}
		_ = b.View()
	})
	if len(tiles) == 0 {
		t.Fatal("the tiled board rendered no markdown: its tiles are not being measured")
	}

	if want := most + len(tiles) + copyWidths; render.CachedWidths != want {
		t.Errorf("render.CachedWidths is %d, and a %d-column frame renders at %d widths with the board's %d beside them, plus %d for the copy path: want %d",
			render.CachedWidths, frameWidth, most, len(tiles), copyWidths, want)
	}
}
