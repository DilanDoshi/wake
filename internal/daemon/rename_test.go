package daemon

// Renaming a session, and saying what it is working on.
//
// Two claims carry the feature and both are asserted rather than argued: the
// registry ends up holding exactly one of the two names, whichever way the
// rename went; and the set of sessions a rename may touch is a verdict per
// state derived from the producer rather than two `if`s somebody wrote.

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// **The ruling: a rename changes the handle, so the old name goes back to the
// pool in the same locked step the new one is taken.**
//
// Both halves matter and each kills a different half-implementation. A rename
// that took the new name and kept the old one holds two names for one session,
// so a fleet of thirty runs the pool down at twice the rate and `@old` reaches
// an agent the roster does not advertise. A rename that released the old and
// failed to take the new leaves the session answering to a name anything else
// can claim - which is the collision `no two live sessions share a name` is the
// whole point of.
func TestARenameReleasesTheOldNameAndTakesTheNewInOneStep(t *testing.T) {
	r := newNameRegistry()
	if _, err := r.claim("alex"); err != nil {
		t.Fatalf("claim alex: %v", err)
	}

	to, err := r.rename("alex", "bob")
	if err != nil {
		t.Fatalf("rename alex to bob: %v", err)
	}
	if to != "bob" {
		t.Fatalf("the rename answered %q", to)
	}

	// Released: the next session may be called alex, which is what "a name is
	// released and reissued" already means when a session ends.
	if _, err := r.claim("alex"); err != nil {
		t.Errorf("alex was not released by the rename (%v), so one session holds two names and the "+
			"old handle reaches an agent the roster no longer advertises", err)
	}
	// Taken: nothing else may be called bob.
	if _, err := r.claim("bob"); err == nil {
		t.Error("bob was not taken by the rename, so two live sessions can hold it - which is the one " +
			"guarantee core.Resolve's exact match rests on")
	}
}

// A name another live session holds is refused, and the refusal costs nothing:
// the session keeps the name it had.
//
// The second half is the one a partial implementation loses. A rename that
// released first and checked afterwards leaves the loser holding no name at
// all, and nothing in this build can report a session that has one fewer name
// than it started with.
func TestARefusedRenameLeavesBothSessionsNamedAsTheyWere(t *testing.T) {
	for _, requested := range []string{
		"sydney",   // held by another live session
		"all",      // the router spends it on a broadcast
		"manager",  // the router spends it on the service
		"beefcafe", // entirely hex, so it could shadow a printed short id
		"Not A Name",
		strings.Repeat("x", maxNameLen+1),
		"",
	} {
		t.Run(requested, func(t *testing.T) {
			r := newNameRegistry()
			for _, held := range []string{"alex", "sydney"} {
				if _, err := r.claim(held); err != nil {
					t.Fatalf("claim %s: %v", held, err)
				}
			}

			if _, err := r.rename("alex", requested); err == nil {
				t.Fatalf("renaming alex to %q was allowed", requested)
			}
			if _, err := r.claim("alex"); err == nil {
				t.Error("the refused rename released alex anyway, so the session that kept its name is " +
					"one anything else can now be called")
			}
			if _, err := r.claim("sydney"); err == nil {
				t.Error("the refused rename released sydney, which it never held")
			}
		})
	}
}

// Renaming a session to the name it already has is a no-op rather than a
// collision with itself. The arm exists because the obvious order - check
// whether the target is held, then swap - reports "a live session is already
// called alex" about the session doing the asking.
func TestRenamingToTheNameItAlreadyHasIsAllowed(t *testing.T) {
	r := newNameRegistry()
	if _, err := r.claim("alex"); err != nil {
		t.Fatalf("claim alex: %v", err)
	}
	if to, err := r.rename("alex", "ALEX"); err != nil || to != "alex" {
		t.Errorf("renaming alex to itself answered (%q, %v), want (alex, nil): normalizeName folds case, "+
			"so this is the same name typed differently", to, err)
	}
	if _, err := r.claim("alex"); err == nil {
		t.Error("the no-op rename released the name")
	}
}

