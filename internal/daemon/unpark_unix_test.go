//go:build unix

// Waking a parked session, and the proof that has to come first.
//
// Unix-only because the whole subject is a real process on a real machine: the
// bystander below is started, its command line is read back out of ps, and the
// refusal it provokes is the one thing standing between a wake and two live
// processes on one transcript. liveid_other.go's answer - "this platform
// cannot look" - is a refusal too, and it needs no process to prove.

package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// bystander starts a long-lived process carrying extra words in its own argv
// and returns the command line ps reports for it.
//
// **Reading the argv back is the whole helper**, and the alternative is a
// fixture that silently stops being one. The first draft was
// `sh -c "sleep 30" --session-id <id>` - sh takes the extra words as $0/$1 and
// ignores them, so the argv reads correctly on the page. It does not survive:
// /bin/sh execs a lone simple command in place rather than forking, so the
// process ps actually sees is `sleep 30` and the words are gone. Measured, not
// guessed at: the refusal test below then passed the wake, waited out the full
// testTimeout and reported a daemon that had not refused - a fixture failure
// wearing a production failure's clothes. A trailing `:` makes it a list, which
// is not the shape that gets exec'd, and this readback is what says so on the
// machine the test runs on rather than on the one it was written on.
//
// It goes through inspect, which asks about this *pid*. That is a different
// question from idsInUse's, which scans every command line on the machine for
// a marker - a precondition established with the function under test would
// agree with it by construction and could never see it break.
func bystander(t *testing.T, words ...string) string {
	t.Helper()

	cmd := exec.Command("/bin/sh", append([]string{"-c", "sleep 30; :"}, words...)...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the bystander: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	p, err := inspect(ctx, cmd.Process.Pid)
	if err != nil {
		t.Fatalf("read the bystander's own command line back: %v", err)
	}
	for _, w := range words {
		if !strings.Contains(p.argv, w) {
			t.Fatalf("the bystander runs as %q, which does not carry %q: it is not the process this test "+
				"needs, so whatever the daemon says next is about something else", p.argv, w)
		}
	}
	return p.argv
}

// holdsTheSession reports whether an argv is one core would recognise as a live
// process running a session.
func holdsTheSession(argv, sessionID string) bool {
	return slices.ContainsFunc(core.SessionArgvMarkers(sessionID), func(m string) bool {
		return strings.Contains(argv, m)
	})
}

// onlyPsOnPath leaves the machine able to answer "is anything holding this id"
// and unable to start a claude, which is the one combination that reaches
// launch's failure path.
//
// The real ps is resolved *before* PATH is replaced, because resolving it
// afterwards is the bug this exists to avoid.
func onlyPsOnPath(t *testing.T) {
	t.Helper()

	realPs, err := exec.LookPath("ps")
	if err != nil {
		t.Fatalf("locate ps before replacing PATH: %v", err)
	}
	dir := t.TempDir()
	if err := os.Symlink(realPs, filepath.Join(dir, "ps")); err != nil {
		t.Fatalf("symlink ps: %v", err)
	}
	t.Setenv("PATH", dir)
}

// checkoutBranch points a directory's .git/HEAD at a branch, which is where
// taskLabel reads a label from.
func checkoutBranch(t *testing.T, dir, branch string) {
	t.Helper()

	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", gitDir, err)
	}
	writeFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/"+branch+"\n")
}

// wakeResult is what a wake did: the daemon's refusal and why, or the row the
// session came back in.
type wakeResult struct {
	why  string
	row  rpc.SessionStatus
	woke bool
}

