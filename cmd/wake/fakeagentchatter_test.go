package main

// The agent behind the room-trim screen test: every turn it says chatterCount
// short messages, each one line in the group chat.

import (
	"fmt"
	"time"
)

// scriptChatters is fakeAgentEcho that answers a turn with a burst of messages.
const scriptChatters = "chatters"

// chatterCount is how many messages one turn says: past the 3,000 at which the
// room drops its oldest thousand (internal/ui/roomtrim.go), and short of a second trim.
const chatterCount = 3_100

// chatterEvery paces them. Unpaced, the burst outruns the daemon's queue for the
// window and 971 frames are dropped as a gap, so the room never holds them all.
const chatterEvery = time.Millisecond

// chatterText is the nth message; the first is the oldest line the trim takes.
func chatterText(n int) string { return fmt.Sprintf("chatter %04d", n) }

func fakeAgentChatters(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)
	for line := range agentStdin() {
		if _, ok := userTextOf(line); !ok {
			continue
		}
		for n := 1; n <= chatterCount; n++ {
			sayText(sid, chatterText(n))
			time.Sleep(chatterEvery)
		}
		sayResult(sid)
	}
	return 0
}
