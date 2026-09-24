package ui

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Four servers, one in each state the recording caught.
var (
	mcpFirecrawl = core.MCPServerStatus{Name: "firecrawl", State: core.MCPConnected, Scope: "user",
		Transport: "stdio", Target: "npx -y firecrawl-mcp", Info: "firecrawl-mcp 3.2.1",
		Tools: []core.MCPTool{{Name: "firecrawl_scrape"}, {Name: "firecrawl_search", ReadOnly: true}}}
	mcpHiggsfield = core.MCPServerStatus{Name: "higgsfield", State: core.MCPNeedsAuth, Scope: "user",
		Transport: "http", Target: "https://mcp.higgsfield.ai/mcp"}
	mcpGithub = core.MCPServerStatus{Name: "github", State: core.MCPFailed, Scope: "project",
		Transport: "stdio", Target: "/usr/local/bin/github-mcp", Error: "ENOENT: no such file or directory"}
	mcpEcho = core.MCPServerStatus{Name: "echo", State: core.MCPDisabled, Scope: "project", Transport: "stdio"}
)

func mcpReplyFrame(session string, r core.MCPResult) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: session, Event: &core.Event{Kind: core.KindMCPReply, SessionID: session, MCP: &r}}
}

func serversReply(session string, servers ...core.MCPServerStatus) rpc.Frame {
	return mcpReplyFrame(session, core.MCPResult{Ask: core.MCPAskServers, Servers: servers})
}

// deliver folds a frame the way the drain does - through Update, so settle
// runs and whatever the fold owes the daemon is written.
func deliver(a App, f rpc.Frame) App {
	m, cmd := a.Update(frameMsg{Frame: f})
	drainBatch(cmd)
	return m.(App)
}

func pressMenu(a App, k tea.KeyMsg) App {
	next, cmd := pressKey(a, k)
	drainBatch(cmd)
	return next
}

var (
	keyDown  = tea.KeyMsg{Type: tea.KeyDown}
	keyUp    = tea.KeyMsg{Type: tea.KeyUp}
	keyEnter = tea.KeyMsg{Type: tea.KeyEnter}
	keyEsc   = tea.KeyMsg{Type: tea.KeyEsc}
)

// mcpDM is alex's conversation with /mcp typed into it, the ask on the wire
// and nothing else.
func mcpDM(t *testing.T) (App, *recorder) {
	t.Helper()
	fresh(t)
	rec := newRecorder(t)
	a := dmApp(rec, Stream{}, "s1", "alex").withAgents("alex", "bea", "cy").withSize(120, 40)
	rec.taken(t)
	m, cmd := typeAndSubmit(a, "/mcp")
	drainBatch(cmd)
	return m.(App), rec
}

func sentKinds(frames []rpc.Frame) []string {
	var out []string
	for _, f := range frames {
		out = append(out, f.Kind+" "+f.SessionID+" "+f.Text)
	}
	return out
}

func TestSlashMCPAsksTheConversationsAgentForItsServers(t *testing.T) {
	a, rec := mcpDM(t)
	got := rec.taken(t)
	if len(got) != 1 || got[0].Kind != rpc.FrameMCPList || got[0].SessionID != "s1" {
		t.Fatalf("wrote %v, want one mcp list for s1", sentKinds(got))
	}
	if view := shown(a); !strings.Contains(view, "Manage MCP servers") || !strings.Contains(view, "checking servers") {
		t.Errorf("the menu is not up and waiting:\n%s", view)
	}
	if a.composer().Value() != "" {
		t.Error("the draft was left in the composer")
	}
}

func TestTheListIsGroupedByWhereEachServerIsConfigured(t *testing.T) {
	a, _ := mcpDM(t)
	a = deliver(a, serversReply("s1", mcpFirecrawl, mcpHiggsfield, mcpGithub, mcpEcho))
	view := shown(a)
	for _, want := range []string{
		"4 servers", "Project MCPs", ".mcp.json", "User MCPs", "~/.claude.json",
		"firecrawl", "2 tools", "higgsfield", "needs authentication",
		"github", "failed", "echo", "disabled", "claude.ai connectors",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the list does not show %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "Project MCPs") > strings.Index(view, "User MCPs") {
		t.Error("project servers are listed after user ones; Claude lists the closer scope first")
	}
}

