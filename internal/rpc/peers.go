package rpc

// The machine's other Claude sessions, asked of the daemon. Declared here
// rather than in wire.go for team.go's reason: wire.go is at the file-size
// hard max.
//
// The daemon asks an idle agent for a bare /list-agents, a local command that
// lists every session on the machine, and answers every client that asked
// while it was in flight - or, with no agent idle, answers at once from the
// last listing. See internal/daemon/peers.go.

import "github.com/DilanDoshi/wake/internal/core"

const (
	FramePeers      = "peers"       // client → daemon: the machine's other Claude sessions
	FramePeersReply = "peers_reply" // daemon → client: PeersFrame
)

// PeersFrame is FramePeersReply's payload, on Frame.Peers: one listing, as an
// agent last gave it. The zero value is "none known".
type PeersFrame struct {
	// Self is the answering session's own claude name - the one session a
	// listing of the *other* sessions leaves out.
	Self  string      `json:"self,omitempty"`
	Peers []core.Peer `json:"peers,omitempty"`

	// AgeMS is how long ago the listing was taken, in milliseconds for
	// SessionStatus.QuietMS's reason; 0 when none was.
	AgeMS int64 `json:"age_ms,omitempty"`
}
