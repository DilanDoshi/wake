package render

// Where a streamed answer's finished blocks end: a pure function of the text read
// so far, with no markdown parser, that cuts only where what is above cannot depend
// on what comes after. The argument and the numbers are in docs/notes/decisions.md
// (2026-08-15, second amendment of 2026-10-07).
//
// A cut falls:
//
//   - at a blank line outside a fence, once the next line has started and sits at
//     column 0 - never before an indented line, `<`, or (in a block holding a list)
//     a list marker, and a definition (": text") goes on with its term;
//   - just after the closing line of a fence opened at column 0 (same character,
//     at least as long, at column 0: a closer indented 1-3 spaces is valid
//     CommonMark but is not read, so the block stays raw - the safe direction).
//
// Never inside a fence; an indented one is read through and ends nothing. A line
// opening with `<` (a <pre> runs through blank lines), or a line past MaxChunk,
// freezes the block raw. The decision is made on a prefix, so the same text cuts the
// same way however its tokens arrived.
//
// Transient, since the answer lands whole: a reference or footnote definition below
// changes how a use above renders; a fence in a list item whose body drops back to
// column 0 is closed by CommonMark where this reads on; a bare file name glamour
// links (`tally.txt`) wraps a word earlier than Prose does.

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
	at      int32 // the start of the first line not read yet
	fenceN  int32 // how many of it opened the fence
	fence   byte  // the character of the open fence, or 0
	flush   bool  // the fence opened at column 0, so its closer ends the block
	content bool  // the block holds a line that is not blank
	list    bool  // ... and one opening with a list marker at indent 3 or less
	def     bool  // the block's last line is a definition: ": text"
	gap     bool  // blank lines follow the block's last line: a cut may fall at `at`
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
	if s.at > MaxChunk {
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
		s.at += int32(nl + 1)
		return 0, true, s
	}
	if s.gap {
		switch s.verdictOn(rest) {
		case wait:
			return 0, false, s
		case cut:
			return int(s.at), false, s
		case freeze:
			s.frozen = true
			return 0, false, s
		}
		s.gap = false
	}
	if nl < 0 {
		return 0, false, s.unended(rest) // the line has not ended: it is read when it has
	}
	s.at += int32(nl + 1)
	return 0, true, s.read(rest[:nl])
}

// unended freezes on a line still being written past MaxChunk. Only that line, never
// everything unread, so a whole answer read at once cuts where its stream would.
func (s Splitter) unended(line string) Splitter {
	s.frozen = s.frozen || len(line) > MaxChunk
	return s
}

// inFence reads a line of an open fence: it ends the block only if it closes it.
func (s Splitter) inFence(rest string, nl int) (int, bool, Splitter) {
	if nl < 0 {
		return 0, false, s.unended(rest)
	}
	closes := closesFence(rest[:nl], s.fence, int(s.fenceN), s.flush)
	s.at += int32(nl + 1)
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
	return int(s.at), false, s
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
	s.def = strings.HasPrefix(text, ": ") || strings.HasPrefix(text, ":\t")
	if strings.HasPrefix(text, "<") {
		s.frozen = true
		return s
	}
	// An indented fence - an item's, or a top-level one - is read through but ends
	// nothing: the block goes on to where the list or the paragraph does.
	if ch, n := opensFence(text); n > 0 {
		s.fence, s.fenceN, s.flush = ch, int32(n), indent == 0
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
	case ':': // a definition goes on with its term
		if len(rest) == 1 {
			return wait
		}
		if rest[1] == ' ' || rest[1] == '\t' {
			return carry
		}
	}
	if s.def {
		return carry // the next term of the same definition list
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
