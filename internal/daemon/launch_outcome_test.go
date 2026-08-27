//go:build unix

package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Moving wake settlement back after launch returns makes both cells expose a
// status/error frame while the durable outcome is still blocked.
func TestLaunchSettlesOutcomeBeforeAnyFrameIsObservable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*testing.T)
		want    bool
	}{
		{name: "success", prepare: func(t *testing.T) { fakeClaudeOnPath(t, "") }, want: true},
		{name: "failure", prepare: noClaudeAnywhere, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.prepare(t)
			s := newServer(tempSocket(t))
			c := newClient(nil)
			cfg := core.Config{SessionID: idAlpha, Name: "alex", Dir: t.TempDir()}
			var reserved parkedRecord
			if tc.want {
				reserved = durableParkedRecord(t)
				cfg = core.Config{
					SessionID: reserved.ID, ResumeFrom: reserved.ID,
					Name: reserved.Name, Dir: reserved.Dir,
				}
				if err := s.parked.add(reserved); err != nil {
					t.Fatalf("write reserved record: %v", err)
				}
				if _, ok, err := s.parked.reserve(reserved.ID); err != nil || !ok {
					t.Fatalf("reserve record: %v, %v", ok, err)
				}
			}
			entered := make(chan bool, 1)
			release := make(chan struct{})
			done := make(chan bool, 1)
			go func() {
				done <- launchWithOutcomeForTest(s, c, cfg, func(success bool) {
					entered <- success
					<-release
					if tc.want {
						s.settleParkReservation(reserved, true, success)
					}
				})
			}()

			if got := <-entered; got != tc.want {
				t.Fatalf("outcome = %v, want %v", got, tc.want)
			}
			if tc.want {
				durable := loadParkBook(parkBookPath(s.socket))
				if len(durable) != 1 || !sameParkedRecord(durable[0], reserved) {
					t.Fatalf("durable record while settlement is blocked = %+v, want exact %+v", durable, reserved)
				}
				a, held := s.agent(cfg.SessionID)
				roster := loadRoster(rosterPath(s.socket))
				if !held || len(roster) != 1 || roster[0].ID != cfg.SessionID ||
					roster[0].PID <= 0 || roster[0].PID != a.sess.Pgid() {
					t.Fatalf("ownership while settlement is blocked: agent held=%v, roster=%+v, want session %s under live pgid", held, roster, cfg.SessionID)
				}
			}
			var early *rpc.Frame
			var durableBefore []parkedRecord
			select {
			case frame := <-c.out:
				early = &frame
				durableBefore = loadParkBook(parkBookPath(s.socket))
			default:
			}
			close(release)
			if launched := <-done; launched != tc.want {
				t.Fatalf("launch = %v, want %v", launched, tc.want)
			}
			if early != nil {
				t.Fatalf("frame %s became observable before launch outcome settled; durable park book was still %+v", early.Kind, durableBefore)
			}
			frame := <-c.out
			if tc.want && frame.Kind != rpc.FrameStatusReply {
				t.Fatalf("successful launch frame = %s, want %s", frame.Kind, rpc.FrameStatusReply)
			}
			if !tc.want && frame.Kind != rpc.FrameError {
				t.Fatalf("failed launch frame = %s, want %s", frame.Kind, rpc.FrameError)
			}
			if tc.want {
				if got := loadParkBook(parkBookPath(s.socket)); len(got) != 0 {
					t.Fatalf("successful settled launch left durable record %+v", got)
				}
				s.beginQuit(quitStop)
				if err := s.shutdown(); err != nil {
					t.Fatalf("shutdown: %v", err)
				}
			}
		})
	}
}

// The pre-fix shape: wake code learned the launch result only after launch had
// already queued its frame. The implementation changes this adapter to pass the
// callback through launch itself.
func launchWithOutcomeForTest(s *server, c *client, cfg core.Config, outcome func(bool)) bool {
	return s.launch(c, cfg, "", nil, outcome)
}