// Another agent's servers are not this menu's.
func TestAReplyForAnotherAgentLeavesTheMenuWaiting(t *testing.T) {
	a, _ := mcpDM(t)
	a = deliver(a, serversReply("s2", mcpFirecrawl))
	if view := shown(a); !strings.Contains(view, "checking servers") || strings.Contains(view, "firecrawl") {
		t.Errorf("another agent's reply filled the menu:\n%s", view)
	}
}

// openDetail opens the menu's detail view on the server at list position n
// (in the order the list draws: project, then user).
func openDetail(t *testing.T, a App, n int) App {
	t.Helper()
	for range n {
		a = pressMenu(a, keyDown)
	}
	return pressMenu(a, keyEnter)
}

func loadedMenu(t *testing.T) (App, *recorder) {
	t.Helper()
	a, rec := mcpDM(t)
	a = deliver(a, serversReply("s1", mcpFirecrawl, mcpHiggsfield, mcpGithub, mcpEcho))
	rec.taken(t)
	return a, rec
}

func TestEachStateOffersItsOwnActions(t *testing.T) {
	cases := []struct {
		pos         int
		name        string
		show, avoid []string
	}{
		{0, "github", []string{"Status:", "failed", "ENOENT", "Command:", "/usr/local/bin/github-mcp", "Reconnect", "Disable"}, []string{"Authenticate", "View tools"}},
		{1, "echo", []string{"disabled", "Enable"}, []string{"Reconnect", "Authenticate"}},
		{2, "firecrawl", []string{"connected", "firecrawl-mcp 3.2.1", "npx -y firecrawl-mcp", "View tools", "Reconnect", "Disable"}, []string{"Authenticate", "Enable"}},
		{3, "higgsfield", []string{"needs authentication", "URL:", "https://mcp.higgsfield.ai/mcp", "Authenticate", "Disable"}, []string{"View tools", "Reconnect"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := loadedMenu(t)
			view := shown(openDetail(t, a, c.pos))
			for _, w := range c.show {
				if !strings.Contains(view, w) {
					t.Errorf("the %s detail does not show %q:\n%s", c.name, w, view)
				}
			}
			for _, w := range c.avoid {
				if strings.Contains(view, w) {
					t.Errorf("the %s detail offers %q, which cannot help it:\n%s", c.name, w, view)
				}
			}
		})
	}
}

// Reconnect writes the ask, a refusal lands in the detail view in Claude's own
// words, and a success refreshes the list so the state shown is the live one.
func TestReconnectAsksAndItsAnswerIsShown(t *testing.T) {
	a, rec := loadedMenu(t)
	a = pressMenu(openDetail(t, a, 0), keyEnter) // github: Reconnect is the first action
	got := rec.taken(t)
	if len(got) != 1 || got[0].Kind != rpc.FrameMCPReconnect || got[0].SessionID != "s1" || got[0].Text != "github" {
		t.Fatalf("wrote %v, want a reconnect of github on s1", sentKinds(got))
	}
	if !strings.Contains(shown(a), "reconnecting") {
		t.Errorf("nothing says a reconnect is under way:\n%s", shown(a))
	}

	a = deliver(a, mcpReplyFrame("s1", core.MCPResult{Ask: core.MCPAskReconnect, Server: "github", Error: "ENOENT: still missing"}))
	if view := shown(a); !strings.Contains(view, "ENOENT: still missing") {
		t.Errorf("the refusal is not in the detail view:\n%s", view)
	}
	if n, ok := notice.Latest(); ok && strings.Contains(n.String(), "refused") {
		t.Errorf("an MCP refusal reached the notice row as a mode refusal: %q", n.String())
	}

	a = pressMenu(a, keyEnter)
	rec.taken(t)
	deliver(a, mcpReplyFrame("s1", core.MCPResult{Ask: core.MCPAskReconnect, Server: "github"}))
	if got := rec.taken(t); len(got) != 1 || got[0].Kind != rpc.FrameMCPList {
		t.Errorf("a successful reconnect wrote %v, want a fresh list", sentKinds(got))
	}
}

