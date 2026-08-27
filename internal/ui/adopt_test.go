package ui

// `/adopt` from the outside: the word, the set, and the ids it waits on.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// `/import` is not Wake's word, and the recorded corpus is why.
//
// This is the finding made falsifiable rather than written down. The obvious
// spelling for the founding message's *"select which ones you would like to
// add"* is `/import`, and `import` is what claude advertises on its `init`
// frame on the machine these recordings came from - so taking it would replace
// a working command with a refusal, which is the exact failure
// `TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising` exists for.
//
// It is asserted **in both directions** because either half alone rots. If the
// corpus stopped carrying the word, a test that only checks Wake does not own
// it would pass over nothing and the next person would take the word back.
func TestImportIsNotWakesWordBecauseTheCorpusShowsClaudeAdvertisingIt(t *testing.T) {
	seen := recordedClaudeCommands(t)
	where, advertised := seen[refusedAdoptWord]
	if !advertised {
		t.Fatalf("%q is not in the recorded corpus any more, so the reason `/%s` is spelled the way it "+
			"is has gone: re-derive the word before trusting this",
			refusedAdoptWord, adoptCommand)
	}
	if _, mine := commands[refusedAdoptWord]; mine {
		t.Errorf("Wake owns %q and %s records claude advertising it: the shell verb is `wake import` and "+
			"the room's word has to be a different one", refusedAdoptWord, where)
	}
	if _, mine := commands[adoptCommand]; !mine {
		t.Errorf("the room has no %q command, so nothing in this build reaches the picker without "+
			"leaving it", adoptCommand)
	}
}

// The plural is the feature: one command, one moment, a set.
//
// The founding sentence is *"select which **ones** you would like to add"*, and
// `wake import` takes one session per invocation. This is what closes it, so
// the assertion is on the whole set arriving from one draft - and on it being
// **one** command, which is App.write's rule: bubbletea runs every tea.Cmd on
// its own goroutine and rpc's write lock is process-wide.
func TestAdoptImportsEverySessionNamedInOneCommand(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).WithSessions(fakeSessions{
		resolved: []string{srcA, srcB},
	})

	next, cmd := adoptArrival(t, a, "/adopt 1111 2222")

	frames := sentFrames(t, next, cmd)
	if len(frames) != 2 {
		t.Fatalf("/adopt over two sessions wrote %d frames, want two: %+v", len(frames), frames)
	}
	sources := map[string]bool{}
	for _, f := range frames {
		if f.Kind != rpc.FrameImport {
			t.Errorf("/adopt wrote a %q frame: an import is the one kind that carries a source, and a "+
				"spawn under the same id is the plausible-looking empty agent it would become", f.Kind)
		}
		sources[f.ParentID] = true
	}
	if !sources[srcA] || !sources[srcB] {
		t.Errorf("/adopt asked for %v, want both %s and %s: a set the operator chose is not a set the "+
			"command may narrow", sources, srcA, srcB)
	}
	if n := commandCount(cmd); n != 1 {
		t.Errorf("/adopt over two sessions used %d commands. Thirty would be thirty goroutines queueing "+
			"on one process-wide write lock for one keystroke", n)
	}
}

// The id `/adopt` waits on is the one it minted, never the source's.
//
// `wake fork`'s rule, for `wake fork`'s reason, and the failure is silent
// rather than loud: the daemon addresses every import refusal to the **new**
// session's id, so a client waiting on the source's id is not refused - it
// waits forever with nothing on screen, which is indistinguishable from a
// daemon that is thinking.
func TestTheIdsAdoptWaitsOnAreTheOnesItMintedAndNeverTheSources(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).WithSessions(fakeSessions{
		resolved: []string{srcA, srcB},
	})

	next, cmd := adoptArrival(t, a, "/adopt 1111 2222")
	frames := sentFrames(t, next, cmd)

	if len(next.pendingStarts) != 2 {
		t.Fatalf("/adopt over two sessions waits on %d ids, want two: %v", len(next.pendingStarts), next.pendingStarts)
	}
	for _, src := range []string{srcA, srcB} {
		if _, waiting := next.pendingStarts[src]; waiting {
			t.Errorf("/adopt waits on the source id %s. The daemon addresses every import refusal to the "+
				"new session's id, so this wait is never settled and never reported", src)
		}
	}
	for _, f := range frames {
		if _, waiting := next.pendingStarts[f.SessionID]; !waiting {
			t.Errorf("/adopt asked for session %s and is not waiting on it: the conversation the operator "+
				"asked for would never open", f.SessionID)
		}
		if f.SessionID == f.ParentID {
			t.Errorf("the frame names one id twice (%s): an import is a fork onto a **new** id, and the "+
				"source keeps its own", f.SessionID)
		}
	}
}

