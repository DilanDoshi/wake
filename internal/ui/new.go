package ui

// `/new` — starting an agent from inside the room.
//
// # The gap this closes
//
// docs/goals.md §3: *"Wake can manage agents but cannot create or name them
// from inside itself"* — the largest single gap between the founding message
// and the build, and the founding message's own spelling of the fix is
// `/new agent(session) in <project dir or by default at root dir>`. Every
// session before this one was started from a shell.
//
// # What it decides, which is almost nothing
//
// The same division ⌃F makes, for the same reason: this view never touches a
// process. `/new` mints a UUID — Wake originates identity, and `maySpawn`
// refuses anything that is not one because the reaper's only proof of a process
// group is that id in an argv — resolves the directory, and writes one
// `rpc.FrameSpawn`. **The name is the daemon's**: an empty `Text` means "draw
// one from the pool", a non-empty one is a request the daemon may refuse, and
// no local check stands in front of either, because "no two live sessions share
// a name" is a statement about the whole fleet and only the daemon can see the
// fleet. A cheerful pre-check here would be a second copy of `normalizeName`,
// wrong from the first day the two disagree.
//
// # The directory, and why it is not the focused agent's
//
// `rpc.Frame.Dir` is where the agent runs and where claude persists its
// transcript, so an absent one is not a refusal into safety: `spawnDir` falls
// back to the daemon's own working directory, which is whichever repository
// happened to fork it. So one is always sent, and the default is **`wake`'s own
// working directory** — the same directory `wake new` typed in that terminal
// would send, so the two verbs agree.
//
// The tempting alternative is the focused conversation's directory, which
// `bangDir` uses. It is right there and wrong here, and the difference is who
// the draft is addressed to: a bang is addressed *to* a conversation and its
// output lands in one, while `/new` is addressed to Wake. A default that
// followed the focus would be one command starting agents in two repositories
// depending on which pane had the keys, with nothing on screen saying which.
// `in <dir>` is how an operator says otherwise, and it is the founding
// message's own syntax.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// newFailed names the write that could not happen, so the notice row says
	// which command was typed rather than only what the socket said about it.
	newFailed = "starting a new agent"

	// dirKeyword separates the name from the directory, and it is the founding
	// message's word rather than a flag: `/new sydney in ~/project`.
	dirKeyword = "in"

	// agentNoun and sessionNoun are the nouns the founding message wraps the
	// verb in — *"`/new agent(session) in <dir>`"* — and they are skipped
	// rather than taken as a name.
	//
	// This is the one piece of vocabulary on this side of the socket, and it is
	// here because the alternative is worse in the direction this project cares
	// about: `/new agent in ~/p` would otherwise start an agent *called* agent,
	// silently, on the exact string the product was asked for in. The cost is
	// that `/new` cannot name an agent `agent` or `session`; `wake new agent`
	// at a shell still can, and neither is a name anybody wants at 30 sessions.
	agentNoun   = "agent"
	sessionNoun = "session"

	// startedFormat is what a fresh agent says when the daemon confirms it.
	// The label is the second display half — the branch checked out where it
	// was started — which is the vocabulary the roster already answers "which
	// of these thirty" in.
	startedFormat = "%s%s started in %s"

	// startedUnlabelled is the same line for a session whose directory names
	// nothing: outside a repository, on a detached HEAD. taskLabel returns
	// empty there deliberately, and `started in ` with nothing after it reads
	// as information and is not any.
	startedUnlabelled = "%s%s started"

	// homePrefix is what an operator types for their home directory. A shell
	// would have expanded it before `wake new` ever saw it; nothing expands it
	// on the way from a composer, and the daemon refuses a path that is not
	// absolute — so `~/project` would be a refusal about a syntax that works
	// everywhere else the operator types.
	homePrefix = "~"

	// newAsked and newAskedNamed are said on the keypress rather than on the
	// confirmation, because the daemon may refuse and the operator should know
	// the command was read either way. Two of them fold into one notice entry
	// with a count, which is the right reading of two agents asked for.
	newAsked      = "starting an agent…"
	newAskedNamed = "starting %s%s…"

	// noWorkingDir and noHomeDir are the two questions this side has to answer
	// and can fail to. Both are refusals rather than a spawn with no directory:
	// an absent Dir is not refused by the daemon, it is silently answered with
	// the daemon's own directory, which is the mistake this field exists to end.
	noWorkingDir = "wake cannot tell which directory it is running in, so it cannot say where a new agent should run"
	noHomeDir    = "wake cannot find your home directory, so it cannot resolve that path"
)

// startedLine is what the room says when an agent it asked for arrives.
func startedLine(name, label string) string {
	if label == "" {
		return fmt.Sprintf(startedUnlabelled, agentPrefix, name)
	}
	return fmt.Sprintf(startedFormat, agentPrefix, name, label)
}

