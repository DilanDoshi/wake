package ui

// Which of Wake's keys mean something else in Claude Code, held as a
// maintained fact rather than as something a human found once by pressing them.
//
// Wake runs Claude Code sessions, so its operators arrive with Claude Code's
// keyboard in their hands. A chord that means one thing there and another here
// is a reflex that misfires, and the cost runs from "nothing happened" to a
// closed window with a live fleet behind it. Everything in `acknowledged` is
// accepted; one more is a failure, because the day one appears is the day
// somebody decides whether it is acceptable, and that day is now rather than
// after a release. No count is written down here on purpose - a number nothing
// asserts drifts, and the maps below are the record.
//
// **A shared key is not always a collision.** `⇧⇥` cycles a permission mode on
// both sides, which is agreement rather than something to tolerate - it lives
// in `agrees` and is asserted rather than excused. Filing it as a collision
// would have cost the one signal worth having: an acknowledged row stays green
// when the meaning changes, so an alignment recorded as a collision is an
// alignment nobody is told about when it breaks.
//
// Both sides are derived. Claude's is maintained by hand against Claude Code,
// the way the palette is; Wake's is
// legendEntries, so a key added to this build is checked by construction.

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
)

// keymapFixture is Claude Code's default bindings, extracted rather than typed.
const keymapFixture = "testdata/claude-keymap.json"

type claudeKeymap struct {
	Source string `json:"_source"`

	// Bindings is action id to key name, for the pairs the binary is
	// unambiguous about.
	Bindings map[string]string `json:"bindings"`

	// Conflicts is what the extraction refused to guess at: an action whose
	// sites disagree about its key. Carried so a gap in the evidence is
	// visible here rather than being indistinguishable from an absence.
	Conflicts map[string][]string `json:"_conflicts"`
}

func loadKeymap(t *testing.T) claudeKeymap {
	t.Helper()
	raw, err := os.ReadFile(keymapFixture)
	if err != nil {
		t.Fatalf("reading %s: %v\nthe fixture is maintained by hand", keymapFixture, err)
	}
	var km claudeKeymap
	if err := json.Unmarshal(raw, &km); err != nil {
		t.Fatalf("parsing %s: %v", keymapFixture, err)
	}
	return km
}

// collision is one of Wake's keys and one Claude Code action that key already
// means.
type collision struct{ key, action string }

func (c collision) String() string { return c.key + " → " + c.action }

// acknowledged is every collision the owner has accepted, and why it is
// tolerable. A row here is a decision, not an exemption: each one says what the
// wrong reflex does in Wake and how the operator gets back.
//
// ⌃O is the only one with a mechanism behind it, because it is the only one
// whose result is not on screen to be undone - see detach.go.
var acknowledged = map[collision]string{
	{"ctrl+o", "app:toggleTranscript"}: "Claude Code expands a truncated tool result; Wake detaches. " +
		"The one collision that costs something, and the one with an arm on it: the first press says " +
		"what the second does and names ⌃E. detach.go.",

	{"ctrl+t", "app:toggleTodos"}: "Claude Code raises the todo panel; Wake flips the mention mode. " +
		"The composer's target line names the reading that is live, so the wrong press is visible in " +
		"the row above the keys and the same key puts it back.",
	{"ctrl+t", "theme:toggleSyntaxHighlighting"}: "The same key again, further down Claude Code's own " +
		"list. Wake has no syntax toggle to be confused with, and the mention flip is reversible.",

	{"ctrl+g", "chat:externalEditor"}: "Claude Code opens the draft in $EDITOR; Wake toggles the " +
		"workspaces sidebar. A sidebar appearing is the whole result and the same key puts it back. " +
		"Wake has no external editor at all.",

	{"ctrl+r", "history:search"}: "Claude Code searches your prompt history; Wake toggles the activity " +
		"sidebar. Same shape as ⌃G - visible, reversible, one press. Wake has no prompt search; ⌥↑↓ " +
		"walks the history without one.",

	{"ctrl+b", "task:background"}: "Claude Code backgrounds the running task; Wake stacks the picked " +
		"conversation under the focused pane. A pane opening is visible and ⌃W closes it.",

	{"ctrl+e", "confirm:toggleExplanation"}: "Claude Code expands the explanation on its confirmation " +
		"surface; Wake expands the focused conversation's tool results. Wake answers an ask with the " +
		"card's own keys - a/d, the digits, ↵ - so the two surfaces are never both taking ⌃E, and the " +
		"expand is one press to put back.",
	{"ctrl+e", "theme:editCustom"}: "Claude Code opens its custom-theme editor; Wake expands tool " +
		"results. Wake has no theme editor to be confused with - the palette is extracted from the " +
		"binary (theme.go) and is not editable at all - so the wrong reflex draws more of a tool result " +
		"and the same key folds it back.",
}

// claudeSpelling is where bubbletea and Claude Code name the same key
// differently. One row, and it is here rather than assumed: two vocabularies
// agreeing everywhere except on one word is exactly how a real collision hides.
var claudeSpelling = map[string]string{"esc": "escape"}

