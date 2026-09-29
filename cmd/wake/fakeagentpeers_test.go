package main

// The agent behind the @ menu screen test: it advertises /list-agents and the
// built-in subagent types in its init, and it is also the daemon's bare
// one-shot, told apart by the --bare its argv carries.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// scriptAtMenu is fakeAgentEcho that opens with a real init.
const scriptAtMenu = "atmenu"

// builtinAgents are the subagent types a sterile recording's init carries.
var builtinAgents = []string{"claude", "Explore", "general-purpose", "Plan", "statusline-setup"}

// bareListingFixture is the recorded bare /list-agents; its first line is an
// init that advertises the command.
const bareListingFixture = "list-agents-bare.jsonl"

func bareFixtureLines() []string {
	_, here, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(here), "..", "..", "testdata", "stream", bareListingFixture))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake: read fixture:", err)
		os.Exit(1)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// fakeBareListing reads its stdin to EOF, then answers with the recording.
func fakeBareListing() int {
	_, _ = io.Copy(io.Discard, os.Stdin)
	fmt.Println(strings.Join(bareFixtureLines(), "\n"))
	return 0
}

// sayAtMenuInit is the recorded init, re-keyed to this session, run where it
// is, and carrying the built-in agents.
func sayAtMenuInit(sid string) {
	var init map[string]any
	if err := json.Unmarshal([]byte(bareFixtureLines()[0]), &init); err != nil {
		fmt.Fprintln(os.Stderr, "fake: decode init:", err)
		os.Exit(1)
	}
	cwd, _ := os.Getwd()
	init["session_id"], init["cwd"], init["agents"] = sid, cwd, builtinAgents
	line, _ := json.Marshal(init)
	fmt.Println(string(line))
}

func fakeAgentAtMenu(sid string) int {
	sayAtMenuInit(sid)
	sayText(sid, "ready")
	sayResult(sid)
	for line := range agentStdin() {
		text, ok := userTextOf(line)
		if !ok {
			continue
		}
		sayAtMenuInit(sid)
		sayText(sid, heardPrefix+text)
		sayResult(sid)
	}
	return 0
}
