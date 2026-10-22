package rpc

// A Wake-minted message uuid, carried in the one field of a sent line the model
// never reads. Claude records the uuid Wake stamps as that turn's own on disk
// (docs/superpowers/notes/2026-09-29-transcript-uuid-findings.md), so a restore
// can tell what Wake wrote - a room turn, or the daemon resuming an agent a
// usage limit stopped - from bytes that otherwise read as the operator's.
//
// The layout is Wake's own inside RFC 9562's version-8 shape:
//
//	bytes 0-3    "wake"           recognises it
//	byte  4      flags            MessageDirect, MessageResumed, MessageTeam, MessageSolo
//	bytes 5-10   send             shared by every target of one room send
//	bytes 11-15  target           per target, so a broadcast's copies stay
//	                              distinct records and only a fork shares one
//
// with the version and variant written over their bits. A DM send keeps a
// random version-4 uuid, which never carries the prefix and version together.

import (
	"crypto/rand"

	"github.com/google/uuid"
)

// messageVersion is the version Wake's uuids are minted as. It rides a field
// claude validates - a stdin line it rejects ends the agent - and 8 is the
// version recorded accepted; falling back to 4 is this one line.
const messageVersion = 8

var messageMark = [4]byte{'w', 'a', 'k', 'e'}

// The flags byte.
const (
	// MessageDirect is a room send a lone direct @name addressed (the echo's `to`).
	MessageDirect byte = 1 << iota
	// MessageResumed is the daemon's "continue" to an agent a usage limit stopped.
	MessageResumed
	// MessageTeam is a room send to a team, so a restore can address it to the
	// members whose transcripts hold it.
	MessageTeam
	// MessageSolo is a room send that reached exactly one session. A restore sees
	// only the transcripts that came back, so one record cannot say a send was
	// not a broadcast; this can.
	MessageSolo
)

// MessageSend is one send: its flags and send bytes, shared by the uuid of
// every target it reaches.
type MessageSend struct{ head [11]byte }

// NewMessageSend mints a send carrying flags.
func NewMessageSend(flags byte) MessageSend {
	var s MessageSend
	_, _ = rand.Read(s.head[5:]) // crypto/rand.Read never fails
	copy(s.head[:4], messageMark[:])
	s.head[4] = flags
	return s
}

// ForTeam marks the send as a team's.
func (s MessageSend) ForTeam() MessageSend {
	s.head[4] |= MessageTeam
	return s
}

// ForSolo marks the send as one session's.
func (s MessageSend) ForSolo() MessageSend {
	s.head[4] |= MessageSolo
	return s
}

// ID is one target's uuid for this send.
func (s MessageSend) ID() string {
	var u uuid.UUID
	copy(u[:], s.head[:])
	_, _ = rand.Read(u[len(s.head):])
	u[6] = u[6]&0x0f | messageVersion<<4
	u[8] = u[8]&0x3f | 0x80
	return u.String()
}

// MessageOf reads a uuid as Wake's: which send, its flags, and whether it is
// Wake's at all.
func MessageOf(id string) (send [6]byte, flags byte, ok bool) {
	u, err := uuid.Parse(id)
	if err != nil || [4]byte(u[:4]) != messageMark || u.Version() != messageVersion {
		return send, 0, false
	}
	copy(send[:], u[5:11])
	return send, u[4], true
}
