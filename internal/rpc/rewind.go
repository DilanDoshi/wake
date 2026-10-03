package rpc

// The rewind frames: what a session could be rewound to, and the four ways to
// rewind it - its conversation, its files, both, or a preview of its files.
// Split out of wire.go and history.go when file rewind added three kinds.
//
// Each of the four writes is a line on the stdin of a process that already
// exists, so each goes through the agent's queue as FrameSend / FrameMode do,
// and each answers with a receipt on the event stream rather than a reply
// frame. Four kinds rather than one carrying a mode field, FrameAllow /
// FrameDeny's reason: no default is safe, and code is restored only when a
// frame says so by its kind.

const (
	// FrameRewindTargets asks a session's active-branch user prompts, uuid
	// and text oldest first, and FrameRewindTargetsReply answers. FrameRewind
	// itself takes a message uuid core.Event never carries, so this is the
	// UI's only source for one; the last entry is the last_seen tip
	// RewindLastSeen wants.
	FrameRewindTargets      = "rewind_targets"       // client → daemon: what could this session be rewound to
	FrameRewindTargetsReply = "rewind_targets_reply" // daemon → client: its active-branch user prompts, uuid+text, oldest first

	// FrameRewind asks a running session to rewind its conversation to an
	// earlier user turn - RewindTarget and RewindLastSeen. It answers the way
	// FrameMode does - no reply frame, only a control_response on the event
	// stream, arriving as a core.KindRewindReceipt, which is the only
	// authority on whether it rewound.
	FrameRewind = "rewind" // client → daemon: rewind a running session's conversation

	// FrameRewindPreview asks which files restoring RewindTarget would change,
	// and changes nothing. Its answer, a core.KindFilesRewindReceipt with
	// Preview set, goes only to the client that asked, as an MCP answer does.
	FrameRewindPreview = "rewind_preview" // client → daemon: what would restoring this session's files change

	// FrameRewindFiles restores the files the session's tools edited to their
	// state at RewindTarget, leaving the conversation as it is.
	FrameRewindFiles = "rewind_code" // client → daemon: restore a running session's files

	// FrameRewindBoth restores the files, then - only once the restore has
	// succeeded - rewinds the conversation to the same RewindTarget, declaring
	// RewindLastSeen. A failed restore leaves the conversation as it was.
	FrameRewindBoth = "rewind_both" // client → daemon: restore a session's files, then its conversation
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
