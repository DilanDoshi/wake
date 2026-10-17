package ui

// Provenance for a turn typed in the room, carried in the one field of a sent
// line the model never reads: its uuid. Claude records the uuid Wake stamps as
// that turn's own on disk (docs/superpowers/notes/2026-09-29-transcript-uuid-findings.md),
// so the restore can tell a room turn from a DM turn - the bytes of which are
// otherwise the same - without the two-transcript inference a single-target
// message could never satisfy.
//
// The layout is Wake's own inside RFC 9562's version-8 shape:
//
//	bytes 0-3    "wake"           recognises it
//	byte  4      flags            bit 0: a lone direct @name; bit 1: a team send
//	                              (the address a restore gives it)
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

// roomMessageVersion is the version a room send's uuid is minted as. It rides a
// field claude validates - a stdin line it rejects ends the agent - and 8 is the
// version recorded accepted; falling back to 4 is this one line.
const roomMessageVersion = 8

var roomMessageMark = [4]byte{'w', 'a', 'k', 'e'}

const (
	roomDirectFlag = 1
	roomTeamFlag   = 2
)

// roomSend is one room message: its flags and send bytes, shared by the uuid of
// every target it reaches.
type roomSend struct{ head [11]byte }

// newRoomSend mints a send; direct is whether a lone direct @name addressed it.
func newRoomSend(direct bool) roomSend {
	var s roomSend
	_, _ = rand.Read(s.head[5:]) // crypto/rand.Read never fails
	copy(s.head[:4], roomMessageMark[:])
	if direct {
		s.head[4] = roomDirectFlag
	}
	return s
}

// forTeam marks the send as a team's, so a restore can address it to the members
// whose transcripts hold it.
func (s roomSend) forTeam() roomSend {
	s.head[4] |= roomTeamFlag
	return s
}

// messageID is one target's uuid for this send.
func (s roomSend) messageID() string {
	var u uuid.UUID
	copy(u[:], s.head[:])
	_, _ = rand.Read(u[len(s.head):])
	u[6] = u[6]&0x0f | roomMessageVersion<<4
	u[8] = u[8]&0x3f | 0x80
	return u.String()
}

// roomSendHead reads a uuid as a room send's, and whether it is one at all.
func roomSendHead(id string) (u uuid.UUID, ok bool) {
	u, err := uuid.Parse(id)
	return u, err == nil && [4]byte(u[:4]) == roomMessageMark && u.Version() == roomMessageVersion
}

// roomSendOf reads a uuid as a room send: which send, whether it was direct,
// and whether it is one at all.
func roomSendOf(id string) (send [6]byte, direct, ok bool) {
	u, ok := roomSendHead(id)
	if !ok {
		return send, false, false
	}
	copy(send[:], u[5:11])
	return send, u[4]&roomDirectFlag != 0, true
}

// roomSendTeam is whether a uuid is a room send to a team.
func roomSendTeam(id string) bool {
	u, ok := roomSendHead(id)
	return ok && u[4]&roomTeamFlag != 0
}
