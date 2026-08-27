// Forking: the three flags that have to travel together, and the two shapes
// the CLI punishes for splitting them up.
//
// Every claim here has a recording behind it in testdata/stream and a section
// in docs/superpowers/notes/2026-08-09-resume-fork-findings.md or
// 2026-08-10-live-fork-findings.md. Nothing here is reasoned from the help
// text.

package core

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// The ids these tests fork between. UUIDs because that is what Wake mints and
// what the reaper matches in an argv.
//
// They are not invented: they are the recorded pair - P and F of the live-fork
// spike - so the argv these tests assert is the argv that produced
// live-fork-child.jsonl. See live-fork-parent.jsonl:1, live-fork-child.jsonl:1
// and the [disk] rows in live-fork-parent.transcript.sha256.
const (
	idForkParent = "2ca1b3a0-0319-4eb4-904c-9c6563985736"
	idForkChild  = "37f6e67d-99f1-4d58-8309-be14ef0693ae"
)

// The invocation, byte for byte, from the recordings that made it.
//
// Order and adjacency are both asserted because neither has an alternative
// with a recording behind it: resume-fork-both-flags.jsonl, live-fork-child.jsonl
// and fork-of-fork.jsonl all sent exactly this, in exactly this order, in the
// position buildArgs puts --session-id.
func TestAForkCarriesTheRecordedTripleInTheRecordedOrder(t *testing.T) {
	s := NewSession(Config{
		SessionID:      idForkChild,
		ForkFrom:       idForkParent,
		Name:           "sydney",
		PermissionMode: "manual",
	})

	args, err := s.buildArgs()
	if err != nil {
		t.Fatalf("buildArgs for a fork: %v", err)
	}
	joined := " " + strings.Join(args, " ") + " "
	want := " --resume " + idForkParent + " --fork-session --session-id " + idForkChild + " "
	if !strings.Contains(joined, want) {
		t.Errorf("a fork's argv is\n  %s\nand it must contain\n  %s\n"+
			"- that triple is what three recordings sent, and nothing has recorded a permutation of it", joined, want)
	}
}

// A wake is a bare --resume and nothing else, which is the whole of the shape.
//
// resume-wake.jsonl is a separate process from resume-park.jsonl - different
// pid in init.messaging_socket_path - started with --resume on the id the
// first one was given, and every line of it carries that same id. So the flag
// reuses the id rather than needing one supplied, and supplying one anyway is
// the shape resume-session-id-without-fork.stderr.txt records being refused at
// startup with nothing on stdout.
func TestAWakeIsABareResumeAndCarriesNoSessionId(t *testing.T) {
	s := NewSession(Config{
		SessionID:      idForkParent,
		ResumeFrom:     idForkParent,
		Name:           "sydney",
		PermissionMode: "manual",
	})

	args, err := s.buildArgs()
	if err != nil {
		t.Fatalf("buildArgs for a wake: %v", err)
	}
	if !containsSeq(args, []string{"--resume", idForkParent}) {
		t.Errorf("a wake's argv is\n  %v\nand it has to carry `--resume %s`", args, idForkParent)
	}
	if hasFlag(args, "--session-id") {
		t.Errorf("a wake's argv is\n  %v\nand it carries --session-id beside --resume. The CLI refuses "+
			"that pair at startup, exit 1, with nothing on stdout - so the only diagnosis is the "+
			"stderr tail, and a park path that appended --resume and left the rest alone is exactly "+
			"how it gets built", args)
	}
	if hasFlag(args, "--fork-session") {
		t.Errorf("a wake's argv is\n  %v\nand it carries --fork-session. That would mint a new "+
			"session under the id being resumed instead of continuing it", args)
	}
}

