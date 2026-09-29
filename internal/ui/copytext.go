package ui

// What a transcript copy gives back. A pane keeps only its drawn rows, so a copy
// has to undo the wraps that drew them: markdown's through render.Rejoins, the
// operator's own turn by matching its rows back to what was typed. Every other
// row - a tool's output, a diff, a label - copies as drawn.

import (
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"

	"github.com/charmbracelet/x/ansi"
)

// rejoin is how a block's rows go back together on the clipboard.
type rejoin int

const (
	rowsAsDrawn  rejoin = iota // every row break is kept
	markdownRows               // render.Markdown drew them
	typedRows                  // shadedOwn drew the operator's text
)

// textRows is one block in transcript.texts: where it ends, and how it rejoins.
type textRows struct {
	end   int
	how   rejoin
	typed string
}

// copiedAs is how the DM draws an event, as far as a copy cares. A subagent's
// block sits inside a gutter no rule here reads past, so it copies as drawn.
func copiedAs(ev core.Event) (rejoin, string) {
	switch {
	case ev.Subagent != nil:
		return rowsAsDrawn, ""
	case ev.Kind == core.KindAssistantText, ev.Kind == core.KindUserText && ev.Echoed:
		return markdownRows, ""
	case ev.Kind == core.KindUserText:
		return typedRows, ev.Text
	}
	return rowsAsDrawn, ""
}

// hardBreak is how a row follows the one above it when nothing proves a wrap.
var hardBreak = render.Rejoin{Sep: "\n"}

// rejoins is how each of lines - what selectionLines returned, lines[0] at
// absolute index first - follows the row above it.
func (t transcript) rejoins(lines []string, first int) []render.Rejoin {
	out := make([]render.Rejoin, len(lines))
	for i := range out {
		out[i] = hardBreak
	}
	for from, span := range t.texts {
		from = max(from, t.lines.first())
		if span.end <= first || from >= first+len(lines) {
			continue
		}
		rows := t.lines.slice(from, span.end)
		var js []render.Rejoin
		switch span.how {
		case markdownRows:
			js = render.Rejoins(rows)
		case typedRows:
			js = typedRejoins(rows, span.typed)
		}
		for k, j := range js {
			if i := from + k - first; i >= 0 && i < len(out) {
				out[i] = j
			}
		}
	}
	return out
}

// typedRejoins matches the rows shadedOwn drew back to the text they came from:
// each row follows exactly the whitespace its wrap consumed, a typed newline
// included. Rows before the first that opens the text (the DM's "you" label)
// are kept as drawn. nil when the rows do not match - the copy then keeps every
// row break, which is never worse than what was drawn.
func typedRejoins(rows []string, typed string) []render.Rejoin {
	src := strings.TrimSpace(typed)
	out := make([]render.Rejoin, len(rows))
	pos := -1 // where in src the rows have reached; -1 before the body
	for i, row := range rows {
		plain := strings.TrimRight(ansi.Strip(row), " ")
		lead := min(bodyIndent, len(plain)-len(strings.TrimLeft(plain, " ")))
		text := plain[lead:]
		if pos < 0 {
			out[i] = hardBreak
			if lead == bodyIndent && text != "" && strings.HasPrefix(src, text) {
				out[i].Lead, pos = lead, len(text)
			}
			continue
		}
		sep, ok := consumed(src[pos:], text)
		if !ok {
			return nil
		}
		out[i] = render.Rejoin{Sep: sep, Lead: lead}
		pos += len(sep) + len(text)
	}
	return out
}

// consumed is the whitespace at the head of rest that a wrap took before a row
// reading text: a blank row takes one typed newline, any other row the longest
// whitespace run it can while its own leading spaces still match.
func consumed(rest, text string) (string, bool) {
	ws := rest[:len(rest)-len(strings.TrimLeft(rest, " \t\n"))]
	if text == "" {
		n := strings.IndexByte(ws, '\n')
		return ws[:n+1], n >= 0
	}
	for k := len(ws); k >= 0; k-- {
		if strings.HasPrefix(rest[k:], text) {
			return ws[:k], true
		}
	}
	return "", false
}
