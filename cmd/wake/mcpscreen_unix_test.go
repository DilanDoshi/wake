//go:build unix

package main

import (
	"strings"
	"testing"
)

// /mcp on a real terminal draws the agent's own servers, grouped and marked.
func TestSlashMCPDrawsTheServerMenuOnScreen(t *testing.T) {
	withScriptedAgent(t, scriptMCP)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 120, 36)
	s.await("ready")
	s.send("/mcp\r")
	s.await("Manage MCP servers")
	s.await("needs authentication")
	for _, want := range []string{"User MCPs", "firecrawl", "1 tool", "higgsfield"} {
		if !strings.Contains(s.text(), want) {
			t.Errorf("the menu does not show %q:\n%s", want, s.dump())
		}
	}
}

// openHiggsfield opens the menu and the needs-auth server's detail view.
func openHiggsfield(t *testing.T) *screen {
	t.Helper()
	withScriptedAgent(t, scriptMCP)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	s := startWakeInAConversation(t, 120, 36)
	s.await("ready")
	s.send("/mcp\r")
	s.await("needs authentication")
	s.send("\x1b[B\r") // down to higgsfield, open it
	s.await("Authenticate")
	return s
}

// Authenticate hands the real terminal to claude's own sign-in: what is typed
// reaches it (a pipe would have been refused), Wake comes back afterwards, and
// the agent is reconnected on the new login.
func TestAuthenticateHandsTheTerminalToTheSignInAndComesBack(t *testing.T) {
	s := openHiggsfield(t)
	s.send("\r")
	s.await("Signing in to higgsfield for Wake")
	s.await("Paste the redirect URL here")
	s.send("hello\r")
	// Wake has the terminal back, the agent reconnected, and the refreshed
	// detail view shows the server connected.
	s.await("higgsfield signed in · reconnected on")
	s.await("✔ connected")
	if got := s.text(); strings.Contains(got, "stdin isn't a terminal") {
		t.Fatalf("the sign-in was handed a pipe, not the terminal:\n%s", s.dump())
	}
}

// ⌃C at the sign-in's prompt is the sign-in's: it cancels that, and Wake - which
// the same two bytes would close outright once it has the terminal back - stays.
func TestCtrlCAtTheSignInCancelsItAndNotWake(t *testing.T) {
	s := openHiggsfield(t)
	s.send("\r")
	s.await("Paste the redirect URL here")
	s.send("\x03\x03")
	s.await("did not finish")
	if got := s.text(); strings.Contains(got, emergencyLine) {
		t.Fatalf("⌃C⌃C at the child's prompt fired Wake's emergency exit:\n%s", s.dump())
	}
}
