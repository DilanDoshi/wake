package ui

// The token count on a roster row.
//
// The conversation pane has carried `↓ 12.4k tokens` since PR #15 and the
// sidebar carried none, so the one surface that shows the whole fleet said
// nothing about what any of it had produced.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestARosterRowCarriesItsAgentsTokenCount(t *testing.T) {
	row := stripANSI(headLine(Agent{ID: "s1", Name: "sydney", State: rpc.StateWorking, TurnTokens: 12_400}, rosterWidth))

	if !strings.Contains(row, "12.4k") {
		t.Errorf("the row is %q, want it to carry the agent's token count", row)
	}
	// The same abbreviation the working line uses, so a number in the sidebar
	// and the same number beside a conversation cannot read differently.
	if !strings.Contains(row, tokenArrow+" "+humanTokens(12_400)) {
		t.Errorf("the row is %q, want %q spelled the way the working line spells it", row, tokenArrow+" "+humanTokens(12_400))
	}
	// The word costs seven of twenty-three columns and the arrow already says
	// what the number is.
	if strings.Contains(row, "tokens") {
		t.Errorf("the row is %q; the sidebar has no room for the word", row)
	}
}

// A session that has not finished a turn has no count, and a zero would be a
// figure rather than an absence.
func TestARosterRowWithNoTokensSaysNothingAboutThem(t *testing.T) {
	row := stripANSI(headLine(Agent{ID: "s1", Name: "sydney", State: rpc.StateWorking}, rosterWidth))

	if strings.Contains(row, tokenArrow) {
		t.Errorf("the row is %q, want no count at all before the first turn ends", row)
	}
}

// Only a row with a turn in flight carries one. The count answers "what is this
// costing me right now", and a third of a row spent on a figure that stopped
// moving is clutter on the surface whose whole job is to be scanned.
//
// The domain is every state the roster draws rather than a hand-picked two, so
// a seventh state is a decision somebody has to make here rather than a row that
// quietly starts carrying a number.
func TestOnlyAWorkingRowCarriesATokenCount(t *testing.T) {
	for state := range stateGlyph {
		row := stripANSI(headLine(Agent{ID: "s1", Name: "sydney", State: state, TurnTokens: 12_400}, rosterWidth))
		got := strings.Contains(row, tokenArrow)
		if want := state == rpc.StateWorking; got != want {
			t.Errorf("a %s row is %q; carries a count = %v, want %v", state, row, got, want)
		}
	}
}

// The load-bearing rule of this file: lipgloss joins columns on their widest
// line, so a row one column too wide shoves the room and the conversation
// sideways for as long as it is on screen.
//
// **The width half of this is not evidence about the count** and is kept as an
// invariant rather than as a guard on this feature: Roster.rows clips every line
// it returns, so a headLine that overflowed would be cut rather than drawn wide,
// and deleting the budget in headLine leaves this half green. Measured, not
// assumed.
//
// The half that *is* evidence is the second one. A clip through a figure
// produces `↓ 12` from `↓ 12.4k` - a different number, silently - which is why
// the count is dropped whole rather than shortened, and why an arrow on a row
// has to come with the whole figure behind it.
func TestNoTokenCountMakesARowWiderThanTheSidebarOrIsCutIntoADifferentNumber(t *testing.T) {
	for _, name := range []string{"jo", "sydney", "alexandra", "bartholomew", "a-very-long-agent-name"} {
		for _, tokens := range []int{0, 890, 12_400, 1_200_000} {
			for _, unread := range []int{0, 3, 120} {
				a := Agent{ID: "s1", Name: name, State: rpc.StateWorking, TurnTokens: tokens, Unread: unread}
				for _, line := range (Roster{}).rows(a, nil, rosterWidth) {
					row := stripANSI(line)
					if got := lipgloss.Width(row); got > rosterWidth {
						t.Errorf("name %q, %d tokens, %d unread: the row is %d columns against a sidebar of %d:\n%q",
							name, tokens, unread, got, rosterWidth, row)
					}
					if strings.Contains(row, tokenArrow) && !strings.Contains(row, humanTokens(tokens)) {
						t.Errorf("name %q, %d tokens, %d unread: the row carries a cut figure, which is a different number:\n%q",
							name, tokens, unread, row)
					}
				}
			}
		}
	}
}

// The name is how an agent is addressed and the count is something to know, so
// a row too narrow for both keeps the name whole and drops the count. The badge
// still wins over both, which is the rule that was already here.
func TestALongNameDropsTheCountRatherThanBeingCutForIt(t *testing.T) {
	const long = "bartholomew-the-third"
	row := stripANSI(headLine(Agent{ID: "s1", Name: long, State: rpc.StateWorking, TurnTokens: 12_400}, rosterWidth))

	if strings.Contains(row, tokenArrow) {
		t.Errorf("the row is %q, want the count dropped rather than the name cut to fit it", row)
	}

	// And the badge is still budgeted first, which is the older ruling this
	// must not have quietly reversed.
	badged := stripANSI(headLine(Agent{ID: "s1", Name: long, State: rpc.StateWorking, TurnTokens: 12_400, Unread: 3}, rosterWidth))
	if !strings.HasSuffix(strings.TrimRight(badged, " "), "3") {
		t.Errorf("the row is %q, want the unread badge kept before anything else", badged)
	}
}

// A short name has room for both, which is what makes the test above about the
// budget rather than about the count never being drawn.
func TestAShortNameKeepsBothTheCountAndTheBadge(t *testing.T) {
	row := stripANSI(headLine(Agent{ID: "s1", Name: "jo", State: rpc.StateWorking, TurnTokens: 890, Unread: 3}, rosterWidth))

	for _, want := range []string{"jo", "890", "3"} {
		if !strings.Contains(row, want) {
			t.Errorf("the row is %q, want it to carry %q", row, want)
		}
	}
}

// And it is on the sidebar an operator actually sees, not only in the helper.
func TestTheSidebarDrawsTheTokenCount(t *testing.T) {
	agents := []Agent{{ID: "s1", Name: "sydney", State: rpc.StateWorking, TurnTokens: 12_400}}
	out := stripANSI(Roster{}.View(agents, nil, rosterWidth, 8))

	if !strings.Contains(out, "12.4k") {
		t.Errorf("the sidebar does not draw the count:\n%s", out)
	}
}
