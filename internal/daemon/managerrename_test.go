package daemon

// The manager's end part: claude knows it as manager-<part>, and the operator
// chooses the part (`@manager /rename ops`) while Wake's own name for it stays
// `manager`, which is what its tools and the room's default addressee key on.

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// aManager is an unstarted manager as launch leaves one: Wake's name, and the
// manager-<fleet> claude was started under.
func aManager(t *testing.T) (*agent, *nameRegistry) {
	t.Helper()
	r := newNameRegistry()
	if _, err := r.claimManager(); err != nil {
		t.Fatalf("claim the manager name: %v", err)
	}
	a := newAgent(idAlpha, core.ManagerName, "dev", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	a.claudeName, a.claudeWant = "manager-harbor", "manager-harbor"
	return a, r
}

// What an operator types becomes manager-<part>: the part alone, or the whole
// name pasted back from where it is drawn, folded like any name.
func TestTheManagersEndPartIsReadFromWhatTheOperatorTypes(t *testing.T) {
	longest := strings.Repeat("a", maxFleetName)
	for _, tc := range []struct{ in, want string }{
		{"ops", "manager-ops"},
		{"  Ops ", "manager-ops"},
		{"manager-ops", "manager-ops"},
		{"MANAGER-Ops", "manager-ops"},
		{"2", "manager-2"},
		{"a_b-c", "manager-a_b-c"},
		{longest, "manager-" + longest},
	} {
		got, err := managerClaudeNameOf(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%q read as %q (%v), want %q", tc.in, got, err, tc.want)
		}
	}
	// A session id is a name's characters and the reaper's proof of identity, so
	// the bound is what keeps one out of an argv: it is 36 and a part is at most 32.
	const sessionID = "a11a0000-0000-4000-8000-00000000a11a"
	for _, in := range []string{"", "  ", "manager", "manager-", "two words", "ops!", "@ops", "é", longest + "a", sessionID} {
		if got, err := managerClaudeNameOf(in); err == nil {
			t.Errorf("%q was read as %q, want a refusal", in, got)
		}
	}
}

// The rename moves what claude is called and nothing Wake keys on: the manager
// is still `manager` to the registry, and the one /rename claude is owed names
// the whole manager-<part>.
func TestRenamingTheManagerMovesClaudesNameAndNotWakes(t *testing.T) {
	a, r := aManager(t)
	mustRename(t, a, r, "ops")

	if a.name != core.ManagerName {
		t.Errorf("the manager is called %q, so its tools and @manager now key on nothing", a.name)
	}
	if got := a.wantedClaude(); got != "manager-ops" {
		t.Errorf("claude is to be called %q, want manager-ops", got)
	}
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename manager-ops"}) {
		t.Errorf("a rename of an idle manager queued %q, want one [/rename manager-ops]", got)
	}
	if _, held := r.taken["ops"]; held || len(r.taken) != 1 {
		t.Errorf("the registry holds %v, want only the manager's own name", r.taken)
	}
	// Nothing may take the new name from the other side either.
	if _, err := r.rename("alex", "manager-ops"); err == nil {
		t.Error("an ordinary agent was renamed to the manager's new name")
	}
}

// A refusal changes nothing and arms nothing, as for any other rename.
func TestARefusedManagerRenameChangesNothing(t *testing.T) {
	a, r := aManager(t)
	for _, in := range []string{"two words", "manager-", ""} {
		if err := a.rename(r, in, false); err == nil {
			t.Fatalf("a manager rename to %q was accepted", in)
		}
	}
	if got := a.wantedClaude(); got != "manager-harbor" {
		t.Errorf("a refused rename left claude to be called %q", got)
	}
	if got := queuedRenames(a); len(got) != 0 {
		t.Errorf("a refused rename queued %q", got)
	}
}