// wakeOutcome sends a wake and waits for whichever of the two things happened.
//
// **Every wake in this file goes through it**, and that is the point rather
// than tidiness. A wait for the refusal alone turns any mutation of resumeSafe,
// isParked, admit or launch into a testTimeout printing *a frame that did not
// come* instead of the session that did: measured at **15.05s per test, four
// tests**, against 0.06s once the wait spanned both outcomes. That is
// docs/notes/decisions.md's *"a wait for one outcome turns the defect into a
// timeout"*, and this function existing is not enough on its own - two tests
// sent the frame themselves and waited for the good answer, and a review found
// them by forcing every wake to be refused.
//
// **The positive arm needs evidence that postdates the send, and the pgid is
// what supplies it.** testClient.await consults the frames earlier waits read
// past *before* it reads anything new, so "a push naming this id in a state
// other than parked" can be satisfied by a push written before the wake was -
// which is what happened on the fork path, where the floor below it never fired
// and the test died fifteen seconds later somewhere else. launch starts a new
// process, so a woken session's pgid differs from the one it had a moment ago;
// the row before the send is read through a fresh status *reply*, which is one
// to one with its question and therefore cannot itself be stale.
func wakeOutcome(c *testClient, id string) wakeResult {
	c.t.Helper()

	was := sessionRow(c.status(), id)

	c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: id})
	var row rpc.SessionStatus
	f := c.await("the daemon's answer to a wake of "+id, func(f rpc.Frame) bool {
		if f.Kind == rpc.FrameError && f.SessionID == id {
			return true
		}
		// A woken session is a running one under a *new* process group, and
		// launch announces it to everybody.
		if f.Kind != rpc.FrameStatusPush || f.Status == nil {
			return false
		}
		got := sessionRow(*f.Status, id)
		if got.State == "" || got.State == rpc.StateParked || got.PID == was.PID {
			return false
		}
		row = got
		return true
	})
	if f.Kind == rpc.FrameError {
		return wakeResult{why: f.Text}
	}
	return wakeResult{row: row, woke: true}
}

// Waking proves the parked process is gone, and refuses when it cannot.
//
// This is the failure with no symptom: two live processes under one session id
// are both accepted, both answer correctly from their own history, neither is
// told about the other, and the transcript branches in place with last-writer
// wins (2026-08-09 findings §5, resume-collide-first/-second). There is no
// error to detect on the stream, so the check has to be Wake's own and it has
// to happen before the second process exists.
//
// The bystander is a real process carrying the marker in a real argv, because
// a check whose negative result you have never seen turn positive is a check
// that cannot fail.
func TestWakingRefusesWhileAnotherProcessHoldsTheSessionId(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	// A process whose argv carries `--session-id <idAlpha>`, holding the id the
	// way a stray `claude --resume` would.
	if argv := bystander(t, "--session-id", idAlpha); !holdsTheSession(argv, idAlpha) {
		t.Fatalf("the bystander runs as %q and core would not read that as a process holding session %s: "+
			"nothing is holding it, so a refusal below would be about something else and its absence "+
			"about nothing", argv, idAlpha)
	}

	got := wakeOutcome(c, idAlpha)
	if got.woke {
		t.Fatalf("session %s was woken while another process was holding its id, and came back as %+v. "+
			"Two live processes under one id are both accepted, both answer from their own history, and "+
			"the transcript branches in place with last-writer-wins - there is no error on any wire to "+
			"find this afterwards", idAlpha, got.row)
	}
	if why := got.why; !strings.Contains(why, "still running") {
		t.Errorf("the refusal is %q and does not say a process is still running under that id. "+
			"Nothing on claude's wire reports the collision - no error, no frame - so this sentence "+
			"is the only account anyone gets", why)
	}
	if st := c.status(); stateOf(st, idAlpha) != rpc.StateParked {
		t.Errorf("a refused wake left session %s in state %q, want parked: a wake that cannot be "+
			"proved safe must change nothing", idAlpha, stateOf(st, idAlpha))
	}
}

// A woken session keeps its identity, which is what --resume buys.
func TestAWokenSessionComesBackUnderItsOwnIdAndName(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	before := c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	got := wakeOutcome(c, idAlpha)
	if !got.woke {
		t.Fatalf("the parked session was not woken: %s", got.why)
	}
	after := got.row

	if after.Name != before.Name || after.Dir != before.Dir || after.Label != before.Label {
		t.Errorf("a woken session came back as %+v, want the identity it parked with %+v", after, before)
	}
	if after.PID == before.PID {
		t.Errorf("a woken session reports the same process group %d it parked with: park ends the "+
			"process, so a pgid that did not change means nothing was restarted", after.PID)
	}
}

