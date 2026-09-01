// Claude's vocabulary resolved into Wake's, at unit level: the display
// argument, the diff, the transcript envelope, and the notices that are the
// narrowed half of the airlock ruling. The code under test is vocabulary.go.
//
// Split out of protocol_test.go when that file passed this project's 800-line
// hard max - the same limit that split the airlock itself and then
// fixtures_test.go, and it applies to tests too. protocol_test.go carries the
// airlock's own rule and the shared helpers.

package core

import "testing"

// The first of the ruling's three rows. Display is the one argument worth
// showing beside a tool's name, resolved here so no renderer indexes Input.
func TestToolCallResolvesItsDisplayArgument(t *testing.T) {
	cases := []struct{ tool, key, value, want string }{
		{"Bash", "command", "go test ./...", "go test ./..."},
		{"Read", "file_path", "auth.go", "auth.go"},
		{"Grep", "pattern", "TODO", "TODO"},
		// The case that decides where the map lives: the wire name is Agent
		// where init.tools advertises Task, so only the airlock can know.
		{"Agent", "description", "Count lines", "Count lines"},
		// No mapped argument, and a mapped tool whose key is absent.
		{"MysteryTool", "command", "ls", ""},
		{"Bash", "timeout", "30", ""},
	}
	for _, c := range cases {
		t.Run(c.tool+"/"+c.key, func(t *testing.T) {
			line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"` +
				c.tool + `","input":{"` + c.key + `":"` + c.value + `"}}]}}`
			ev := onlyEvent(t, line, 0)
			if ev.Tool == nil {
				t.Fatal("Tool is nil")
			}
			if ev.Tool.Display != c.want {
				t.Errorf("Display = %q, want %q", ev.Tool.Display, c.want)
			}
		})
	}
}

// A non-string argument still renders. Recorded inputs are not all strings.
func TestToolCallDisplayFormatsANonStringArgument(t *testing.T) {
	line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":42}}]}}`

	if got := onlyEvent(t, line, 0).Tool.Display; got != "42" {
		t.Errorf("Display = %q, want %q", got, "42")
	}
}

// The second row. Both halves must be present and both must be strings, so a
// tool carrying neither degrades to its header rather than guessing.
func TestToolCallResolvesItsDiffOnlyWhenBothHalvesArePresent(t *testing.T) {
	cases := map[string]struct {
		input        string
		wantDiff     bool
		wantOldNewIs [2]string
	}{
		"both halves":  {`{"file_path":"a.go","old_string":"alpha","new_string":"bravo"}`, true, [2]string{"alpha", "bravo"}},
		"only old":     {`{"file_path":"a.go","old_string":"alpha"}`, false, [2]string{}},
		"only new":     {`{"file_path":"a.go","new_string":"bravo"}`, false, [2]string{}},
		"neither":      {`{"command":"ls"}`, false, [2]string{}},
		"not a string": {`{"old_string":7,"new_string":"bravo"}`, false, [2]string{}},
		// An empty side is a real edit: deleting text, or creating it.
		"empty old": {`{"old_string":"","new_string":"bravo"}`, true, [2]string{"", "bravo"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Edit","input":` + c.input + `}]}}`
			ev := onlyEvent(t, line, 0)
			if got := ev.Tool.Diff != nil; got != c.wantDiff {
				t.Fatalf("Diff present = %v, want %v (input %s)", got, c.wantDiff, c.input)
			}
			if !c.wantDiff {
				return
			}
			if ev.Tool.Diff.Old != c.wantOldNewIs[0] || ev.Tool.Diff.New != c.wantOldNewIs[1] {
				t.Errorf("Diff = %+v, want %v", *ev.Tool.Diff, c.wantOldNewIs)
			}
		})
	}
}

// The third row. The markers are pure wire format, so they are stripped here
// and no renderer names them.
func TestUserTextLosesTheLocalCommandStdoutEnvelope(t *testing.T) {
	cases := map[string]struct{ content, want string }{
		"wrapped":            {`"<local-command-stdout>Compacted </local-command-stdout>"`, "Compacted"},
		"empty wrapper":      {`"<local-command-stdout></local-command-stdout>"`, ""},
		"unwrapped survives": {`"just text"`, "just text"},
		// Only a whole envelope is unwrapped; a stray half is content.
		"half an envelope": {`"<local-command-stdout>dangling"`, "<local-command-stdout>dangling"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			line := `{"type":"user","session_id":"s1","message":{"role":"user","content":` + c.content + `}}`
			ev := onlyEvent(t, line, 0)
			if ev.Kind != KindUserText {
				t.Fatalf("Kind = %q, want %q", ev.Kind, KindUserText)
			}
			if ev.Text != c.want {
				t.Errorf("Text = %q, want %q", ev.Text, c.want)
			}
		})
	}
}

