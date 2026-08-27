package ui

// The last-read marker: where you stopped, still visible after you have been
// somewhere else.
//
// Every assertion here is by *event* rather than by row, and that is the whole
// design under test. A marker asserted by scroll offset - or by a line index -
// is an aggregate that a re-wrap keeps "correct" while moving the boundary it
// names, which is the shape that has caught this project twice on rendering
// code. So each test finds the line the last read event rendered to, the line
// the first unread one rendered to, and requires the marker to sit between
// them, at whatever width.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// said is one line from an agent, arriving the way the daemon delivers one.
func said(a App, sessionID, text string) App { return a.applyFrame(eventFrame(sessionID, text)) }

// dmLines is one conversation as a reader sees it, at the size it is laid out
// for - so View takes its fast path and re-lays nothing.
func dmLines(t *testing.T, a App, sessionID string) []string {
	t.Helper()
	if _, ok := a.dms[sessionID]; !ok {
		t.Fatalf("no conversation is held for %q, so there is nothing to read", sessionID)
	}
	// Through dmFor, which is what dmPane draws through: the agent and the
	// dispatch list are projected there and are on no stored DM, so reading
	// a.dms directly draws a pane missing both.
	d := a.dmFor(sessionID)
	return strings.Split(stripANSI(d.View(d.width, d.height)), "\n")
}

// lineWith is where a phrase first appears, or -1.
func lineWith(lines []string, want string) int {
	for i, l := range lines {
		if strings.Contains(l, want) {
			return i
		}
	}
	return -1
}

// boundaries is every line a last-read rule was drawn on.
func boundaries(lines []string) []int {
	var out []int
	for i, l := range lines {
		if strings.Contains(l, lastReadLabel) {
			out = append(out, i)
		}
	}
	return out
}

// between requires a boundary to sit after one event and before another, and
// reports what it found when it does not.
//
// The two events are named by their text rather than by row, which is the whole
// point: a re-wrap moves every row and moves neither event, so an assertion
// phrased in rows is one a re-wrap can satisfy while the boundary has moved
// between a different pair of events entirely.
func between(t *testing.T, what string, lines []string, read, unread string) {
	t.Helper()
	last := lineWith(lines, read)
	first := lineWith(lines, unread)
	if last < 0 || first < 0 {
		t.Fatalf("%s: the conversation is missing %q (%d) or %q (%d):\n%s", what, read, last, unread, first, strings.Join(lines, "\n"))
	}
	found := boundaries(lines)
	for _, at := range found {
		if at > last && at < first {
			return
		}
	}
	if len(found) == 0 {
		t.Errorf("%s: there is no boundary between what had been read and what arrived while somebody was elsewhere:\n%s", what, strings.Join(lines, "\n"))
		return
	}
	t.Errorf("%s: the boundaries are on lines %v, and none of them is between the event on line %d and the one on line %d - they name a different pair of events than the one they were set at:\n%s", what, found, last, first, strings.Join(lines, "\n"))
}

// twoAndTwo is the owner's case in miniature: read a conversation, go and deal
// with another agent, and come back to find more of it than you left.
func twoAndTwo(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a = a.openDMWith("s1", "sydney")
	a = said(a, "s1", "read this first")
	a = said(a, "s1", "and read this second")

	a = a.openDMWith("s2", "john") // an hour with somebody else
	a = said(a, "s1", "arrived third while away")
	a = said(a, "s1", "arrived fourth while away")

	return a.openDMWith("s1", "sydney")
}

// The marker itself. Coming back to the bottom of a wall of text with no idea
// where you stopped is what makes three concurrent long-reading sessions
// impossible rather than merely permitted.
//
// Mutation check: deleting the markerBefore call from DM.Append fails this at
// "there is no boundary between what had been read and what arrived".
func TestComingBackToAConversationShowsWhereYouStopped(t *testing.T) {
	between(t, "after coming back", dmLines(t, twoAndTwo(t), "s1"), "and read this second", "arrived third while away")
}

