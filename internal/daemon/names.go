// The pool of human names, the registry that hands them out, and what happens
// when it runs dry.
//
// # Why the daemon owns this and not the client
//
// "No two live sessions share a name" is a statement about the whole fleet, and
// the only process that can see the whole fleet is this one. A client that
// picked its own name would be picking from a list it cannot check: two `wake`
// invocations racing in two terminals would mint the same name, and the second
// one to arrive would have no way to know. The roster is here, so the names are
// here.
//
// # A name is display; an id is identity
//
// Nothing is ever addressed by name on the wire. rpc.Frame carries SessionID
// and the reaper finds a process group by locating that UUID in its argv, which
// is what makes a SIGKILL to a pid Wake did not spawn defensible. A name that
// became an address would put a word somebody typed where that proof has to be.
// `wake attach <name>` resolves the name to an id in the *client*, and what
// crosses the socket is the id.
//
// # Nothing here survives a daemon restart, deliberately
//
// The roster on disk records each session's name, but this registry starts
// empty every time, and the reason is stronger than "the reaper cleaned up".
//
// **Nothing reads the roster back into s.agents or into this registry.** A
// starting daemon holds no live sessions by construction: loadRoster's only
// consumers are reapOrphans, which signals what it can verify and clears the
// records it finished (retaining what it could not, for a later daemon), and
// FleetOnDisk, which builds a report for a client. No code path turns a record
// into an agent. So there is no session for a persisted name to
// belong to, and reserving one would reserve it against nothing.
//
// Note that reapOrphans is *not* unconditional - Serve gates it on
// lock.exclusive, because "I could not take the lock" is not "nobody else holds
// it" and the difference is 15-30 SIGKILLs. A daemon that starts without the
// exclusive claim leaves the previous fleet alone. That does not weaken the
// paragraph above: it still holds none of those sessions, and could not talk to
// one if it wanted to.
//
// The roster's name and label are display for `wake status` on a machine whose
// daemon died, and that is why the on-disk format did not have to change.

package daemon

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/DilanDoshi/wake/internal/core"
)

// maxNameLen bounds a name a client asks for. It is a **safety** bound as much
// as a display one, and the safety half is the one not to relax.
//
// The display half is ordinary: `wake status` sizes its first column against
// it, and a name longer than this is a sentence in the column that is supposed
// to make thirty agents scannable.
//
// The safety half is that a name is handed to the child as `--name`, so it
// lands in that process's argv - and the reaper's entire proof that a process
// group is still the agent it recorded is `strings.Contains(argv, sessionID)`.
// A name able to *contain* another session's UUID would forge that proof: after
// pid reuse, `verifyAgent(pid, idA)` inspects a process that is now B, finds idA
// inside B's `--name`, and SIGKILLs B's whole group mid-Edit. A UUID is 36
// characters. **The only thing standing between that and this build is this
// constant being smaller than 36** - not the character set, which admits every
// character a UUID is made of.
//
// TestANameCannotCarryASessionIDIntoAnAgentsArgv pins that relationship against
// a real UUID rather than leaving it to this comment, because a comment is what
// somebody widening a column for cosmetic reasons reads and then disbelieves.
const maxNameLen = 24

// nameOrdinalSep joins a pooled name to the ordinal an exhausted pool puts on
// it: alex-2. A hyphen because it is already legal inside a name, so the result
// is a name the same validation accepts.
const nameOrdinalSep = '-'