// hasFlag reports whether an argv carries a bare flag.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// containsSeq reports whether args carries want as a contiguous run, in order.
// Adjacency is the point: the triple is asserted as one literal, not as three
// flags that happen to be present somewhere.
func containsSeq(args, want []string) bool {
	if len(want) == 0 || len(want) > len(args) {
		return false
	}
	for i := 0; i+len(want) <= len(args); i++ {
		if slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

// identityValue is one value an identity field can take, paired with the
// session it names.
//
// The `names` label is the test's own ground truth and is deliberately not
// computed by asking production: a test that used sameSession to decide which
// ids are equal would agree with sameSession by construction and could never
// see it break. These labels come from the uuid.Parse probe behind
// TestAForkOntoTheParentsOwnIdIsRefusedHoweverTheIdIsSpelled - uppercase hex
// parses to the same UUID as the canonical spelling - and from plain string
// equality for the two values that are not UUIDs at all.
type identityValue struct {
	spelling string
	names    string // the session it names; "" when the field is absent
}

// identityDomain is one representative of every distinct *kind* of value the
// two identity fields can hold: absent, a UUID, the same UUID spelled another
// way, a different UUID, and two strings that are not UUIDs.
//
// One alternate spelling is enough here - the full table (braced, urn:uuid:,
// undashed, and both cases of each) belongs to the dedicated spelling test.
// What this representative buys is that normalisation participates in the
// cross product rather than only in the test written for it.
func identityDomain() []identityValue {
	return []identityValue{
		{"", ""},
		{idForkParent, "parent"},
		{strings.ToUpper(idForkParent), "parent"},
		{idForkChild, "child"},
		{"parent-label", "literal:parent-label"},
		{"child-label", "literal:child-label"},
	}
}

// decorations are the fields that must **not** reach an identity flag, in the
// two states that matter: none of them set, and all of them set.
//
// Every cell of the cross product is built under both, and that second
// dimension is the whole reason this test can see the failure that defeated its
// two predecessors. A switch arm keyed on a third field - `ForkFrom != "" &&
// Model != ""` - produces two different identity fragments for one
// (ForkFrom, SessionID) pair, and the oracle below knows about two fields only.
// So the decorated half of that pair disagrees with it and goes red.
func decorations() []struct {
	name  string
	apply func(Config) Config
} {
	return []struct {
		name  string
		apply func(Config) Config
	}{
		{"no other field set", func(c Config) Config { return c }},
		{"every other field set", func(c Config) Config {
			c.Name, c.Dir, c.Model, c.Effort, c.PermissionMode = "sydney", "/tmp", "opus", "max", "manual"
			// The manager's two, added the day they were added to Config. A
			// field this dimension does not set is a field an identity arm can
			// be narrowed on invisibly, which is the failure the second
			// dimension exists for - so a field added to Config is a field
			// added here, in the same change.
			c.MCPConfig, c.AppendSystemPrompt = "/tmp/wake/mcp.json", "You are Wake's manager."
			return c
		}},
	}
}

// identityOracle is what one (ForkFrom, ResumeFrom, SessionID) triple has to
// produce, derived from the three values' own labels and from nothing the
// production code says.
//
// The flag booleans are separate from the fragment because they are a
// different claim: the fragment says the shape asked for was built, and the
// booleans say nothing *beyond* it was - which is what catches an extra flag
// containsSeq would read straight past.
type identityOracle struct {
	err                          bool
	why                          string
	fragment                     []string
	wantResume, wantFork, wantID bool
	outcome                      string
}

// expectedIdentity is the oracle, and it is a function of the three labels.
//
// It is a function rather than a block inside the loop so the loop stays
// readable at four dimensions, and so `why` cannot drift from the case that set
// it - the version before park/wake had a separate whyRefused() taking one
// value, which had no way to tell a resume conflict from a fork with no id and
// would have said the wrong one about the new arm.
func expectedIdentity(from, res, id identityValue) identityOracle {
	switch {
	case from.names != "" && res.names != "":
		return identityOracle{err: true, outcome: "refused: fork and resume at once",
			why: "a fork and a resume at once, which are two different verbs"}
	case from.names != "" && id.names == "":
		return identityOracle{err: true, outcome: "refused: fork with no id",
			why: "a fork with no id of its own"}
	case from.names != "" && id.names == from.names:
		return identityOracle{err: true, outcome: "refused: fork onto its own id",
			why: "the same session, so this is a fork onto the parent's own id"}
	case from.names != "":
		return identityOracle{
			fragment:   []string{"--resume", from.spelling, "--fork-session", "--session-id", id.spelling},
			wantResume: true, wantFork: true, wantID: true,
			outcome: "the fork triple",
		}
	case res.names != "":
		if id.names != "" && id.names != res.names {
			return identityOracle{err: true, outcome: "refused: resumed under a different id",
				why: "a resume of one session under another's id, which --resume refuses at startup"}
		}
		return identityOracle{
			fragment:   []string{"--resume", res.spelling},
			wantResume: true,
			outcome:    "the bare resume",
		}
	case id.names != "":
		return identityOracle{
			fragment: []string{"--session-id", id.spelling},
			wantID:   true,
			outcome:  "plain --session-id",
		}
	default:
		return identityOracle{outcome: "no identity flags"}
	}
}

// The switch's whole input space, against the two shapes the CLI punishes and
// the two shapes it must produce.
//
// `identityArgs` reads exactly three fields, so the **cross product of those
// three fields is exhaustive over its input** rather than a sample of it -
// which is a different kind of claim from a list of Configs somebody chose, and
// this project has a name for the second one: *"a hand-written list standing in
// for something the code already declares."* Each triple is then built twice,
// once bare and once with every non-identity field set, so an arm that reads a
// *fourth* field is visible as a disagreement rather than as a gap in whatever
// the list happened to cover.
//
// The third dimension arrived with park/wake's arm, and adding it was not
// optional: the two-dimensional product set no ResumeFrom anywhere, so the
// moment the switch grew a third input this walked a *subset* of it while
// claiming to be exhaustive - which is the failure docs/notes/decisions.md
// names twice. A dimension added to the switch is a dimension added here in the
// same change.
//
// Every cell is checked against an oracle computed from the three values' own
// labels, so the assertion is per member. The history is why:
//
//   - **Shipped with two negative assertions and a `built` floor.** Deleting
//     the fork case entirely left it green - every Config then built a plain
//     --session-id, all six counted, and both negatives were vacuously
//     satisfied over a set containing no fork at all.
//   - **A `forks` reach-floor was added, and it was still not enough.**
//     *Narrowing* the fork case rather than deleting it - `case ForkFrom != ""
//     && Model != ""` returning --session-id alone - left the floor satisfied
//     by whichever fork Config avoided the new field, and the whole tree stayed
//     green. Every fork of a session naming a model would have become an
//     ordinary empty agent under the child's id.
//
// **A floor counts that a class occurred; it cannot see the class occurring for
// the wrong subset, and a hand-picked enumeration decides which subsets exist
// to be wrong about.** Both are fixed by the same two moves: assert per member,
// and enumerate from the input space rather than from imagination. See
// docs/notes/decisions.md.
//
// One thing worth knowing about the negative assertions kept below: they are
// not independent. `resume && id && !fork` implies `fork && !resume` is the
// only other punished shape, and the first can never fire on its own. It is
// kept for the diagnosis rather than the coverage - the two shapes fail in
// opposite ways, one loudly at startup and one silently with a plausible empty
// agent, and being told which one you built is the whole difference.
func TestNoConfigProducesAnIllegalIdentityShape(t *testing.T) {
	domain := identityDomain()
	decs := decorations()

	cells := 0
	outcomes := map[string]int{}

	for _, from := range domain {
		for _, res := range domain {
			for _, id := range domain {
				for _, dec := range decs {
					cells++
					outcomes[checkOneIdentityCell(t, from, res, id, dec.name, dec.apply)]++
				}
			}
		}
	}

	// The floors. The per-cell oracle above cannot see an enumeration that
	// stopped enumerating, by construction, so these hold the loop itself.
	if want := len(domain) * len(domain) * len(domain) * len(decs); cells != want {
		t.Fatalf("walked %d cells, want %d - the cross product is what makes this exhaustive over the "+
			"three fields identityArgs reads, and a partial one is a hand-written list wearing its clothes", cells, want)
	}
	for _, outcome := range []string{
		"the fork triple", "the bare resume", "plain --session-id", "no identity flags",
		"refused: fork with no id", "refused: fork onto its own id",
		"refused: resumed under a different id", "refused: fork and resume at once",
	} {
		if outcomes[outcome] == 0 {
			t.Fatalf("no cell reached %q, so every assertion about it above held over nothing.\nreached: %v",
				outcome, outcomes)
		}
	}
}

// checkOneIdentityCell builds one cell and asserts the shape asked for is the
// shape built, returning which outcome the oracle expected so the caller can
// hold the loop to its floors.
func checkOneIdentityCell(t *testing.T, from, res, id identityValue, where string, decorate func(Config) Config) string {
	t.Helper()

	want := expectedIdentity(from, res, id)
	cfg := decorate(Config{ForkFrom: from.spelling, ResumeFrom: res.spelling, SessionID: id.spelling})
	args, err := NewSession(cfg).buildArgs()

	if want.err {
		if err == nil {
			t.Errorf("ForkFrom=%q ResumeFrom=%q SessionID=%q (%s) was accepted and built\n  %v\n"+
				"those are %s, and no shape they build has a recording behind it",
				from.spelling, res.spelling, id.spelling, where, args, want.why)
		}
		return want.outcome
	}
	if err != nil {
		t.Errorf("ForkFrom=%q ResumeFrom=%q SessionID=%q (%s) was refused: %v\n"+
			"that is an ordinary spawn, an ordinary fork or an ordinary wake",
			from.spelling, res.spelling, id.spelling, where, err)
		return want.outcome
	}

	// The shape asked for is the shape built - adjacent and in order, so the
	// fork triple stays one literal.
	if len(want.fragment) > 0 && !containsSeq(args, want.fragment) {
		t.Errorf("ForkFrom=%q ResumeFrom=%q SessionID=%q (%s) had to build\n  %v\nand built\n  %v\n"+
			"a fork request that builds anything else starts an ordinary empty session under the "+
			"id that was meant to receive the fork - exit 0, empty stderr, nothing red",
			from.spelling, res.spelling, id.spelling, where, want.fragment, args)
	}
	checkIdentityFlags(t, from, res, id, where, want, args)
	return want.outcome
}

// checkIdentityFlags asserts that nothing beyond the fragment reached the argv,
// and that neither punished shape did.
//
// Split from the cell check rather than inlined, because these are assertions
// about the *flag set* where the one above is about a fragment's adjacency, and
// together they were past this project's 50-line bound on a function.
func checkIdentityFlags(t *testing.T, from, res, id identityValue, where string, want identityOracle, args []string) {
	t.Helper()

	// Exact flag presence catches an extra flag that containsSeq would read
	// straight past. Read off the oracle rather than re-derived from the
	// labels, because with three arms building flags there is no one formula
	// the derivation could use.
	resume, fork, hasID := hasFlag(args, "--resume"), hasFlag(args, "--fork-session"), hasFlag(args, "--session-id")
	if resume != want.wantResume || fork != want.wantFork || hasID != want.wantID {
		t.Errorf("ForkFrom=%q ResumeFrom=%q SessionID=%q (%s) builds --resume=%v --fork-session=%v --session-id=%v, want %v/%v/%v:\n  %v",
			from.spelling, res.spelling, id.spelling, where, resume, fork, hasID,
			want.wantResume, want.wantFork, want.wantID, args)
	}

	// The two punished shapes, stated as themselves so a failure says which one
	// it is.
	if resume && hasID && !fork {
		t.Errorf("ForkFrom=%q ResumeFrom=%q SessionID=%q (%s) builds --resume together with --session-id and no "+
			"--fork-session:\n  %v\nthe CLI refuses that at startup with nothing on stdout, so the "+
			"only diagnosis is the stderr tail", from.spelling, res.spelling, id.spelling, where, args)
	}
	// One direction only, and the direction is the recorded one.
	// --fork-session with no --resume is accepted and silently ignored - exit
	// 0, empty stderr, SessionStart:startup, an ordinary empty session under
	// the id that was meant to *receive* the fork
	// (fork-session-no-resume.jsonl). The converse is now legal: a bare
	// --resume is what a wake is.
	if fork && !resume {
		t.Errorf("ForkFrom=%q ResumeFrom=%q SessionID=%q (%s) builds --fork-session with no --resume:\n  %v\n"+
			"the CLI accepts that and ignores the flag, so the process starts an ordinary empty session "+
			"under the id that was meant to receive the fork",
			from.spelling, res.spelling, id.spelling, where, args)
	}
}

// withCountingFakeExec is withFakeExec that also counts how many times Start
// reached the exec seam, so a test can assert the thing "refused before there
// is a process at all" actually claims.
//
// It matters because `err != nil` is not that claim: a Start that got as far as
// the process and *then* failed on a pipe returns an error too, and so would a
// Start that grew an unrelated refusal - a Dir check, say - while the fork
// guards were quietly gone. execCommand is the only route to a `claude`
// process in this package, so zero calls is proof none was built or launched.
func withCountingFakeExec(t *testing.T) *int {
	t.Helper()
	orig := execCommand
	reached := 0
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		reached++
		return fakeExec(ctx, name, args...)
	}
	t.Cleanup(func() { execCommand = orig })
	return &reached
}

// refusedBefore asserts Start refused this Config, said why in words naming the
// reason, and started nothing.
func refusedBefore(t *testing.T, cfg Config, wantMsg string) {
	t.Helper()
	reached := withCountingFakeExec(t)
	s := NewSession(cfg)

	err := s.Start(context.Background())
	if err == nil {
		t.Fatalf("Start accepted Config %+v", cfg)
	}
	if !strings.Contains(err.Error(), wantMsg) {
		t.Errorf("Start refused Config %+v with\n  %v\nand the reason has to name %q - "+
			"an error that says something else is a refusal for a reason this test is not about",
			cfg, err, wantMsg)
	}
	if *reached != 0 {
		t.Errorf("Start refused Config %+v but reached the exec seam %d time(s) doing it: the argv is "+
			"built before anything is handed to exec precisely so an unrecorded shape never reaches "+
			"a real agent", cfg, *reached)
	}
	// A refusal happens under the lock with s.cmd still nil, so the session is
	// still startable. Nothing depends on that yet; it is asserted because a
	// half-started session is the expensive way to find out otherwise.
	if pgid := s.Pgid(); pgid != 0 {
		t.Errorf("Start refused Config %+v and left a process group %d behind", cfg, pgid)
	}
}

// A fork with no id of its own would build `--resume <p> --fork-session
// --session-id ""`, which is not a shape anybody has recorded. Refused where
// the argv is built, so no process is started to find out.
func TestStartRefusesAForkWithNoIdOfItsOwn(t *testing.T) {
	refusedBefore(t,
		Config{ForkFrom: idForkParent, Dir: t.TempDir()},
		"needs a Wake-assigned id of its own")
}

// Forking a session onto its own id is the one way to write `--resume <x>
// --fork-session --session-id <x>`, which nothing has recorded and which the
// whole identity model exists to keep impossible: two live processes under one
// id break the roster, maySpawn and the reaper at once.
func TestStartRefusesAForkOntoTheParentsOwnId(t *testing.T) {
	refusedBefore(t,
		Config{SessionID: idForkParent, ForkFrom: idForkParent, Dir: t.TempDir()},
		"cannot be forked onto its own id")
}

// And the spelling case reaches Start too, not only buildArgs - because Start
// is where a caller finds out, and the whole point of refusing in identityArgs
// is that nothing downstream has to know.
func TestStartRefusesAForkOntoTheParentsOwnIdInAnotherSpelling(t *testing.T) {
	refusedBefore(t,
		Config{SessionID: strings.ToUpper(idForkParent), ForkFrom: idForkParent, Dir: t.TempDir()},
		"cannot be forked onto its own id")
}

// Two ids that are not the same string can still be the same session, and the
// guard above has to be about the session rather than about the bytes.
//
// The id space Wake accepts is uuid.Parse's, not string equality's:
// daemon.mintedByWake - the check that decides an id came from Wake at all -
// is uuid.Parse, and uuid.Parse reads all six spellings below as one UUID.
// Probed against google/uuid rather than assumed: uppercase hex, the braced
// form, the urn form and the 32-char undashed form all parse equal to the
// canonical one, while a blank or a leading space does not parse at all.
//
// So a parent handed in canonically and a child handed in uppercase pass every
// check either side of this one and build
// `--resume <x> --fork-session --session-id <X>`. Nothing has recorded that,
// and on macOS's case-insensitive default filesystem the two ids name the same
// <uuid>.jsonl - which is the collision the guard exists to prevent, arriving
// through the one door a string compare leaves open.
func TestAForkOntoTheParentsOwnIdIsRefusedHoweverTheIdIsSpelled(t *testing.T) {
	canonical := idForkParent
	for _, spelling := range []string{
		canonical,
		strings.ToUpper(canonical),
		"{" + canonical + "}",
		"urn:uuid:" + canonical,
		strings.ReplaceAll(canonical, "-", ""),
		strings.ToUpper(strings.ReplaceAll(canonical, "-", "")),
	} {
		// Both directions: the parent may arrive in either spelling too, and
		// the guard must not depend on which side was normalized.
		for _, cfg := range []Config{
			{SessionID: spelling, ForkFrom: canonical},
			{SessionID: canonical, ForkFrom: spelling},
		} {
			args, err := NewSession(cfg).buildArgs()
			if err == nil {
				t.Errorf("a fork of %s onto %s was accepted and built\n  %v\n"+
					"uuid.Parse reads those two strings as the same session, so this is a fork onto the "+
					"parent's own id in a different spelling - two live processes under one id, and one "+
					"transcript file on a case-insensitive filesystem",
					cfg.ForkFrom, cfg.SessionID, args)
			}
		}
	}
}

// The other half of the same rule: normalising the comparison must not start
// refusing forks between genuinely different sessions, however they are spelled.
func TestAForkBetweenTwoDifferentSessionsIsAcceptedHoweverTheyAreSpelled(t *testing.T) {
	for _, cfg := range []Config{
		{SessionID: idForkChild, ForkFrom: idForkParent},
		{SessionID: strings.ToUpper(idForkChild), ForkFrom: idForkParent},
		{SessionID: idForkChild, ForkFrom: strings.ToUpper(idForkParent)},
		{SessionID: strings.ReplaceAll(idForkChild, "-", ""), ForkFrom: idForkParent},
		// Neither is a UUID at all. internal/core does not own well-formedness
		// - see identityArgs - so two distinct non-ids still build a fork.
		{SessionID: "child", ForkFrom: "parent"},
		// And one of each, both ways round: a mixed pair is the only way to
		// reach sameSession's second parse, and "one of them is not a UUID"
		// must read as "not the same session" rather than as an error.
		{SessionID: idForkChild, ForkFrom: "parent"},
		{SessionID: "child", ForkFrom: idForkParent},
	} {
		if _, err := NewSession(cfg).buildArgs(); err != nil {
			t.Errorf("a fork of %s onto %s was refused: %v\n"+
				"those name two different sessions, and normalising the self-fork guard must not "+
				"start refusing ordinary forks", cfg.ForkFrom, cfg.SessionID, err)
		}
	}
}
