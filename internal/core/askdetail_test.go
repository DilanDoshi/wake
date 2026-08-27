// What an interactive ask is *asking*: the questions with their options, and
// the plan.
//
// ask_test.go beside this one covers the other half - which of the three kinds
// an ask is, classified from two wire fields and never from a tool's name.
// This covers what that kind is asking, which is the half a client cannot
// answer without: an answer is keyed on a question's own text and carries an
// option's own label, and both live inside ToolCall.Input, which nothing above
// the airlock may index.
//
// Every expectation here is read out of the recorded line the assertion is
// about, never copied into this file. A hand-written "What output format
// should the report use?" would pin one recording's prose and prove nothing
// about the resolution - and it would keep passing if the resolver returned a
// constant. So the test parses the fixture's own input with encoding/json and
// requires the decoder to have found the same thing.
//
// This file is one of the airlock's own tests, so it may name Claude's frame
// types - and only ever to prove the airlock decodes them. See
// fixtures_helpers_test.go's header for the full list.

package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// askFixtures are the six recordings of an interactive ask. Named rather than
// globbed because each one is here for a different shape, and a glob would
// stop saying which.
const (
	askChoiceFixture  = "question-answer.jsonl"
	askPlanFixture    = "question-plan.jsonl"
	permissionFixture = "permission.jsonl"
)

// recordedAsks decodes every permission ask in one fixture, paired with the
// raw input the wire carried for it. The pairing is what lets an assertion
// compare Wake's resolution against Claude's own bytes on the same line.
func recordedAsks(t *testing.T, fixture string) []askPair {
	t.Helper()
	var out []askPair
	for n, line := range fixtureLines(t, "../../testdata/stream/"+fixture) {
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s:%d: %v", fixture, n+1, err)
		}
		for _, ev := range evs {
			if ev.Kind != KindPermissionRequest {
				continue
			}
			out = append(out, askPair{ev: ev, raw: rawAskInput(t, line)})
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s carries no permission ask: this test is asserting nothing", fixture)
	}
	return out
}

type askPair struct {
	ev  Event
	raw map[string]any
}

// rawAskInput pulls the ask's input straight off the recorded line, so an
// expectation is the wire's own and not this file's memory of it.
func rawAskInput(t *testing.T, line string) map[string]any {
	t.Helper()
	var f struct {
		Request struct {
			Input map[string]any `json:"input"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		t.Fatalf("re-reading the recorded ask: %v", err)
	}
	return f.Request.Input
}

// A question ask has to arrive with its questions and their options already
// resolved. Nothing above the airlock may index ToolCall.Input, so an
// unresolved payload is an operator choosing between options they cannot see -
// and, worse, a client that cannot key an answer to anything.
func TestAQuestionAskResolvesTheQuestionsAndOptionsTheWireCarried(t *testing.T) {
	for _, p := range recordedAsks(t, askChoiceFixture) {
		if p.ev.Ask != AskChoice {
			t.Fatalf("ask kind = %q, want %q", p.ev.Ask, AskChoice)
		}
		if p.ev.Tool == nil || p.ev.Tool.Ask == nil {
			t.Fatal("an AskChoice arrived with no resolved payload: nothing above the airlock can read its questions, so the operator sees a bare tool name and the answer cannot be keyed to anything")
		}

		want := rawQuestions(t, p.raw)
		got := p.ev.Tool.Ask.Questions
		if len(got) != len(want) {
			t.Fatalf("resolved %d questions, the wire carried %d", len(got), len(want))
		}
		for i := range want {
			if got[i].Text != want[i].text {
				t.Errorf("question %d resolved to %q, the wire says %q: the text is the key an answer is keyed on, so a resolution that differs by one byte answers nothing", i, got[i].Text, want[i].text)
			}
			if got[i].Header != want[i].header {
				t.Errorf("question %d resolved to header %q, the wire says %q: the chip is what tells one question of four from the next", i, got[i].Header, want[i].header)
			}
			assertOptions(t, got[i].Options, want[i].options)
		}
	}
}

func assertOptions(t *testing.T, got []Option, want []rawOption) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("resolved %d options, the wire carried %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Label != want[i].label {
			t.Errorf("option %d resolved to label %q, the wire says %q: the label is what the answer carries", i, got[i].Label, want[i].label)
		}
		if got[i].Detail != want[i].detail {
			t.Errorf("option %d resolved to detail %q, the wire says %q", i, got[i].Detail, want[i].detail)
		}
		if got[i].Preview != want[i].preview {
			t.Errorf("option %d resolved to preview %q, the wire says %q: the preview is what an option looks like applied, and two labels can be equally plausible until you see it", i, got[i].Preview, want[i].preview)
		}
	}
}

type rawQuestion struct {
	text    string
	header  string
	options []rawOption
}

type rawOption struct{ label, detail, preview string }

// rawQuestions reads the questions out of the recorded input with no help
// from the code under test.
func rawQuestions(t *testing.T, input map[string]any) []rawQuestion {
	t.Helper()
	blob, err := json.Marshal(input["questions"])
	if err != nil {
		t.Fatalf("re-marshalling the recorded questions: %v", err)
	}
	var qs []struct {
		Question string `json:"question"`
		Header   string `json:"header"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
			Preview     string `json:"preview"`
		} `json:"options"`
	}
	if err := json.Unmarshal(blob, &qs); err != nil {
		t.Fatalf("re-reading the recorded questions: %v", err)
	}
	if len(qs) == 0 {
		t.Fatal("the recorded ask carries no questions: this fixture cannot produce the condition")
	}
	out := make([]rawQuestion, 0, len(qs))
	for _, q := range qs {
		opts := make([]rawOption, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, rawOption{label: o.Label, detail: o.Description, preview: o.Preview})
		}
		out = append(out, rawQuestion{text: q.Question, header: q.Header, options: opts})
	}
	return out
}

