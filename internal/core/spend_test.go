package core

import (
	"strconv"
	"testing"
)

// TestValidBudgetTakesAnAmountAndRefusesTheRest is the fence that makes
// Frame.MaxBudgetUSD safe to carry: the value reaches an argv, so anything that
// is not an amount must be refused before a process exists.
//
// The refusals worth naming are the ones that are *not* obviously malformed. A
// bare "0" parses, is finite, and is the one amount that would cap a session at
// nothing - a spawn an operator would read as "no cap" and claude would read as
// "stop". A negative is the same defect with a sign. Both are refused rather
// than clamped, because a clamp decides for the operator what they meant.
func TestValidBudgetTakesAnAmountAndRefusesTheRest(t *testing.T) {
	valid := []string{"0.25", "5", "5.00", ".5", "5.", "1000000"}
	for _, amount := range valid {
		if !ValidBudget(amount) {
			t.Errorf("ValidBudget(%q) = false, want true: it is an amount claude would take", amount)
		}
	}

	invalid := map[string]string{
		"":         "the empty string means nobody chose, which callers spell by not passing one",
		"0":        "a cap of nothing is not a cap",
		"0.00":     "the same amount spelled longer",
		"-1":       "a negative cap",
		"$5":       "a currency symbol claude does not take",
		"5 ":       "trailing space, which would reach the argv as written",
		" 5":       "leading space, likewise",
		"five":     "a word",
		"NaN":      "parses as a float and is not an amount",
		"Inf":      "likewise, and it is the cap that is not one",
		"-Inf":     "likewise",
		"5,000":    "a thousands separator claude does not take",
		"1e400":    "overflows to +Inf",
		"--effort": "a flag, which is the shape that matters if a value is ever built from an argument",

		// The ones that matter, and none of them look malformed. Go's
		// strconv.ParseFloat takes all of them; claude's own argParser is
		// `Number(a)` with a reject on NaN or <= 0, read out of the 2.1.233
		// bundle, and Number does not agree about any of them - so admitting one
		// means Wake vouches for an amount and the process dies at startup with
		// nothing on stdout.
		"0x1p4":  "a hex float. Go reads 16 and Number gives NaN, so claude throws and the session exits 1 after Wake has already claimed its name",
		"0x.1p4": "the same, spelled so it does not look like hex at a glance",
		"1_000":  "a plausible thousands mark. Go reads 1000 and Number gives NaN",
		"+5":     "a leading sign. Harmless in itself and refused with the rest, because the rule is a spelling every parser reads identically rather than a list of the ones that bite",
		"1e2":    "an exponent. JS agrees with Go here, and it is refused anyway for +5's reason - nobody writes a budget this way, and the narrow rule is the one that needs no argument per spelling",
		"5e-1":   "likewise",
		"1.2.3":  "two dots",
		".":      "a dot and no digits",
		"1__0":   "doubled underscores, which Go itself refuses - here to pin that the refusal survives the shape check rather than resting on ParseFloat",
	}
	for amount, why := range invalid {
		if ValidBudget(amount) {
			t.Errorf("ValidBudget(%q) = true, want false: %s", amount, why)
		}
	}
}

// TestValidFallbackModelTakesAChainAndRefusesAnEmptyLink holds the difference
// from ValidBudget: a budget has a knowable shape and a model does not.
// ValidModel already refuses only the empty string - a name released after this
// line was written has to be reachable without a Wake release - so this asks the
// same question of every link in the chain and nothing more.
//
// The link that matters is the empty one. "opus,,sonnet" and "opus," both reach
// claude as a chain naming a model with no name, and nothing in this build would
// say so afterwards.
func TestValidFallbackModelTakesAChainAndRefusesAnEmptyLink(t *testing.T) {
	valid := []string{"sonnet", "opus,sonnet", "opus,sonnet,haiku", "claude-fable-5", "opus[1m],opus"}
	for _, chain := range valid {
		if !ValidFallbackModel(chain) {
			t.Errorf("ValidFallbackModel(%q) = false, want true", chain)
		}
	}

	invalid := map[string]string{
		"":            "the empty string means nobody chose",
		",":           "a chain of one model with no name",
		"opus,":       "a trailing separator, which is a link naming nothing",
		",opus":       "a leading one",
		"opus,,haiku": "an empty link in the middle, which is the one a reader skims past",
		"opus, ":      "whitespace is not a model name",
		"   ":         "nor is a chain of it",
	}
	for chain, why := range invalid {
		if ValidFallbackModel(chain) {
			t.Errorf("ValidFallbackModel(%q) = true, want false: %s", chain, why)
		}
	}
}

// TestABudgetIsASpellingEveryParserReadsTheSameWay is the argument for the
// shape check, stated as a property rather than as the table above's list.
//
// The value crosses a language boundary: Wake validates it in Go and claude
// reads it in JavaScript. strconv.ParseFloat and JS parseFloat disagree about
// hex floats, underscores and a leading sign - measured, not assumed - and every
// disagreement is Wake vouching for one amount while the process runs under
// another. So the accepted set is narrowed to digits and at most one dot, which
// is the spelling the two agree on for every input.
//
// Derived rather than listed: any string outside that shape is refused,
// whatever ParseFloat thinks of it.
func TestABudgetIsASpellingEveryParserReadsTheSameWay(t *testing.T) {
	for _, amount := range []string{"0x1p4", "1_000", "+5", "-5", "1e2", "Inf", "NaN", "0x10"} {
		if _, err := strconv.ParseFloat(amount, 64); err != nil {
			continue // Go refuses it too, so it proves nothing about the shape check
		}
		if ValidBudget(amount) {
			t.Errorf("ValidBudget(%q) = true: Go parses it and this is exactly the class where the far side does not agree", amount)
		}
	}
}

// TestAFallbackChainIsNotNarrowedToAKnownModel is the property that would be
// quietly lost by writing the obvious validator. ValidModel exists because there
// is no knowable set of models - the init frame names one, --help gives an e.g.
// - so a chain checked against a list would refuse names claude accepts.
func TestAFallbackChainIsNotNarrowedToAKnownModel(t *testing.T) {
	const unreleased = "claude-something-6,claude-something-6-mini"
	if !ValidFallbackModel(unreleased) {
		t.Errorf("ValidFallbackModel(%q) = false: a model shipped after this line was written has to be reachable without a Wake release, which is ValidModel's own argument", unreleased)
	}
}
