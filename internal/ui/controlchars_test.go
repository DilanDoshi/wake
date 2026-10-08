package ui

// docs/notes/bugs.md BUG-50, the class guard: a character a terminal acts on,
// carried by text Wake did not author, never reaches the frame - whichever
// surface draws it. Tabs are the sibling sweep's (tabs_test.go); everything
// else a terminal can be driven by is here.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// hostileText is every class, raw and as the character references markdown
// decodes, between two words the fences must leave standing: CSI (a clear, a
// cursor move, home), OSC (a title, an OSC 8 link), the 8-bit CSI, BS, CR, VT,
// FF, DEL, the two Unicode separators, and SGRs no style of Wake's draws.
const hostileText = "leftword " +
	"\x1b[2J\x1b[5A\x1b[H\x1b]0;title\a\x1b]8;;https://example.invalid\x1b\\link\x1b]8;;\x1b\\" +
	"\u009b2J\b\r\v\f\x7f\u2028\u2029 " +
	"&#x1b;[2J&#x1b;]0;title&#7;&#155;2J&#8;&#13;&#11;&#12;&#127;&#x2028;&#x1b;[8m&#27;[5m" +
	" rightword"

// frameHoldsNoControlCharacter reports the first character in a drawn frame a
// terminal would act on: any C0 but the newline, DEL, C1 (or a byte that is not
// UTF-8, which an 8-bit terminal reads as one), U+2028/U+2029, or an escape that
// does not open one of the SGR runs Wake draws its own styling with. Wake's
// frame carries no OSC of its own - the clipboard's goes straight to the
// terminal (clipboard.go) - so none is allowed.
func frameHoldsNoControlCharacter(view string) error {
	for i := 0; i < len(view); {
		if n := ownSGR(view[i:]); n > 0 {
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(view[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			return fmt.Errorf("byte %#x at %d is not UTF-8: %q", view[i], i, near(view, i))
		case r == '\n':
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f, r == '\u2028', r == '\u2029':
			return fmt.Errorf("%U at byte %d: %q", r, i, near(view, i))
		}
		i += size
	}
	return nil
}

// ownSGR is the length of the SGR run opening s when every parameter is one a
// lipgloss style or the markdown renderer writes, or 0. That is termenv's whole
// vocabulary but blink, which no style of Wake's sets, plus the renderer's
// list-item tag (59); an extended colour's arguments are skipped, never read
// as codes.
func ownSGR(s string) int {
	if len(s) < 3 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	n := strings.IndexByte(s, 'm')
	if n < 0 || strings.Trim(s[2:n], "0123456789;") != "" {
		return 0
	}
	params := strings.Split(s[2:n], ";")
	args := map[string]int{"5": 1, "2": 3} // an index, or three components
	for i := 0; i < len(params); i++ {
		switch p := params[i]; p {
		case "", "0", "1", "2", "3", "4", "7", "9", "53", "59",
			"30", "31", "32", "33", "34", "35", "36", "37", "39",
			"40", "41", "42", "43", "44", "45", "46", "47", "49",
			"90", "91", "92", "93", "94", "95", "96", "97",
			"100", "101", "102", "103", "104", "105", "106", "107":
		case "38", "48":
			if i+1 >= len(params) || args[params[i+1]] == 0 || i+1+args[params[i+1]] >= len(params) {
				return 0
			}
			i += 1 + args[params[i+1]]
		default:
			return 0
		}
	}
	return n + 1
}

// near is the frame around byte i, for a failure message.
func near(view string, i int) string {
	return view[max(i-40, 0):min(i+40, len(view))]
}

// decoded is a stream-json line through the airlock, as the daemon hands it on.
func decoded(t *testing.T, line map[string]any) []rpc.Frame {
	t.Helper()
	raw, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	events, err := core.DecodeLine(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	frames := make([]rpc.Frame, 0, len(events))
	for _, ev := range events {
		ev.SessionID = "s1"
		frames = append(frames, rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
	}
	return frames
}

// conversationWith is alex's conversation, every frame folded in.
func conversationWith(t *testing.T, frames ...rpc.Frame) App {
	t.Helper()
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 50)
	for _, f := range frames {
		a = deliver(a, f)
	}
	return a
}

func assistantLine(content ...map[string]any) map[string]any {
	return map[string]any{"type": "assistant", "session_id": "s1",
		"message": map[string]any{"role": "assistant", "content": content}}
}

// surfaces plants hostileText on each surface that draws text Wake did not
// author, and returns each drawn frame.
func surfaces(t *testing.T) map[string]string {
	t.Helper()
	fresh(t)
	out := map[string]string{}

	disk := DiskSession{ID: "abcd1234-5678-4abc-8def-000000000000", Dir: "/tmp/" + hostileText,
		Title: hostileText, Preview: "said " + hostileText, Modified: time.Now()}
	out["the resume picker"] = openedResumePicker(t, parkedFleetApp(t, disk)).View()

	// The machine's sessions come the way the daemon reads them: the one-shot's
	// result through the airlock, then the listing parsed out of its text.
	listing := "Other Claude sessions (1):\n  [idle]  ·  wf-alpha  ·  /tmp/" + hostileText + "  ·  started 1m ago"
	result := decoded(t, map[string]any{"type": "result", "subtype": "success", "session_id": "s1",
		"result": listing, "num_turns": 0})
	peers, _, ok := core.PeersFromListAgents(result[0].Event.Text)
	if !ok || len(peers) != 1 {
		t.Fatalf("the listing did not parse: %v %q", ok, result[0].Event.Text)
	}
	menu, _ := typedAsking(t, peerFleet(t, ""), runes("@wf")...)
	out["the @ menu"] = menu.applyFrame(peersReply(peers...)).View()

	out["an assistant's reply"] = conversationWith(t, decoded(t, assistantLine(
		map[string]any{"type": "text", "text": hostileText}))...).View()

	use := decoded(t, assistantLine(map[string]any{"type": "tool_use", "id": "t1", "name": "Bash",
		"input": map[string]any{"command": "echo " + hostileText}}))
	res := decoded(t, map[string]any{"type": "user", "session_id": "s1", "message": map[string]any{"role": "user",
		"content": []map[string]any{{"type": "tool_result", "tool_use_id": "t1", "content": hostileText}}}})
	run, _ := pressKey(conversationWith(t, append(use, res...)...), tea.KeyMsg{Type: tea.KeyCtrlE})
	out["a tool's output"] = run.View()

	ask := decoded(t, map[string]any{"type": "control_request", "request_id": "r1", "request": map[string]any{
		"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "requires_user_interaction": true, "input": map[string]any{
			"questions": []map[string]any{{"question": hostileText, "header": "hdr " + hostileText,
				"options": []map[string]any{{"label": "pick " + hostileText, "description": hostileText}}}}}}})
	out["a card's question"] = conversationWith(t, ask...).View()

	mcp, _ := mcpDM(t)
	mcp = deliver(mcp, decoded(t, map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": "mcp-01", "response": map[string]any{"mcpServers": []map[string]any{{
			"name": "broken", "status": "failed", "error": hostileText, "scope": "project",
			"config": map[string]any{"type": "stdio", "command": "/usr/local/bin/broken"}}}}}})[0])
	out["an MCP server's error"] = openDetail(t, mcp, 0).View()
	return out
}

