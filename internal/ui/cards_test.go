package ui

// The three shapes, and the one that can be answered wrongly in silence.
//
// Two habits shape this file and both are the project's own:
//
//   - The shape table is derived, not written. core declares three AskKinds
//     and this package declares three CardShapes; a hand-written map between
//     them would pin what already exists and say nothing about a fourth, which
//     is the failure decisions.md names "a hand-written list standing in for
//     something the code already declares". So both sets come out of the
//     source with go/ast and the assertion is a bijection.
//
//   - The answer assertions are made on what the far side received, not on the
//     frame this package minted. The defect this whole mechanism exists to
//     prevent emitted a perfectly well-formed frame and a perfectly successful
//     turn; every field a unit test could reach said the answer arrived. So
//     the frames here are run through the encoder the daemon runs them
//     through, and the assertion is on the bytes that would reach stdin.

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The recordings this file drives. A card built from a hand-written ask is a
// card built from what somebody remembers the wire looks like; these are the
// six lines the spike recorded.
const (
	choiceFixture    = "question-answer.jsonl"
	planFixture      = "question-plan.jsonl"
	permFixture      = "permission.jsonl"
	bareAllowFixture = "question-bare-allow.jsonl"
)

// recordedAsks decodes every permission ask in one recording.
//
// It fails rather than returning an empty slice: a fixture that stopped
// carrying an ask would turn every loop below into a pass over nothing, which
// is the shape of a test that cannot fail.
func recordedAsks(t testing.TB, fixture string) []core.Event {
	t.Helper()
	blob, err := os.ReadFile(filepath.Join("..", "..", "testdata", "stream", fixture))
	if err != nil {
		t.Fatalf("reading %s: %v", fixture, err)
	}
	var out []core.Event
	for _, line := range splitLines(string(blob)) {
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s: %v", fixture, err)
		}
		for _, ev := range evs {
			if ev.Kind == core.KindPermissionRequest {
				out = append(out, ev)
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s carries no permission ask: this test is asserting nothing", fixture)
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	for _, line := range bytesLines(s) {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func bytesLines(s string) []string {
	var out []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// cardFor promotes one recorded ask and hands back the card the room would
// draw for it.
func cardFor(t testing.TB, ev core.Event) Card {
	t.Helper()
	c, ok := Cards{}.Add("s1", ev).Top()
	if !ok {
		t.Fatal("a recorded permission ask produced no card")
	}
	return c
}

// One mechanism, three shapes - and the mapping between them is derived from
// both declarations rather than restated here.
//
// A fourth AskKind added to core with no case in Shape falls to the default
// and collides with AskPermission, so the injectivity check fails. A fourth
// CardShape with no kind reaching it fails the count. Neither is something a
// hand-written table of the three that exist today could notice.
func TestOneMechanismCarriesThreeShapes(t *testing.T) {
	kinds := constValuesOfType(t, filepath.Join("..", "core", "event.go"), "AskKind")
	if len(kinds) < 3 {
		t.Fatalf("found %d AskKind constants in core: the scan is broken and this test is asserting nothing", len(kinds))
	}
	shapes := constCountOfType(t, "cards.go", "CardShape")
	if shapes < 3 {
		t.Fatalf("found %d CardShape constants: the scan is broken", shapes)
	}
	if len(kinds) != shapes {
		t.Fatalf("core declares %d ask kinds and this package draws %d shapes: one of them has grown a case the other has not, and the odd one out renders as whatever the default happens to be", len(kinds), shapes)
	}

	seen := map[CardShape]string{}
	for name, value := range kinds {
		got := Card{Ask: core.AskKind(value)}.Shape()
		if other, clash := seen[got]; clash {
			t.Errorf("core.%s and core.%s both draw as shape %d: two asks answered in different ways are being offered the same card, which is exactly the collision that made a bare allow right for one tool and wrong for the other", name, other, got)
		}
		seen[got] = name
	}
	if len(seen) != shapes {
		t.Errorf("%d ask kinds reached %d of %d shapes: a shape nothing produces is dead code, and a kind that shares one is a card that answers wrongly", len(kinds), len(seen), shapes)
	}
}

// The assertion that matters most, made where it can be made: on the bytes
// that would reach the agent's stdin.
//
// An answered question and a bare allow differ nowhere a client can see. Both
// are well-formed, both are accepted, both end the turn subtype "success" with
// permission_denials empty - and one of them tells the model "The user did not
// answer the questions." So a test that stops at the frame this package minted
// is a test of the wrong thing.
func TestAQuestionsAnswerReachesTheWireAndABarePermissionAllowStillDoesNot(t *testing.T) {
	ask := recordedAsks(t, choiceFixture)[0]
	card := cardFor(t, ask)
	for i := range card.Detail.Questions {
		card = card.Pick(i, 0)
	}

	f := card.Allow()
	if f.Kind != rpc.FrameAnswer {
		t.Fatalf("a question was settled with a %q frame. Only rpc.FrameAnswer carries the choices; an allow on this ask runs the tool and tells the model nobody replied, on a turn that still ends successfully with nothing anywhere reporting the loss", f.Kind)
	}
	if f.RequestID != ask.RequestID {
		t.Errorf("the answer names request %q, the ask is %q", f.RequestID, ask.RequestID)
	}

	got := answersOnTheWire(t, f)
	want := map[string]string{}
	for _, q := range card.Detail.Questions {
		want[q.Text] = q.Options[0].Label
	}
	if len(got) != len(want) {
		t.Fatalf("the wire carried %d answers, the ask put %d questions", len(got), len(want))
	}
	for q, label := range want {
		if got[q] != label {
			t.Errorf("the wire carried %q for %q, want %q", got[q], q, label)
		}
	}

	perm := cardFor(t, recordedAsks(t, permFixture)[0])
	if pf := perm.Allow(); pf.Kind != rpc.FrameAllow || pf.UpdatedInput != nil || pf.Answers != nil {
		t.Errorf("a permission allow = %+v. Absent means \"run it exactly as it asked\", which is the only shape with a recording behind it; sending {} instead was never tested", pf)
	}

	plan := planCard(t)
	if pf := plan.Allow(); pf.Kind != rpc.FrameAllow || pf.UpdatedInput != nil || pf.Answers != nil {
		t.Errorf("a plan approval = %+v. A bare allow is a complete and correct approval for a plan - the one accidental recording of a wrong shape being accepted is not a licence to send it", pf)
	}
}

// planCard is the recorded ExitPlanMode ask, promoted.
func planCard(t *testing.T) Card { return cardFor(t, planAsk(t)) }

// planAsk is the recorded ExitPlanMode permission request - the event a plan
// card is promoted from, and the one a test seats into an App to pin one.
func planAsk(t testing.TB) core.Event {
	t.Helper()
	for _, ev := range recordedAsks(t, planFixture) {
		if ev.Ask == core.AskApproval {
			return ev
		}
	}
	t.Fatalf("%s carries no plan ask: this test is asserting nothing", planFixture)
	return core.Event{}
}

// answersOnTheWire runs the frame through the encoder the daemon runs it
// through and reads the answers back out of the control_response.
//
// This is the far side. core.Session.AnswerQuestion calls exactly this, so a
// frame that survives here is a frame that reaches the agent - and one that
// does not is refused with nothing written, which is the designed outcome and
// not a silent loss.
func answersOnTheWire(t *testing.T, f rpc.Frame) map[string]string {
	t.Helper()
	line, err := core.EncodeAnswer(f.RequestID, f.UpdatedInput, f.Answers)
	if err != nil {
		t.Fatalf("the answer this card minted was refused by the encoder and nothing reached stdin: %v", err)
	}
	var out struct {
		Response struct {
			Response struct {
				UpdatedInput struct {
					Answers map[string]string `json:"answers"`
				} `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal(line, &out); err != nil {
		t.Fatalf("re-reading the encoded answer: %v", err)
	}
	return out.Response.Response.UpdatedInput.Answers
}

// The answer's schema is question text to chosen label. Not an index, not an
// object, and not the header the tool also carries - and the text has to be
// the ask's own, because the encoder checks it against the questions the ask
// actually put and refuses the whole answer when one does not match.
func TestAnAnswerIsKeyedOnQuestionTextAndCarriesLabelsNotIndices(t *testing.T) {
	ask := recordedAsks(t, choiceFixture)[0]
	card := cardFor(t, ask)
	if len(card.Detail.Questions) < 2 {
		t.Fatalf("the recorded ask puts %d questions; this test needs two to tell a per-question answer from a single one", len(card.Detail.Questions))
	}
	// Deliberately different indices per question: an implementation that
	// answered every question with option zero, or that keyed the map on the
	// question's position, would pass a test that picked the same one twice.
	card = card.Pick(0, 1).Pick(1, 0)

	got := answersOnTheWire(t, card.Allow())
	q0, q1 := card.Detail.Questions[0], card.Detail.Questions[1]
	if got[q0.Text] != q0.Options[1].Label {
		t.Errorf("answer for %q = %q, want %q", q0.Text, got[q0.Text], q0.Options[1].Label)
	}
	if got[q1.Text] != q1.Options[0].Label {
		t.Errorf("answer for %q = %q, want %q", q1.Text, got[q1.Text], q1.Options[0].Label)
	}
}

// A question nobody answered must not go out as an answer, and must not go out
// as a bare allow either. The encoder refuses it with nothing written, which
// leaves the ask outstanding and still answerable - the opposite of the
// failure this mechanism exists to close, where the frame is accepted and the
// answer is gone.
func TestAQuestionNobodyAnsweredIsRefusedRatherThanDeliveredEmpty(t *testing.T) {
	card := cardFor(t, recordedAsks(t, bareAllowFixture)[0])
	if card.Answered() {
		t.Fatal("a freshly promoted question card reports itself answered before anybody chose anything")
	}

	f := card.Allow()
	if f.Kind == rpc.FrameAllow {
		t.Fatal("an unanswered question minted a bare allow: the tool runs, the model is told nobody answered, and the turn ends successfully with nothing reporting it")
	}
	_, err := core.EncodeAnswer(f.RequestID, f.UpdatedInput, f.Answers)
	if !errors.Is(err, core.ErrNotWritten) {
		t.Errorf("an incomplete answer encoded to %v, want an error wrapping core.ErrNotWritten: a refusal is what keeps the ask outstanding instead of settling it with an empty reply", err)
	}

	// One question short is the same defect arriving one question at a time.
	partial := card.Pick(0, 0)
	if len(partial.Detail.Questions) > 1 {
		if partial.Answered() {
			t.Error("a card with one of two questions chosen reports itself answered")
		}
		if _, err := core.EncodeAnswer(partial.Allow().RequestID, partial.Allow().UpdatedInput, partial.Allow().Answers); !errors.Is(err, core.ErrNotWritten) {
			t.Errorf("a partial answer encoded to %v, want a refusal", err)
		}
	}
}

// An option the question does not offer is not a choice, and accepting one
// would leave the card reporting itself answered while the answer it builds is
// short a question - so the operator presses the key and is handed a refusal
// they did nothing to earn.
func TestPickIgnoresAnOptionTheQuestionDoesNotOffer(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	beyond := len(card.Detail.Questions[0].Options)

	for _, o := range []int{-1, beyond, beyond + 5} {
		got := card.Pick(0, o)
		if got.chosen(0) != noChoice {
			t.Errorf("Pick(0, %d) on a question offering %d options recorded choice %d", o, beyond, got.chosen(0))
		}
		if got.Answered() {
			t.Errorf("Pick(0, %d) left the card claiming to be answered: the answer it builds omits that question, so the encoder refuses the whole thing", o)
		}
	}
	if card.Pick(len(card.Detail.Questions), 0).Answered() {
		t.Error("a choice made against a question the ask did not put was accepted")
	}
}

// A card whose choices are out of step with its questions must read as
// unanswered, not as answered with option zero.
//
// Reachable: an ask learned from a fleet report carries no choices at all, and
// nothing stops a later change from giving one a payload. Option zero is the
// worst available default - it is a real label, so it would be delivered, and
// the operator would never know a choice had been made for them.
func TestACardWhoseChoicesAreOutOfStepWithItsQuestionsReportsNothingChosen(t *testing.T) {
	full := cardFor(t, recordedAsks(t, choiceFixture)[0])
	bare := full
	bare.Choice = nil

	for i := range bare.Detail.Questions {
		if bare.chosen(i) != noChoice {
			t.Errorf("question %d reads as choice %d on a card carrying no choices at all", i, bare.chosen(i))
		}
		if bare.picked(i, 0) {
			t.Errorf("question %d draws option zero as chosen on a card nobody has answered", i)
		}
	}
	if bare.Answered() {
		t.Error("a card carrying no choices reports itself answered: the operator would be offered a key that mints an answer nobody gave")
	}
	if bare.Allow().Answers != nil {
		t.Error("a card carrying no choices minted an answer anyway")
	}
}

// Every recorded question, not the one the tests above happen to use. A card
// that minted the right frame for the first ask in one fixture and a bare
// allow for the rest would pass everything above.
func TestNoRecordedQuestionCanBeSettledWithABareAllow(t *testing.T) {
	questions := 0
	for _, fixture := range []string{choiceFixture, planFixture, bareAllowFixture, "question-park-close.jsonl", "question-park-long.jsonl", "question-plan-bare.jsonl"} {
		for _, ev := range recordedAsks(t, fixture) {
			card := cardFor(t, ev)
			if card.Shape() != ShapeQuestion {
				continue
			}
			questions++
			for i := range card.Detail.Questions {
				card = card.Pick(i, 0)
			}
			if f := card.Allow(); f.Kind != rpc.FrameAnswer {
				t.Errorf("%s: an answered question minted a %q frame", fixture, f.Kind)
				continue
			}
			answersOnTheWire(t, card.Allow())
		}
	}
	if questions == 0 {
		t.Fatal("no recorded question ask was exercised: this test is asserting nothing")
	}
}

// Deny carries prose the model reads verbatim, so it is the one channel for
// saying what to do instead of retrying the identical call.
func TestADenyNamesTheAskAndCarriesTheReasonToTheModel(t *testing.T) {
	card := cardFor(t, recordedAsks(t, permFixture)[0])
	f := card.Deny("write it under reports/ instead")
	if f.Kind != rpc.FrameDeny || f.RequestID != card.RequestID || f.SessionID != card.AgentID {
		t.Fatalf("deny frame = %+v", f)
	}
	if f.Reason != "write it under reports/ instead" {
		t.Errorf("reason = %q: it reaches the model verbatim, so anything this layer adds or trims is a word the agent reads", f.Reason)
	}
	if f.UpdatedInput != nil || f.Answers != nil {
		t.Error("a deny carried an answer: neither field means anything on a refusal")
	}
}

// --- deriving a declaration rather than restating it -------------------------

// constValuesOfType returns every constant of the named type declared in a
// file, as name -> value. Values only make sense for the string-valued kinds
// this file asks about; an iota block is counted by constCountOfType instead.
func constValuesOfType(t *testing.T, path, typeName string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, spec := range constSpecsOfType(t, path, typeName) {
		for i, name := range spec.Names {
			if i >= len(spec.Values) {
				continue
			}
			lit, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("%s: unquoting %s: %v", path, name.Name, err)
			}
			out[name.Name] = v
		}
	}
	return out
}

// constCountOfType counts the constants in the block that declares typeName.
// An iota block names its type once, on the first spec, so the whole block is
// the answer rather than the specs that repeat it.
func constCountOfType(t *testing.T, path, typeName string) int {
	t.Helper()
	n := 0
	for _, spec := range constSpecsOfType(t, path, typeName) {
		n += len(spec.Names)
	}
	return n
}

func constSpecsOfType(t *testing.T, path, typeName string) []*ast.ValueSpec {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var out []*ast.ValueSpec
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST || !blockDeclares(gen, typeName) {
			continue
		}
		for _, spec := range gen.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok {
				out = append(out, vs)
			}
		}
	}
	return out
}

// blockDeclares reports whether any spec in the block names typeName, which is
// how an iota block is recognised from its first line alone.
func blockDeclares(gen *ast.GenDecl, typeName string) bool {
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if id, ok := vs.Type.(*ast.Ident); ok && id.Name == typeName {
			return true
		}
	}
	return false
}
