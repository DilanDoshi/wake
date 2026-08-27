// `wake import [<id>]`: adopting a session Wake never started.
//
// The feature this exists for, in the owner's words: *"If you have a bunch of
// sessions open in terminals scattered or in cmux, be able to open a group chat
// among them - select which ones you would like to add - and then query them via
// the group chat."*
//
// **The picker is the listing, and the listing is deliberately disk-only.**
// Bare `wake import` dials nothing, starts no daemon and needs no fleet, which
// is not an economy: this is the verb somebody runs *before* they have a fleet,
// on the first day they try Wake, and a picker that could not answer without a
// running daemon would be asking them to build the thing they came here to
// populate.
//
// It selects one at a time rather than several, and that is a scope decision
// with a reason rather than a gap. Each import is a name, a process and a turn's
// worth of somebody's money, and there is no live cap anywhere in this build -
// so `wake import --all` across 428 transcripts is the failure mode
// `mcpguard_test.go` refuses a spawn tool for, arriving at the shell where
// nobody would think to refuse it. Repeat the verb.

package main

import (
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// importSession adopts a transcript on this machine and opens it.
//
// The id is resolved against the disk before anything is dialled, for the
// reason forkSession resolves its parent first: connect() forks a daemon when
// nothing is listening, so dialling first would answer "there is no such
// transcript" by starting a daemon that was never going to know.
//
// **The id it waits on is the new session's, never the source's** - `wake
// fork`'s rule, for `wake fork`'s reason. awaitSpawn matches a refusal on
// `f.SessionID == sessionID` and has no deadline by design, and the daemon
// addresses every import refusal to the new id. Waiting on the source's would
// leave this parked on a blank terminal forever, which looks exactly like a
// daemon that is thinking.
func importSession(socket, who string, out io.Writer) error {
	src, err := importTarget(who)
	if err != nil {
		return err
	}
	if src.Dir == "" {
		// **Refused here rather than over the socket**, because this is
		// decidable from the disk and the rule this verb already follows is
		// that a disk-decidable refusal must not dial: connect() calls
		// EnsureRunning, which forks a daemon on a machine that had none. It is
		// 97 of 428 rows on the recording machine - roughly a quarter of every
		// pick - and it would spend the one property the picker's "dials
		// nothing" design exists to protect.
		//
		// The daemon still refuses it, and that refusal is the authority: this
		// side cannot see the fleet and must not be the only check. What this
		// avoids is the cost of asking.
		return fmt.Errorf("nothing on this machine proves which directory session %s ran in, and an import has to "+
			"run there: claude locates a transcript by the directory it was started in. Open that session where "+
			"it lives and it can be imported from there", shortID(src.ID))
	}
	sessionID := uuid.NewString()
	return openSession(socket, sessionID, func(conn net.Conn) error {
		return requestImport(conn, sessionID, src.ID)
	}, announceImport, out)
}

// requestImport writes the one frame this verb sends.
//
// It carries no directory, and that is the rule rather than an omission: the
// directory an import runs in is the one the daemon's own discovery proved, and
// a directory chosen on this side would be a client's guess at the one fact
// claude gives no way to recover.
func requestImport(conn net.Conn, sessionID, sourceID string) error {
	if err := rpc.WriteFrameTo(conn, rpc.Frame{
		Kind:      rpc.FrameImport,
		SessionID: sessionID,
		ParentID:  sourceID,
	}); err != nil {
		return fmt.Errorf("import a session: %w", err)
	}
	return nil
}

// announceImport says, on the notice row of the conversation this verb opens,
// what an imported session is and what it is not.
//
// **Two claims, and the second is the one nothing else in this build says.**
//
// It is a copy: the import is a fork (see internal/daemon/import.go), so it
// carries the conversation as of now and the original transcript is untouched.
// That half is SnapshotNotice's claim about a fork, and it is true here for the
// same reason.
//
// And the original may still be open. Wake cannot tell - 2026-08-12 findings §5
// counted four live `claude` processes whose whole argv is the word `claude`,
// which core.SessionArgvMarkers cannot match - so the operator is the only one
// who knows whether a terminal somewhere still has it. That is a sentence rather
// than a guard for the reason forkRefusal's snapshot claim is: Wake cannot
// prevent it, and the honest move is to say so once, where somebody will read
// it.
//
// The sentence lives here rather than in internal/ui because there is no
// keystroke that imports: it describes the outcome of a shell verb, and
// CLAUDE.md's rule is that a sentence naming a shell verb from inside
// internal/ui is a claim about a part of the build that package never reads.
func announceImport(sess rpc.SessionStatus, _ *rpc.Status) {
	notice.Report("%s", importedNotice(displayName(sess), sess.ParentID))
}

// importedNotice is the sentence, split out so a test can read it without a
// terminal.
//
// **"is not written to by this copy" rather than "is untouched", and the
// qualifier is the whole edit.** The evidence is 2026-08-10 findings §5: sha256
// identical, at one moment, on one machine, with an **idle** parent that had
// finished its turn and flushed. §12 of that note declines to cover a *mid-turn*
// parent - "whether the parent's flush and the fork's read can race" is its own
// sentence - and §5 closes with "the claim recorded here is 'it did not change',
// not 'it cannot change'."
//
// An import's central case is a session somebody is using **right now**, which
// is the mid-turn case by construction. `wake fork` guards it with forkRefusal;
// the import path **has no analogue and cannot have one**, because Wake holds no
// state about a stranger's session. That gap is unavoidable. Asserting it away
// in the sentence the operator reads is not - and this project refuses
// unrecorded behaviour rather than designing around it.
func importedNotice(name, sourceID string) string {
	return fmt.Sprintf("@%s is a copy of session %s, with that conversation as of now. "+
		"This copy does not write to the original - measured on an idle session. If it is still open "+
		"somewhere, that session and this one carry on separately and neither sees the other.",
		name, shortID(sourceID))
}

// importTarget resolves what somebody typed to one transcript on disk.
//
// It answers the same three ways matchSession does, and for the same reasons: an
// exact id wins outright, a unique prefix resolves, and anything ambiguous is
// refused with the candidates rather than guessed at. `wake status` prints eight
// characters of an id and invites them to be copied, so a prefix is the form
// somebody will actually type.
//
// **Nothing here decides whether the import is allowed.** The daemon owns that:
// it is the only process that can see whether this session is already in the
// fleet, and the only one that can ask the OS whether anything is running under
// the id. This is a lookup, and its refusals are all "which one did you mean".
func importTarget(who string) (daemon.FoundSession, error) {
	found, err := daemon.Discoverable()
	if err != nil {
		return daemon.FoundSession{}, err
	}
	return resolveImportable(found, who)
}

// resolveImportable is importTarget's answer over a listing somebody already
// has.
//
// Split out for one caller and one reason: `/adopt a b c` resolves three words
// from one draft, and importTarget walks every transcript on the machine per
// call - so three calls is three walks of 428 files for one keystroke. The
// walk and the lookup are separate questions and only the first is expensive.
func resolveImportable(found []daemon.FoundSession, who string) (daemon.FoundSession, error) {
	if len(found) == 0 {
		return daemon.FoundSession{}, fmt.Errorf("there are no claude sessions on this machine to import")
	}
	var matches []daemon.FoundSession
	for _, f := range found {
		if f.ID == who {
			return f, nil
		}
		if strings.HasPrefix(f.ID, who) {
			matches = append(matches, f)
		}
	}
	switch len(matches) {
	case 0:
		return daemon.FoundSession{}, fmt.Errorf("no session on this machine has an id starting %q; `wake import` lists them", who)
	case 1:
		return matches[0], nil
	default:
		return daemon.FoundSession{}, fmt.Errorf("%q matches %d sessions (%s); use more of the id", who, len(matches), shortIDs(matches))
	}
}

// shortIDs names the candidates an ambiguous prefix matched, bounded, because
// the honest answer to "which one" on a machine with 428 transcripts is not all
// of them.
func shortIDs(matches []daemon.FoundSession) string {
	const most = 5
	var ids []string
	for _, m := range matches {
		if len(ids) == most {
			ids = append(ids, fmt.Sprintf("and %d more", len(matches)-most))
			break
		}
		ids = append(ids, shortID(m.ID))
	}
	return strings.Join(ids, ", ")
}

// printImportable is the picker: every session on this machine, newest first,
// with what can and cannot be said about each.
//
// **A row is not an offer.** A transcript is not evidence about a process, so a
// row here means "this conversation exists on disk" and nothing more - not that
// it is closed, not that it is importable. The daemon decides that when asked,
// and the two things it can refuse for that this listing cannot see are a
// session already in the fleet and a process still holding the id.
//
// What the listing *can* say is the one thing that is decided here: whether
// there is a directory to run in. A session with none is drawn with its reason
// in the place the directory would go, because that is the refusal an operator
// would otherwise meet only after choosing.
func printImportable(out io.Writer) error {
	found, err := daemon.Discoverable()
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, formatImportable(found, importUsage, allRows))
	return err
}

