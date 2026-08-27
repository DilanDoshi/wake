package ui

// The room's working line, drawn minimal: `✻ Sailed for 49s`, where the DM keeps
// Claude's fuller `✻ Sailing… (49s · ↓ 11.6k tokens)`. The room is the fleet
// glance, so it drops the token clause and the ellipsis and leans on a word.
//
// # Why these words
//
// Past tense, and nautical or dawn to a word, because both are Wake's own joke:
// a wake is what a ship leaves behind, and a wake is a morning. They are written
// rather than sampled - heartbeatwords.go argues why the pool must be ours, and
// the same rule holds here. Do not "improve" this list from Claude's.
//
// # Why it is short
//
// heartbeatwords.go holds 205 because a thirty-agent fleet draws one word each
// and a short pool puts the same word on screen three times. The room draws
// exactly one - roomWorkingLine names the single oldest turn - so the only job
// here is variety from one turn to the next, which a curated pool does at a
// fraction of the length. TestRoomWordPoolIsWellFormed holds the floor at 40.

import "time"

// roomFor is the connector between the word and the age, in place of the DM
// line's ellipsis-and-parenthesis.
const roomFor = " for "

var roomWorkingWords = []string{
	// Nautical - a wake is a ship's trail.
	"Sailed", "Anchored", "Moored", "Charted", "Sounded", "Trawled", "Rigged",
	"Tacked", "Navigated", "Hoisted", "Dredged", "Ferried", "Docked", "Drifted",
	"Crested", "Helmed", "Jibed", "Buoyed", "Becalmed", "Portaged", "Paddled",
	"Rowed", "Fathomed", "Voyaged", "Weathered", "Steered", "Piloted", "Reefed",
	"Furled", "Coasted", "Berthed", "Ebbed", "Swelled",
	// Dawn - a wake is a morning.
	"Dawned", "Woke", "Yawned", "Awakened", "Roused", "Kindled", "Rekindled",
	"Stirred", "Rose", "Brightened", "Glimmered", "Gleamed", "Glinted",
	"Surfaced", "Bloomed", "Warmed", "Reddened", "Glowed",
}

// roomWorkingWord is the word an agent shows in the room for one turn: its own
// activeForm when it wrote one - the same fallback the DM's workingLine uses -
// else a past-tense word held for the turn by seeding on the turn's start.
func roomWorkingWord(id, doing string, started time.Time) string {
	if doing != "" {
		return doing
	}
	return roomWorkingWords[turnSeed(id, started)%uint64(len(roomWorkingWords))]
}

// roomHeartbeatLine is the room's minimal working line, bounded to width: the
// glyph, the turn's word, and how long it has run, with no ellipsis and no
// parenthesised token clause. It is heartbeatLine one clause shorter, and drawn
// the same way - the head (`✻ Sailed`) shimmers as the alive signal, and " for
// 49s" is dim chrome about the turn.
func roomHeartbeatLine(word string, elapsed time.Duration, width int) string {
	head := heartbeatGlyph(elapsed) + " " + word
	meta := roomFor + elapsedText(elapsed)
	return boundedShimmerLine(head, elapsed, meta, width)
}