// renameableStates is asserted over the states a running daemon can report, and
// the domain is read out of the producer.
//
// docs/notes/decisions.md rung 4: `agent.stateLocked` decides what can arrive,
// and `rpc`'s constant block is wider than that - it declares `StateOrphaned`,
// which only `FleetOnDisk` writes and which no frame reaching `dispatch` can
// name. So a seventh state stateLocked learns to return is a build failure
// until somebody says what a rename does with it, and a cell for a state it
// cannot return is a verdict over an impossible input that reads as coverage.
func TestEveryStateAgentStateLockedCanReportHasARenameVerdict(t *testing.T) {
	reported := statesStateLockedReports(t)
	for state := range reported {
		if _, decided := renameableStates[state]; !decided {
			t.Errorf("agent.stateLocked can report %q and renameableStates says nothing about it. A "+
				"rename is display, so the tempting answer is `always allowed` - and it is wrong for a "+
				"parked session, whose name is on disk in a record only the park writes", state)
		}
	}
	for state := range renameableStates {
		if !reported[state] {
			t.Errorf("renameableStates rules on %q and agent.stateLocked never returns it: a verdict over "+
				"an input no producer can make reads as coverage", state)
		}
	}
}

// And the verdict is the behaviour, driven through the transitions the daemon
// itself calls rather than through the flags they set.
//
// The two refusals are the whole of what this feature declines to do, and each
// is refused for its own reason: a parked session's name lives in `parked.json`
// and a rename that did not rewrite it would come back under the old name after
// a restart, and an ended session has already given its name back to the pool.
func TestRenamingIsRefusedForAParkedOrEndedSessionAndSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		set     func(*agent)
		state   string
		refused bool
	}{
		{name: "idle", set: func(*agent) {}, state: rpc.StateIdle},
		{name: "working", set: func(a *agent) { a.noteSent() }, state: rpc.StateWorking},
		{name: "parked", set: func(a *agent) { a.beginPark(); a.finish(nil); a.markParked() }, state: rpc.StateParked, refused: true},
		{name: "ended", set: func(a *agent) { a.finish(nil) }, state: rpc.StateEnded, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newNameRegistry()
			if _, err := r.claim("alex"); err != nil {
				t.Fatalf("claim alex: %v", err)
			}
			a := newAgent(idAlpha, "alex", "dev", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
			tc.set(a)

			// Rung 6: the situation is read back through a different question
			// before the behaviour, because a transition that stopped producing
			// the state would leave every cell asserting about `idle`.
			a.mu.Lock()
			got := a.stateLocked(time.Now())
			a.mu.Unlock()
			if got != tc.state {
				t.Fatalf("the fixture is %q rather than %q, so this cell is about a state nobody built", got, tc.state)
			}

			err := a.rename(r, "bob")
			switch {
			case tc.refused && err == nil:
				t.Fatalf("a %s session was renamed", tc.name)
			case tc.refused:
				if want := renameableStates[tc.state]; !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal is %q and renameableStates says %q", err, want)
				}
				if _, cerr := r.claim("bob"); cerr != nil {
					t.Error("the refused rename took the new name anyway")
				}
			case err != nil:
				t.Fatalf("a %s session could not be renamed: %v", tc.name, err)
			}

			// The label half takes the same verdict from the same table, which
			// is what stops the two drifting into different answers about one
			// state.
			if lerr := a.relabel("ui fixes"); (lerr != nil) != tc.refused {
				t.Errorf("relabel answered %v on a %s session while rename answered %v", lerr, tc.name, err)
			}
		})
	}
}

