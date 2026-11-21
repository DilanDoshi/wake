package core

import (
	"os"
	"strings"
	"testing"
)

// A skill or custom command a headless session runs is recorded as its name
// twice, as the message and as the command, then the arguments: not the text
// that was typed. Restored as it is recorded, a reopened conversation draws
// three rows of raw tags where the operator wrote "/init-orch you are …". The
// airlock hands the reader the command as it was typed. A built-in's record
// opens with <command-name> and is plumbing (isLocalCommandPlumbing), unchanged.
// rec is a skill call's record with a command name, as a JSON string.
func rec(name, args string) string {
	r := `"<command-message>x</command-message>\n<command-name>` + name + `</command-name>`
	if args != "" {
		r += `\n<command-args>` + args + `</command-args>`
	}
	return r + `"`
}

// recText is that record as the text it decodes to.
func recText(name string) string {
	return "<command-message>x</command-message>\n<command-name>" + name + "</command-name>"
}

func TestASkillCallIsReadBackAsTheCommandThatWasTyped(t *testing.T) {
	cases := map[string]struct {
		content string // the JSON value of message.content, quotes included
		want    string
	}{
		"with arguments": {
			`"<command-message>init-orch</command-message>\n<command-name>/init-orch</command-name>\n<command-args>you are responsible for the ingest branch</command-args>"`,
			"/init-orch you are responsible for the ingest branch",
		},
		"no arguments":         {`"<command-message>slow-probe</command-message>\n<command-name>/slow-probe</command-name>"`, "/slow-probe"},
		"empty arguments":      {`"<command-message>loop</command-message>\n<command-name>/loop</command-name>\n<command-args></command-args>"`, "/loop"},
		"namespaced":           {`"<command-message>codex:rescue</command-message>\n<command-name>/codex:rescue</command-name>\n<command-args>look at it</command-args>"`, "/codex:rescue look at it"},
		"multi-line arguments": {`"<command-message>x</command-message>\n<command-name>/x</command-name>\n<command-args>one\ntwo</command-args>"`, "/x one\ntwo"},
		"typed text kept":      {`"/init-orch plain text"`, "/init-orch plain text"},
		"prose naming a tag":   {`"see <command-message>init-orch</command-message> for details"`, `see <command-message>init-orch</command-message> for details`},
		"half an envelope":     {`"<command-message>init-orch</command-message>\n<command-name>/init-orch"`, `<command-message>init-orch</command-message>` + "\n" + `<command-name>/init-orch`},

		// What is not a whole record is the operator's text, kept as it is.
		"arguments trimmed":    {`"  <command-message>n</command-message>\r\n<command-name>/n</command-name>\r\n<command-args>  x  </command-args>\n "`, "/n x"},
		"a name with a space":  {rec("/a b", ""), recText("/a b")},
		"a bare slash":         {rec("/", ""), recText("/")},
		"a name with no slash": {rec("deploy", ""), recText("deploy")},
		// An escape is already contained before this reads the record; a tab is not.
		"a control character":           {`"<command-message>x</command-message>\n<command-name>/x\ty</command-name>"`, "<command-message>x</command-message>\n<command-name>/x\ty</command-name>"},
		"no message tag closed":         {`"<command-message>x\n<command-name>/x</command-name>"`, "<command-message>x\n<command-name>/x</command-name>"},
		"arguments never closed":        {`"<command-message>x</command-message>\n<command-name>/x</command-name>\n<command-args>y"`, "<command-message>x</command-message>\n<command-name>/x</command-name>\n<command-args>y"},
		"text after the arguments":      {`"<command-message>x</command-message>\n<command-name>/x</command-name>\n<command-args>y</command-args> more"`, "<command-message>x</command-message>\n<command-name>/x</command-name>\n<command-args>y</command-args> more"},
		"a closing tag with no opening": {`"<command-message>x</command-message>\n<command-name>/x</command-name>\nstray</command-args>"`, "<command-message>x</command-message>\n<command-name>/x</command-name>\nstray</command-args>"},
		"text where arguments go":       {`"<command-message>x</command-message>\n<command-name>/x</command-name>\nstray"`, "<command-message>x</command-message>\n<command-name>/x</command-name>\nstray"},
		"tags inside the arguments":     {`"<command-message>x</command-message>\n<command-name>/x</command-name>\n<command-args>a</command-args> b <command-args>c</command-args>"`, "/x a</command-args> b <command-args>c"},
		"an array block is the text":    {`[{"type":"text","text":"<command-message>x</command-message>\n<command-name>/x</command-name>\n<command-args>y</command-args>"}]`, "<command-message>x</command-message>\n<command-name>/x</command-name>\n<command-args>y</command-args>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			line := `{"type":"user","session_id":"s1","message":{"role":"user","content":` + c.content + `}}`
			evs, err := DecodeTranscriptLine([]byte(strings.Replace(line, `"session_id"`, `"uuid":"u1","session_id"`, 1)))
			if err != nil || len(evs) != 1 {
				t.Fatalf("decoded %d events (err %v), want 1", len(evs), err)
			}
			if evs[0].Kind != KindUserText || evs[0].Echoed || evs[0].Text != c.want {
				t.Errorf("read back as kind %v echoed %v %q, want your own turn %q", evs[0].Kind, evs[0].Echoed, evs[0].Text, c.want)
			}
		})
	}
}

// The shape is the recorded one: the replayed user line of a saved command and of
// a /loop with arguments, in the corpus.
func TestTheRecordedSkillCallsAreReadBackAsTyped(t *testing.T) {
	for file, want := range map[string]string{
		"../../testdata/stream/workflow-saved-command.jsonl": "/slow-probe",
		"../../testdata/stream/loop-self-paced-end.jsonl":    "/loop Reply with exactly the word tick.",
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, `<command-message>`) || !strings.Contains(line, `"type":"user"`) {
				continue
			}
			for _, ev := range decodeOne(t, line) {
				found = true
				if ev.Kind != KindUserText || !strings.HasPrefix(ev.Text, want) || strings.Contains(ev.Text, "<command") {
					t.Errorf("%s: read back as %q, want it to open %q with no tags", file, ev.Text, want)
				}
			}
		}
		if !found {
			t.Errorf("%s holds no recorded skill call to read back", file)
		}
	}
}
