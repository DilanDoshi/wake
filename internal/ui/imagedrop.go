package ui

// Dropping an image into the composer.
//
// A file dragged onto the terminal is inserted as a bracketed paste of its
// path - one KeyMsg carrying the whole path as runes, with spaces backslash-
// escaped or the path quoted. A paste that is entirely absolute image paths is
// a drop; anything else is ordinary pasted text and is left to the composer.
//
// The read is a tea.Cmd, off the draw goroutine, for the reason completion's
// directory read is (completionpath.go): a file on a stalled mount must not
// wedge the Update loop. The *decision* to treat a paste as a drop is made from
// the path's shape alone with no I/O, so nothing here can block; existence and
// readability are the read's problem, and a read that fails puts the path back
// into the draft rather than eating it.

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

// imageExts are the extensions worth treating a paste as a drop for. The read
// sniffs the real media type from magic bytes; this only gates the hijack, so
// an ordinary text paste is never mistaken for a drop.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// maxImageBytes bounds one image's raw file size, a cheap early reject before
// the file is read. maxTotalImageBytes bounds the *base64* of every image in one
// message together, which is the number that actually reaches the wire: a send
// frame past rpc's 16MB cap makes the daemon's scanner refuse it and hang up on
// the client (the failure internal/daemon/history.go already guards its own
// replies against). One 8MB file is ~10.7MB of base64, so a single drop always
// fits; the total is what a multi-image drop has to be held under, with headroom
// left for the text and the JSON envelope. Claude budgets and downscales the
// pixels itself, so both are transport guards, not quality ones.
const (
	maxImageBytes      = 8 << 20
	maxTotalImageBytes = 12 << 20
)

// imageDropMsg is one finished read, addressed to the pane the drop landed in
// ("" for the room) the way a bang result is - focus may have moved while the
// file was read, and the chips belong to the draft they were dropped into.
type imageDropMsg struct {
	conv    string
	results []droppedImage
}

// droppedImage is one path's outcome: a decoded block, or the error that keeps
// it from becoming one.
type droppedImage struct {
	path  string
	block core.ImageBlock
	err   error
}

// imageDropPaths returns the paths a paste carries if it is entirely absolute
// image file paths, else nil. No filesystem I/O: the shape alone decides.
func imageDropPaths(paste string) []string {
	fields := splitDropPaths(paste)
	if len(fields) == 0 {
		return nil
	}
	for _, f := range fields {
		if !filepath.IsAbs(f) || !imageExts[strings.ToLower(filepath.Ext(f))] {
			return nil
		}
	}
	return fields
}

// splitDropPaths tokenizes a dropped paste into paths, honouring the backslash
// escapes and quotes a terminal wraps a path's spaces in. A single path with
// spaces comes back as one field.
func splitDropPaths(s string) []string {
	var out []string
	var cur strings.Builder
	inTok, esc := false, false
	var quote rune
	flush := func() {
		if inTok {
			out = append(out, cur.String())
			cur.Reset()
			inTok = false
		}
	}
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			inTok, esc = true, false
		case r == '\\' && quote != '\'':
			esc, inTok = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inTok = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	flush()
	return out
}

// readDroppedImages reads each path off the draw goroutine and reports the
// batch back to the pane it was dropped into.
func readDroppedImages(conv string, paths []string) tea.Cmd {
	return func() tea.Msg {
		results := make([]droppedImage, 0, len(paths))
		for _, p := range paths {
			results = append(results, readOneImage(p))
		}
		return imageDropMsg{conv: conv, results: results}
	}
}

