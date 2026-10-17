package ui

// The daemon resumes an agent a usage limit stopped with a "continue" of its
// own (internal/daemon/usagelimit.go), announced as core.NoticeAutoResumed and
// marked on disk. These hold that every surface draws it as Wake's line - never
// as the operator's words - live and restored, and that the limit's pin says
// Wake will do it.

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// resumeEvent is the daemon's announcement that it resumed a session.
func resumeEvent(sessionID string, at time.Time) core.Event {
	return core.Event{Kind: core.KindUserText, SessionID: sessionID, Notice: core.NoticeAutoResumed, Text: "continue", At: at}
}

func resumeFrame(sessionID string) rpc.Frame {
	ev := resumeEvent(sessionID, time.Time{})
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: sessionID, Event: &ev}
}

func TestTheUsageLimitPinSaysWakeResumesIt(t *testing.T) {
	pin := twoAgents(t).apply(usageLimitFrame("s1")).pinnedNotice()
	if !strings.Contains(pin, "resets 9:50pm") || !strings.Contains(pin, "Wake resumes it") || strings.Contains(pin, "send again") {
		t.Errorf("the usage-limit pin should name the reset and say Wake resumes it: %q", pin)
	}
}

// Live in the conversation: Wake's line, and never a prompt ↑ recalls.
func TestAnAutoResumeIsWakesLineInTheConversation(t *testing.T) {
	a := twoAgents(t).apply(resumeFrame("s1"))
	view := shown(a)
	if !strings.Contains(view, autoResumedLabel) {
		t.Errorf("the conversation drew no auto-resume line:\n%s", view)
	}
	if prompts := promptsIn([]core.Event{resumeEvent("s1", time.Time{})}); len(prompts) != 0 {
		t.Errorf("↑ would recall Wake's resume as the operator's prompt: %q", prompts)
	}
}

// Live in the room: Wake's line naming the agent, not a "continue" the
// operator never typed.
func TestAnAutoResumeIsWakesLineInTheRoom(t *testing.T) {
	m := roomWithHistoryAsked(t).apply(resumeFrame("s1"))
	view := shown(m)
	if !strings.Contains(view, "sydney"+autoResumedTail) {
		t.Errorf("the room drew no auto-resume line for sydney:\n%s", view)
	}
	if strings.Contains(view, "continue") {
		t.Errorf("the room drew Wake's \"continue\" as words:\n%s", view)
	}
}

// Restored: two agents resumed in the same second are two Wake lines, not one
// operator broadcast of "continue" read out of two transcripts.
func TestRestoredResumesAreWakesLinesNotABroadcast(t *testing.T) {
	a := roomWithHistoryAsked(t)
	long := clock().Add(-time.Hour)
	m := a.apply(oneReply("s1", resumeEvent("s1", long)))
	m = m.apply(oneReply("s2", resumeEvent("s2", long.Add(40*time.Millisecond))))
	view := shown(m)
	for _, name := range []string{"sydney", "john"} {
		if !strings.Contains(view, name+autoResumedTail) {
			t.Errorf("the restored room drew no auto-resume line for %s:\n%s", name, view)
		}
	}
	if strings.Contains(view, "continue") {
		t.Errorf("the restored room drew Wake's \"continue\" as words:\n%s", view)
	}
}
