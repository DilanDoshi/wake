package ui

// What the transcript behind an ask costs to quiet.
//
// The claim in askdim.go is that it is bounded by what is *drawn* rather than
// by what is held, so a long conversation costs no more than a short one -
// and that the whole of it is small beside a frame. Both are measured here
// rather than asserted, because a per-frame cost is what the non-negotiables
// are about and a comment is not evidence.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// styledRows is a screenful of already-styled transcript, which is what the
// real thing hands quieted: a strip is only work when there is ANSI to strip.
func styledRows(n int) string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = AccentStyle.Render("⏺ Bash") + " " + TextStyle.Render("go test ./internal/ui/ -run TestSomething")
	}
	return strings.Join(rows, "\n")
}

func BenchmarkTranscriptBehindAnAsk(b *testing.B) {
	for _, rows := range []int{10, 30, 60} {
		block := styledRows(rows)
		b.Run(strings.Repeat("", 0)+itoa(rows)+"rows", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = quieted(block)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// The number that decides whether quieting is affordable: a whole frame with an
// ask up, against the same frame with none.
//
// A frame is what the non-negotiables price, and askdim.go's claim is that
// quieting is small beside one. This is the pairing that can refute it.
func BenchmarkFrameBehindAnAsk(b *testing.B) {
	a := wrapped(b, 200, 40, benchEvents).(App)
	a = a.applyGeometry()

	b.Run("no ask", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = a.View()
		}
	})

	ask := recordedAsks(b, choiceFixture)[0]
	asked := a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ask}).applyGeometry()
	if _, ok := asked.cardOf(asked.focus); !ok {
		b.Fatal("the bench App is putting no ask, so both arms measure the same frame")
	}
	b.Run("ask up", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = asked.View()
		}
	})
	b.Run("ask up, no quieting", func(b *testing.B) {
		plain := asked
		plain.room = plain.room.WithAsk(false)
		b.ReportAllocs()
		for b.Loop() {
			_ = plain.View()
		}
	})
}
