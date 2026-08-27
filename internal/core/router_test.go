package core

// Routing, which is one rule with a lot of edges: a live agent name wins, and
// everything else is left alone for the CLI to expand.
//
// Two properties carry the feature and both are asserted rather than argued.
// A leading @name reaches the agent it names; nothing else is touched, because
// `@` already means a file reference to the process on the far side and Wake
// is a layer above it, not a replacement for it.
//
// The third property is not about routing at all and is the one nothing else
// in the tree can check from here: **only an id is ever a target.** A name
// resolves in the client and an id crosses the socket, because the reaper's
// entire proof that a process group is the agent it recorded is a session UUID
// in that process's argv - and a word somebody typed in a composer has no
// business anywhere near that proof.

import (
	"slices"
	"strings"
	"testing"
)

// manager is the service every case below is resolved against: addressable by
// name, the default for an unaddressed draft, and deliberately **not** a member
// of live - which is what makes "a broadcast does not reach it" a property of
// the signature rather than of a filter.
var manager = Addressee{ID: "id-manager", Name: ManagerName}

func TestALiveNameRoutesAndAnythingElseIsLeftForTheCLI(t *testing.T) {
	live := []Addressee{{ID: "id-sydney", Name: "sydney"}, {ID: "id-alex", Name: "alex"}}
	cases := []struct {
		in       string
		targets  []string
		text     string
		resolved string
	}{
		{"@sydney can you look at the retry header", []string{"id-sydney"}, "can you look at the retry header", "sydney"},
		{"@src/auth.ts is the file", []string{"id-manager"}, "@src/auth.ts is the file", ManagerName},
		{"@sydney fix @src/auth.ts", []string{"id-sydney"}, "fix @src/auth.ts", "sydney"},
		{"look at @note.txt", []string{"id-manager"}, "look at @note.txt", ManagerName},
		{"who is stuck?", []string{"id-manager"}, "who is stuck?", ManagerName},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := Resolve(c.in, live, manager)
			if !slices.Equal(got.Targets, c.targets) {
				t.Errorf("targets = %v, want %v", got.Targets, c.targets)
			}
			if got.Text != c.text {
				t.Errorf("text = %q, want %q", got.Text, c.text)
			}
			if got.Resolved != c.resolved {
				t.Errorf("resolved = %q, want %q: the room shows what it resolved to before sending, so a file literally named `alex` is recoverable rather than silently misrouted", got.Resolved, c.resolved)
			}
		})
	}
}

func TestOnlyALeadingAtRoutes(t *testing.T) {
	live := []Addressee{{ID: "id-alex", Name: "alex"}}
	got := Resolve("ask @alex about it", live, manager)
	if !slices.Equal(got.Targets, []string{"id-manager"}) {
		t.Errorf("an inline @ routed to %v. Leading @ routes to an agent; inline @ opens the file picker, exactly as Claude Code does - Slack and Discord already trained everyone on the first half", got.Targets)
	}
}

