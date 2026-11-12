package core

import "os"

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
// "quiet ×N" streak in the catalog. A loop's end has no frame of its own beyond
// CronDelete; a self-paced one's is read off its wakeup turn (Event.LoopWakeup).
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

// LoopKeepsAlive reports claude's switch back to the self-paced loop's rule before
// 2.1.319, whose changelog reads "Changed self-paced /loop to end when a turn
// finishes without rescheduling, instead of scheduling a fallback wakeup; set
// CLAUDE_CODE_LOOP_KEEPALIVE=1 to keep the old behavior". An agent inherits the
// daemon's environment, so the daemon's reading is its agents'.
func LoopKeepsAlive() bool { return os.Getenv("CLAUDE_CODE_LOOP_KEEPALIVE") == "1" }
