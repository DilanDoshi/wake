package daemon

// A session's directory moves, and two different questions have two different
// answers.
//
// `EnterWorktree` and `ExitWorktree` are on the tools list of every session
// Wake spawns - only the manager's built-in set is bounded - so an agent can
// move itself mid-conversation. **Where it is** and **where it started** then
// stop being the same string, and this package needs both:
//
//   - where it *started* is what park writes down, what `unpark` launches from,
//     and what a fork runs in. claude locates a transcript by the directory the
//     process was started in even when every frame names a worktree - which is
//     `discover_test.go`'s 58-of-428 case, and `forkSource`'s refusal says it in
//     as many words. This one must never move.
//   - where it *is* is the roster row, the status bar's branch and the
//     workspaces sidebar. This one has to move or it names a tree the agent
//     left.
//
// The first version of this followed the cwd into `a.dir` and had exactly one
// field, which meant park recorded the worktree and a wake resumed against the
// wrong project slug - an empty conversation under a live session id. That is
// the transcript-branching hazard arriving through the display half.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	spawnedIn = "/repo/api"
	movedTo   = "/repo/api/.wake/worktrees/sydney"
)

func movedAgent(t *testing.T) *agent {
	t.Helper()
	return newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
}

func moved(a *agent, dir string) {
	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Dir: dir}})
}

// The half that must not move. park.go reads a.dir for the book and for
// unpark's resume config, and forkSource reads snapshot().Dir for the same
// reason it refuses a parent whose directory this daemon does not know.
func TestASessionThatMovesKeepsTheDirectoryItStartedIn(t *testing.T) {
	a := movedAgent(t)

	moved(a, movedTo)

	if got := a.snapshot().Dir; got != spawnedIn {
		t.Errorf("the session reports Dir %q after entering a worktree, want the directory it started in (%q). "+
			"claude locates a transcript by that one, so a park here is a wake against the wrong project slug", got, spawnedIn)
	}
}

// The half that must. Nothing else can tell the roster, the status bar or the
// workspaces sidebar that an agent is somewhere new.
func TestASessionThatMovesIsReportedWhereItActuallyIs(t *testing.T) {
	a := movedAgent(t)

	moved(a, movedTo)

	if got := a.snapshot().Cwd; got != movedTo {
		t.Errorf("the session reports Cwd %q, want %q - a row naming a tree the agent has left is what this field is for", got, movedTo)
	}
}

// And back out again, which is ExitWorktree.
func TestASessionThatLeavesAWorktreeIsFollowedBack(t *testing.T) {
	a := movedAgent(t)
	moved(a, movedTo)

	moved(a, spawnedIn)

	if got := a.snapshot().Cwd; got != spawnedIn {
		t.Errorf("Cwd is %q after leaving the worktree, want %q", got, spawnedIn)
	}
}

// A relative path is refused rather than recorded. This value arrives on the
// *child process's own stdout* - it is not a Frame, so it passes through no
// wire fence at all - and everything else in this daemon that decides a
// directory either proves it (discover.go) or requires it absolute (maySpawn).
func TestARelativeDirectoryFromTheChildIsRefused(t *testing.T) {
	for _, bad := range []string{"relative/path", ".", "..", "~/project"} {
		t.Run(bad, func(t *testing.T) {
			a := movedAgent(t)

			moved(a, bad)

			if got := a.snapshot().Cwd; got == bad {
				t.Errorf("the daemon accepted %q as a session's directory from the child's own output; "+
					"nothing downstream distinguishes it from one an operator chose", got)
			}
		})
	}
}

// A frame that names no directory leaves both alone: the facts are merged field
// by field and only init carries one.
func TestAFrameNamingNoDirectoryLeavesBothAlone(t *testing.T) {
	a := movedAgent(t)
	moved(a, movedTo)

	a.observe(core.Event{Kind: core.KindTurnEnd, Session: &core.SessionFacts{OutputTokens: 12}})

	got := a.snapshot()
	if got.Dir != spawnedIn || got.Cwd != movedTo {
		t.Errorf("a frame carrying no directory changed things: Dir %q Cwd %q, want %q and %q", got.Dir, got.Cwd, spawnedIn, movedTo)
	}
}

// Before an agent has said anything, "where it is" is where it was started.
// A consumer that reads Cwd must not have to know whether a turn has happened.
func TestASessionThatHasNotMovedReportsItsStartingDirectoryAsBoth(t *testing.T) {
	got := movedAgent(t).snapshot()

	if got.Dir != spawnedIn || got.Cwd != spawnedIn {
		t.Errorf("a fresh agent reports Dir %q Cwd %q, want %q for both", got.Dir, got.Cwd, spawnedIn)
	}
}

// The directory is refused at launch, which is the one door every session comes
// through - spawn, fork, import and wake - rather than at the spawn frame,
// which is where the only check used to be. That mattered before this change
// and matters more after it: a wake builds its Config from parked.json, a file
// parkbook.go's own header says somebody may have edited by hand, and a fork
// takes the *parent's* directory rather than the frame's.
func TestLaunchRefusesADirectoryThatIsNotAbsolute(t *testing.T) {
	noClaudeAnywhere(t)
	s := newServer(tempSocket(t))
	c := newClient(nil)

	s.launch(c, core.Config{SessionID: idAlpha, Name: "sydney", Dir: "relative/path"}, "", nil, nil)

	select {
	case f := <-c.out:
		if f.Kind != rpc.FrameError || !strings.Contains(f.Text, "absolute") {
			t.Errorf("launch answered %s %q, want an error saying the directory must be absolute", f.Kind, f.Text)
		}
	default:
		t.Error("launch accepted a relative directory. It resolves against the daemon's own working directory - one process for the whole machine - and maySpawn's check reaches only a spawn frame, not a wake, a fork or an import")
	}
}

// And the manager's own bound rests on the directory an *operator* chose, which
// is why Dir must not follow the agent. fleetOccupies reads Dir, and an agent
// that moved itself must not thereby widen where a manager may spawn.
func TestASessionThatMovedDoesNotWidenWhereItRan(t *testing.T) {
	a := movedAgent(t)
	moved(a, movedTo)

	got := a.snapshot()

	if got.Dir == movedTo {
		t.Fatalf("Dir followed the agent to %q", movedTo)
	}
	if got.Dir != spawnedIn {
		t.Errorf("Dir is %q, want the directory the operator spawned into (%q) - internal/mcp's fleetOccupies bounds the manager's spawn tool on exactly this field, and its own comment says the point is that the path came from them rather than from a model", got.Dir, spawnedIn)
	}
}