// The argv a woken agent is actually started with, on a real process, and it
// is the end-to-end half of core's unit test.
//
// It is here rather than only in core for TestAForkedAgentIsStartedWithTheRecordedTriple's
// reason: the shape has to survive Config, unpark, launch and exec, and this is
// the only place the daemon's idea of a wake meets core's. The negative half is
// the load-bearing one - `--resume <id> --session-id <id>` is refused at
// startup, exit 1, with nothing on stdout, so a wake that built it would show
// up as a session that would not start and nothing saying why.
func TestAWokenAgentIsStartedWithABareResumeAndNoSessionId(t *testing.T) {
	fakeClaudeOnPath(t, "argv")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "argv: ")
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	// The wake's own outcome first, so a refusal is reported as a refusal
	// rather than as an argv line that never arrived.
	if woken := wakeOutcome(c, idAlpha); !woken.woke {
		t.Fatalf("the parked session was not woken, so there is no argv to read: %s", woken.why)
	}
	// The frame is addressed by the id the daemon spawned under, so this finds
	// the woken process's line even though its own --session-id is gone - which
	// is the point. It matches any argv line: the pre-park one was consumed
	// above, so the next is the woken process's whatever it says, and the
	// assertions below are what judge it.
	got := c.await("the woken agent reporting its command line", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && f.Event != nil &&
			strings.Contains(f.Event.Text, "argv: ")
	})
	argv := got.Event.Text
	if !strings.Contains(argv, "--resume "+idAlpha) {
		t.Errorf("a woken agent was started as\n  %s\nand it has to carry `--resume %s`: that flag reuses "+
			"the id it is given, which is the whole of how a session keeps its identity", argv, idAlpha)
	}
	if strings.Contains(argv, "--session-id") {
		t.Errorf("a woken agent was started as\n  %s\nand it carries --session-id beside --resume. The CLI "+
			"refuses that pair at startup, exit 1, with nothing on stdout", argv)
	}
	if strings.Contains(argv, "--fork-session") {
		t.Errorf("a woken agent was started as\n  %s\nand it carries --fork-session, which mints a new "+
			"session under the id being resumed instead of continuing it", argv)
	}
}

// A client carrying a session id in its own argv is not a process holding the
// session, and that is what makes the marker a flag and its value rather than
// a bare id.
//
// `wake attach <uuid>` puts one there with nothing in front of it, and the one
// moment resumeSafe is asked is right after somebody parked the session they
// had attached to by id. A bare-id match would refuse every wake an operator
// asks for from the terminal they parked it in - and the fix somebody reaches
// for then is to weaken the check, which is the direction that corrupts a
// transcript.
//
// Without this test the marker rule is a comment: SessionArgvMarkers could
// return the bare id and the whole suite would stay green.
func TestAClientNamingASessionInItsArgvDoesNotHoldIt(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	// `wake attach <uuid>` shaped: the id, and no flag in front of it.
	if argv := bystander(t, idAlpha); holdsTheSession(argv, idAlpha) {
		t.Fatalf("core reads %q as a process holding session %s, so this fixture is a *holder* and the "+
			"wake below is supposed to be refused - the two halves of the marker rule have swapped",
			argv, idAlpha)
	}

	if got := wakeOutcome(c, idAlpha); !got.woke {
		t.Errorf("a wake was refused with %q while the only process naming %s was a client with the bare "+
			"id in its argv. `wake attach <uuid>` is exactly that shape, and it is the shape an operator "+
			"has open at the moment they park", got.why, idAlpha)
	}
}

// Waking something that is not parked is refused, and the refusal is not
// cosmetic: a live session already has a process, so starting a second one on
// its id is the collision resumeSafe exists to prevent - reached through the
// one door resumeSafe cannot see, because Wake's own agent is holding it.
func TestWakingASessionThatIsNotParkedIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)

	got := wakeOutcome(c, idAlpha)
	if got.woke {
		t.Fatalf("a live session was woken and came back as %+v: it already had a process, so this "+
			"started a second one on its id - the collision resumeSafe exists to prevent, reached "+
			"through the one door resumeSafe cannot see, because Wake's own agent is holding it", got.row)
	}
	if why := got.why; !strings.Contains(why, "not parked") {
		t.Errorf("waking a live session was refused with %q, which does not say it is not parked", why)
	}
	if st := c.status(); stateOf(st, idAlpha) != rpc.StateIdle {
		t.Errorf("a refused wake moved a live session to %q, want it left alone", stateOf(st, idAlpha))
	}
}

