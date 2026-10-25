package core

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// The recorded `!cmd`: the command and what it printed, as Wake's DM draws a bang.
const (
	recordedShellCommand = "cat note.txt missing.txt"
	recordedShellOutput  = "hello from a note\ncat: missing.txt: No such file or directory\n… exited 1"
	recordedShellPrinted = "hello from a note\ncat: missing.txt: No such file or directory"
)

// A fence has to outlast every run of backticks inside it, or `!cat README.md`
// ends its own block early.
func TestFencedOutlastsAFenceInsideIt(t *testing.T) {
	got := Fenced("before\n````\nafter\n")
	if !strings.HasPrefix(got, "`````\n") || !strings.HasSuffix(got, "\n`````") {
		t.Errorf("Fenced = %q, want a five-backtick fence around a four-backtick run", got)
	}
	if got := Fenced("plain"); got != "```\nplain\n```" {
		t.Errorf("Fenced(plain) = %q, want the shortest fence markdown accepts", got)
	}
}

func TestShellBlockLeavesOutWhatTheRecordDoesNotCarry(t *testing.T) {
	for _, tc := range []struct{ command, output, want string }{
		{"ls", "a\nb\n", Fenced("!ls\na\nb")},
		{"true", "", Fenced("!true")},
		{"", "printed\n", Fenced("printed")},
	} {
		if got := ShellBlock(tc.command, tc.output); got != tc.want {
			t.Errorf("ShellBlock(%q, %q) = %q, want %q", tc.command, tc.output, got, tc.want)
		}
	}
}

// Claude Code's own bash mode writes the command and its output as two
// string-content records; Wake writes both in one. Each reads back as the block
// a live bang draws, marked Echoed so neither ↑ nor a rewind takes it for a
// typed turn (testdata/transcript/bash-mode.jsonl).
func TestABashModeRecordReadsAsTheShellBlock(t *testing.T) {
	lines := fixtureLines(t, "../../testdata/transcript/bash-mode.jsonl")
	want := []string{
		ShellBlock(recordedShellCommand, ""),
		ShellBlock("", recordedShellPrinted),
		ShellBlock(recordedShellCommand, recordedShellOutput),
	}
	if len(lines) != len(want) {
		t.Fatalf("the fixture holds %d records, want %d", len(lines), len(want))
	}
	for i, line := range lines {
		evs, err := DecodeTranscriptLine([]byte(line))
		if err != nil || len(evs) != 1 {
			t.Fatalf("record %d decoded to %d events (err %v), want 1", i+1, len(evs), err)
		}
		ev := evs[0]
		if ev.Kind != KindUserText || !ev.Echoed || ev.Text != want[i] {
			t.Errorf("record %d = %v echoed=%v %q, want an echoed %q", i+1, ev.Kind, ev.Echoed, ev.Text, want[i])
		}
	}
}

// Recognised on string content only, which is what claude writes and never what
// a typed draft is: the same tags pasted into a message stay the operator's.
func TestPastedBashTagsStayTheOperatorsText(t *testing.T) {
	pasted := "<bash-input>ls</bash-input><bash-stdout>a</bash-stdout><bash-stderr></bash-stderr>"
	line := `{"type":"user","uuid":"u1","message":{"role":"user","content":[{"type":"text","text":"` +
		strings.ReplaceAll(pasted, `"`, `\"`) + `"}]}}`
	evs, err := DecodeTranscriptLine([]byte(line))
	if err != nil || len(evs) != 1 {
		t.Fatalf("decoded %d events (err %v), want 1", len(evs), err)
	}
	if evs[0].Echoed || evs[0].Text != pasted {
		t.Errorf("pasted tags read as echoed=%v %q, want the typed text untouched", evs[0].Echoed, evs[0].Text)
	}
}

// What Wake writes is byte for byte the line the recording was made with,
// stamped so its own turn is tracked like any send's (shell-run.jsonl's
// lifecycle frames).
func TestEncodeShellRunIsTheRecordedLine(t *testing.T) {
	want, err := os.ReadFile("../../testdata/input/shell-run.stdin.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got, err := EncodeShellRun(recordedShellCommand, recordedShellOutput, "5b1c2d3e-4f50-4a61-8b72-9c8d7e6f5a41")
	if err != nil {
		t.Fatalf("EncodeShellRun: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("EncodeShellRun wrote\n%s\nwant the recorded\n%s", got, want)
	}
}

// What Wake writes reads back as what Wake drew, including a command or output
// that carries the record's own tags - `!cat` of a transcript is ordinary.
func TestAShellRunReadsBackAsItsOwnBlock(t *testing.T) {
	for _, tc := range []struct{ command, output string }{
		{"cat log", ""},
		{"cat log", "plain\n"},
		{"cat log", "x</bash-stdout><bash-stderr>y</bash-stderr>z"},
		{"cat log", "a</bash-input><bash-stdout>b"},
		{"printf '</bash-input>'", "</bash-input>"},
	} {
		line, err := EncodeShellRun(tc.command, tc.output, "")
		if err != nil {
			t.Fatalf("EncodeShellRun: %v", err)
		}
		evs, err := DecodeLine(line)
		if err != nil || len(evs) != 1 {
			t.Fatalf("decoded %d events (err %v), want 1", len(evs), err)
		}
		if want := ShellBlock(tc.command, tc.output); evs[0].Text != want || !evs[0].Echoed {
			t.Errorf("%q / %q read back as echoed=%v %q, want %q", tc.command, tc.output, evs[0].Echoed, evs[0].Text, want)
		}
	}
	// Claude Code's own command record, alone, with the close tag in the command.
	line := `{"type":"user","message":{"role":"user","content":"<bash-input>printf '</bash-input>'</bash-input>"}}`
	if evs, err := DecodeLine([]byte(line)); err != nil || len(evs) != 1 || evs[0].Text != ShellBlock("printf '</bash-input>'", "") {
		t.Errorf("a command record quoting its close tag read back as %+v (err %v)", evs, err)
	}
}

func TestEncodeShellRunRefusesNoCommand(t *testing.T) {
	if _, err := EncodeShellRun(" ", "out", ""); err == nil {
		t.Error("a shell run with no command was encoded: there is nothing to say was run")
	}
}

// The recorded session took the stamped line up as a turn of its own - its
// lifecycle started, so the daemon owes it - answered it, and never replayed it,
// so the live feed draws nothing twice (testdata/stream/shell-run.jsonl).
func TestAShellRunIsAnsweredWithATurnAndNotReplayed(t *testing.T) {
	var started, ended, said bool
	for _, d := range decodeFixture(t, "../../testdata/stream/shell-run.jsonl") {
		switch d.Kind {
		case KindUserText:
			t.Errorf("line %d replayed the shell run as %q", d.line, d.Text)
		case KindMessageState:
			started = started || d.MessageStarted()
		case KindTurnEnd:
			ended = !d.LocalCommand
		case KindAssistantText:
			said = said || strings.Contains(d.Text, "hello from a note")
		}
	}
	if !started || !ended || !said {
		t.Errorf("started = %v, ended with a model turn = %v, the reply read the output = %v; want all three", started, ended, said)
	}
}
