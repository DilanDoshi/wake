package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A send-now and a take-back each reach claude as the line recorded doing what
// they are for (testdata/input/midturn-now-human.stdin.jsonl,
// midturn-cancel.stdin.jsonl), through the agent's own input queue. The fake
// echoes every line it does not answer.
func TestASendNowAndARecallReachTheAgentAsRecorded(t *testing.T) {
	fakeClaudeOnPath(t, "mode")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "steer", MessageID: "m1", Now: true})
	c.awaitEvent(idAlpha, `"uuid":"m1","priority":"now","origin":{"kind":"human"}`)

	c.send(rpc.Frame{Kind: rpc.FrameRecall, SessionID: idAlpha, MessageID: "m1"})
	c.awaitEvent(idAlpha, `"subtype":"cancel_async_message","message_uuid":"m1"`)
}
