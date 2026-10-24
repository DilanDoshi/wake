package main

// The agent behind the session-usage screen test: every turn's API response
// carries the five-hour window, 47% used, resetting three hours on.

import (
	"fmt"
	"time"
)

// scriptUsageReadings is fakeAgentEcho with a usage reading on every turn.
const scriptUsageReadings = "usagereadings"

func fakeAgentUsageReadings(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)
	for line := range agentStdin() {
		text, ok := userTextOf(line)
		if !ok {
			continue
		}
		resets := time.Now().Add(3 * time.Hour).Unix()
		fmt.Printf(`{"type":"rate_limit_event","session_id":%q,"rate_limit_info":{"status":"allowed","rateLimitType":"five_hour","resetsAt":%d,"unifiedWindows":{"five_hour":{"utilization":0.47,"resetsAt":%d}}}}`+"\n", sid, resets, resets)
		sayText(sid, heardPrefix+text)
		sayResult(sid)
	}
	return 0
}
