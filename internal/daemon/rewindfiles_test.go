package daemon

// Restoring an agent's files: the daemon's half of FrameRewindPreview,
// FrameRewindFiles and FrameRewindBoth, over the real-process harness.
// Session.RewindFiles and the airlock have their own tests in internal/core.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// fakeRewind answers rewind_files and rewind_conversation the way 2.1.288 does
// (rewind-files.jsonl, rewind-files-both.jsonl), and echoes every other line.
// A restore aimed at "FAIL" is refused with the bare error claude sends.
func fakeRewind(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	for line := range stdinLines() {
		id := controlRequestID(line)
		switch {
		case strings.Contains(line, `"subtype":"rewind_files"`) && strings.Contains(line, `"dry_run":true`):
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"canRewind":true,"filesChanged":["/p/a.txt"],"insertions":1,"deletions":2}}}`+"\n", id)
		case strings.Contains(line, `"subtype":"rewind_files"`) && strings.Contains(line, `"FAIL"`):
			fmt.Printf(`{"type":"control_response","response":{"subtype":"error","request_id":%q,"error":"No file checkpoint found for this message."}}`+"\n", id)
		case strings.Contains(line, `"subtype":"rewind_files"`):
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"canRewind":true,"skippedLinks":0}}}`+"\n", id)
		case strings.Contains(line, `"subtype":"rewind_conversation"`):
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"rewound":true,"targetMessageUuid":"T","prefillText":"again","precedingAssistantUuid":"A","error":null}}}`+"\n", id)
		default:
			emitText(sid, "echo: "+line)
			emitResult(sid)
		}
	}
	return 0
}

func isFilesReceipt(f rpc.Frame) bool {
	return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindFilesRewindReceipt
}

func isConversationReceipt(f rpc.Frame) bool {
	return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindRewindReceipt
}

// A preview goes to the window that asked and no other - the MCP answer's
// rule: two windows previewing at once would each take the other's.
func TestARewindPreviewGoesOnlyToTheWindowThatAsked(t *testing.T) {
	fakeClaudeOnPath(t, "rewind")
	d := startDaemon(t)
	asker := attach(t, d.socket)
	other := attach(t, d.socket)
	asker.spawn(idAlpha, "sydney")
	asker.awaitEvent(idAlpha, "ready")
	other.awaitEvent(idAlpha, "ready")

	asker.send(rpc.Frame{Kind: rpc.FrameRewindPreview, SessionID: idAlpha, RewindTarget: "T"})
	got := asker.await("its preview", isFilesReceipt).Event.Files
	if got == nil || !got.Preview || got.Target != "T" || len(got.Files) != 1 || got.Deletions != 2 {
		t.Fatalf("preview = %+v, want T's one file, labelled a preview", got)
	}

	asker.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "after"})
	other.awaitEvent(idAlpha, "after")
	for _, f := range other.seen {
		if isFilesReceipt(f) {
			t.Fatalf("another window received the asker's preview: %+v", f.Event.Files)
		}
	}
}

// A code restore is a restore, never a preview, and every window hears it.
func TestRewindFilesRestoresAndEveryWindowHearsIt(t *testing.T) {
	fakeClaudeOnPath(t, "rewind")
	d := startDaemon(t)
	asker := attach(t, d.socket)
	other := attach(t, d.socket)
	asker.spawn(idAlpha, "sydney")
	asker.awaitEvent(idAlpha, "ready")
	other.awaitEvent(idAlpha, "ready")

	asker.send(rpc.Frame{Kind: rpc.FrameRewindFiles, SessionID: idAlpha, RewindTarget: "T"})
	for _, c := range []*testClient{asker, other} {
		got := c.await("the restore's receipt", isFilesReceipt).Event.Files
		if got == nil || got.Preview || got.Both || !got.Restorable || got.Target != "T" {
			t.Fatalf("receipt = %+v, want T restored", got)
		}
	}
	for _, f := range asker.seen {
		if isConversationReceipt(f) {
			t.Fatal("a code restore rewound the conversation too")
		}
	}
}

// Both restores the files first and rewinds the conversation only on the
// restore's success, so the conversation receipt follows the files one.
func TestRewindBothRewindsTheConversationAfterTheRestore(t *testing.T) {
	fakeClaudeOnPath(t, "rewind")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameRewindBoth, SessionID: idAlpha, RewindTarget: "T", RewindLastSeen: "S"})
	files := c.await("the restore's receipt", isFilesReceipt).Event.Files
	if files == nil || !files.Both || !files.Restorable {
		t.Fatalf("receipt = %+v, want the code half of both, restored", files)
	}
	conv := c.await("the conversation rewind's receipt", isConversationReceipt).Event.Rewind
	if conv == nil || !conv.Rewound {
		t.Fatalf("rewind receipt = %+v, want rewound", conv)
	}
}

// A failed restore leaves the conversation alone: nothing more is written.
func TestRewindBothLeavesTheConversationWhenTheRestoreFails(t *testing.T) {
	fakeClaudeOnPath(t, "rewind")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameRewindBoth, SessionID: idAlpha, RewindTarget: "FAIL", RewindLastSeen: "S"})
	files := c.await("the refusal", isFilesReceipt).Event.Files
	if files == nil || !files.Both || files.Restorable || files.Error == "" {
		t.Fatalf("receipt = %+v, want a refused restore carrying claude's reason", files)
	}
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "after"})
	c.awaitEvent(idAlpha, "after")
	for _, f := range c.seen {
		if isConversationReceipt(f) {
			t.Fatal("the conversation was rewound over a restore that failed")
		}
	}
}

// The three writes are refused while the session is stopped on a permission
// ask, FrameRewind's own guard; a malformed frame is refused before stdin.
func TestTheFileRewindsAreRefusedWhileBlockedOrMalformed(t *testing.T) {
	fakeClaudeOnPath(t, "ask")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.await("the permission ask", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindPermissionRequest
	})
	for _, f := range []rpc.Frame{
		{Kind: rpc.FrameRewindFiles, SessionID: idAlpha, RewindTarget: "T"},
		{Kind: rpc.FrameRewindBoth, SessionID: idAlpha, RewindTarget: "T", RewindLastSeen: "S"},
	} {
		c.send(f)
		if got := c.await("a refusal", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError }); !strings.Contains(got.Text, "permission request") {
			t.Errorf("%s: error = %q, want it to name the outstanding ask", f.Kind, got.Text)
		}
	}
	if got := stateOf(c.status(), idAlpha); got != rpc.StateBlocked {
		t.Errorf("after the refusals the session reports %q, want %q", got, rpc.StateBlocked)
	}
}

func TestAFileRewindWithoutItsUUIDsIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "rewind")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	for _, f := range []rpc.Frame{
		{Kind: rpc.FrameRewindPreview, SessionID: idAlpha},
		{Kind: rpc.FrameRewindFiles, SessionID: idAlpha},
		{Kind: rpc.FrameRewindBoth, SessionID: idAlpha, RewindTarget: "T"},
	} {
		c.send(f)
		c.await(f.Kind+"'s refusal", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	}
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "after"})
	c.awaitEvent(idAlpha, "after")
	for _, f := range c.seen {
		if isFilesReceipt(f) || isConversationReceipt(f) {
			t.Fatalf("a refused frame still reached stdin: %+v", f.Event)
		}
	}
}
