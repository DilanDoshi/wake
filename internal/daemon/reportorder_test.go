package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A report is queued in the order it was built. Two pushes racing used to
// enqueue a stale snapshot behind a newer one, so a client read a session that
// had left the fleet as live again - which is what an API-failure pin and its
// auto-wake read as a resume (docs/notes/deferred.md, 2026-09-24).
//
// The barrier is alpha's own mutex rather than timing: holding it parks the
// older push inside its snapshot of alpha, after it listed the fleet.
func TestAStatusReportIsNeverQueuedBehindANewerOne(t *testing.T) {
	s := newServer(tempSocket(t))
	a := newAgent(idAlpha, "sydney", "dev-5748", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	if !s.register(a) {
		t.Fatal("register refused an id nothing else holds")
	}
	c := newClient(nil)
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()

	a.mu.Lock()
	older := make(chan struct{})
	go func() { defer close(older); s.pushStatus() }()
	waitParkedIn(t, "(*agent).snapshot", "")

	// alpha leaves the fleet, and a newer report says so.
	s.mu.Lock()
	delete(s.agents, idAlpha)
	s.mu.Unlock()
	newer := make(chan struct{})
	go func() { defer close(newer); s.pushStatus() }()
	waitDoneOrParkedIn(t, newer, "pushStatus", "(*agent).snapshot")

	a.mu.Unlock()
	<-older
	<-newer

	var last rpc.Frame
	for len(c.out) > 0 {
		last = <-c.out
	}
	if rows := statusRows(last, idAlpha); rows != 0 {
		t.Fatal("the last report a client holds lists alpha, which had left the fleet: the older snapshot was queued behind the newer one")
	}
}

// waitParkedIn waits for a goroutine parked on a mutex with fn on its stack and
// not on it.
func waitParkedIn(t *testing.T, fn, not string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !parkedIn(fn, not) {
		if time.Now().After(deadline) {
			t.Fatalf("no goroutine parked in %s\n%s", fn, allStacks())
		}
		time.Sleep(time.Millisecond)
	}
}

// waitDoneOrParkedIn waits for done to close, or for a goroutine parked in fn.
func waitDoneOrParkedIn(t *testing.T, done <-chan struct{}, fn, not string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		select {
		case <-done:
			return
		default:
		}
		if parkedIn(fn, not) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("neither done nor parked in %s\n%s", fn, allStacks())
		}
		time.Sleep(time.Millisecond)
	}
}

func parkedIn(fn, not string) bool {
	for _, g := range strings.Split(allStacks(), "\n\n") {
		head, _, _ := strings.Cut(g, "\n")
		if strings.Contains(head, "[sync.Mutex.Lock") && strings.Contains(g, fn) && (not == "" || !strings.Contains(g, not)) {
			return true
		}
	}
	return false
}
