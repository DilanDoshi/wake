package rpc

import (
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// A PeersFrame survives the wire the way WorkflowFrame does - over a real
// net.Conn, read back with ReadFrames.
func TestPeersFrameRoundTrips(t *testing.T) {
	mine, theirs := net.Pipe()
	t.Cleanup(func() { _ = mine.Close(); _ = theirs.Close() })

	want := Frame{
		Kind: FramePeersReply,
		Peers: &PeersFrame{
			Self:  "wf-gamma",
			Peers: []core.Peer{{Name: "wf-beta", Dir: "/private/tmp/wake-rec/beta", State: "idle"}},
			AgeMS: 1500,
		},
	}

	frames, errs := drained(t, theirs)
	go func() {
		if err := WriteFrameTo(mine, want); err != nil {
			t.Errorf("WriteFrameTo: %v", err)
		}
	}()

	select {
	case got := <-frames:
		if got.Kind != want.Kind || !reflect.DeepEqual(got.Peers, want.Peers) {
			t.Fatalf("got %s %+v, want %s %+v", got.Kind, got.Peers, want.Kind, want.Peers)
		}
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(recvTimeout):
		t.Fatal("frame never arrived")
	}
}
