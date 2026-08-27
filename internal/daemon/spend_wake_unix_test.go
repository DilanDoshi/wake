//go:build unix

package daemon

// The budget cap and the failover chain, from the spawn frame to the agent's
// record and back out of a park.
//
// Asserted through parked.json for model_wake_unix_test.go's reason: the book is
// what a successor daemon reads, so it is the observable that decides whether a
// woken session comes back capped. A cap that survived only in a live row would
// be a ceiling ⌃Q silently removes.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// parkedSpend is what the book beside this daemon's socket says one session was
// started with.
func parkedSpend(t *testing.T, d *testDaemon, id string) (budget, fallback string) {
	t.Helper()
	for _, rec := range newParkBook(parkBookPath(d.socket)).records() {
		if rec.ID == id {
			return rec.MaxBudgetUSD, rec.FallbackModel
		}
	}
	t.Fatalf("no park book row for %s", id)
	return "", ""
}

// A budget the daemon cannot read as an amount is refused before a name is
// claimed and before anything is started, which is what makes the wire field
// safe to carry: a client that never ran cmd/wake's parser still cannot put an
// arbitrary word where an amount goes.
func TestASpawnWithABudgetThatIsNotAnAmountStartsNothing(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), MaxBudgetUSD: "lots"})
	c.await("a refusal naming the budget", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && strings.Contains(f.Text, "lots")
	})
}

// Zero is the refusal worth its own test. It parses, it is finite, and it is the
// one amount an operator would read as "no cap" and claude would read as "stop"
// - so a build that admitted it would cap a session at nothing on a frame that
// looks like it asked for nothing.
func TestASpawnWithAZeroBudgetStartsNothing(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), MaxBudgetUSD: "0"})
	c.await("a refusal naming the budget", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && strings.Contains(f.Text, "0")
	})
}

// A chain with a link naming nothing is refused for the same reason, and it is
// the shape a hand-built frame produces most easily - a trailing comma.
func TestASpawnWithAnEmptyLinkInTheChainStartsNothing(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), FallbackModel: "opus,"})
	c.await("a refusal naming the chain", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && strings.Contains(f.Text, "opus,")
	})
}

// Both reach the agent's record, which is the producer core.Config's two fields
// would otherwise never have.
func TestASpawnCarriesItsBudgetAndChainOntoTheConfig(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(),
		MaxBudgetUSD: "0.25", FallbackModel: "sonnet,haiku"})
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	budget, fallback := parkedSpend(t, d, idAlpha)
	if budget != "0.25" {
		t.Errorf("the session parked capped at %q, want the amount the spawn frame named", budget)
	}
	if fallback != "sonnet,haiku" {
		t.Errorf("the session parked with chain %q, want the one the spawn frame named", fallback)
	}
}

// **The one that matters.** There is no runtime command for either flag, so a
// cap is a property of the process - and a wake builds a new process from the
// book, with the client that chose the cap long gone. A cap that did not survive
// would make ⌃Q the way to uncap a fleet.
func TestAParkedSessionComesBackCappedAndWithItsChain(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(),
		MaxBudgetUSD: "0.25", FallbackModel: "sonnet,haiku"})
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if got := wakeOutcome(c, idAlpha); !got.woke {
		t.Fatalf("the parked session was not woken: %s", got.why)
	}

	// Parked a second time, which is what makes this about the wake rather than
	// about the first record: the row now on disk was written by the woken
	// session out of the config unpark built for it.
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	budget, fallback := parkedSpend(t, d, idAlpha)
	if budget != "0.25" {
		t.Errorf("a woken session is capped at %q, want the %q it was started with", budget, "0.25")
	}
	if fallback != "sonnet,haiku" {
		t.Errorf("a woken session carries chain %q, want the one it was started with", fallback)
	}
}

// Absent is neither an amount nor a chain: it means Wake chose none, the flags
// are left off the argv entirely, and claude applies its own defaults. Not
// refused - absent and invalid are different, and this is the floor under that
// distinction.
func TestASpawnWithNoBudgetOrChainPutsNoFlagOnTheArgv(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir()})
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	budget, fallback := parkedSpend(t, d, idAlpha)
	if budget != "" || fallback != "" {
		t.Errorf("a spawn that named neither recorded budget %q and chain %q", budget, fallback)
	}
}