func TestNoSurfaceDrawsAControlCharacterItWasHanded(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor: Wake's own styling is in the frame the guard reads
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	for surface, view := range surfaces(t) {
		t.Run(surface, func(t *testing.T) {
			if text := stripANSI(view); !strings.Contains(text, "leftword") && !strings.Contains(text, "rightword") {
				t.Fatalf("the frame does not draw the planted text, so this asserts nothing:\n%s", stripANSI(view))
			}
			if err := frameHoldsNoControlCharacter(view); err != nil {
				t.Errorf("%s reaches the terminal: %v", surface, err)
			}
		})
	}
}

// The guard's own premise: it passes Wake's styling and fails each class it
// names, so a pass above is the frame's and not the predicate's.
func TestTheFrameGuardTellsWakesStylingFromWhatItWasHanded(t *testing.T) {
	own := lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(lipgloss.Color("#b1b9f9")).Render("ok") +
		"\n\x1b[38;5;7m\x1b[48;2;1;2;3m\x1b[0m\x1b[m"
	if err := frameHoldsNoControlCharacter(own); err != nil {
		t.Errorf("Wake's own styling reads as a control character: %v", err)
	}
	for _, c := range []string{"\x1b[2J", "\x1b[5A", "\x1b]0;t\a", "\x1b]8;;x\x1b\\", "\u009b", "\b", "\r", "\v",
		"\f", "\x7f", "\u2028", "\u2029", "\x1b[8m", "\x1b[5m", "\x1b[38;5m", "\x9b", "\t"} {
		if frameHoldsNoControlCharacter("a"+c+"b") == nil {
			t.Errorf("%q passes the guard", c)
		}
	}
}
