package core

// Wake's own vocabulary for Claude Code's native /goal, decoded from the wire by
// wire.go's goalOp and folded onto a session by the daemon and internal/ui. This
// file names none of Claude's JSON - the recognizer that does is behind the
// airlock; here are only the Wake types it produces.

// KindGoal is one /goal lifecycle signal, carried as Event.Goal. Claude runs the
// goal inside the agent process (a session-scoped Stop hook); Wake forwards the
// command and renders what it observes, with no scheduler of its own.
const KindGoal EventKind = "goal"

// GoalOpKind is which /goal signal a KindGoal event carries.
//
// There is deliberately no "achieved" op: a met goal is simply not followed by
// another Stop-hook feedback frame - the wire carries no clean "achieved"
// signal - so achieve is a fold-layer question (the daemon/ui), not a decode.
// See the design's §6.
type GoalOpKind string

const (
	// GoalSet is /goal <condition>: a goal begins, condition set.
	GoalSet GoalOpKind = "set"
	// GoalProgress is a per-turn Stop-hook refresh that re-drives the goal -
	// condition again, plus the evaluator's latest reason.
	GoalProgress GoalOpKind = "progress"
	// GoalCleared is /goal clear (or an alias): the goal ends.
	GoalCleared GoalOpKind = "cleared"
	// GoalNone is a bare /goal status with nothing set: no goal.
	GoalNone GoalOpKind = "none"
)

// GoalOp is a decoded /goal signal. Condition is the completion condition the
// agent is working toward, empty only on GoalNone; Reason is the evaluator's
// latest verdict, set on a progress refresh and empty otherwise.
type GoalOp struct {
	Op        GoalOpKind
	Condition string
	Reason    string
}
