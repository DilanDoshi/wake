// What a spawn frame is allowed to configure, and what it is refused for.
//
// Split out of spawn.go, which owns the spawn *path* - claim a name, make a
// directory, launch, publish. What is here answers one question between them:
// is this configuration something a session may be started with. spawn.go was 2
// lines over the 800-line hard max when the fourth check arrived, which is the
// guard naming the seam the way it named watchdog.go's.
//
// # Two doors, and configFor between them
//
// `configRefusal` reads a spawn **frame**, before a name is claimed and before
// anything is started. `launchRefusal` reads the **Config**, at the last place
// before the argv, so a wake, a fork and an import get it too - those build a
// Config from a row or from a file parkbook.go's own header says somebody may
// have edited by hand. `configFor` is the one thing in this file that is
// neither: it is a method, because a debug log's *name* becomes a path here and
// that needs the socket and can fail.
//
// # Why the frame's checks are refusals in one function rather than a validator
//
// Every one has the same shape - `absent is fine, present-and-wrong is a
// refusal` - and none of them may be collapsed into a shared predicate, because
// what they are checking is not the same kind of thing. Effort's set is closed;
// a model's is unknowable; an amount has a shape; a chain is an amount of
// nothing and a list of the unknowable; a path is none of those. The uniformity
// is in when they run, not in what they know, and rpc.Frame's own comments carry
// that argument per field.
//
// **The empty string is never a refusal.** It means "Wake chose nothing"
// throughout, which leaves the flag off the argv entirely and lets claude apply
// its own default. Absent and invalid are different, and a build that conflated
// them would refuse every spawn that configured nothing - which is most of them.

package daemon

