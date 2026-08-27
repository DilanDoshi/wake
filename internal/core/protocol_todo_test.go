// The task list a TodoWrite carries. See fixtures_helpers_test.go for why this
// file may name Claude's frame types.
//
// Driven by hand-written frames rather than by the corpus, and that is recorded
// in airlock_test.go's notInTheCorpus rather than hidden here: no session in
// testdata/stream ever called the tool, so the shape comes from the shipped
// binary's own description of it.

package core

import "testing"

func todoFrame(t *testing.T, input string) *ToolCall {
	t.Helper()
	line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":` +
		`[{"type":"tool_use","id":"toolu_1","name":"TodoWrite","input":` + input + `}]}}`
	evs := decodeLineT(t, line)
	for _, ev := range evs {
		if ev.Kind == KindToolUse {
			return ev.Tool
		}
	}
	t.Fatalf("no tool call decoded from %s", line)
	return nil
}

func TestATaskListDecodesInOrder(t *testing.T) {
	tool := todoFrame(t, `{"todos":[
		{"content":"Airlock: encode set_permission_mode","status":"in_progress","activeForm":"Building the airlock"},
		{"content":"core.Session.SetMode","status":"pending","activeForm":"Wiring core"},
		{"content":"The key and the cycle","status":"completed","activeForm":"Binding the key"}]}`)

	if len(tool.Todos) != 3 {
		t.Fatalf("decoded %d items, want 3: %+v", len(tool.Todos), tool.Todos)
	}
	for i, want := range []TodoStatus{TodoActive, TodoPending, TodoDone} {
		if tool.Todos[i].Status != want {
			t.Errorf("item %d is %q, want %q", i, tool.Todos[i].Status, want)
		}
	}
	if tool.Todos[0].Text != "Airlock: encode set_permission_mode" {
		t.Errorf("the first item's text is %q", tool.Todos[0].Text)
	}
	if tool.Todos[0].ActiveForm != "Building the airlock" {
		t.Errorf("the first item's active form is %q", tool.Todos[0].ActiveForm)
	}
}

// The set is Claude's and can grow. An item nobody has ruled on is one still to
// do - never a fourth state no renderer knows how to draw.
func TestAnUnknownTodoStatusIsPending(t *testing.T) {
	tool := todoFrame(t, `{"todos":[{"content":"something","status":"deferred"}]}`)
	if got := tool.Todos[0].Status; got != TodoPending {
		t.Errorf("an unrecognised status decoded to %q, want %q", got, TodoPending)
	}
}

// Written to survive being wrong about the envelope: anything that is not a
// list of objects with text yields nil rather than a partial list.
func TestAMalformedTaskListIsNoList(t *testing.T) {
	for _, input := range []string{
		`{"todos":[]}`,
		`{"todos":"not a list"}`,
		`{"todos":[{"status":"pending"}]}`,
		`{"todos":[null,7,"x"]}`,
		`{}`,
	} {
		if tool := todoFrame(t, input); tool.Todos != nil {
			t.Errorf("input %s decoded to %+v, want no list", input, tool.Todos)
		}
	}
}

// An entry with no text is dropped and the rest survive: a blank row in a
// checklist reads as a task nobody named.
func TestATaskListDropsOnlyTheNamelessItems(t *testing.T) {
	tool := todoFrame(t, `{"todos":[{"content":"","status":"pending"},{"content":"real","status":"pending"}]}`)
	if len(tool.Todos) != 1 || tool.Todos[0].Text != "real" {
		t.Errorf("decoded %+v, want only the named item", tool.Todos)
	}
}

// Every other tool carries no list, so nothing else grows a checklist.
func TestOnlyATaskListDecodesToTodos(t *testing.T) {
	line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":` +
		`[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`
	for _, ev := range decodeLineT(t, line) {
		if ev.Tool != nil && ev.Tool.Todos != nil {
			t.Errorf("a Bash call decoded a task list: %+v", ev.Tool.Todos)
		}
	}
}
