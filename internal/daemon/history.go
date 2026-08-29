package daemon

// The conversation a session already had, read back off claude's own disk.
//
// # Why the daemon reads it and not the TUI
//
// The file is a Claude format, and internal/ui may not know one - it sees Wake's
// own Event and nothing else. discover.go is already the airlock for this
// particular format and holds the three keys it reads to one file; this is the
// fourth reader of the same directory and it goes through core.
//
// # Why the path is found rather than built
//
// The project-dir slug is lossy - `/`, `.`, ` ` and `_` all map to `-` - so a
// path built from a directory is a guess, and slugOf may only ever appear as an
// operand of == or != for exactly that reason. A transcript is found by looking
// for its id as a *filename*, which is what sessionIDOf already reads.

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// historyEvents bounds how many events one conversation hands back.
//
// The tail rather than the head: somebody reopening a conversation wants where
// it got to, and a transcript can be tens of thousands of lines. It is a client
// bound as much as a wire one - internal/ui renders every event it is given
// through glamour, and a re-wrap is proportional to what it holds.
const historyEvents = 400

// historyBytes bounds the same reply in bytes, and it is the bound that
// actually protects the client.
//
// 400 events is not a size: one line can be a megabyte, so 400 of them is 400.
// A review measured 400 realistic file-reading turns encoding to a **39MB**
// frame - past rpc's 16MB cap, which means the client's scanner refuses it,
// ReadFrames ends, and the socket ends with it. That is not a pane missing its
// history: it is every session's events stopping and the window reattaching.
//
// Four megabytes is a quarter of that cap, which leaves room for the frame's
// own envelope and for the events still arriving beside it.
const historyBytes = 4 << 20

// History is the conversation on disk for one session, oldest last-400 first,
// as its live branch - a rewind's dead turns are on disk and dropped here.
//
// An id with no transcript is not an error: a session that has never taken a
// turn has no file yet, and neither does one somebody started outside a
// directory claude tracks. The caller draws nothing, which is what it would
// have drawn anyway.
//
// Two passes over the file. The first reads only the tree - one small node per
// line, no content - and reconstructs which uuids are live; the second emits
// the events of the live lines, tail-bounded. Two passes rather than holding
// the file so that memory stays proportional to the node index plus the tail
// ring, never the whole file's content.
func History(id string) ([]core.Event, error) {
	path, ok := transcriptPath(id)
	if !ok {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	active, err := activeBranchOf(f)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return liveHistory(f, id, active)
}

// activeBranchOf reads a transcript's tree - identity and rewind markers, no
// content - and returns the uuids on its live branch. Memory is one small node
// per line, never the line itself, which is the whole reason History reads the
// file twice rather than once into events. See core.ActiveBranch.
func activeBranchOf(r io.Reader) (map[string]bool, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var nodes []core.TranscriptNode
	for {
		line, err := readTranscriptLine(br)
		if len(line) > 0 {
			if n, ok := core.DecodeTranscriptNode(line); ok {
				nodes = append(nodes, n)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return core.ActiveBranch(nodes), nil
			}
			return nil, err
		}
	}
}

// liveHistory emits the events of the live branch's lines, tail-bounded. A line
// whose node uuid is not in active is a rewound dead-branch turn: it is skipped
// whole and its content is never decoded. A line with no tree node - the
// on-disk chatter, and the rewind markers themselves - is not a branch node and
// passes through as before.
func liveHistory(r io.Reader, id string, active map[string]bool) ([]core.Event, error) {
	// A ring rather than everything-then-slice. A transcript carries whole file
	// contents inside attachment records, so "read it all and keep the last
	// 400" is memory proportional to the file - and the tail's backing array
	// would hold the rest of it alive afterwards.
	ring := make([]core.Event, 0, historyEvents)
	bytes := 0
	keep := func(ev core.Event) {
		// Raw is the undecoded line, and nothing above internal/core may read
		// it. Dropped here because it is the whole of what makes an event
		// large, and this is the one path that holds hundreds at once.
		ev.Raw = nil
		// Stamped because the on-disk key is `sessionId` where the stream's is
		// `session_id`, and this is the caller that knows which file it opened.
		ev.SessionID = id
		bytes += len(ev.Text)
		ring = append(ring, ev)
		// Trimmed from the front, which is what makes both bounds keep the
		// *tail*: somebody reopening a conversation wants where it got to.
		for len(ring) > historyEvents || (bytes > historyBytes && len(ring) > 1) {
			bytes -= len(ring[0].Text)
			ring = ring[1:]
		}
	}

	// The effort probe leaves a /model command and its "Current model:" reply on
	// disk; Wake suppresses them live and drops them here on the way back, so a
	// reopened conversation never shows the question Wake asked on its own. The
	// reply is the line after the command, so one line of lookahead closes it.
	// Safe because an operator's bare /model is intercepted by internal/ui and
	// never sent, so any /model on disk is Wake's probe.
	dropReply := false
	keepFiltered := func(ev core.Event) {
		if dropReply {
			dropReply = false
			if ev.Kind == core.KindAssistantText && core.IsModelReply(ev.Text) {
				return
			}
		}
		if ev.Kind == core.KindUserText && strings.TrimSpace(ev.Text) == slashPrefix+modelVerb {
			dropReply = true
			return
		}
		keep(ev)
	}

	// bufio.Reader rather than Scanner: a Scanner *stops* on a line longer than
	// its buffer, so one oversized attachment would mean no history at all for
	// that conversation. This reads the long line in pieces and drops it.
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := readTranscriptLine(br)
		if len(line) > 0 {
			// Skip only a line that carries a uuid the reconstruction ruled
			// dead; everything else - live nodes, chatter, markers - is kept.
			if node, ok := core.DecodeTranscriptNode(line); !ok || node.UUID == "" || active[node.UUID] {
				events, decErr := core.DecodeTranscriptLine(line)
				if decErr != nil {
					// One unreadable line is not an unreadable conversation.
					logf("wake: session %s has a transcript line that could not be decoded: %v", id, decErr)
				}
				for _, ev := range events {
					keepFiltered(ev)
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ring, nil
			}
			return nil, err
		}
	}
}

// readTranscriptLine returns one line, or nothing at all for a line longer than
// transcriptScanBytes - which it still consumes, so the next call starts at the
// next record rather than in the middle of this one.
func readTranscriptLine(br *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		chunk, more, err := br.ReadLine()
		if len(out) <= transcriptScanBytes {
			out = append(out, chunk...)
		}
		if !more || err != nil {
			if len(out) > transcriptScanBytes {
				return nil, err
			}
			return out, err
		}
	}
}

