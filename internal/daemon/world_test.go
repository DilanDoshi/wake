package daemon

import (
	"slices"
	"testing"
)

// A machine, as ps lists it: the daemon (100), an agent's launcher and claude
// (200, 201) with an MCP server (202) in their group, a background shell (300)
// and its sleep (301) in a group of their own, and the operator's own shell
// (400), which has nothing to do with the fleet.
func machine() []procRow {
	return []procRow{
		{100, 1, 100, "Wed Nov 11 09:00:00 2026", ""},
		{200, 100, 200, "Wed Nov 11 09:01:00 2026", ""},
		{201, 200, 200, "Wed Nov 11 09:01:01 2026", ""},
		{202, 201, 200, "Wed Nov 11 09:01:02 2026", ""},
		{300, 201, 300, "Wed Nov 11 09:02:00 2026", ""},
		{301, 300, 300, "Wed Nov 11 09:02:01 2026", ""},
		{400, 1, 400, "Wed Nov 11 08:00:00 2026", ""},
	}
}

func pgids(gs []procGroup) []int {
	out := make([]int, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.pgid)
	}
	slices.Sort(out)
	return out
}

func pids(g procGroup) []int {
	out := make([]int, 0, len(g.members))
	for _, m := range g.members {
		out = append(out, m.pid)
	}
	slices.Sort(out)
	return out
}

func TestTheWorldIsTheAgentsGroupAndTheGroupsItsMembersLead(t *testing.T) {
	w := worldOf(machine(), 200)

	if got := pids(w.own); !slices.Equal(got, []int{200, 201, 202}) {
		t.Errorf("own group members = %v, want [200 201 202]", got)
	}
	if got := pgids(w.foreign); !slices.Equal(got, []int{300}) {
		t.Fatalf("foreign groups = %v, want only the shell's, 300", got)
	}
	if got := pids(w.foreign[0]); !slices.Equal(got, []int{300, 301}) {
		t.Errorf("the shell's group = %v, want the shell and its sleep", got)
	}
}

// Another fleet's daemon, started by a command an agent ran, is a child of that
// command: reaching it would end a fleet this one does not own.
func TestOnlyADirectChildOfTheAgentsGroupCountsAsItsGroup(t *testing.T) {
	rows := append(machine(),
		procRow{500, 301, 500, "Wed Nov 11 09:03:00 2026", ""}, // another daemon, led from the shell's sleep
		procRow{600, 202, 100, "Wed Nov 11 09:04:00 2026", ""}, // a child that joined the daemon's group
		procRow{700, 202, 710, "Wed Nov 11 09:05:00 2026", ""}, // a child in a group it does not lead
	)

	got := pgids(worldOf(rows, 200).foreign)

	if !slices.Equal(got, []int{300}) {
		t.Errorf("foreign groups = %v, want only 300: a grandchild's group, the daemon's own, and a group its member does not lead are not the agent's to end", got)
	}
}

// With nothing at the agent's pid leading its group, the agent is gone and the
// number may be anybody's.
func TestAGroupWhoseLeaderIsNotListedIsNotTheAgents(t *testing.T) {
	rows := machine()[:0:0]
	rows = append(rows, procRow{900, 1, 200, "Thu Nov 12 09:00:00 2026", ""})

	if w := worldOf(rows, 200); len(w.own.members) != 0 || len(w.foreign) != 0 {
		t.Errorf("world = %+v, want nothing: no process leads group 200", w)
	}
	for _, bad := range []int{0, 1, -5} {
		if w := worldOf(machine(), bad); len(w.own.members) != 0 || len(w.foreign) != 0 {
			t.Errorf("worldOf(%d) = %+v, want nothing", bad, w)
		}
	}
}

// A group stays noted while any process noted in it runs, whatever became of
// the rest - the shell that left its child behind - and goes the moment none
// does.
func TestARefreshKeepsAGroupWhileANotedMemberLives(t *testing.T) {
	first := worldOf(machine(), 200)

	// The shell (300) exited; its sleep (301) is orphaned under launchd.
	later := []procRow{
		{100, 1, 100, "Wed Nov 11 09:00:00 2026", ""},
		{200, 100, 200, "Wed Nov 11 09:01:00 2026", ""},
		{201, 200, 200, "Wed Nov 11 09:01:01 2026", ""},
		{301, 1, 300, "Wed Nov 11 09:02:01 2026", ""},
		{310, 201, 310, "Wed Nov 11 09:10:00 2026", ""}, // a second shell
	}
	w := first.refreshed(later, 200)

	if got := pgids(w.foreign); !slices.Equal(got, []int{300, 310}) {
		t.Fatalf("foreign groups = %v, want 300 (kept by its sleep) and 310 (new)", got)
	}
	for _, g := range w.foreign {
		if g.pgid == 300 && !slices.Equal(pids(g), []int{301}) {
			t.Errorf("group 300 members = %v, want only the survivor [301]", pids(g))
		}
	}

	gone := w.refreshed(later[:3], 200)
	if len(gone.foreign) != 0 {
		t.Errorf("foreign groups = %v after nothing in them was left, want none", pgids(gone.foreign))
	}
}

