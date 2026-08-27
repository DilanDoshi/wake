// `wake status`: what is running, and the third answer nobody remembers to
// write.

package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The columns a session's line is laid out in. Padded rather than tabbed so
// the output is the same whatever a terminal's tab stops are.
//
// titleColumn is the widest title a daemon can produce from its own pool: a
// six-character pooled name, the separator, and a label capped at eighteen. A
// name somebody chose can be longer and pushes the rest of its row right, which
// is their choice - these are columns for scanning thirty rows, not a table
// with a border to break.
const (
	titleColumn = 28
	stateColumn = 9
	idColumn    = 8
)

const unnamed = "(unnamed)"

// nameLabelSeparator joins a session's two halves: `sydney <> dev-5748`, per
// spec §5. Spelled once, because the DM's header and this listing have to agree
// about what an agent is called.
const nameLabelSeparator = " <> "

// printStatus asks the daemon what is running and writes it out.
//
// The exchange goes through daemon.Status rather than through frames written
// here. FrameStatusReply is both the answer to a request and an unsolicited
// push the daemon sends on every state change, with nothing on the frame to
// tell them apart - so a client that writes FrameStatus and reads the next
// reply can be handed one that predates its own question. Keeping that one
// exchange in one place is what lets it be fixed once.
func printStatus(socket string, out io.Writer) error {
	st, err := daemon.Status(socket)
	if err != nil {
		return fmt.Errorf("asking what is running: %w", err)
	}
	_, err = io.WriteString(out, formatStatus(st))
	return err
}

// formatStatus renders a fleet report. It answers three questions, and the
// third is the one that is easy to leave out.
func formatStatus(st rpc.Status) string {
	var b strings.Builder

	switch {
	case st.Running:
		fmt.Fprintf(&b, "wake daemon running (pid %d) on %s\n", st.PID, st.Socket)

	case len(st.Sessions) > 0:
		// Not "nothing is running". This is a fleet whose daemon died, read
		// back off the roster it left on disk, and it is the case that roster
		// exists for: 15-30 process trees with nobody holding a handle.
		fmt.Fprintf(&b, "No daemon is running, but a previous one left %s behind.\n", agents(len(st.Sessions)))
		b.WriteString("Nothing is holding them. Starting a daemon with `wake` reaps what it can identify.\n")

	default:
		return "No daemon is running.\n"
	}

	if len(st.Sessions) == 0 {
		b.WriteString("No sessions.\n")
		return b.String()
	}
	names := sessionNames(st)
	for _, s := range st.Sessions {
		b.WriteString(sessionLine(s, names))
	}
	return b.String()
}

// sessionNames indexes the report by id, so a row can name another row - and
// only where that name still points at one session.
//
// **A name is never an address**, and this is the listing where that bites. A
// name goes back to the pool when its session ends while the ending stays in the
// report for up to recentEndings rotations, so a fork's parent can end, a new
// session can draw the same name, and both rows are in the same report under it.
// Printing `forked from sydney` then names a live agent that is not the parent -
// and `wake attach sydney` resolves that live one, because pickOne prefers live
// over ended, so there is no way from the listing back to the actual parent. A
// reused name is therefore left out entirely and forkedFrom falls through to the
// short id, which is what the id column prints and what wake attach resolves.
//
// Built once per report rather than searched per row: at 30 sessions a scan per
// line is quadratic for a listing, which is the "work per frame that could be
// work per change" shape the non-negotiables name, arriving in a command
// instead of a draw loop. Two passes are still one pass each.
func sessionNames(st rpc.Status) map[string]string {
	holders := make(map[string]int, len(st.Sessions))
	for _, s := range st.Sessions {
		if s.Name != "" {
			holders[s.Name]++
		}
	}
	names := make(map[string]string, len(st.Sessions))
	for _, s := range st.Sessions {
		if s.Name != "" && holders[s.Name] == 1 {
			names[s.ID] = s.Name
		}
	}
	return names
}

// forkedFrom is the ancestry half of a row: which session this one was branched
// from, by name while the report still holds it and by the short id once it
// does not.
//
// Nothing on Claude's wire carries this. It is Wake's own memory of the fork,
// held for as long as the daemon holds the fork itself.
func forkedFrom(s rpc.SessionStatus, names map[string]string) string {
	if s.ParentID == "" {
		return ""
	}
	if name, ok := names[s.ParentID]; ok {
		return "  forked from " + name
	}
	return "  forked from " + shortID(s.ParentID)
}

// sessionLine is one agent's row: who, what state, which session, what it came
// from, and then whatever that state owes an explanation for.
func sessionLine(s rpc.SessionStatus, names map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %-*s %-*s %-*s", titleColumn, sessionTitle(s), stateColumn, s.State, idColumn, shortID(s.ID))

	if s.QuietMS > 0 {
		fmt.Fprintf(&b, "  quiet %s", quiet(s.QuietMS))
	}
	if len(s.RequestIDs) > 0 {
		// A blocked session is stopped dead until somebody answers, and these
		// are the correlators an answer needs - one per outstanding ask.
		fmt.Fprintf(&b, "  waiting on %s", strings.Join(s.RequestIDs, ", "))
	}
	b.WriteString(forkedFrom(s, names))
	if s.Error != "" {
		// Not necessarily a crash: a clean exit becomes an error whenever
		// something the agent spawned held stderr past core's bound.
		fmt.Fprintf(&b, "  %s", s.Error)
	}
	// Contained on the way out, and this row is outside the TUI rather than
	// exempt from it: the stderr tail above is a *grandchild's* bytes, and a
	// terminal executes an escape sequence printed to stdout exactly as it
	// executes one drawn on the alt screen. docs/notes/bugs.md BUG-9.
	return daemon.OneLine(trimmed(b.String())) + "\n"
}

// displayName is the handle an agent is addressed by: its name, and never the
// label. It is what the DM header and the notice row both draw, and they draw
// the same string so that one agent does not read as two.
//
// It stays bare on purpose. Spec §7 routes on `@name`, and §8's roster draws
// `@sydney`; a handle with a branch glued to it is not a handle. The labelled
// form is sessionTitle, and it belongs where sessions are being *listed* rather
// than addressed.
func displayName(s rpc.SessionStatus) string {
	if s.Name == "" {
		return unnamed
	}
	return s.Name
}

// sessionTitle is a session's whole identity in one column: `sydney <> dev-5748`.
//
// The label half is dropped when there is not one, rather than shown as an
// empty field. A trailing separator would read as a label that failed to load,
// and a session started somewhere that names nothing simply has no second half.
func sessionTitle(s rpc.SessionStatus) string {
	if s.Label == "" {
		return displayName(s)
	}
	return displayName(s) + nameLabelSeparator + s.Label
}

// shortID is enough of a session id to recognise it by, and now also enough to
// use: `wake attach` takes a unique prefix, which is what makes this column
// something to copy rather than something to squint at. The whole id is a UUID
// and nobody should have to type one.
func shortID(id string) string {
	if len(id) <= idColumn {
		return id
	}
	return id[:idColumn]
}

// quiet is how long a session has said nothing, rounded to something a person
// reads rather than a millisecond count.
func quiet(ms int64) time.Duration {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return d.Round(100 * time.Millisecond)
	}
	return d.Round(time.Second)
}
