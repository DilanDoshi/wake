// The two questions a room asks this machine, and who answers them.
//
// `internal/ui` may not import `internal/daemon` - a view may not depend on the
// daemon, and discovery reads claude's **on-disk transcript**, a second Claude
// format contained to one file across the whole tree. So `/adopt` cannot look
// for itself, and `ui.Sessions` is the seam it is handed instead.
//
// This is the implementation, and it is deliberately thin: both halves are the
// functions `wake import` already uses. One picker and one resolver in the
// build rather than two, which matters most on the row the two would drift on
// first - a session with no provable directory, listed with its reason in the
// directory's place.
//
// It is the same arrangement as the dialer `cmd/wake` hands the same model, for
// the same reason: which socket, which directory and what is on this disk are
// all questions this package already answers, and a second answer inside a view
// is the parallel implementation this project forbids.

package main

import (
	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/ui"
)

// adoptRows bounds the picker drawn in a pane.
//
// The shell prints every row into a scrolling terminal, which is right there
// and wrong in a room: the recording machine has 428 transcripts and a
// transcript is where somebody's conversation lives. Ten is the count that
// answers the question actually being asked - *"the sessions I have been
// working in"* - because the listing is ordered by recency and nothing else,
// and 93 of every 97 rows further down are transcripts whose directory has been
// deleted.
//
// It bounds the **rows**, not the bytes: a row is one session and half a
// session is not one.
const adoptRows = 10

// machineSessions answers ui.Sessions out of this package's own two functions.
//
// A type with no state because there is none to hold: every answer is a fresh
// walk of ~/.claude/projects, which is the only thing that can be true about a
// directory other processes are writing to. Caching it would be a picker that
// stopped listing a session somebody started a minute ago - and the walk
// happens on a tea.Cmd's goroutine, never on the one that draws.
type machineSessions struct{}

// Listing is the picker, bounded for a pane and naming the room's own command.
//
// `ui.AdoptUsage` rather than a sentence written here: the word `/adopt` is
// internal/ui's, and every `/command` this build tells an operator to type is
// held to that package's own set by
// TestEverySlashCommandAnySentenceNamesIsOneThisPackageAnswers. A copy over
// here would be a claim about a command set this package cannot read.
func (machineSessions) Listing() (string, error) {
	found, err := daemon.Discoverable()
	if err != nil {
		return "", err
	}
	return formatImportable(found, ui.AdoptUsage, adoptRows), nil
}

// Resolve turns what an operator typed into whole session ids, in the order
// they typed them.
//
// **One walk for the whole set.** `/adopt a b c` is one keystroke and three
// resolutions, and importTarget walks every transcript on the machine per call
// - so asking it three times is three walks of 428 files for one draft. The
// listing is taken once and each word resolved against it.
//
// **The first word it cannot resolve refuses all of them**, and the sentence is
// importTarget's own: an ambiguous prefix names its candidates, an unmatched
// one says the listing exists. Partial success is the thing not to build here -
// the daemon refuses a source that is already in this fleet, so adopting two of
// three and refusing the third leaves a draft the operator cannot correct and
// retype.
//
// **Nothing here decides whether an import is allowed**, which is importTarget's
// own ruling read through a second surface. A session with no provable
// directory resolves exactly like any other, and the refusal it earns is the
// daemon's - the one that says where to open it instead.
func (machineSessions) Resolve(typed []string) ([]string, error) {
	found, err := daemon.Discoverable()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(typed))
	for _, who := range typed {
		src, err := resolveImportable(found, who)
		if err != nil {
			// Not wrapped: every one of resolveImportable's refusals already
			// quotes what was typed, and a second quoting of the same word is
			// noise on the row an operator reads once.
			return nil, err
		}
		ids = append(ids, src.ID)
	}
	return ids, nil
}
