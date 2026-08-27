package ui

// Mention mode: what `@alex hey` means.
//
// §7 makes it configurable because the two readings differ by 20× in cost -
// one turn, or one per agent. Everything here is about which of those two a
// leading mention buys, and about the three routes that are *not* a mention
// and must be untouched by the mode: @all, @manager, and the draft nobody
// addressed.

import (
	"go/ast"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The feature, both readings, over one draft.
//
// Direct is the default and is what this build has always done. Open is the
// other reading: everybody hears it and one of them is addressed.
func TestAMentionReachesOneAgentWhenDirectAndTheFleetWhenOpen(t *testing.T) {
	room := func(mode MentionMode) App {
		a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
		a.mention = mode
		return a.withDraft("@john ship it")
	}

	a, cmd := pressKey(room(MentionDirect), tea.KeyMsg{Type: tea.KeyEnter})
	direct := sentFrames(t, a, cmd)
	if len(direct) != 1 {
		t.Fatalf("a direct mention sent %d frames, want 1: direct is one agent and one turn", len(direct))
	}
	if direct[0].SessionID != idOfAgentNamed(t, a, "john") {
		t.Errorf("a direct mention reached %q, want john", direct[0].SessionID)
	}

	a, cmd = pressKey(room(MentionOpen), tea.KeyMsg{Type: tea.KeyEnter})
	open := sentFrames(t, a, cmd)
	if len(open) != 3 {
		t.Fatalf("an open mention sent %d frames, want one per live agent (3): open is everybody hearing it", len(open))
	}
	if n := commandCount(cmd); n != 1 {
		t.Errorf("an open mention used %d commands: it is a fan-out like @all, and thirty of those would be "+
			"thirty goroutines queueing on one process-wide write lock for one keystroke", n)
	}
}

// The mention stays in the text when everybody is receiving it, and that is
// the whole of what makes open mode safe rather than catastrophic.
//
// Stripping it is the obvious implementation - direct mode strips it, and the
// widening is one line away from reusing that text. It would hand nineteen
// agents a bare instruction with nothing in it saying whose it was, so
// nineteen agents start doing it. Direct mode still strips it, because there
// the pane and the recipient are the same fact.
func TestAnOpenMentionKeepsTheNameSoTheRestKnowItIsNotForThem(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
	a.mention = MentionOpen
	a, cmd := pressKey(a.withDraft("@john ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	for _, f := range sentFrames(t, a, cmd) {
		if f.Text != "@john ship it" {
			t.Errorf("an open mention sent %q. The name has to survive: a fleet handed \"ship it\" with "+
				"nothing saying whose it was is a fleet where everybody ships it", f.Text)
		}
	}

	b := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
	b, cmd = pressKey(b.withDraft("@john ship it"), tea.KeyMsg{Type: tea.KeyEnter})
	if f := sentFrame(t, b, cmd); f.Text != "ship it" {
		t.Errorf("a direct mention sent %q: the mention is stripped before sending, not echoed at the agent", f.Text)
	}
}

// Open mode reaches exactly what @all reaches, and the list is derived from
// @all rather than restated here.
//
// That is the whole exclusion argument inherited for free. @all is to the
// fleet and the manager is not in it; a parked or ended session has no process
// to read anything. Open mode widens a mention *to the fleet*, so the day
// live() rules on a seventh state both routes move together - and the
// realistic mutant, widening to fleet.Agents() because it is the list already
// in hand, dies on all three at once.
func TestAnOpenMentionReachesExactlyWhatAllReaches(t *testing.T) {
	fleet := func() App {
		a := newRoomApp(t).withSize(200, 40)
		return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
			Running: true,
			Sessions: []rpc.SessionStatus{
				{ID: "s1", Name: "sydney", State: rpc.StateIdle},
				{ID: "s2", Name: "alex", State: rpc.StateWorking},
				{ID: "s3", Name: "john", State: rpc.StateBlocked},
				{ID: "s4", Name: "dana", State: rpc.StateParked},
				{ID: "s5", Name: "kim", State: rpc.StateEnded},
				{ID: "s6", Name: core.ManagerName, State: rpc.StateIdle},
			},
		}})
	}

	a, cmd := pressKey(fleet().withDraft("@all ship it"), tea.KeyMsg{Type: tea.KeyEnter})
	broadcast := frameIDs(sentFrames(t, a, cmd))
	if len(broadcast) < 3 {
		t.Fatalf("@all reached %v: the fixture has to have a fleet, or this compares two empty lists", broadcast)
	}

	b := fleet()
	b.mention = MentionOpen
	b, cmd = pressKey(b.withDraft("@sydney ship it"), tea.KeyMsg{Type: tea.KeyEnter})
	open := frameIDs(sentFrames(t, b, cmd))

	if strings.Join(open, ",") != strings.Join(broadcast, ",") {
		t.Errorf("an open mention reached %v and @all reaches %v. Open widens a mention to the fleet, and "+
			"the fleet is what @all is addressed to - a manager that hears one and not the other, or a "+
			"parked session addressed by one of them, is the same defect twice", open, broadcast)
	}
}

