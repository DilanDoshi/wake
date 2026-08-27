// Adopting a session Wake never started.
//
// # Why an import is a fork and not a resume
//
// The obvious shape is `--resume <id>`, which reuses the id (2026-08-09
// findings §2), so an imported session would keep its identity. That is the
// shape this feature was expected to take and it is the wrong one, for a reason
// that is measured rather than argued.
//
// Two live processes on one session id do not collide - they branch. Both are
// accepted, both answer correctly from their own history, neither is told about
// the other, and the transcript branches in place with last-writer-wins
// (2026-08-09 findings §5). **There is no error on any wire**, so the check has
// to be Wake's own and it has to happen before the second process exists.
//
// For a session Wake parked, that check is sound: the park completed in retire,
// after core's Wait returned, so the process is provably gone. For a stranger's
// session there is no such proof, and resumeSafe cannot manufacture one.
// idsInUse matches core.SessionArgvMarkers - a flag and its value - and
// 2026-08-12 findings §5 counted, on the machine this was written on, **four
// live `claude` processes whose entire command line is the word `claude`**. The
// CLI minted their ids itself; no id appears in any argv; SessionArgvMarkers
// cannot see them. And that is precisely the shape the feature was asked for:
// *"a bunch of sessions open in terminals scattered."*
//
// So a resume here would be a guess, and a wrong guess silently corrupts
// somebody's conversation with nothing anywhere reporting it.
//
// The corpus already named the alternative. 2026-08-09 findings §5 closes with
// *"it makes the fork the safe primitive for importing, not just for
// forking"*, and 2026-08-10 §5 measured a fork taken from a **live** parent
// leaving that parent's transcript **byte-identical**, by sha256 at both
// generations. A fork writes its own file and does not touch the source's.
//
// **What that costs is the id**, and it is the honest cost: an imported session
// is a new Wake session carrying that conversation, not the original session
// under new management. The alternative is keeping an id by writing into a file
// another process may be writing to.
//
// # resumeSafe is still asked, and it is a narrowing rather than the safety
//
// It catches what it can catch - twelve of the sixteen live claude processes on
// the recording machine carry an id in their argv - and every error from it is
// a refusal, including "I could not look". Nothing here routes around it. What
// changed is what a *pass* from it is allowed to mean: for a park it is close to
// proof, and here it is only the absence of the evidence it can see. The fork is
// what makes the residue safe.

package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// importSession adopts a transcript on disk as a new Wake agent.
//
// It differs from fork in exactly one place - where the source comes from - and
// shares launch, maySpawn, naming and confirmation with spawn and fork, which
// is what makes an imported session an ordinary one from the moment it starts.
//
// **Every refusal carries f.SessionID, the new session's own id, never the
// source's.** The client is waiting on that id and neither wait in this project
// has a deadline by design, so a refusal addressed to the source leaves it
// waiting forever - fork's rule, for fork's reason.
func (s *server) importSession(ctx context.Context, c *client, f rpc.Frame) {
	if !s.maySpawn(ctx, c, f) {
		return
	}
	src, err := s.importSource(f.ParentID)
	if err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	name, err := s.names.claim(f.Text)
	if err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	// ForkFrom rather than ResumeFrom, which is the whole header above, and
	// Dir is the directory **discovery proved** rather than anything the client
	// said. The parent edge is the source id: it is true, it is the only record
	// anywhere that this conversation came from somewhere, and nothing on
	// claude's wire would ever say so (2026-08-09 findings §3). It resolves to
	// no name in this fleet, which the surfaces already handle - the DM header
	// names a parent by name or not at all.
	s.launch(c, core.Config{
		SessionID:      f.SessionID,
		ForkFrom:       src.ID,
		Name:           name,
		Dir:            src.Dir,
		PermissionMode: spawnPermissionMode,
	}, src.ID, nil, nil)
}