// A plan ask carries a document, and a card that cannot show it leaves the
// operator approving something they have not read.
func TestAPlanAskResolvesThePlanTheWireCarried(t *testing.T) {
	plans := 0
	for _, p := range recordedAsks(t, askPlanFixture) {
		if p.ev.Ask != AskApproval {
			continue
		}
		plans++
		want, _ := p.raw["plan"].(string)
		if want == "" {
			t.Fatal("the recorded plan ask carries no plan: this fixture cannot produce the condition")
		}
		if p.ev.Tool == nil || p.ev.Tool.Ask == nil || p.ev.Tool.Ask.Plan != want {
			t.Errorf("the plan did not survive the airlock: a plan card would show an empty body and the operator would approve a document they never saw")
		}
		if len(p.ev.Tool.Ask.Questions) != 0 {
			t.Errorf("a plan ask resolved %d questions: an approval has nothing to choose between, and a card offering a choice on one would send an answer to a tool that reads none", len(p.ev.Tool.Ask.Questions))
		}
	}
	if plans == 0 {
		t.Fatalf("%s produced no AskApproval: this test is asserting nothing", askPlanFixture)
	}
}

// An ordinary permission ask resolves nothing, and that is the whole point of
// the pointer being nil: a renderer keys on it and gets the yes/no shape.
func TestAnOrdinaryPermissionAskResolvesNoPayloadAtAll(t *testing.T) {
	for _, p := range recordedAsks(t, permissionFixture) {
		if p.ev.Ask != AskPermission {
			t.Fatalf("ask kind = %q, want the zero value", p.ev.Ask)
		}
		if p.ev.Tool != nil && p.ev.Tool.Ask != nil {
			t.Error("a plain permission ask carries a resolved payload: nothing on the wire licensed one, and a card would draw a question with no questions in it")
		}
	}
}

