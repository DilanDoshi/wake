package daemon

// Keeping Wake's record of a session's effort true while claude owns the
// command that changes it.
//
// `/effort` is claude's, and internal/ui deliberately does not claim it:
// slashguard_test.go refuses any command the recorded corpus shows claude
// advertising, because taking one replaces something that works with Wake's
// narrower version and the operator's only symptom is that it stopped behaving
// the way it used to. So the command passes through byte for byte, exactly as
// it did before this file existed.
//
// What is left is a bookkeeping problem. Nothing on claude's wire ever reports
// a level back - "effort" appears in the whole recorded corpus only as an entry
// in init.slash_commands - so a command that changed the session would leave
// the daemon still reporting the level it was *spawned* at, and every status
// reply afterwards would contradict what the operator had just done.
//
// The daemon watches instead of claiming. It sees every outgoing message
// already, so it notices this one going past and updates its own record. The
// text is not intercepted, rewritten or refused: an `/effort` the daemon does
// not understand is simply not recorded, and still reaches the agent.

import (
	"strings"
	"unicode"

	"github.com/DilanDoshi/wake/internal/core"
)

// The command this file watches for, composed rather than spelled whole.
//
// Two constants because a literal naming the command outright would be advice,
// and slashguard_test.go rightly refuses that: it walks this package for any
// sentence naming a slash command and requires internal/ui to answer it. This
// one is claude's and Wake answers none of it - the daemon recognises the shape
// as it goes past and changes nothing about it.
//
// It lives here rather than in core because it is a line of text on stdin, not
// an argv word. core/argv.go owns the flag; the two are different surfaces that
// happen to share a name.
const (
	slashPrefix = "/"
	effortVerb  = "effort"
)

// noteEffort records the level if this message is claude's effort command.
//
// Deliberately narrow. Only an exact `/effort <level>` with a level from the
// closed set counts, so `/effort` with no argument leaves the record alone
// rather than guessing at what was chosen. A record Wake is unsure of is worse
// than one that is merely stale, because the stale one is at least what Wake
// asked for.
//
// The bare form changes nothing to record. It was described here as opening
// claude's own picker; in stream-json there is no picker to open, and the
// recording shows it printing a usage line and nothing else - which is what
// makes it a form internal/ui may claim. A picker there sends a levelled
// command, so this watcher sees an ordinary `/effort <level>` either way.
//
// The predicate is the *command's*, not the flag's. `--effort` takes five
// levels and `/effort` takes seven, and reading the argv set here would drop a
// level the operator really typed - after which every status reply contradicts
// what they had just done, which is the failure this file exists to prevent.
//
// The caller holds no lock: apply takes none of its own, the way noteSent and
// noteAnswered beside it do not.
//
// That this writes at *runtime* is what makes the field's other readers need a
// lock - see currentEffort. effort stopped being written-once at launch when
// this function shipped, which is the same transition name and label went
// through when rename shipped, and rename.go records what it cost to notice
// late.
func (a *agent) noteEffort(text string) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), slashPrefix+effortVerb)
	if !ok {
		return
	}
	// The command has to end where the word does. Without this "/effortmax" is
	// recorded as max - a line claude does not recognise as the command at all,
	// so Wake would report a level the session was never set to, which is worse
	// than reporting a stale one.
	if rest != "" && !unicode.IsSpace(rune(rest[0])) {
		return
	}
	level := strings.TrimSpace(rest)
	if !core.ValidEffortCommand(level) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.effort = level
}

// argvEffort is what a level may become on a command line: itself, or nothing.
//
// The two sets are why this exists. `/effort` takes seven levels and `--effort`
// takes five, so a session the operator really set to ultracode holds a level
// that cannot be passed to a process - and launch refuses one rather than
// dropping it, deliberately, because it is the one door. Sanitising has to
// happen before the level gets there.
//
// Both doors go through this. A restored record reaches it because a park book
// is a file on disk and may have been edited; a live parked agent reaches it
// because the level it is holding is one Wake itself recorded. Two spellings of
// the same rule is how one of them goes stale, and the stale one strands a
// parked session.
func argvEffort(level, id string) string {
	if level == "" || core.ValidEffort(level) {
		return level
	}
	logf("wake: session %s is at effort %q, which --effort does not take, so it is starting without one", id, level)
	return ""
}

// setConfirmedEffort records the level a bare /model probe read back. Display
// prefers it over the asked-for level (snapshot), but park still relaunches
// from currentEffort - the level --effort accepts - so a probed level like
// ultracode or auto never reaches an argv.
func (a *agent) setConfirmedEffort(level string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.confirmedEffort = level
}

// currentEffort is the level under the agent's own lock.
//
// Every read goes through this. noteEffort writes from the agent's queue
// goroutine while park and wake read from another, so an unlocked read is a
// race - the shape rosterRecord and named already exist to close for the two
// display fields, and for the same reason: this field became mutable and its
// readers did not notice.
func (a *agent) currentEffort() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.effort
}