// A pid that is a different process now - same number, another start time - is
// not the one that was noted, and its group is not either.
func TestAGroupIsSweptOnlyWhileANotedMemberIsStillThatProcess(t *testing.T) {
	noted := worldOf(machine(), 200).foreign

	same := machine()
	if got := sweepable(same, noted); !slices.Equal(got, []int{300}) {
		t.Errorf("sweepable = %v, want [300] while the noted processes run", got)
	}

	recycled := []procRow{
		{301, 1, 300, "Fri Nov 13 10:00:00 2026", ""}, // 301 again, an hour of nobody's business later
	}
	if got := sweepable(recycled, noted); len(got) != 0 {
		t.Errorf("sweepable = %v, want none: the process at 301 started at another time", got)
	}

	moved := []procRow{
		{301, 1, 777, "Wed Nov 11 09:02:01 2026", ""}, // the noted process, in another group now
	}
	if got := sweepable(moved, noted); len(got) != 0 {
		t.Errorf("sweepable = %v, want none: the noted process left group 300", got)
	}

	if got := sweepable(nil, noted); len(got) != 0 {
		t.Errorf("sweepable = %v over an empty listing, want none", got)
	}
}

func TestEachGroupIsNamedOnceHoweverManyTimesItWasNoted(t *testing.T) {
	g := worldOf(machine(), 200).foreign
	twice := append(slices.Clone(g), g...)

	if got := sweepable(machine(), twice); !slices.Equal(got, []int{300}) {
		t.Errorf("sweepable = %v, want [300] once", got)
	}
}

func TestParsingAListingTakesPidParentGroupStartAndCommand(t *testing.T) {
	out := []byte("  100     1   100 Wed Nov 11 09:00:00 2026 /usr/local/bin/wake daemon\n" +
		"  201   200   200 Wed Nov 11 09:01:01 2026 sleep   7311\n" +
		"  202   200   200 Wed Nov 11 09:01:02 2026\n" +
		"not a row\n" +
		"  x   1   2 Wed Nov 11 09:00:00 2026 cmd\n" +
		"\n")

	got := parseProcRows(out)

	want := []procRow{
		{100, 1, 100, "Wed Nov 11 09:00:00 2026", "/usr/local/bin/wake daemon"},
		{201, 200, 200, "Wed Nov 11 09:01:01 2026", "sleep 7311"},
		{202, 200, 200, "Wed Nov 11 09:01:02 2026", ""},
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestTheLedgerReleasesWhatAnAgentOrItsLaterSelfHeld(t *testing.T) {
	var l worldLedger
	a := []procGroup{{pgid: 200}}
	b := []procGroup{{pgid: 300}}
	l.hold([]string{"id-a", "conv-a"}, a)
	l.hold([]string{"id-b"}, b)

	if got := l.take("conv-a"); !slices.Equal(pgids(got), []int{200}) {
		t.Errorf("take(conv-a) = %v, want [200]", pgids(got))
	}
	if got := l.take("conv-a"); len(got) != 0 {
		t.Errorf("a second take = %v, want none: it was released", pgids(got))
	}
	if got := l.takeAll(); !slices.Equal(pgids(got), []int{300}) {
		t.Errorf("takeAll = %v, want [300]", pgids(got))
	}
}

// A session that parks, wakes and parks again is one entry: what its first life
// left is still held beside what its second did.
func TestTheLedgerKeepsOneEntryPerSessionAcrossRepeatedParks(t *testing.T) {
	var l worldLedger
	l.hold([]string{"id-a", "conv-a"}, []procGroup{{pgid: 200}, {pgid: 300}})
	l.hold([]string{"conv-a", "conv-b"}, []procGroup{{pgid: 300}, {pgid: 400}})

	if len(l.held) != 1 {
		t.Fatalf("%d entries held for one session, want 1", len(l.held))
	}
	if got := l.take("conv-b"); !slices.Equal(pgids(got), []int{200, 300, 400}) {
		t.Errorf("take(conv-b) = %v, want every group either life left, once each: [200 300 400]", pgids(got))
	}
}

// Another fleet's daemon, or an agent's launcher, is never ended with this one,
// even if a claude ever ran a command in the agent's own group: its group is not
// noted whoever led it.
func TestAGroupHoldingAnotherFleetIsNeverNoted(t *testing.T) {
	for name, argv := range map[string]string{
		"a daemon":   "/opt/bin/wake daemon",
		"a launcher": "/opt/bin/wake --wake-agent-launcher --print --input-format stream-json",
	} {
		rows := append(machine(),
			procRow{800, 201, 800, "Wed Nov 11 09:20:00 2026", "sleep 9"}, // a shell's own group, kept
			procRow{810, 201, 810, "Wed Nov 11 09:21:00 2026", argv},      // another fleet's, led by a direct child
			procRow{811, 810, 810, "Wed Nov 11 09:21:01 2026", "claude"},  // and a member of it
			procRow{820, 201, 820, "Wed Nov 11 09:22:00 2026", "sh -c x"}, // a shell whose member is the fleet's
			procRow{821, 820, 820, "Wed Nov 11 09:22:01 2026", argv},      //
		)

		got := pgids(worldOf(rows, 200).foreign)

		if !slices.Equal(got, []int{300, 800}) {
			t.Errorf("%s: foreign groups = %v, want only the shells', 300 and 800", name, got)
		}
	}
}

// A listing a sweep could not read puts its groups back, so the daemon's exit
// can try again; nothing was signalled on it.
func TestAGroupAFailedListingCouldNotProveIsHeldForTheExit(t *testing.T) {
	brokenPsOnPath(t, psRefuses)
	s := &server{}

	s.sweepGroups([]procGroup{{pgid: 300, members: []procRef{{301, "Wed Nov 11 09:02:01 2026"}}}})

	if got := s.worlds.takeAll(); !slices.Equal(pgids(got), []int{300}) {
		t.Errorf("held after the failed sweep = %v, want [300]", pgids(got))
	}
}
