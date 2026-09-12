package ui

import (
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// what says a move into working is a turn *resuming* rather than starting.
//
// Read off the daemon's own stateLocked rather than guessed at, and the two
// that are easy to get wrong are the two that decide this: **blocked** is a
// permission ask outstanding with the turn still owed, and **silent** is
// owed-and-quiet. Both are the same turn as the working either side of them.
// Treating either as a boundary restarts the turn's clock and throws away the
// tokens it has produced, so a permission answered halfway through leaves the
// row counting the suffix while the result frame states the whole.
//
// The others are boundaries by construction: idle is `!owed`, and parked, ended
// and orphaned have no process owing anything.
//
// Its domain is derived from stateGlyph by
// TestEveryStateTheRosterDrawsIsInFlightOrIsNot, so a seventh state is a
// decision somebody has to make here rather than one that silently reads as a
// boundary.
func turnInFlight(state string) bool {
	switch state {
	case rpc.StateWorking, rpc.StateBlocked, rpc.StateSilent:
		return true
	default:
		return false
	}
}

// countsAsUnread: everything the room draws is something you have not seen,
// except the words you typed yourself.
//
// The quiet marker counts, and that is the case worth stating. For 8 of 52
// recorded turns it is the only thing the room shows, so an agent whose turn
// said nothing would otherwise leave a line in the room with no badge anywhere
// saying it is there.
// A progress frame is not something you have not read either: it draws no line
// anywhere, and thirty working agents would otherwise drive every badge in the
// sidebar on their own. It never reaches this - fold returns nothing for it, and
// Observe only counts an event the room takes - but the pair is stated here
// because that is where the question is answered.
func countsAsUnread(kind core.EventKind) bool {
	return kind != core.KindUserText && kind != core.KindTurnTokens
}

// blank is text with nothing in it.
//
// The decoder does not drop an empty text block and dm_blocks.userBlock
// already renders one as nothing, so an empty run of prose would set spoke,
// suppress the quiet marker, and leave the turn showing nothing at all in the
// room - the failure the marker exists to prevent, arriving through the branch
// meant to prevent it.
func blank(s string) bool { return strings.TrimSpace(s) == "" }
