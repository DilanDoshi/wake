package render

// Where a streamed answer's finished blocks end.
//
// An answer arrives a token at a time and lands whole a moment later. Rendering it
// per token is the cost the 2026-08-15 ruling rules out, but a block that has
// *finished* never changes again, so it may be rendered once, the moment it
// completes, while only the block still being written stays raw. This finds those
// moments: a pure function of the text read so far, with no markdown parser, that
// names a cut only where the render of what is above it cannot depend on what comes
// after.
//
// A cut is allowed at exactly two places:
//
//   - a blank line outside a fence, once the line after it has started and that
//     line sits at column 0, does not open with `<`, and is not a list marker while
//     the block already holds one - so a list is never split, loose or tight, and a
//     block that carries on (an indented line, an item's paragraph) is never cut
//     from the line it belongs to;
//   - just after the closing line of a fence that opened at column 0, a closer of
//     the same character and at least the opener's length, also at column 0.
//
// Never inside a fence. A fence indented 1-3 spaces, an item's or not, is read
// through - its closer may sit up to 3 spaces in - but ends nothing: a list item's
// fence gives no cut, and the block ends where the list does. Only a fence opened at
// column 0 ends the block, and its closer is read at column 0 only; CommonMark also
// allows that one indented 1-3 spaces, but then the splitter stays in the fence and
// cuts nothing for the rest of the block, which stays the raw preview it is today -
// the safe direction.
//
// Two things stop cutting for the rest of the block, and the block is then the
// raw preview it was before this existed: a line opening with `<` (an HTML block
// can run through blank lines, and a <pre> does), and a block past MaxChunk, so a
// block this cannot read the end of costs nothing.
//
// The decision is made on a prefix. A block is cut when the first characters of the
// next one are in - one for most, a few more for a number that may be a marker - so
// formatting lags the next block's first three characters, never its end, and the
// same text cuts the same way however its tokens were cut.
//
// Not covered, and transient - the answer lands whole and is drawn right then: a
// reference-style link definition or a footnote definition further down changes how
// a use above it renders, which a block rendered alone cannot know; a definition
// list ("Term" over ": definition") is one list across a blank line, which two
// blocks are not; a fence inside a list item whose body drops back to column 0
// is closed by CommonMark where the splitter reads on; and a bare file name that
// glamour links (`tally.txt`) is a styled row it wraps itself, a word earlier than
// Prose would.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// MaxChunk is the longest block the splitter will cut, in bytes: past it the block
// is frozen. A block's length is counted in whole lines read, so the same text
// freezes at the same place however it arrived.
const MaxChunk = 4096

// maxMarkerDigits is how many digits a CommonMark ordered-list marker may have.
const maxMarkerDigits = 9

// Splitter is where a scan of the open text has got to. The zero value reads the
// start of a block. It is a value: Next returns the next one.
type Splitter struct {
	at      int  // the start of the first line not read yet
	fence   byte // the character of the open fence, or 0
	fenceN  int  // how many of it opened the fence
	flush   bool // the fence opened at column 0, so its closer ends the block
	content bool // the block holds a line that is not blank
	list    bool // ... and one opening with a list marker at indent 3 or less
	gap     bool // blank lines follow the block's last line: a cut may fall at `at`
	frozen  bool
}

// Frozen reports that cutting has stopped for the rest of the block.
func (s Splitter) Frozen() bool { return s.frozen }

// verdict is what the start of the line after a gap says about a cut.
type verdict int

const (
	wait   verdict = iota // too few characters to tell
	cut                   // the block above is finished
	carry                 // the block goes on
	freeze                // an HTML block may start: stop cutting
)

// marker is whether text opens with a list marker.
type marker int

const (
	notMarker marker = iota
	isMarker
	mayBeMarker // the characters so far could still become one
)

// Next reads the open text - everything since the last cut - and returns where the
// first finished block ends, or 0 when none has. After a cut the returned Splitter
// is ready for the remainder, open[cut:]; with none it has read as far as it can.
func (s Splitter) Next(open string) (int, Splitter) {
	for !s.frozen {
		at, more, next := s.step(open)
		if at > 0 {
			return at, Splitter{}
		}
		if s = next; !more {
			break
		}
	}
	return 0, s
}

// step reads one line, or decides at one. It returns a cut offset (0 for none),
// whether there is more to read, and the splitter after it.
func (s Splitter) step(open string) (int, bool, Splitter) {
	if s.at > MaxChunk || len(open)-s.at > MaxChunk {
		s.frozen = true
		return 0, false, s
	}
	rest := open[s.at:]
	nl := strings.IndexByte(rest, '\n')
	if s.fence != 0 {
		return s.inFence(rest, nl)
	}
	if nl >= 0 && blankLine(rest[:nl]) {
		s.gap = s.gap || s.content
		s.at += nl + 1
		return 0, true, s
	}
	if s.gap {
		switch s.verdictOn(rest) {
		case wait:
			return 0, false, s
		case cut:
			return s.at, false, s
		case freeze:
			s.frozen = true
			return 0, false, s
		}
		s.gap = false
	}
	if nl < 0 {
		return 0, false, s // the line has not ended: it is read when it has
	}
	s.at += nl + 1
	return 0, true, s.read(rest[:nl])
}

