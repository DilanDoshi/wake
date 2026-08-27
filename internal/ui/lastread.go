package ui

// The last-read marker: the boundary between what a reader had read when they
// left a conversation and what arrived while they were away.
//
// # Why it is not a scroll position
//
// The workflow this exists for is three research sessions running at once, each
// producing long replies that are read carefully. You leave one mid-reply, deal
// with another agent, and come back - and without a boundary you come back to
// the bottom of a wall of text with no idea where you stopped. The marker is
// what makes reading three long answers concurrently possible rather than
// merely permitted.
//
// A scroll offset cannot be that. transcript.scroll is an index into lines that
// a *width* change re-derives from scratch: DM.SetSize replaces every line
// beneath the boundary, so an offset that was right before the re-wrap points
// somewhere else afterwards while still looking like a valid position. Nothing
// fails; the boundary just quietly moves. That is the shape this project has
// been caught by twice on rendering code - an aggregate a re-wrap keeps
// "correct" while changing what it names.
//
// So the anchor is an **event index**, and the marker is a *block*. Blocks are
// what a re-wrap is derived from - DM.renderAll walks the events and rebuilds
// every one of them - so a marker emitted from there is regenerated in the same
// place at the new width, between the same two events, for free. There is one
// rule deciding where it goes (markerBefore) and both the incremental path and
// the re-wrap path ask it, which is what stops them drifting.
//
// # The anchor is plural, because the artefact is
//
// This shipped as one slot (`mark int`, `marked bool`) behind a transcript that
// holds one rule per absence, and that made the paragraph above **false in the
// case the feature exists for**: a re-wrap regenerated the newest rule and
// dropped every earlier one, and an ordinary glance-and-return - which cleared
// the single `marked` flag - left a later re-wrap emitting none at all. The rule
// that survived looked right. Nothing errored.
//
// It is the exact failure named two paragraphs up, arriving through the fix for
// it: an aggregate a re-wrap keeps "correct" while changing what it names. **A
// state that regenerates N artefacts has to hold N anchors.** marks is that,
// and the guard is TestEveryRuleSurvivesTheReWrap - a second absence *and* a
// width change, which is the intersection two separately-sound tests left
// unasserted.
//
// # Why the rule stays where it was drawn, rather than moving
//
// The messaging-app convention is one "unread" line that moves. It is the wrong
// shape here for a reason specific to this workflow: **moving it erases the
// landmark at the moment it is in use.** You are half way through a 400-line
// answer that arrived during an absence, with the rule above it; you glance at
// another agent; something lands during the glance; a moving marker relocates
// *below* the answer you are inside. The line you were reading back toward is
// gone, exactly when you are using it. A rule that stays is not subject to that,
// and the past-tense label follows: "you left off here" does not stop being
// true, where "new since you left" would quietly become false the next time
// somebody left.
//
// # What sets it
//
// Leaving a conversation, and nothing else. App.openDMWith and App.closeDM are
// the only two places a.focus stops naming a DM, and they are the same two places
// Fleet.Focus already marks that agent's arrivals read - so the badge in the
// sidebar and the rule in the transcript are two renderings of one fact rather
// than two accounts of it.
//
// A focus change is deliberately *not* leaving: with both panes drawn, a DM that
// has lost the keyboard is still on screen and still being read. What takes a
// conversation away from a reader is another one taking its pane.

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	// lastReadLabel names the boundary in the reader's own terms. It answers
	// "what am I looking at" rather than "what is this rule" - which is the
	// question somebody has when they come back to a pane that grew while they
	// were gone.
	//
	// Past tense, and that is load-bearing rather than a turn of phrase. The
	// rule is never moved, so a label in the present tense - "new since you
	// left" - would be a true statement that quietly became false the next time
	// the reader left. "You left off here" is true forever at the point it is
	// drawn.
	lastReadLabel = "you left off here"

	// lastReadRule is the glyph the boundary is drawn with: a rule rather than
	// a banner, because it separates two halves of one conversation and a
	// heavier marker would read as a third participant in it.
	lastReadRule = "─"

	// lastReadLead is how much rule sits to the left of the label. Short, so
	// the words start near the margin where the eye scanning back for them
	// already is, and non-zero so the line reads as a rule rather than as
	// something an agent said.
	lastReadLead = 2

	// maxLastReadRules is how many boundaries a conversation keeps.
	//
	// A decision rather than a threshold to tune, and the count is the one the
	// workflow names: three concurrent research sessions, so "here, and before
	// that here" stays legible and no further. Each rule costs two lines in
	// Accent - the loudest colour in the palette, and the one reserved for what
	// the reader is hunting for - and they do not spread evenly through a
	// transcript: every one sits exactly in the band a returning reader scrolls
	// back through. Past about ten the accent stops answering "where did I
	// stop" and starts answering "everywhere you have ever stopped", which is a
	// different and less useful question.
	//
	// The bound is on the *anchors*, so it is what a re-wrap restores the
	// transcript to. See markerBefore for the one asymmetry that leaves.
	maxLastReadRules = 3
)

// lastReadStyle draws the boundary in LastRead's purple.
//
// Loud rather than muted, and that is the one place this marker is allowed to
// be: it is a landmark somebody is actively hunting for in a pane of prose, and
// HintStyle exists for things that must recede.
//
// **Purple rather than the accent**, which it used to be. This pane is 1:1
// Claude Code everywhere else, and the accent is Claude's own colour - a rule
// drawn in it reads as something Claude said. The last-read boundary is the one
// thing on this surface that is Wake's and not Claude's, so it is the one thing
// that should not wear Claude's colour. See LastRead.
var lastReadStyle = lipgloss.NewStyle().Foreground(LastRead)