// The one that decides the design. A width change re-wraps every line beneath
// the boundary, so a marker anchored to a line index names a different place
// afterwards while claiming to name the same one - and the re-wrap is exactly
// what a reader does when they widen the pane to read the long answer they came
// back for.
//
// Mutation check: emitting the marker only from Append and not from renderAll
// fails this at "there is no boundary" after the drag; anchoring it to a line
// offset fails at "it names a different pair of events".
func TestTheMarkerSurvivesTheReWrapADividerDragCauses(t *testing.T) {
	a := twoAndTwo(t)
	before := a.dms["s1"].width

	a = grab(t, a, dividerColumnOf(a))
	for x := dividerColumnOf(a); x > dividerColumnOf(a)-30; x-- {
		a = dragTo(a, x)
	}
	a = settle(a)

	if a.dms["s1"].width == before {
		t.Fatalf("the drag left the conversation at %d columns, so nothing was re-wrapped and this test proves nothing", before)
	}
	between(t, "after a 30-column drag re-wrapped every line", dmLines(t, a, "s1"), "and read this second", "arrived third while away")
}

// While you are looking at a conversation there is no boundary to draw: you are
// reading it as it arrives. A rule that appears under the cursor of somebody who
// never left is the same lie as a legend entry for a key that does nothing.
//
// Mutation check: making markerBefore match any index fails this.
func TestNothingIsMarkedInAConversationNobodyHasLeft(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a = a.openDMWith("s1", "sydney")
	for _, text := range []string{"one", "two", "three"} {
		a = said(a, "s1", text)
	}
	lines := dmLines(t, a, "s1")
	// The positive control. Without it this passes against a build where the
	// conversation received nothing at all - which is the shape of the defect
	// this task found in its own brief, arriving in its own file.
	if lineWith(lines, "three") < 0 {
		t.Fatalf("the conversation is empty, so an absent boundary proves nothing:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(strings.Join(lines, "\n"), lastReadLabel) {
		t.Errorf("a conversation nobody has left carries a last-read boundary:\n%s", strings.Join(lines, "\n"))
	}
	// And the same conversation does draw one once somebody does leave it, so
	// the assertion above is about leaving rather than about the marker being
	// broken everywhere.
	if n := len(boundaries(dmLines(t, away(a, "four"), "s1"))); n != 1 {
		t.Errorf("one absence in the same conversation drew %d rules, want 1: the negative above is not discriminating", n)
	}
}

// Leaving with nothing having happened is not a boundary either. Otherwise a
// glance at another agent and straight back would put a rule above the next
// thing the agent says, which is a claim about an absence nobody was away for.
//
// Mutation check: deleting the Resume call from openDMWith fails this.
func TestAGlanceElsewhereAndStraightBackMarksNothing(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a = a.openDMWith("s1", "sydney")
	a = said(a, "s1", "before the glance")

	a = a.openDMWith("s2", "john")
	a = a.openDMWith("s1", "sydney") // nothing arrived in between
	a = said(a, "s1", "after the glance")
	// And through a re-wrap, so a mark Resume left behind cannot show up later:
	// the two paths draw from the same anchors, and "no anchor" has to mean the
	// same thing on both.
	a, _ = a.resized(160, 40)
	a = settle(a)

	lines := dmLines(t, a, "s1")
	// The positive control: both turns are on screen, so an absent boundary is
	// about the glance rather than about a conversation that received nothing.
	if lineWith(lines, "before the glance") < 0 || lineWith(lines, "after the glance") < 0 {
		t.Fatalf("the conversation is missing its own turns, so an absent boundary proves nothing:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(strings.Join(lines, "\n"), lastReadLabel) {
		t.Errorf("a boundary was drawn for an absence nothing arrived during:\n%s", strings.Join(lines, "\n"))
	}
	// And a real absence in the same conversation still earns one.
	if n := len(boundaries(dmLines(t, away(a, "arrived during a real absence"), "s1"))); n != 1 {
		t.Errorf("a real absence after the glance drew %d rules, want 1: the negative above is not discriminating", n)
	}
}

// The union of the two guards above, and the one that was missing.
//
// One of them asserts that two absences leave two rules and never re-wraps; the
// other re-wraps and only ever has one absence. Read together they sound like
// "rules persist, and they survive a re-wrap". In code they assert the
// intersection of nothing - and the intersection is where the anchor's whole
// claim lives, because a single-slot anchor behind a transcript that holds one
// rule per absence regenerates the newest and drops the rest on the floor.
//
// Mutation check: collapsing marks back to one slot fails this at "none of them
// is between the event on line N and the one on line M" for the first absence.
func TestEveryRuleSurvivesTheReWrap(t *testing.T) {
	a := twoAndTwo(t)
	a = a.openDMWith("s2", "john")
	a = said(a, "s1", "arrived fifth while away")
	a = a.openDMWith("s1", "sydney")

	if n := len(boundaries(dmLines(t, a, "s1"))); n != 2 {
		t.Fatalf("two absences drew %d rules before any re-wrap, so this test is not measuring what a re-wrap does to several", n)
	}
	a, _ = a.resized(160, 40)
	a = settle(a)

	lines := dmLines(t, a, "s1")
	between(t, "the first absence, after a re-wrap", lines, "and read this second", "arrived third while away")
	between(t, "the second absence, after a re-wrap", lines, "arrived fourth while away", "arrived fifth while away")
	if n := len(boundaries(lines)); n != 2 {
		t.Errorf("a width change left %d of the 2 rules, want both:\n%s", n, strings.Join(lines, "\n"))
	}
}

// The owner's literal case, which loses the rule entirely: read, leave, come
// back, glance somewhere and straight back, then widen the pane to read the long
// answer you came back for.
//
// Suppressing a rule for an absence nothing arrived during must not disturb the
// rule already drawn for an absence something did. Widening is the action the
// whole feature is named around, so a marker that a widen deletes is worse than
// no marker: the reader trusts it right up to the moment it is gone.
//
// Mutation check: making Resume clear the state behind a drawn rule - which is
// what a single `marked` flag does - fails this at "there is no boundary".
func TestAGlanceDoesNotDestroyTheRuleAnEarlierAbsenceEarned(t *testing.T) {
	a := twoAndTwo(t)
	a = a.openDMWith("s2", "john")   // a glance
	a = a.openDMWith("s1", "sydney") // and straight back, nothing having arrived
	if n := len(boundaries(dmLines(t, a, "s1"))); n != 1 {
		t.Fatalf("the glance left %d rules on screen, want the 1 the earlier absence earned", n)
	}

	a, _ = a.resized(160, 40)
	a = settle(a)
	between(t, "after the glance and a widen", dmLines(t, a, "s1"), "and read this second", "arrived third while away")
}

// A rule above the first line a conversation ever carries has no "before" on the
// other side of it, so it reads as chrome rather than as a boundary - and it is
// the common case, because a DM opened for the first time starts empty
// (App.observe only appends to conversations already in the map, which is
// deferred item 1).
//
// Mutation check: dropping the `at == 0` guard from Leave fails this at "the
// transcript opens with a boundary above everything".
func TestAnEmptyConversationEarnsNoRuleAboveItsFirstLine(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a = a.openDMWith("s1", "sydney") // opens empty
	if a.dms["s1"].events.len() != 0 {
		t.Fatal("the conversation opened with something in it, so this is not the first-visit case")
	}
	a = away(a, "everything here arrived while away")
	// Through a re-wrap as well: "no anchor" has to mean the same thing on the
	// incremental path and on the one that rebuilds from the anchors.
	a, _ = a.resized(160, 40)
	a = settle(a)

	lines := dmLines(t, a, "s1")
	if lineWith(lines, "everything here arrived") < 0 {
		t.Fatalf("the arrival is missing, so an absent boundary proves nothing:\n%s", strings.Join(lines, "\n"))
	}
	if n := len(boundaries(lines)); n != 0 {
		t.Errorf("the transcript opens with a boundary above everything, which has nothing on the other side of it:\n%s", strings.Join(lines, "\n"))
	}
}

// away is one absence: leave, let something arrive, come back.
//
// The two ways out alternate, because they are two call sites of the same Leave
// and a fixture that only ever took one of them would leave the other's
// intersection with everything below unreached - which is how both findings of
// the second review round got in.
func away(a App, text string) App {
	if len(a.dms["s1"].marks)%2 == 0 {
		a = a.openDMWith("s2", "john")
	} else {
		a = a.closeDM()
	}
	a = said(a, "s1", text)
	return a.openDMWith("s1", "sydney")
}

// The cap, and the one asymmetry it leaves - both halves, so this is a decision
// rather than something somebody discovers.
//
// Lines are append-only, so a rule already rendered cannot be withdrawn from the
// transcript; only the anchors can be dropped. **The surplus is unbounded until
// the next width change** - the transcript keeps one rule per absence for as
// long as nobody resizes, and one re-wrap drops all but the newest three at
// once. Measured at ten absences: ten rules against three anchors, and a single
// widen removes seven. Five is what this test uses because it is enough to
// discriminate; the number is not a bound.
//
// What a re-wrap does to the surplus is *apply the cap* - those boundaries are
// already declared stale by it - and what it must never do is disturb the newest
// three, which is the promise.
//
// Mutation check: dropping the trim in Leave leaves five rules after the
// re-wrap and fails the count.
func TestTheCapIsWhatAReWrapRestoresTheTranscriptTo(t *testing.T) {
	a := twoAndTwo(t) // absences 1
	for _, text := range []string{"absence two", "absence three", "absence four", "absence five"} {
		a = away(a, text)
	}
	if n := len(boundaries(dmLines(t, a, "s1"))); n != 5 {
		t.Fatalf("five absences drew %d rules, so this test is not measuring what the cap does to a surplus", n)
	}

	a, _ = a.resized(160, 40)
	a = settle(a)
	lines := dmLines(t, a, "s1")

	if n := len(boundaries(lines)); n != maxLastReadRules {
		t.Errorf("a re-wrap left %d rules, want the %d the cap keeps:\n%s", n, maxLastReadRules, strings.Join(lines, "\n"))
	}
	// The newest three, by event, and the oldest boundary gone rather than moved
	// somewhere else - a rule that relocated would keep the count and lie.
	between(t, "the newest absence", lines, "absence four", "absence five")
	between(t, "the one before it", lines, "absence three", "absence four")
	if at := lineWith(lines, "arrived third while away"); at >= 0 {
		for _, rule := range boundaries(lines) {
			if rule < at {
				t.Errorf("a rule survived above the cap's window, on line %d:\n%s", rule, strings.Join(lines, "\n"))
			}
		}
	}
}

// The intersection the two guards above still left: prior rules **at the cap**
// *and* a glance that earns nothing.
//
// One of them has a glance with the cap not full; the other fills the cap with
// no glance. Between them sits the case where a glance costs one of the three
// boundaries the cap promises to keep - because a mark is appended and trimmed
// on the way *out*, before anything knows whether it will earn a rule, and
// removing the pending mark on the way back in cannot restore what the trim
// evicted. A glance disturbing nothing is the design's own promise about it.
//
// Both ways out of a conversation, because the trim is on the shared path and a
// test that took only one of them would leave the other's intersection open in
// exactly the way this test exists to close.
//
// Mutation check: trimming in Leave rather than when the mark is crossed fails
// this at "none of them is between the event on line N and the one on line M"
// for the oldest boundary.
func TestAGlanceCostsNoBoundaryWhenTheCapIsFull(t *testing.T) {
	for _, tc := range []struct {
		what  string
		leave func(App) App
	}{
		{"⇥ to another conversation", func(a App) App { return a.openDMWith("s2", "john") }},
		{"⌃W back to the room", func(a App) App { return a.closeDM() }},
	} {
		a := twoAndTwo(t)
		a = away(a, "absence two")
		a = away(a, "absence three")
		if n := len(boundaries(dmLines(t, a, "s1"))); n != maxLastReadRules {
			t.Fatalf("%s: the cap is not full (%d rules), so this test is not measuring what a glance costs at the cap", tc.what, n)
		}

		a = tc.leave(a)                  // a glance that earns nothing
		a = a.openDMWith("s1", "sydney") // and straight back
		a, _ = a.resized(160, 40)
		a = settle(a)

		lines := dmLines(t, a, "s1")
		if n := len(boundaries(lines)); n != maxLastReadRules {
			t.Errorf("%s: a glance that earned nothing left %d boundaries, want the %d the cap keeps:\n%s", tc.what, n, maxLastReadRules, strings.Join(lines, "\n"))
		}
		between(t, tc.what+": the oldest boundary the cap still covers", lines, "and read this second", "arrived third while away")
	}
}

// A second absence earns its own rule, and the first one does not move.
//
// Both halves matter and the second is the one with an argument behind it. The
// messaging-app convention is a single line that relocates; here that would
// erase the landmark at exactly the moment it is in use - somebody half way
// through a long answer glances at another agent, and the rule they were reading
// back to has jumped to the bottom. A rule stays where it was earned because
// "you left off here" does not stop being true.
//
// Mutation check: making DM.Leave keep an existing mark rather than replace it
// leaves the second absence unmarked and fails this at "there is no boundary".
func TestASecondAbsenceGetsItsOwnRuleAndTheFirstStaysWhereItWas(t *testing.T) {
	a := twoAndTwo(t)
	a = a.openDMWith("s2", "john") // away a second time
	a = said(a, "s1", "arrived fifth while away")
	a = a.openDMWith("s1", "sydney")

	lines := dmLines(t, a, "s1")
	between(t, "the second absence", lines, "arrived fourth while away", "arrived fifth while away")
	between(t, "the first absence, after a second one", lines, "and read this second", "arrived third while away")
	if n := len(boundaries(lines)); n != 2 {
		t.Errorf("two absences drew %d rules, want one each:\n%s", n, strings.Join(lines, "\n"))
	}
}

// And a rule is never drawn twice for one absence, however much arrives during
// it - the mark is crossed once.
func TestOneAbsenceDrawsOneRuleHoweverMuchArrivesDuringIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a = a.openDMWith("s1", "sydney")
	a = said(a, "s1", "read before leaving")

	a = a.openDMWith("s2", "john")
	for _, text := range []string{"one", "two", "three", "four"} {
		a = said(a, "s1", text)
	}
	a = a.openDMWith("s1", "sydney")

	if n := len(boundaries(dmLines(t, a, "s1"))); n != 1 {
		t.Errorf("one absence with four arrivals drew %d rules, want 1:\n%s", n, strings.Join(dmLines(t, a, "s1"), "\n"))
	}
}

// Closing the pane is leaving it too. ⌃W is the other way out of a
// conversation, and a boundary that only ⇥ and ⌃D could set would be missing
// for exactly the reader who closes the DM to look at the room.
func TestClosingTheConversationIsAlsoLeavingIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a.roster.Selected = "s1"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	a = said(a, "s1", "read before closing")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlW})
	a = said(a, "s1", "arrived while the pane was shut")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})

	between(t, "after ⌃W and ⌃D", dmLines(t, a, "s1"), "read before closing", "arrived while the pane was shut")
}

