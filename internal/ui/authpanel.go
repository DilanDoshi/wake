package ui

// The `/login` panel: whether this machine is signed in to claude, and - when it
// is not - the command to sign in from a terminal.
//
// It is the `/mcp` panel (mcppanel.go) one subject over: `claude auth status
// --json` in place of `claude mcp list`, run through the same bounded shell
// (authapp.go), parsed here, and drawn as a transcript block. Wake never runs the
// login itself - the flow wants a TTY and a browser, which is the no-PTY
// non-negotiable - so the panel hands the command over the way mcppanel.go hands
// over `claude mcp login`.

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// authStatus is the two facts the panel shows, and deliberately only those.
// `claude auth status --json` also returns the account email, org id and org
// name; this is a public repo, so those are never decoded, displayed or stored -
// leaving them off the struct is what keeps them off the screen. Method is the
// one value drawn, and today it is a category (`claude.ai`) rather than PII.
type authStatus struct {
	LoggedIn bool   `json:"loggedIn"`
	Method   string `json:"authMethod"`
}

// parseAuthStatus reads `claude auth status --json`. ok is false when the output
// carries no JSON object - a command that failed, or a claude that answered
// something else - which the panel draws as "could not read".
//
// It starts at the line that opens the object and decodes exactly one value, so a
// CLI notice combined into the same stream by the bounded shell does not defeat
// the parse - not even one carrying a brace of its own, which a first-`{` to
// last-`}` slice would swallow. claude pretty-prints the object across lines, so
// this is line-aware rather than a per-line parse like the /mcp panel's.
func parseAuthStatus(out string) (authStatus, bool) {
	start := jsonObjectStart(out)
	if start < 0 {
		return authStatus{}, false
	}
	var st authStatus
	if err := json.NewDecoder(strings.NewReader(out[start:])).Decode(&st); err != nil {
		return authStatus{}, false
	}
	// Contain the method after unmarshalling: the bounded shell contains the raw
	// bytes, but json decodes a \uXXXX escape back into a real control character,
	// so a value carrying one would otherwise reach the panel and drive the
	// terminal. The /mcp panel avoids this by splitting plain text; the JSON path
	// has to re-contain what it decoded. LoggedIn is a bool and needs none.
	st.Method = core.Contained(st.Method)
	return st, true
}

// jsonObjectStart is the byte offset of the first line whose first non-blank
// character opens a JSON object, or -1. A notice line before the object is
// skipped whether or not it carries a brace, and the whitespace before the `{` is
// left for the decoder to skip.
func jsonObjectStart(out string) int {
	off := 0
	for _, line := range strings.SplitAfter(out, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " \t\r"), "{") {
			return off
		}
		off += len(line)
	}
	return -1
}

const (
	// authTitle heads the panel.
	authTitle = "Authentication"

	// authLoginCmd is the line somebody runs to sign in. No directory: account
	// auth is machine-global (~/.claude), unlike per-directory MCP servers, so
	// there is no `cd` to prepend the way mcpLoginCmd does.
	authLoginCmd = "claude auth login"

	// authSignInHint introduces that command when signed out, and it is a command
	// to run elsewhere rather than something Wake offers to do - the no-PTY rule,
	// the same wall mcpAuthHint hits.
	authSignInHint = "sign in from a terminal: "

	// authCheckHint is its counterpart when the status could not be read: the same
	// command, since signing in is the recovery whether the failure was a missing
	// login or a check that would not run.
	authCheckHint = "check from a terminal: "

	authSignedIn   = "Signed in"
	authSignedOut  = "Not signed in"
	authUnreadable = "Could not read authentication status"

	// authOK and authNeeds are claude's own status glyphs, the pair mcppanel.go
	// draws for the same job.
	authOK    = "✔"
	authNeeds = "!"
)

// authPanel renders the status.
func authPanel(st authStatus, ok bool, width int) string {
	switch {
	case !ok:
		return joinBlock(accentLine(authTitle, width),
			warnLine(authNeeds+" "+authUnreadable, width),
			mutedLine("  "+authCheckHint+authLoginCmd, width))
	case st.LoggedIn:
		line := authOK + " " + authSignedIn
		if m := strings.TrimSpace(st.Method); m != "" {
			line += " (" + m + ")"
		}
		return joinBlock(accentLine(authTitle, width), signedInLine(line, width))
	default:
		return joinBlock(accentLine(authTitle, width),
			warnLine(authNeeds+" "+authSignedOut, width),
			mutedLine("  "+authSignInHint+authLoginCmd, width))
	}
}

// signedInLine is the one green row this panel draws; there is no successStyle
// helper, so it is spelled here the way mcpRowLine spells its own.
func signedInLine(s string, width int) string {
	return lipgloss.NewStyle().Foreground(Success).MaxWidth(width).Render(s)
}
