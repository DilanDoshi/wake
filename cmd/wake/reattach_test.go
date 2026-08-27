package main

// `wake attach <id>`: the way back into a conversation that is still running.
//
// Everything the verb does before it opens a terminal is here - which session,
// and every way there is not one. The half that needs a real agent behind it is
// in reattach_unix_test.go.

import (
	"bytes"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The session ids these tests use, minted once per test binary.
//
// They were fixed constants, and `internal/daemon/harness_test.go` carries the
// whole argument for why that broke: two test binaries running at once each see
// the other's fake agent holding the id, and `resumeSafe` correctly refuses the
// wake. `go test ./...` runs packages in parallel by default, and this package
// and that one both spawn agents - so the two halves of the collision were
// already shipping together.
//
// The nonce is per-process here too, and deliberately not shared with that
// package: two test binaries must not agree on it, which is the opposite of
// what a shared helper would give.
var (
	idAlpha = testSessionID("a11a")
	idBeta  = testSessionID("b22b")
)

var testRunNonce = fmt.Sprintf("%08x", rand.Uint32())

// testSessionID is one fixture role's id for this process. The tag sits at both
// ends so a failure message still reads a11a...a11a, and only the node field
// moves, so it stays the valid v4 UUID `mintedByWake` requires.
func testSessionID(tag string) string {
	return tag + "0000-0000-4000-8000-" + testRunNonce + tag
}

func liveFleet() rpc.Status {
	return rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", State: rpc.StateWorking},
		{ID: idBeta, Name: "sam", State: rpc.StateIdle},
	}}
}

// The whole verb rests on this: an id that names a session the daemon is
// holding resolves to that session, with the name the DM will put in its
// header.
func TestAttachFindsALiveSession(t *testing.T) {
	d := startFakeDaemon(t, 0, liveFleet())

	got, _, err := liveSession(d.socket, idBeta)
	if err != nil {
		t.Fatalf("liveSession: %v", err)
	}
	if got.ID != idBeta || got.Name != "sam" {
		t.Errorf("attached to %+v, want the session named sam", got)
	}
}

// A session id is a UUID and `wake status` prints the first few characters of
// one, so a prefix is what a person actually has in front of them.
func TestAttachTakesAUniquePrefix(t *testing.T) {
	d := startFakeDaemon(t, 0, liveFleet())

	got, _, err := liveSession(d.socket, idAlpha[:8])
	if err != nil {
		t.Fatalf("liveSession on a prefix: %v", err)
	}
	if got.ID != idAlpha {
		t.Errorf("a prefix of alpha resolved to %s", got.ID)
	}
}

// Ambiguity is refused rather than guessed at. The thing being picked is
// somebody's conversation, and picking the wrong one is not a recoverable
// mistake: they type into it.
func TestAnAmbiguousPrefixIsRefusedRatherThanGuessed(t *testing.T) {
	d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "abc10000-0000-4000-8000-000000000001", Name: "alex", State: rpc.StateIdle},
		{ID: "abc20000-0000-4000-8000-000000000002", Name: "sam", State: rpc.StateIdle},
	}})

	_, _, err := liveSession(d.socket, "abc")
	if err == nil {
		t.Fatal("an ambiguous prefix picked a session")
	}
	if !strings.Contains(err.Error(), "names 2 sessions") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	if !strings.Contains(err.Error(), "alex") || !strings.Contains(err.Error(), "sam") {
		t.Errorf("the refusal does not list what it was choosing between: %v", err)
	}
}

// An id nothing is holding must not become a connection. A client that attaches
// to nothing gets a working socket and a conversation that never says
// anything, which is indistinguishable from an agent that is thinking.
func TestAttachRefusesAnIdNothingIsHolding(t *testing.T) {
	d := startFakeDaemon(t, 0, liveFleet())

	_, _, err := liveSession(d.socket, "nosuchsession")
	if err == nil {
		t.Fatal("attaching to an id nothing is holding was allowed")
	}
	if !strings.Contains(err.Error(), "no session here starts with") {
		t.Errorf("the refusal does not name the problem: %v", err)
	}
	// And it lists what there is, so a wrong id costs one command rather than
	// two.
	if !strings.Contains(err.Error(), shortID(idAlpha)) {
		t.Errorf("the refusal does not list what is running: %v", err)
	}
}

