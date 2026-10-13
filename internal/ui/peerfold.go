package ui

// A peer's cross-session message folds past peerFoldRows, in the room and in a
// conversation, and the pointer under it is the body's own Muted so a fold reads
// as one colour. ⌃E opens every fold; a click opens one - the room's by its line
// id (roomexpand.go), a conversation's by peerKey through openTool.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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
// the same words twice open together, which is the same message to read.
func peerKey(ev core.Event) string {
	sum := sha256.Sum256([]byte(ev.FromName + "\x00" + ev.Text))
	return peerKeyPrefix + hex.EncodeToString(sum[:8])
}