// Every collision between the two keyboards is one somebody has ruled on.
func TestEveryCollisionWithClaudeCodesKeysIsAcknowledged(t *testing.T) {
	km := loadKeymap(t)
	if len(km.Bindings) == 0 {
		t.Fatal("the keymap fixture holds no bindings, so this test compares Wake's keys against nothing")
	}

	found := map[collision]bool{}
	for _, e := range legendEntries {
		keys, ok := legendKeyNames[e.glyph]
		if !ok {
			t.Errorf("the legend advertises %q (%s) and legendKeyNames does not say which key it is, "+
				"so this guard cannot check it against Claude Code's", e.glyph, e.what)
			continue
		}
		for _, k := range keys {
			name := k.msg.String()
			if spelled, differs := claudeSpelling[name]; differs {
				name = spelled
			}
			for _, action := range slices.Sorted(collidingActions(km, name)) {
				c := collision{key: name, action: action}
				found[c] = true
				if _, same := agrees[c]; same {
					continue
				}
				if _, ruled := acknowledged[c]; ruled {
					continue
				}
				t.Errorf("%s %s collides with Claude Code %s in %s, and nothing has ruled on it.\n"+
					"An operator who reaches for that chord out of habit gets Wake's meaning instead. "+
					"Decide what the wrong press costs, then either move nothing and add a row to "+
					"`acknowledged` saying why it is tolerable, or give the key an arm the way ⌃O has "+
					"one (detach.go).", e.glyph, e.what, c, km.Source)
			}
		}
	}

	// The other direction, because an exemption nobody can lose is an exemption
	// that outlives its reason: a rebinding on either side leaves a row here
	// describing a collision that no longer exists, and the next reader trusts it.
	for c := range acknowledged {
		if !found[c] {
			t.Errorf("`acknowledged` carries %s and that is no longer a collision in %s - either Wake "+
				"moved the key or Claude Code did. Delete the row rather than leaving a ruling about "+
				"nothing", c, km.Source)
		}
	}

	// And the agreements, where a disappearance means the opposite thing: not a
	// ruling gone stale but an alignment that has broken, and an operator's
	// habit that now misfires on a key nobody has ruled on.
	for c := range agrees {
		if !found[c] {
			t.Errorf("`agrees` carries %s and %s no longer binds it that way. That key used to mean the "+
				"same thing on both sides and now does not, so it is a collision nobody has decided "+
				"about - rule on it and move the row to `acknowledged`", c, km.Source)
		}
	}
}

// agrees is a key that means the same thing on both sides. It is asserted, not
// tolerated, and that is the whole reason it is a second map: a row in
// `acknowledged` says a wrong reflex is survivable, and there is no wrong
// reflex here.
//
// It is held rather than left implicit because the failure it catches is the
// one an `acknowledged` row would swallow. If Claude Code rebinds `cycleMode`,
// the agreement is gone and an operator's habit starts misfiring - and a row
// sitting in `acknowledged` would keep the guard green through exactly that.
// Here, the staleness check below turns it into a failure that says the
// alignment broke.
var agrees = map[collision]string{
	{"shift+tab", "chat:cycleMode"}: "Both cycle a permission mode. Wake's moves the *picked agent's* " +
		"and the label moves on the daemon's receipt rather than on the keystroke - see mode.go - so " +
		"the scope differs where the meaning does not.",
	{"shift+tab", "confirm:cycleMode"}: "Claude Code's confirmation surface cycles the same mode from " +
		"the same key. Wake answers an ask with the card's own keys, so the two never overlap on screen.",
	{"ctrl+e", "transcript:toggleShowAll"}: "Both reveal what the pane folded away: Claude Code shows " +
		"the whole transcript, Wake shows its tool results whole (expand.go). **Reached by accident** - " +
		"⌃E was picked because ⌃O was spent on detach and ⌃E shadows only the text area's line-end - " +
		"but an alignment nobody planned is still one an operator's habit rides on, which is why it is " +
		"asserted here rather than excused in `acknowledged`.",
}

// collidingActions yields every Claude Code action bound to this key.
func collidingActions(km claudeKeymap, key string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for action, bound := range km.Bindings {
			if bound == key && !yield(action) {
				return
			}
		}
	}
}

// The fixture is only evidence if it was read out of a binary that exists and
// carried enough of the keyboard to be worth comparing against.
func TestTheKeymapFixtureNamesItsSource(t *testing.T) {
	km := loadKeymap(t)
	if km.Source == "" {
		t.Fatal("fixture carries no _source; it is maintained by hand")
	}
	// The _source anchor the fixture must carry. Held
	// here too, because a fixture is read long after it was written.
	if got := km.Bindings["app:toggleTranscript"]; got != "ctrl+o" {
		t.Errorf("app:toggleTranscript is %q in %s, want ctrl+o - the binding Claude Code prints under "+
			"every truncated tool result, and the reason ⌃O is armed here", got, km.Source)
	}
	if len(km.Bindings) < 20 {
		t.Errorf("fixture holds %d bindings, fewer than the 22 extracted from 2.1.232 and 2.1.233: "+
			"the scan has probably broken, and a short keymap reports a keyboard with no collisions "+
			"on it", len(km.Bindings))
	}
	for action, keys := range km.Conflicts {
		t.Logf("unconfirmed: %s is beside %v at different sites, so it was omitted rather than guessed",
			action, keys)
	}
}

// A reason is a sentence somebody wrote, not a marker.
func TestEveryAcknowledgedCollisionCarriesAReason(t *testing.T) {
	for c, why := range acknowledged {
		if len(why) < 40 {
			t.Errorf("%s is acknowledged with %q. The row is the whole record of a decision - what the "+
				"wrong reflex does here, and how the operator gets back", c, fmt.Sprint(why))
		}
	}
	// `agrees` too, and for the same reason rather than for symmetry: a shared
	// key is only self-evidently an agreement to whoever just checked. The row
	// is where the next reader learns the meaning really is the same and where
	// the scopes differ.
	for c, why := range agrees {
		if len(why) < 40 {
			t.Errorf("%s is filed as an agreement with %q. Say what both sides do with the key, and "+
				"where they differ, so the next reader does not have to re-derive it", c, fmt.Sprint(why))
		}
	}
}