// A bare `/adopt` puts the picker in the room.
//
// The notice row is one line and this listing is many, so it goes into a
// transcript - and it is the **room's**, never a DM's, whichever pane has the
// keys. A bang goes to the conversation it was typed into because a bang is
// addressed to that conversation; `/adopt` is addressed to Wake, and a
// machine-wide listing pasted into a DM would make Wake author a turn inside
// somebody's conversation with claude.
func TestABareAdoptPutsThePickerInTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).WithSessions(fakeSessions{
		listing: "2 sessions on this machine\n  deadbeef  2h ago  /tmp/one\n",
	})

	next, _ := adoptArrival(t, a, "/adopt")

	if got := shown(next); !strings.Contains(got, "deadbeef") || !strings.Contains(got, "/tmp/one") {
		t.Errorf("a bare /adopt did not put the picker in the room:\n%s", got)
	}
}

// Nothing is adopted when one of the names does not resolve.
//
// All or nothing, and the reason is what a retry costs: the daemon refuses a
// source that is already in the fleet, so adopting two of three and refusing
// the third leaves the operator with a command they cannot simply retype.
func TestAdoptRefusesTheWholeSetWhenOneNameDoesNotResolve(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).WithSessions(fakeSessions{
		err: errors.New("no session on this machine has an id starting \"zzzz\""),
	})

	next, cmd := adoptArrival(t, a, "/adopt 1111 zzzz")

	if cmd != nil {
		t.Fatalf("an import went out for a set that did not resolve: %+v", sentFrames(t, next, cmd))
	}
	if len(next.pendingStarts) != 0 {
		t.Errorf("/adopt is waiting on %v after refusing the set: a wait nothing will settle is a "+
			"conversation that never opens and never says why", next.pendingStarts)
	}
	if got := shown(next); !strings.Contains(got, "zzzz") {
		t.Errorf("the refusal does not name what could not be resolved:\n%s", got)
	}
}

// A row is not an offer here either, and the picker is not this side's
// decision to make.
//
// The listing says which sessions exist on disk. Whether one may be imported is
// the daemon's question, re-decided over five refusals every time - so a
// directory this side cannot prove is **still asked for**, and the operator
// reads the daemon's own sentence rather than a local guess at it.
func TestAdoptAsksForASessionTheListingCouldNotProveADirectoryFor(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).WithSessions(fakeSessions{resolved: []string{srcA}})

	next, cmd := adoptArrival(t, a, "/adopt 1111")

	frames := sentFrames(t, next, cmd)
	if len(frames) != 1 || frames[0].ParentID != srcA {
		t.Fatalf("/adopt did not ask for %s: %+v", srcA, frames)
	}
}

// A model with no way to see this machine says so rather than doing nothing.
//
// The seam is injected by cmd/wake, exactly as the dialer is, so a model built
// without one is a real shape - every unit test in this package is one. Silence
// there would be a command that looks like it worked.
func TestAdoptWithNoWayToSeeThisMachineSaysSo(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30)

	next, cmd, handled := a.slash("/adopt")
	if !handled {
		t.Fatal("/adopt was not taken by the router")
	}
	if cmd != nil {
		t.Fatalf("a command went out with no way to read this machine: %+v", sentFrames(t, next, cmd))
	}
	if got := shown(next); !strings.Contains(got, "cannot") {
		t.Errorf("a model with no session seam said nothing about it:\n%s", got)
	}
}

// The picker is asked for off the draw goroutine.
//
// Discovery walks every transcript under ~/.claude/projects - 428 files on the
// recording machine - and Bubble Tea has one Update goroutine and it renders.
// Doing that walk inline freezes the room for the length of the walk, which is
// the "no work on the draw path that could be work on a command" rule the inbox
// already exists for.
func TestAdoptReadsTheMachineOffTheDrawGoroutine(t *testing.T) {
	seam := &countingSessions{listing: "nothing here\n"}
	a := newRoomApp(t).withSize(160, 30).WithSessions(seam)

	next, cmd, handled := a.slash("/adopt")
	if !handled {
		t.Fatal("/adopt was not taken by the router")
	}
	if seam.calls != 0 {
		t.Fatalf("the machine was read %d times while the router ran: that walk is on the goroutine "+
			"that draws", seam.calls)
	}
	if cmd == nil {
		t.Fatal("/adopt returned no command, so the picker is never asked for at all")
	}
	msg := cmd()
	if _, ok := msg.(adoptedMsg); !ok {
		t.Fatalf("the command produced %T, want an adoptedMsg the model can fold", msg)
	}
	if seam.calls != 1 {
		t.Errorf("the command read the machine %d times, want once", seam.calls)
	}
	_ = next
}

