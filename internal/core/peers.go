package core

// Peer is one other Claude session on this machine, as a /list-agents reply
// names it. Not an airlock file: the reply is parsed in localreply.go, and this
// is Wake's shape for what it said.
//
// Parsed from Event text, which DecodeLine has already contained.
type Peer struct {
	Name  string `json:"name"`
	Dir   string `json:"dir,omitempty"`
	State string `json:"state,omitempty"` // the listing's own word ("idle", "busy"), display only
}