// The envelope also has to be stripped when it arrives as a text block rather
// than as bare string content, or the same wrapper reaches a reader by the
// other path.
func TestLocalCommandStdoutIsStrippedFromATextBlockToo(t *testing.T) {
	line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"type":"text","text":"<local-command-stdout>Compacted </local-command-stdout>"}]}}`

	if got := onlyEvent(t, line, 0).Text; got != "Compacted" {
		t.Errorf("Text = %q, want %q", got, "Compacted")
	}
}

// ...and never from the assistant's side, which is the only side the envelope
// has never appeared on. Stripping there would silently edit agent speech.
func TestAssistantTextKeepsWhatLooksLikeAnEnvelope(t *testing.T) {
	line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"<local-command-stdout>x</local-command-stdout>"}]}}`

	ev := onlyEvent(t, line, 0)
	if ev.Kind != KindAssistantText {
		t.Fatalf("Kind = %q, want %q", ev.Kind, KindAssistantText)
	}
	if ev.Text != "<local-command-stdout>x</local-command-stdout>" {
		t.Errorf("Text = %q, want it untouched", ev.Text)
	}
}

// The narrowed half of the ruling. The raw subtype still arrives as Text -
// the passthrough is deliberate and the set is open - and Notice is the
// closed resolution a renderer switches on instead.
func TestSystemSubtypeResolvesToANoticeWithoutLosingTheSubtype(t *testing.T) {
	cases := map[string]Notice{
		"compact_boundary":  NoticeContextCompacted,
		"permission_denied": NoticeToolDenied,
		// Open set: an unmodelled subtype must still arrive as a system
		// event carrying its subtype, and earn no notice.
		"task_progress": "",
		"hook_started":  "",
	}
	for subtype, want := range cases {
		t.Run(subtype, func(t *testing.T) {
			ev := onlyEvent(t, `{"type":"system","subtype":"`+subtype+`","session_id":"s1"}`, 0)
			if ev.Kind != KindSystem {
				t.Fatalf("Kind = %q, want %q", ev.Kind, KindSystem)
			}
			if ev.Text != subtype {
				t.Errorf("Text = %q, want the raw subtype %q - the passthrough is the point", ev.Text, subtype)
			}
			if ev.Notice != want {
				t.Errorf("Notice = %q, want %q", ev.Notice, want)
			}
		})
	}
}

