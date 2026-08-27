package main

// The seam `internal/ui` is handed so a room can reach this machine.

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/ui"
)

// The picker `/adopt` shows and the picker `wake import` prints are one
// function, and this is where that is asserted rather than intended.
//
// Two pickers would be two answers to "what is on this machine", drifting from
// the day one of them grew a column - and the one that matters here is the row
// with **no provable directory**, which is 97 of 428 on the recording machine
// and is listed with its reason in the directory's place. A second renderer
// that hid it would lose a conversation that exists.
func TestTheRoomsPickerIsTheOneTheShellPrints(t *testing.T) {
	projects := projectsTree(t)
	real := t.TempDir()
	transcript(t, projects, real, importA, real)
	// A session whose directory cannot be proven: the transcript names a
	// worktree while the file sits under the directory it was started in.
	started := t.TempDir()
	worktree := filepath.Join(started, "wt")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	transcript(t, projects, started, importB, worktree)

	listing, err := theMachine.Listing()
	if err != nil {
		t.Fatalf("Listing: %v", err)
	}
	for _, want := range []string{shortID(importA), real, shortID(importB), "no directory can be proven"} {
		if !strings.Contains(listing, want) {
			t.Errorf("the room's picker does not carry %q:\n%s", want, listing)
		}
	}
	if !strings.Contains(listing, "close it there first") {
		t.Errorf("the room's picker drops the one warning Wake can give about a session that is still "+
			"open in a terminal:\n%s", listing)
	}
}

// The room's picker names the room's command, and the shell's names the
// shell's.
//
// A listing that told an operator in a room to run a shell verb would be the
// thing this whole command exists to stop them doing. The sentence comes from
// `internal/ui`, because that is the package that owns the word and the only
// one whose prose is walked by
// TestEverySlashCommandAnySentenceNamesIsOneThisPackageAnswers.
func TestEachPickerNamesTheCommandItsOwnSurfaceAnswers(t *testing.T) {
	projects := projectsTree(t)
	real := t.TempDir()
	transcript(t, projects, real, importA, real)

	room, err := theMachine.Listing()
	if err != nil {
		t.Fatalf("Listing: %v", err)
	}
	if !strings.Contains(room, ui.AdoptUsage) {
		t.Errorf("the room's picker does not say how to adopt from a room (%q):\n%s", ui.AdoptUsage, room)
	}
	if strings.Contains(room, importUsage) {
		t.Errorf("the room's picker tells an operator to run a shell verb, which is the trip out of "+
			"Wake this command exists to remove:\n%s", room)
	}

	shell := formatImportable([]daemon.FoundSession{{ID: importA}}, importUsage, 0)
	if !strings.Contains(shell, importUsage) {
		t.Errorf("the shell picker no longer says how to import one:\n%s", shell)
	}
}

// The room's picker is bounded and says what it left out.
//
// The shell prints all 428 rows into a scrolling terminal, which is right
// there and wrong in a pane: a transcript is where the operator's conversation
// lives, and 428 sessions is two lines each on top of it. So the room takes the
// newest few - recency is the only thing that orders them - and names the total
// and the shell verb that shows the rest, because a cap that hid its own
// existence would be a picker that quietly stopped listing somebody's session.
func TestTheRoomsPickerIsCappedAndSaysWhatItLeftOut(t *testing.T) {
	found := make([]daemon.FoundSession, 0, adoptRows+3)
	for i := range adoptRows + 3 {
		found = append(found, daemon.FoundSession{
			ID:  strings.Repeat(string(rune('a'+i%26)), 8) + "-cafe",
			Dir: "/tmp/p" + string(rune('a'+i%26)),
		})
	}

	got := formatImportable(found, ui.AdoptUsage, adoptRows)

	if strings.Contains(got, found[adoptRows].ID[:8]) {
		t.Errorf("row %d was drawn past the cap of %d:\n%s", adoptRows, adoptRows, got)
	}
	if !strings.Contains(got, found[0].ID[:8]) {
		t.Errorf("the newest row is not in the capped listing:\n%s", got)
	}
	if !strings.Contains(got, "3 more") {
		t.Errorf("the capped listing does not say how many it left out, so a session that is on this "+
			"machine can be missing with nothing saying so:\n%s", got)
	}
	if !strings.Contains(got, "`wake import`") {
		t.Errorf("the capped listing does not name the verb that shows all of them:\n%s", got)
	}
}

// An uncapped listing says nothing about a cap.
func TestTheShellPickerIsNotCapped(t *testing.T) {
	found := make([]daemon.FoundSession, 0, adoptRows+3)
	for i := range adoptRows + 3 {
		found = append(found, daemon.FoundSession{ID: strings.Repeat(string(rune('a'+i%26)), 8) + "-cafe"})
	}

	got := formatImportable(found, importUsage, 0)

	if strings.Contains(got, "more") {
		t.Errorf("the shell listing claims to have left something out:\n%s", got)
	}
	if !strings.Contains(got, found[len(found)-1].ID[:8]) {
		t.Errorf("the shell listing dropped its last row:\n%s", got)
	}
}

