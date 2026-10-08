package render

// The renderers built so far, one per width, bounded to the widths in use.

import (
	"slices"

	"github.com/charmbracelet/glamour"
	gansi "github.com/charmbracelet/glamour/ansi"
)

// CachedWidths is how many widths keep a renderer. It is the count
// internal/ui's TestTheRendererCacheHoldsEveryWidthOneFrameDraws measures -
// every width a 200-column frame asks for with the most conversation columns
// the layout draws at distinct widths, a plan card up, and the tiled board at
// its most tiles - plus one for a copy-time render at a fixed width.
//
// A wider terminal draws more widths than this. A miss costs one renderer
// build, ~10µs measured, and only when a block is rendered - on an event or a re-wrap,
// never on a frame, since the panes store rendered lines - so the cap governs
// the ~36KB a renderer was measured to retain rather than the speed of anything.
const CachedWidths = 9

// rendererCache holds the renderers built so far, least recently used first.
// Every access happens under mu.
type rendererCache struct {
	byWidth map[int]*glamour.TermRenderer
	recent  []int
}

// renderers is the process's one cache, since mu is its one lock.
var renderers rendererCache

// get is the renderer cached for width, which becomes the most recently used.
func (c *rendererCache) get(width int) (*glamour.TermRenderer, bool) {
	r, ok := c.byWidth[width]
	if ok {
		i := slices.Index(c.recent, width)
		c.recent = append(slices.Delete(c.recent, i, i+1), width)
	}
	return r, ok
}

// put caches r for width, evicting the least recently used past CachedWidths.
func (c *rendererCache) put(width int, r *glamour.TermRenderer) {
	if c.byWidth == nil {
		c.byWidth = map[int]*glamour.TermRenderer{}
	}
	c.byWidth[width] = r
	c.recent = append(c.recent, width)
	if len(c.recent) > CachedWidths {
		delete(c.byWidth, c.recent[0])
		c.recent = slices.Delete(c.recent, 0, 1)
	}
}

// rendererFor returns the renderer cached for width, building one on first
// use. Only the lookup and the build happen under mu; the terminal probe is
// resolved before the lock is taken.
func rendererFor(width int) (*glamour.TermRenderer, error) {
	width = boundedWidth(width)
	style := resolvedStyle() // must precede mu.Lock: this can block on the TTY

	mu.Lock()
	defer mu.Unlock()
	if r, ok := renderers.get(width); ok {
		return r, nil
	}
	r, err := newRenderer(rendererOptions(style, width)...)
	if err != nil {
		return nil, err
	}
	renderers.put(width, r)
	return r, nil
}

// rendererOptions is how a markdown renderer is built: rendererFor's, and the
// output fence's derivation tests', so what they price is what ships.
func rendererOptions(style gansi.StyleConfig, width int) []glamour.TermRendererOption {
	return []glamour.TermRendererOption{glamour.WithStyles(style), glamour.WithWordWrap(width)}
}