func TestBroadcastNamesEveryLiveAgentAndSaysSo(t *testing.T) {
	live := []Addressee{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got := Resolve("@all stop and report", live, manager)
	if !got.Broadcast || len(got.Targets) != 3 {
		t.Errorf("broadcast=%v targets=%v, want a broadcast to all 3", got.Broadcast, got.Targets)
	}
	if got.Text != "stop and report" {
		t.Errorf("text = %q", got.Text)
	}
	if got.Resolved != BroadcastName {
		t.Errorf("resolved = %q, want %q: a broadcast is the one route that is not to a person, and the room has to be able to say so before it fires N turns", got.Resolved, BroadcastName)
	}
}

func TestAnEndedAgentsNameDoesNotRoute(t *testing.T) {
	// Nothing is listening, and the name may already have gone back to the
	// pool. Falling through to the CLI is the honest behaviour: the message
	// reaches the default addressee with the text intact.
	got := Resolve("@ghost hello", nil, manager)
	if !slices.Equal(got.Targets, []string{"id-manager"}) || got.Text != "@ghost hello" {
		t.Errorf("a dead name routed: targets=%v text=%q", got.Targets, got.Text)
	}
}

// The immutability rule, reachable rather than assumed.
//
// Its first draft compared live[0].Name against a literal, which no plausible
// implementation could have changed - Resolve reads that field and never
// assigns it. The mutation this shape *can* see is the one worth seeing:
// building Targets with append(live, …) writes into the caller's spare
// capacity, and a caller that appended to the same slice afterwards would find
// an agent it never added. So the slice is given room to grow into and the
// whole backing array is compared, not the first element.
//
// The spare slots are filled first, and that is not decoration. With them left
// at their zero value a mutation that did `append(live, Addressee{})` wrote
// the zero value over the zero value and this test stayed green - a fixture
// that could not produce the condition it was named for. A sentinel makes any
// write visible, whatever is written.
func TestResolveDoesNotMutateWhatItIsGiven(t *testing.T) {
	live := make([]Addressee, 4)
	for i := range live {
		live[i] = Addressee{ID: "spare-id", Name: "spare-name"}
	}
	live[0] = Addressee{ID: "id-alex", Name: "alex"}
	live = live[:1]
	backing := live[:cap(live)]
	before := slices.Clone(backing)

	_ = Resolve("@all hi", live, manager)
	_ = Resolve("@alex hi", live, manager)
	_ = Resolve("@ghost hi", live, manager)

	if !slices.Equal(backing, before) {
		t.Errorf("Resolve wrote through to its caller's slice: %v became %v", before, backing)
	}
	if len(live) != 1 || live[0].Name != "alex" {
		t.Errorf("Resolve changed the length or contents of what it was handed: %v", live)
	}
}

// The guard that stops a nameless addressee catching a bare `@`.
//
// Every addressee the room has carries a name, but Addressee is a struct a
// caller fills in and a zero Name is one keystroke away - and `@ ` is a real
// thing to type, because `@` on its own is how somebody starts a mention and
// then thinks better of it. Without a rule that an empty mention names nobody,
// `@ hello` matches the first agent whose Name has not been set and sends
// "hello" to it, silently.
//
// Nothing in the table above reaches this: every case there either has no
// leading @ or has a word after it.
func TestAnAtWithNothingAfterItNamesNobody(t *testing.T) {
	live := []Addressee{{ID: "id-nameless"}, {ID: "id-alex", Name: "alex"}}
	for _, in := range []string{"@ hello", "@", "@\nhello", "  @  hello"} {
		t.Run(in, func(t *testing.T) {
			got := Resolve(in, live, manager)
			if !slices.Equal(got.Targets, []string{"id-manager"}) {
				t.Errorf("targets = %v, want the default addressee: `@` names nobody, so it is text", got.Targets)
			}
			if got.Text != in {
				t.Errorf("text = %q, want %q untouched", got.Text, in)
			}
			if got.Resolved != ManagerName {
				t.Errorf("resolved = %q, want %q: `@` names nobody, so this is an unaddressed draft and it "+
					"goes to the service like any other", got.Resolved, ManagerName)
			}
		})
	}
}

// A mention is a whole token, and that is the entire safety argument.
//
// The reason `@sydney` may be treated as an agent rather than a path is that
// daemon/names.go guarantees no pooled name is a prefix of another - but that
// guarantee is worth nothing if the matcher takes a *prefix* of what was
// typed. An implementation that matched the longest leading run of name
// characters would route `@sydney/notes.md` to sydney and eat the path.
func TestAMentionIsAWholeTokenSoNoPathStartsWithAName(t *testing.T) {
	live := []Addressee{{ID: "id-sydney", Name: "sydney"}, {ID: "id-alex", Name: "alex"}}
	routed := 0
	for _, a := range live {
		for _, tail := range []string{"/notes.md", ".go", ",", ":12", "ish", "-report"} {
			in := "@" + a.Name + tail + " look here"
			got := Resolve(in, live, manager)
			// Resolved to *that agent* is the failure, not resolved at all: an
			// unaddressed draft now resolves to the service, so "nothing
			// resolved" stopped being the shape of the right answer here.
			if got.Resolved == a.Name || !slices.Equal(got.Targets, []string{"id-manager"}) {
				t.Errorf("Resolve(%q) resolved to %q and targeted %v: a mention is the whole token, or a live name becomes the front of every path that starts with it",
					in, got.Resolved, got.Targets)
			}
			if got.Text != in {
				t.Errorf("Resolve(%q) rewrote the text to %q", in, got.Text)
			}
			routed++
		}
	}
	if routed == 0 {
		t.Fatal("no case ran: this test is asserting nothing")
	}
}

// A broadcast to a fleet that is not there.
//
// The alternative was falling through to the default addressee, and it is
// worse in the way this file keeps guarding against: the manager would receive
// "stop and report" as if somebody had asked it, or "@all stop and report"
// with a mention the CLI would then try to open as a file. Nothing is sent,
// Broadcast and Resolved both say what was meant, and the room can draw "all -
// nobody live" instead of inventing a recipient.
func TestABroadcastWithNothingLiveSendsToNobodyAndStillSaysWhatItWas(t *testing.T) {
	for _, live := range [][]Addressee{nil, {}} {
		got := Resolve("@all stop and report", live, manager)
		if len(got.Targets) != 0 {
			t.Errorf("targets = %v, want none: there is nobody to broadcast to", got.Targets)
		}
		if !got.Broadcast || got.Resolved != BroadcastName {
			t.Errorf("broadcast=%v resolved=%q, want the route to still name itself so the room can show it reached nobody", got.Broadcast, got.Resolved)
		}
		if got.Text != "stop and report" {
			t.Errorf("text = %q", got.Text)
		}
	}
}

// With no service, an unaddressed draft is addressed to nobody.
//
// The manager is built now, and this case is the room that has not started one
// - which is every room until somebody types `wake manager`, and every room
// again the moment the manager ends. Answering with a target of "" would be an
// id that names no session, sent to a daemon that would refuse it: a message
// lost with a receipt. An empty Targets is the honest answer and the room can
// see it.
//
// This comment used to say "the manager is not built", which was a claim about
// the whole build made by a test that reads none of it - the shape
// docs/notes/decisions.md calls rung 7, and one of the two hits its own audit
// found. It expired with this task.
func TestWithNoServiceNothingIsAddressed(t *testing.T) {
	live := []Addressee{{ID: "id-alex", Name: "alex"}}
	for _, in := range []string{"who is stuck?", "@ghost hello", "@ hello"} {
		got := Resolve(in, live, Addressee{})
		if len(got.Targets) != 0 {
			t.Errorf("Resolve(%q, …, \"\") targets %v, want none - not a blank id", in, got.Targets)
		}
		if got.Text != in {
			t.Errorf("Resolve(%q, …, \"\") text = %q", in, got.Text)
		}
	}
	// And a name still routes, so the empty default is not a way to lose a
	// message somebody addressed.
	got := Resolve("@alex hello", live, Addressee{})
	if !slices.Equal(got.Targets, []string{"id-alex"}) {
		t.Errorf("an addressed message went to %v when no default was nominated", got.Targets)
	}
}

// The manager answers to its own name, and it is not in the fleet.
//
// Both halves are the same decision seen from two sides. `@manager` resolves
// down the ordinary name path so the room needs no special case for it - the
// alternative considered was special-casing @manager onto the default
// addressee, which reads well until there is no manager: the mention would be
// stripped, the message would reach whoever the caller nominated, and Resolved
// would say "manager" over the top of it. A misroute with a confident label is
// precisely what Resolved exists to prevent.
//
// And it is a **service, not a participant**, so it arrives beside live rather
// than in it. That is what makes the broadcast exclusion below structural: a
// filter inside broadcast could be widened by a later change; a list the
// manager is not in cannot.
func TestTheServiceAnswersToItsOwnNameAndIsNotInTheFleet(t *testing.T) {
	live := []Addressee{{ID: "id-alex", Name: "alex"}}

	got := Resolve("@"+ManagerName+" what is everyone doing", live, manager)
	if !slices.Equal(got.Targets, []string{"id-manager"}) || got.Resolved != ManagerName {
		t.Errorf("targets=%v resolved=%q, want the manager reached by the ordinary name path", got.Targets, got.Resolved)
	}
	if got.Text != "what is everyone doing" {
		t.Errorf("text = %q", got.Text)
	}

	// No manager: nothing to address, so nothing is addressed and the text
	// keeps its mention. It must not be special-cased onto whoever else the
	// caller nominated.
	got = Resolve("@"+ManagerName+" hello", live, Addressee{ID: "id-fallback", Name: "fallback"})
	if got.Resolved == ManagerName || !slices.Equal(got.Targets, []string{"id-fallback"}) {
		t.Errorf("targets=%v resolved=%q: with no manager live, @manager is text", got.Targets, got.Resolved)
	}
	if got.Text != "@"+ManagerName+" hello" {
		t.Errorf("text = %q, want the mention left for the CLI", got.Text)
	}
}

// A broadcast is to the fleet, and the manager is the thing that manages the
// fleet.
//
// A manager told to report on the fleet by a message it also received is a
// manager reporting on itself, and at 30 agents `@all` is already the most
// expensive key in the room - so the exclusion is not tidiness, it is one turn
// per broadcast that answers nothing.
//
// Asserted over the ids rather than over the count, because a count is
// satisfied by any N-element answer and the failure being guarded is one
// specific member appearing.
func TestABroadcastReachesTheFleetAndNotTheService(t *testing.T) {
	live := []Addressee{{ID: "id-sydney", Name: "sydney"}, {ID: "id-alex", Name: "alex"}}
	got := Resolve("@"+BroadcastName+" status", live, manager)

	if !got.Broadcast || got.Resolved != BroadcastName {
		t.Fatalf("broadcast=%v resolved=%q, want a broadcast that says so", got.Broadcast, got.Resolved)
	}
	if slices.Contains(got.Targets, manager.ID) {
		t.Errorf("a broadcast reached the manager: targets=%v. It is a service, not a participant - and a "+
			"manager told to report on the fleet by a message it also received is a manager reporting on "+
			"itself", got.Targets)
	}
	if !slices.Equal(got.Targets, []string{"id-sydney", "id-alex"}) {
		t.Errorf("targets = %v, want every live agent and only those", got.Targets)
	}
}

// The composer is a textarea, so a mention on its own line is ordinary. The
// body keeps its own shape: only the whitespace that separated the mention
// from it is taken.
func TestAMentionOnItsOwnLineRoutesAndTheBodyKeepsItsShape(t *testing.T) {
	live := []Addressee{{ID: "id-sydney", Name: "sydney"}}
	const body = "fix the retry header\n\n    return nil\n"
	got := Resolve("@sydney\n"+body, live, manager)
	if got.Resolved != "sydney" {
		t.Errorf("resolved = %q, want sydney", got.Resolved)
	}
	if got.Text != body {
		t.Errorf("text = %q, want %q", got.Text, body)
	}
}

// The rule that rules everything here, checked over every class of route this
// package can produce: a name resolves in the client and only an id crosses
// the socket.
//
// It is worth a test of its own rather than being implied by the table above,
// because the table asserts equality against ids it wrote down - and an
// implementation that returned Addressee.Name would fail those cases for a
// reason that reads like a typo. This one fails with the reason.
//
// Checked both ways. Targets may hold only ids, because rpc.Frame carries a
// SessionID and daemon/reap.go proves a process group's identity by finding
// that UUID in an argv. Resolved may hold only a name, because it is what the
// room shows a person before the message goes anywhere, and eight characters
// of a UUID is the thing names exist to replace.
func TestOnlyAnIDIsEverATargetAndOnlyANameIsEverResolved(t *testing.T) {
	live := []Addressee{{ID: "id-sydney", Name: "sydney"}, {ID: "id-alex", Name: "alex"}}
	def := manager

	ids := map[string]bool{def.ID: true}
	names := map[string]bool{def.Name: true}
	for _, a := range live {
		ids[a.ID] = true
		names[a.Name] = true
	}

	inputs := []string{
		"@" + BroadcastName + " stop and report",
		"@ghost hello",
		"look at @note.txt",
		"@src/auth.ts is the file",
		"who is stuck?",
		"@ hello",
	}
	for _, a := range live {
		inputs = append(inputs, "@"+a.Name+" hello", "ask @"+a.Name+" about it")
	}

	sent, addressed := 0, 0
	for _, in := range inputs {
		got := Resolve(in, live, def)
		for _, target := range got.Targets {
			sent++
			if names[target] {
				t.Errorf("Resolve(%q) targets %q, which is a *name*: names resolve in the client and only an id crosses the socket, because the reaper's proof of identity is a session UUID in a process's argv", in, target)
			}
			if !ids[target] {
				t.Errorf("Resolve(%q) targets %q, which is neither a live session's id nor the default addressee", in, target)
			}
		}
		if got.Resolved == "" {
			continue
		}
		addressed++
		if ids[got.Resolved] {
			t.Errorf("Resolve(%q) resolved to %q, which is an id: Resolved is what the room shows a person before sending", in, got.Resolved)
		}
		if got.Resolved != BroadcastName && !names[got.Resolved] {
			t.Errorf("Resolve(%q) resolved to %q, which names no live agent", in, got.Resolved)
		}
	}
	if sent == 0 || addressed == 0 {
		t.Fatalf("%d targets over %d addressed routes: this test is asserting nothing", sent, addressed)
	}
}

// Resolve is pure, and the cheap version of saying so is that the same
// arguments give the same answer whatever has happened in between.
func TestResolveIsAFunctionOfItsArgumentsAlone(t *testing.T) {
	live := []Addressee{{ID: "id-sydney", Name: "sydney"}, {ID: "id-alex", Name: "alex"}}
	const in = "@sydney fix @src/auth.ts"

	first := Resolve(in, live, manager)
	for _, other := range []string{"@all hi", "@alex hi", "plain", "@ghost hi"} {
		_ = Resolve(other, live, manager)
	}
	again := Resolve(in, live, manager)

	if !slices.Equal(first.Targets, again.Targets) || first.Text != again.Text ||
		first.Broadcast != again.Broadcast || first.Resolved != again.Resolved {
		t.Errorf("Resolve(%q) gave %+v and then %+v: it is holding state", in, first, again)
	}
}

// Whatever a route decides, the message that goes out is the message that was
// typed minus at most a leading mention. A router that rewrote the body would
// be editing somebody's words on their behalf, and the inline @path it would
// most plausibly "fix" is the one thing that already works.
func TestNothingButALeadingMentionIsEverRemovedFromTheText(t *testing.T) {
	live := []Addressee{{ID: "id-sydney", Name: "sydney"}, {ID: "id-alex", Name: "alex"}}
	inputs := []string{
		"@sydney fix @src/auth.ts and @lib/b.ts",
		"@all read @docs/readme.md",
		"@ghost @note.txt",
		"look at @note.txt",
		"@ @note.txt",
		"@sydney",
	}
	for _, in := range inputs {
		got := Resolve(in, live, manager)
		if !strings.HasSuffix(in, got.Text) {
			t.Errorf("Resolve(%q) sent %q, which is not a suffix of what was typed: only a leading mention may be taken", in, got.Text)
		}
		// Keyed on what was *removed* rather than on whether anything resolved,
		// which the version before the manager could conflate: an unaddressed
		// draft now resolves - to the service - and removes nothing, so
		// "resolved" no longer implies "a mention was taken".
		cut := strings.TrimSuffix(in, got.Text)
		if cut == "" {
			continue
		}
		if got.Resolved == "" {
			t.Errorf("Resolve(%q) removed %q while resolving nothing", in, cut)
		}
		if !strings.HasPrefix(strings.TrimLeft(cut, " \t\r\n"), "@") {
			t.Errorf("Resolve(%q) removed %q, which is not a mention", in, cut)
		}
	}
}

// A stray space before a mention must not quietly stop it routing. Nothing in
// the table above reaches this: every case there begins with the character it
// is about.
func TestALeadingSpaceDoesNotStopAMentionRouting(t *testing.T) {
	live := []Addressee{{ID: "id-sydney", Name: "sydney"}}
	for _, in := range []string{" @sydney hi", "\t@sydney hi", "\n@sydney hi"} {
		got := Resolve(in, live, manager)
		if !slices.Equal(got.Targets, []string{"id-sydney"}) || got.Resolved != "sydney" {
			t.Errorf("Resolve(%q) targeted %v and resolved %q", in, got.Targets, got.Resolved)
		}
		if got.Text != "hi" {
			t.Errorf("Resolve(%q) sent %q, want %q", in, got.Text, "hi")
		}
	}
}

// Broadcast is what tells a caller it is about to fire N turns instead of one,
// so it has to be false everywhere else. Nothing above asserts that: the
// broadcast test checks it is set, and no other test reads the field at all.
func TestOnlyABroadcastIsMarkedAsOne(t *testing.T) {
	live := []Addressee{{ID: "id-sydney", Name: "sydney"}, {ID: "id-alex", Name: "alex"}}
	for _, in := range []string{
		"@sydney hello", "@alex hello", "@ghost hello", "@ hello",
		"look at @note.txt", "who is stuck?", "@src/auth.ts is the file",
		"@" + ManagerName + " hello",
	} {
		if got := Resolve(in, live, manager); got.Broadcast {
			t.Errorf("Resolve(%q) is marked a broadcast: a caller showing the count would say %d turns for one message", in, len(got.Targets))
		}
	}
	if got := Resolve("@"+BroadcastName+" hello", live, manager); !got.Broadcast {
		t.Error("a broadcast is not marked as one: this test would pass against a field nothing ever sets")
	}
}
