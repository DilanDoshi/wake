package rpc

import (
	"testing"

	"github.com/google/uuid"
)

// A Wake-minted uuid carries its send and its flags, and a random one carries
// neither: what lets a restore tell a room send or a resume from anything else.
func TestAMessageIDCarriesItsSendAndFlags(t *testing.T) {
	send := NewMessageSend(MessageResumed)
	a, b := send.ID(), send.ID()
	if a == b {
		t.Fatal("two targets of one send got the same uuid")
	}
	for _, id := range []string{a, b} {
		u, err := uuid.Parse(id)
		if err != nil || u.Variant() != uuid.RFC4122 {
			t.Fatalf("%q is not a uuid claude takes: %v", id, err)
		}
		if _, flags, ok := MessageOf(id); !ok || flags != MessageResumed {
			t.Errorf("%q reads as flags %b, ok %v; want the resume flag", id, flags, ok)
		}
	}
	sa, _, _ := MessageOf(a)
	sb, _, _ := MessageOf(b)
	other, _, _ := MessageOf(NewMessageSend(MessageResumed).ID())
	if sa != sb || sa == other {
		t.Error("the send a uuid carries does not group one send's targets")
	}
	if _, flags, _ := MessageOf(NewMessageSend(MessageDirect).ID()); flags != MessageDirect {
		t.Errorf("a direct send reads as flags %b", flags)
	}
	if _, _, ok := MessageOf(uuid.NewString()); ok {
		t.Error("a random uuid reads as Wake's")
	}
	if MessageDirect&MessageResumed|MessageDirect&MessageTeam|MessageResumed&MessageTeam != 0 {
		t.Error("the flags share a bit")
	}
}

// A team mark rides beside the other flags and survives the uuid.
func TestATeamSendKeepsItsOtherFlags(t *testing.T) {
	_, flags, ok := MessageOf(NewMessageSend(MessageDirect).ForTeam().ID())
	if !ok || flags != MessageDirect|MessageTeam {
		t.Errorf("a direct team send reads as flags %b, ok %v; want direct and team", flags, ok)
	}
	if _, flags, _ := MessageOf(NewMessageSend(0).ID()); flags&MessageTeam != 0 {
		t.Error("a send that was not marked reads as a team's")
	}
}