// The manager's name is not display, so it is the one session that cannot be
// renamed at all.
//
// Everything that makes a manager a manager is keyed on the name: `launch`
// applies the MCP configuration and the scoping prompt by it, `internal/mcp`
// keeps it off its own roster by it, and the room finds its default addressee
// by it. A manager under a pooled name holds tools that act on the whole fleet,
// answers to nothing, and appears in `list_agents` as an ordinary agent.
func TestTheManagerCannotBeRenamed(t *testing.T) {
	r := newNameRegistry()
	if _, err := r.claimManager(); err != nil {
		t.Fatalf("claim the manager name: %v", err)
	}
	a := newAgent(idAlpha, core.ManagerName, "dev", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	if err := a.rename(r, "bob"); err == nil {
		t.Fatal("the manager was renamed, so @manager now reaches nothing and the session holding Wake's " +
			"own tools looks like an ordinary agent")
	}
	if a.name != core.ManagerName {
		t.Errorf("the manager is called %q", a.name)
	}
	// The other direction is normalizeName's and is checked with the rest of the
	// reserved words above; asserted here too because "nothing may be renamed to
	// manager" and "the manager may not be renamed" are two different holes.
	if _, err := r.rename("alex", core.ManagerName); err == nil {
		t.Error("an ordinary agent was renamed to the manager's name")
	}
}

// A label somebody types is held to exactly what a derived one is held to,
// because they land in the same column beside the same thirty rows.
func TestATypedLabelIsHeldToTheBoundsADerivedOneIs(t *testing.T) {
	long := strings.Repeat("z", maxLabelLen+10)
	for _, tc := range []struct{ name, in, want string }{
		{name: "prose keeps its spaces", in: "ui fixes", want: "ui fixes"},
		{name: "control characters go", in: "ui\x1b[31m fixes", want: "ui[31m fixes"},
		{name: "a newline cannot make a second row", in: "ui\nfixes", want: "uifixes"},
		{name: "surrounding space goes", in: "   ui fixes  ", want: "ui fixes"},
		{name: "too long is cut and says so", in: long, want: string([]rune(long)[:maxLabelLen-1]) + truncationMark},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeLabel(tc.in)
			if err != nil {
				t.Fatalf("normalizeLabel(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("normalizeLabel(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if len([]rune(got)) > maxLabelLen {
				t.Errorf("%q is %d runes against a column of %d", got, len([]rune(got)), maxLabelLen)
			}
		})
	}
	for _, empty := range []string{"", "   ", "\x00\x01"} {
		if got, err := normalizeLabel(empty); err == nil {
			t.Errorf("normalizeLabel(%q) = %q with no error: an empty label is refused rather than read as "+
				"`put it back to the branch`, which is a verb nothing has asked for and is what a dropped "+
				"argument looks like", empty, got)
		}
	}
}

// statesStateLockedReports is what agent.stateLocked can return - the producer,
// which is the authority on what can arrive rather than on what rpc's constant
// block permits.
//
// A method rather than a function, so forkgate_test.go's funcDecl (which
// requires a nil receiver) cannot be reused; the walk is otherwise the one
// internal/ui's forkguard_test.go does from the other side of the package
// boundary.
func statesStateLockedReports(t *testing.T) map[string]bool {
	t.Helper()

	byName := sessionStateConstants(t)
	var body *ast.BlockStmt
	for _, decl := range parseFile(t, "agent.go").Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv != nil && fn.Name.Name == "stateLocked" && fn.Body != nil {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatal("no method stateLocked in agent.go: it was renamed or moved, and the domain this scan " +
			"derives is empty, which reads as `every state is ruled on`")
	}

	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "rpc" {
			return true
		}
		if value, declared := byName[sel.Sel.Name]; declared {
			out[value] = true
		}
		return true
	})
	if len(out) == 0 {
		t.Fatal("agent.stateLocked names no rpc.State… constant: the scan is broken and every reachability " +
			"claim resting on it is asserting nothing")
	}
	return out
}

// End to end over a real daemon: the display change reaches the whole fleet
// without anybody asking, and the on-disk roster follows.
//
// The roster half is the one nothing else reaches. `wake status` on a machine
// whose daemon died reads that file, so a rename the roster did not follow
// would print the name a session was born with — which is worse than no name,
// because it is a handle somebody would type. It is also the read that stopped
// being safe when the display halves stopped being written once at launch, so
// this runs under `-race` with a fan-out goroutine live behind the agent.
//
// **One frame per subtest, and that is rung 6's second shape rather than
// tidiness.** The first draft sent the rename and the label together and read
// the roster once. Deleting `published` from `renameSession` left it green:
// `relabelSession` published a moment later and carried both changes with it,
// so the artefact was right and the path under test had not run. Two writers of
// one artefact make the one under test invisible — ask what would still be true
// with this path deleted, and if the answer is "everything", the test is about
// the artefact. Driven alone, the mutant fails.
func TestADisplayChangeReachesTheFleetReportAndTheOnDiskRoster(t *testing.T) {
	for _, tc := range []struct {
		name, kind, text string
		want             func(rpc.SessionStatus) bool
		recorded         func(record) bool
	}{
		{
			name: "rename", kind: rpc.FrameRename, text: "bob",
			want:     func(s rpc.SessionStatus) bool { return s.Name == "bob" },
			recorded: func(r record) bool { return r.Name == "bob" },
		},
		{
			name: "label", kind: rpc.FrameLabel, text: "ui fixes",
			want:     func(s rpc.SessionStatus) bool { return s.Label == "ui fixes" },
			recorded: func(r record) bool { return r.Label == "ui fixes" },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeClaudeOnPath(t, "")
			d := startDaemon(t)
			c := attach(t, d.socket)
			c.spawn(idAlpha, "alex")
			c.awaitState(idAlpha, rpc.StateIdle)

			// Read back before the behaviour: a fixture already carrying the
			// value would satisfy everything below with the verb deleted.
			recs := loadRoster(rosterPath(d.socket))
			if len(recs) != 1 || tc.recorded(recs[0]) {
				t.Fatalf("the roster holds %+v before the %s, so this cannot show a change it never saw the absence of", recs, tc.name)
			}

			c.send(rpc.Frame{Kind: tc.kind, SessionID: idAlpha, Text: tc.text})
			c.await("the changed session in a status frame", func(f rpc.Frame) bool {
				if f.Status == nil {
					return false
				}
				for _, s := range f.Status.Sessions {
					if s.ID == idAlpha && tc.want(s) {
						return true
					}
				}
				return false
			})

			if recs := loadRoster(rosterPath(d.socket)); len(recs) != 1 || !tc.recorded(recs[0]) {
				t.Errorf("the roster holds %+v after the %s: `wake status` on a machine whose daemon died "+
					"reads that file, so it would print what the session was born with", recs, tc.name)
			}
		})
	}
}

