package ui

// The tiled board's transcripts: one rendered DM per on-screen tile, seeded from
// disk and fed live while the tiled board is up, dropped whole when it closes.
// Isolated from the pane map (a.dms) - the ⇥ ring, App.wants and hideDM never see
// these, so a tile costs nothing where a pane is counted. It reuses the DM's
// render-once-cache machinery (transcriptWindow) so no glamour runs per frame:
// the cost is a render when a tile is first seeded or paged onto, and a re-wrap
// on a width change - work per change, not per frame. Guardrail 2 of the
// 2026-08-27 tiled-board design, revised 2026-09-01: a bounded transcript window,
// still view-only and unscrollable.

import (
	"maps"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// visibleBoardAgents is the agents whose tiles are drawn this frame - the same
// window tileView draws, so the built set and the drawn set cannot disagree.
func (a App) visibleBoardAgents() []Agent {
	agents := a.fleet.OnRoster()
	if len(agents) == 0 {
		return nil
	}
	g := a.boardTileGrid(len(agents))
	start := tileWindowStart(a.boardCursor(agents), len(agents), g.cols, g.rows)
	end := min(start+g.cols*g.rows, len(agents))
	return agents[start:end]
}

// ensureBoardDMs builds a board DM for each visible tile that lacks one - queuing
// a disk-history ask - and re-wraps one whose cell width has moved. A no-op off
// the tiled board, so calling it every Update is work per change: a stable width
// with every tile already built stores nothing.
func (a App) ensureBoardDMs() App {
	if !a.board.Up || !a.board.Tiled {
		return a
	}
	inner, rows := a.tileInner()
	for _, ag := range a.visibleBoardAgents() {
		d, ok := a.boardDMs[ag.ID]
		if !ok {
			nd := NewDM(ag.ID, ag.Name)
			nd, _ = nd.transcriptWindow(inner, rows) // set the width so the draw never re-wraps
			a = a.withBoardDM(ag.ID, nd).askBoardHistory(ag.ID)
			continue
		}
		if d.width != inner { // a resize: re-wrap once here, off the draw path
			nd, _ := d.transcriptWindow(inner, rows)
			a = a.withBoardDM(ag.ID, nd)
		}
	}
	return a
}

// tileInner is the inner width and body-row budget of one tile at the current
// grid - the same arithmetic tileBody applies, so a board DM is sized to exactly
// what the draw asks for and the draw never re-wraps.
func (a App) tileInner() (inner, rows int) {
	g := a.boardTileGrid(len(a.fleet.OnRoster()))
	return max(g.cellW-boxFrameWidth, 1), max(g.cellH-tileFrameRows, minTileTailRows)
}

// withBoardDM returns an App whose boardDMs map is its own, one entry replaced -
// withDM's rule, one map over.
func (a App) withBoardDM(id string, d DM) App {
	next := make(map[string]*DM, len(a.boardDMs)+1)
	maps.Copy(next, a.boardDMs)
	next[id] = &d
	a.boardDMs = next
	return a
}

// askBoardHistory records a disk-history ask for a board DM and queues the
// FrameHistory write - askHistory's board twin, sharing the one wire and fold
// (historyArrived routes a non-pane reply to boardHistoryArrived). The event
// count at ask time is what makes "nothing arrived since" checkable.
func (a App) askBoardHistory(id string) App {
	if _, asked := a.boardHistoryAsked[id]; id == "" || asked {
		return a
	}
	next := make(map[string]int, len(a.boardHistoryAsked)+1)
	maps.Copy(next, a.boardHistoryAsked)
	next[id] = a.boardDMs[id].events.len()
	a.boardHistoryAsked = next
	a.pendingHistory = append(append([]string(nil), a.pendingHistory...), id)
	return a
}

// boardHistoryArrived folds a disk-history reply into a board DM, with
// historyArrived's own guards: dropped if the DM is gone or if anything arrived
// since the ask (which would double-render an event that is on disk and live).
func (a App) boardHistoryArrived(f rpc.Frame) App {
	d, ok := a.boardDMs[f.SessionID]
	if !ok || len(f.Events) == 0 {
		return a
	}
	held, asked := a.boardHistoryAsked[f.SessionID]
	if !asked || d.events.len() != held {
		return a.forgetBoardHistoryAsk(f.SessionID)
	}
	return a.withBoardDM(f.SessionID, d.Before(f.Events))
}

// forgetBoardHistoryAsk lets a board DM be asked about again - the drop path when
// the reply is stale, so a paged-back tile re-reads rather than staying empty.
func (a App) forgetBoardHistoryAsk(id string) App {
	next := make(map[string]int, len(a.boardHistoryAsked))
	for k, v := range a.boardHistoryAsked {
		if k != id {
			next[k] = v
		}
	}
	a.boardHistoryAsked = next
	return a
}

// foldBoard appends one agent's event to its board DM while the tiled board is
// up - foldTail's successor, feeding a whole transcript rather than a one-block
// tail. Gated on a board DM already existing, which is what bounds the render to
// visible tiles: a paged-off agent has none, so its blocks are not rendered until
// the cursor pages it back and ensureBoardDMs seeds it from disk.
func (a App) foldBoard(sessionID string, ev core.Event) App {
	if !a.board.Up || !a.board.Tiled {
		return a
	}
	d, ok := a.boardDMs[sessionID]
	if !ok {
		return a
	}
	return a.withBoardDM(sessionID, d.Append(ev))
}
