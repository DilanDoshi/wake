package daemon

// Where a session's debug log goes.
//
// **The client names it; the daemon places it.** rpc.Frame.DebugFile carries
// one segment and this turns it into a path beside the socket, which is where
// every other per-fleet file already lives - parked.json, sessions.json,
// mcp.json. That split is manager.go's ruling arriving one field over: a path
// on the wire would let anything that can dial this socket choose where a
// `claude` process creates and truncates a file, and unlike an agent's own
// write there is no transcript, no room line and no permission ask to see it
// happen.
//
// **Wake never removes one**, worktree.go's terms: a log is what somebody
// turned logging on to read, so deleting it on park or on `wake stop` would
// take away the artifact at the moment the episode ends.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// debugDirName keeps the logs together and out of the fleet directory's
	// own listing, beside the socket rather than in the session's repository:
	// writing a log into somebody's checkout is not something a spawn is
	// entitled to do. Same argument as mcpConfigName.
	debugDirName = "debug"

	// debugFileExt is appended by Wake, so the name a client chose cannot pick
	// an extension either.
	debugFileExt = ".log"

	// debugDirPerm keeps the directory to its owner: a debug log carries this
	// session's prompts and tool arguments.
	debugDirPerm = 0o700
)

// debugFilePath is the file a named debug log is written to, or "" for a
// session that logs nothing.
//
// The directory is created here rather than left to claude: nothing recorded
// says whether `--debug-file` makes its parents, and a session that silently
// logged nowhere is the failure this flag exists to end.
//
// The name is fenced here **as well as** at configRefusal, and that is not
// belt-and-braces: this is the only statement in the tree that joins a word a
// client chose onto a filesystem path, so it is where a name that escapes the
// directory has to be refused. A guard three call frames away in another file
// is one a second caller does not inherit.
func debugFilePath(socket, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if err := rpc.ValidDebugFileName(name); err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(socket), debugDirName)
	if err := os.MkdirAll(dir, debugDirPerm); err != nil {
		return "", fmt.Errorf("make the directory for this session's debug log: %w", err)
	}
	return debugFileLocation(socket, name), nil
}

func debugFileLocation(socket, name string) string {
	return filepath.Join(filepath.Dir(socket), debugDirName, name+debugFileExt)
}

func debugFileHeldRefusal(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), debugFileExt)
	return fmt.Sprintf("--debug-file %q is already reserved for another session; park or end that session before reusing the name, or choose another", name)
}

// The default macOS volume aliases ASCII case. Only the claim key folds; the
// argv path keeps the operator's spelling.
func debugFileKey(path string) string {
	return strings.ToLower(path)
}

// debugFileRefusal is the early, read-only half of the claim. The spawn path
// repeats it atomically because two clients can pass this check together.
func (s *server) debugFileRefusal(path string) string {
	if path == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := debugFileKey(path)
	for _, held := range s.debugFiles {
		if held == key {
			return debugFileHeldRefusal(path)
		}
	}
	return ""
}

// A session claims once, and a second claim under a live id is refused rather
// than treated as the same spawn arriving twice. Two spawn frames can carry one
// session id - admit is what refuses the second, and it runs much later - so a
// duplicate that were allowed to share this entry would delete it on its own
// refusal, freeing the name under the session that is writing to the log.
func (s *server) claimDebugFile(sessionID, path string) error {
	if path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := debugFileKey(path)
	if held, ok := s.debugFiles[sessionID]; ok {
		return fmt.Errorf("session %s has already reserved --debug-file %q", sessionID,
			strings.TrimSuffix(filepath.Base(held), debugFileExt))
	}
	for _, held := range s.debugFiles {
		if held == key {
			return errors.New(debugFileHeldRefusal(path))
		}
	}
	s.debugFiles[sessionID] = key
	return nil
}

func (s *server) ownsDebugFile(sessionID, path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.debugFiles[sessionID] == debugFileKey(path)
}

func (s *server) releaseDebugFile(sessionID string) {
	s.mu.Lock()
	delete(s.debugFiles, sessionID)
	s.mu.Unlock()
}
