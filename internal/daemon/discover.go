// What sessions exist on this machine that Wake did not start, and where each
// one may be run.
//
// # This file is a second airlock leak, and it is confined here on purpose
//
// CLAUDE.md's non-negotiable is that internal/core's four files are the only
// non-test files that know Claude's JSON. That rule is about Claude's *stream*
// - the thing that makes a Codex port four files - and this reads a different
// Claude artefact: the transcript on disk. A Codex port rewrites discovery
// outright, because "where does the tool persist a conversation" has no
// model-agnostic answer at all.
//
// So the leak is real and the containment is the same one argv.go made for the
// CLI flags: the transcript's keys are spelled **in this file and nowhere else
// in the tree**, held by TestTheTranscriptKeysAreSpelledOnlyInDiscover, and
// anything outside asks FoundSession. docs/notes/deferred.md carries the ruling
// this wants next - whether the airlock's file set should grow a fifth member.
//
// # Why a directory is verified rather than decoded
//
// claude locates a transcript by the working directory the process was started
// in, so importing has to run there and nowhere else (2026-08-10 findings §12).
// The directory is not recoverable from the slug: 2026-08-12 findings §2
// measured `/`, `.`, ` ` and `_` all mapping to `-`, over 83 slug directories
// none of which contains any character but [A-Za-z0-9-]. The function is
// many-to-one and has no inverse.
//
// And the transcript's own cwd is not the answer either: it is a property of a
// *message*, so a --worktree session's frames name the worktree while the file
// sits under the directory it started in. 58 of 428 transcripts disagree with
// their own first cwd (§3).
//
// The rule that survives both is a comparison of the two against each other,
// and it is in verifiedDir.

package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The transcript's own key names. Wake reads three of them and no more, and
// this const block is the whole of what this package knows about that format.
//
// keyCwd is a directory a message was written from. keyLastPrompt and
// keyCustomTitle are the two preview sources, measured on 2026-08-12 at 379 and
// 390 of 428 transcripts, 424 with either - so a session with neither is a real
// case and is listed with no preview rather than skipped.
const (
	keyCwd         = "cwd"
	keyLastPrompt  = "lastPrompt"
	keyCustomTitle = "customTitle"
)

// previewBytes bounds one preview line. A prompt is unbounded text somebody
// typed, and this lands in a terminal listing beside a column of ids.
const previewBytes = 72

// transcriptScanBytes bounds one line of a transcript. A transcript carries
// whole file contents inside attachment frames, so a line can be megabytes; the
// three keys this file wants are all short and all top-level, and a line too
// long to scan is one this file has nothing to learn from.
const transcriptScanBytes = 1 << 20

// FoundSession is one transcript on disk, and what can and cannot be proven
// about it.
//
// **Nothing here says the session is closed.** A transcript is not evidence
// about a process: 2026-08-12 findings §5 counted four live `claude` processes
// whose whole argv is the word `claude`, so no id appears in it and
// core.SessionArgvMarkers cannot see them. Modified is recency and recency is a
// hint. Whether this may be imported is importRefusal's question, not this
// type's.
type FoundSession struct {
	// ID is the transcript's filename, which is where a session id comes from:
	// 428 of 428 files on the recording machine are named <uuid>.jsonl.
	ID string

	// Dir is the directory this session may be run in, or empty when none
	// could be proven. Empty is a first-class answer and 97 of 428 transcripts
	// had it - see verifiedDir.
	Dir string

	// Slug is the project directory the transcript was found under. Carried for
	// the listing, because it is the only thing there is to show when Dir is
	// empty, and it is what an operator recognises.
	Slug string

	// Path is the transcript itself.
	Path string

	// Modified is the file's mtime. It orders the listing and says nothing else
	// - in particular it does not say the session ended then, only that nothing
	// has been written since.
	Modified time.Time

	// Preview is one bounded line of what this session was last asked to do, or
	// empty. Contained by oneLine: it is text somebody typed and it is drawn on
	// a row beside other sessions' rows.
	Preview string
}