// The operator's own `/rename ops` renames claude to `ops` before Wake can
// object; the mirror owes it the whole name, once, after claude's reply.
func TestTheOperatorsRenameOfTheManagerEndsAsItsEndPart(t *testing.T) {
	a, r := aManager(t)
	mustMirror(t, a, r, "ops")
	a.noteSent()
	a.noteRenameSent("/rename ops")
	streamed(a, renamedEvent("ops"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: ops", LocalCommand: true})
	a.probeIfWanted()
	a.probeIfWanted()
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename manager-ops"}) {
		t.Fatalf("claude took ops and Wake queued %q, want one [/rename manager-ops]", got)
	}

	// Pasted back whole, the same words are claude's own name already.
	b, rb := aManager(t)
	mustMirror(t, b, rb, "manager-ops")
	b.noteSent()
	b.noteRenameSent("/rename manager-ops")
	streamed(b, renamedEvent("manager-ops"))
	streamed(b, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: manager-ops", LocalCommand: true})
	b.probeIfWanted()
	if got := queuedRenames(b); len(got) != 0 {
		t.Fatalf("claude took manager-ops itself and Wake queued %q", got)
	}
}

// Claude taking exactly the name Wake wants - the whole manager-<part> pasted
// back - settles the want in the reply that releases it. Wake's own name is not
// the comparison: it is never what claude was asked for.
func TestAHeldWantOfTheManagerIsSettledWhenClaudeTookTheWholeName(t *testing.T) {
	a, r := aManager(t)
	mustMirror(t, a, r, "manager-ops")
	a.noteRenameSent("/rename manager-ops")
	a.noteRenamed(renamedEvent("manager-ops"))
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.renameHeld || a.probeWanted[renameProbe] {
		t.Fatalf("after claude named itself manager-ops: held %v, wanted %v; want neither", a.renameHeld, a.probeWanted[renameProbe])
	}
}

// A mirror Wake refuses still renamed claude, so the reply owes it the name it
// had - the manager's own, not Wake's.
func TestARefusedMirrorOfTheManagerBringsClaudeBackToItsName(t *testing.T) {
	a, r := aManager(t)
	if err := a.rename(r, "ops!", true); err == nil {
		t.Fatal("a mirror onto a name with a bang was accepted")
	}
	a.noteSent()
	a.noteRenameSent("/rename ops!")
	streamed(a, renamedEvent("ops!"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: ops!", LocalCommand: true})
	a.probeIfWanted()
	a.probeIfWanted()
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename manager-harbor"}) {
		t.Fatalf("claude took ops! past a refused mirror and Wake queued %q, want one [/rename manager-harbor]", got)
	}
}

// A park keeps the rename in the book, and only for the manager: every other
// agent's claude name is its Wake name, which the record already holds.
func TestOnlyTheManagersParkRecordCarriesAClaudeName(t *testing.T) {
	a, r := aManager(t)
	mustRename(t, a, r, "ops")
	if got := recordFor(a).ClaudeName; got != "manager-ops" {
		t.Errorf("the manager's record names claude %q, want manager-ops", got)
	}
	b, rb := renamingAgent(t)
	mustRename(t, b, rb, "bob")
	if got := recordFor(b).ClaudeName; got != "" {
		t.Errorf("an ordinary agent's record names claude %q, want nothing", got)
	}
}

// The book is a file somebody may have edited, so a name read back is held to
// the shape a rename is, and only the manager's record is read for one.
func TestAParkedManagersClaudeNameIsReadBackOnlyIfItIsOne(t *testing.T) {
	for _, tc := range []struct {
		rec  parkedRecord
		want string
	}{
		{parkedRecord{Name: core.ManagerName, ClaudeName: "manager-ops"}, "manager-ops"},
		{parkedRecord{Name: core.ManagerName, ClaudeName: "ops"}, "manager-ops"},
		{parkedRecord{Name: core.ManagerName, ClaudeName: "x y; rm"}, ""},
		{parkedRecord{Name: core.ManagerName}, ""},
		{parkedRecord{Name: "alex", ClaudeName: "manager-ops"}, ""},
	} {
		if got := restoredClaudeName(tc.rec); got != tc.want {
			t.Errorf("%+v was restored as %q, want %q", tc.rec, got, tc.want)
		}
	}
}

// Before anything is renamed the report names the manager for its fleet; once
// a record or the agent says otherwise it names that, so clients draw it so and
// the peer listing drops it by the name claude really has.
func TestTheServedManagerNameFollowsTheRenameAndTheBook(t *testing.T) {
	s := newServer(tempSocket(t))
	fleet := s.managerClaudeDefault()

	if got := s.managerClaude(); got != fleet {
		t.Fatalf("with no manager the report names %q, want the fleet's %q", got, fleet)
	}
	if err := s.parked.add(parkedRecord{ID: idBeta, Name: core.ManagerName, ClaudeName: "manager-ops"}); err != nil {
		t.Fatalf("book the parked manager: %v", err)
	}
	if got := s.managerClaude(); got != "manager-ops" {
		t.Errorf("a parked manager's record says manager-ops and the report names %q", got)
	}

	a, r := aManager(t)
	s.agents[idAlpha] = a
	mustRename(t, a, r, "dev")
	if got := s.managerClaude(); got != "manager-dev" {
		t.Errorf("a renamed live manager is reported as %q, want manager-dev", got)
	}
}

// `manager` by itself asks for the fleet's own name back, since it names no end
// part; any other manager text and every other agent's is passed on untouched.
func TestTheBareWordAsksForTheFleetsNameBack(t *testing.T) {
	s := newServer(tempSocket(t))
	a, _ := aManager(t)
	b, _ := renamingAgent(t)
	for _, tc := range []struct {
		a        *agent
		in, want string
	}{
		{a, "manager", s.managerClaudeDefault()},
		{a, " Manager ", s.managerClaudeDefault()},
		{a, "ops", "ops"},
		{b, "manager", "manager"},
	} {
		if got := s.renameText(tc.a, tc.in); got != tc.want {
			t.Errorf("%q to %s became %q, want %q", tc.in, tc.a.name, got, tc.want)
		}
	}
}

// awaitManagerNamed waits for the report to name the manager so.
func awaitManagerNamed(c *testClient, name string) {
	c.t.Helper()
	c.await("the report naming the manager "+name, func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameStatusPush && f.Status != nil && f.Status.ManagerClaude == name
	})
}

// Over a real process: a rename reaches claude as one /rename of the whole
// name, the report follows it, and the bare word brings the fleet's back.
func TestRenamingTheManagerOverTheWireRenamesItsClaudeSession(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	fleet := core.ManagerPrefix + filepath.Base(filepath.Dir(d.socket))

	spawnManager(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasSuffix(got, "name="+fleet) {
		t.Fatalf("the manager was started as %q, want --name %s", got, fleet)
	}
	// Claude was started under the fleet's name, so asking for it sends nothing.
	c.send(rpc.Frame{Kind: rpc.FrameRename, SessionID: idAlpha, Text: "manager"})
	c.send(rpc.Frame{Kind: rpc.FrameRename, SessionID: idAlpha, Text: "ops"})
	awaitManagerNamed(c, "manager-ops")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [manager-ops]") {
		t.Fatalf("claude was sent %q, want exactly one /rename manager-ops", got)
	}

	c.send(rpc.Frame{Kind: rpc.FrameRename, SessionID: idAlpha, Text: "manager"})
	awaitManagerNamed(c, fleet)
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 2 [manager-ops "+fleet+"]") {
		t.Fatalf("claude was sent %q, want /rename manager-ops then /rename %s", got, fleet)
	}
}

// A woken manager is started under the name it was renamed to, or claude would
// take --name over the rename and every other agent would reach it by the old
// one.
func TestAWokenManagerKeepsItsRename(t *testing.T) {
	fakeClaudeOnPath(t, "argv")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnManager(c, idAlpha)
	managerArgv(c, idAlpha)
	c.send(rpc.Frame{Kind: rpc.FrameRename, SessionID: idAlpha, Text: "ops"})
	awaitManagerNamed(c, "manager-ops")
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if woken := wakeOutcome(c, idAlpha); !woken.woke {
		t.Fatalf("the parked manager was not woken: %s", woken.why)
	}
	if argv := managerArgv(c, idAlpha); !strings.Contains(argv, "--name manager-ops ") {
		t.Errorf("a woken manager was started as\n  %s\nwithout --name manager-ops", argv)
	}
}

// A manager booked by an earlier daemon is served and woken under the name its
// record holds: the book is the only memory of the rename across a ⌃Q.
func TestAManagerWokenFromTheBookKeepsItsRename(t *testing.T) {
	fakeClaudeOnPath(t, "argv")
	brokenPsOnPath(t, psPartial)
	socket := tempSocket(t)
	s := newServer(socket)
	rec := parkedRecord{
		ID: idAlpha, Name: core.ManagerName, Label: "manager", Dir: t.TempDir(), ClaudeName: "manager-ops",
		Parked: time.Date(2026, time.August, 24, 1, 2, 3, 4, time.UTC),
	}
	if err := s.parked.add(rec); err != nil {
		t.Fatalf("write manager park record: %v", err)
	}
	d := startControlledServer(t, s)
	c := attach(t, socket)

	if got := c.status().ManagerClaude; got != "manager-ops" {
		t.Errorf("a daemon holding the book reports the manager as %q, want manager-ops", got)
	}
	if woken := wakeOutcome(c, idAlpha); !woken.woke {
		t.Fatalf("the booked manager was not woken: %s", woken.why)
	}
	if argv := managerArgv(c, idAlpha); !strings.Contains(argv, "--name manager-ops ") {
		t.Errorf("a manager woken from the book was started as\n  %s\nwithout --name manager-ops", argv)
	}
	d.stop(t)
}

// Claude answers the operator's `/rename ops` with a variant when a session is
// already called ops. The passthrough never asked for manager-ops, so no reply to
// it is final: Wake still owes the whole name, once.
func TestAVariantForTheManagersRenameIsStillChasedToItsWholeName(t *testing.T) {
	a, r := aManager(t)
	mustMirror(t, a, r, "ops")
	a.noteSent()
	a.noteRenameSent("/rename ops")
	streamed(a, renamedEvent("ops-2"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: ops-2", LocalCommand: true})
	a.probeIfWanted()
	a.probeIfWanted()
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename manager-ops"}) {
		t.Fatalf("claude took ops-2 and Wake queued %q, want one [/rename manager-ops]", got)
	}
}

// Two windows' renames in one round trip: the first reply releases the hold while
// the later passthrough is still unanswered, and the want must survive it.
func TestTwoWindowsRenamingTheManagerEndAsTheLaterOne(t *testing.T) {
	a, r := aManager(t)
	mustMirror(t, a, r, "dev")
	mustMirror(t, a, r, "ops")
	a.noteSent()
	a.noteRenameSent("/rename dev")
	a.noteRenameSent("/rename ops")
	streamed(a, renamedEvent("dev"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: dev", LocalCommand: true})
	streamed(a, renamedEvent("ops"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: ops", LocalCommand: true})
	a.probeIfWanted()
	a.probeIfWanted()
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename manager-ops"}) {
		t.Fatalf("claude ended as ops and Wake queued %q, want one [/rename manager-ops]", got)
	}
}

// When claude answers Wake's own /rename with a variant of the whole name - a
// session already holds manager-ops - the manager is that variant: the report
// and the book must not name a session that is somebody else's. A reply that
// does not extend the want belongs to a newer rename still pending, and an
// ordinary agent's variant is claude's own choice.
func TestTheManagerTakesTheVariantClaudeAnsweredItsOwnRenameWith(t *testing.T) {
	a, r := aManager(t)
	mustRename(t, a, r, "ops")
	if got := queuedRenames(a); len(got) != 1 {
		t.Fatalf("the idle rename queued %q, want one probe", got)
	}
	if text := a.renameWrite(); text != "/rename manager-ops" {
		t.Fatalf("the probe wrote %q, want /rename manager-ops", text)
	}
	a.incProbe(renameProbe)
	streamed(a, renamedEvent("manager-ops-2"))
	if !a.adoptedVariant() {
		t.Fatal("the manager did not take the variant claude answered with")
	}
	if got := a.wantedClaude(); got != "manager-ops-2" {
		t.Errorf("the manager is called %q to claude, want manager-ops-2", got)
	}
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: manager-ops-2", LocalCommand: true})
	a.probeIfWanted()
	if got := queuedRenames(a); len(got) != 0 {
		t.Errorf("claude was asked again after the variant was taken: %q", got)
	}

	b, rb := aManager(t)
	mustRename(t, b, rb, "ops")
	b.renameWrite()
	b.incProbe(renameProbe)
	mustRename(t, b, rb, "dev")
	streamed(b, renamedEvent("manager-ops-2"))
	if b.adoptedVariant() || b.wantedClaude() != "manager-dev" {
		t.Errorf("a newer rename's want was replaced by %q", b.wantedClaude())
	}

	c, rc := renamingAgent(t)
	mustRename(t, c, rc, "bob")
	c.renameWrite()
	c.incProbe(renameProbe)
	streamed(c, renamedEvent("bob-2"))
	if c.adoptedVariant() {
		t.Error("an ordinary agent took claude's variant as its name")
	}
}

// Two rename probes in flight, the first for a name the second extends: its
// stale reply is the variant of nothing the manager wants now, and taking it
// would leave Wake naming a session claude no longer carries.
func TestAStaleProbeReplyDoesNotRenameTheManagerBackwards(t *testing.T) {
	a, r := aManager(t)
	mustRename(t, a, r, "ops-long")
	queuedRenames(a)
	if text := a.renameWrite(); text != "/rename manager-ops-long" {
		t.Fatalf("the first probe wrote %q", text)
	}
	a.incProbe(renameProbe)
	mustRename(t, a, r, "ops")
	queuedRenames(a)
	if text := a.renameWrite(); text != "/rename manager-ops" {
		t.Fatalf("the second probe wrote %q", text)
	}
	a.incProbe(renameProbe)

	streamed(a, renamedEvent("manager-ops-long"))
	if a.adoptedVariant() || a.wantedClaude() != "manager-ops" {
		t.Fatalf("the stale reply made the manager %q, want manager-ops kept", a.wantedClaude())
	}
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: manager-ops-long", LocalCommand: true})
	streamed(a, renamedEvent("manager-ops"))
	if a.adoptedVariant() || a.wantedClaude() != "manager-ops" {
		t.Fatalf("after the second reply the manager is %q, want manager-ops", a.wantedClaude())
	}

	// One probe in flight, and a newer rename that extends what it asked is not
	// yet written: the reply is exactly what was asked and answers nothing newer.
	b, rb := aManager(t)
	mustRename(t, b, rb, "ops-long")
	queuedRenames(b)
	b.renameWrite()
	b.incProbe(renameProbe)
	mustRename(t, b, rb, "ops")
	streamed(b, renamedEvent("manager-ops-long"))
	if b.adoptedVariant() || b.wantedClaude() != "manager-ops" {
		t.Fatalf("a reply to an older ask made the manager %q, want manager-ops kept", b.wantedClaude())
	}
}

// Over a real process: a collision on the whole name leaves the manager the
// variant claude took, and the report says so.
func TestTheReportNamesTheManagerClaudeTookNotTheOneAsked(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnManager(c, idAlpha)
	c.send(rpc.Frame{Kind: rpc.FrameRename, SessionID: idAlpha, Text: "clash"})
	awaitManagerNamed(c, "manager-clash-2")
}

// Two manager records in one book - one spawned while another sat parked - give
// the report the newest, not whichever the map offers first.
func TestTheNewestManagerRecordNamesTheParkedManager(t *testing.T) {
	s := newServer(tempSocket(t))
	for id, rec := range map[string]parkedRecord{
		idAlpha: {Name: core.ManagerName, ClaudeName: "manager-old", Parked: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
		idBeta:  {Name: core.ManagerName, ClaudeName: "manager-new", Parked: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)},
	} {
		rec.ID = id
		if err := s.parked.add(rec); err != nil {
			t.Fatalf("book %s: %v", id, err)
		}
	}
	for range 40 {
		if got := s.managerClaude(); got != "manager-new" {
			t.Fatalf("the report names the parked manager %q, want the newest record's manager-new", got)
		}
	}
}