// newAgent asks the daemon to start an agent, and waits for it the way ⌃F waits
// for a fork.
//
// It reports the ask on the keypress and the arrival on the report, which is
// the split fork.go argues: the daemon may refuse — a name already taken, a
// directory that is not there — and the operator should know the command was
// read either way, while the room's mention of it (see starts.go's
// draftMention) may not be drafted until something exists behind it.
func (a App) newAgent(arg string) (App, tea.Cmd) {
	req, err := parseNew(arg)
	if err != nil {
		notice.Report("%s", err.Error())
		return a, nil
	}
	id := uuid.NewString()
	a = a.clearDraft().awaitingStart(id)
	notice.Report("%s", askedFor(req.Name))
	return a, a.write(newFailed, rpc.Frame{
		Kind:          rpc.FrameSpawn,
		SessionID:     id,
		Text:          req.Name,
		Dir:           req.Dir,
		Worktree:      req.Worktree,
		AddDir:        req.AddDir,
		Debug:         req.Debug,
		DebugFile:     req.DebugFile,
		MaxBudgetUSD:  req.MaxBudgetUSD,
		FallbackModel: req.FallbackModel,
	})
}

// askedFor is the keypress line: the name that was asked for, or that one was
// not. It never invents a name — the pool is the daemon's, and a client that
// guessed would be wrong for every agent it did not name.
func askedFor(name string) string {
	if name == "" {
		return newAsked
	}
	return fmt.Sprintf(newAskedNamed, agentPrefix, name)
}

// newRequest is what `/new` resolved out of what was typed: the two words the
// grammar counts, and the flags newflags.go stripped before counting them.
//
// One value rather than a return per field, because the frame it builds has six
// and a signature of six results is one nobody can read a call site of.
type newRequest struct {
	Name string
	Dir  string
	newFlags
}

// parseNew reads `[name] [in <dir>]`, and the flags, into what the frame
// carries.
//
// Every failure of the *grammar* is the same sentence, because every one of
// them is the operator having typed a shape this does not read and the useful
// answer is what the shapes are. A flag's own refusal names the flag, because
// there the operator typed a shape this reads and a value it will not send.
func parseNew(arg string) (newRequest, error) {
	fields, flags, err := takeNewFlags(strings.Fields(arg))
	if err != nil {
		return newRequest{}, err
	}
	before, after, said := fields, []string(nil), false
	for i, f := range fields {
		if strings.EqualFold(f, dirKeyword) {
			before, after, said = fields[:i], fields[i+1:], true
			break
		}
	}
	// An `in` with nothing after it is a sentence somebody did not finish, and
	// both available guesses are silent: start in the default directory, which
	// is the one thing they said they did not want, or send `in` as a name.
	if said && len(after) == 0 {
		return newRequest{}, errUsage()
	}
	req := newRequest{newFlags: flags}
	// The words before `in` are the requested name, hyphen-joined: `/new john
	// doe` asks for `john-doe`, one token normalizeName accepts and `@john-doe`
	// routes to. strings.Fields dropped empty tokens, so there are no double
	// hyphens, and an over-length join is the daemon's refusal to make. The lone
	// founding noun - `/new agent in <dir>` - names nothing and draws from the
	// pool; a noun with words after it (`/new agent smith`) is a chosen name.
	if len(before) != 1 || !isNoun(before[0]) {
		req.Name = strings.Join(before, "-")
	}
	// Joined rather than taken as one field: a directory may hold spaces, and
	// the operator typing one has no way to quote it into a composer.
	req.Dir, err = absoluteDir(strings.Join(after, " "))
	return req, err
}

func errUsage() error { return errors.New(newUsage) }

// isNoun reports whether a word is the founding message's noun rather than a
// name somebody chose.
func isNoun(word string) bool {
	return strings.EqualFold(word, agentNoun) || strings.EqualFold(word, sessionNoun)
}

// absoluteDir turns what was typed into the absolute path the daemon requires,
// against `wake`'s own working directory.
//
// The daemon refuses a relative path rather than resolving it, because it would
// resolve against the *daemon's* directory — one process for the whole machine,
// started from whichever repository forked it. So the resolution has to happen
// on the side that knows what the operator meant, which is this one.
func absoluteDir(typed string) (string, error) {
	base, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("%s: %w", noWorkingDir, err)
	}
	switch {
	case typed == "":
		return base, nil
	case typed == homePrefix || strings.HasPrefix(typed, homePrefix+string(filepath.Separator)):
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("%s: %w", noHomeDir, herr)
		}
		return filepath.Join(home, strings.TrimPrefix(typed, homePrefix)), nil
	case filepath.IsAbs(typed):
		return filepath.Clean(typed), nil
	default:
		return filepath.Join(base, typed), nil
	}
}