// And the counts the sidebar draws say the same thing the rule does, because
// both are the answer to "what arrived while I was elsewhere". Two mechanisms
// that can disagree about that is two accounts of the same absence.
func TestTheUnreadCountAndTheMarkerAgreeAboutWhatArrivedWhileYouWereAway(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a = a.openDMWith("s1", "sydney")
	a = said(a, "s1", "read this first")

	a = a.openDMWith("s2", "john")
	a = said(a, "s1", "arrived while away")

	if agent, _ := a.fleet.Agent("s1"); agent.Unread != 1 {
		t.Fatalf("unread = %d while one line arrived with the conversation off screen, want 1", agent.Unread)
	}
	a = a.openDMWith("s1", "sydney")
	if agent, _ := a.fleet.Agent("s1"); agent.Unread != 0 {
		t.Errorf("unread = %d after opening the conversation, want 0", agent.Unread)
	}
	between(t, "with the badge cleared", dmLines(t, a, "s1"), "read this first", "arrived while away")
}

// hideDM does four things and only one of them is gated by leaveRing. The other
// three - keeping the reader's place, marking the conversation left, and
// clearing the focus so arrivals count as unread - happen on both paths, and
// until now nothing said so: three single-line changes to hideDM survived the
// whole package.
//
// These pin the two halves of the one that matters most, which is whether ⇥
// counts as leaving. It depends on the width, and that is the whole subtlety:
// at a split width the conversation is still on screen next to the room, so
// moving the keys to the room is a focus change and nothing more. Below the
// takeover the same key takes the conversation off screen entirely, which is
// leaving in the only sense the marker cares about.
//
// A boundary in the first case would be a "you left off here" rule appearing in
// a conversation the reader never looked away from. A missing one in the second
// loses their place at the width where the DM is the only pane there is.
func TestWhetherTabToTheRoomIsLeavingDependsOnWhetherTheConversationGoesAway(t *testing.T) {
	for _, tc := range []struct {
		what      string
		width     int
		wantRules int
	}{
		{"beside the room, still on screen", 200, 0},
		{"below the takeover, off screen", 110, 1},
	} {
		a := newRoomApp(t).withSize(tc.width, 40).withAgents("sydney")
		a = a.openDMWith("s1", "sydney")
		a = said(a, "s1", "read this before leaving")

		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
		if a.focus != "" {
			t.Fatalf("%s: ⇥ did not move the keys to the room, so this case tests nothing", tc.what)
		}
		a = said(a, "s1", "arrived after the keys moved")

		if got := len(boundaries(dmLines(t, a, "s1"))); got != tc.wantRules {
			t.Errorf("%s: %d boundaries at %d columns, want %d\n%s",
				tc.what, got, tc.width, tc.wantRules,
				strings.Join(dmLines(t, a, "s1"), "\n"))
		}
	}
}

// And the badge has to tell the same story the rule does, on the path the rule's
// own test never reaches. TestTheUnreadCountAndTheMarkerAgree… goes through
// openDMWith at a split width and never enters hideDM at all, so gating
// fleet.Focus("") on leaveRing left the two renderings of one fact free to
// disagree: the rule says you were away, the sidebar says nothing arrived.
func TestTheBadgeCountsWhatArrivedAfterTabTookTheConversationOffScreen(t *testing.T) {
	a := newRoomApp(t).withSize(110, 40).withAgents("sydney")
	a = a.openDMWith("s1", "sydney")
	a = said(a, "s1", "read this first")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	if a.focus != "" {
		t.Fatalf("the conversation is still open at a takeover width, so it never went away")
	}
	a = said(a, "s1", "arrived while the room had the keys")

	agent, _ := a.fleet.Agent("s1")
	if agent.Unread != 1 {
		t.Errorf("unread = %d after one line arrived with the conversation off screen, want 1: the sidebar says nothing happened while the rule below says you were away", agent.Unread)
	}
	if got := len(boundaries(dmLines(t, a, "s1"))); got != 1 {
		t.Errorf("%d boundaries, want 1: the two renderings of one fact disagree the other way", got)
	}
}
