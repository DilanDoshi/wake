package render

import (
	"testing"

	"github.com/charmbracelet/glamour"
)

// freshCache empties the renderer cache for one test and counts the renderers
// built through the newRenderer seam, putting both back afterwards.
func freshCache(t *testing.T) *int {
	t.Helper()
	mu.Lock()
	kept := renderers
	renderers = rendererCache{}
	mu.Unlock()
	orig := newRenderer
	built := new(int)
	newRenderer = func(opts ...glamour.TermRendererOption) (*glamour.TermRenderer, error) {
		*built++
		return orig(opts...)
	}
	t.Cleanup(func() {
		newRenderer = orig
		mu.Lock()
		renderers = kept
		mu.Unlock()
	})
	return built
}

// A terminal dragged through every width used to leave a renderer (~36KB
// retained, measured) cached at each one, for the life of the process. The cache
// keeps the newest CachedWidths, and those still answer without a rebuild.
func TestTheRendererCacheKeepsOnlyTheNewestWidths(t *testing.T) {
	built := freshCache(t)
	last := minMarkdownWidth + 3*CachedWidths
	for w := minMarkdownWidth; w < last; w++ {
		Markdown("x", w)
	}
	if got := len(renderers.byWidth); got != CachedWidths {
		t.Errorf("a sweep across %d widths left %d renderers cached, want %d", last-minMarkdownWidth, got, CachedWidths)
	}
	before := *built
	for w := last - CachedWidths; w < last; w++ {
		Markdown("x", w)
	}
	if *built != before {
		t.Errorf("the %d newest widths built %d renderers again, want none", CachedWidths, *built-before)
	}
}

// Newest by use, not by build: a width drawn again is kept over an older one
// nobody has drawn since, so a pane that stays on screen never pays for the
// widths a resize passed through.
func TestAWidthInUseOutlivesOnesNobodyDrew(t *testing.T) {
	built := freshCache(t)
	first := minMarkdownWidth
	for w := first; w < first+CachedWidths; w++ {
		Markdown("x", w)
	}
	Markdown("x", first)              // the oldest build, drawn again
	Markdown("x", first+CachedWidths) // one more width evicts the least recently used
	before := *built
	if Markdown("x", first); *built != before {
		t.Errorf("the width drawn most recently but one was evicted")
	}
	if Markdown("x", first+1); *built != before+1 {
		t.Errorf("width %d, the least recently used, was kept (%d builds), want it evicted", first+1, *built-before)
	}
}