// A machine read that never answers costs one goroutine, not one per repeated
// command. The completion path applies the same bound to its filesystem read.
func TestAdoptStartsOnlyOneMachineReadAtATime(t *testing.T) {
	seam := &countingSessions{listing: "nothing here\n"}
	a := newRoomApp(t).withSize(160, 30).WithSessions(seam)

	reading, cmd, handled := a.slash("/adopt")
	if !handled || cmd == nil {
		t.Fatal("the first /adopt did not start a machine read")
	}
	waiting, second, handled := reading.slash("/adopt")
	if !handled {
		t.Fatal("the second /adopt was not taken by the router")
	}
	if second != nil {
		t.Fatal("a second /adopt started another machine read while the first was still outstanding")
	}

	msg := cmd()
	model, _ := waiting.Update(msg)
	_, third, handled := model.(App).slash("/adopt")
	if !handled || third == nil {
		t.Fatal("/adopt did not allow another machine read after the first answered")
	}
}

// The single-flight guard is only half the answer: the read it holds behind is
// bounded by nothing, so a stalled mount latches it for the life of the window.
// A command that says nothing at all is the silence cannotSeeMachine exists to
// avoid, and the draft is kept because the read it was refused by may never
// answer.
func TestASecondAdoptWhileOneIsOutstandingSaysSoAndKeepsTheDraft(t *testing.T) {
	notice.Reset()
	seam := &countingSessions{listing: "nothing here\n"}
	a := newRoomApp(t).withSize(160, 30).WithSessions(seam)

	reading, cmd, _ := a.slash("/adopt")
	if cmd == nil {
		t.Fatal("the first /adopt did not start a machine read")
	}
	typed := "/adopt " + srcA
	waiting, second, _ := reading.withDraft(typed).slash(typed)
	if second != nil {
		t.Fatal("a second /adopt started another machine read while the first was outstanding")
	}
	if notice.Count(adoptStillReading) != 1 {
		t.Error("the refused /adopt said nothing, so an operator cannot tell it from a command that worked")
	}
	if got := waiting.composer().Value(); got != typed {
		t.Errorf("draft = %q, want %q kept: the read that refused it may never answer", got, typed)
	}
}

// --- fixtures ------------------------------------------------------------

const (
	srcA = "11111111-1111-4111-8111-111111111111"
	srcB = "22222222-2222-4222-8222-222222222222"
)

// fakeSessions is the seam with the answers written down. It records nothing;
// countingSessions is for the one test that asks when it was called.
type fakeSessions struct {
	listing  string
	resolved []string
	err      error
}

func (f fakeSessions) Listing() (string, error) { return f.listing, f.err }

func (f fakeSessions) Resolve(typed []string) ([]string, error) {
	if f.err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(typed, " "), f.err)
	}
	return f.resolved, nil
}

type countingSessions struct {
	listing string
	calls   int
}

func (c *countingSessions) Listing() (string, error) { c.calls++; return c.listing, nil }

func (c *countingSessions) Resolve(typed []string) ([]string, error) {
	c.calls++
	return nil, fmt.Errorf("no")
}

// adoptArrival runs one `/adopt` draft the way Bubble Tea does: the router
// hands back a command, the command produces a message, and the model folds it.
//
// Written out rather than hidden because the two halves are separately
// falsifiable - a router that wrote frames inline would satisfy the second half
// of every assertion while doing the disk walk on the goroutine that draws.
func adoptArrival(t *testing.T, a App, draft string) (App, tea.Cmd) {
	t.Helper()
	next, cmd, handled := a.slash(draft)
	if !handled {
		t.Fatalf("%q was not taken by the router", draft)
	}
	if cmd == nil {
		t.Fatalf("%q produced no command, so the machine is never read", draft)
	}
	msg, ok := cmd().(adoptedMsg)
	if !ok {
		t.Fatalf("%q produced %T, want an adoptedMsg", draft, cmd())
	}
	model, out := next.Update(msg)
	return model.(App), out
}

// The claim an import owes is said when it is asked for, not when it arrives.
//
// `cmd/wake`'s `importedNotice` makes two claims about an adopted session and
// the arrival sentence in a room makes only one of them. The one it makes is
// the snapshot - `SnapshotNotice`'s `forkOpenedUnnamed` arm, because an
// import's parent is a session id no fleet holds - and the one it does not is
// the half that is specific to a *stranger's* session: **the original may still
// be open in a terminal, and Wake cannot tell.**
//
// It belongs on the ask rather than on the arrival, and that is the card-arm
// argument one door over: the moment the operator can still decide otherwise is
// before the frame is written. After it, a `claude` is running.
func TestAdoptSaysWhatItCannotKnowAboutTheOriginalBeforeItAsks(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(160, 30).WithSessions(fakeSessions{resolved: []string{srcA}})

	next, _ := adoptArrival(t, a, "/adopt 1111")

	got := shown(next)
	if !strings.Contains(got, "copy") {
		t.Errorf("/adopt never says an adopted session is a copy:\n%s", got)
	}
	if !strings.Contains(got, "still open") {
		t.Errorf("/adopt never says the original may still be open in a terminal. Wake cannot detect "+
			"that - 2026-08-12 findings §5 counted live `claude` processes whose whole argv is the "+
			"word `claude` - so this sentence is the whole of the warning anybody gets, and the room "+
			"is now a surface where an import happens:\n%s", got)
	}
}
