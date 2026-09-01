package core

// The fence between a child's words and the operator's terminal.
//
// docs/notes/bugs.md BUG-9: a claude child's text reached the drawn frame with
// its escape sequences intact, so what the model wrote was what the terminal
// executed. Measured rather than argued - an assistant block carrying
// `\x1b]52;c;…\a` set the operator's system clipboard with no keystroke, and one
// carrying `\x1b[2J\x1b[H` cleared the alt screen and homed the cursor, which is
// enough to erase Wake's frame and draw a forged room line or a forged
// permission card in its place.
//
// **It is here because this is where a child's bytes first become a Wake
// value.** Two consequences fall out of that placement and both were the reason
// for it: DecodeTranscriptLine delegates to DecodeLine, so one call covers a
// live session *and* a conversation read back off disk - including a transcript
// written by a process Wake never supervised - and every surface above sees
// only text that has already been through it. Containing at the draw sites
// instead would be roughly twenty-five call sites across the transcript, the
// cards, the roster, the task rows, the preview panel, the completion menu, the
// workspaces sidebar and the working line, with a new surface getting no fence
// by default.
//
// The recorded lesson says the same thing read the other way. `oneLine` was
// once applied to one field at construction while an uncontained neighbour was
// interpolated beside it on the same row, and one session in gave two rows out
// (cmd/wake/importrows_test.go). What that cost was not the placement but the
// coverage: containing *every* string a child authors is what makes a row safe,
// and Event.contained is written to be total rather than to be a list.
//
// Two characters are exempt, and they are the whole difference between this and
// the four oneLine-shaped fences already in the tree. Prose is legitimately
// multi-line - a markdown paragraph, a code fence, a tool result - so a fence
// that flattened `\n` would collapse every one of them into a single row, and
// the renderers that expand `\t` (render/diff.go, render/tool.go) would lose the
// alignment they expand it for. Nothing else survives: a carriage return
// redraws the row from column zero, and U+009B is a CSI with no ESC in front of
// it for a terminal in 8-bit mode.
//
// **It substitutes and never deletes**, which is internal/mcp's ruling and its
// test: a padded row measures its columns before containment runs, so a
// deletion shifts every column right of the character an agent chose to insert.
// The payload around the introducer is left alone and stays legible - `[2J`
// reads as the nonsense it is, which is honest about what arrived.

import "strings"

// lineSeparator and paragraphSeparator are structure to a renderer the way a
// newline is structure to a terminal.
const (
	lineSeparator      = '\u2028'
	paragraphSeparator = '\u2029'
)

// Contained substitutes a space for every character a terminal or a renderer
// would act on, leaving the newline and the tab that prose needs.
//
// strings.Map allocates nothing when nothing matches, which is every line of
// the recorded corpus: 2,178 of them carry no control character outside tab and
// newline, so the ordinary path is a scan and returns the original string.
//
// **Measured rather than assumed, because this runs per event.** A paragraph of
// an agent's prose costs ~2.3µs and no allocation (BenchmarkContainedOrdinaryProse),
// which at the corpus median of ~1,300 events a second across a fleet is about
// 3ms of a second - under half a percent of one core - and about a quarter of
// what decoding the line already costs (BenchmarkDecodeLineWithTheFence).
// Deliberately not optimised further: a byte-wise fast path would help, and
// nothing here is near a budget that would pay for the branch.
func Contained(s string) string {
	return strings.Map(func(r rune) rune {
		if actsOnTheTerminal(r) {
			return ' '
		}
		return r
	}, s)
}

// actsOnTheTerminal is the class, over the character rather than over a list of
// sequences. A predicate that named `\x1b[2J` and `\x1b]52` would be a fence
// somebody has to keep in step with a terminal's whole vocabulary; the
// introducer is the thing that can act, and there are only so many of them.
func actsOnTheTerminal(r rune) bool {
	if r == '\n' || r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) ||
		r == lineSeparator || r == paragraphSeparator
}

