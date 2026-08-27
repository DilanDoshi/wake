package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realMCPList is `claude mcp list` as it actually came back on 2026-08-14, with
// all three states in it. A recording rather than a shape somebody imagined:
// the parser splits on claude's glyphs, and the thing most likely to break it
// is a real URL rather than a placeholder.
const realMCPList = `Checking MCP server health…

claude.ai Slack: https://mcp.slack.com/mcp - ✔ Connected
firecrawl: npx -y firecrawl-mcp - ✔ Connected
linear-server: https://mcp.linear.app/mcp (HTTP) - ✔ Connected
playwright: npx @playwright/mcp@latest --browser chrome --isolated - ✔ Connected
slack: https://mcp.slack.com/mcp (HTTP) - ✘ Failed to connect — Incompatible auth server: does not support dynamic client registration
higgsfield: https://mcp.higgsfield.ai/mcp (HTTP) - ! Needs authentication`

func TestTheListIsParsedIntoRows(t *testing.T) {
	rows := parseMCPList(realMCPList)
	if len(rows) != 6 {
		t.Fatalf("parsed %d rows from six servers: %+v", len(rows), rows)
	}

	byName := map[string]mcpRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}

	// A URL survives whole. Splitting on ":" or "-" cuts one of these in half,
	// which is why the parser splits on the status glyph.
	if got := byName["linear-server"].Where; got != "https://mcp.linear.app/mcp (HTTP)" {
		t.Errorf("linear-server is at %q, want the URL and its transport intact", got)
	}
	if got := byName["playwright"].Where; !strings.Contains(got, "--browser chrome --isolated") {
		t.Errorf("playwright's command lost its flags: %q", got)
	}
	// A name with a space in it is a name, not two fields.
	if _, ok := byName["claude.ai Slack"]; !ok {
		t.Errorf("a server named with a space was not parsed: %+v", rows)
	}
	// And claude's own first line is not a server.
	if _, ok := byName["Checking MCP server health…"]; ok {
		t.Error("the health-check banner was parsed as a server")
	}
}

// The two broken states are told apart, which is the whole reason this panel
// pays for a health check instead of reading the init frame Wake already has.
//
// init.mcp_servers reports `needs-auth` for both of these. Only one of them can
// be fixed by authenticating; the other's auth server is incompatible, so a
// prompt would fail every time somebody tried.
func TestAFailureIsNotTheSameAsNeedingAuthentication(t *testing.T) {
	rows := parseMCPList(realMCPList)
	byName := map[string]mcpRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}

	if !byName["higgsfield"].NeedsAuth() {
		t.Error("higgsfield needs authentication and the panel does not say so")
	}
	if byName["slack"].NeedsAuth() {
		t.Error("slack failed to connect and the panel offers to authenticate it: its auth server " +
			"does not support dynamic client registration, so that prompt cannot succeed")
	}
	if got := byName["slack"].Detail; !strings.Contains(got, "dynamic client registration") {
		t.Errorf("slack's row lost the reason it failed (%q), which is the half that says whether "+
			"authenticating would help", got)
	}
}

// Only a server authenticating could fix is told how, and the command carries
// the directory.
func TestOnlyAServerThatCanBeAuthenticatedIsToldHow(t *testing.T) {
	panel := mcpPanel(parseMCPList(realMCPList), "/tmp/repo", 100)

	if !strings.Contains(panel, "claude mcp login higgsfield") {
		t.Errorf("the panel does not say how to authenticate higgsfield:\n%s", panel)
	}
	if strings.Contains(panel, "claude mcp login slack") {
		t.Errorf("the panel offers to authenticate a server whose auth server is incompatible:\n%s", panel)
	}
	if !strings.Contains(panel, "/tmp/repo") {
		t.Errorf("the command has no directory in it, so it is a step short:\n%s", panel)
	}
	for _, connected := range []string{"firecrawl", "playwright"} {
		if strings.Contains(panel, "login "+connected) {
			t.Errorf("the panel offers to authenticate %s, which is connected:\n%s", connected, panel)
		}
	}
}

// A directory with no servers says so, and does not look like a failure.
func TestNoServersIsNotAProblem(t *testing.T) {
	panel := mcpPanel(nil, "/tmp/repo", 100)
	if !strings.Contains(panel, mcpNone) {
		t.Errorf("an empty panel does not say it is empty:\n%s", panel)
	}
	if strings.Contains(panel, mcpAuthHint) {
		t.Errorf("an empty panel tells somebody to authenticate something:\n%s", panel)
	}
}

// docs/notes/bugs.md BUG-9. A server's name and the reason it failed are the
// server's own words, and they reach the panel from a process Wake ran rather
// than through the airlock - so a server answering with an OSC 52 in its name
// set the operator's clipboard the moment they typed `/mcp`.
//
// Driven through bangRun, which is the shell this panel is built from and where
// the fence is: the health check's bytes are contained as they become a Wake
// string, and the rows and the panel are derived from the contained text. The
// predicate is bang_test.go's, for the reason written there.
func TestAServersOwnWordsCannotDriveTheTerminal(t *testing.T) {
	dir := t.TempDir()
	const hostile = "Checking MCP server health\u2026\n\n" +
		"slack\x1b]52;c;cHduZWQ=\a: https://mcp.slack.com/mcp (HTTP) - \u2718 Failed to connect \u2014 \x1b[2J\x1b[Hforged\n" +
		"higgsfield: npx thing\u009b2J - ! Needs authentication\n"
	if err := os.WriteFile(filepath.Join(dir, "list.txt"), []byte(hostile), 0o600); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}

	rows := parseMCPList(bangRunWithin(t, bangTestLimit, dir, "cat list.txt", bangTimeout, bangWaitDelay))
	if len(rows) != 2 {
		t.Fatalf("parsed %d rows from two servers, so this asserted nothing about them: %+v", len(rows), rows)
	}
	// A short directory, because the login line carries one and a temp path
	// would be cut off by the width rather than by anything under test.
	panel := mcpPanel(rows, "/tmp/repo", 120)
	if i := strings.IndexFunc(panel, actsOnATerminal); i >= 0 {
		t.Errorf("the panel keeps a character a terminal acts on, at %d: %q", i, panel)
	}
	// Contained, not dropped: the row is still the row somebody opened /mcp to
	// read, and the server that can be authenticated is still told how.
	if !strings.Contains(panel, "Failed to connect") || !strings.Contains(panel, "claude mcp login higgsfield") {
		t.Errorf("containment took the panel's own content with it:\n%s", panel)
	}
}
