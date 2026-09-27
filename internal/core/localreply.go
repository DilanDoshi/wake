// The text replies of local commands Wake parses - part of the airlock; see
// protocol.go.
//
// A local command answers with no model turn (num_turns 0): its reply is Claude
// Code's rendered English in the result text. That English is a wire format as
// surely as a JSON key, so the three Wake reads - /model, /list-agents and
// /rename - are recognised here and nowhere else.
//
// The airlock is these five files and nothing else in Wake knows Claude
// Code's stream-json format:
//
//	protocol.go    decoding - one wire line in, core.Events out
//	wire.go        the shapes it decodes into
//	vocabulary.go  Claude's words resolved into Wake's
//	encode.go      the frames Wake writes back
//	localreply.go  the text replies of local commands Wake parses
//
// internal/core/airlock_test.go enforces that over the whole tree and reads
// the same list. protocol.go's header carries the full rule.

package core

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A bare /model reply names the session's model and reasoning level:
//
//	Current model: Opus 5 (1M context) (effort: xhigh)
//
// It is the one place a session's effort is reported back at all
// (testdata/stream/bare-model.jsonl) - nothing Wake receives unasked carries
// it - so the daemon sends the bare command and reads the level out of this
// line to confirm the effort it asked for. Recognised here because it is
// Claude's rendered output shape, which is exactly what the airlock quarantines.
const modelReplyPrefix = "Current model:"

// effortClause matches the parenthesised level in a /model reply.
var effortClause = regexp.MustCompile(`\(effort:\s*([a-zA-Z]+)\)`)

// IsModelReply reports whether text is a bare /model reply. Used to suppress the
// probe's own frames and to filter its line out of a restored transcript.
func IsModelReply(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), modelReplyPrefix)
}

// ModelFromModelReply reads the model's display name out of a /model reply, or
// reports false when the text is not one.
//
// It is the model name Claude Code itself renders ("Opus 5 (1M context)"),
// which is why the status bar prefers it over the init frame's raw id: after a
// runtime /model the id on the wire is a turn stale, and this is read back at
// once by re-probing. The name may carry its own parentheses, so the effort
// clause is removed by pattern rather than by cutting at the first "(".
func ModelFromModelReply(text string) (string, bool) {
	if !IsModelReply(text) {
		return "", false
	}
	line := strings.TrimSpace(text)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, modelReplyPrefix))
	line = strings.TrimSpace(effortClause.ReplaceAllString(line, ""))
	if line == "" {
		return "", false
	}
	return line, true
}

// EffortFromModelReply reads the reasoning level out of a /model reply, or
// reports false when there is no clause or the level is not one /effort takes.
func EffortFromModelReply(text string) (string, bool) {
	m := effortClause.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	level := strings.ToLower(m[1])
	if !ValidEffortCommand(level) {
		return "", false
	}
	return level, true
}

// A bare /list-agents reply names this session, then every other Claude
// session on the machine (testdata/stream/list-agents.jsonl):
//
//	This session: <name> [<short-id>] (the name other sessions use to message it)
//
//	Other Claude sessions (<n>):
//	  [<state>]  ·  <name>  ·  <cwd>  ·  started <age>
//
// or, with nobody else, one line opening with listAgentsNone. It is human text
// rather than a schema, so exactly these shapes parse and anything else is
// refused whole: a wrong row is worse than none.
const (
	listAgentsSelf   = "This session: "
	listAgentsOthers = "Other Claude sessions ("
	listAgentsNone   = "No subagents, teammates or other Claude sessions"
	listAgentsColumn = "  ·  "
)

// listAgentsSelfName is the name before the short id on the self line.
var listAgentsSelfName = regexp.MustCompile(`^(.+?) \[[0-9a-f]+\]`)

// IsListAgentsReply reports whether text opens like a /list-agents reply, so a
// body PeersFromListAgents refuses is still suppressed as the probe's own.
func IsListAgentsReply(text string) bool {
	_, ok := selfFromListAgents(firstLine(text))
	return ok
}

// PeersFromListAgents reads this session's own name and the other sessions out
// of a /list-agents reply, in the order it lists them. ok is false, with
// nothing else, for any line it does not recognise.
func PeersFromListAgents(text string) (self string, peers []Peer, ok bool) {
	var lines []string
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) < 2 {
		return "", nil, false
	}
	if self, ok = selfFromListAgents(lines[0]); !ok {
		return "", nil, false
	}
	if len(lines) == 2 && strings.HasPrefix(lines[1], listAgentsNone) {
		return self, nil, true
	}
	if peers, ok = peersFromListAgents(lines[1], lines[2:]); !ok {
		return "", nil, false
	}
	return self, peers, true
}

func selfFromListAgents(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, listAgentsSelf)
	m := listAgentsSelfName.FindStringSubmatch(rest)
	if !ok || m == nil {
		return "", false
	}
	return m[1], true
}

// peersFromListAgents reads the rows under the count header; a count that
// disagrees with the rows is a shape this parse was not shown.
func peersFromListAgents(header string, rows []string) ([]Peer, bool) {
	count, opened := strings.CutPrefix(header, listAgentsOthers)
	count, closed := strings.CutSuffix(count, "):")
	n, err := strconv.Atoi(count)
	if !opened || !closed || err != nil || n != len(rows) {
		return nil, false
	}
	var peers []Peer
	for _, row := range rows {
		p, ok := peerFromRow(row)
		if !ok {
			return nil, false
		}
		peers = append(peers, p)
	}
	return peers, true
}

// peerFromRow reads one `[state]  ·  name  ·  cwd  ·  started age` row. The age
// is required and unread.
func peerFromRow(row string) (Peer, bool) {
	cols := strings.Split(row, listAgentsColumn)
	if len(cols) != 4 {
		return Peer{}, false
	}
	for _, c := range cols {
		if c == "" || c != strings.TrimSpace(c) {
			return Peer{}, false
		}
	}
	state, opened := strings.CutPrefix(cols[0], "[")
	state, closed := strings.CutSuffix(state, "]")
	if !opened || !closed || state == "" || !filepath.IsAbs(cols[2]) {
		return Peer{}, false
	}
	return Peer{Name: cols[1], Dir: cols[2], State: state}, true
}

// A bare /rename replies with the name it took (list-agents.jsonl), which the
// next /list-agents names the session by.
const renamedPrefix = "Session renamed to: "

// RenamedFromReply reads the new name out of a /rename reply, or reports false
// when the text is not one.
func RenamedFromReply(text string) (string, bool) {
	name, ok := strings.CutPrefix(firstLine(text), renamedPrefix)
	if name = strings.TrimSpace(name); !ok || name == "" {
		return "", false
	}
	return name, true
}

// firstLine is text's first line, trimmed.
func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}