// A session that has ended is not attachable, and why it ended is the thing
// worth saying - the row is about to disappear from `wake status` too.
func TestAttachRefusesAnEndedSession(t *testing.T) {
	d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", State: rpc.StateEnded, Error: "exit status 1"},
	}})

	_, _, err := liveSession(d.socket, idAlpha)
	if err == nil {
		t.Fatal("attaching to an ended session was allowed")
	}
	if !strings.Contains(err.Error(), "has ended") || !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("the refusal does not say the session ended, or why: %v", err)
	}
}

// No daemon is a different failure from no session, and it must not be answered
// by starting one: connect() calls EnsureRunning, which forks a daemon that has
// never heard of this session, and attaching would then appear to work.
func TestAttachRefusesWhenNoDaemonIsRunning(t *testing.T) {
	socket := tempSocket(t)

	_, _, err := liveSession(socket, idAlpha)
	if err == nil {
		t.Fatal("attaching with no daemon running was allowed")
	}
	if !strings.Contains(err.Error(), "no daemon is running") {
		t.Errorf("the refusal does not name the problem: %v", err)
	}
}

// --- the command line -------------------------------------------------------

// `wake attach` with no id could plausibly mean "the only one running", and
// that guess is wrong the moment there are two - which is the ordinary case for
// a product whose premise is 15-30 of them.
func TestAttachWithNoIdAsksRatherThanGuessing(t *testing.T) {
	err := run([]string{cmdAttach}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("`wake attach` with no id did something")
	}
	if !strings.Contains(err.Error(), "one session id") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
}

func TestAttachWithTwoIdsIsRefused(t *testing.T) {
	err := run([]string{cmdAttach, idAlpha, idBeta}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("`wake attach` took two session ids")
	}
	if !strings.Contains(err.Error(), "one session id") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}

// The verbs that take nothing still take nothing. Adding an arity rule for one
// of them is exactly how the others lose theirs.
func TestTheOtherVerbsStillTakeNoArguments(t *testing.T) {
	for _, verb := range []string{cmdStatus, cmdStop} {
		err := run([]string{verb, "extra"}, &bytes.Buffer{})
		if err == nil {
			t.Fatalf("`wake %s extra` was accepted", verb)
		}
		if !strings.Contains(err.Error(), "takes no arguments") {
			t.Errorf("`wake %s extra`: %v", verb, err)
		}
	}
}

// A typo must not become an agent. `wake atach` reaching the spawn path would
// make every mistyped verb a new claude process.
func TestAMistypedVerbIsRefusedRatherThanSpawning(t *testing.T) {
	err := run([]string{"atach", idAlpha}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("a mistyped verb was accepted")
	}
	if !strings.Contains(err.Error(), `unknown command "atach"`) {
		t.Errorf("a mistyped verb was not reported as one: %v", err)
	}
	if !strings.Contains(err.Error(), "wake attach") {
		t.Errorf("the usage does not mention the verb that was mistyped: %v", err)
	}
}

// `wake attach` reaching a real socket and failing there - rather than being
// swallowed by dispatch - is the property that a verb reaches its own command.
// The dispatch switch has been silently wrong here before.
func TestAttachReachesItsOwnCommand(t *testing.T) {
	socket := tempSocket(t)
	t.Setenv("WAKE_SOCKET", socket)

	err := run([]string{cmdAttach, idAlpha}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("`wake attach` against nothing succeeded")
	}
	if !strings.Contains(err.Error(), "no daemon is running") {
		t.Errorf("`wake attach` did not reach liveSession: %v", err)
	}
}

