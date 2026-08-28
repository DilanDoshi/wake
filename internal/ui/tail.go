package ui

// The tiled board's live tail: App.tails, one partial preview per agent,
// accumulated only while the board is up and tiled. Split out once app.go
// crossed the 800-line hard max, write.go's own reason.

import (
	"maps"

	"github.com/DilanDoshi/wake/internal/core"
)

// withTail returns an App whose tails map is its own, one entry replaced.
// Mirrors withDM: the map is tiny (partial is two strings and an int, and
// the strings are bounded by partial.add), so copying it per token is cheap.
func (a App) withTail(id string, p partial) App {
	next := make(map[string]partial, len(a.tails)+1)
	maps.Copy(next, a.tails)
	next[id] = p
	a.tails = next
	return a
}

// foldTail accumulates one agent's live output tail while the tiled board is
// up, and clears it when the block lands or the turn ends - the DM preview's
// own triggers (dm.go), off the same partial type. Gated: when the wall is
// down or in rows, this is a no-op and App.tails stays empty.
func (a App) foldTail(sessionID string, ev core.Event) App {
	if !a.board.Up || !a.board.Tiled {
		return a
	}
	switch ev.Kind {
	case core.KindPartialText:
		return a.withTail(sessionID, a.tails[sessionID].add(ev.Text))
	case core.KindAssistantText, core.KindTurnEnd:
		if a.tails[sessionID].text == "" {
			return a
		}
		return a.withTail(sessionID, a.tails[sessionID].cleared())
	}
	return a
}
