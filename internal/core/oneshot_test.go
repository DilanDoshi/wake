package core

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The one-shot that lists the machine's sessions is the recorded command line
// (2026-09-27-at-menu-findings.md §1a): bare, persisting nothing, stream-json
// both ways, and naming no session of its own.
func TestTheListAgentsOneShotIsTheRecordedArgv(t *testing.T) {
	want := []string{"--print", "--bare", "--no-session-persistence",
		"--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
	cmd := ListAgentsCommand(context.Background(), t.TempDir())
	if cmd.Args[0] != claudeBinary || !reflect.DeepEqual(cmd.Args[1:], want) {
		t.Errorf("the one-shot runs %q, want %s %q", cmd.Args, claudeBinary, want)
	}
}

// It runs as an agent does - the nested-session variables scrubbed, in the
// directory it is given - and is ended whole: a group of its own that its
// context's cancel kills, with the wait after the kill bounded.
func TestTheListAgentsOneShotRunsAsAnAgentDoes(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	dir := t.TempDir()
	cmd := ListAgentsCommand(context.Background(), dir)
	if cmd.Dir != dir {
		t.Errorf("Dir = %q, want %q", cmd.Dir, dir)
	}
	if cmd.Env == nil || strings.Contains(strings.Join(cmd.Env, "\x00"), "CLAUDECODE=") {
		t.Error("the one-shot inherits the nested-session variables, so it would announce its parent's identity")
	}
	if cmd.Cancel == nil || cmd.WaitDelay != waitDelay {
		t.Errorf("Cancel set %v, WaitDelay %v: a hung one-shot must be killed whole and waited on for no longer than %v",
			cmd.Cancel != nil, cmd.WaitDelay, waitDelay)
	}
}

// It carries no credential and no third-party provider switch: /list-agents
// needs neither (the bare recordings show apiKeySource none), and without them
// a bare claude cannot spend. Everything else an agent inherits it inherits too.
func TestTheListAgentsOneShotCarriesNoCredential(t *testing.T) {
	gone := []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
	}
	for _, name := range gone {
		t.Setenv(name, "1")
	}
	t.Setenv("WAKE_ONESHOT_KEPT", "1")
	env := ListAgentsCommand(context.Background(), t.TempDir()).Env
	for _, name := range gone {
		if slices.Contains(env, name+"=1") {
			t.Errorf("the one-shot carries %s, so a model turn it ran could be billed", name)
		}
	}
	if !slices.Contains(env, "WAKE_ONESHOT_KEPT=1") {
		t.Error("the one-shot lost an ordinary variable an agent keeps")
	}
}
