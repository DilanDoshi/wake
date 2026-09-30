package daemon

// The manager's startup self-test: before claude is handed mcp.json, run the
// command it names, ask initialize and tools/list, and refuse the launch unless
// this build's tools come back. It closes Wake's half of a manager whose tools
// are silently absent - a binary that moved, one that is not wake, one replaced
// by another build - and not claude's: whether claude accepts the handshake is
// still docs/live-testing.md §13.1.
//
// It runs inside launch, on the dispatch goroutine of the client that asked,
// and stays inline so a refusal is enqueued ahead of any FrameStatus written
// behind the spawn (cmd/wake's act reads that order as "taken") - which is why
// the bound is load-bearing. It holds from the moment the process exists: the
// exec itself is the kernel's, as it is for the StartObserved every launch makes
// of this same binary a few lines later. It is safe there because internal/mcp
// answers these two requests without its Fleet: the server never dials this
// socket, so it opens no client and cannot move the daemon's client count.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/mcp"
)

// mcpSelfTestTimeout bounds the exchange. A `wake mcp` answers in milliseconds,
// so anything slower is the failure being looked for. A var only so a test can
// shorten it, gitTimeout's seam.
var mcpSelfTestTimeout = 2 * time.Second

// selfTest runs srv the way claude will and checks it serves this build's tools.
func (srv mcpServer) selfTest() error {
	run := "`" + strings.Join(append([]string{srv.Command}, srv.Args...), " ") + "`"
	refuse := func(err error) error {
		return fmt.Errorf("refusing to start the %s: its tools (%s) failed their self-test: %w", core.ManagerName, run, err)
	}
	requests, err := mcp.SelfTestRequests()
	if err != nil {
		return refuse(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpSelfTestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, srv.Command, srv.Args...)
	cmd.Env = os.Environ()
	for k, v := range srv.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdin = bytes.NewReader(requests)
	// git's two bounds, for git's two failures (worktree.go): the group lets the
	// deadline reach anything the server started, WaitDelay a pipe it left open.
	cmd.WaitDelay = gitWaitDelay
	worktreeSetGroup(cmd)
	cmd.Cancel = func() error { return worktreeKillGroup(cmd) }

	out, err := cmd.Output()
	switch {
	case err != nil && ctx.Err() != nil:
		return refuse(fmt.Errorf("it did not answer within %v", mcpSelfTestTimeout))
	case err != nil:
		return refuse(ranWith(err))
	}
	if err := mcp.CheckSelfTest(out); err != nil {
		if errors.Is(err, mcp.ErrOtherTools) {
			err = fmt.Errorf("%w; the binary was replaced while this fleet's daemon kept running: "+
				"⌃Q⌃Q the fleet, then reopen it, to run it on one build", err)
		}
		return refuse(err)
	}
	return nil
}

// ranWith is a failed run with the first line of the server's stderr, when it
// said anything: "exit status 1" alone is nothing an operator can act on, and a
// panic's trace is more than a notice row can hold.
func ranWith(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if said, _, _ := strings.Cut(strings.TrimSpace(string(exit.Stderr)), "\n"); said != "" {
			return fmt.Errorf("%w: %s", err, said)
		}
	}
	return err
}