import (
	"fmt"
	"path/filepath"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// configRefusal is why this frame's configuration cannot start a session, or ""
// for one that can.
//
// A string rather than an error because every caller does the same thing with
// it - enqueues it as an error frame addressed to the spawning id - and an error
// type here would be a wrapper nothing unwraps.
//
// It runs **before the name is claimed and before anything is started**, which
// is the property that makes these fields safe to carry on a socket at all: a
// value this build does not accept costs no name, no directory and no process,
// so the worst a client that never ran cmd/wake's own parser can do is get a
// sentence back.
func (s *server) configRefusal(f rpc.Frame) string {
	switch {
	// A level this build does not know must cost nothing. Refusing it here is
	// what makes rpc.Frame.Effort safe to carry.
	case f.Effort != "" && !core.ValidEffort(f.Effort):
		return fmt.Sprintf("unknown effort %q", f.Effort)

	// A model is refused only for being absent-but-present: "" already leaves
	// the flag off, so a frame setting the field to something ValidModel
	// refuses is a client contradicting itself. There is no list to check
	// against - see rpc.Frame.Model.
	case f.Model != "" && !core.ValidModel(f.Model):
		return fmt.Sprintf("unknown model %q", f.Model)

	// Zero and negatives are refused rather than clamped: a cap of nothing is a
	// spawn an operator reads as uncapped and claude reads as stop. See
	// core.ValidBudget.
	case f.MaxBudgetUSD != "" && !core.ValidBudget(f.MaxBudgetUSD):
		return fmt.Sprintf("%q is not a spend ceiling: --max-budget-usd takes an amount above zero", f.MaxBudgetUSD)

	// A link naming nothing is the shape a trailing comma produces, and no frame
	// afterwards would report it.
	case f.FallbackModel != "" && !core.ValidFallbackModel(f.FallbackModel):
		return fmt.Sprintf("%q is not a failover chain: --fallback-model takes models separated by commas, and every one of them has to name something", f.FallbackModel)
	}

	// The two path fields, which do not fit the switch's shape: one is a list
	// and both carry their own sentence. See rpc/paths.go for why one of them is
	// fenced as a path and the other as a name.
	if err := rpc.ValidAddDirs(f.AddDir); err != nil {
		return err.Error()
	}
	if f.DebugFile != "" {
		if err := rpc.ValidDebugFileName(f.DebugFile); err != nil {
			return err.Error()
		}
		if why := s.debugFileRefusal(debugFileLocation(s.socket, f.DebugFile)); why != "" {
			return why
		}
	}
	if f.Debug != "" && !core.ValidDebugFilter(f.Debug) {
		return fmt.Sprintf("%q is not a debug filter: it takes categories separated by commas, each optionally negated with !, as in api,hooks or !1p,!file", f.Debug)
	}
	return debugPairing(f.Debug, f.DebugFile)
}

// debugPairing refuses a filter with nothing to write to.
//
// `--debug` alone produces nothing readable in the mode every agent runs in -
// recorded in core/debug.go's header - so it is a flag an operator believes
// they turned on and no log anywhere. Refused rather than emitted, and refused
// rather than silently paired with a name Wake chose, which would be a file
// nobody asked for under a name nobody could guess.
//
// Held here rather than in core because it is a rule about two fields, and
// again in launchRefusal because that is where the resolved shape arrives.
func debugPairing(filter, file string) string {
	if filter != "" && file == "" {
		return "a debug filter needs a log to write to: on its own it writes no log anywhere that can be read, in the headless mode every agent runs in"
	}
	return ""
}

// launchRefusal is the last door before the argv, for the Config fields that
// are a path rather than a word.
//
// Here rather than at the three call sites so that a **wake**, a fork and an
// import get it too: configRefusal checks the spawn *frame*, and those three
// build a Config from a row or a file on disk that parkbook.go's own header
// says somebody may have edited by hand.
//
// Absolute is the whole check for a directory - a relative one resolves against
// the daemon's own working directory, which is the confusion Frame.Dir exists
// to end. For the debug log it is also the *proof of provenance*: a client
// sends a name, debugFilePath is the only thing that turns one into a path, and
// configFor reserves it for the session. That is why launch hands
// configRefusal no DebugFile - by then it is a resolved, claimed path and the
// frame's fence describes a name.
func (s *server) launchRefusal(cfg core.Config) string {
	if cfg.Dir != "" && !filepath.IsAbs(cfg.Dir) {
		return "a session directory must be absolute, got " + cfg.Dir
	}
	if cfg.DebugFile != "" && !filepath.IsAbs(cfg.DebugFile) {
		return "a debug log path must be absolute, got " + cfg.DebugFile
	}
	if cfg.DebugFile != "" && !s.ownsDebugFile(cfg.SessionID, cfg.DebugFile) {
		if why := s.debugFileRefusal(cfg.DebugFile); why != "" {
			return why
		}
		return "a debug log path must be reserved by this session before launch, got " + cfg.DebugFile
	}
	if err := rpc.ValidAddDirs(cfg.AddDir); err != nil {
		return err.Error()
	}
	// The filter's *shape* as well as its pairing. launch hands configRefusal
	// Effort, MaxBudgetUSD and FallbackModel to be re-checked and cannot hand it
	// this one - a frame carrying a Debug and no DebugFile is the pairing
	// refusal, and by here the file is a resolved path rather than a name - so
	// without this line the filter would be checked once, at the spawn frame,
	// and never at the last door.
	if cfg.Debug != "" && !core.ValidDebugFilter(cfg.Debug) {
		return fmt.Sprintf("unknown debug filter %q", cfg.Debug)
	}
	return debugPairing(cfg.Debug, cfg.DebugFile)
}

// configFor is the configuration half of a spawn's Config, from a frame that has
// already passed configRefusal.
//
// Separate from the identity half deliberately: SessionID, Name and Dir are
// decided by the daemon on the way through spawn - a name comes from the pool, a
// directory may be a worktree it just created - and the rest are carried from
// the client untouched. Keeping the two apart is what makes it visible that
// nothing here is derived.
//
// **DebugFile is the one exception and it is a method for it**: the client sends
// a name and this is where it becomes a path, creates its directory and is
// claimed for a direct caller. spawn has already claimed it before worktree
// creation. A spawn that could not place its log is a refusal rather than a
// session that logs nowhere.
func (s *server) configFor(f rpc.Frame) (core.Config, error) {
	debugFile, err := debugFilePath(s.socket, f.DebugFile)
	if err != nil {
		return core.Config{}, err
	}
	if err := s.claimDebugFile(f.SessionID, debugFile); err != nil {
		return core.Config{}, err
	}
	return core.Config{
		Effort:        f.Effort,
		Model:         f.Model,
		MaxBudgetUSD:  f.MaxBudgetUSD,
		FallbackModel: f.FallbackModel,
		AddDir:        f.AddDir,
		Debug:         f.Debug,
		DebugFile:     debugFile,
	}, nil
}
