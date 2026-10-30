package ui

// A peer's cross-session message folds past peerFoldRows, in the room and in a
// conversation, and the pointer under it is the body's own Muted so a fold reads
// as one colour. ⌃E opens every fold; a click opens one - the room's by its line
// id (roomexpand.go), a conversation's by peerKey through openTool.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	// peerFoldRows is how many rows of a peer's message show before it folds -
	// enough to read what is being asked, not the whole brief another session wrote.
	peerFoldRows = 3

	// peerFoldFormat is the pointer under a fold; %s is the rows it hides.
	peerFoldFormat = "⤷ +%s · " + expandKey + " or click to expand"

	// peerKeyPrefix keeps a peer message's click key apart from a tool_use id.
	peerKeyPrefix = "peer:"

	// peerKeyBytes is how much of the digest is kept: far more than the messages
	// of one conversation can collide in.
	peerKeyBytes = 8

	// sentKeyLead marks a send's recipient in its key, so it never collides with
	// an incoming message from a session of the same name.
	sentKeyLead = "→"

	// sentArrow is what a delivery receipt puts before the recipient.
	sentArrow = "→ "
)

// peerFolds reports whether a peer's message draws as a pointer at this width.
func peerFolds(text string, width int) bool {
	return renderedRows(crossSessionBody(text, width)) > peerFoldRows
}

// peerBody is a peer's message as drawn: whole when it fits peerFoldRows or is
// expanded, else its first peerFoldRows rows and a pointer saying how to open it.
func peerBody(text string, width int, expanded bool) string {
	body := crossSessionBody(text, width)
	rows := renderedRows(body)
	if expanded || rows <= peerFoldRows {
		return body
	}
	return foldWithPointer(body, fmt.Sprintf(peerFoldFormat, plural(rows-peerFoldRows, "line")), width, peerFoldRows)
}

// peerKey is what a click on a conversation's folded peer message resolves to. A
// live message carries no id of its own, so the key is its sender and words:
// the same words twice open together, which is the same message to read. A send
// is keyed by its recipient and words, apart from an incoming one's.
func peerKey(ev core.Event) string {
	from, text := ev.FromName, ev.Text
	if s := sentOf(ev); s != nil {
		from, text = sentKeyLead+s.To, s.Text
	}
	sum := sha256.Sum256([]byte(from + "\x00" + text))
	return peerKeyPrefix + hex.EncodeToString(sum[:peerKeyBytes])
}

// sentOf is the message one of this conversation's own SendMessage calls carries,
// and nil for any other event - a subagent's send is its own, in its own view.
func sentOf(ev core.Event) *core.PeerSend {
	if ev.Kind != core.KindToolUse || ev.Tool == nil || ev.Subagent != nil {
		return nil
	}
	return ev.Tool.Send
}

// delivered reports whether text is the delivery receipt of a send to s.To, in the
// shape its address form gets: `“summary” → <name> (…)` for a name
// (manager-relay.jsonl), `“summary” → uds:<socket>; …` for a socket
// (send-by-address.jsonl).
func delivered(s *core.PeerSend, text string) bool {
	return s != nil && (strings.Contains(text, sentArrow+s.To+" (") || strings.Contains(text, sentArrow+s.To+"; "))
}