func TestDisableAndEnableSwitchTheServer(t *testing.T) {
	a, rec := loadedMenu(t)
	a = openDetail(t, a, 2)                       // firecrawl: View tools, Reconnect, Disable
	a = pressMenu(pressMenu(a, keyDown), keyDown) // onto Disable
	a = pressMenu(a, keyEnter)
	if got := rec.taken(t); len(got) != 1 || got[0].Kind != rpc.FrameMCPDisable || got[0].Text != "firecrawl" {
		t.Fatalf("wrote %v, want firecrawl disabled", sentKinds(got))
	}
	a = pressMenu(a, keyEsc)
	a = openDetail(t, pressMenu(pressMenu(a, keyUp), keyUp), 1) // echo, from the top
	pressMenu(a, keyEnter)
	if got := rec.taken(t); len(got) != 1 || got[0].Kind != rpc.FrameMCPEnable || got[0].Text != "echo" {
		t.Fatalf("wrote %v, want echo enabled", sentKinds(got))
	}
}

// A digit picks an action the way it picks a card's option.
func TestADigitPicksAnAction(t *testing.T) {
	a, rec := loadedMenu(t)
	a = openDetail(t, a, 2)
	pressMenu(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if got := rec.taken(t); len(got) != 1 || got[0].Kind != rpc.FrameMCPReconnect {
		t.Fatalf("2 wrote %v, want the second action, Reconnect", sentKinds(got))
	}
}

func TestViewToolsListsThemAndEscapeWalksBackOut(t *testing.T) {
	a, _ := loadedMenu(t)
	a = pressMenu(openDetail(t, a, 2), keyEnter) // firecrawl: View tools
	view := shown(a)
	if !strings.Contains(view, "firecrawl_scrape") || !strings.Contains(view, "firecrawl_search") || !strings.Contains(view, "read-only") {
		t.Fatalf("the tools are not listed:\n%s", view)
	}
	a = pressMenu(a, keyEsc)
	if !strings.Contains(shown(a), "Status:") {
		t.Fatalf("esc did not return to the detail view:\n%s", shown(a))
	}
	a = pressMenu(a, keyEsc)
	if !strings.Contains(shown(a), "4 servers") {
		t.Fatalf("esc did not return to the list:\n%s", shown(a))
	}
	a = pressMenu(a, keyEsc)
	if strings.Contains(shown(a), "Manage MCP servers") {
		t.Fatalf("esc on the list did not close the menu:\n%s", shown(a))
	}
}

// While the menu is up it has the keys: a typed letter does not leak into the
// draft behind it.
func TestTheMenuTakesEveryKey(t *testing.T) {
	a, _ := loadedMenu(t)
	a = pressMenu(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if a.composer().Value() != "" {
		t.Errorf("a key reached the draft under the menu: %q", a.composer().Value())
	}
}

func TestTheMenuClosesWhenItsAgentEnds(t *testing.T) {
	a, _ := loadedMenu(t)
	a = a.withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateEnded})
	if strings.Contains(shown(a), "Manage MCP servers") {
		t.Error("the menu outlived its agent")
	}
}

// From the room the menu is aimed with @who, and a bare /mcp with nobody
// picked says how.
func TestTheRoomAimsTheMenuWithAMention(t *testing.T) {
	fresh(t)
	rec := newRecorder(t)
	a := NewRoomApp(rec, Stream{}, nil).withAgents("alex", "bea").withSize(120, 40)
	rec.taken(t)

	m, cmd := typeAndSubmit(a, "@bea /mcp")
	drainBatch(cmd)
	got := rec.taken(t)
	if len(got) != 1 || got[0].Kind != rpc.FrameMCPList || got[0].SessionID != "s2" {
		t.Fatalf("@bea /mcp wrote %v, want a list for bea (s2)", sentKinds(got))
	}
	if !strings.Contains(shown(m), "Manage MCP servers") {
		t.Errorf("the menu did not open in the room:\n%s", shown(m))
	}
}

