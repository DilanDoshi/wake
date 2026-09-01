package ui

// The words a Wake agent shows when a turn is done. Wake's own, written for Wake
// - the same rule heartbeatwords.go argues at length: a word list is a creative
// compilation, so Wake does not borrow Claude Code's. "Cooked" is a plain past
// tense verb and nobody owns it; the selection and arrangement here are ours. Do
// not "improve" this list by sampling Claude's.
//
// These are the past-tense mirror of the working pool's themes - a wake is what a
// ship leaves behind (the nautical run), the name is a dawn (the dawn run), and
// the kitchen run is the product's own joke - each read to sit after "for 1m 59s"
// on the done line: "Cooked for 1m 59s", "Sailed for 1m 59s".
//
// Shorter than the 205-word working pool on purpose: a working word is drawn by
// every one of 15-30 agents at once, where the done line is one agent's own in
// its DM, so a repeat is neither side-by-side nor frequent. A few plain words
// (Finished, Computed, Sorted) stay for heartbeatwords.go's reason - a pool with
// no straight word never lets the line be plain. TestDonePoolIsWellFormed holds
// the floor and the shape.
var doneWords = []string{
	"Anchored", "Assembled",
	"Baked", "Berthed", "Brewed", "Bundled", "Buttered",
	"Caffeinated", "Calibrated", "Charted", "Churned", "Computed", "Cooked",
	"Dawned", "Decanted", "Distilled", "Docked", "Dredged", "Drifted",
	"Faffed", "Fathomed", "Ferried", "Ferreted", "Finished", "Foraged", "Fossicked", "Frothed",
	"Gleamed", "Glimmered", "Glinted",
	"Helmed", "Hoisted",
	"Kindled", "Kneaded",
	"Landed",
	"Marshalled", "Moored",
	"Navigated",
	"Portaged", "Processed", "Proofed",
	"Quenched",
	"Reckoned", "Rekindled", "Rigged", "Roasted", "Rowed", "Rummaged",
	"Sailed", "Sealed", "Settled", "Simmered", "Sorted", "Sounded", "Spliced", "Squared",
	"Steeped", "Stitched",
	"Tacked", "Tallied", "Threaded", "Toasted", "Trawled",
	"Whisked", "Wrapped",
}

// doneWord is the word an agent shows for one finished turn. The caller passes a
// seed that is constant for the turn - turnSeed on the turn's start - so the word
// is chosen once and does not flicker while the done line stands.
func doneWord(seed uint64) string {
	return doneWords[seed%uint64(len(doneWords))]
}
