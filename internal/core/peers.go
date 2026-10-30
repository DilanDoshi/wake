package core

// Peer is one other Claude session on this machine, as a /list-agents reply
// names it. Not an airlock file: the reply is parsed in localreply.go, and this
// is Wake's shape for what it said.
//
// Parsed from Event text, which DecodeLine has already contained.
type Peer struct {
	Name string `json:"name"`
	Dir  string `json:"dir,omitempty"`
}

// SessionRecord is one running Claude session as Claude Code's registry names
// it: its id, its name and the socket its cross-session messages arrive on.
// Decoded in vocabulary.go.
type SessionRecord struct {
	ID     string
	Name   string
	Socket string
}
