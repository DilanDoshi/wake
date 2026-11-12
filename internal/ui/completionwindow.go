package ui

// The completion menu's window: the rows of its offers a pane draws, and the line
// under them. offers is every match to the ceiling; a pane clips a menu block from
// the bottom, so the window is sized to the rows the pane has (a window taller
// than that keeps the cursor on a row nobody sees, and ↵ takes it unseen) and
// moves the least distance that shows the cursor (decisions.md, 2026-11-12).

import (
	"slices"
	"strconv"
	"strings"
)

// windowed sizes the open menu's window to its pane and scrolls it to the cursor.
// It runs where the offers, the draft or the geometry change - recompleted, a
// read or git landing, resizePanes - and never in a draw; a walk only scrolls
// (windowedTo). A card over the menu takes none of its rows: the page scrolls
// to the box instead (pagescroll.go), and a picker closes the draft.
func (a App) windowed() App {
	rows := completionRows
	if id := a.completion.pane; a.completion.open() {
		if w, h, ok := a.paneSize(id); ok {
			rows = clamp(a.menuRoom(id, w, h)-1, 1, completionRows) // one row is the key line
		}
	}
	a.completion = a.completion.windowedTo(rows)
	return a
}

// windowedTo is the menu drawing rows offers, from the least move of top that
// shows the cursor, kept inside the offers.
func (c completion) windowedTo(rows int) completion {
	c.rows = rows
	c.top = clampScroll(band{top: c.cursor, h: 1}.reveal(c.top, rows), len(c.offers), rows)
	return c
}

// View draws it, through the same rows a card and the picker draw.
func (c completion) View(width int) string {
	shown := c.offers[c.top:min(c.top+c.rows, len(c.offers))]
	rows := make([]string, 0, len(shown)+1)
	for i, offer := range shown {
		rows = append(rows, optionRow(c.rowLabel(offer, width), width, c.top+i == c.cursor, false, CompletionStyle))
	}
	return strings.Join(append(rows, detailRow(c.keyLine(), width)), "\n")
}

// keyLine is the menu's own advertisement, with what the window leaves out folded
// into it, and which way - two rows for that would be a row per menu spent on
// chrome. Below is what ↓ reaches; more is what only typing does (matches past
// the ceiling, names git never indexed), and a cut listing asks for typing too.
func (c completion) keyLine() string {
	below := len(c.offers) - c.top - min(c.rows, len(c.offers)-c.top)
	counts := []string{countWords(c.top, below)}
	if c.more > 0 {
		counts = append(counts, "+"+strconv.Itoa(c.more)+" more")
	}
	line := joinedWords(" · ", counts...)
	if c.more > 0 || c.cut {
		line = joinedWords(" - ", line, keepTyping)
	}
	if line == "" {
		return completionKeys
	}
	return line + hintSep + completionKeys
}

// joinedWords is the parts that say something, joined.
func joinedWords(sep string, parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), sep)
}
