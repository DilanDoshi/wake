package main

// The agent behind the unreachable-API screen test: every turn fails as claude
// fails one it cannot get to the API while the offline file exists, and is heard
// once it is gone - the way the network coming back lets a turn through.

import (
	"fmt"
	"os"
)

// scriptUnreachable is fakeAgentEcho behind a network that may be down.
const scriptUnreachable = "unreachable"

// offlineFileEnv names the file whose presence cuts every agent off the API.
const offlineFileEnv = "WAKE_FAKE_AGENT_OFFLINE"

func fakeAgentUnreachable(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)
	for line := range agentStdin() {
		text, ok := userTextOf(line)
		if !ok {
			continue
		}
		if _, err := os.Stat(os.Getenv(offlineFileEnv)); err == nil {
			fmt.Printf(`{"type":"assistant","is_api_error_message":true,"error":"server_error","session_id":%q,"message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)"}]}}`+"\n", sid)
		} else {
			sayText(sid, heardPrefix+text)
		}
		sayResult(sid)
	}
	return 0
}
