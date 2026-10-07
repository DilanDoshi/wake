package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A client that learns of an agent only through a report - which is every late
// attach and every reattach - still gets its advertised commands, so the
// completion menu is not empty for it. The commands ride the one-per-turn init
// *event*, and no event is replayed to a client that attached after it, so
// without the report carrying them a reattached client saw no menu for any
// agent: `@alex /co` and `/co` in alex's own DM both showed nothing.
//
// This is the same route Effort and Budget already take, and for the same
// reason - see rpc.SessionStatus.Commands.
func TestAdvertisedCommandsSurviveAnAttachViaTheReport(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)

	// A status push naming an agent with commands and no prior init event: the
	// reattach case, where the report is the only thing the client has.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle, Commands: []string{"compact", "commit-push", "complete-linear-ticket"}},
	}}})

	agent, ok := a.fleet.Agent("s1")
	if !ok {
		t.Fatal("no agent s1 after the report")
	}
	if got := slices.Collect(agent.advertised.words()); len(got) != 3 {
		t.Errorf("advertised is %v after a report carrying 3 commands; a client that only has the report "+
			"(every reattach) never learns them, so /co shows no menu", got)
	}
}

// A just-spawned agent has taken no turn, so no init ever named its commands -
// the daemon's report is all that does, from the handshake's reply. Typing a
// first brief to it in the room and ending it with a /command must still offer
// that agent's skills, and ⇥ must replace only the token being typed (BUG-46).
func TestAFreshAgentsReportedSkillsCompleteAtTheEndOfALongBrief(t *testing.T) {
	brief := func(commands []string) App {
		fresh(t)
		a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
			Running: true, Sessions: []rpc.SessionStatus{
				{ID: "s1", Name: "juno", State: rpc.StateIdle, Commands: commands},
			}}})
		a = a.withDraft("@juno read the failing test in the parser")
		for _, line := range []string{"and find out why it only fails on CI", "then, when it is green,"} {
			a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlJ})
			a = a.withDraft(line)
		}
		return a.withDraft(" run /compl")
	}

	if none := brief(nil); none.completionUp() {
		t.Fatalf("an agent with no commands drew a menu %v: the fixture asserts nothing", none.completion.offers)
	}

	a := brief([]string{"compact", "complete-linear-ticket"})
	if !a.completionUp() || !slices.Contains(a.completion.offers, "/complete-linear-ticket") {
		t.Fatalf("a brief ending in /compl drew no offer of the agent's skill: %v", a.completion.offers)
	}
	before := a.composer().Value()
	a, _, ok := a.completionKey(tea.KeyMsg{Type: tea.KeyTab})
	if !ok {
		t.Fatal("⇥ was not taken by the completion menu")
	}
	if got, want := a.composer().Value(), strings.TrimSuffix(before, "/compl")+"/complete-linear-ticket "; got != want {
		t.Errorf("⇥ left the draft %q, want only the token replaced: %q", got, want)
	}
}