func TestAParkedAgentHasNoServersToAsk(t *testing.T) {
	fresh(t)
	rec := newRecorder(t)
	a := dmApp(rec, Stream{}, "s1", "alex").withSize(120, 40).
		withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked})
	rec.taken(t)
	m, cmd := typeAndSubmit(a, "/mcp")
	drainBatch(cmd)
	if got := rec.taken(t); len(got) != 0 {
		t.Errorf("asked a parked agent: %v", sentKinds(got))
	}
	if strings.Contains(shown(m), "Manage MCP servers") || !strings.Contains(latestNotice(t), "parked") {
		t.Errorf("a parked agent's menu opened, or nothing said why not (%q)", latestNotice(t))
	}
}

// fakeHandOver stands in for cmd/wake's terminal hand-off: it records what
// would have been run and runs nothing.
type fakeHandOver struct{ cmds *[]*exec.Cmd }

func (f fakeHandOver) handOver(cmd *exec.Cmd, header string) tea.ExecCommand {
	*f.cmds = append(*f.cmds, cmd)
	return stubExec{}
}

type stubExec struct{}

func (stubExec) Run() error          { return nil }
func (stubExec) SetStdin(io.Reader)  {}
func (stubExec) SetStdout(io.Writer) {}
func (stubExec) SetStderr(io.Writer) {}

// Authenticate hands the terminal to Claude's own sign-in for that server, in
// the agent's directory - the same flow Claude Code's /mcp runs, on the
// operator's real terminal.
func TestAuthenticateHandsTheTerminalToClaudesSignIn(t *testing.T) {
	a, _ := loadedMenu(t)
	var ran []*exec.Cmd
	a = a.WithHandOver(fakeHandOver{&ran}.handOver)
	_, cmd := pressKey(openDetail(t, a, 3), keyEnter) // higgsfield: Authenticate
	if cmd == nil || len(ran) != 1 {
		t.Fatalf("Authenticate handed nothing over (cmd %v, %d commands)", cmd, len(ran))
	}
	if args := strings.Join(ran[0].Args, " "); args != "claude mcp login higgsfield" {
		t.Errorf("ran %q, want claude's own sign-in for higgsfield", args)
	}
}

// Once signed in, every agent stuck on that server is reconnected - not only
// the one whose menu asked - and one line says which.
func TestASignInReconnectsEveryAgentStuckOnThatServer(t *testing.T) {
	a, rec := loadedMenu(t)
	var ran []*exec.Cmd
	a = a.WithHandOver(fakeHandOver{&ran}.handOver)
	a, _ = pressKey(openDetail(t, a, 3), keyEnter)
	rec.taken(t)

	m, cmd := a.Update(mcpSignedInMsg{Session: "s1", Server: "higgsfield"})
	drainBatch(cmd)
	a = m.(App)
	got := sentKinds(rec.taken(t))
	for _, want := range []string{rpc.FrameMCPReconnect + " s1 higgsfield", rpc.FrameMCPList + " s2 ", rpc.FrameMCPList + " s3 "} {
		if !contains(got, want) {
			t.Errorf("after the sign-in the client wrote %v, missing %q", got, want)
		}
	}

	// bea is stuck on it too; cy never had it.
	a = deliver(a, serversReply("s2", mcpHiggsfield))
	if got := sentKinds(rec.taken(t)); !contains(got, rpc.FrameMCPReconnect+" s2 higgsfield") {
		t.Errorf("bea was stuck on higgsfield and was not reconnected: %v", got)
	}
	a = deliver(a, serversReply("s3", mcpFirecrawl))
	if got := rec.taken(t); len(got) != 0 {
		t.Errorf("cy has no higgsfield and was asked for %v", sentKinds(got))
	}

	a = deliver(a, mcpReplyFrame("s1", core.MCPResult{Ask: core.MCPAskReconnect, Server: "higgsfield"}))
	deliver(a, mcpReplyFrame("s2", core.MCPResult{Ask: core.MCPAskReconnect, Server: "higgsfield"}))
	if n := latestNotice(t); !strings.Contains(n, "higgsfield") || !strings.Contains(n, "alex") || !strings.Contains(n, "bea") {
		t.Errorf("the closing line = %q, want higgsfield reconnected on alex and bea", n)
	}
}