// A question with no text of its own cannot be keyed to, and an option with no
// label carries nothing - so neither is offered.
//
// The line is hand-built, and it has to be: no recording carries either shape,
// so the corpus cannot produce the condition and a test driven from it would
// pass over nothing. This is the same narrow exemption protocol_test.go takes
// for wire shapes the fixtures do not hold.
//
// What the guards buy is an honest card rather than a deliverable answer:
// EncodeAnswer reads the *raw* input, so an ask carrying an untitled question
// is refused whatever this resolves. Without the guards the operator is shown a
// blank row with nothing to choose and an option that would answer with an
// empty string; with them the card shows only what can actually be answered,
// and the refusal - which arrives either way - is the one thing left to report.
func TestAQuestionOrOptionThatCannotBeKeyedToIsNotOffered(t *testing.T) {
	const line = `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool",` +
		`"requires_user_interaction":true,"input":{"questions":[` +
		`{"question":"","options":[{"label":"lost"}]},` +
		`{"question":"which one?","options":[{"label":"a"},{"description":"no label at all"},{"label":"b"}]}` +
		`]}}}`

	evs, err := DecodeLine([]byte(line))
	if err != nil {
		t.Fatalf("decoding the hand-built ask: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindPermissionRequest || evs[0].Ask != AskChoice {
		t.Fatalf("the hand-built line did not decode as a choice ask: %+v", evs)
	}

	got := evs[0].Tool.Ask
	if got == nil || len(got.Questions) != 1 {
		t.Fatalf("resolved %v, want the one question that carries text: a question an answer cannot be keyed to is a row the operator can look at and never answer", got)
	}
	if got.Questions[0].Text != "which one?" {
		t.Errorf("resolved question %q, want the one with text", got.Questions[0].Text)
	}
	if len(got.Questions[0].Options) != 2 {
		t.Fatalf("resolved %d options, want 2: an option with no label answers with an empty string, which the encoder refuses as a blank choice", len(got.Questions[0].Options))
	}
	for _, o := range got.Questions[0].Options {
		if o.Label != "a" && o.Label != "b" {
			t.Errorf("resolved an option labelled %q", o.Label)
		}
	}
}

// The resolution and the encoder have to agree about what a question is
// called, and this is the only test that can prove it: the answer is keyed on
// the question's text, the encoder reads that text out of the ask's own input,
// and the card reads it out of the resolved payload. One byte of difference
// between those two paths and every answer is refused - or worse, accepted
// against a question the ask never put.
//
// Driven over every recorded question ask rather than one, and it builds the
// answer the way a card does: one label per question, taken from the resolved
// options.
func TestAnAnswerBuiltFromTheResolvedPayloadIsAcceptedByTheEncoder(t *testing.T) {
	asks := 0
	for _, fixture := range []string{askChoiceFixture, askPlanFixture, "question-bare-allow.jsonl", "question-park-close.jsonl", "question-park-long.jsonl", "question-plan-bare.jsonl"} {
		for _, p := range recordedAsks(t, fixture) {
			if p.ev.Ask != AskChoice {
				continue
			}
			asks++
			answers := map[string]string{}
			for _, q := range p.ev.Tool.Ask.Questions {
				if len(q.Options) == 0 {
					t.Fatalf("%s: a resolved question offers no options, so a card has nothing to answer with", fixture)
				}
				answers[q.Text] = q.Options[0].Label
			}
			line, err := EncodeAnswer(p.ev.RequestID, p.ev.Tool.Input, answers)
			if err != nil {
				t.Errorf("%s: an answer built from the resolved payload was refused: %v\nThe resolution and EncodeAnswer disagree about what a question is called, which makes every answer this UI can build undeliverable", fixture, err)
				continue
			}
			if !strings.Contains(string(line), `"answers"`) {
				t.Errorf("%s: the encoded answer carries no answers key", fixture)
			}
		}
	}
	if asks == 0 {
		t.Fatal("no recorded question ask was exercised: this test is asserting nothing")
	}
}

// Corpus-wide, and the reason it is separate from the assertions above: those
// read one fixture each and a resolver that special-cased them would pass. This
// requires the property of every ask the recordings hold.
func TestEveryRecordedInteractiveAskResolvesSomethingAnOperatorCanAnswer(t *testing.T) {
	choices, approvals := 0, 0
	for _, path := range fixtureFiles(t) {
		for n, line := range fixtureLines(t, path) {
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d: %v", path, n+1, err)
			}
			for _, ev := range evs {
				switch ev.Ask {
				case AskChoice:
					choices++
					assertAnswerable(t, path, n+1, ev)
				case AskApproval:
					approvals++
					if ev.Tool == nil || ev.Tool.Ask == nil || strings.TrimSpace(ev.Tool.Ask.Plan) == "" {
						t.Errorf("%s:%d is an approval with no plan resolved", path, n+1)
					}
				}
			}
		}
	}
	if choices == 0 || approvals == 0 {
		t.Fatalf("corpus produced %d choice asks and %d approvals: the sweep found nothing to check", choices, approvals)
	}
}

func assertAnswerable(t *testing.T, path string, line int, ev Event) {
	t.Helper()
	if ev.Tool == nil || ev.Tool.Ask == nil || len(ev.Tool.Ask.Questions) == 0 {
		t.Errorf("%s:%d is a choice ask with no questions resolved", path, line)
		return
	}
	for i, q := range ev.Tool.Ask.Questions {
		if strings.TrimSpace(q.Text) == "" {
			t.Errorf("%s:%d question %d resolved with no text: an answer cannot be keyed to it", path, line, i)
		}
		if len(q.Options) < 2 {
			t.Errorf("%s:%d question %d resolved %d options; the tool's schema says 2-4", path, line, i, len(q.Options))
		}
		for j, o := range q.Options {
			if strings.TrimSpace(o.Label) == "" {
				t.Errorf("%s:%d question %d option %d resolved with no label: it is what the answer carries", path, line, i, j)
			}
		}
	}
}
