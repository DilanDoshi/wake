package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const (
	// loggedInWithPII is a signed-in status carrying the account fields claude
	// also returns. The panel must show none of them - this is a public repo - so
	// the email, org id and org name are here precisely so a test can prove they
	// never reach the screen.
	loggedInWithPII = `{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty",` +
		`"email":"someone@example.com","orgId":"org-1234","orgName":"Someone's Org","subscriptionType":"max"}`

	// loggedOutJSON is the shape `claude auth status --json` returns when nobody
	// is signed in.
	loggedOutJSON = `{"loggedIn":false,"authMethod":null}`
)

func TestParseAuthStatusReadsSignedIn(t *testing.T) {
	st, ok := parseAuthStatus(loggedInWithPII)
	if !ok || !st.LoggedIn {
		t.Fatalf("a signed-in status parsed loggedIn=%v ok=%v", st.LoggedIn, ok)
	}
	if st.Method != "claude.ai" {
		t.Errorf("the auth method parsed as %q, want claude.ai", st.Method)
	}
}

func TestParseAuthStatusReadsSignedOut(t *testing.T) {
	st, ok := parseAuthStatus(loggedOutJSON)
	if !ok {
		t.Fatalf("a well-formed status did not parse")
	}
	if st.LoggedIn {
		t.Errorf("loggedIn:false parsed as signed in")
	}
}

func TestParseAuthStatusRejectsNonJSON(t *testing.T) {
	for _, out := range []string{"", "claude: command not found", "   ", "not json at all", "{oops not json}"} {
		if _, ok := parseAuthStatus(out); ok {
			t.Errorf("%q parsed as a status, want the refusal the panel draws as \"could not read\"", out)
		}
	}
}

// A CLI notice combined into the same stream by the bounded shell must not
// defeat the parse - even one carrying a brace of its own, which a first-`{`
// to last-`}` slice would swallow. claude pretty-prints the object across lines,
// so the parse starts at the line that opens it and reads exactly that one value.
func TestParseAuthStatusSkipsSurroundingNotices(t *testing.T) {
	pretty := "{\n  \"loggedIn\": true,\n  \"authMethod\": \"claude.ai\"\n}"
	for name, out := range map[string]string{
		"plain multiline":       pretty,
		"leading brace notice":  "warning: config {legacy} in use\n" + pretty,
		"trailing brace notice": pretty + "\nnotice: update {available} soon",
		"both":                  "warning {x}\n" + pretty + "\ndone {y}",
	} {
		st, ok := parseAuthStatus(out)
		if !ok || !st.LoggedIn || st.Method != "claude.ai" {
			t.Errorf("%s: parsed ok=%v loggedIn=%v method=%q, want a signed-in claude.ai read",
				name, ok, st.LoggedIn, st.Method)
		}
	}
}

// The signed-in panel says so and shows the auth method, and it shows none of
// the account PII the same status carries, and it does not tell a signed-in user
// to sign in.
func TestTheSignedInPanelNeverShowsAccountPII(t *testing.T) {
	st, ok := parseAuthStatus(loggedInWithPII)
	panel := authPanel(st, ok, 120)
	if !strings.Contains(panel, authSignedIn) {
		t.Errorf("a signed-in panel does not say so:\n%s", panel)
	}
	for _, secret := range []string{"someone@example.com", "example.com", "org-1234", "Someone's Org"} {
		if strings.Contains(panel, secret) {
			t.Errorf("the panel leaks account PII (%q) onto a public-repo surface:\n%s", secret, panel)
		}
	}
	if strings.Contains(panel, authLoginCmd) {
		t.Errorf("a signed-in panel offers the sign-in command:\n%s", panel)
	}
}

func TestASignedOutPanelSaysHowToSignIn(t *testing.T) {
	st, ok := parseAuthStatus(loggedOutJSON)
	panel := authPanel(st, ok, 120)
	if !strings.Contains(panel, authSignedOut) {
		t.Errorf("a signed-out panel does not say so:\n%s", panel)
	}
	if !strings.Contains(panel, authLoginCmd) {
		t.Errorf("a signed-out panel does not name the sign-in command:\n%s", panel)
	}
}

func TestAnUnreadablePanelSaysSoAndOffersTheCommand(t *testing.T) {
	panel := authPanel(authStatus{}, false, 120)
	if !strings.Contains(panel, authUnreadable) {
		t.Errorf("an unreadable status is not admitted:\n%s", panel)
	}
	if !strings.Contains(panel, authLoginCmd) {
		t.Errorf("an unreadable panel offers no way forward:\n%s", panel)
	}
}

// The hint row names a command, so a panel narrower than it cuts it with a
// mark: cut bare at the edge, "sign in from a terminal: claude auth login" reads
// as "claude" - a complete command, and the wrong one.
func TestANarrowPanelMarksItsCutCommand(t *testing.T) {
	signedOut, ok := parseAuthStatus(loggedOutJSON)
	for _, c := range []struct {
		st   authStatus
		ok   bool
		hint string
	}{
		{signedOut, ok, authSignInHint},
		{authStatus{}, false, authCheckHint},
	} {
		full := "  " + c.hint + authLoginCmd
		width := ansi.StringWidth(full) - len(" auth login") // the old cut ended on "claude"
		row := panelRow(t, authPanel(c.st, c.ok, width), c.hint)
		assertCutAt(t, row, full, width)
		if wide := panelRow(t, authPanel(c.st, c.ok, 120), c.hint); wide != full {
			t.Errorf("a hint the panel holds was changed:\n got %q\nwant %q", wide, full)
		}
	}
}

// panelRow is the panel's row that starts with lead, as drawn.
func panelRow(t *testing.T, panel, lead string) string {
	t.Helper()
	for _, row := range strings.Split(stripANSI(panel), "\n") {
		if strings.HasPrefix(strings.TrimSpace(row), strings.TrimSpace(lead)) {
			return row
		}
	}
	t.Fatalf("no row starting %q in the panel:\n%s", lead, panel)
	return ""
}

// The status is claude's own output, so a value carrying a terminal escape
// cannot drive the terminal - the /mcp panel's BUG-9 rule, one subcommand over.
//
// The threat the JSON path has that /mcp's plain-text one does not: the bounded
// shell contains the raw bytes, but a `\u001b` escape survives that as printable
// text and json then decodes it back into a live ESC. So the fixture carries the
// escape, not a raw byte, and the containment under test is the one after
// unmarshalling.
func TestAStatusCannotDriveTheTerminal(t *testing.T) {
	dir := t.TempDir()
	// A JSON \u001b escape inside the method value - a raw byte would be blanked
	// before the parse and prove nothing about the decode.
	hostile := `{"loggedIn":true,"authMethod":"claude\u001b[2Jai"}`
	if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte(hostile), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	st, ok := parseAuthStatus(bangRunWithin(t, bangTestLimit, dir, "cat status.json", bangTimeout, bangWaitDelay))
	if !ok || !strings.Contains(st.Method, "claude") {
		t.Fatalf("the hostile status did not parse, so this asserted nothing: ok=%v method=%q", ok, st.Method)
	}
	panel := authPanel(st, ok, 120)
	if i := strings.IndexFunc(panel, actsOnATerminal); i >= 0 {
		t.Errorf("the panel keeps a character a terminal acts on at %d:\n%q", i, panel)
	}
}