// namePool is the set of names a session is drawn from when nobody asked for
// one. Spec §5: "agents get a random human name on creation (sydney, alex,
// john, …)".
//
// Three properties are enforced by names_test.go rather than left to whoever
// adds the next one, because all three are invisible from here:
//
//   - No name appears twice. A duplicate would silently shrink the pool, and
//     the prefix property below cannot see one - it compares distinct entries.
//   - No name is a prefix of another. `wake attach` resolves a unique prefix
//     across names and ids together, so a pooled name that prefixes another
//     pooled name makes an attach ambiguous between two agents nobody named.
//   - No name is entirely hex characters. A session id is a UUID and its first
//     eight characters are what `wake status` prints; a name like "ada" or
//     "cafe" could be a prefix of one, which would collide the two spaces.
//     normalizeName enforces the same predicate on a name somebody *chooses*,
//     which is where it actually bites - see isHex.
//
// The size is chosen against the fleet the spec sizes for - 15-30 sessions - so
// that a full fleet leaves most of the pool free and the numbered fallback
// below stays a corner rather than the ordinary experience.
var namePool = []string{
	"alex", "john", "sydney", "maya", "omar", "priya", "luca", "nora",
	"kenji", "ivy", "mateo", "ruth", "felix", "hana", "oscar", "lena",
	"dmitri", "yara", "tomas", "freya", "ravi", "ingrid", "pablo", "wren",
	"hugo", "zaid", "clara", "milo", "noor", "silas", "esme", "jonas",
	"tariq", "alma", "viktor", "rosa", "elias", "thea", "kwame", "juno",
	"marco", "otis", "leila", "nikhil", "greta", "dante", "mira", "soren",
	"aisha", "bruno", "tessa", "kai", "delia", "sana", "iris", "hakim",
	"orla", "petra", "yusuf", "mabel", "quinn", "rafi", "tova", "ulric",
}

// reservedNames are the words the room's routing spends on something other
// than one agent, so no agent may wear one. `wake new all` would otherwise
// silently disable broadcast, and an agent called manager would take every
// message meant for the service.
//
// They are the router's own constants rather than a second spelling of them.
// The alternative - writing "all" and "manager" out here so the daemon does
// not reach into a client concern - is a hand-written list standing in for
// something the code already declares, which is the shape this project has
// been caught by five times: renaming BroadcastName would leave the daemon
// reserving a word nothing routes and handing out one that routes. The daemon
// already imports internal/core for Session, and a constant is not the router.
//
// What the import cannot carry is *completeness* - a third routing word added
// to router.go and not to this map. TestTheReservedNamesAreExactlyTheOnesRoutingSpends
// closes that by deriving router.go's name-shaped literals from its AST and
// requiring this set to equal them.
var reservedNames = map[string]bool{
	core.BroadcastName: true,
	core.ManagerName:   true,
}

// nameRegistry is which names the live fleet is using.
//
// It is a set rather than a derivation from s.agents, and the difference is a
// window rather than a style: a name has to be reserved *before* core.NewSession
// is called, because it becomes the child's --name on the command line, and the
// agent does not enter s.agents until that process has started. Two spawns in
// that window would both look free.
type nameRegistry struct {
	mu    sync.Mutex
	taken map[string]struct{}

	// pick chooses an index in [0,n). A seam for tests, and the only
	// nondeterminism in this file.
	pick func(n int) int
}

func newNameRegistry() *nameRegistry {
	return &nameRegistry{taken: make(map[string]struct{}), pick: rand.IntN}
}

// claim reserves a name for a session that is starting, and is the only way one
// is handed out.
//
// An empty request means "pick one", which is what bare `wake` sends. A
// non-empty one is a person naming an agent on purpose, and it fails rather
// than being quietly renamed: somebody who typed `wake new sydney` wants
// sydney, and being given sydney-2 without being told is worse than being told.
func (r *nameRegistry) claim(requested string) (string, error) {
	name, err := normalizeName(requested)
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" {
		name = r.freeNameLocked()
	} else if _, held := r.taken[name]; held {
		return "", fmt.Errorf("a live session is already called %q; `wake attach %s` opens it, or choose another name", name, name)
	}
	r.taken[name] = struct{}{}
	return name, nil
}