// And the name it was born with is free again, which is the ruling read from
// the outside: a second session may be called alex the moment the first stops
// being.
//
// Through a real registry and a real spawn rather than through nameRegistry
// directly, because what is being asserted is that the *daemon's* registry saw
// the rename - the unit test above proves the registry can do it, and this is
// the wire reaching it.
func TestTheNameARenamedSessionGaveUpIsFreeForTheNextSpawn(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FrameRename, SessionID: idAlpha, Text: "bob"})
	c.await("the renamed session", func(f rpc.Frame) bool {
		if f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == idAlpha && s.Name == "bob" {
				return true
			}
		}
		return false
	})

	// spawn fails the test if the daemon refuses, which is what a registry that
	// kept the old name would do.
	c.spawn(idBeta, "alex")
}

// unlockedReadsOfTheDisplayHalves is every function that reads an agent's `name`
// or `label` **outside** that agent's own lock, with what makes it sound.
//
// # Why an allowlist and not a rule
//
// `agent`'s struct declares `id`, `name`, `label`, `dir` and `parent` above `mu`
// and everything else below it, and that layout is the contract every unlocked
// reader in this package was written against: those five were written once, in
// newAgent, and never again. **Two of them stopped being written once the day
// `/name` and `/task` shipped**, and the readers did not move.
//
// One of them was a real data race - `managerAgent` read `a.name` under `s.mu`,
// which orders nothing against a write under `a.mu` - and `-race` found it only
// because a reviewer went looking. The rest are sound, and each is sound for a
// reason that is invisible from the read itself: an `a.mu` acquisition earlier
// on the same goroutine that the write is ordered behind. A reason that cannot
// be seen from the code is one the next reader inherits without knowing it, so
// it is written down here and a new reader is a build failure until somebody
// says which of these it is.
//
// This is `slashIsAPathSeparatorIn`'s shape and it is checked the same way, in
// both directions: an entry that stops reading either field is deleted in the
// change that made it untrue, rather than left to cover whatever is written
// there next.
var unlockedReadsOfTheDisplayHalves = map[string]string{
	"unpark":   "reads a parked agent, and a parked one cannot be renamed. isParked() took a.mu on this goroutine immediately above, which is what orders this read behind markParked's write",
	"labelFor": "called from launch on a parked agent being woken, behind unpark's own isParked(); same ordering, same refusal",
	"retire":   "finish() took a.mu on this goroutine two statements above, and a session that has ended cannot be renamed",
}

// Three, and newAgent is deliberately not among them: it writes the two fields
// as composite-literal keys rather than reading them off a receiver, so the
// scan does not see it and the excuse would have been a dead entry covering
// whatever was written next. The guard said so on its first run.
//
// It was five. completePark and bookParked both stopped reading either field
// when the park book row moved into recordFor, which takes the lock - so their
// excuses went with them, which is what the second half of this test is for.
const unlockedDisplayReaderCount = 3