// Resolve is importTarget's answers, plural - one walk of the disk for the
// whole set, and the first thing it cannot resolve refuses all of them.
//
// One walk because `/adopt a b c` on the recording machine is otherwise three
// walks of 428 transcripts for one keystroke.
func TestResolveAnswersTheWholeSetOrNamesTheOneItCouldNot(t *testing.T) {
	projects := projectsTree(t)
	real := t.TempDir()
	transcript(t, projects, real, importA, real)
	transcript(t, projects, real, importB, real)

	ids, err := theMachine.Resolve([]string{importA[:8], importB[:8]})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(ids) != 2 || ids[0] != importA || ids[1] != importB {
		t.Errorf("Resolve answered %v, want the two whole ids in the order they were typed", ids)
	}

	if _, err := theMachine.Resolve([]string{importA[:8], "zzzzzzzz"}); err == nil {
		t.Error("a name that resolves to nothing was accepted, so the daemon would be asked for a " +
			"session that is not on this machine and the refusal would arrive one round trip later")
	} else if !strings.Contains(err.Error(), "zzzzzzzz") {
		t.Errorf("the refusal does not name what could not be resolved: %v", err)
	}
}

// Resolve refuses an ambiguous prefix rather than picking, and says which ones
// it matched - importTarget's ruling, reached through the room.
func TestResolveRefusesAnAmbiguousPrefixWithTheCandidates(t *testing.T) {
	projects := projectsTree(t)
	real := t.TempDir()
	transcript(t, projects, real, ambiguousA, real)
	transcript(t, projects, real, ambiguousB, real)

	_, err := theMachine.Resolve([]string{ambiguousA[:4]})
	if err == nil {
		t.Fatal("an ambiguous prefix resolved to one session: the room would open a conversation with " +
			"whichever transcript sorted first")
	}
	if !strings.Contains(err.Error(), "use more of the id") {
		t.Errorf("the refusal does not say how to disambiguate: %v", err)
	}
}

// ambiguousA and ambiguousB share the first four characters of their ids, which
// is what makes a prefix ambiguous rather than short.
const (
	ambiguousA = "beef1111-1111-4111-8111-111111111111"
	ambiguousB = "beef2222-2222-4222-8222-222222222222"
)

// theMachine is the seam under test. A value rather than a literal at each call
// site because a composite literal cannot open an `if` statement.
var theMachine machineSessions

// The seam this package hands over is the one internal/ui declares.
//
// A compile-time assertion, and it is the whole reason the interface is worth
// having: the room may not import internal/daemon, so nothing but the type
// checker can say these two halves still fit.
var _ ui.Sessions = machineSessions{}

// Every model this package builds is given a way to see this machine.
//
// # Why nothing behavioural can see this
//
// A model built without the seam is a *legitimate shape* - it is what every
// unit test in `internal/ui` is, and it answers `/adopt` with "wake cannot read
// the claude sessions on this machine from here". So dropping `.WithSessions`
// from a constructor here does not crash, does not fail a type check and does
// not fail any test in either package: it silently turns the room's picker into
// a refusal, on a build where `wake import` still works perfectly at a shell.
// That is the same gap `TestTheRestoreOfferIsMadeByTheOnlyCommandThatIsAboutTheFleet`
// exists for - the success path runs into an alt screen that takes stdin.
//
// The domain is **derived from the producer**: every call to `ui.NewRoomApp` in
// this package's own non-test files, so a third constructor in a new file is a
// build failure until somebody wires it, rather than inheriting the gap
// quietly. Two floors, because a scan that matched nothing would report the
// strongest possible pass for the weakest possible reason.
func TestEveryRoomThisPackageBuildsCanSeeThisMachine(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	builders, scanned := 0, 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		for _, decl := range parseFile(t, name).Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !callsFunctionNamed(fn, roomAppCall) {
				continue
			}
			builders++
			if !callsFunctionNamed(fn, sessionsCall) && !hasSelectorCall(fn, sessionsMethod) {
				t.Errorf("%s builds a room and never calls %s. A model with no way to look answers "+
					"`/adopt` with a refusal - it does not fail, it does not crash, and no test in "+
					"either package can tell the difference, so the room quietly loses the picker "+
					"while `wake import` goes on working at a shell", fn.Name.Name, sessionsMethod)
			}
		}
	}
	if scanned < 10 {
		t.Fatalf("only %d non-test files were scanned: cmd/wake is larger than that, so the glob is "+
			"looking at the wrong directory and this check is passing over nothing", scanned)
	}
	if builders == 0 {
		t.Fatalf("no function in this package calls %s: it was renamed, and this guard now approves "+
			"every room that cannot see this machine", roomAppCall)
	}
}

const (
	// roomAppCall is the constructor every room-bearing model comes out of.
	roomAppCall = "ui.NewRoomApp"

	// sessionsCall and sessionsMethod are the wiring, spelled both ways it can
	// appear: chained off the constructor, or called on a named model.
	sessionsCall   = "ui.App.WithSessions"
	sessionsMethod = "WithSessions"
)

// hasSelectorCall reports whether a function calls a method of that name on
// anything at all - which is how `.WithSessions(…)` reads in a builder chain,
// where the receiver is an expression rather than a name.
func hasSelectorCall(fn *ast.FuncDecl, method string) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == method {
			found = true
		}
		return !found
	})
	return found
}