// @manager is not widened by open mode, for the reason it is not in @all: the
// manager is the thing that manages the fleet rather than a member of it.
//
// Stated once and true in both directions - the exclusion above says the fleet
// does not include it, and this says addressing it does not include the fleet.
func TestOpenModeDoesNotWidenAMentionOfTheManager(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", core.ManagerName)
	a.mention = MentionOpen
	managerID := idOfAgentNamed(t, a, core.ManagerName)

	a, cmd := pressKey(a.withDraft("@"+core.ManagerName+" who is stuck?"), tea.KeyMsg{Type: tea.KeyEnter})
	f := sentFrame(t, a, cmd)
	if f.SessionID != managerID {
		t.Errorf("@%s in open mode reached %q, want the manager alone: the manager is not in the fleet, so "+
			"widening a mention to the fleet cannot widen this one", core.ManagerName, f.SessionID)
	}
	if f.Text != "who is stuck?" {
		t.Errorf("the mention was not taken off: %q", f.Text)
	}
}

// The two routes nobody typed a name for are untouched by the mode.
//
// This is the half that has to hold or the toggle is a routing redesign: an
// unaddressed draft still goes to the manager and to nothing else, and with no
// manager the room still refuses rather than guessing. A widening keyed on
// "the route resolved" rather than on "a mention resolved to a fleet agent"
// passes every test above and turns `who is free` into thirty turns.
func TestOpenModeChangesNothingAboutADraftWithNoMention(t *testing.T) {
	withManager := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", core.ManagerName)
	withManager.mention = MentionOpen
	managerID := idOfAgentNamed(t, withManager, core.ManagerName)

	a, cmd := pressKey(withManager.withDraft("who is free"), tea.KeyMsg{Type: tea.KeyEnter})
	f := sentFrame(t, a, cmd)
	if f.SessionID != managerID {
		t.Errorf("an unaddressed draft in open mode went to %q, want the manager alone. Mention mode decides "+
			"what a mention means; a draft with no mention has none to widen", f.SessionID)
	}

	noManager := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex")
	noManager.mention = MentionOpen
	b, cmd := pressKey(noManager.withDraft("who is free"), tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("open mode sent an unaddressed message to %+v. The refusal is the default and stays the "+
			"default: with thirty members, send-to-whoever is not something an operator can mean",
			sentFrames(t, b, cmd))
	}
	if notice.Count(NoAddressee) != 1 {
		t.Error("the room swallowed the keystroke in open mode without saying how to address it")
	}
}

