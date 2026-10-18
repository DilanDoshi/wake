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
// and whether it is one at all. The daemon's resume is Wake's but no room send.
func roomSendOf(id string) (send [6]byte, direct, ok bool) {
	send, flags, ok := rpc.MessageOf(id)
	if !ok || flags&rpc.MessageResumed != 0 {
		return send, false, false
	}
	return send, flags&rpc.MessageDirect != 0, true
}
