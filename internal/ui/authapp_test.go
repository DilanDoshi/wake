package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/notice"
)

// `/login` with an argument is refused and no shell runs: Wake shows status and
// hands the sign-in command over, so `/login foo` is a shape this build does not
// have.
func TestLoginRefusesAnArgument(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)
	a := newRoomApp(t).withSize(200, 40)
	_, cmd := a.login("please")
	if cmd != nil {
		t.Errorf("/login with an argument dispatched a command; only the bare form checks status")
	}
	if notice.Count(authOnlyStatus) != 1 {
		got, _ := notice.Latest()
		t.Errorf("/login foo was not refused with the usage line; latest notice: %q", got)
	}
}

func TestBareLoginRunsTheStatusCheck(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)
	a := newRoomApp(t).withSize(200, 40)
	_, cmd := a.login("")
	if cmd == nil {
		t.Errorf("bare /login ran no status check")
	}
}

// The panel lands in the conversation that asked - the room when the room asked.
func TestAuthResultPutsThePanelInTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	a = a.authResult(authResultMsg{ID: "", Text: loggedOutJSON})
	if out := shown(a); !strings.Contains(out, authSignedOut) || !strings.Contains(out, authLoginCmd) {
		t.Errorf("the room did not show the sign-in panel:\n%s", out)
	}
}

// A result for a conversation that has since closed lands nowhere and does not
// panic - the same drop mcpResult makes for an id it no longer holds.
func TestAuthResultForAClosedConversationIsDropped(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	if got := a.authResult(authResultMsg{ID: "ghost", Text: loggedOutJSON}); len(got.dms) != len(a.dms) {
		t.Errorf("a result for an unheld conversation changed the dm set")
	}
}

// panelResult routes each subcommand result to its own fold - the one Update
// case /mcp and /login share.
func TestPanelResultRoutesEachKind(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	if out := shown(a.panelResult(authResultMsg{ID: "", Text: loggedOutJSON})); !strings.Contains(out, authSignedOut) {
		t.Errorf("panelResult did not route an auth result to the auth panel:\n%s", out)
	}
	if out := shown(a.panelResult(mcpResultMsg{ID: ""})); !strings.Contains(out, mcpNone) {
		t.Errorf("panelResult did not route an mcp result to the mcp panel:\n%s", out)
	}
}

// And into the DM when a conversation asked, with no account email even there.
func TestAuthResultPutsThePanelInTheDMThatAsked(t *testing.T) {
	a := newRoomApp(t).withAgents("sydney").withSize(200, 40).openDMWith("s1", "sydney")
	a = a.authResult(authResultMsg{ID: "s1", Text: loggedInWithPII})
	out := shown(a)
	if !strings.Contains(out, authSignedIn) {
		t.Errorf("the DM did not show the signed-in panel:\n%s", out)
	}
	if strings.Contains(out, "someone@example.com") {
		t.Errorf("the DM panel leaked the account email:\n%s", out)
	}
}