// And waking an id this daemon has never held says so rather than starting one.
//
// Reaching a transcript off disk that no daemon here recorded is session
// importing's, and it needs the directory the session ran in: claude locates a
// transcript by the working directory it was started in, and resuming in a
// different one is completely unrecorded (2026-08-10 findings §12). The same
// argument forkSource makes about a stranger's parent.
func TestWakingAnIdThisDaemonNeverHeldIsRefused(t *testing.T) {
	d := startDaemon(t)
	c := attach(t, d.socket)

	got := wakeOutcome(c, idBeta)
	if got.woke {
		t.Fatalf("an id this daemon has never held was woken and came back as %+v: nothing here knows "+
			"which directory that transcript was recorded in, and resuming somewhere else is "+
			"completely unrecorded", got.row)
	}
	if !strings.Contains(got.why, "unknown session") {
		t.Errorf("waking an id the daemon does not hold was refused with %q, want it named as unknown", got.why)
	}
}

// "I could not check" is a refusal, and it is the arm with no other way in.
//
// idsInUse answers three things and only two of them are answers: the id is
// held, the id is free, and the machine could not be asked. A ps that runs and
// rejects these flags - busybox in a container, anything neither procps nor BSD
// - fails for every id, and folding that into "free" would make the check
// silently unconditional on exactly the machines where nobody would notice.
//
// The refusal has to say which of the two it is, because the operator's next
// move differs: "close it there first" is an instruction, and "I could not
// look" is a broken machine.
func TestAWakeIsRefusedWhenTheMachineCannotBeAsked(t *testing.T) {
	// The three ways a ps can run and fail to answer, and none of them is the
	// same as "that id is free". `quiet` is the one this file added a guard
	// for: exit 0 with an empty listing, which no stock ps produces and which
	// would otherwise answer "nothing is holding it" for every id on every call.
	for _, how := range []string{psRefuses, psQuiet, psHangs} {
		t.Run(how, func(t *testing.T) {
			fakeClaudeOnPath(t, "")
			d := startDaemon(t)
			c := attach(t, d.socket)
			c.spawn(idAlpha, "alex")
			c.awaitState(idAlpha, rpc.StateIdle)
			c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
			c.awaitState(idAlpha, rpc.StateParked)

			// Shadowed rather than removed, for brokenPsOnPath's own reason: a
			// missing ps fails at exec, which is already an error, and the
			// dangerous case is a ps that *runs* and then cannot answer.
			brokenPsOnPath(t, how)
			shortProbeTimeout(t, 200*time.Millisecond)

			got := wakeOutcome(c, idAlpha)
			if got.woke {
				t.Fatalf("session %s was woken on a machine whose ps could not answer, so nothing proved "+
					"its parked process was gone. \"I could not check\" is not \"nothing is there\"", idAlpha)
			}
			if why := got.why; !strings.Contains(why, "could not check") {
				t.Errorf("a wake on a machine whose ps cannot answer was refused with %q, which does not say the "+
					"check itself failed. A refusal that reads as \"something is holding it\" sends the operator "+
					"hunting for a process that is not there", why)
			}
			if st := c.status(); stateOf(st, idAlpha) != rpc.StateParked {
				t.Errorf("a wake that could not be proved safe left session %s in state %q, want parked",
					idAlpha, stateOf(st, idAlpha))
			}
		})
	}
}

// A wake that cannot start a process leaves the session parked, which is the
// whole of what `replaces` changes about launch.
//
// Both halves are separately falsifiable and each is a different loss. The row
// is what `--resume` needs - without it the id is gone and the transcript is
// unreachable from Wake. The name is what the room routes on, and releasing it
// hands `@alex` to whoever spawns next while the real alex is still parked
// under it.
//
// The failure is manufactured by taking `claude` off PATH between the park and
// the wake, which is the honest shape of it: launch fails at exec, before there
// is a process or an agent, which is the one path where a name released here
// would never be re-claimed.
//
// **ps stays on PATH, and that is not tidiness.** The first draft pointed PATH
// at an empty directory, which took ps with it - so resumeSafe refused before
// launch was ever called and the test passed while never reaching the guard it
// is named for. Found by mutating the guard: releasing the name unconditionally
// survived. A test that cannot reach its subject is a test that agrees with
// every mutation of it.
func TestAWakeThatCannotStartAProcessLeavesTheSessionParkedAndNamed(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	onlyPsOnPath(t)

	if got := wakeOutcome(c, idAlpha); got.woke {
		t.Fatalf("a wake started a session with no claude on PATH at all, and reported it as %+v", got.row)
	}

	if st := c.status(); stateOf(st, idAlpha) != rpc.StateParked {
		t.Errorf("a wake that could not start a process left session %s in state %q, want parked: the row "+
			"is the only thing that says which id to hand --resume, and the operator has to be able to "+
			"try again", idAlpha, stateOf(st, idAlpha))
	}
	// The name, from the far side: a spawn asking for it must be refused *by
	// the name*.
	//
	// There is deliberately no `started` arm. onlyPsOnPath leaves no claude
	// anywhere, so a spawn cannot start whatever the registry says, and a cell
	// asserting a verdict over an outcome that cannot arrive is the class
	// cmd/wake/forkguard_test.go exists to police. Nothing is lost: a released
	// name lets the spawn past nameRegistry.claim and it fails at exec instead,
	// whose message names no name - which is exactly what the one assertion
	// below catches, and it is how the mutation that releases it dies.
	why, _ := c.spawnOutcome(idBeta, "alex")
	if !strings.Contains(why, "alex") {
		t.Errorf("the spawn was refused with %q, which does not name the name it asked for. A launch that "+
			"never produced an agent released it, so the spawn got past the registry and failed at exec "+
			"instead - and alex comes back to find somebody else answering to it", why)
	}
}