// claimManager reserves the one name claim refuses to everybody, and is how
// there comes to be exactly one manager.
//
// It is a second entry point rather than a flag on claim, because the two are
// asking different questions. claim takes a name a *client* asked for and
// normalizeName refuses the reserved ones, which is what stops `wake new
// manager` silently taking every message meant for the service. This takes no
// argument at all: there is one manager name, the daemon chooses it, and there
// is no string here for a caller to get wrong.
//
// **The refusal of a second manager is this, and nothing else.** @manager
// addresses one session; two would make the room's default addressee a coin
// flip, and both would hold tools that act on the whole fleet. A separate check
// would be a second answer to a question the registry already answers
// atomically - and the registry is the only thing that answers it before a
// process has been started.
// errManagerNameHeld is claimManager's whole failure, and it deliberately says
// nothing an operator would read.
//
// The registry knows a name is taken; it does not know whether the session
// holding it is running or parked, and those need opposite sentences - one says
// @manager reaches it, the other says a keystroke parked it and /resume brings
// it back. A registry that guessed would produce the confidently-wrong half of
// that pair, which is exactly what shipped first.
var errManagerNameHeld = errors.New("the manager name is already claimed")

func (r *nameRegistry) claimManager() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, held := r.taken[core.ManagerName]; held {
		// The name and nothing else, because a registry cannot know **what** is
		// holding it: a parked session keeps its name claimed, and the sentence
		// an operator needs is completely different in that case. The caller
		// has the fleet - see server.managerRefusal.
		return "", errManagerNameHeld
	}
	r.taken[core.ManagerName] = struct{}{}
	return core.ManagerName, nil
}

// release gives a name back. Every path out of a session reaches it, including
// the ones where the process never started, so releasing a name nothing holds
// is a no-op rather than a fault.
func (r *nameRegistry) release(name string) {
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.taken, name)
}

// freeNameLocked draws a name for a session that did not ask for one.
func (r *nameRegistry) freeNameLocked() string {
	free := make([]string, 0, len(namePool))
	for _, n := range namePool {
		if _, held := r.taken[n]; !held {
			free = append(free, n)
		}
	}
	if len(free) > 0 {
		return free[r.pick(len(free))]
	}
	return r.numberedLocked(namePool[r.pick(len(namePool))])
}

// numberedLocked is what an exhausted pool answers with: a pooled name and the
// first ordinal nothing holds.
//
// # Why exhaustion is answered rather than refused
//
// Because a display name is not allowed to be the reason somebody cannot start
// an agent. The pool is sized so this is unreachable at the fleet the spec
// describes, but "unreachable" is a claim about today's cap and not about the
// machine somebody runs this on - and the failure of refusing is that `wake`
// stops working with a message about names. A name is display; an id is
// identity; the identity is a UUID and is never in short supply.
//
// The other answer considered was a name derived from the session id -
// "a4f78b3d" - and it is worse in the way that matters: the whole point of §5's
// naming is that a fleet is talked about out loud, and a hex string is the thing
// names exist to replace.
//
// # Why the search is bounded, and what the bound is not
//
// `n == last` is a **stop, not an answer**, and it is offered as neither a
// guard nor a fallback: it cannot fire on any state this registry can reach.
// This is only called when every pooled name is held, so of the len(taken)
// candidates tried at most len(taken)-len(namePool) can be taken, and a free one
// turns up almost immediately. An earlier draft returned a value after the loop
// and claimed that line was reachable. It was not, mutating it to "" survived
// the whole suite, and a branch no test can hold is not a guard - the same
// reasoning that deleted matchSession's whole-id pass.
//
// The bound stays, because it is doing a different job from a guard. Written as
// an open loop, mutating ordinalName **hung the test binary instead of failing
// it**: this runs inside the registry lock on the spawn path, so an
// implementation that stops producing distinct candidates wedges every spawn on
// the machine, and a loop that cannot end reports nothing at all. The bound
// turns that into a wrong answer, which is something a test can be about - and
// TestAnExhaustedPoolStillNamesTheNextSession now fails it in 0.00s.
func (r *nameRegistry) numberedLocked(base string) string {
	last := len(r.taken) + 2
	for n := 2; ; n++ {
		candidate := ordinalName(base, n)
		if n == last || !r.heldLocked(candidate) {
			return candidate
		}
	}
}

func (r *nameRegistry) heldLocked(name string) bool {
	_, held := r.taken[name]
	return held
}

// ordinalName is a pooled name with a number on it: alex-2.
func ordinalName(base string, n int) string {
	return fmt.Sprintf("%s%c%d", base, nameOrdinalSep, n)
}

