package core

// Routing: where a message goes.
//
// Pure. Text and the live fleet in, ids out - no processes, no I/O, no state.
//
// # `@` means two things and only one of them is Wake's
//
// Wake's design routes on a leading @name; Claude Code expands @path into the
// file's contents before the model sees it, with no tool call at all. That was
// measured through Wake's own argv rather than assumed: asking a session about
// @note.txt came back with the file's contents and no tool call in the stream.
// In a DM there is no conflict - there is one agent and nothing to route to,
// so @ is always a file, and it already works.
//
// In the room they collide, and this is the resolution: **a live agent name
// wins, anything else passes through untouched.** That is safe because names
// come from a 64-name pool of short human words and daemon/names.go guarantees
// no name is a prefix of another, so `@sydney` cannot be the front of a path
// somebody meant - provided the match is on the whole token, which is why
// splitMention takes a word and not a run of name characters. The residual
// ambiguity is a file literally named `alex` at the repository root - rare,
// and Route.Resolved is what makes it recoverable: the room shows what it
// resolved to before the message goes anywhere.
//
// Nothing here reimplements @file, and nothing here may. The mention is
// removed from the text only when it resolved to a live agent; every other
// byte of what was typed reaches the CLI exactly as typed, which is what keeps
// the half that already works working.
//
// # Only an id crosses the socket
//
// This returns ids, never names. daemon/names.go rules that nothing is ever
// addressed by name on the wire and a test enforces it, because the reaper
// proves a process group by finding a session UUID in its argv - a name that
// became an address would put a typed word where that proof has to be.
// Route.Resolved is the other half of the same rule read backwards: it carries
// a name and never an id, because it is for a person to read.

import (
	"strings"
	"unicode"
)

// The two names that are not one ordinary agent, and the two the daemon
// therefore refuses to hand out. daemon/names.go's normalizeName reads these
// constants directly, and TestTheReservedNamesAreExactlyTheOnesRoutingSpends
// derives this file's own name-shaped constants out of its AST and requires
// the daemon to refuse every one - so a third routing word cannot be added
// here and left claimable there.
const (
	// BroadcastName addresses every live agent. It is a name rather than a
	// chord because a chord is a capability that depends on a terminal: ⌃↵
	// requires the Kitty keyboard protocol to arrive at all, and a broadcast
	// that only works in some terminals is worse than one that is typed.
	BroadcastName = "all"

	// ManagerName is the manager session's name, and the manager is an
	// ordinary Addressee wearing it: `@manager` resolves down the same path
	// `@sydney` does, with no special case anywhere below.
	//
	// The special case was written and deleted. Sending `@manager` to the
	// default addressee reads well until there is no manager - the default is
	// then whatever the caller nominated, so the mention would be stripped,
	// the message would reach some agent, and Resolved would say "manager"
	// over the top of it. A misroute with a confident label is the one failure
	// Resolved exists to prevent, so `@manager` with no manager live is text,
	// exactly like any other name nothing answers to.
	//
	// It is reserved for the reason BroadcastName is: an agent called manager
	// would silently take every message meant for the service.
	ManagerName = "manager"
)

// mentionPrefix is the character that opens a mention, and the whole of the
// collision this file resolves.
const mentionPrefix = "@"

// Addressee is one thing a message can be sent to.
type Addressee struct {
	ID   string
	Name string
}

// Route is where a message is going and what will be sent.
type Route struct {
	// Targets is the session ids to send to.
	//
	// It is empty only where there is provably nothing to send to: a
	// broadcast with no live agents, or a fall-through when the caller
	// nominated no default addressee. Both are states the room can see and
	// draw, because Broadcast and Resolved still say what was meant - an
	// invented recipient would be the message going somewhere nobody asked
	// for, and a blank id would be one lost with a receipt.
	Targets []string

	// Text is what to send: what was typed, with a leading mention removed
	// only if that mention resolved, and everything else - including inline
	// @paths - untouched.
	//
	// **One caller deliberately breaks that**, and it is the exception rather
	// than a second rule. internal/ui's open mention mode widens a resolved
	// @name to the fleet and KEEPS the name in the text, because the whole
	// point of open is that the other agents can see who was addressed - a
	// message stripped of its mention arrives at twenty agents saying "ship
	// it" with no subject.
	//
	// The cost is that this is the first path in the build that puts a leading
	// @name on an agent's stdin. Claude Code expands a leading @ before the
	// model sees it (see the header), so those agents receive a draft whose
	// first token their CLI treats as a file reference. What a leading @ naming
	// nothing does is UNRECORDED - see docs/notes/deferred.md - and Resolved's
	// usual mitigation does not reach it: the room shows what it resolved to,
	// which is true about the routing and silent about the expansion.
	Text string

	// Broadcast marks N turns rather than one, so a caller can show the count
	// before it fires.
	Broadcast bool

	// Resolved is the name a leading mention matched - an agent's, or
	// BroadcastName - and "" when nothing was routed. It is what the room
	// displays before sending, so `@alex` meaning a file is a visible mistake
	// rather than a silent one. Never an id: this is for a person to read.
	Resolved string
}