// containedAll is Contained over a slice, returning a new one.
func containedAll(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = Contained(s)
	}
	return out
}

// contained returns a copy of this event with every string a child authored put
// through Contained.
//
// One method and six plain functions rather than seven methods named `contained`:
// argvguard_test.go resolves calls by name across this package and refuses a
// name declared twice, because two functions sharing one make every resolution
// below it a guess.
//
// Explicit rather than reflective: DecodeLine runs at roughly
// 1,300 events a second across a fleet streaming at the corpus median, and a
// reflection walk per event is a cost per token. Totality is the test's job -
// TestEveryStringAChildAuthorsIsContained walks the struct and fails on a field
// that is neither contained here nor excused in notAuthoredByTheChild.
//
// Every nested value is rebuilt rather than written through, because an Event
// carries pointers and a caller may hold the one it was decoded from.
func (e Event) contained() Event {
	e.Text = Contained(e.Text)
	e.FromName = Contained(e.FromName)
	e.FromAddr = Contained(e.FromAddr)
	e.PermissionMode = Contained(e.PermissionMode)
	e.Tool = containedTool(e.Tool)
	e.Subagent = containedSubagent(e.Subagent)
	e.Task = containedTask(e.Task)
	e.Control = containedControl(e.Control)
	e.Rewind = containedRewind(e.Rewind)
	e.Session = containedFacts(e.Session)
	return e
}

func containedTool(t *ToolCall) *ToolCall {
	if t == nil {
		return nil
	}
	c := *t
	c.Name = Contained(c.Name)
	c.Display = Contained(c.Display)
	c.Title = Contained(c.Title)
	c.Command = Contained(c.Command)
	c.Receipt = Contained(c.Receipt)
	if c.Todos != nil {
		todos := make([]Todo, len(c.Todos))
		for i, td := range c.Todos {
			td.Text, td.ActiveForm = Contained(td.Text), Contained(td.ActiveForm)
			todos[i] = td
		}
		c.Todos = todos
	}
	if c.Checklist != nil {
		op := *c.Checklist
		op.ID, op.Text, op.ActiveForm = Contained(op.ID), Contained(op.Text), Contained(op.ActiveForm)
		c.Checklist = &op
	}
	if c.Diff != nil {
		d := *c.Diff
		d.Old, d.New = Contained(d.Old), Contained(d.New)
		c.Diff = &d
	}
	c.Ask = containedAsk(c.Ask)
	return &c
}

func containedAsk(a *AskDetail) *AskDetail {
	if a == nil {
		return nil
	}
	c := *a
	c.Plan = Contained(c.Plan)
	if c.Questions != nil {
		qs := make([]Question, len(c.Questions))
		for i, q := range c.Questions {
			q.Text, q.Header = Contained(q.Text), Contained(q.Header)
			if q.Options != nil {
				opts := make([]Option, len(q.Options))
				for j, o := range q.Options {
					o.Label, o.Detail, o.Preview = Contained(o.Label), Contained(o.Detail), Contained(o.Preview)
					opts[j] = o
				}
				q.Options = opts
			}
			qs[i] = q
		}
		c.Questions = qs
	}
	return &c
}

func containedSubagent(s *Subagent) *Subagent {
	if s == nil {
		return nil
	}
	c := *s
	c.Dispatch, c.Agent = Contained(c.Dispatch), Contained(c.Agent)
	c.Type, c.Task = Contained(c.Type), Contained(c.Task)
	c.Result = SubagentResult(Contained(string(c.Result)))
	return &c
}

func containedTask(t *TaskUpdate) *TaskUpdate {
	if t == nil {
		return nil
	}
	c := *t
	c.ID, c.Dispatch = Contained(c.ID), Contained(c.Dispatch)
	c.Label, c.Type, c.Tool = Contained(c.Label), Contained(c.Type), Contained(c.Tool)
	c.Kind, c.Phase = TaskKind(Contained(string(c.Kind))), TaskPhase(Contained(string(c.Phase)))
	c.Status = TaskStatus(Contained(string(c.Status)))
	return &c
}

