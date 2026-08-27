package core

// What a session may spend, and what it falls back to when its model is not
// there.
//
// Two knobs that matter at fleet scale and nowhere else, which is why they are
// one file: thirty unbudgeted agents is a bill nobody set a ceiling on, and one
// overloaded model stops all thirty at once rather than one. Both flags are
// documented "only works with --print", which is the mode every Wake agent runs
// in - verified present in claude 2.1.233 as `--max-budget-usd <amount>` and
// `--fallback-model <model>`.
//
// The predicates are here and the flags are spelled in argv.go, which is
// effort.go's arrangement and for its reason: a layer that needs to know whether
// a value is legal asks, and the command-line words live in one file.
//
// **They are validated at two layers and neither is redundant.** The CLI's check
// turns a typo into a sentence naming what is legal; the daemon's is what makes
// the wire fields safe against a client that never ran that code. Same rule
// spawnflags.go already states for effort and model.
//
// # What neither of them confirms
//
// **Nothing reports the cap back, and nothing says when it is hit.**
// total_cost_usd is a level Wake already reads, so the most that could be drawn
// is progress toward a ceiling Wake set itself - a weaker claim than the pane
// makes about a model, and effort's own standing, which is why the banner names
// it as a cap asked for rather than a budget observed. What claude *does* when
// the ceiling is reached is unrecorded: no fixture in testdata/stream/ covers
// it, so nothing here is designed around it.
//
// A failover is quieter still. The frames it produces are an ordinary turn's, so
// a session running its second-choice model is indistinguishable on Wake's wire
// from one running its first - the init frame names the model in use, which is
// the one thing that would say so, and it is per-turn rather than per-failover.

import (
	"math"
	"strconv"
	"strings"
)

// ValidBudget reports whether an amount may reach a command line as
// --max-budget-usd.
//
// Closed in a way ValidModel is not, because a budget has a knowable shape where
// a model name does not. Two checks, and the order is the point.
//
// # The shape first, because ParseFloat is not the far side's parser
//
// **This value crosses a language boundary**, and the far side's parser is
// recorded rather than guessed. Claude 2.1.233's `--max-budget-usd` argParser
// runs the argument through JavaScript's `Number()` and rejects the result if
// it is `NaN` or `<= 0`.
//
// `Number` is not `strconv.ParseFloat`, and the two disagree in both directions
// about strings neither one calls malformed:
//
//	"0x1p4"     Go 16      JS NaN   claude throws: exit 1, nothing on stdout
//	"1_000"     Go 1000    JS NaN   the same, from a plausible thousands mark
//	"0x10"      Go error   JS 16    Wake refuses what claude would take
//	" 5"        Go error   JS 5     the same, since Number trims and Go does not
//	"Infinity"  Go +Inf    JS Inf   claude *accepts* it; the check below does not
//
// The first two are the ones that cost something: Wake vouches for the amount,
// claims a name, creates any worktree asked for, starts a process - and the
// process exits 1 with a startup rejection on stderr and zero bytes on stdout,
// which CLAUDE.md notes is byte-identical to an interrupt. So the accepted
// spelling is narrowed to **digits and at most one dot**, which both parsers
// read identically for every input, rather than to whichever divergences are
// known today. `+5` and `1e2` are refused by that rule even though `Number`
// agrees about them, because a rule needing an argument per spelling is a rule
// that acquires a hole the next time the two parsers differ.
//
// # Then the value
//
//   - The empty string. It means "Wake chose nothing", which callers spell by
//     not passing a budget at all - "" already leaves the flag off the argv, the
//     meaning it carries for effort and model.
//   - Zero. A cap of nothing is not a cap: it is a spawn an operator reads as
//     unbudgeted and claude reads as stop. Refused rather than clamped, because
//     a clamp decides what somebody meant.
//   - Overflow. The shape check makes NaN, the infinities and a sign
//     unwritable, but not a four-hundred-digit number, which ParseFloat returns
//     as +Inf with no error.
//
// The string is passed on exactly as given rather than reformatted. It is one
// element of an exec argv and cannot introduce another - Frame.Model's argument
// - and reformatting would put a number on the command line the operator did not
// type.
func ValidBudget(amount string) bool {
	if !plainDecimal(amount) {
		return false
	}
	usd, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		return false
	}
	return usd > 0 && !math.IsInf(usd, 0)
}

// plainDecimal reports whether a string is digits with at most one dot and at
// least one digit: the spelling of a dollar amount, and nothing else.
//
// Hand-written rather than a regexp because it is the cheaper of the two to read
// and this package imports no regexp today. It admits "5", "0.25", ".5" and
// "5." and refuses everything with a sign, an exponent, an underscore, a base
// prefix, a separator or a space.
func plainDecimal(s string) bool {
	digits, dots := 0, 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.':
			dots++
		default:
			return false
		}
	}
	return digits > 0 && dots <= 1
}

// fallbackSep is how claude spells a chain: "Accepts a comma-separated list to
// try each in order" (2.1.233 --help).
const fallbackSep = ","

// ValidFallbackModel reports whether a failover chain may reach a command line.
//
// Every link is asked ValidModel's question and nothing more, which is the whole
// difference from ValidBudget: there is no knowable set of models, so a chain
// checked against a list would refuse names claude accepts. What this adds is
// the one shape ValidModel cannot see from a single name - an **empty link**.
// "opus,,sonnet" and "opus," both reach claude as a chain naming a model with no
// name, and no frame afterwards would say so.
//
// **A chain naming the primary model is deliberately not refused here.** The
// 2.1.233 bundle carries "Fallback model cannot be the same as the main model",
// which looks like the check to copy and is not: it is thrown by the **Agent
// SDK's** argv builder, in the same statement that pushes `--fallback-model`
// onto a command line it is about to spawn. Wake builds its own argv and never
// goes through that path, so nothing there says what the CLI does with
// `--model opus --fallback-model opus`, and this project does not design around
// unrecorded behaviour. A string comparison could not close it anyway: `opus`
// and `claude-opus-5` may be one model, and nothing here can resolve an alias.
// What would settle it: one recorded spawn with the two equal.
func ValidFallbackModel(chain string) bool {
	if chain == "" {
		return false
	}
	for _, model := range strings.Split(chain, fallbackSep) {
		if !ValidModel(strings.TrimSpace(model)) {
			return false
		}
	}
	return true
}
