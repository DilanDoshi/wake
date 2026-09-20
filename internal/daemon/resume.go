package daemon

// Resuming an on-disk conversation in place — the picker's disk half.
//
// resumeSession is importSession's near-twin, and the two differences are the
// whole point of the verb. It **resumes** rather than forks: the Config carries
// ResumeFrom and the source's own id, so the argv is `--resume <id>` and the
// session continues its transcript instead of branching into a new one. And it
// **does not ask resumeSafe**.
//
// Import forks precisely because Wake cannot prove a session it never started is
// not still open in a terminal — a hand-started `claude`'s argv is just the word
// `claude`, invisible to resumeSafe's `ps` match — so a fork onto a fresh id was
// the only primitive it could guarantee. `/resume` accepts exactly that risk,
// matching Claude Code's own `/resume`, on the owner's ruling (2026-09-20, "same
// as Claude Code, no guard"). The transcript branches if the source really is
// open elsewhere; nothing here guards against it. See docs/notes/decisions.md
// and CLAUDE.md's amended non-negotiable.
//
// A parked session takes FrameWake/unparkRecord instead (its resumeSafe intact —
// Wake parked it, so that path is genuinely safe); this frame carries only the
// on-disk ids the picker dedups against the live and parked fleet.

import (
	"context"
	"errors"
	"fmt"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// resumeSession resumes an on-disk conversation in place: same id, no fork, no
// resumeSafe. The frame carries the id to resume as SessionID.
func (s *server) resumeSession(ctx context.Context, c *client, f rpc.Frame) {
	if !s.maySpawn(ctx, c, f) {
		return
	}
	src, err := s.resumeSource(f.SessionID)
	if err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	name, err := s.names.claim(f.Text)
	if err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	// ResumeFrom and the source's own id, where importSession sets ForkFrom and a
	// minted id. Dir is the directory discovery proved, never one a client chose.
	s.launch(c, core.Config{
		SessionID:      src.ID,
		ResumeFrom:     src.ID,
		Name:           name,
		Dir:            src.Dir,
		PermissionMode: spawnPermissionMode,
	}, src.ID, nil, nil)
}

// resumeSource is the transcript a `/resume` may take, or why it may not: it
// needs a real session id, a transcript on this machine, and a provable
// directory to run in.
//
// It is importSource with the two guard steps removed — the fleet-holds check
// and resumeSafe. The holds check is the picker's job (it dedups the live and
// parked fleet out of the disk rows), and resumeSafe is the reversal in the
// header. What stays is what a resume cannot do without: something to resume,
// and somewhere to run it.
func (s *server) resumeSource(sourceID string) (FoundSession, error) {
	if sourceID == "" {
		return FoundSession{}, errors.New("a resume needs a session to resume")
	}
	if !mintedByWake(sourceID) {
		return FoundSession{}, fmt.Errorf("a session id must be a UUID, got %q: claude names every transcript for the session's own id, so anything else is not one", sourceID)
	}
	found, err := discover(ProjectsDir())
	if err != nil {
		return FoundSession{}, fmt.Errorf("could not read the sessions on this machine: %w", err)
	}
	src, ok := findSession(found, sourceID)
	if !ok {
		return FoundSession{}, fmt.Errorf("there is no transcript for session %s on this machine: "+
			"claude keeps one file per session and /resume lists the ones it can see", sourceID)
	}
	if src.Dir == "" {
		// The 97-of-428 case: claude locates a transcript by the directory the
		// process started in, so a resume with no proven directory would run in
		// the daemon's own and open an empty session under a live-looking header.
		return FoundSession{}, fmt.Errorf("nothing on this machine proves which directory session %s ran in, and a resume has to run there: "+
			"claude locates a transcript by the directory it was started in, and %q is a name several real directories could produce. "+
			"Open that session where it lives instead", sourceID, src.Slug)
	}
	return src, nil
}
