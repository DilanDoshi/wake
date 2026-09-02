package ui

// The picker, from the outside: what opens one, what it is aimed at, and what
// it sends.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The reported case: a bare command at an agent draws the menu claude cannot
// draw headless, rather than sending a line whose only reply is a usage string.
func TestABareCommandDrawsTheMenuInsteadOfSendingIt(t *testing.T) {
	for word := range bareOnlyCommands {
		t.Run(word, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

			m, cmd := typeAndSubmit(a, SlashPrefix+word)
			got := m.(App)
			if !got.picker.Open() {
				t.Fatalf("/%s did not open a picker", word)
			}
			if got.picker.Word != word {
				t.Errorf("the picker answers %q, want %q", got.picker.Word, word)
			}
			if len(got.picker.Options) == 0 {
				t.Errorf("/%s opened a picker with nothing to choose", word)
			}
			// And nothing went to the agent: the whole point is that the bare
			// form never reaches it.
			if cmd != nil {
				go func() { _ = runCmdQuietly(cmd) }()
				select {
				case f := <-sent:
					t.Errorf("/%s wrote %+v; the bare form is Wake's and must not reach the agent", word, f)
				default:
				}
			}
		})
	}
}

// The fence, stated as behaviour: a draft with an argument is a message and
// reaches the agent byte for byte, exactly as it did before this layer existed.
func TestACommandWithAnArgumentIsNotTakenAtAll(t *testing.T) {
	for _, text := range []string{
		SlashPrefix + effortCommand + " " + core.EffortMax,
		SlashPrefix + effortCommand + " " + core.EffortUltracode,
		SlashPrefix + modelCommand + " opus",
		SlashPrefix + effortCommand + " nonsense",
	} {
		t.Run(text, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

			m, cmd := typeAndSubmit(a, text)
			if m.(App).picker.Open() {
				t.Fatalf("%q opened a picker; only the bare form is Wake's", text)
			}
			go func() { _ = runCmdQuietly(cmd) }()
			f := awaitFrame(t, sent)
			if f.Kind != rpc.FrameSend || f.Text != text {
				t.Errorf("submitting %q wrote %+v, want a FrameSend carrying it unchanged", text, f)
			}
		})
	}
}

// Confirming builds the command and sends it as ordinary text - the same line a
// person types, down the path daemon.noteEffort already watches.
func TestConfirmingSendsTheChosenCommandAsOrdinaryText(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	m, _ := typeAndSubmit(a, SlashPrefix+effortCommand)
	got := m.(App)
	// Down two rows, so the choice is not whatever happened to be first.
	got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyDown})
	got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyDown})
	want := SlashPrefix + effortCommand + " " + got.picker.Options[got.picker.Cursor]

	after, cmd := pressKey(got, tea.KeyMsg{Type: tea.KeyEnter})
	if after.picker.Open() {
		t.Error("the picker is still up after a choice was confirmed")
	}
	go func() { _ = runCmdQuietly(cmd) }()
	f := awaitFrame(t, sent)
	if f.Kind != rpc.FrameSend || f.Text != want {
		t.Errorf("confirming wrote %+v, want a FrameSend carrying %q", f, want)
	}
}

// Escape takes it down and sends nothing.
func TestEscapeClosesThePickerAndSendsNothing(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	m, _ := typeAndSubmit(a, SlashPrefix+modelCommand)
	after, cmd := pressKey(m.(App), tea.KeyMsg{Type: tea.KeyEsc})
	if after.picker.Open() {
		t.Fatal("esc left the picker up")
	}
	if cmd != nil {
		go func() { _ = runCmdQuietly(cmd) }()
	}
	select {
	case f := <-sent:
		t.Errorf("esc wrote %+v; cancelling sends nothing", f)
	default:
	}
}

// The typed escape hands back a half-written command rather than opening a
// second input mode. What the operator finishes has an argument, so the fence
// no longer claims it.
func TestTheTypedEscapeLeavesADraftRatherThanOpeningAnInput(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	m, _ := typeAndSubmit(a, SlashPrefix+modelCommand)
	got := m.(App)
	at := -1
	for i, option := range got.picker.Options {
		if option == typedEscape {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the model picker offers no typed escape: %v", got.picker.Options)
	}
	for range at {
		got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyDown})
	}
	after, cmd := pressKey(got, tea.KeyMsg{Type: tea.KeyEnter})

	if after.picker.Open() {
		t.Error("the typed escape left the picker up")
	}
	if want := SlashPrefix + modelCommand + " "; after.composer().Value() != want {
		t.Errorf("the composer holds %q, want %q", after.composer().Value(), want)
	}
	if cmd != nil {
		go func() { _ = runCmdQuietly(cmd) }()
	}
	select {
	case f := <-sent:
		t.Errorf("the typed escape wrote %+v; it sends nothing", f)
	default:
	}
}

// The direct regression for the finding that made Picker its own type.
//
// Cards.Reconcile rebuilds the open set from every fleet report and drops what
// is absent; a picker has no request id and is in no report, so a picker held
// there would be deleted by the next status push - which lands whenever any of
// thirty agents changes state.
func TestAPickerSurvivesAStatusPush(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	m, _ := typeAndSubmit(a, SlashPrefix+effortCommand)
	got := m.(App)
	if !got.picker.Open() {
		t.Fatal("no picker to push against")
	}
	got = got.withAgents("alex", "sydney")
	if !got.picker.Open() {
		t.Fatal("a status push deleted the picker. It is not a Card for exactly this reason - " +
			"Cards.Reconcile drops what the fleet report does not name, and a picker is in no report")
	}
}

