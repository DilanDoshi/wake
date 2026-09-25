package core

// Wake's own vocabulary for Claude Code's native /loop. Headless the /loop slash
// is not a command - the model reproduces it with the bundled scheduler tools -
// so Wake reads the tool calls it makes: a recurring CronCreate (a fixed cron
// cadence) and ScheduleWakeup (self-paced, Claude picks each delay). This file
// names none of Claude's JSON; vocabulary.go's toolLoopOp is the recognizer
// behind the airlock, and it rides a KindToolUse event as ToolCall.Loop the way
// a checklist op does.

// LoopKind is which native scheduler a loop rides.
type LoopKind string

const (
	// LoopFixed is a recurring CronCreate - a fixed cron cadence.
	LoopFixed LoopKind = "fixed"
	// LoopSelfPaced is a ScheduleWakeup - Claude chooses each delay.
	LoopSelfPaced LoopKind = "self_paced"
)

// LoopOp is a decoded /loop signal, recognized from a scheduler tool_use.
//
// Cron is the five-field expression a fixed loop was scheduled on; DelaySeconds
// is the delay a self-paced iteration chose. Stop marks a CronDelete that ends
// the loop, and Noop a self-paced quiet tick (ScheduleWakeup noop:true) - the
// "quiet ×N" streak in the catalog. A met/ended loop has no clean frame beyond
// CronDelete, the same silence /goal achieve has.
type LoopOp struct {
	Kind         LoopKind
	Cron         string
	DelaySeconds int
	Stop         bool
	Noop         bool
}

// intArg is one input value as an int, and 0 for a key a tool omits or whose
// value is not a number - JSON numbers decode as float64 through encoding/json.
//
// Moved from encode.go (2026-09-24, workflow task 2) to hold that file under
// the 800-line hard max; unchanged, and reads no Claude JSON of its own - it
// is toolLoopOp's own helper, so this is its ordinary home.
func intArg(input map[string]any, key string) int {
	v, ok := input[key].(float64)
	if !ok {
		return 0
	}
	return int(v)
}