// @all is a broadcast in either mode, and it still says how many turns.
func TestOpenModeChangesNothingAboutABroadcast(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
	a.mention = MentionOpen
	a, cmd := pressKey(a.withDraft("@all ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	frames := sentFrames(t, a, cmd)
	if len(frames) != 3 {
		t.Fatalf("@all in open mode sent %d frames, want 3", len(frames))
	}
	for _, f := range frames {
		if f.Text != "ship it" {
			t.Errorf("@all in open mode sent %q, want the mention stripped as it always was", f.Text)
		}
	}
}

// An open mention echoes once, however many agents heard it - @all's rule, for
// @all's reason. One broadcast is one thing you said, not N.
func TestAnOpenMentionEchoesOnce(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
	a.mention = MentionOpen
	a, _ = pressKey(a.withDraft("@john ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	if n := strings.Count(shown(a), "ship it"); n != 1 {
		t.Errorf("your own message appears %d times, want 1:\n%s", n, shown(a))
	}
}

// --- the key ---------------------------------------------------------------

// The room opens in the cheap reading, with nothing having said so.
//
// The zero value carries this rather than a constructor, because an App built
// any other way - a test, a future caller - must not be the one that starts
// somebody in a mode where every `@name` costs thirty turns.
func TestTheRoomStartsInTheCheapReading(t *testing.T) {
	if got := newRoomApp(t).mention; got != MentionDirect {
		t.Errorf("a fresh room is in %s mode, want %s. §7 names direct the default, and the expensive "+
			"reading is the one an operator has to ask for", got, MentionDirect)
	}
	var zero App
	if got := zero.mention; got != MentionDirect {
		t.Errorf("the zero App is in %s mode: the default has to be the zero value, or an App built by "+
			"anything that is not the constructor starts thirty turns per mention", got)
	}
}

// ⌃T flips it, and flips it back.
func TestControlTFlipsTheMentionModeAndBack(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlT})
	if a.mention != MentionOpen {
		t.Fatalf("⌃T left the mode at %s, want %s", a.mention, MentionOpen)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlT})
	if a.mention != MentionDirect {
		t.Errorf("⌃T twice left the mode at %s, want it back at %s: it is a toggle over two readings, "+
			"not a cycle", a.mention, MentionDirect)
	}
}

// The flip reaches the target line without another keystroke.
//
// retarget runs on a keystroke and on a fleet report, which is what keeps it
// off every drawn frame - and ⌃T is neither of those things happening to the
// draft. A flip that did not re-read would leave `→ @john · direct` on screen
// over a send that fans out, which is exactly the promise this feature rests
// on, broken by the key that exists to change it.
func TestFlippingTheModeRedrawsWhereEnterWillSend(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john").withDraft("@john ship it")
	if got := a.room.Composer().Target(); got != "→ @john · direct" {
		t.Fatalf("the draft reads %q before ⌃T is pressed", got)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlT})
	if got := a.room.Composer().Target(); got != "→ @john · open · 3 turns" {
		t.Errorf("after ⌃T the draft still reads %q. The line is the whole safety argument for the mode "+
			"existing, so the key that changes the mode has to change the line", got)
	}
}

// ⌃T says what it just did, because the line that carries the mode is only
// drawn when there is a draft.
//
// Pressed on an empty composer - which is when somebody sets a mode, before
// typing - nothing on screen moves. A key that silently changes what the next
// message costs is the failure this project treats as worse than a refusal,
// arriving on the surface whose whole argument is "it is never a memory
// problem".
func TestControlTSaysWhichReadingItTurnedOn(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlT})
	if n := notice.Count(mentionFlipped(MentionOpen)); n != 1 {
		t.Errorf("⌃T reported %q %d times, want 1: on an empty composer there is no target line, so this "+
			"is the only thing that moves", mentionFlipped(MentionOpen), n)
	}
	if !strings.Contains(mentionFlipped(MentionOpen), MentionOpen.String()) ||
		!strings.Contains(mentionFlipped(MentionDirect), MentionDirect.String()) {
		t.Error("the report does not name the mode it turned on, so it says a change happened and not which")
	}

	back, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlT})
	if back.mention != MentionDirect {
		t.Fatalf("the second ⌃T left the mode at %s", back.mention)
	}
	if n := notice.Count(mentionFlipped(MentionDirect)); n != 1 {
		t.Errorf("flipping back reported %q %d times, want 1", mentionFlipped(MentionDirect), n)
	}
}

// ⌃T works from inside a DM, and the notice is why that is not a silent
// change either.
//
// The mode is the client's rather than the pane's: a DM is locked to one agent
// and has nothing to widen, so refusing the key there would mean an operator
// has to find their way back to the room to set a mode before typing into it.
func TestControlTFlipsTheModeFromInsideADM(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(200, 40)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlT})

	if a.mention != MentionOpen {
		t.Errorf("⌃T inside a DM left the mode at %s: the mode is this client's, not this pane's", a.mention)
	}
	if notice.Count(mentionFlipped(MentionOpen)) != 1 {
		t.Error("⌃T inside a DM changed the mode and said nothing, and there is no target line in a DM to say it")
	}
}

