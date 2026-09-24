package core

// Wake's own vocabulary for Claude Code's native /goal, decoded from the wire by
// wire.go's goalOp and folded onto a session by the daemon and internal/ui. This
// file names none of Claude's JSON - the recognizer that does is behind the
// airlock; here are only the Wake types it produces, and goalProgress, which
// reads none of Claude's vocabulary either (see its own comment).

import "strings"

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

// goalProgress parses a "Stop hook feedback" refresh: the condition sits in
// the first [..] and the evaluator's latest reason follows "]: ".
//
// Moved from encode.go (2026-09-24, workflow task 2) to hold that file under
// the 800-line hard max; unchanged, and reads no Claude JSON of its own - the
// three punctuation marks it matches on are Wake's own parse, not a wire word.
func goalProgress(text string) (GoalOp, bool) {
	open := strings.Index(text, "[")
	if open < 0 {
		return GoalOp{}, false
	}
	cond, reason, closed := strings.Cut(text[open+1:], "]")
	cond = strings.TrimSpace(cond)
	if !closed || cond == "" {
		return GoalOp{}, false
	}
	reason = strings.TrimSpace(strings.TrimPrefix(reason, ":"))
	return GoalOp{Op: GoalProgress, Condition: cond, Reason: reason}, true
}