// A park book is a file on disk, and neither of these fields was checked on the
// way back out of one.
//
// Found by a review of this branch, and it is the *same* finding a review of the
// effort feature made one release earlier: every check lived at the spawn frame,
// and three of the four ways a session starts do not go through one. A wake
// builds its Config from parked.json, so a row that has been hand-edited - or
// written by a build that validated differently - put an unchecked string on a
// live process's command line with every frame check in place and passing.
//
// The verdict is bookEffort's, because a book row is a decoration and refusing
// to wake a session over one is worse than waking it without it. **The direction
// that costs something is named in the log**: a dropped cap comes back
// *uncapped*, which is what every session did before the flag existed, and the
// operator has to be told rather than left to find out from a bill.
func TestARestoredSessionDropsABudgetOrChainThatIsNotOne(t *testing.T) {
	for _, tc := range []struct {
		name, budget, chain   string
		wantBudget, wantChain string
	}{
		{"both survive", "0.25", "sonnet,haiku", "0.25", "sonnet,haiku"},
		{"nothing stays nothing", "", "", "", ""},
		{"a cap of nothing is dropped", "0", "", "", ""},
		{"a negative is dropped", "-5", "", "", ""},
		{"an injected argument is dropped", "; touch /tmp/pwned #", "", "", ""},
		{"a spelling claude reads differently is dropped", "0x1p4", "", "", ""},
		{"an empty link is dropped", "", "opus,", "", ""},
		{"one bad field does not take the other", "0", "sonnet", "", "sonnet"},
		// A chain link that reads like a shell injection **survives**, and that
		// is the design rather than a hole. ValidModel admits any non-empty
		// name because there is no knowable set of models, and what protects
		// the command line is that a link is one element of an exec argv which
		// cannot introduce another - never a shell. rpc.Frame.Model makes the
		// same argument for the same reason. The effort path can refuse this
		// shape only because its set is closed.
		{"a link that looks like an injection is one argv word", "", "opus; touch /tmp/pwned #", "", "opus; touch /tmp/pwned #"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := parkedRecord{ID: idAlpha, Name: "alex", Dir: "/tmp/repo", MaxBudgetUSD: tc.budget, FallbackModel: tc.chain}
			budget, chain := bookSpend(rec)
			if budget != tc.wantBudget {
				t.Errorf("a book naming budget %q resumed at %q, want %q", tc.budget, budget, tc.wantBudget)
			}
			if chain != tc.wantChain {
				t.Errorf("a book naming chain %q resumed at %q, want %q", tc.chain, chain, tc.wantChain)
			}
		})
	}
}

// launch is the one door, so the check is on it as well.
//
// Driven through launch rather than through a frame because that is the claim:
// not "the spawn frame is checked" but "nothing this daemon starts can carry a
// budget that is not an amount or a chain with a link naming nothing". The
// dropping above is what keeps a wake from *hitting* this; this is what makes
// the fourth way in - one nobody has written yet - safe by construction.
func TestLaunchRefusesABudgetOrChainThatIsNotOne(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	for _, budget := range []string{"0", "-5", "0x1p4", "; touch /tmp/pwned #", "--model=evil"} {
		if core.ValidBudget(budget) {
			t.Fatalf("core admits %q as a budget: this test would assert nothing", budget)
		}
		c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), MaxBudgetUSD: budget})
		c.await("a refusal for "+budget, func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	}
	// No injection case here, unlike the effort test: a chain names models and
	// there is no knowable set of those, so the only shape that can be refused
	// is a link naming nothing. See the table above.
	for _, chain := range []string{"opus,", ",", ",opus", "opus,,haiku"} {
		if core.ValidFallbackModel(chain) {
			t.Fatalf("core admits %q as a chain: this test would assert nothing", chain)
		}
		c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), FallbackModel: chain})
		c.await("a refusal for "+chain, func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	}
}
