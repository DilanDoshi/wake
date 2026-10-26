package ui

// Provenance for a turn typed in the room: the uuid Wake stamps on it
// (rpc.MessageSend) comes back on disk as that turn's own, so the restore can
// tell a room turn from a DM turn - the bytes of which are otherwise the same -
// without the two-transcript inference a single-target message could never
// satisfy. The layout is rpc/messageid.go's.

import "github.com/DilanDoshi/wake/internal/rpc"

// newRoomSend mints a send; direct is whether a lone direct @name addressed it.
func newRoomSend(direct bool) rpc.MessageSend {
	var flags byte
	if direct {
		flags = rpc.MessageDirect
	}
	return rpc.NewMessageSend(flags)
}

// roomSendOf reads a uuid as a room send: which send, whether it was direct,
// and whether it is one at all. The daemon's resume is Wake's but no room send,
// and nor is a bang typed in a conversation.
func roomSendOf(id string) (send [6]byte, direct, ok bool) {
	send, flags, ok := rpc.MessageOf(id)
	if !ok || flags&rpc.MessageResumed != 0 || flags&rpc.MessageShell != 0 && flags&rpc.MessageDirect == 0 {
		return send, false, false
	}
	return send, flags&rpc.MessageDirect != 0, true
}

// reconnected is whether a uuid is the daemon's resume of an agent an
// unreachable API stopped, rather than one a usage limit stopped.
func reconnected(id string) bool {
	_, flags, ok := rpc.MessageOf(id)
	return ok && flags&rpc.MessageResumed != 0 && flags&rpc.MessageReconnected != 0
}

// roomSendSolo is whether a uuid is a room send that reached exactly one session.
func roomSendSolo(id string) bool {
	_, flags, ok := rpc.MessageOf(id)
	return ok && flags&rpc.MessageSolo != 0
}

// roomSendTeam is whether a uuid is a room send to a team.
func roomSendTeam(id string) bool {
	_, flags, ok := rpc.MessageOf(id)
	return ok && flags&rpc.MessageTeam != 0
}