// A second command replaces the first rather than stacking: it is somebody
// changing their mind, not a second question.
func TestASecondCommandReplacesThePicker(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	m, _ := typeAndSubmit(a, SlashPrefix+effortCommand)
	m, _ = typeAndSubmit(m.(App), SlashPrefix+modelCommand)
	if got := m.(App).picker.Word; got != modelCommand {
		t.Errorf("the picker answers %q after a second command, want %q", got, modelCommand)
	}
}

// The header names the target, which is what makes an unaddressed command safe:
// the target is on screen before a key is pressed rather than after.
func TestThePickerHeaderNamesWhatItWillConfigure(t *testing.T) {
	one := Picker{Word: effortCommand, Names: []string{"alex"}}
	if got := one.Header(); !strings.Contains(got, "alex") || !strings.Contains(got, effortCommand) {
		t.Errorf("a one-agent header is %q; it names neither the command nor the agent", got)
	}
	many := Picker{Word: effortCommand, Names: []string{"alex", "sydney", "jo"}}
	if got := many.Header(); !strings.Contains(got, "3") {
		t.Errorf("a three-agent header is %q; it does not say how many", got)
	}
	if strings.Contains(many.Header(), "alex") {
		t.Errorf("a three-agent header names one of them: %q", many.Header())
	}
}

// The effort menu opens on the level the session is already at and marks it, so
// a bare /effort answers "what is it now" before it changes anything - the
// owner's request. The options are the effort words themselves, so the current
// level is one of them and can be both the cursor's start and a marked row.
func TestTheEffortMenuOpensOnAndMarksTheCurrentLevel(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withSize(160, 30)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle, Effort: "xhigh"},
	}}})
	a = a.openPicker(effortCommand, []string{"s1"})

	if a.picker.Current != "xhigh" {
		t.Errorf("the effort menu's current level is %q, want xhigh", a.picker.Current)
	}
	if got := a.picker.Options[a.picker.Cursor]; got != "xhigh" {
		t.Errorf("the effort menu opened on %q, want the current level xhigh", got)
	}
	view := stripANSI(a.picker.View(160))
	if !strings.Contains(view, "current: xhigh") {
		t.Errorf("the effort menu does not list the current level: %q", view)
	}
	if !strings.Contains(view, cardChosen+"xhigh") {
		t.Errorf("the current level is not marked in the list: %q", view)
	}
}

// The model menu lists the current model too, but as a line rather than a mark:
// a model id/display name does not reverse-map to one of the aliases the menu
// offers (opus vs opus[1m] vs opusplan all render the same name), so guessing
// which row is current would be a mark that is wrong as often as right.
func TestTheModelMenuListsTheCurrentModelAsALine(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withSize(160, 30)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle, ConfirmedModel: "Opus 5 (1M context)"},
	}}})
	a = a.openPicker(modelCommand, []string{"s1"})

	if a.picker.Current != "Opus 5 (1M context)" {
		t.Errorf("the model menu's current is %q, want the confirmed display name", a.picker.Current)
	}
	view := stripANSI(a.picker.View(160))
	if !strings.Contains(view, "current: Opus 5 (1M context)") {
		t.Errorf("the model menu does not list the current model: %q", view)
	}
	if strings.Contains(view, cardChosen) {
		t.Errorf("the model menu marked a row as current though no alias matched: %q", view)
	}
}

// Several targets have no single current value, so nothing is claimed: a
// broadcast /effort configures many sessions that may each be at a different
// level, and a menu naming one of them would be naming the wrong one for the rest.
func TestTheMenuClaimsNoCurrentAcrossSeveralTargets(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withSize(160, 30)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle, Effort: "xhigh"},
		{ID: "s2", Name: "sydney", State: rpc.StateIdle, Effort: "low"},
	}}})
	a = a.openPicker(effortCommand, []string{"s1", "s2"})
	if a.picker.Current != "" {
		t.Errorf("a two-target effort menu claimed a current level %q", a.picker.Current)
	}
}

// Every word Wake claims has something to offer, held as a bijection so the two
// cannot drift.
func TestEveryBareOnlyCommandHasOptions(t *testing.T) {
	if len(bareOnlyCommands) != bareOnlyCommandCount {
		t.Fatalf("Wake claims %d bare commands and the count says %d: one added without looking at the "+
			"recording rule is a word taken from claude on an assertion",
			len(bareOnlyCommands), bareOnlyCommandCount)
	}
	for word := range bareOnlyCommands {
		if len(pickerOptions(word)) == 0 {
			t.Errorf("%q is claimed and offers nothing, so it opens an empty menu", word)
		}
	}
}

// The options are core's own, not a second list beside them.
func TestThePickerOffersTheVocabularyCoreHolds(t *testing.T) {
	for _, level := range core.EffortCommands {
		if !contains(pickerOptions(effortCommand), level) {
			t.Errorf("the effort picker does not offer %q, which /effort takes", level)
		}
	}
	for _, alias := range core.ModelAliases {
		if !contains(pickerOptions(modelCommand), alias) {
			t.Errorf("the model picker does not offer %q, which the recorded reply names", alias)
		}
	}
	if !contains(pickerOptions(modelCommand), typedEscape) {
		t.Error("the model picker has no typed escape, so a model shipped tomorrow is unreachable")
	}
	if contains(pickerOptions(effortCommand), typedEscape) {
		t.Error("the effort picker offers a typed escape; its set is closed and printed in the usage line")
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
