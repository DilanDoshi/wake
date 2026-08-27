package core

// What a session is running as, and how full it is.
//
// Split out of event.go when that file crossed the 800-line hard max, and at
// this seam rather than at the line count: everything here is one subject -
// the facts about a session that are *not* the conversation - and it is the
// subject CLAUDE.md's own locations table already names separately. event.go
// keeps the vocabulary and the shape of an event; this keeps what a lifecycle
// frame says about the session behind it.
//
// Not an airlock file: the airlock is protocol.go, wire.go, vocabulary.go and
// encode.go, and these are Wake's shapes rather than Claude's.

// SessionFacts are the facts about a session that are not the conversation:
// what model it is running and how full its context is. They arrive on
// lifecycle frames rather than in the transcript, and a consumer keeps the
// newest of each rather than folding them together.
//
// Zero means "this frame did not say", never "zero": init names a model and no
// window, and result names a window and a level. A consumer that overwrote a
// known model with the empty string from a result would blank the status bar
// once per turn.
// MCPServer is one MCP server a session holds, as Wake names it.
//
// The name is for a person and the status is for a comparison. Neither is
// interpreted here - whether "needs-auth" is worth a line on screen is a
// question about a surface, and this file's job is to say what the wire said.
// Tagged in Wake's spelling rather than Claude's, which is this file's rule
// wherever the two would collide: `status` is a policed wire word and this is
// Wake's own transport, so the field that carries it across it is `state`.
type MCPServer struct {
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
}

type SessionFacts struct {
	// MCPServers is every MCP server the session reported on its last init, in
	// the order claude listed them.
	//
	// Carried whole rather than counted, because the count somebody wants
	// depends on the surface: the banner counts the ones needing
	// authentication, and a `/mcp` would name all of them. Counting here would
	// decide that for both, and the airlock does not decide.
	//
	// Empty is the ordinary case and is not a warning - most sessions hold no
	// MCP servers at all.
	MCPServers []MCPServer `json:"mcp,omitempty"`

	// SlashCommands is every command the session advertised on its last init,
	// in the order claude listed them: its own, and the operator's own
	// `.claude/commands` files beside them.
	//
	// Carried whole rather than filtered, for MCPServers' reason. What Wake
	// does with it is a question about a surface - the composer offers it as a
	// completion menu, and nothing routes on it, because the list is per
	// session and a routing decision is per keystroke.
	//
	// Tagged in Wake's spelling rather than Claude's, like OutputTokens below:
	// `slash_commands` is a policed wire word that only the airlock may name.
	SlashCommands []string `json:"commands,omitempty"`

	// Model is Claude's own id for it, e.g. "claude-sonnet-5", not a display
	// name. Resolving it to something a human reads belongs above the airlock:
	// this file's job is to say what the wire said.
	Model string `json:"model,omitempty"`

	// Dir is the directory the session is running in, as its last init said.
	//
	// It is on init and on nothing else, so it is empty on every other frame -
	// which matters, because every consumer merges these facts field by field
	// and a result frame naming a directory would overwrite a live one once per
	// turn. Same shape as Model and the context window beside it.
	//
	// **It is not the directory the session was spawned in**, and that is the
	// whole reason to read it. EnterWorktree and ExitWorktree are on the tools
	// list of every session Wake starts, so an agent can move itself between
	// worktrees mid-conversation - and claude locates a transcript by the
	// directory its process started in, which makes a stale belief here a
	// resume in the wrong place rather than a wrong label.
	Dir string `json:"dir,omitempty"`

	// ContextTokens is how much of the window the last turn actually used,
	// cache included. A level, not a running total - see wireUsage.
	ContextTokens int `json:"context_tokens,omitempty"`

	// ContextWindow is how large the window is. The stream says this in
	// exactly one place, modelUsage, so a percentage is underivable from any
	// frame that lacks it.
	ContextWindow int `json:"context_window,omitempty"`

	// OutputTokens is what *this turn* produced, and is the one figure here a
	// consumer adds up rather than replaces. The distinction matters: the two
	// above are levels and this is an increment, so folding them the same way
	// gives a context that grows forever or a token count stuck at the last
	// turn's.
	//
	// Tagged in Wake's spelling rather than Claude's, like the two above it -
	// this is Wake's own transport, and `output_tokens` is a policed wire word
	// that only the airlock may name.
	OutputTokens int `json:"turn_output_tokens,omitempty"`

	// TurnOutputTokens is what one message of the turn in flight produced, on a
	// KindTurnTokens and on nothing else. Like OutputTokens it is an increment
	// rather than a level - a turn is several messages and each states its own -
	// but it is a *different* increment, and the two must not be folded into one
	// figure: these arrive while the turn runs and OutputTokens states the same
	// turn's total when it ends, so adding both counts every token twice.
	//
	// Its own spelling for that reason, and because the field it decodes from is
	// on a shape no recording covers. See KindTurnTokens.
	TurnOutputTokens int `json:"progress_output_tokens,omitempty"`
}
