package rpc

// The machine's other Claude sessions, asked of the daemon. Declared here
// rather than in wire.go for team.go's reason: wire.go is at the file-size
// hard max.
//
// The daemon runs a bare one-shot claude for a /list-agents - never an agent,
// whose context the listing would enter - and answers every client that asked
// while it ran. See internal/daemon/peers.go.

import "github.com/DilanDoshi/wake/internal/core"

const (
	FramePeers      = "peers"       // client → daemon: the machine's other Claude sessions
	FramePeersReply = "peers_reply" // daemon → client: PeersFrame
)

// PeersFrame is FramePeersReply's payload, on Frame.Peers: the sessions the
// listing named, or none when there were none or it could not be read.
type PeersFrame struct {
	Peers []core.Peer `json:"peers,omitempty"`
}
