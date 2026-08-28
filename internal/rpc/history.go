// The read queries a client can ask about a session's transcript, and their
// reply kinds. Split from wire.go, which owns the frame envelope and the two
// directions it travels, on lifecycle.go's own precedent: wire.go was
// crowding its 800-line hard max, and this is the subject seam - three
// questions about the same on-disk file, each answered by its own reply kind
// rather than by a field on FrameStatusReply's design.

package rpc

const (
	// FrameHistory asks for the conversation a session already had, and
	// FrameHistoryReply answers with it. Two kinds for FrameStatusReply's
	// reason: a client that could not tell an answer from an announcement
	// would seed a conversation from whichever arrived first.
	//
	// The reply carries events rather than transcript lines, so nothing above
	// internal/core sees claude's on-disk format - which is a different format
	// from the stream, and the one internal/daemon/discover.go already fences.
	FrameHistory      = "history"       // client → daemon: what did this session say
	FrameHistoryReply = "history_reply" // daemon → client: what it said

	// FrameRoomHistory is the same question asked for the room, and
	// FrameRoomHistoryReply answers it. The daemon reads the same file with the
	// same function; two kinds exist because the *client* keeps a ledger per
	// surface.
	//
	// A conversation is asked about once per session per client, or a second
	// fold would draw it twice. If the room shared that ledger, then opening a
	// conversation for a session the room had already asked about would find
	// the ask spent and leave the pane empty - which is the bug history.go was
	// written to remove, arriving through the feature that reuses it. Two
	// questions about one file, for two surfaces, with two answers.
	FrameRoomHistory      = "room_history"       // client → daemon: what did this session say, for the room
	FrameRoomHistoryReply = "room_history_reply" // daemon → client: what it said

	// FrameRewindTargets asks a session's active-branch user prompts, uuid
	// and text oldest first, and FrameRewindTargetsReply answers. FrameRewind
	// itself takes a message uuid core.Event never carries, so this is the
	// UI's only source for one; the last entry is the last_seen tip
	// RewindLastSeen wants.
	FrameRewindTargets      = "rewind_targets"       // client → daemon: what could this session be rewound to
	FrameRewindTargetsReply = "rewind_targets_reply" // daemon → client: its active-branch user prompts, uuid+text, oldest first
)

// RewindTarget is one user prompt a session could be rewound to: a
// transcript message's own uuid, paired with the text it carries. See
// FrameRewindTargets.
//
// The json tags spell neither "uuid" nor "text" - both are Claude's own wire
// words, policed outside the airlock even on Wake's own socket - so this
// wire deliberately uses different words for the same two things.
type RewindTarget struct {
	UUID string `json:"target"`
	Text string `json:"content"`
}
