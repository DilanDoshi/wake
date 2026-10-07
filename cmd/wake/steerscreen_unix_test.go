//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// scriptSteers keeps its first turn open and queues every line written into it,
// as claude does (docs/superpowers/notes/2026-10-02-mid-turn-delivery-findings.md):
// a cancel_async_message gives a queued line back, and a line sent now is read
// at once and ends the turn.
const scriptSteers = "steers"

func fakeAgentSteers(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)
	running := false
	var queued []string
	for line := range agentStdin() {
		var f struct {
			Type      string `json:"type"`
			UUID      string `json:"uuid"`
			Priority  string `json:"priority"`
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype     string `json:"subtype"`
				MessageUUID string `json:"message_uuid"`
			} `json:"request"`
		}
		if json.Unmarshal([]byte(line), &f) != nil {
			continue
		}
		text, _ := userTextOf(line)
		switch {
		case f.Request.Subtype == "cancel_async_message":
			i := slices.Index(queued, f.Request.MessageUUID)
			if i >= 0 {
				queued = slices.Delete(queued, i, i+1)
				sayLifecycle(sid, f.Request.MessageUUID, "cancelled")
			}
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"cancelled":%t}}}`+"\n", f.RequestID, i >= 0)
		case f.Type != "user":
		case !running:
			running = true
			sayLifecycle(sid, f.UUID, "started")
			sayText(sid, workingMarker+": "+text)
		case f.Priority == "now":
			sayLifecycle(sid, f.UUID, "started")
			sayText(sid, heardPrefix+text)
			sayResult(sid)
			running = false
		default:
			sayLifecycle(sid, f.UUID, "queued")
			queued = append(queued, f.UUID)
		}
	}
	return 0
}

func sayLifecycle(sid, msgID, state string) {
	fmt.Printf(`{"type":"command_lifecycle","command_uuid":%q,"state":%q,"session_id":%q}`+"\n", msgID, state, sid)
}

// A message typed while the agent works is pinned, and ↑ - the real arrow's
// bytes off a pty - takes it back into the draft.
func TestUpTakesAQueuedMessageBackOnScreen(t *testing.T) {
	withScriptedAgent(t, scriptSteers)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	s.send("start\r")
	s.await(workingMarker + ": start")
	s.send("steer me\r")
	s.await("⧗ steer me")

	s.send("\x1b[A")
	s.awaitGone("⧗")
	if !strings.Contains(s.text(), "steer me") {
		t.Fatalf("the taken-back message is not in the draft.\n%s", s.dump())
	}
}

// ⌃]'s byte (GS) off a pty sends the draft now: the agent reads it at once, in
// the turn it is still running.
func TestCtrlCloseBracketSendsNowOnScreen(t *testing.T) {
	withScriptedAgent(t, scriptSteers)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	s.send("start\r")
	s.await(workingMarker + ": start")
	s.send("change course\x1d")
	s.await(heardPrefix + "change course")
}
