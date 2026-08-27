package ui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// In the room, `@name /command` strips the mention and sends `/command` to that
// agent, so the menu after it must surface that agent's own commands - where an
// operator's custom skills live. The bug: Wake's own verbs (eleven, once /board
// joined them) fill the menu to its bound and push the agent's commands below
// the fold, so `@juno /` shows no custom skill at all.
func TestARoomMentionSurfacesTheAgentsCustomSkills(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("juno").withSize(200, 40)
	a = a.advertising("s1", "deploy-prod", "clear", "compact")
	a = a.withDraft("@juno /")

	if !slices.Contains(a.completion.offers, "/deploy-prod") {
		t.Errorf("`@juno /` does not offer juno's custom skill /deploy-prod - it is crowded out by "+
			"Wake's own verbs, which is the whole complaint: %v", a.completion.offers)
	}
	for _, wake := range []string{"/resume", "/manager-stop", "/adopt"} {
		if slices.Contains(a.completion.offers, wake) {
			t.Errorf("`@juno /` offers Wake's own %q, but `@juno %s` routes it to juno as text rather "+
				"than invoking Wake - and it displaces juno's own commands: %v", wake, wake, a.completion.offers)
		}
	}
}

// Typing more filters to the agent's matching commands and drops Wake's own -
// `/name` and `/new` are Wake verbs, so a menu still offering them for `@juno /n`
// is the crowding bug one prefix along, and juno's `/notebook` is what an operator
// reaching for a custom skill wants there. `/n` rather than `/c` on purpose: no
// Wake verb starts with `/c`, so that prefix cannot tell the fix from its absence.
func TestARoomMentionFiltersToTheAgentsCommands(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("juno").withSize(200, 40)
	a = a.advertising("s1", "notebook", "clear")
	a = a.withDraft("@juno /n")

	if !slices.Contains(a.completion.offers, "/notebook") {
		t.Fatalf("`@juno /n` does not offer juno's /notebook: %v", a.completion.offers)
	}
	for _, wake := range []string{"/name", "/new"} {
		if slices.Contains(a.completion.offers, wake) {
			t.Errorf("`@juno /n` still offers Wake's own %q behind the mention: %v", wake, a.completion.offers)
		}
	}
}

// Accepting a mentioned command keeps the mention, because that is what routes
// the command to the agent it was typed for.
func TestAcceptingAMentionedCommandKeepsTheMention(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("juno").withSize(200, 40)
	a = a.advertising("s1", "clear")
	a = a.withDraft("@juno /cl")

	a, _, ok := a.completionKey(tea.KeyMsg{Type: tea.KeyTab})
	if !ok {
		t.Fatal("⇥ was not taken by the completion menu")
	}
	if got := a.composer().Value(); got != "@juno /clear " {
		t.Errorf("accepting `/clear` under `@juno /cl` gave %q, want %q", got, "@juno /clear ")
	}
}

// A command with no leading mention is unchanged: it still offers Wake's own
// verbs, which is what a bare `/resume` in the room is for.
func TestAPlainCommandStillOffersWakeVerbs(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("juno").withSize(200, 40)
	a = a.advertising("s1", "clear")
	a = a.withDraft("/r")

	if !slices.Contains(a.completion.offers, "/resume") {
		t.Errorf("`/r` no longer offers Wake's own /resume: the mention branch leaked into the plain "+
			"command path: %v", a.completion.offers)
	}
}