func containedControl(r *ControlResult) *ControlResult {
	if r == nil {
		return nil
	}
	c := *r
	c.Error = Contained(c.Error)
	c.StillQueued, c.Cancelled = containedAll(c.StillQueued), containedAll(c.Cancelled)
	return &c
}

// containedRewind is containedControl's own shape: like StillQueued and
// Cancelled, the two uuids here are receipt payload rather than a key Wake
// matches on - RequestID already does that job for this receipt - so they are
// contained rather than excused.
func containedRewind(r *RewindResult) *RewindResult {
	if r == nil {
		return nil
	}
	c := *r
	c.TargetMessageUUID = Contained(c.TargetMessageUUID)
	c.PrefillText = Contained(c.PrefillText)
	c.PrecedingAssistantUUID = Contained(c.PrecedingAssistantUUID)
	c.Error = Contained(c.Error)
	return &c
}

func containedFacts(f *SessionFacts) *SessionFacts {
	if f == nil {
		return nil
	}
	c := *f
	c.Model, c.Dir = Contained(c.Model), Contained(c.Dir)
	c.SlashCommands = containedAll(c.SlashCommands)
	if c.MCPServers != nil {
		servers := make([]MCPServer, len(c.MCPServers))
		for i, s := range c.MCPServers {
			s.Name, s.State = Contained(s.Name), Contained(s.State)
			servers[i] = s
		}
		c.MCPServers = servers
	}
	return &c
}

// notAuthoredByTheChild is every string reachable from an Event that this fence
// deliberately leaves alone, and why.
//
// The rule underneath the list is one sentence: **anything Wake hands back to
// the CLI keeps its bytes, and anything Wake resolved into its own vocabulary
// never had the child's.** Everything else is contained.
//
// It also names the kinds the guard cannot walk *through* - a map's values are
// not addressable and an interface has no field to set - so an opaque field is
// a ruling here rather than a silent skip. Today that is Tool.Input alone. A field named here is exempt from
// TestEveryStringAChildAuthorsIsContained; a field that is neither contained
// nor named is a build failure, which is what makes a field added later a
// decision rather than an oversight.
//
// The paths are the reflection walk's own spelling, so they read as the struct
// does.
var notAuthoredByTheChild = map[string]string{
	// Wake's own vocabulary, assigned by the airlock from a closed set. A
	// control character here would be this package's bug rather than a
	// child's, and containing it would hide that.
	"Event.Kind":   "Wake's own vocabulary, not the wire's",
	"Event.Notice": "Wake's own vocabulary, resolved in the airlock",

	// Correlators. These are matched, never drawn: SessionID addresses a
	// frame, RequestID answers an ask, and a substitution in either would
	// break the match rather than protect a surface. They are also the two
	// ids Wake mints or echoes rather than reads as prose.
	"Event.SessionID": "an address, matched rather than drawn",
	"Event.RequestID": "the correlator an answer is answered by",
	"Event.MessageID": "a boundary marker, matched rather than drawn",

	// Re-encoded rather than rendered, both of them. The id is what an answer is
	// addressed to, and Input is what an allow sends back as updatedInput - a
	// substitution there would change what the model is told it may run, and
	// nothing draws it (internal/ui reads Name and Display).
	"Event.Tool.ID":    "the tool_use_id an answer is addressed to",
	"Event.Tool.Input": "sent back to the CLI as updatedInput, never drawn",

	// Wake's own vocabulary a second time, and these three are the ones the
	// guard caught rather than the ones anybody remembered. AskKind is resolved
	// by askKind from the payload's *shape* - never from the tool's name - and
	// both statuses go through todoStatus, which maps anything unrecognised to
	// TodoPending. None of the three carries a byte the child chose.
	"Event.Ask":                   "resolved by askKind from the payload shape",
	"Event.Tool.Todos[].Status":   "resolved by todoStatus into Wake's closed set",
	"Event.Tool.Checklist.Status": "resolved by todoStatus into Wake's closed set",
}
