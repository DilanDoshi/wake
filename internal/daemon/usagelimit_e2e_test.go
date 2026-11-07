package daemon

// Resuming agents a usage limit stopped, over real processes: the usagelimit
// fake hits the limit while Claude's config file names acctLimited and works on
// any other account, the way a login to another account lifts a limit.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

	// fakeOldLoginEnv is how many "continue"s after a login the fake answers as a
	// process still on the old login: the old account's limit again, or with
	// fakeOldLoginAuthEnv set, "Not logged in" (api-error-auth.jsonl's words).
	fakeOldLoginEnv     = "WAKE_FAKE_OLD_LOGIN"
	fakeOldLoginAuthEnv = "WAKE_FAKE_OLD_LOGIN_AUTH"
	notLoggedIn         = "Not logged in · Please run /login"
)

// fakeUsageLimit answers every turn by the account Claude's config file names:
// the usage limit on acctLimited, "working as <account>" on any other. "lines?"
// reports how many "continue"s arrived and how many carried the resume mark.
func fakeUsageLimit(sid string) int {
	path := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), ".claude.json")
	continues, marked, turns := 0, 0, 0
	oldLogin, _ := strconv.Atoi(os.Getenv(fakeOldLoginEnv))
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
		switch {
		case acct.ID != acctLimited && oldLogin > 0 && strings.Contains(line, `"text":"continue"`):
			oldLogin--
			if os.Getenv(fakeOldLoginAuthEnv) != "" {
				emitAPIError(sid, "authentication_failed", notLoggedIn)
			} else {
				emitAPIError(sid, "rate_limit", os.Getenv(fakeLimitTextEnv))
			}
		case limited:
			emitAPIError(sid, "rate_limit", os.Getenv(fakeLimitTextEnv))
		default:
			emitText(sid, "working as "+acct.ID)
		}
		emitResult(sid)
	}
	return 0
}

// emitAPIError fails a turn the way claude does: a synthetic assistant frame
// naming the error's kind.
func emitAPIError(sid, kind, text string) {
	fmt.Printf(`{"type":"assistant","is_api_error_message":true,"error":%q,"session_id":%q,"message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n",
		kind, sid, text)
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

// compressRetries runs a login's retries at a test's pace, still growing.
func compressRetries(t *testing.T) {
	t.Helper()
	was := loginRetries
	loginRetries = []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond}
	t.Cleanup(func() { loginRetries = was })
}

// pastTheRetries waits out every retry a login could still make.
func pastTheRetries() {
	var all time.Duration
	for _, d := range loginRetries {
		all += d
	}
	time.Sleep(4 * all)
}

// oldLoginAnswers makes the fake answer the first n "continue"s after a login as
// a process still holding the old login, by the auth failure if auth.
func oldLoginAnswers(t *testing.T, n int, auth bool) {
	t.Helper()
	t.Setenv(fakeOldLoginEnv, strconv.Itoa(n))
	if auth {
		t.Setenv(fakeOldLoginAuthEnv, "1")
	}
}

// A process still on the old login answers the login's resume with the old
// account's limit (BUG-64: six of 31 agents, 2026-11-06). The daemon tries again
// once that process has had time to take the new login, and stops once a turn
// goes through.
func TestALoginsResumeTheOldLoginAnswersIsRetried(t *testing.T) {
	compressRetries(t)
	oldLoginAnswers(t, 1, false)
	c, dir := limitedFleet(t, noResetNamed)
	id := testSessionID("e5e5")
	stopOnLimit(t, c, id, "sydney")

	signIn(t, dir, "acct-b", "2")
	c.awaitResumed(id)
	c.awaitEvent(id, noResetNamed)
	c.awaitResumed(id)
	c.awaitEvent(id, "working as acct-b")
	pastTheRetries()
	continuesSent(c, id, "continues=2 marked=2")
}

// A login's resume that reaches the process before its new login can be read
// fails "Not logged in", which is no stop at all (BUG-64, 2026-10-21): the
// daemon tries again rather than leaving the agent idle with nothing to wake it.
func TestALoginsResumeThatFindsNoLoginYetIsRetried(t *testing.T) {
	compressRetries(t)
	oldLoginAnswers(t, 1, true)
	c, dir := limitedFleet(t, noResetNamed)
	id := testSessionID("e6e6")
	stopOnLimit(t, c, id, "sydney")

	signIn(t, dir, "acct-b", "2")
	c.awaitResumed(id)
	c.awaitEvent(id, notLoggedIn)
	c.awaitResumed(id)
	c.awaitEvent(id, "working as acct-b")
	pastTheRetries()
	continuesSent(c, id, "continues=2 marked=2")
}

// A login that never takes - the old login answers every time - is tried
// loginRetries' number of times after the first, then left to its reset or the
// next login like any stop.
func TestALoginsRetriesStopAtTheirCap(t *testing.T) {
	compressRetries(t)
	oldLoginAnswers(t, 100, false)
	c, dir := limitedFleet(t, noResetNamed)
	id := testSessionID("e7e7")
	stopOnLimit(t, c, id, "sydney")

	signIn(t, dir, "acct-b", "2")
	tries := 1 + len(loginRetries)
	for range tries {
		c.awaitResumed(id)
	}
	pastTheRetries()
	continuesSent(c, id, fmt.Sprintf("continues=%d marked=%d", tries, tries))
}
