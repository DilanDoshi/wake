package main

// The agents behind the send screen test: each registers in Claude Code's
// session registry, as a real session does, and on the operator's word sends a
// peer a message by its socket address or receives one.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// scriptSends is fakeAgentEcho plus the three words sendsTurn reads.
const scriptSends = "sends"

// The words sendsTurn reads: `send <name> <word>` sends <name> a message by its
// socket address, `sendref <name> <word>` by `name [ref]`, and `hear <name>
// <word>` replays one from <name> as received.
const (
	sendWord    = "send"
	sendRefWord = "sendref"
	hearWord    = "hear"
)

// sentPrefix heads a message's words, so the room's line is told apart from the
// operator's own turn that asked for it.
const sentPrefix = "message "

// sentSaid is what the sender says after a send, so a test can wait on the send
// having happened whether or not the room draws it.
const sentSaid = "sent "

// heardSaid is sentSaid for the receiver.
const heardSaid = "heard "

// sessionRecord is a session's file in Claude Code's registry: the keys Wake
// reads, matched against Claude Code, maintained by hand.
type sessionRecord struct {
	PID    int    `json:"pid"`
	ID     string `json:"sessionId"`
	Name   string `json:"name"`
	Socket string `json:"messagingSocketPath"`
}

// registerSession writes rec as <pid>.json under the registry in config.
func registerSession(config string, rec sessionRecord) error {
	dir := filepath.Join(config, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", rec.PID)), b, 0o600)
}

// registered is the record named name in config's registry.
func registered(config, name string) (sessionRecord, bool) {
	files, _ := filepath.Glob(filepath.Join(config, "sessions", "*.json"))
	for _, f := range files {
		var rec sessionRecord
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &rec) == nil && rec.Name == name {
			return rec, true
		}
	}
	return sessionRecord{}, false
}

func fakeAgentSends(sid string) int {
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	pid := os.Getpid()
	rec := sessionRecord{PID: pid, ID: sid, Name: agentArg(os.Args, "--name"),
		Socket: filepath.Join(config, "socks", fmt.Sprintf("%d.sock", pid))}
	if err := registerSession(config, rec); err != nil {
		fmt.Fprintln(os.Stderr, "fake: register:", err)
		return 1
	}
	sayText(sid, "ready")
	sayResult(sid)
	for line := range agentStdin() {
		if text, ok := userTextOf(line); ok {
			sendsTurn(sid, config, text)
			sayResult(sid)
		}
	}
	return 0
}

// sendsTurn carries out one of the three words, or echoes anything else.
func sendsTurn(sid, config, text string) {
	f := strings.Fields(text)
	for len(f) > 0 && strings.HasPrefix(f[0], "@") {
		f = f[1:] // the room's mention, which the message keeps
	}
	if len(f) != 3 {
		sayText(sid, heardPrefix+text)
		return
	}
	peer, ok := registered(config, f[1])
	words := sentPrefix + f[2]
	switch {
	case f[0] == sendWord && ok:
		saySend(sid, "uds:"+peer.Socket, words)
		sayText(sid, sentSaid+f[2])
	case f[0] == sendRefWord:
		saySend(sid, f[1]+" [0a1b2c]", words)
		sayText(sid, sentSaid+f[2])
	case f[0] == hearWord && ok:
		sayHeard(sid, "uds:"+peer.Socket, peer.Name, words)
		sayText(sid, heardSaid+f[2])
	default:
		sayText(sid, heardPrefix+text)
	}
}

// saySend is a SendMessage call, its input as a live one carries it.
func saySend(sid, to, words string) {
	fmt.Printf(`{"type":"assistant","session_id":%q,"message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_send","name":"SendMessage","input":{"to":%q,"summary":"a note","message":%q}}]}}`+"\n",
		sid, to, words)
}

// sayHeard is a peer's message replayed as received (cross-session.jsonl).
func sayHeard(sid, from, name, words string) {
	content := fmt.Sprintf("Another Claude session sent a message:\n<cross-session-message from=%q from-name=%q from-mode=\"prompting\">\n%s\n</cross-session-message>\n\nThis came from another Claude session.", from, name, words)
	fmt.Printf(`{"type":"user","isReplay":true,"session_id":%q,"message":{"role":"user","content":%q}}`+"\n", sid, content)
}
