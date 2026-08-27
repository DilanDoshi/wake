package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// The warning row is Claude Code's, and it costs nothing when there is nothing
// to warn about.
//
// Empty is the overwhelmingly common case - most sessions hold no MCP servers -
// so a row reserved for a warning nobody has would be spent on every
// conversation in the fleet, forever.
func TestTheMCPRowAppearsOnlyWhenSomethingNeedsAuthenticating(t *testing.T) {
	for _, tc := range []struct {
		needsAuth int
		want      string
	}{
		{0, ""},
		{1, "1 MCP server needs authentication"},
		{2, "2 MCP servers need authentication"},
		{7, "7 MCP servers need authentication"},
	} {
		got := mcpWarning(tc.needsAuth)
		if tc.want == "" {
			if got != "" {
				t.Errorf("nothing needs authenticating and the banner said %q", got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("with %d needing auth the row reads %q, want it to contain %q",
				tc.needsAuth, got, tc.want)
		}
		// And it names no command. Claude Code says "· run /mcp"; Wake cannot,
		// because every session here is headless and the bare interactive
		// commands anybody has recorded there are inert. A surface may not
		// advertise something that does nothing - see mcpWarning.
		if strings.Contains(got, "/") {
			t.Errorf("the row %q names a command; nothing has recorded one that works from a "+
				"headless session, and the legend's rule binds this surface too", got)
		}
	}
}

// One is one. "1 MCP servers need" is the tell that a count was formatted
// rather than written, and this line sits at the top of a conversation.
func TestTheMCPRowCountsOneAsOne(t *testing.T) {
	got := mcpWarning(1)
	if strings.Contains(got, "servers") || strings.Contains(got, "need authentication") {
		t.Errorf("the single-server row reads %q, which is the plural wording", got)
	}
}

// The count is derived from what actually crossed the airlock, not from a
// number somebody set.
//
// This is the whole path: an init frame's roster, decoded by core, folded onto
// the agent, counted for the banner. Driven from a real recording, so the shape
// is Claude's rather than this file agreeing with itself.
func TestTheCountComesFromAnInitFramesRoster(t *testing.T) {
	f := &core.SessionFacts{Model: "claude-opus-4-6", MCPServers: []core.MCPServer{
		{Name: "firecrawl", State: core.MCPConnected},
		{Name: "linear-server", State: core.MCPPending},
		{Name: "slack", State: core.MCPNeedsAuth},
		{Name: "higgsfield", State: core.MCPNeedsAuth},
	}}

	a := Agent{ID: "s1"}.withFacts(f)
	if a.MCPNeedsAuth != 2 {
		t.Fatalf("a roster with two needing auth counted %d: connected and pending are not warnings, "+
			"and counting them would put a row on every session holding any server at all", a.MCPNeedsAuth)
	}
	if !strings.Contains(dmBanner(a, 100).text, "2 MCP servers") {
		t.Errorf("the count reached the agent and not the banner:\n%s", dmBanner(a, 100).text)
	}
}

// A turn ending must not blank the warning.
//
// A result frame names no servers, so a fold that recounted unconditionally
// would clear the row once per turn - the row would appear at the start of a
// turn and vanish at the end of it, which reads as a bug in whatever the agent
// just did. This is the same trap Model and the context window are guarded
// against, one field over.
func TestATurnEndingDoesNotClearTheMCPWarning(t *testing.T) {
	a := Agent{ID: "s1"}.withFacts(&core.SessionFacts{
		Model:      "claude-opus-4-6",
		MCPServers: []core.MCPServer{{Name: "slack", State: core.MCPNeedsAuth}},
	})
	if a.MCPNeedsAuth != 1 {
		t.Fatalf("the init did not establish the count: %d", a.MCPNeedsAuth)
	}

	// What a result frame carries: usage, and no roster at all.
	after := a.withFacts(&core.SessionFacts{ContextTokens: 900, ContextWindow: 200_000, OutputTokens: 12})
	if after.MCPNeedsAuth != 1 {
		t.Errorf("a turn ending left %d servers needing auth, want 1: the roster rides on init and a "+
			"frame that carries none says nothing about it", after.MCPNeedsAuth)
	}
}

// The room never draws it. The room is the fleet, not a session, and there is
// no one roster to warn about.
func TestTheRoomBannerHasNoMCPRow(t *testing.T) {
	if got := roomBanner(100).text; strings.Contains(got, "MCP") {
		t.Errorf("the room banner warns about MCP servers:\n%s", got)
	}
}
