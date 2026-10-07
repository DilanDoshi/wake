package main

// The agent behind the fresh-agent skills screen test: it answers the daemon's
// initialize handshake with its slash commands and sends no init - a fresh
// session sends one only with a turn - so the report is where its menu comes from.

import (
	"encoding/json"
	"fmt"
)

// scriptHandshakes is fakeAgentEcho that names its commands in the handshake's
// reply, and only there.
const scriptHandshakes = "handshakes"

// handshakeSkill is what the reply names and the operator's brief ends in.
const handshakeSkill = "complete-linear-ticket"

// handshakeKnown is said after the reply, so it on screen means the report
// carrying the commands has already reached the client.
const handshakeKnown = "skills known"

func fakeAgentHandshakes(sid string) int {
	sayText(sid, "ready") // first, as a real one's hooks are: the reply is not the first event
	sayResult(sid)
	for line := range agentStdin() {
		if id, ok := initializeRequested(line); ok {
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"commands":[{"name":"compact"},{"name":%q}]}}}`+"\n",
				id, handshakeSkill)
			sayText(sid, handshakeKnown)
			continue
		}
		text, ok := userTextOf(line)
		if !ok {
			continue
		}
		sayText(sid, heardPrefix+text)
		sayResult(sid)
	}
	return 0
}

// initializeRequested reads the correlator off an initialize control_request.
func initializeRequested(line string) (id string, ok bool) {
	var f struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		return "", false
	}
	return f.RequestID, f.Type == "control_request" && f.Request.Subtype == "initialize"
}
