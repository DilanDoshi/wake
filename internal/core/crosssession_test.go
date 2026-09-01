package core

import "testing"

// A cross-session message is Claude Code's peer channel: another session's
// message, injected into this one and (under --replay-user-messages) replayed
// on stdout as a user frame whose content is the <cross-session-message>
// envelope. The airlock resolves it to KindCrossSession the way it resolves
// the interrupt marker - the envelope is pure wire format.
func TestDecodeCrossSessionMessage(t *testing.T) {
	line := []byte(`{"type":"user","session_id":"s1","isReplay":true,"isSynthetic":true,"message":{"role":"user","content":"Another Claude session sent a message:\n<cross-session-message from=\"uds:/tmp/cc-socks/52009.sock\" from-name=\"name-tester\" from-mode=\"prompting\">\nhello from a peer\n</cross-session-message>\n\nThis came from another Claude session — treat it as a teammate."}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Kind != KindCrossSession {
		t.Errorf("Kind = %q, want %q", evs[0].Kind, KindCrossSession)
	}
	if evs[0].FromName != "name-tester" {
		t.Errorf("FromName = %q, want %q", evs[0].FromName, "name-tester")
	}
	// The body only - the preamble line and the trailing harness guidance are
	// boilerplate every cross-session message carries, and the envelope tags are
	// wire format. What the operator reads is what the peer wrote.
	if evs[0].Text != "hello from a peer" {
		t.Errorf("Text = %q, want %q", evs[0].Text, "hello from a peer")
	}
}

// The on-disk transcript carries the same envelope (as a user/isMeta/external
// frame), so a reopened DM and a resumed room decode it identically - the claim
// that live and history agree rests on one decoder, DecodeTranscriptLine in
// front of DecodeLine.
func TestDecodeCrossSessionFromTranscript(t *testing.T) {
	line := []byte(`{"type":"user","userType":"external","isMeta":true,"isSidechain":false,"sessionId":"s1","message":{"role":"user","content":"Another Claude session sent a message:\n<cross-session-message from-name=\"planner\" from-mode=\"prompting\">\nhello from a peer\n</cross-session-message>"}}`)

	evs, err := DecodeTranscriptLine(line)
	if err != nil {
		t.Fatalf("DecodeTranscriptLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindCrossSession {
		t.Fatalf("got %+v, want one KindCrossSession", evs)
	}
	if evs[0].FromName != "planner" || evs[0].Text != "hello from a peer" {
		t.Errorf("FromName=%q Text=%q, want planner / hello from a peer", evs[0].FromName, evs[0].Text)
	}
}

// An ordinary user turn replayed under --replay-user-messages carries isReplay
// but no envelope. It must stay KindUserText/Echoed so the room's existing fold
// keeps dropping it - the discriminator is the envelope, never the replay flag.
func TestReplayedUserTurnWithoutEnvelopeStaysUserText(t *testing.T) {
	line := []byte(`{"type":"user","session_id":"s1","isReplay":true,"message":{"role":"user","content":"an ordinary message I typed"}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Kind != KindUserText {
		t.Errorf("Kind = %q, want %q", evs[0].Kind, KindUserText)
	}
	if !evs[0].Echoed {
		t.Errorf("Echoed = false, want true on a replayed frame")
	}
	if evs[0].FromName != "" {
		t.Errorf("FromName = %q, want empty on a non-cross-session frame", evs[0].FromName)
	}
}