func TestASignInThatDidNotFinishSaysSo(t *testing.T) {
	a, rec := loadedMenu(t)
	var ran []*exec.Cmd
	a = a.WithHandOver(fakeHandOver{&ran}.handOver)
	a, _ = pressKey(openDetail(t, a, 3), keyEnter)
	rec.taken(t)
	m, cmd := a.Update(mcpSignedInMsg{Session: "s1", Server: "higgsfield", Err: errors.New("exit status 1")})
	drainBatch(cmd)
	if got := rec.taken(t); len(got) != 0 {
		t.Errorf("a failed sign-in still reconnected: %v", sentKinds(got))
	}
	if view := shown(m); !strings.Contains(view, "did not finish") {
		t.Errorf("the detail view does not say the sign-in failed:\n%s", view)
	}
}

// With no terminal to hand over there is still a way through: the command,
// in the directory it has to run in.
func TestWithoutATerminalAuthenticateSaysWhatToRun(t *testing.T) {
	a, _ := loadedMenu(t)
	_, cmd := pressKey(openDetail(t, a, 3), keyEnter)
	if cmd != nil {
		drainBatch(cmd)
	}
	if n := latestNotice(t); !strings.Contains(n, "claude mcp login higgsfield") {
		t.Errorf("notice = %q, want the command to run", n)
	}
}

// A name that reads as a flag is not handed to claude's argument parser.
func TestASignInRefusesANameThatReadsAsAFlag(t *testing.T) {
	a, _ := mcpDM(t)
	odd := mcpHiggsfield
	odd.Name = "--help"
	a = deliver(a, serversReply("s1", odd))
	var ran []*exec.Cmd
	a = a.WithHandOver(fakeHandOver{&ran}.handOver)
	pressKey(pressMenu(a, keyEnter), keyEnter)
	if len(ran) != 0 {
		t.Errorf("ran %v for a server named like a flag", ran[0].Args)
	}
}

// An action the daemon refused is never going to be answered, so the menu
// stops waiting on it and says why, and the next action is not blocked.
func TestARefusedActionStopsWaiting(t *testing.T) {
	a, rec := loadedMenu(t)
	a = pressMenu(openDetail(t, a, 0), keyEnter) // reconnect github
	rec.taken(t)
	a = deliver(a, rpc.Frame{Kind: rpc.FrameError, SessionID: "s1", Text: "session s1 is not accepting input"})
	if view := shown(a); strings.Contains(view, "reconnecting") || !strings.Contains(view, "not accepting input") {
		t.Fatalf("the refused reconnect is still waiting, or the refusal is not shown:\n%s", view)
	}
	pressMenu(a, keyEnter)
	if got := rec.taken(t); len(got) != 1 || got[0].Kind != rpc.FrameMCPReconnect {
		t.Errorf("after a refusal the next reconnect wrote %v", sentKinds(got))
	}
}

// A gap may have eaten the answer: the menu stops waiting, re-asks for its
// list, and a sweep reports what it has rather than waiting forever.
func TestAGapSettlesEveryWaitingMCPAsk(t *testing.T) {
	a, rec := loadedMenu(t)
	var ran []*exec.Cmd
	a = a.WithHandOver(fakeHandOver{&ran}.handOver)
	a = pressMenu(openDetail(t, a, 0), keyEnter) // reconnect github: busy
	m, cmd := a.Update(mcpSignedInMsg{Session: "s1", Server: "higgsfield"})
	drainBatch(cmd)
	a = m.(App)
	rec.taken(t)

	a = a.notedGap(3)
	a, cmd = a.settle()
	drainBatch(cmd)
	if got := sentKinds(rec.taken(t)); !contains(got, rpc.FrameMCPList+" s1 ") {
		t.Errorf("after the gap the menu did not re-ask for its list: %v", got)
	}
	if view := shown(a); strings.Contains(view, "reconnecting") {
		t.Errorf("the menu still waits on an answer the gap may have eaten:\n%s", view)
	}
	if a.mcpUI.sweep.active() {
		t.Error("the sign-in's sweep still waits after the gap")
	}
	if n := latestNotice(t); !strings.Contains(n, "higgsfield signed in") || strings.Contains(n, "failing") {
		t.Errorf("the sweep's line = %q; after a gap it may not claim a failure it never saw", n)
	}
}