// readOneImage reads, sniffs, and base64-encodes one dropped file. The media
// type is read from magic bytes, never trusted from the extension, and only
// the four types a headless session accepts are allowed through.
func readOneImage(path string) droppedImage {
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return droppedImage{path: path, err: err}
	case !info.Mode().IsRegular():
		// A directory, a device, or a named pipe. The last is why this is a
		// mode check and not just IsDir(): os.ReadFile on a FIFO blocks until
		// something writes to it, and though the read is off the draw goroutine
		// it would leak that goroutine for the life of the process.
		return droppedImage{path: path, err: fmt.Errorf("%s is not a regular file", filepath.Base(path))}
	case info.Size() > maxImageBytes:
		return droppedImage{path: path, err: fmt.Errorf("%s is larger than %dMB", filepath.Base(path), maxImageBytes>>20)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return droppedImage{path: path, err: err}
	}
	media := strings.SplitN(http.DetectContentType(data), ";", 2)[0]
	if !supportedImageType(media) {
		return droppedImage{path: path, err: fmt.Errorf("%s is not a PNG, JPEG, GIF, or WebP", filepath.Base(path))}
	}
	return droppedImage{path: path, block: core.ImageBlock{
		MediaType: media,
		Data:      base64.StdEncoding.EncodeToString(data),
	}}
}

// supportedImageType is the four media types a headless session reads. An
// unsupported one degrades to a text block the model reads silently
// (2026-08-15-image-input-findings.md), so it is refused here rather than sent.
func supportedImageType(media string) bool {
	switch media {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// droppedImage recognises a dragged image - a bracketed paste that is entirely
// absolute image paths - and starts reading it, reporting handled so the key
// never reaches the key switch or the composer. Only an actual paste is taken;
// ordinary typing and ordinary pastes fall through as text.
func (a App) droppedImage(m tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.Paste {
		return a, nil, false
	}
	paths := imageDropPaths(string(m.Runes))
	if len(paths) == 0 {
		return a, nil, false
	}
	return a.disarmed().closePicker().closeBoard(), readDroppedImages(a.focus, paths), true
}

// imageDropped folds a finished read into the draft it was dropped into. A
// decoded image becomes an `[Image #N]` chip; a failed one reports why and puts
// its path back, so a drop that could not be read loses nothing.
func (a App) imageDropped(m imageDropMsg) (App, tea.Cmd) {
	comp, ok := a.composerFor(m.conv)
	if !ok {
		// The pane closed while the file was read; nothing to attach to.
		return a, nil
	}
	for _, r := range m.results {
		if r.err != nil {
			notice.Report("image drop: %v", r.err)
			comp = comp.InsertText(r.path)
			continue
		}
		// Refused up front rather than at send: an image that would push the
		// message past the frame cap is dropped here with a notice, so the send
		// path never builds a frame the daemon would refuse - and no chip is left
		// promising an image that would not go.
		if comp.imageBytes()+len(r.block.Data) > maxTotalImageBytes {
			notice.Report("image drop: %s would take the message past the %dMB limit",
				filepath.Base(r.path), maxTotalImageBytes>>20)
			continue
		}
		comp = comp.Attach(r.block)
	}
	// The draft changed, so the room's target line and any open completion menu
	// are re-derived, exactly as they are after a keystroke. No directory read:
	// a chip is neither a command nor a mention, so the cursor lands past any
	// completion word.
	return a.withComposerFor(m.conv, comp).retarget().recompleted(), nil
}

// composerFor addresses one pane's composer by conversation id, "" for the
// room, and reports whether that pane is still open. Its by-focus sibling is
// panes.go's composer(); this one exists because a drop is addressed to the
// pane it landed in, which may no longer have the focus.
func (a App) composerFor(conv string) (Composer, bool) {
	if conv == "" {
		return a.room.Composer(), true
	}
	dm, ok := a.dms[conv]
	if !ok {
		return Composer{}, false
	}
	return dm.Composer(), true
}

// withComposerFor writes a composer back to the pane it belongs to, addressed
// by id rather than by focus. A pane closed in the meantime is left alone.
func (a App) withComposerFor(conv string, c Composer) App {
	if conv == "" {
		a.room = a.room.WithComposer(c)
		return a
	}
	if _, ok := a.dms[conv]; !ok {
		return a
	}
	return a.withDM(conv, a.dms[conv].WithComposer(c))
}
