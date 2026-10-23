package daemon

// Resuming agents an unreachable API stopped, over real processes: the
// unreachable fake fails every turn as claude does when it cannot reach the API
// while its offline file names it, and works once the file is gone - the way the
// network coming back lets the next "continue" through.

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

// fakeOfflineEnv names the file that holds the sessions the fake cannot reach
// the API from: every session while it is empty, none once it is gone.
const fakeOfflineEnv = "WAKE_FAKE_OFFLINE"

// fakeUnreachable answers each turn with claude's give-up frame for an API it
// cannot reach while offline, and "working" otherwise. "lines?" reports how many
// "continue"s arrived and how many carried the reconnect mark.
func fakeUnreachable(sid string) int {
	continues, marked := 0, 0
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
			if lineReconnected(line) {
				marked++
			}
		}
		if offline(sid) {
			fmt.Printf(`{"type":"assistant","is_api_error_message":true,"error":"server_error","session_id":%q,"message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)"}]}}`+"\n", sid)
		} else {
			emitText(sid, "working")
		}
		emitResult(sid)
	}
	return 0
}

// offline reports whether the offline file holds this session, or no session.
func offline(sid string) bool {
	raw, err := os.ReadFile(os.Getenv(fakeOfflineEnv))
	return err == nil && (len(raw) == 0 || strings.Contains(string(raw), sid))
}

// lineReconnected reports whether a stdin line carries the reconnect mark.
func lineReconnected(line string) bool {
	var f struct {
		UUID string `json:"uuid"`
	}
	if json.Unmarshal([]byte(line), &f) != nil {
		return false
	}
	_, flags, ok := rpc.MessageOf(f.UUID)
	return ok && flags&rpc.MessageResumed != 0 && flags&rpc.MessageReconnected != 0
}

// goOffline cuts these sessions off the API, or every session for none.
func goOffline(t *testing.T, path string, ids ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(ids, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}

// goOnline lets every session reach the API again.
func goOnline(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// fakeNetwork stands in for the OS's network watch: each send is one change, at
// the time sent, and returns once the resume goroutine has taken it. Called
// before the daemon starts, so its cleanup runs after the daemon's.
func fakeNetwork(t *testing.T) chan<- time.Time {
	t.Helper()
	changes := make(chan time.Time)
	was := watchNetwork
	watchNetwork = func() (<-chan time.Time, func()) { return changes, func() {} }
	t.Cleanup(func() { watchNetwork = was })
	return changes
}

// offlineFleet starts a daemon whose agents cannot reach the API.
func offlineFleet(t *testing.T) (*testClient, string, chan<- time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "offline")
	goOffline(t, path)
	t.Setenv(fakeOfflineEnv, path)
	network := fakeNetwork(t)
	fakeClaudeOnPath(t, "unreachable")
	d := startDaemon(t)
	return attach(t, d.socket), path, network
}

// stopUnreachable spawns an agent and sends it a turn the API never answers.
func stopUnreachable(t *testing.T, c *testClient, id, name string) {
	t.Helper()
	c.spawn(id, name)
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: "work"})
	c.awaitEvent(id, "ENOTFOUND")
}

// awaitReconnected waits for the daemon's announcement that it resumed one
// agent an unreachable API stopped.
func (c *testClient) awaitReconnected(id string) {
	c.t.Helper()
	c.await("the reconnect resume of "+id, func(f rpc.Frame) bool {
		if f.Kind != rpc.FrameEvent || f.SessionID != id || f.Event == nil || f.Event.Notice != core.NoticeAutoResumed {
			return false
		}
		_, flags, ok := rpc.MessageOf(f.Event.MessageID)
		return ok && flags&rpc.MessageReconnected != 0
	})
}

// Another agent's turn going through once the network is back resumes every
// agent the API stopped, once each, with the reconnect mark on what it writes.
func TestAnotherAgentsTurnResumesTheAgentsTheAPIStopped(t *testing.T) {
	c, path, _ := offlineFleet(t)
	alpha, beta, gamma := testSessionID("a5a5"), testSessionID("b6b6"), testSessionID("c7c7")
	stopUnreachable(t, c, alpha, "sydney")
	stopUnreachable(t, c, beta, "maya")

	goOnline(t, path)
	c.spawn(gamma, "kai")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: gamma, Text: "work"})
	c.awaitEvent(gamma, "working")

	for _, id := range []string{alpha, beta} {
		c.awaitReconnected(id)
		c.awaitEvent(id, "working")
		continuesSent(c, id, "continues=1 marked=1")
	}
}

// The network coming back resumes an agent with nothing else running to prove
// it - the whole fleet cut off at once.
func TestTheNetworkComingBackResumesTheAgentsTheAPIStopped(t *testing.T) {
	c, path, network := offlineFleet(t)
	alpha := testSessionID("d8d8")
	stopUnreachable(t, c, alpha, "sydney")

	goOnline(t, path)
	network <- time.Now()

	c.awaitReconnected(alpha)
	c.awaitEvent(alpha, "working")
	continuesSent(c, alpha, "continues=1 marked=1")
}

// An agent another agent's turn resumed, and that fails again, waits for the
// network and is resumed by it.
func TestAnAgentThatFailsAgainAfterItsResumeIsResumedByTheNetwork(t *testing.T) {
	c, path, network := offlineFleet(t)
	alpha, beta := testSessionID("e9e9"), testSessionID("f0f0")
	stopUnreachable(t, c, alpha, "sydney")

	goOffline(t, path, alpha)
	c.spawn(beta, "maya")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: beta, Text: "work"})
	c.awaitEvent(beta, "working")
	c.awaitReconnected(alpha)
	c.awaitEvent(alpha, "ENOTFOUND")

	goOnline(t, path)
	network <- time.Now()
	c.awaitReconnected(alpha)
	c.awaitEvent(alpha, "working")
	continuesSent(c, alpha, "continues=2 marked=2")
}

// A change the kernel reported before the stop, taken by the goroutine after it,
// resumes nothing: the change is as old as the kernel says, not as the take.
func TestANetworkChangeFromBeforeTheStopResumesNothing(t *testing.T) {
	c, _, network := offlineFleet(t)
	alpha := testSessionID("a1b2")
	before := time.Now()
	stopUnreachable(t, c, alpha, "sydney")
	network <- before
	network <- before // taken only once the pass for the first has run
	continuesSent(c, alpha, "continues=0 marked=0")
}