// The ordering inside redial, which nothing pinned.
//
// connect() calls daemon.EnsureRunning, and EnsureRunning *forks a daemon* when
// nothing is listening. So a redial that dials before it asks the fleet answers
// "the daemon died" by starting a fresh one that has never heard of this
// session - and then fails, having left a daemon behind that `wake status` will
// report as running. The next `wake attach` against it finds a live daemon and
// no session, which is a different and more confusing failure than the true one.
//
// Mutation check: swapping the liveSession and connect calls in redial leaves
// the whole cmd/wake suite green except this, which fails at "a reattach that
// could not find its session started a daemon". The end-to-end reattach test is
// killed by dropping held.replace, not by reordering - it never noticed.
func TestAFailedReattachDoesNotStartADaemon(t *testing.T) {
	socket := tempSocket(t)
	t.Setenv(daemon.SocketEnv, socket)

	// Registered before anything can fork one, because a daemon started by the
	// mutation must not outlive the test: tempSocket's cleanup deletes the
	// directory its socket, roster and lock live in.
	t.Cleanup(func() { _ = stopFleet(socket, io.Discard) })

	held := &connection{}
	t.Cleanup(held.close)

	if _, _, _, _, err := redial(socket, idAlpha, held); err == nil {
		t.Fatal("redial succeeded against a socket with no daemon on it")
	}

	st, err := daemon.Status(socket)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Running {
		t.Errorf("a reattach that could not find its session started a daemon: it is now answering on %s, and it has never heard of the session", socket)
	}
}

// A status report carries recent endings, so an agent that ended a minute ago
// is still in the list a prefix is matched against. A prefix that is
// unambiguous to the person typing it must not be refused because of a row they
// can no longer attach to.
//
// Mutation check: putting ended sessions back in the same bucket as live ones
// leaves this failing at `"abc" names 2 sessions`.
func TestALiveSessionWinsAPrefixItSharesWithAnEndedOne(t *testing.T) {
	const live, dead = "abc10000-0000-4000-8000-000000000001", "abc20000-0000-4000-8000-000000000002"
	d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: dead, Name: "gone", State: rpc.StateEnded, Error: "exit status 1"},
		{ID: live, Name: "alex", State: rpc.StateWorking},
	}})

	got, _, err := liveSession(d.socket, "abc")
	if err != nil {
		t.Fatalf("a prefix shared only with an ended session was refused: %v", err)
	}
	if got.ID != live {
		t.Errorf("attached to %s, want the live session %s", got.ID, live)
	}
}

// And when every match has ended, the refusal says so rather than claiming
// there is no such session - the two are different things to the person who
// just watched one finish.
func TestAPrefixMatchingOnlyEndedSessionsSaysTheySEnded(t *testing.T) {
	d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", State: rpc.StateEnded, Error: "exit status 1"},
	}})

	_, _, err := liveSession(d.socket, idAlpha[:8])
	if err == nil {
		t.Fatal("attaching to an ended session by prefix was allowed")
	}
	if !strings.Contains(err.Error(), "has ended") {
		t.Errorf("the refusal does not say the session ended: %v", err)
	}
}

// An unnamed session is named the same way in both places it is named. It was
// "@(unnamed)" on the notice row and a bare "@" in the DM header, which reads
// as two different agents.
func TestAnUnnamedSessionIsNamedTheSameWayEverywhere(t *testing.T) {
	s := rpc.SessionStatus{ID: idAlpha, State: rpc.StateIdle}
	if got := displayName(s); got == "" {
		t.Fatal("an unnamed session has no display name at all, so the header would draw a bare @")
	}
	named := rpc.SessionStatus{ID: idAlpha, Name: "alex", State: rpc.StateIdle}
	if got := displayName(named); got != "alex" {
		t.Errorf("displayName(%+v) = %q", named, got)
	}
}
