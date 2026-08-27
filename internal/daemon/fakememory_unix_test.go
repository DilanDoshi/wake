//go:build unix

package daemon

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fake `claude` that remembers, because until this existed nothing in the
// tree could tell a resumed conversation from a fresh one.
//
// Every other fake here is stateless: it echoes, or stalls, or asks, and it
// begins each process knowing nothing. That is enough to prove a woken session
// is the same *session* and is alive - and it is exactly why "does a wake keep
// the conversation" could not be asked. There was nothing for a resume to be
// wrong about.
//
// This one keeps a transcript on disk keyed by session id, and reads it back
// when it is started with `--resume`. It is deliberately the smallest thing
// that makes the question answerable rather than a model of claude's real
// storage: one file, one line per turn, no JSON.

const (
	// memoryDirEnv is where the transcripts go. Per-test, so two tests cannot
	// see each other's memory - which would make a passing recall meaningless.
	memoryDirEnv = "WAKE_FAKE_MEMORY_DIR"

	// memoryScript selects this fake in runFakeClaude's switch.
	memoryScript = "memory"

	// passphrase is what a session is told before it is parked and asked for
	// after it is woken. Nothing derives it; it is only ever *carried*.
	passphrase = "cobalt-mango-7731"

	// recallWord is the question, and it deliberately does not contain the
	// answer. A fake that echoed its input would pass a recall test whose
	// question carried the passphrase, and prove nothing at all.
	recallWord = "recall"

	// recalledPrefix is how an answer from memory announces itself, so a test
	// can await the reply rather than the passphrase - and then assert on the
	// passphrase separately. Awaiting the passphrase directly would make the
	// await and the assertion the same act, and a timeout would report as
	// "no frame arrived" rather than as "it did not remember".
	recalledPrefix = "recalled:"

	// notedPrefix acknowledges a turn was stored. A test waits for it before
	// parking, so the park cannot race the write it depends on.
	notedPrefix = "noted:"
)

// rememberingClaudeOnPath puts the remembering fake on PATH, with a transcript
// directory of its own.
func rememberingClaudeOnPath(t *testing.T) {
	t.Helper()
	fakeClaudeOnPath(t, memoryScript)
	t.Setenv(memoryDirEnv, t.TempDir())
}

// memoryPath is the transcript for one session.
//
// It keys on the session id rather than on anything the process was told,
// because that is the claim under test: a woken process is handed `--resume
// <id>` and *nothing else* connecting it to what came before. If this keyed on
// an environment variable the daemon set per-spawn, a resume would find its
// history for a reason the real thing does not have.
func memoryPath(sid string) string {
	dir := os.Getenv(memoryDirEnv)
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "memory-"+sid+".txt")
}

// fakeMemory is the agent. It appends every turn it is given and, when it was
// started with `--resume`, begins from what the previous process wrote.
//
// `resumed` is passed rather than re-derived so that the one thing this fake
// exists to vary is visible at the call site in runFakeClaude.
func fakeMemory(sid string, resumed bool) int {
	path := memoryPath(sid)

	var history []string
	if resumed {
		history = readMemory(path)
	}

	emitText(sid, "ready")
	emitResult(sid)

	for line := range stdinLines() {
		switch {
		case strings.Contains(line, recallWord):
			// Answer from what is held, not from what was just asked. The
			// question does not carry the passphrase, so an empty history
			// cannot produce it.
			emitText(sid, recalledPrefix+" "+strings.Join(history, " | "))
		default:
			history = append(history, line)
			appendMemory(path, line)
			emitText(sid, notedPrefix+" "+fmt.Sprint(len(history)))
		}
		emitResult(sid)
	}
	return 0
}

// readMemory is every turn a previous process wrote for this session, or
// nothing if there was none.
//
// A missing file is not an error: the first process of a session has no
// history, and a resume of a session that never spoke is a legitimate empty
// one. Anything else that goes wrong is reported on stderr rather than
// swallowed, because a fake that silently forgets would make the test it
// serves pass for the wrong reason.
func readMemory(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "fake claude: cannot read its own memory at %s: %v\n", path, err)
		}
		return nil
	}
	defer func() { _ = f.Close() }()

	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fake claude: its memory at %s stopped short: %v\n", path, err)
	}
	return out
}

// appendMemory adds one turn, opening and closing per turn so the bytes are on
// disk before the turn is acknowledged. A test parks on that acknowledgement,
// so a buffered writer here would let the park race the write and produce a
// flake whose cause is this file rather than the code under test.
func appendMemory(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake claude: cannot write its own memory at %s: %v\n", path, err)
		return
	}
	if _, err := fmt.Fprintln(f, line); err != nil {
		fmt.Fprintf(os.Stderr, "fake claude: cannot append to %s: %v\n", path, err)
	}
	if err := f.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "fake claude: cannot close %s: %v\n", path, err)
	}
}