const (
	// importUsage is how the shell's own picker says to take one. Named
	// because two surfaces now render this listing and each has to say the
	// command *its own* reader can type: telling somebody who is inside Wake to
	// run a shell verb is the trip out of the room `/adopt` exists to remove.
	importUsage = "`wake import <id>` adopts one."

	// allRows is the cap that is not one. A scrolling terminal can take 428
	// rows and a pane cannot, so the bound belongs to the caller rather than to
	// the renderer - see ui.AdoptUsage's caller in adopt.go.
	allRows = 0
)

// formatImportable renders the picker. Built as a string and written once, which
// is printStatus's shape: one checked write rather than a Fprintf per row whose
// error nothing looks at.
//
// `how` is the sentence naming the command that takes one, and `most` bounds
// the rows - zero for all of them. **Both are parameters rather than two
// renderers**, because two would be two answers to "what is on this machine",
// and the row they would drift on first is the one with no provable directory:
// 97 of 428 on the recording machine, listed with its reason in the directory's
// place precisely so the refusal does not arrive after somebody has chosen.
//
// The count in the header is the **true** one whatever the cap is, and the cap
// says how many it left out: a listing that quietly stopped at ten would be a
// session missing from the machine as far as its reader is concerned.
func formatImportable(found []daemon.FoundSession, how string, most int) string {
	if len(found) == 0 {
		return "no claude sessions on this machine\n"
	}
	shown, hidden := found, 0
	if most > 0 && len(found) > most {
		shown, hidden = found[:most], len(found)-most
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d sessions on this machine, newest first. %s\n\n", len(found), how)
	for _, f := range shown {
		// **The whole row, not each field.** Preview is contained where it is
		// built and Dir was not, so a directory name carrying a newline forged
		// a row - one session in, two rows out, the forged one indistinguishable
		// from a real session. Containing the assembled line means a field added
		// to FoundSession inherits it, which is the rule CLAUDE.md states for
		// the manager's surface and which this listing owed the operator.
		row(&b, "  %s  %-9s  %s", shortID(f.ID), age(f.Modified), whereOrWhyNot(f))
		if f.Preview != "" {
			row(&b, "  %-8s  %-9s  %s", "", "", f.Preview)
		}
	}
	if hidden > 0 {
		fmt.Fprintf(&b, "\n%d more, older than these. `wake import` lists every one.\n", hidden)
	}
	b.WriteString("\nA session that is still open in a terminal cannot be imported: close it there first.\n")
	return b.String()
}

// row writes one contained line of the listing.
//
// The newline is appended **after** containment, so it is the only one on the
// row and nothing interpolated into it can add a second.
func row(b *strings.Builder, format string, args ...any) {
	b.WriteString(daemon.OneLine(fmt.Sprintf(format, args...)))
	b.WriteByte('\n')
}

// whereOrWhyNot is the directory column, and it carries the refusal when there
// is no directory.
//
// The path is shown as-is rather than shortened to a basename: two sessions in
// `delta-agent` and `delta-agent/.claude/worktrees/dev-1919` are the case this
// listing exists to tell apart, and the basename is the same for both.
func whereOrWhyNot(f daemon.FoundSession) string {
	if f.Dir != "" {
		return f.Dir
	}
	return "(no directory can be proven from " + filepath.Base(f.Slug) + " - open it where it lives)"
}

// age is a coarse "how long since anything was written", and the coarseness is
// the honesty: the number says when the file was last touched, which is not when
// the session ended and is certainly not whether it is running.
func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