// inFence reads a line of an open fence: it ends the block only if it closes it.
func (s Splitter) inFence(rest string, nl int) (int, bool, Splitter) {
	if nl < 0 {
		return 0, false, s
	}
	closes := closesFence(rest[:nl], s.fence, s.fenceN, s.flush)
	s.at += nl + 1
	if !closes {
		return 0, true, s
	}
	if !s.flush {
		s.fence = 0 // an indented fence is read through, and the block goes on
		return 0, true, s
	}
	if s.at > MaxChunk {
		s.frozen = true
		return 0, false, s
	}
	return s.at, false, s
}

// read takes a line into the block: the fence it opens, the list it belongs to,
// the HTML that freezes it.
func (s Splitter) read(line string) Splitter {
	s.content = true
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 {
		return s
	}
	text := line[indent:]
	if strings.HasPrefix(text, "<") {
		s.frozen = true
		return s
	}
	// An indented fence - an item's, or a top-level one - is read through but ends
	// nothing: the block goes on to where the list or the paragraph does.
	if ch, n := opensFence(text); n > 0 {
		s.fence, s.fenceN, s.flush = ch, n, indent == 0
		return s
	}
	if listMarker(text, true) == isMarker {
		s.list = true
	}
	return s
}

// verdictOn is what the line at the start of rest, which may be only its first
// characters so far, says about a cut above it.
func (s Splitter) verdictOn(rest string) verdict {
	if rest == "" {
		return wait
	}
	switch rest[0] {
	case ' ', '\t', '\r':
		if blankLine(rest) {
			return wait // it may yet turn out to be another blank line
		}
		return carry
	case '<':
		return freeze
	}
	if s.list {
		switch listMarker(rest, false) {
		case mayBeMarker:
			return wait
		case isMarker:
			return carry
		}
	}
	return cut
}

// listMarker reports whether text opens with a list marker. A complete line has all
// its characters, so its end is whitespace; a prefix does not.
func listMarker(text string, complete bool) marker {
	spaced := func(i int) marker { // a marker is followed by whitespace, or by the line's end
		switch {
		case i >= len(text) && !complete:
			return mayBeMarker
		case i >= len(text) || strings.IndexByte(" \t\r\n", text[i]) >= 0:
			return isMarker
		}
		return notMarker
	}
	if text == "" {
		return notMarker
	}
	switch c := text[0]; {
	case c == '-' || c == '*' || c == '+':
		return spaced(1)
	case c >= '0' && c <= '9':
		n := 1
		for n < len(text) && n <= maxMarkerDigits && text[n] >= '0' && text[n] <= '9' {
			n++
		}
		switch {
		case n > maxMarkerDigits:
			return notMarker
		case n >= len(text) && !complete:
			return mayBeMarker
		case n >= len(text) || (text[n] != '.' && text[n] != ')'):
			return notMarker
		}
		return spaced(n + 1)
	}
	return notMarker
}

// blankLine reports a line with nothing on it but whitespace.
func blankLine(line string) bool { return strings.TrimLeft(line, " \t\r") == "" }

// opensFence reports a line, at column 0, that opens a fence: the character and
// how many of it. A backtick fence's info string may not hold a backtick, which is
// what keeps a line of inline code from opening one.
func opensFence(text string) (byte, int) {
	if text == "" || (text[0] != '`' && text[0] != '~') {
		return 0, 0
	}
	n := len(text) - len(strings.TrimLeft(text, text[:1]))
	if n < 3 || (text[0] == '`' && strings.Contains(text[n:], "`")) {
		return 0, 0
	}
	return text[0], n
}

// closesFence reports a line that closes a fence of n of ch: the same character,
// at least as many, and nothing after them but whitespace. A fence opened at
// column 0 is closed at column 0; an indented one by a closer indented up to 3.
func closesFence(line string, ch byte, n int, flush bool) bool {
	t := strings.TrimRight(line, " \t\r")
	if !flush && len(t)-len(strings.TrimLeft(t, " ")) <= 3 {
		t = strings.TrimLeft(t, " ")
	}
	return len(t) >= n && strings.Trim(t, string(ch)) == ""
}

// Stack puts a block's rows under the rows above it the way Markdown draws two
// blocks: one blank row between them. A blank row the block above ends in is its
// own - the model's last blank line of code, a nested list's - and takes the
// separator all the same; the one exception is a horizontal rule, whose own
// trailing row is that blank row. It returns a new slice and leaves both arguments
// alone.
func Stack(above, rows []string) []string {
	out := make([]string, 0, len(above)+len(rows)+1)
	out = append(out, above...)
	if len(out) > 0 && len(rows) > 0 && !endsInRule(out) {
		out = append(out, "")
	}
	return append(out, rows...)
}

// endsInRule reports rows ending in a horizontal rule and the blank row glamour
// draws under it.
func endsInRule(rows []string) bool {
	n := len(rows)
	return n >= 2 && blankRow(rows[n-1]) && strings.TrimSpace(ansi.Strip(rows[n-2])) == horizontalRule
}