// Leave records where the reader stopped, so what arrives next is drawn under a
// boundary rather than merged into what they had already read.
//
// Nothing is drawn here: an absence nothing arrived during earns no rule, and
// that is not knowable until something arrives or does not. The mark is
// *pending* until an event lands on it - which is what Resume undoes and what
// markerBefore turns into a rule.
//
// A conversation with nothing in it records nothing. A rule above the first line
// a transcript ever carries has no "before" on the other side of it, so it reads
// as chrome rather than as a boundary - and it would be the common case, because
// a DM opened for the first time starts empty.
//
// **It also drops the preview, and that is not a second job.** Leaving is the
// one thing that happens on every path a pane stops being drawn on - closed,
// displaced by another conversation, or pushed off screen by a width that
// cannot afford it - which is exactly the set App.wants stops accumulating for.
// Freezing the tail without dropping it left a pane reopened mid-block
// appending to text from before it went away, with everything generated in
// between missing: a sentence the agent never wrote, in the one part of the
// pane that is meant to be a live picture of what it is writing now.
//
// Before the returns below, because they are about the *marker*. A conversation
// whose only content is a preview has no events to anchor one to and trips the
// first of them - which is the case this bug lives in, not an edge of it.
func (d DM) Leave() DM {
	d.partial = d.partial.cleared()
	at := d.events.len()
	if at == 0 {
		return d
	}
	if n := len(d.marks); n > 0 && d.marks[n-1] == at {
		return d // already pending here; leaving twice is one absence
	}
	d.marks = append(slices.Clone(d.marks), at)
	return d
}

// crossed applies the cap, at the moment a mark stops being pending and becomes
// a rule somebody can see.
//
// Here rather than in Leave, and that is not tidiness. Leaving does not know
// whether the absence will earn anything, so trimming on the way out charges a
// glance that earns nothing one of the boundaries the cap promises to keep -
// and Resume can remove the pending mark it added but cannot restore what the
// trim evicted. Evicting only when a rule is actually drawn makes the cap a
// statement about rules rather than about departures, and bounds marks at
// maxLastReadRules + 1: the boundaries drawn, plus at most one pending.
func (d DM) crossed() DM {
	if len(d.marks) > maxLastReadRules {
		d.marks = slices.Clone(d.marks[len(d.marks)-maxLastReadRules:])
	}
	return d
}

// Resume drops a mark nothing accumulated under.
//
// Leaving a conversation always records where the reader got to, because nothing
// at that moment knows whether anything will arrive. Coming back to find it
// exactly as it was is the answer: there was no absence to draw a line across,
// and without this the next thing the agent said - said while the reader was
// sitting in front of it - would get a rule above it claiming they had been away.
//
// Only the *pending* mark, which is why it is a trailing one equal to the
// transcript's length rather than a flag. A flag is what this was, and clearing
// it destroyed the anchors of every rule already drawn: after one glance a later
// width change emitted nothing at all.
func (d DM) Resume() DM {
	n := len(d.marks)
	if n == 0 || d.marks[n-1] != d.events.len() {
		return d
	}
	d.marks = slices.Clone(d.marks[:n-1])
	return d
}

// markerBefore is the boundary block that belongs above the event at index i,
// if a boundary is there.
//
// The one rule, asked by both the incremental append and the whole-transcript
// re-wrap. Two copies of this condition is how a marker ends up in one place
// while the pane is narrow and another place after it is widened.
//
// # The one asymmetry the cap leaves, at its real size
//
// Lines are append-only (see chunked), so a rule already rendered cannot be
// withdrawn from the transcript - only the anchors can be dropped. **The
// surplus is therefore unbounded until the next width change**: the transcript
// keeps one rule per absence for as long as nobody resizes, and a single
// re-wrap regenerates it from the anchors and drops all of them but the newest
// maxLastReadRules at once. Measured: ten absences with no resize leaves ten
// rules against three anchors, and one widen removes seven.
//
// It is stated at that size because the smaller framing - "a fourth absence
// keeps showing the rule it drew" - is the one somebody reads and predicts 4→3
// from, and this task has already been burned once by "bounded by construction"
// describing a bound that was really a bug.
//
// What the promise covers is the newest maxLastReadRules boundaries: those sit
// between the same two events at every width, always. Nothing that survives ever
// moves, which is the failure class the whole design exists to prevent - the
// re-wrap moves the screen *toward* the model rather than away from it, and only
// for boundaries the cap has already declared out of scope.
// TestTheCapIsWhatAReWrapRestoresTheTranscriptTo pins both halves so this is a
// decision rather than a surprise.
func (d DM) markerBefore(i int) (block, bool) {
	if !slices.Contains(d.marks, i) {
		return block{}, false
	}
	return block{text: lastReadLine(d.blockWidth())}, true
}

// lastReadLine draws the rule at a width, label and all.
//
// It is built to the width it is given rather than to the pane's, for the same
// reason every other block is: transcript.view cuts a line that is too wide for
// the window, and a marker that measured itself against the window would be the
// one block in the transcript whose length was not a function of the width it
// was rendered at.
func lastReadLine(width int) string {
	label := " " + lastReadLabel + " "
	tail := max(width-lipgloss.Width(label)-lastReadLead, 0)
	return lastReadStyle.Render(strings.Repeat(lastReadRule, lastReadLead) + label + strings.Repeat(lastReadRule, tail))
}
