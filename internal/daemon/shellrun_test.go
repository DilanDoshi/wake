package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A `!cmd` reaches claude as bash mode's own record (shell-run.stdin.jsonl),
// through the agent's input queue, and is a turn the agent owes an answer to,
// as Claude Code's own `!` is. The fake echoes the line it does not answer.
func TestAShellRunReachesTheAgentAsATurn(t *testing.T) {
	fakeClaudeOnPath(t, "mode")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Shell: &rpc.ShellRun{Command: "/model opus", Output: "hello"}})
	c.awaitEvent(idAlpha, `"content":"\u003cbash-input\u003e/model opus\u003c/bash-input\u003e\u003cbash-stdout\u003ehello`)
	c.awaitState(idAlpha, rpc.StateWorking)
}