// --- what the composer promises -------------------------------------------

// The target line names the mode under both readings, and the cheap one is
// named too.
//
// Absence would be the memory problem §7 exists to close: if only `open`
// annotated the line, `→ @john` would mean direct by having nothing said about
// it, and reading the line correctly would require remembering that the build
// has a mode at all. That is the legend's own failure one surface over.
func TestTheComposerNamesTheMentionModeUnderBothReadings(t *testing.T) {
	room := func(mode MentionMode) App {
		a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
		a.mention = mode
		return a.withDraft("@john ship it")
	}

	if got := room(MentionDirect).room.Composer().Target(); got != "→ @john · direct" {
		t.Errorf("a mention under the cheap reading draws %q, want → @john · direct", got)
	}
	if got := room(MentionOpen).room.Composer().Target(); got != "→ @john · open · 3 turns" {
		t.Errorf("a mention under the expensive reading draws %q, want → @john · open · 3 turns. The mode "+
			"and the price are the two things worth knowing before ↵, and this is the one draft where "+
			"neither is written in what was typed", got)
	}
}

// The two routes that are not a mention are drawn without a mode, in either
// reading.
//
// A mode on `→ @manager` would be the composer claiming a choice was made
// where none was offered - the manager is not in the fleet, so there is
// nothing for open to widen to, and an operator reading `· direct` there would
// reasonably conclude ⌃T would change where it goes.
func TestTheComposerNamesNoModeOnARouteMentionModeDoesNotDecide(t *testing.T) {
	for _, mode := range []MentionMode{MentionDirect, MentionOpen} {
		for _, draft := range []string{"@" + core.ManagerName + " who is stuck?", "who is stuck?"} {
			a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", core.ManagerName)
			a.mention = mode
			got := a.withDraft(draft).room.Composer().Target()
			if got != "→ @"+core.ManagerName {
				t.Errorf("in %s mode the draft %q draws %q, want → @%s with no mode on it: mention mode "+
					"decides what a mention of a fleet agent means, and this is neither", mode, draft, got, core.ManagerName)
			}
		}
	}
}

// turnsClaim reads the number the target line promises, which is the only
// number on it.
var turnsClaim = regexp.MustCompile(`· ([0-9]+) turns`)

// The number on the composer is the number of turns ↵ actually starts, over
// every route and both readings.
//
// §7's entire safety argument for making this configurable is *"the composer
// always shows the current target and mode, so it is never a memory
// problem"*. A composer promising one turn over a send that starts thirty is
// that argument failing, in the expensive direction, on the one draft whose
// price is not written in what was typed - and it is invisible to any test
// that drives the composer or the send but not both against each other.
//
// A line with no number claims one turn, which is what the three unannotated
// routes are. Every draft here has somewhere to go, so a zero is a failure
// rather than a case: the refusal has its own test.
func TestWhatTheComposerPromisesIsWhatEnterSends(t *testing.T) {
	drafts := []string{
		"@john ship it",
		"@all ship it",
		"@" + core.ManagerName + " ship it",
		"@nobody ship it",
		"who is free",
	}
	for _, mode := range []MentionMode{MentionDirect, MentionOpen} {
		for _, draft := range drafts {
			a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john", core.ManagerName)
			a.mention = mode
			a = a.withDraft(draft)
			line := a.room.Composer().Target()

			b, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Errorf("in %s mode the draft %q went nowhere, and every draft here has a recipient", mode, draft)
				continue
			}
			want := 1
			if m := turnsClaim.FindStringSubmatch(line); m != nil {
				n, err := strconv.Atoi(m[1])
				if err != nil {
					t.Fatalf("the target line %q carries an unreadable count: %v", line, err)
				}
				want = n
			}
			if sent := len(sentFrames(t, b, cmd)); sent != want {
				t.Errorf("in %s mode the draft %q drew %q and sent %d frames, want %d. What is on screen "+
					"before ↵ is the whole of what makes a mode safe", mode, draft, line, sent, want)
			}
		}
	}
}