// A compaction is signalled on the wire by two system/status frames - the
// binary "compacting" start flag and a terminal frame carrying compact_result -
// and the airlock resolves each to a notice a reader drives a status line from.
// Both share the subtype "status", so the resolution reads the payload, not the
// subtype; and the end keys on compact_result, because a *failed* compaction
// emits no compact_boundary (slash-commands.jsonl) where a success does.
func TestCompactionStatusResolvesToNotices(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Notice
	}{
		{"start", `{"type":"system","subtype":"status","status":"compacting","session_id":"s1"}`, NoticeCompacting},
		{"end success", `{"type":"system","subtype":"status","status":null,"compact_result":"success","session_id":"s1"}`, NoticeCompacted},
		{"end failed", `{"type":"system","subtype":"status","status":null,"compact_result":"failed","session_id":"s1"}`, NoticeCompacted},
		// Other status frames share the subtype and earn no compaction notice: a
		// "requesting" heartbeat and a mode receipt (which carries permissionMode
		// on a subtype-"status" frame) both fall through to none.
		{"a requesting status earns nothing", `{"type":"system","subtype":"status","status":"requesting","session_id":"s1"}`, ""},
		{"a mode receipt earns nothing", `{"type":"system","subtype":"status","status":null,"permissionMode":"plan","session_id":"s1"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := onlyEvent(t, c.line, 0)
			if ev.Kind != KindSystem {
				t.Fatalf("Kind = %q, want %q", ev.Kind, KindSystem)
			}
			if ev.Text != "status" {
				t.Errorf("Text = %q, want the raw subtype passed through", ev.Text)
			}
			if ev.Notice != c.want {
				t.Errorf("Notice = %q, want %q", ev.Notice, c.want)
			}
		})
	}
}

// The real recorded wire bytes resolve, not just hand-written ones. A success
// (compaction.jsonl) and a failure (slash-commands.jsonl) bracket the same way,
// and the failure is the one that proves the end cannot key on the boundary: it
// carries a compact_result and no compact_boundary at all.
func TestRecordedCompactionFramesResolve(t *testing.T) {
	cases := []struct {
		fixture, marker string
		want            Notice
	}{
		{"compaction.jsonl", `"status":"compacting"`, NoticeCompacting},
		{"compaction.jsonl", `"compact_result":"success"`, NoticeCompacted},
		{"slash-commands.jsonl", `"status":"compacting"`, NoticeCompacting},
		{"slash-commands.jsonl", `"compact_result":"failed"`, NoticeCompacted},
	}
	for _, c := range cases {
		t.Run(c.fixture+" "+c.marker, func(t *testing.T) {
			line, n := findFixtureLine(t, c.fixture, c.marker)
			if got := onlyEvent(t, line, 0).Notice; got != c.want {
				t.Errorf("%s:%d resolved to %q, want %q", c.fixture, n, got, c.want)
			}
		})
	}
}

// The benign status is every sample the corpus has, and drawing it is chrome.
// Anything else is the reason an agent stalled, so it earns a notice - and
// the status itself still reaches a consumer either way.
func TestRateLimitEarnsANoticeOnlyWhenSomethingIsWrong(t *testing.T) {
	cases := map[string]Notice{
		"allowed":       "",
		"":              "",
		"rate_limited":  NoticeRateLimited,
		"unknown_state": NoticeRateLimited,
	}
	for status, want := range cases {
		t.Run("status="+status, func(t *testing.T) {
			ev := onlyEvent(t, `{"type":"rate_limit_event","session_id":"s1","rate_limit_info":{"status":"`+status+`"}}`, 0)
			if ev.Notice != want {
				t.Errorf("Notice = %q, want %q", ev.Notice, want)
			}
			if ev.Text != status {
				t.Errorf("Text = %q, want the status %q", ev.Text, status)
			}
		})
	}
}

// Task 4's pump reads with a bufio.Scanner, whose buffer is reused on the
// next Scan. If Raw aliased that buffer, every buffered event would be
// silently rewritten by the next line to arrive.
func TestDecodeRawDoesNotAliasTheCallersBuffer(t *testing.T) {
	line := []byte(`{"type":"result","session_id":"s1","result":"hello"}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	before := string(evs[0].Raw)
	if before != string(line) {
		t.Fatalf("Raw = %q, want the whole line", before)
	}

	for i := range line {
		line[i] = 'x'
	}
	if got := string(evs[0].Raw); got != before {
		t.Errorf("Raw changed with the caller's buffer: %q, want %q", got, before)
	}
}

// The header shape: which key heads a call in place of the tool's name, which
// is drawn under it, and what a folded result collapses to. Bash is the only
// tool with a title, and Read the only one with a receipt - see toolShapes for
// why an unrecorded tool gets neither.
func TestToolCallResolvesItsHeaderShape(t *testing.T) {
	cases := []struct {
		name                    string
		tool, input             string
		title, command, receipt string
	}{
		{"bash heads on its description", "Bash",
			`{"command":"ls -la","description":"Listing files"}`,
			"Listing files", "ls -la", ""},
		{"bash with no description falls back", "Bash",
			`{"command":"ls -la"}`, "", "ls -la", ""},
		{"read collapses to a count", "Read",
			`{"file_path":"auth.go"}`, "", "", "Read %d lines"},
		// Agent carries a description too and deliberately keeps the
		// name(argument) shape: nothing recorded says Claude Code heads a
		// dispatch with it, and primaryArg already puts it beside the name.
		{"agent keeps its name", "Agent",
			`{"description":"Count lines"}`, "", "", ""},
		{"an unmapped tool gets nothing", "MysteryTool",
			`{"command":"ls","description":"d"}`, "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"` +
				c.tool + `","input":` + c.input + `}]}}`
			ev := onlyEvent(t, line, 0)
			if ev.Tool == nil {
				t.Fatal("Tool is nil")
			}
			if ev.Tool.Title != c.title {
				t.Errorf("Title = %q, want %q", ev.Tool.Title, c.title)
			}
			if ev.Tool.Command != c.command {
				t.Errorf("Command = %q, want %q", ev.Tool.Command, c.command)
			}
			if ev.Tool.Receipt != c.receipt {
				t.Errorf("Receipt = %q, want %q", ev.Tool.Receipt, c.receipt)
			}
		})
	}
}

// Derived from the recording rather than asserted: the Bash header shape only
// works because every recorded call carries both halves, and a corpus where
// that stops holding fails here with the line that broke it.
func TestEveryRecordedBashCallCarriesBothHalvesOfItsHeader(t *testing.T) {
	calls := 0
	for _, path := range fixtureFiles(t) {
		for _, d := range decodeFixture(t, path) {
			if d.Kind != KindToolUse || d.Tool == nil || d.Tool.Name != "Bash" {
				continue
			}
			calls++
			if d.Tool.Title == "" {
				t.Errorf("%s:%d Bash call has no Title to head it", path, d.line)
			}
			if d.Tool.Command == "" {
				t.Errorf("%s:%d Bash call has no Command to draw under it", path, d.line)
			}
		}
	}
	if calls == 0 {
		t.Fatal("no Bash tool_use in the corpus: this test proves nothing")
	}
	t.Logf("%d recorded Bash calls, all carrying a description and a command", calls)
}