// slugOf is how a directory becomes the name of the directory its transcripts
// live under - or rather, how Wake *checks* that it did.
//
// **This is only ever used for comparison, and that is what makes it safe to
// ship without knowing claude's real function.** 2026-08-12 findings §2: the
// corpus exercises `/`, `.`, ` ` and `_`, all of which map to `-`, and contains
// no directory carrying any other special character - so "replace [/._ ]" and
// "replace everything outside [A-Za-z0-9-]" are indistinguishable on 401 real
// pairs. This takes the broader of the two.
//
// If claude's function is the narrower one, a directory containing some other
// character slugs differently here, the comparison in verifiedDir fails, and
// the session is listed with no directory. That is a **false negative**, which
// costs an import somebody can still do by hand. The inverse - constructing a
// path from a slug and being wrong - is the failure that resumes a session in
// the wrong place, and no value of this function can produce it, because
// nothing calls it that way round.
func slugOf(dir string) string {
	var b strings.Builder
	b.Grow(len(dir))
	for _, r := range dir {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ProjectsDir is where claude persists transcripts, and it is the one path in
// this file that is not derived from something Wake was told.
//
// WAKE_PROJECTS overrides it, for the suite rather than for an operator: every
// test here needs a projects tree it owns, and the alternative is a global the
// daemon reads. It is the same shape WAKE_SOCKET already has.
func ProjectsDir() string {
	if p := os.Getenv("WAKE_PROJECTS"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// Discoverable is every session on this machine, for the one client that has to
// draw a picker.
//
// Exported for `wake import`, which needs the same list the daemon decides
// against - a listing assembled some other way is the parallel implementation
// this project's rules forbid, and the two would disagree about the one field
// that matters. **It dials nothing and starts nothing**: this is the verb
// somebody runs before they have a fleet.
//
// It grants no permission. What comes back says a transcript exists, and the
// daemon still decides whether it may be imported - see importSource.
func Discoverable() ([]FoundSession, error) {
	return discover(ProjectsDir())
}

// discover reports every session transcript under a projects tree, newest
// first.
//
// A missing tree is **not** an error: it is a machine that has never run
// claude, and the true answer to "what could be imported" is none. Failing
// there would make the verb fail on exactly the machine whose empty answer is
// correct.
//
// A directory that cannot be read is skipped rather than fatal, for the same
// reason one bad park book entry does not lose the other nineteen: one
// unreadable project must not cost the operator the other eighty-two.
func discover(projects string) ([]FoundSession, error) {
	if projects == "" {
		return nil, errors.New("no projects directory to look in: claude persists transcripts under ~/.claude/projects and this process cannot tell where home is")
	}
	entries, err := os.ReadDir(projects)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []FoundSession
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, sessionsUnder(projects, e.Name())...)
	}
	// Newest first, and by id where two share a timestamp so the order is
	// stable rather than whatever the filesystem said.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Modified.Equal(out[j].Modified) {
			return out[i].Modified.After(out[j].Modified)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// sessionsUnder reads one project directory.
func sessionsUnder(projects, slug string) []FoundSession {
	dir := filepath.Join(projects, slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		logf("wake: session %s could not be read while looking for importable sessions: %v", dir, err)
		return nil
	}
	var out []FoundSession
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id, ok := sessionIDOf(e.Name())
		if !ok {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, ok := regularTranscript(path)
		if !ok {
			continue
		}
		cwds, preview := readTranscript(path)
		out = append(out, FoundSession{
			ID:       id,
			Dir:      verifiedDir(slug, cwds),
			Slug:     slug,
			Path:     path,
			Modified: info.ModTime(),
			Preview:  preview,
		})
	}
	return out
}

// sessionIDOf takes a session id out of a transcript's filename, which is the
// only place one comes from here.
//
// mintedByWake is the same predicate maySpawn and the reaper apply, and it is
// the right one for a stranger's transcript too: the id has to be a UUID
// because the reaper proves a process group by finding that id in an argv, and
// an id that is not one could match somebody's shell job. A file called
// `notes.jsonl` is not a session.
func sessionIDOf(name string) (string, bool) {
	id, ok := strings.CutSuffix(name, ".jsonl")
	if !ok || !mintedByWake(id) {
		return "", false
	}
	return id, true
}

// verifiedDir is the whole of what discovery may claim about where a session
// ran, and it is a comparison rather than a decode.
//
// Three facts held against each other, because no two of them are enough:
//
//   - the transcript **names** the directory, so nothing is invented;
//   - its slug **is** the directory the transcript was found under, so a cwd
//     that belongs to a subagent or a worktree cannot answer;
//   - it is **absolute**, which is maySpawn's rule arriving at the one door
//     that bypasses maySpawn;
//   - it **still exists and is a directory**, so a session whose project was
//     deleted - or replaced by a file of that name - is listed and refused
//     rather than resumed into a path that is not there.
//
// **Exactly one, or none.** The slug is many-to-one (§2), so `/a/a b` and
// `/a/a.b` share one - and if a transcript names both there is no evidence
// which it ran in. That arm did not fire once on 428 real transcripts, which is
// the reason to keep it rather than to drop it: it costs one comparison and it
// is what stands between a lossy function and a confident wrong answer.
func verifiedDir(slug string, cwds []string) string {
	var found string
	for _, c := range cwds {
		// maySpawn's rule, applied to the one directory that does not arrive
		// through maySpawn. It refuses a relative Dir off the wire because "a
		// relative directory would resolve against the daemon's own working
		// directory, which is the confusion this field exists to end" - and a
		// Dir supplied out of band here would be Stat'd and then run relative
		// to the daemon, which is the $PWD failure the whole file prevents.
		if !filepath.IsAbs(c) {
			continue
		}
		if slugOf(c) != slug {
			continue
		}
		if fi, err := os.Stat(c); err != nil || !fi.IsDir() {
			continue
		}
		if found != "" && found != c {
			return ""
		}
		found = c
	}
	return found
}

// readTranscript pulls the three keys this package knows out of one file.
//
// One pass, line by line, decoding each line into a map rather than a struct:
// a struct would put a `json` tag carrying one of claude's key names into this
// tree, and the airlock's detector reads tags and literals alike. A map keyed
// on the consts above keeps the whole vocabulary in one const block where a
// guard can see it.
//
// A line that will not parse is skipped rather than fatal. A transcript is
// appended to by a live process, so the last line of a file being read may be a
// partial write - 2026-08-12 findings §7 records that no torn line was observed
// in 428 files and that this is therefore not designed around, only survived.
func readTranscript(path string) (cwds []string, preview string) {
	f, err := os.Open(path)
	if err != nil {
		logf("wake: transcript %s could not be opened: %v", path, err)
		return nil, ""
	}
	defer func() { _ = f.Close() }()

	seen := map[string]bool{}
	var title string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), transcriptScanBytes)
	for sc.Scan() {
		var line map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			continue
		}
		if c, ok := decodeString(line, keyCwd); ok && !seen[c] {
			seen[c] = true
			cwds = append(cwds, c)
		}
		// Last one wins for both: a transcript carries one `last-prompt` frame
		// per turn, and the newest is what says what this session is doing.
		if p, ok := decodeString(line, keyLastPrompt); ok {
			preview = p
		}
		if t, ok := decodeString(line, keyCustomTitle); ok {
			title = t
		}
	}
	if preview == "" {
		// 379 of 428 carry a prompt and 390 carry a title; 424 carry either. A
		// title is a name somebody chose, which is a worse "what is this doing"
		// and a better nothing.
		preview = title
	}
	return cwds, oneLine(preview, previewBytes)
}