// --- and that both of them ask one question -------------------------------

const (
	// routerCall is the router every room draft is resolved by, and the one
	// thing on the far side of this package's boundary that decides where a
	// message goes.
	routerCall = "core.Resolve"

	// routeFunc is the one function in this package allowed to call it.
	routeFunc = "route"
)

// routeCallers are the two questions that must be one question: where the
// composer says ↵ will send, and where ↵ sends.
var routeCallers = []string{"sendRoom", "retarget"}

// Nothing resolves a room draft except the one function that does.
//
// # Why a fixture cannot close this
//
// TestWhatTheComposerPromisesIsWhatEnterSends drives both halves against each
// other over five drafts and two modes, and it dies to the mutant that widens
// or narrows *every* route. It cannot see one that narrows a **subset**:
//
//	if len(live) > 8 { return roomRoute{Route: core.Resolve(draft, live, svc)} }
//
// in retarget alone leaves the composer promising one turn on exactly the
// fleet sizes this product is for, and every fixture above uses three agents.
// That is the same shape as `awaitSpawn` substituting the parent's id for some
// subset of parents: a branch nobody's example reaches, in the direction that
// costs money. So the close is static - **one call to the router in the whole
// package, inside one function, and both askers call that function.**
//
// # The floors
//
// Three, each because its absence reads as the strongest possible pass: the
// router has to be called *somewhere* (a renamed function or a broken scan
// otherwise yields "no violation"), both callers have to be found by name, and
// the enclosing function of the one call has to be the one named here.
func TestNothingRoutesARoomDraftExceptTheOneFunctionThatDoes(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	resolvers := map[string]int{}
	callers := map[string]int{}
	found := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		for fn, body := range funcBodies(t, name) {
			found[fn] = true
			for _, call := range calledNames(body) {
				switch call {
				case routerCall:
					resolvers[fn]++
				case routeFunc:
					callers[fn]++
				}
			}
		}
	}

	if total := sum(resolvers); total != 1 {
		t.Fatalf("%s is called %d times in this package's non-test files (%v), want exactly once. Two "+
			"callers of the router are two answers to one question, and the one on screen and the one "+
			"on the wire are then free to disagree", routerCall, total, resolvers)
	}
	if n := resolvers[routeFunc]; n != 1 {
		t.Errorf("%s is not called inside %s: it is called from %v. The scan found a call somewhere else, "+
			"or this constant no longer names the function that routes", routerCall, routeFunc, resolvers)
	}
	for _, fn := range routeCallers {
		if !found[fn] {
			t.Fatalf("this package has no function %q: the scan is looking at the wrong thing and every "+
				"check below it asserts nothing", fn)
		}
		if callers[fn] == 0 {
			t.Errorf("%s does not call %s. It is one of the two things that has to answer \"where does "+
				"this draft go\", and the two answers have to be one computation", fn, routeFunc)
		}
	}
}

// funcBodies is every function and method declared in one file, keyed by name.
func funcBodies(t *testing.T, file string) map[string]*ast.BlockStmt {
	t.Helper()
	out := map[string]*ast.BlockStmt{}
	for _, decl := range parseGoFile(t, file).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		out[fn.Name.Name] = fn.Body
	}
	return out
}

// calledNames is every function called in a body, spelled `f` or `pkg.f` - a
// selector's own tail, so `a.route(…)` and `x.route(…)` are the same name.
// That is deliberate: what is being asserted is that the routing decision is
// made in one place, and a second receiver reaching the same method is not a
// second decision.
func calledNames(body *ast.BlockStmt) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			out = append(out, fn.Name)
		case *ast.SelectorExpr:
			out = append(out, fn.Sel.Name)
			if pkg, ok := fn.X.(*ast.Ident); ok {
				out = append(out, pkg.Name+"."+fn.Sel.Name)
			}
		}
		return true
	})
	return out
}

func sum(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}

// frameIDs is the sessions a batch of frames was addressed to, sorted so two
// fan-outs can be compared without depending on the order either produced.
func frameIDs(frames []rpc.Frame) []string {
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		out = append(out, f.SessionID)
	}
	sort.Strings(out)
	return out
}