// Resolve decides where text goes.
//
// # live and service are two lists because they are two kinds of recipient
//
// live is the **fleet**: the agents doing the work. service is the manager, or
// a zero Addressee when there is none. The manager is addressable by name like
// any agent - `@manager` resolves down this same path, with no special case
// below - and it is where an unaddressed draft goes, which is the whole reason
// this function takes a default at all.
//
// **What it is not is a member of the fleet**, and that is why it arrives
// separately rather than as a row in live with a flag on it. A broadcast is to
// the fleet; the manager is the thing that manages the fleet, and a manager
// told to report on the fleet by a message it also received is a manager
// reporting on itself. Passing it apart makes that exclusion true by
// construction rather than by a filter inside broadcast that a later change
// could widen.
//
// A zero service is "there is no manager", and then an unaddressed draft
// resolves to nobody: a blank id would be a message lost with a receipt, and
// picking an agent would be the misroute Resolved exists to prevent.
func Resolve(text string, live []Addressee, service Addressee) Route {
	mention, rest, ok := leadingMention(text)
	switch {
	case !ok:
		return passThrough(text, service)
	case mention == BroadcastName:
		return broadcast(rest, live)
	}
	for _, a := range live {
		if a.Name == mention {
			return Route{Targets: []string{a.ID}, Text: rest, Resolved: a.Name}
		}
	}
	// The service answers to its own name, after the fleet: it is not in live,
	// and a mention that matches nothing there may still be it.
	if service.ID != "" && service.Name != "" && service.Name == mention {
		return Route{Targets: []string{service.ID}, Text: rest, Resolved: service.Name}
	}
	// Not a live agent and not the service, so it is a path, or a name that has
	// ended. Either way the text goes through exactly as typed - the CLI
	// expands @path itself.
	return passThrough(text, service)
}

// leadingMention splits text into the name a leading `@` names and the body
// after it. ok is false when there is no mention to speak of, which includes a
// bare `@`: an empty mention names nobody, and without that an Addressee whose
// Name was never set would answer to `@ hello` and quietly receive it.
func leadingMention(text string) (mention, rest string, ok bool) {
	body := strings.TrimLeftFunc(text, unicode.IsSpace)
	if !strings.HasPrefix(body, mentionPrefix) {
		return "", "", false
	}
	mention, rest = splitWord(strings.TrimPrefix(body, mentionPrefix))
	if mention == "" {
		return "", "", false
	}
	return mention, rest, true
}

// splitWord takes the first whitespace-delimited word and returns it with what
// follows, less the whitespace that separated them.
//
// A word rather than a run of name characters, and that is the safety property
// rather than a detail: the argument for treating `@sydney` as an agent at all
// is that no pooled name is a prefix of another, and a matcher that took a
// prefix of what was typed would route `@sydney/notes.md` and eat the path.
func splitWord(s string) (word, rest string) {
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimLeftFunc(s[i:], unicode.IsSpace)
}

// broadcast addresses every live agent, and nobody when there is nobody.
func broadcast(rest string, live []Addressee) Route {
	ids := make([]string, 0, len(live))
	for _, a := range live {
		ids = append(ids, a.ID)
	}
	return Route{Targets: ids, Text: rest, Broadcast: true, Resolved: BroadcastName}
}

// passThrough is the answer for everything Wake does not route: the text
// exactly as it was typed, addressed to the service.
//
// It carries Resolved, which the version with no manager did not and could not:
// an unaddressed draft *is* routed now, and the room draws where ↵ will send
// before the key is pressed. Saying nothing there would make `@src/auth.ts is
// the file` look unaddressed while going to the manager - a silent route, which
// is the failure this field exists to prevent, arriving on the one draft nobody
// addressed on purpose.
func passThrough(text string, service Addressee) Route {
	if service.ID == "" {
		return Route{Text: text}
	}
	return Route{Targets: []string{service.ID}, Text: text, Resolved: service.Name}
}
