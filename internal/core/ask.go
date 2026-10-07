// Interactive asks, resolved - part of the airlock; see protocol.go.
//
// One control_request subtype, can_use_tool, carries three different questions
// for the operator: an ordinary permission, a plan to approve, and a set of
// questions to answer. This file reads the ask's two wire fields and its
// payload and resolves them into AskKind and AskDetail, so nothing above the
// airlock names the keys they are spelled with. Claude asks and Wake answers:
// the answer is written by encode.go.
//
// The airlock is these seven files and nothing else in Wake knows Claude
// Code's stream-json format:
//
//	protocol.go    decoding - one wire line in, core.Events out
//	wire.go        the shapes it decodes into
//	vocabulary.go  Claude's words resolved into Wake's
//	encode.go      the frames Wake writes back
//	localreply.go  the text replies of local commands Wake parses
//	control.go     control requests Wake writes, and their receipts
//	ask.go         an interactive ask's kind and payload, resolved
//
// internal/core/airlock_test.go enforces that over the whole tree and reads
// the same list. protocol.go's header carries the full rule.

package core

// The three keys an interactive ask and its answer are spelled with.
//
// questionsKey rides the ask's input and is echoed back unchanged inside the
// answer; answersKey is the key the answer adds; questionKey is one question's
// own text, which is what answersKey maps from. All three are recorded, on the
// ask at question-answer.jsonl:37 and on the CLI's echo of the answer it
// received at :38.
//
// They are consts here rather than literals in encode.go because the decoder
// and the encoder both have to spell questionsKey the same way: askKind reads
// it to classify an ask, and answeredInput reads it to check that the answer
// is being built against the questions that were actually asked. Two spellings
// of one word across the airlock is how a decoder starts disagreeing with its
// own encoder.
const (
	questionsKey = "questions"
	answersKey   = "answers"
	questionKey  = "question"
)

// The rest of an interactive ask's payload, read only to resolve AskDetail.
//
// detailKey is Claude's "description", which vocabulary.go's toolShapes also
// reads, for a Bash call's title - one word, two unrelated jobs, one const.
// planKey is the whole payload of an ExitPlanMode ask.
//
// Deliberately absent: multiSelect, and only that. AskDetail's doc comment
// carries why, and the short version is that its answer encoding has no
// recording behind it.
const (
	optionsKey = "options"
	labelKey   = "label"
	detailKey  = "description"
	planKey    = "plan"
	headerKey  = "header"
	previewKey = "preview"
)

// askKind classifies a permission ask by what it needs from the operator. See
// AskKind for why one control_request subtype needs three answers, and why
// this reads two wire fields rather than the tool's name.
//
// The order is the argument. requires_user_interaction first, because without
// it the ask is an ordinary "may I run this" whatever its input holds - that
// is every ask in the corpus that predates the question recordings. Then the
// payload, because the flag is true on both interactive shapes and only the
// one carrying questions has an answer that can be lost.
//
// An interactive ask carrying no questions is AskApproval, which is
// ExitPlanMode's recorded behaviour and is also the safe default for an
// interactive tool nobody has recorded: it says "a human must decide this" and
// claims nothing about a payload this decoder has never seen.
func askKind(r *wireControlReq) AskKind {
	if !r.RequiresUserInteraction {
		return AskPermission
	}
	if _, ok := r.Input[questionsKey]; ok {
		return AskChoice
	}
	return AskApproval
}

// askDetail resolves what an interactive ask is putting to the operator, and
// returns nil for an ordinary permission ask, which puts nothing.
//
// Keyed on the kind askKind already resolved rather than on the payload again,
// so the two cannot disagree about what an ask is: a payload that looks like
// questions on an ask the wire did not mark interactive is not a question, and
// reading the input twice is how a decoder starts arguing with itself.
//
// A kind whose payload this build cannot read yields nil rather than an empty
// shell. Nil is the shape a renderer already has to handle - it is what every
// ordinary permission ask carries - so an unmodelled ask degrades to a yes/no
// on a named tool instead of to a card with an empty body.
func askDetail(kind AskKind, input map[string]any) *AskDetail {
	switch kind {
	case AskChoice:
		if qs := askQuestions(input); len(qs) > 0 {
			return &AskDetail{Questions: qs}
		}
	case AskApproval:
		if plan, ok := input[planKey].(string); ok && plan != "" {
			return &AskDetail{Plan: plan}
		}
	case AskPermission:
	}
	return nil
}

// askQuestions reads the questions an ask put, dropping any that carry no text
// of their own.
//
// Dropping rather than keeping an untitled one is the only safe direction: the
// text is the key an answer is keyed on, so a question with none can be shown
// and never answered - and EncodeAnswer refuses the whole answer when one
// question is unanswerable, which would take the answerable ones down with it.
func askQuestions(input map[string]any) []Question {
	raw, ok := input[questionsKey].([]any)
	if !ok {
		return nil
	}
	out := make([]Question, 0, len(raw))
	for _, q := range raw {
		obj, ok := q.(map[string]any)
		if !ok {
			continue
		}
		text, ok := obj[questionKey].(string)
		if !ok || text == "" {
			continue
		}
		// The header is presentation and the text is the key an answer is keyed
		// on, so a question with no header is still perfectly answerable and is
		// kept. Only a missing text drops one.
		header, _ := obj[headerKey].(string)
		out = append(out, Question{Text: text, Header: header, Options: askOptions(obj[optionsKey])})
	}
	return out
}

// askOptions reads one question's options. An option with no label is dropped
// for the reason an untitled question is: the label is what the answer
// carries, so an unlabelled one is a row that cannot be chosen.
func askOptions(raw any) []Option {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]Option, 0, len(list))
	for _, o := range list {
		obj, ok := o.(map[string]any)
		if !ok {
			continue
		}
		label, ok := obj[labelKey].(string)
		if !ok || label == "" {
			continue
		}
		detail, _ := obj[detailKey].(string)
		preview, _ := obj[previewKey].(string)
		out = append(out, Option{Label: label, Detail: detail, Preview: preview})
	}
	return out
}
