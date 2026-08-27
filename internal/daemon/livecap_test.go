package daemon

// The live cap, through a real daemon on a real socket.
//
// It exists because `spawn_agent` is on the manager's tool surface, and the
// refusal it replaced said why: the failure mode of a spawn tool is thirty
// agents nobody asked for rather than one. So what these hold is not "a number
// is compared" - it is which sessions count, which verbs the cap gates, and
// that the refusal says what to do about it.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A spawn past the cap is refused, and the one before it is not.
//
// Both halves in one test on purpose: a cap that refused everything would pass
// the refusal half alone, and this project has shipped that shape before.
func TestASpawnPastTheCapIsRefusedAndTheOneBeforeItIsNot(t *testing.T) {
	fakeClaudeOnPath(t, "")
	smallCap(t, 2)
	d := startDaemon(t)
	c := attach(t, d.socket)

	first := uuid.NewString()
	if got := spawnFor(c, first, "", t.TempDir()); got.ID != first {
		t.Fatalf("the first spawn under a cap of 2 was refused: %+v", got)
	}
	second := uuid.NewString()
	if got := spawnFor(c, second, "", t.TempDir()); got.ID != second {
		t.Fatalf("the second spawn under a cap of 2 was refused: %+v", got)
	}

	third := uuid.NewString()
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: third, Dir: t.TempDir()})
	why := awaitRefusal(c, third)

	if !strings.Contains(why, strconv.Itoa(liveCap)) {
		t.Errorf("the refusal is %q and does not say how many are running. A cap an operator cannot see the edge of is a refusal they cannot act on", why)
	}
	for _, remedy := range []string{"parks one", "wake stop"} {
		if !strings.Contains(why, remedy) {
			t.Errorf("the refusal is %q and does not name %q. Every other refusal in this daemon names what to do next", why, remedy)
		}
	}
}

// A parked session does not count against the cap.
//
// This is the whole of why liveCount reads state rather than len(s.agents): a
// parked session keeps its row precisely so it can come back, and counting it
// would make park the one recovery that costs an agent its own way home.
func TestParkingMakesRoomUnderTheCap(t *testing.T) {
	fakeClaudeOnPath(t, "")
	smallCap(t, 1)
	d := startDaemon(t)
	c := attach(t, d.socket)

	held := uuid.NewString()
	if got := spawnFor(c, held, "", t.TempDir()); got.ID != held {
		t.Fatalf("the first spawn under a cap of 1 was refused: %+v", got)
	}

	blocked := uuid.NewString()
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: blocked, Dir: t.TempDir()})
	_ = awaitRefusal(c, blocked)

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: held})
	c.await("the parked session", func(f rpc.Frame) bool {
		if f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == held && s.State == rpc.StateParked {
				return true
			}
		}
		return false
	})

	after := uuid.NewString()
	if got := spawnFor(c, after, "", t.TempDir()); got.ID != after {
		t.Fatalf("a spawn was still refused after the only live session parked: %+v", got)
	}
}

// Waking is not gated by the cap, and that line is deliberate.
//
// unpark does not go through maySpawn, so this is a property of the routing
// rather than a check somebody remembered to skip - and it is the right way
// round: the cap bounds *new* sessions, and a conversation the operator already
// owns is not new. Refusing a wake at the cap would make the fleet's own
// recovery the thing the cap takes away.
func TestAWakeIsNotRefusedByTheCap(t *testing.T) {
	fakeClaudeOnPath(t, "")
	smallCap(t, 1)
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := t.TempDir()
	id := uuid.NewString()
	if got := spawnFor(c, id, "", dir); got.ID != id {
		t.Fatalf("the first spawn under a cap of 1 was refused: %+v", got)
	}

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: id})
	c.await("the parked session", func(f rpc.Frame) bool {
		return namesState(f, id, rpc.StateParked)
	})

	// A second session takes the one slot, so the fleet is at the cap when the
	// wake arrives. Without this the wake would be under it and prove nothing.
	filler := uuid.NewString()
	if got := spawnFor(c, filler, "", t.TempDir()); got.ID != filler {
		t.Fatalf("the parked session did not free the slot: %+v", got)
	}

	c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: id})
	c.await("the woken session", func(f rpc.Frame) bool {
		return namesState(f, id, rpc.StateIdle) || namesState(f, id, rpc.StateWorking)
	})
}

