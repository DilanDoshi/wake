package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/mcp"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// fakeMCPEnv picks how this test binary behaves when run as `wake mcp`, which
// the self-test does to whatever os.Executable is - here, this binary.
const fakeMCPEnv = "WAKE_FAKE_MCP"

// The ways a `wake mcp` can fail. Empty serves the real internal/mcp.
const (
	mcpExits      = "exit"
	mcpHangs      = "hang"
	mcpOtherTools = "other-tools"
)

// fakeMCPExitSays is what the exiting server leaves on stderr, so a test can
// see the refusal carry it - and fakeMCPExitTrace below it, which it must not.
const (
	fakeMCPExitSays  = "wake: not the server you were looking for"
	fakeMCPExitTrace = "goroutine 1 [running]:"
)

// runFakeMCP is this binary as `wake mcp`.
func runFakeMCP() int {
	switch os.Getenv(fakeMCPEnv) {
	case mcpExits:
		fmt.Fprintln(os.Stderr, fakeMCPExitSays+"\n\n"+fakeMCPExitTrace)
		return 1
	case mcpHangs:
		// A grandchild holding stdout too, so the bound has to reach the group
		// and not only the process os/exec started.
		exe, err := os.Executable()
		if err != nil {
			return 1
		}
		child := exec.Command(exe)
		child.Env = append(os.Environ(), fakeLingerEnv+"=1")
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			return 1
		}
		time.Sleep(lingerFor)
		return 0
	case mcpOtherTools:
		return serveOtherTools()
	}
	if err := mcp.Serve(context.Background(), os.Stdin, os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// serveOtherTools answers like a wake from another build: the same handshake,
// one tool this build does not have.
func serveOtherTools() int {
	sc := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.ID == nil {
			continue
		}
		result := map[string]any{"tools": []any{map[string]any{"name": "stop_agent"}}}
		if req.Method == "initialize" {
			result = map[string]any{"capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "wake"}}
		}
		if enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}) != nil {
			return 1
		}
	}
	return 0
}

// mcpServerFor is the server a manager on a fresh socket would get, behaving as
// mode says.
func mcpServerFor(t *testing.T, mode string) mcpServer {
	t.Helper()
	srv, err := managerMCPServer(tempSocket(t))
	if err != nil {
		t.Fatalf("managerMCPServer: %v", err)
	}
	if mode != "" {
		srv.Env[fakeMCPEnv] = mode
	}
	return srv
}

func TestTheManagersToolsPassTheirSelfTest(t *testing.T) {
	if err := mcpServerFor(t, "").selfTest(); err != nil {
		t.Errorf("this build's own `wake mcp` failed the self-test: %v", err)
	}
}

// Each way the manager's server can be broken refuses, and says which it was.
func TestABrokenManagerServerFailsItsSelfTestAndSaysHow(t *testing.T) {
	gone := mcpServerFor(t, "")
	gone.Command = filepath.Join(t.TempDir(), "wake")

	cases := []struct {
		name string
		srv  mcpServer
		says string
	}{
		{"a binary that moved", gone, gone.Command},
		{"a binary that exits", mcpServerFor(t, mcpExits), fakeMCPExitSays},
		{"a binary from another build", mcpServerFor(t, mcpOtherTools), "⌃Q⌃Q"},
	}
	for _, c := range cases {
		err := c.srv.selfTest()
		if err == nil {
			t.Errorf("%s passed the self-test, so claude would start a manager with no tools", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: refused with %q, which does not say %q", c.name, err, c.says)
		}
	}
	if err := mcpServerFor(t, mcpExits).selfTest(); err != nil && strings.Contains(err.Error(), fakeMCPExitTrace) {
		t.Errorf("a binary that exits was refused with its whole stderr, which a notice row cannot hold: %q", err)
	}
	if err := mcpServerFor(t, mcpOtherTools).selfTest(); !errors.Is(err, mcp.ErrOtherTools) {
		t.Errorf("a binary from another build: err = %v, want it to wrap mcp.ErrOtherTools", err)
	}
}

// A server that never answers holds the spawn for the bound and no longer, even
// with a grandchild keeping its stdout open.
func TestAManagerServerThatHangsFailsItsSelfTestWithinTheBound(t *testing.T) {
	prev := mcpSelfTestTimeout
	mcpSelfTestTimeout = 200 * time.Millisecond
	t.Cleanup(func() { mcpSelfTestTimeout = prev })

	start := time.Now()
	err := mcpServerFor(t, mcpHangs).selfTest()
	took := time.Since(start)
	if err == nil {
		t.Fatal("a server that never answered passed the self-test")
	}
	if !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("refused with %q, which does not say it did not answer", err)
	}
	// Past the deadline only the group kill can end it early: without it the
	// grandchild holds stdout until WaitDelay closes the pipe.
	if took >= gitWaitDelay {
		t.Errorf("the self-test took %v: the deadline did not reach the server's group, and the spawn it runs "+
			"inside holds a client's dispatch until WaitDelay (%v) gives up", took, gitWaitDelay)
	}
}

// A manager whose tools fail the self-test is refused, its name is free for the
// next try, and an ordinary agent never runs the check.
func TestAManagerWhoseToolsFailTheirSelfTestIsRefusedAndNoOneElseIs(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	t.Setenv(fakeMCPEnv, mcpExits)

	// One wait over both outcomes, so a manager that starts anyway fails here
	// rather than as a timeout on a refusal that never comes.
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Role: rpc.RoleManager, Dir: t.TempDir()})
	f := c.await("the daemon's answer to a manager whose tools are broken", func(f rpc.Frame) bool {
		if f.Kind == rpc.FrameError && f.SessionID == idAlpha {
			return true
		}
		return f.Kind == rpc.FrameStatusReply && f.Status != nil && slices.ContainsFunc(f.Status.Sessions,
			func(s rpc.SessionStatus) bool { return s.ID == idAlpha })
	})
	if f.Kind != rpc.FrameError {
		t.Fatal("a manager whose tools failed their self-test was started: it would answer @manager with nothing to answer from")
	}
	if !strings.Contains(f.Text, "self-test") {
		t.Errorf("the manager was refused with %q, which does not say its tools failed their self-test", f.Text)
	}
	if why, started := c.spawnOutcome(idBeta, ""); !started {
		t.Errorf("an ordinary agent was refused because the manager's tools are broken: %s", why)
	}

	t.Setenv(fakeMCPEnv, "")
	spawnManager(c, idGamma)
}
