package main

// The agent behind the usage-limit screen test: every turn hits the limit while
// Claude's config file names limitedAccount, and is heard on any other - the way
// signing in to another account lifts a limit.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// scriptUsageLimits is fakeAgentEcho behind a usage limit.
const scriptUsageLimits = "usagelimits"

// limitedAccount is the account whose limit is spent.
const limitedAccount = "acct-limited"

func fakeAgentUsageLimits(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)
	path := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), ".claude.json")
	for line := range agentStdin() {
		text, ok := userTextOf(line)
		if !ok {
			continue
		}
		raw, _ := os.ReadFile(path)
		if account, _ := core.DecodeAccount(raw); account.ID == limitedAccount {
			fmt.Printf(`{"type":"assistant","is_api_error_message":true,"error":"rate_limit","session_id":%q,"message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"You've hit your session limit"}]}}`+"\n", sid)
		} else {
			sayText(sid, heardPrefix+text)
		}
		sayResult(sid)
	}
	return 0
}

// signInAs replaces Claude's config file the way claude writes it.
func signInAs(t *testing.T, dir, account string) {
	t.Helper()
	tmp := filepath.Join(dir, ".claude.json.tmp")
	body := fmt.Sprintf(`{"oauthAccount":{"accountUuid":%q,"organizationUuid":"org-1"}}`, account)
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, ".claude.json")); err != nil {
		t.Fatal(err)
	}
}