// regularTranscript is shared by both transcript readers. Lstat refuses a
// final-component symlink, which would let a transcript be read from anywhere
// on the machine, and a non-regular file such as a FIFO can block open
// forever. Neither is a thing claude writes.
func regularTranscript(path string) (os.FileInfo, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	return info, true
}

// transcriptPath is where claude keeps this session's transcript, if it keeps
// one. Found by filename across every project directory, never built from a
// slug - see the header.
func transcriptPath(id string) (string, bool) {
	if !mintedByWake(id) {
		return "", false
	}
	projects := ProjectsDir()
	entries, err := os.ReadDir(projects)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(projects, e.Name(), id+".jsonl")
		if _, ok := regularTranscript(path); ok {
			return path, true
		}
	}
	return "", false
}

// sendHistory answers one client's FrameHistory.
//
// Read on the connection's own goroutine rather than handed to the agent: there
// may be no agent - the whole point is a conversation whose process is gone -
// and the file is claude's rather than Wake's, so nothing here touches a
// session's state.
//
// A read failure is reported to the client that asked rather than logged and
// dropped. It is the difference between "this conversation had nothing before
// now" and "Wake could not tell you what it had", and the pane cannot tell
// those apart from an empty reply.
func (s *server) sendHistory(c *client, id string) {
	s.answerHistory(c, id, rpc.FrameHistoryReply)
}

// sendRoomHistory is the same read answered under the room's kind.
//
// A verb of its own rather than a parameter at the call site, because the
// manager guard scans internal/daemon's dispatch for every rpc constant it
// names: a reply kind spelled there would read as a verb the daemon serves and
// would need a verdict for something no client can send. See
// cmd/wake/mcpguard_test.go.
func (s *server) sendRoomHistory(c *client, id string) {
	s.answerHistory(c, id, rpc.FrameRoomHistoryReply)
}

// answerHistory is the read both asks share. reply is the kind it goes back
// under; one reader of the format, two ledgers on the client.
func (s *server) answerHistory(c *client, id, reply string) {
	events, err := History(s.transcriptID(id))
	if err != nil {
		c.enqueue(errorFrame(id, "could not read that conversation's transcript: "+err.Error()))
		return
	}
	// Addressed by the id the *client* knows, whatever file it came out of.
	for i := range events {
		events[i].SessionID = id
	}
	c.enqueue(rpc.Frame{Kind: reply, SessionID: id, Events: events})
}

// transcriptID is the id claude is writing this conversation under, which is
// the agent's own until a /clear mints a new one and leaves the old transcript
// behind. Reading the old file would show a cleared agent the conversation it
// no longer has.
func (s *server) transcriptID(id string) string {
	s.mu.Lock()
	a, ok := s.agents[id]
	s.mu.Unlock()
	if !ok {
		return id
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.claudeID != "" {
		return a.claudeID
	}
	return id
}