// importSource is the transcript an import may be taken from, or why it may not
// be.
//
// The order of the refusals is the order an operator can act on them, which is
// also cheapest-first: a typo, then a session that is already here, then a
// directory that cannot be proven, then the one that costs a `ps`.
func (s *server) importSource(sourceID string) (FoundSession, error) {
	if sourceID == "" {
		return FoundSession{}, errors.New("an import needs a session to import: `wake import` with no argument lists what is on this machine")
	}
	if !mintedByWake(sourceID) {
		return FoundSession{}, fmt.Errorf("a session id must be a UUID, got %q: claude names every transcript for the session's own id, so anything else is not one", sourceID)
	}
	// Before the disk is walked, because this is the fast, common and most
	// confusing case: the same conversation arriving twice under two names,
	// with the operator's next message going to whichever one they happened to
	// have open.
	if s.holds(sourceID) {
		return FoundSession{}, errors.New(s.heldRefusal(sourceID))
	}
	found, err := discover(ProjectsDir())
	if err != nil {
		return FoundSession{}, fmt.Errorf("could not read the sessions on this machine: %w", err)
	}
	src, ok := findSession(found, sourceID)
	if !ok {
		return FoundSession{}, fmt.Errorf("there is no transcript for session %s on this machine: "+
			"claude keeps one file per session and `wake import` lists the ones it can see", sourceID)
	}
	if src.Dir == "" {
		// The 97-of-428 case, and the refusal has to name the directory
		// problem rather than the session: claude locates a transcript by the
		// working directory the process was started in, so an import with no
		// proven directory would run in whatever directory the daemon happens
		// to hold and open an empty session under a live-looking header.
		return FoundSession{}, fmt.Errorf("nothing on this machine proves which directory session %s ran in, and an import has to run there: "+
			"claude locates a transcript by the directory it was started in, and %q is a name that several real directories could produce. "+
			"Open that session where it lives and it can be imported from there", sourceID, src.Slug)
	}
	// **Unconditional, and asserted as a statement rather than as a call
	// count.** The tempting narrowing is a recency gate - `if
	// time.Since(src.Modified) < staleAfter` - which removes a `ps -Aww` from a
	// path an operator repeats and reads only fields that are already here. It
	// is exactly backwards: a session open in a terminal and idle for a month
	// is the *most* likely import target, and Modified is recency, which
	// discover.go's own doc calls a hint. See
	// TestAnImportAsksResumeSafeUnconditionally.
	if err := s.resumeSafe(sourceID); err != nil {
		return FoundSession{}, s.explainHeldSource(sourceID, err)
	}
	return src, nil
}

// heldRefusal is why a session already in this fleet cannot be imported, and
// **it branches on parked** because the two answers point at different verbs.
//
// A parked session is in s.agents on purpose - completePark leaves the id there
// so `holds` refuses a respawn under it - so it reaches this arm and looks like
// any other held session. It is not: `wake attach` **refuses** a parked
// session, so offering it here hands the operator a verb that will itself
// refuse, and the one thing that works is not named at all.
//
// That is the defect CLAUDE.md records repairing one verb over: `wake attach`'s
// own refusal named `wake fork` alone, so somebody who wanted their
// conversation back was pointed at the verb that makes a second session, spends
// a second name and leaves the original parked. This is the same sentence
// arriving at a new door, so it leads with the same answer.
//
// There is still no `wake resume` shell verb, which is why it names the in-TUI
// command and says where it is typed.
func (s *server) heldRefusal(sourceID string) string {
	for _, p := range s.fleet().Sessions {
		if p.ID != sourceID {
			continue
		}
		if p.State != rpc.StateParked {
			break
		}
		who := p.Name
		if who == "" {
			who = short(p.ID)
		}
		return fmt.Sprintf("session %s is parked in this fleet rather than gone, so there is nothing to import: "+
			"bring it back with `/resume %s` in the room. `wake attach` refuses a parked session, and "+
			"`wake fork %s` would make a second one and leave this one parked", sourceID, who, short(sourceID))
	}
	return fmt.Sprintf("session %s is already in this fleet, so there is nothing to import: "+
		"open it with `wake attach %s`, or branch it with `wake fork %s`", sourceID, short(sourceID), short(sourceID))
}

// explainHeldSource replaces resumeSafe's sentence when the process it saw is
// provably one of Wake's own.
//
// **An imported agent carries `--resume <src>` in its argv for its whole life**,
// and `core.SessionArgvMarkers` matches a flag and its value - so from the
// moment one import succeeds, `idsInUse(src)` matches Wake's own agent forever.
// A second import of that source then gets resumeSafe's sentence, and every
// clause of it is false here: the process is Wake's own rather than a terminal's,
// the import would fork rather than resume, and **"close it there first"
// instructs a destructive action** against the only session the operator can
// find to close - which is the real terminal one, possibly mid-turn, and which
// `wake status` will not even list, because the fleet carries the import's id
// and not the source's.
//
// It is a **different sentence and not a suppressed refusal**: the import is
// still refused. What it cannot claim is that nothing else is also holding the
// id - once Wake's own agent is in the `ps` output there is no way to tell a
// second holder from none - so the sentence says that rather than implying the
// coast is clear.
func (s *server) explainHeldSource(sourceID string, why error) error {
	for _, p := range s.fleet().Sessions {
		if p.ParentID != sourceID {
			continue
		}
		who := p.Name
		if who == "" {
			who = short(p.ID)
		}
		return fmt.Errorf("session %s has already been imported, as @%s, and that agent carries the source id in "+
			"its own command line for as long as it runs - so Wake can no longer tell whether anything else is "+
			"holding it. Open the import with `wake attach %s`, or stop it and import again", sourceID, who, who)
	}
	return why
}

// findSession picks one discovered session out of the listing.
//
// A linear scan rather than a map because the caller wants the whole listing
// anyway for its refusals, and 428 transcripts was this machine's count.
func findSession(found []FoundSession, id string) (FoundSession, bool) {
	for _, f := range found {
		if f.ID == id {
			return f, true
		}
	}
	return FoundSession{}, false
}

// short is the eight characters `wake status` prints and invites somebody to
// copy, so a refusal that offers a next command offers it in the form they will
// actually type.
func short(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}