// normalizeName folds a requested name into the one form the registry stores,
// or says why it cannot be one.
//
// An empty result is not a failure: it is "nobody asked", and claim reads it
// that way.
//
// The character set is narrow on purpose. A name reaches three places that a
// free-form string has no business reaching: the child's argv as --name, the
// on-disk roster, and `wake status`'s columns. Letters, digits, hyphen and
// underscore cover every name the pool holds and every one a person would
// choose, and exclude the whitespace that would break a column, the shell
// metacharacters that would make an argv interesting, and the empty-looking
// names that would render as a session with no name at all.
func normalizeName(requested string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(requested))
	if name == "" {
		return "", nil
	}
	if len(name) > maxNameLen {
		return "", fmt.Errorf("a name is at most %d characters and %q is %d", maxNameLen, name, len(name))
	}
	if !isNameStart(rune(name[0])) {
		return "", fmt.Errorf("a name must start with a letter, and %q does not", name)
	}
	for _, r := range name {
		if !isNameRune(r) {
			return "", fmt.Errorf("a name may hold letters, digits, %q and %q, and %q holds more than that", "-", "_", name)
		}
	}
	if couldPrefixASessionID(name) {
		return "", fmt.Errorf("a name that can be the front of a session id would shadow somebody else's conversation, and %q is one; `wake status` prints eight of those characters and `wake attach` resolves them", name)
	}
	if reservedNames[name] {
		return "", fmt.Errorf("%q is how the room addresses something other than one agent, so it cannot also be an agent's name; choose another", name)
	}
	return name, nil
}

// couldPrefixASessionID reports whether a name can be the front of a session id.
//
// This is the third pool property, and it is enforced on a name somebody
// *chooses* rather than only over the pool - which is where it bites, because
// nobody chooses "ravi" by accident and somebody will choose "beefcafe".
//
// The harm is precise. `wake status` prints eight characters of a session id and
// invites them to be copied; matchSession's first pass resolves a *whole name*
// before it looks at the id space at all, because exactness beats partiality.
// So a chosen name that matches a printed short id wins that pass, and the
// person who copied the id column lands in a different agent's live conversation
// - the harm pickOne's own doc names, "picking one for somebody is not a
// recoverable mistake - they type into it". Both readings are complete there, so
// no ordering rule can separate them; the name has to be refused at the door.
//
// # It asks about the template, not about the alphabet
//
// It used to ask "is every character hex", and that is the wrong question in
// both directions. **The hyphen is a legal name rune and sits at UUID positions
// 8, 13, 18 and 23**, so `a4f78b3d-1e2f` is a genuine prefix of a session id and
// walked straight past an alphabet test - which is the whole class the rule
// exists for, arriving through the one character the rule did not look at. And a
// twelve-character all-hex name is *not* a prefix of any UUID, because position
// 8 has to be a hyphen, so the alphabet test refused names that were never a
// hazard.
//
// A name is folded and validated before it gets here, so every rune is already
// lowercase and already legal; this only has to answer the positional question.
func couldPrefixASessionID(s string) bool {
	if len(s) > len(sessionIDTemplate) {
		return false
	}
	for i, r := range s {
		if sessionIDTemplate[i] == sessionIDSeparator {
			if r != sessionIDSeparator {
				return false
			}
			continue
		}
		if !strings.ContainsRune(hexDigits, r) {
			return false
		}
	}
	return true
}

// sessionIDTemplate is the shape of a canonical UUID: a hex digit everywhere the
// mark is, and the separator everywhere else. Written as a template rather than
// as four lengths so the positions are readable at a glance and cannot drift out
// of step with each other.
const (
	sessionIDTemplate  = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
	sessionIDSeparator = '-'
)

// hexDigits is the alphabet a session id is written in. Lowercase only: a name
// is folded before it reaches couldPrefixASessionID.
const hexDigits = "0123456789abcdef"

// isNameStart is the first character rule: a letter, so that no name can be
// read as a number and none begins with the punctuation the ordinal uses.
func isNameStart(r rune) bool { return r >= 'a' && r <= 'z' }

func isNameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	case r == nameOrdinalSep, r == '_':
		return true
	default:
		return false
	}
}