// decodeString reads one top-level string key, treating any other shape as
// absent. A transcript is a file on disk that outlives whichever build wrote
// it, so a key that is one day an object must not become an error here.
func decodeString(line map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := line[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

// OneLine flattens a string to a single line, and it is exported because the
// containment is a property of the **line** rather than a habit at each call
// site.
//
// That distinction is CLAUDE.md's, in its own words about the manager's
// surface, and this file shipped the other way round once: Preview was
// contained at construction while Dir - which comes from a transcript's cwd,
// text Wake did not write - was rendered raw beside it on the same row of the
// same listing. **One session in, two rows out**, with the forged one
// indistinguishable from a real one. Reachable without anybody being clever:
// anything that can create a directory can create one whose name contains a
// newline, and one `claude` run there populates the matching project slug, so
// verifiedDir's comparison self-satisfies.
//
// So `cmd/wake`'s picker runs the assembled **row** through this, and a
// seventh field on FoundSession inherits it.
//
// It maps anything that can act as structure to a space - the C0 and C1
// ranges, DEL, and U+2028/U+2029 - and **substitutes rather than deletes**,
// because columns are padded before containment runs and a deletion shifts
// every column to the right of the character somebody chose.
//
// **It is not internal/mcp's oneLine**, and the difference is worth stating
// rather than leaving for somebody to call a parallel implementation: that one
// is unexported in a package this one does not import, it contains an agent's
// model output for a *model's* context, and its bound is derived from a
// 30-agent roll-up. This contains an operator's own earlier prompt, and a
// directory name off the filesystem, for a *terminal*. docs/notes/deferred.md
// carries the unification.
func OneLine(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		// C0, DEL, C1, and the two Unicode separators a terminal or a later
		// reader can treat as a line break.
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f, r == '\u2028', r == '\u2029':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// oneLine is OneLine with a bound, for the one field this package renders into
// a value of its own. The bound is separate because a row's bound is the
// terminal's business and a preview's is this file's.
func oneLine(s string, max int) string {
	if s == "" {
		return ""
	}
	out := strings.TrimSpace(OneLine(s))
	if len([]rune(out)) <= max {
		return out
	}
	return string([]rune(out)[:max-1]) + "\u2026"
}
