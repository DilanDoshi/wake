package daemon

// Resuming agents a usage limit stopped, over real processes: the usagelimit
// fake hits the limit while Claude's config file names acctLimited and works on
// any other account, the way a login to another account lifts a limit.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	acctLimited = "acct-limited"

	// fakeLimitTextEnv is the usage-limit message the fake fails a turn with.
	fakeLimitTextEnv = "WAKE_FAKE_LIMIT_TEXT"
	// fakeLimitOnceEnv makes the fake fail only its first turn, as an account
	// whose limit has since reset does.
	fakeLimitOnceEnv = "WAKE_FAKE_LIMIT_ONCE"

	// noResetNamed is a limit message naming no reset, so no timer is armed.
	noResetNamed = "You've hit your session limit"
)

// fakeUsageLimit answers every turn by the account Claude's config file names:
// the usage limit on acctLimited, "working as <account>" on any other. "lines?"
// reports how many "continue"s arrived and how many carried the resume mark.
func fakeUsageLimit(sid string) int {
	path := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), ".claude.json")
	continues, marked, turns := 0, 0, 0
	for line := range stdinLines() {
		switch {
		case strings.Contains(line, `"type":"control_request"`):
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q}}`+"\n", controlRequestID(line))
			continue
		case strings.Contains(line, `"text":"lines?"`):
			emitText(sid, fmt.Sprintf("continues=%d marked=%d", continues, marked))
			emitResult(sid)
			continue
		case strings.Contains(line, `"text":"continue"`):
			continues++
			if lineResumed(line) {
				marked++
			}
		}
		turns++
		raw, _ := os.ReadFile(path)
		acct, _ := core.DecodeAccount(raw)
		limited := acct.ID == acctLimited && (os.Getenv(fakeLimitOnceEnv) == "" || turns == 1)
		if limited {
			fmt.Printf(`{"type":"assistant","is_api_error_message":true,"error":"rate_limit","session_id":%q,"message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n",
				sid, os.Getenv(fakeLimitTextEnv))
		} else {
			emitText(sid, "working as "+acct.ID)
		}
		emitResult(sid)
	}
	return 0
}

// lineResumed reports whether a stdin line carries the daemon's resume mark.
func lineResumed(line string) bool {
	var f struct {
		UUID string `json:"uuid"`
	}
	if json.Unmarshal([]byte(line), &f) != nil {
		return false
	}
	_, flags, ok := rpc.MessageOf(f.UUID)
	return ok && flags&rpc.MessageResumed != 0
}

// signIn writes Claude's config file the way claude does, by replacing it.
func signIn(t *testing.T, dir, account, extra string) {
	t.Helper()
	body := fmt.Sprintf(`{"numStartups":%q,"oauthAccount":{"accountUuid":%q,"organizationUuid":"org-1"}}`, extra, account)
	tmp := filepath.Join(dir, ".claude.json.tmp")
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, ".claude.json")); err != nil {
		t.Fatal(err)
	}
}

// limitedFleet starts a daemon whose agents run as the limited account.
func limitedFleet(t *testing.T, limitText string) (*testClient, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv(fakeLimitTextEnv, limitText)
	signIn(t, dir, acctLimited, "1")
	fakeClaudeOnPath(t, "usagelimit")
	d := startDaemon(t)
	return attach(t, d.socket), dir
}

// stopOnLimit spawns an agent and sends it a turn the limit stops.
func stopOnLimit(t *testing.T, c *testClient, id, name string) {
	t.Helper()
	c.spawn(id, name)
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: "work"})
	c.awaitEvent(id, "session limit")
}

// awaitResumed waits for the daemon's announcement that it resumed one agent.
func (c *testClient) awaitResumed(id string) {
	c.t.Helper()
	c.await("the auto-resume of "+id, func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == id && f.Event != nil &&
			f.Event.Notice == core.NoticeAutoResumed && f.Event.Kind == core.KindUserText
	})
}

// continuesSent asks the fake how many "continue"s reached it.
func continuesSent(c *testClient, id, want string) {
	c.t.Helper()
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: "lines?"})
	c.awaitEvent(id, want)
}

// A login to another account in some other session resumes every agent the
// limit stopped, once each, with the resume mark on what it writes.
func TestALoginToAnotherAccountResumesTheAgentsALimitStopped(t *testing.T) {
	c, dir := limitedFleet(t, noResetNamed)
	alpha, beta := testSessionID("a1a1"), testSessionID("b2b2")
	stopOnLimit(t, c, alpha, "sydney")
	stopOnLimit(t, c, beta, "maya")

	signIn(t, dir, "acct-b", "2")

	for _, id := range []string{alpha, beta} {
		c.awaitResumed(id)
		c.awaitEvent(id, "working as acct-b")
		continuesSent(c, id, "continues=1 marked=1")
	}
}

// A rewrite of the same account - claude touching its own config - resumes
// nothing: had it, the resume would have failed and been stopped again, and the
// later login would have written a second "continue".
func TestARewriteOfTheSameAccountResumesNothing(t *testing.T) {
	c, dir := limitedFleet(t, noResetNamed)
	id := testSessionID("c3c3")
	stopOnLimit(t, c, id, "sydney")

	signIn(t, dir, acctLimited, "2")
	signIn(t, dir, acctLimited, "3")
	signIn(t, dir, "acct-b", "4")

	c.awaitResumed(id)
	c.awaitEvent(id, "working as acct-b")
	continuesSent(c, id, "continues=1 marked=1")
}

// The reset the limit names resumes it with no login at all, once.
func TestTheResetTheLimitNamesResumesIt(t *testing.T) {
	compressReset(t)
	t.Setenv(fakeLimitOnceEnv, "1")
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}
	reset := time.Now().In(la).Add(2 * time.Minute)
	c, _ := limitedFleet(t, "You've hit your session limit · resets "+strings.ToLower(reset.Format("3:04PM"))+" (America/Los_Angeles)")
	id := testSessionID("d4d4")
	stopOnLimit(t, c, id, "sydney")

	c.awaitResumed(id)
	c.awaitEvent(id, "working as "+acctLimited)
	continuesSent(c, id, "continues=1 marked=1")
}

// compressReset moves the resume goroutine's clock a day on, past any reset.
func compressReset(t *testing.T) {
	t.Helper()
	was := limitClock
	limitClock = func() time.Time { return time.Now().Add(24 * time.Hour) }
	t.Cleanup(func() { limitClock = was })
}