// The display halves are read under the agent's lock, or by a function that has
// said why it need not.
func TestEveryUnlockedReadOfTheDisplayHalvesHasAVerdict(t *testing.T) {
	if len(unlockedReadsOfTheDisplayHalves) != unlockedDisplayReaderCount {
		t.Fatalf("%d functions are excused from locking and the count says %d: an excuse added without "+
			"being counted is how a data race ships",
			len(unlockedReadsOfTheDisplayHalves), unlockedDisplayReaderCount)
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	found, scanned := map[string]bool{}, 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		scanned++
		for _, decl := range parseFile(t, file).Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if !readsTheDisplayHalves(fn) {
				continue
			}
			if takesTheAgentLock(fn) {
				continue
			}
			found[fn.Name.Name] = true
			if _, excused := unlockedReadsOfTheDisplayHalves[fn.Name.Name]; !excused {
				t.Errorf("%s in %s reads an agent's name or label without taking that agent's lock, and "+
					"nothing here says why that is safe. Both fields are written by rename and relabel "+
					"under a.mu, so an unlocked read is a data race unless something else on this "+
					"goroutine orders it - say which, or take the lock", fn.Name.Name, file)
			}
		}
	}
	if scanned < 10 {
		t.Fatalf("only %d non-test files were scanned: internal/daemon is larger than that, so this check "+
			"is passing over nothing", scanned)
	}
	for name, why := range unlockedReadsOfTheDisplayHalves {
		if !found[name] {
			t.Errorf("%s is excused from locking because %s, and it no longer reads either field without "+
				"the lock: delete the excuse in the change that made it untrue", name, why)
		}
	}
}

// readsTheDisplayHalves reports whether a function mentions `.name` or `.label`
// as a selector - the two fields a rename writes.
func readsTheDisplayHalves(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "name" || sel.Sel.Name == "label" {
			// `s.name` on something that is not an agent would be a false
			// positive; this package has no such field, and the floor on the
			// scan below is what would notice if that stopped being true.
			found = true
		}
		return true
	})
	return found
}

// takesTheAgentLock reports whether a function locks an agent's mutex itself.
//
// Deliberately syntactic and deliberately generous: it answers "did this
// function take *an* agent lock", not "is every read inside a critical
// section". A function that takes the lock and then reads outside it is not
// caught here, and that boundary is stated rather than left to be discovered -
// the value of this guard is the *list*, which is where the reasoning lives.
func takesTheAgentLock(fn *ast.FuncDecl) bool {
	locked := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Lock" {
			return true
		}
		if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "mu" {
			if x, ok := inner.X.(*ast.Ident); ok && x.Name != "s" && x.Name != "r" && x.Name != "p" {
				locked = true
			}
		}
		return true
	})
	return locked
}

// And the race itself, driven rather than argued.
//
// The static guard above is the durable half - it fails in 0.24s and does not
// depend on a scheduler - but a guard over *spelling* cannot prove the spelling
// it approves is sound. This runs the two goroutines the failure needs: one
// window asking `wake manager` against a socket that already has one, which
// reaches managerRefusal → managerAgent, while another window's `/name` frame
// is dispatched. Under `-race` the previous version reports a write/read pair
// on `agent.name`; under both lanes this is a fast no-op.
func TestTheManagerLookupDoesNotRaceARename(t *testing.T) {
	s := newServer(tempSocket(t))
	names := []string{"alex", "sydney", core.ManagerName}
	agents := make([]*agent, 0, len(names))
	for i, name := range names {
		if _, err := s.names.claim(name); err != nil && name != core.ManagerName {
			t.Fatalf("claim %s: %v", name, err)
		}
		id := testSessionID(fmt.Sprintf("c%03d", i))
		a := newAgent(id, name, "dev", "/repo/api", "", core.NewSession(core.Config{SessionID: id}), func() {})
		s.agents[id] = a
		agents = append(agents, a)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 200 {
			_ = s.managerRefusal()
		}
	}()
	go func() {
		defer wg.Done()
		// alex, back and forth, so every iteration is a real write rather than
		// the same value stored twice - a no-op write is still a write to the
		// detector, but a test that could not tell them apart would pass with
		// the assignment deleted.
		for i := range 200 {
			to := "alex"
			if i%2 == 0 {
				to = "bob"
			}
			if err := agents[0].rename(s.names, to); err != nil {
				t.Errorf("rename to %s: %v", to, err)
				return
			}
		}
	}()
	wg.Wait()
}