// namesState reports whether a frame's report says this session is in a state.
func namesState(f rpc.Frame, id, state string) bool {
	if f.Status == nil {
		return false
	}
	for _, s := range f.Status.Sessions {
		if s.ID == id && s.State == state {
			return true
		}
	}
	return false
}

// A fork is capped too, and it is capped by the same call rather than by a
// second check that could drift from it.
func TestAForkPastTheCapIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "")
	smallCap(t, 1)
	d := startDaemon(t)
	c := attach(t, d.socket)

	parent := uuid.NewString()
	if got := spawnFor(c, parent, "", t.TempDir()); got.ID != parent {
		t.Fatalf("the first spawn under a cap of 1 was refused: %+v", got)
	}

	child := uuid.NewString()
	c.send(rpc.Frame{Kind: rpc.FrameFork, SessionID: child, ParentID: parent})
	if why := awaitRefusal(c, child); !strings.Contains(why, "cap") {
		t.Errorf("a fork past the cap was refused with %q, which is not the cap's refusal. Two paths that create a session and one that checks is the drift maySpawn exists to prevent", why)
	}
}

// The cap holds at the door that takes the row, not only at maySpawn.
//
// maySpawn's count and the admit are separated by a name claim, a worktree,
// and a config build, so two spawns racing can both count 29 and both be
// admitted - every check green and 31 processes running. This drives the
// authoritative door directly, handing launch the state the loser of that
// race reaches: a fleet already at the cap. With no claude on PATH the two
// refusals are distinguishable - an exec failure proves the count was never
// re-read where the row is taken.
//
// The second half is TestAWakeIsNotRefusedByTheCap's ruling carried into the
// re-check: the cap bounds *new* sessions, so a wake at the cap must pass this
// door too, and the exec failure is the proof it did.
func TestALaunchPastTheCapIsRefusedAtTheRowItself(t *testing.T) {
	noClaudeAnywhere(t)
	smallCap(t, 1)
	s := newServer(tempSocket(t))
	c := newClient(nil)

	if !s.register(liveAgent(idAlpha, "alex", t.TempDir())) {
		t.Fatal("the fleet could not be filled to the cap, so nothing below is at the boundary")
	}
	name, err := s.names.claim("blair")
	if err != nil {
		t.Fatalf("claim blair: %v", err)
	}
	s.launch(c, core.Config{SessionID: idBeta, Name: name, Dir: t.TempDir()}, "", nil, nil)

	why := firstRefusal(t, c).Text
	if !strings.Contains(why, "cap") {
		t.Errorf("a launch past the cap was refused with %q, want the cap's refusal: anything else means "+
			"the count is not re-read where the row is taken, and two spawns that both counted under the "+
			"cap are both admitted", why)
	}
	if _, still := s.agents[idBeta]; still {
		t.Errorf("a launch refused by the cap left session %s in the fleet", idBeta)
	}
	if _, err := s.names.claim("blair"); err != nil {
		t.Error("a launch refused by the cap kept its name claimed, so the pool leaks a name per refusal")
	}
	s.names.release("blair")

	// And a wake through the same door is exempt: at the cap, the one refusal
	// it may get here is exec's, which is proof the cap let it through.
	was := parkedRow(t, s, idBeta, "blair")
	s.launch(c, core.Config{SessionID: idBeta, ResumeFrom: idBeta, Name: "blair", Dir: was.dir}, "", was, nil)
	if why := firstRefusal(t, c).Text; strings.Contains(why, "cap") {
		t.Errorf("a wake at the cap was refused with %q: the cap bounds new sessions, and a conversation "+
			"the operator already owns is not new - refusing it makes the fleet's own recovery the thing "+
			"the cap takes away", why)
	}
}
