// The text replies of local commands Wake parses - part of the airlock; see
// protocol.go.
//
// A local command answers with no model turn (num_turns 0): its reply is Claude
// Code's rendered English in the result text. That English is a wire format as
// surely as a JSON key, so the three Wake reads - /model, /list-agents and
// /rename - are recognised here and nowhere else.
//
// The airlock is these six files and nothing else in Wake knows Claude
// Code's stream-json format:
//
//	protocol.go    decoding - one wire line in, core.Events out
//	wire.go        the shapes it decodes into
//	vocabulary.go  Claude's words resolved into Wake's
//	encode.go      the frames Wake writes back
//	localreply.go  the text replies of local commands Wake parses
//	control.go     control requests Wake writes, and their receipts
//
// internal/core/airlock_test.go enforces that over the whole tree and reads
// the same list. protocol.go's header carries the full rule.

package core

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
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

// modelNameQuote wraps the name in a live /model reply, the clause or no
// clause after it (bare-model-effort.jsonl, bare-model-no-effort.jsonl).
const modelNameQuote = "`"

// ModelFromModelReply reads the model's display name out of a /model reply, or
// reports false when the text is not one.
//
// It is the model name Claude Code itself renders ("Opus 5 (1M context)"),
// which is why the status bar prefers it over the init frame's raw id: after a
// runtime /model the id on the wire is a turn stale, and this is read back at
// once by re-probing. The name may carry its own parentheses, so the effort
// clause is removed by pattern rather than by cutting at the first "(".
//
// Beside the prefix, a reply is known by one name in backticks, or in the older
// shape without them (bare-model.jsonl) by a valid effort clause. That is shape
// alone; that the line is Claude's own reply is the caller's to know - the
// daemon reads only a local command's (confirmModelLocked).
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
	name, opened := strings.CutPrefix(line, modelNameQuote)
	name, closed := strings.CutSuffix(name, modelNameQuote)
	if name = strings.TrimSpace(name); opened && closed && name != "" && !strings.Contains(name, modelNameQuote) {
		return name, true
	}
	if _, effort := EffortFromModelReply(text); effort && !opened && line != "" {
		return line, true
	}
	return "", false
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

// A bare /list-agents reply lists every other Claude session on the machine
// (testdata/stream/list-agents-bare.jsonl):
//
//	Other Claude sessions (<n>):
//	  [<state>]  ·  <name>  ·  <cwd>  ·  started <age>
//
// or, with nobody else, one line opening with listAgentsNone. Claude's docs
// name subagent and teammate sections too; a bare one-shot has neither, but
// any `<Title> (<n>):` section is counted and skipped as tolerance of CLI drift.
// It is human text rather than a schema, so any other line - a live session's
// self line included - refuses the reply whole: a wrong row is worse than none.
const (
	listAgentsOthers = "Other Claude sessions"
	listAgentsNone   = "No subagents, teammates or other Claude sessions"
	listAgentsColumn = "  ·  "
)

// listAgentsHeader is a section's unindented title and count.
var listAgentsHeader = regexp.MustCompile(`^(\S.*) \(([0-9]+)\):$`)

// PeersFromListAgents reads the other sessions out of a bare /list-agents
// reply, in the order it lists them. ok is false, with nothing else, for any
// line it does not recognise.
func PeersFromListAgents(text string) (peers []Peer, ok bool) {
	var lines []string
	for line := range strings.Lines(strings.TrimSpace(text)) {
		lines = append(lines, strings.TrimRightFunc(line, unicode.IsSpace))
	}
	if len(lines) == 0 {
		return nil, false
	}
	if onlyNoPeers(lines) {
		return nil, true
	}
	return peersFromSections(lines)
}

// onlyNoPeers reports whether the body is the no-peers line and nothing else.
func onlyNoPeers(body []string) bool {
	said := 0
	for _, line := range body {
		if line != "" {
			said++
			if !strings.HasPrefix(line, listAgentsNone) {
				return false
			}
		}
	}
	return said == 1
}

// peersFromSections walks the sections of a listing. Each header's count must
// match the indented rows that follow it up to the next blank line or header;
// rows are read only under listAgentsOthers, the rest only counted.
func peersFromSections(lines []string) ([]Peer, bool) {
	var peers []Peer
	sections := 0
	for i := 0; i < len(lines); i++ {
		if lines[i] == "" {
			continue
		}
		m := listAgentsHeader.FindStringSubmatch(lines[i])
		rows := indentedRun(lines[i+1:])
		// The count as written, so "02" is a shape this was not shown.
		if m == nil || strconv.Itoa(len(rows)) != m[2] {
			return nil, false
		}
		if m[1] == listAgentsOthers {
			found, ok := peersFromRows(rows)
			if !ok {
				return nil, false
			}
			peers = append(peers, found...)
		}
		sections++
		i += len(rows)
	}
	return peers, sections > 0
}

// indentedRun is the run of indented lines that opens lines: a section's rows.
func indentedRun(lines []string) []string {
	n := 0
	for n < len(lines) && strings.TrimLeftFunc(lines[n], unicode.IsSpace) != lines[n] {
		n++
	}
	return lines[:n]
}

func peersFromRows(rows []string) ([]Peer, bool) {
	var peers []Peer
	for _, row := range rows {
		p, ok := peerFromRow(strings.TrimSpace(row))
		if !ok {
			return nil, false
		}
		peers = append(peers, p)
	}
	return peers, true
}

// peerFromRow reads one `[state]  ·  name  ·  cwd  ·  started age` row. The
// state and age are required for the shape and unread.
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
	return Peer{Name: cols[1], Dir: cols[2]}, true
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