// The parent edge travels with a wake, and Wake's copy is the only copy.
//
// Nothing on claude's wire says a session was forked or from what - a fork's
// init carries 23 top-level keys and not one names an ancestor (2026-08-10
// findings §6) - so this is the daemon's own memory, held exactly as long as
// the two agents it relates. A wake rebuilds the agent from scratch, which is
// the one moment that memory can be dropped, and dropping it is silent: the row
// comes back complete in every other field and the DM header simply stops
// saying who this was forked from.
func TestAWokenForkStillKnowsWhatItWasForkedFrom(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	fork := forkOf(c, idAlpha, idGamma, "")
	if fork.ParentID != idAlpha {
		t.Fatalf("the fork reports ParentID %q before it is parked, want %q: there is no edge here to "+
			"lose and the assertion below would hold over nothing", fork.ParentID, idAlpha)
	}

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idGamma})
	c.awaitState(idGamma, rpc.StateParked)
	woken := wakeOutcome(c, idGamma)
	if !woken.woke {
		t.Fatalf("the parked fork was not woken: %s", woken.why)
	}

	got := woken.row
	if got.ParentID != idAlpha {
		t.Errorf("a woken fork reports ParentID %q, want %q. Nothing on either stream carries this, so a "+
			"wake that does not hand it back has thrown the relationship away for good",
			got.ParentID, idAlpha)
	}
}

// A woken session keeps the label it parked with rather than having it
// re-derived, and the checkout in the middle is what makes that falsifiable.
//
// rpc.FrameWake's own doc comment and CLAUDE.md both promise the label is kept,
// and launch derives one from the directory - so without a branch change
// between the park and the wake the assertion is true of the fixture rather
// than of the code, and a review found exactly that. A parked session is one
// somebody comes back to hours later; a checkout in between is the ordinary
// case, not a contrivance.
//
// The floors matter as much as the assertion. If the spawn's label is not the
// first branch, or if the second branch is not visible to taskLabel, then the
// two labels are equal for a reason that has nothing to do with the wake.
func TestAWokenSessionKeepsTheLabelItParkedWithRatherThanReDerivingIt(t *testing.T) {
	fakeClaudeOnPath(t, "")
	dir := t.TempDir()
	checkoutBranch(t, dir, "dev-5748")

	d := startDaemon(t)
	c := attach(t, d.socket)
	before := spawnFor(c, idAlpha, "alex", dir)
	if before.Label != "dev-5748" {
		t.Fatalf("the session spawned with label %q, want the branch checked out where it started: this "+
			"test cannot show a label surviving one it never had", before.Label)
	}
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	// The checkout somebody does while the session is parked.
	checkoutBranch(t, dir, "main")
	if derived := taskLabel(dir); derived == before.Label {
		t.Fatalf("taskLabel still answers %q for %s after the checkout, so re-deriving and keeping are "+
			"the same answer here and this test asserts nothing", derived, dir)
	}

	got := wakeOutcome(c, idAlpha)
	if !got.woke {
		t.Fatalf("the parked session was not woken: %s", got.why)
	}
	if got.row.Label != before.Label {
		t.Errorf("a woken session came back labelled %q, want the %q it parked with. A wake reuses the "+
			"session's own id and keeps its name and its directory; re-deriving the label relabels a "+
			"conversation nobody moved, and two documents promise it is kept",
			got.row.Label, before.Label)
	}
}
