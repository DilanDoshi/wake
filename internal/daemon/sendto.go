package daemon

// Naming the session a SendMessage reached when its `to` is an address - the
// `uds:` socket a reply copies from the message it answers, or `name [ref]` - from
// Claude Code's session registry, so the room draws it once and under a name.

import (
	"os"
	"path/filepath"

	"github.com/DilanDoshi/wake/internal/core"
)

// addressed is ev with its send's recipient named, or ev as it came for a bare
// name, a subagent's send (drawn by nobody's name), or an address nothing names.
func (s *server) addressed(ev core.Event) core.Event {
	if ev.Tool == nil || ev.Tool.Send == nil || ev.Subagent != nil {
		return ev
	}
	socket, name := core.SendAddress(ev.Tool.Send.To)
	if socket == "" && name == "" {
		return ev
	}
	rec, ok := registered(claudeConfigDir(), socket, name)
	if !ok {
		return ev
	}
	send := *ev.Tool.Send
	send.ToName = rec.Name
	if a, ok := s.conversationRow(rec.ID); ok && inGroup(rec.PID, a.sess.Pgid()) {
		send.ToID = a.id
	}
	tool := *ev.Tool
	tool.Send = &send
	ev.Tool = &tool
	return ev
}

// registered is the one record holding socket, or else the one named name: none
// when two carry the name, since only a ref Wake cannot read tells them apart, or
// when any record was unreadable, since it may be the one named. A nameless
// record names nobody. An unreadable registry or record costs that send its
// name, and is logged.
func registered(config, socket, name string) (core.SessionRecord, bool) {
	if config == "" {
		return core.SessionRecord{}, false
	}
	dir := core.SessionRegistry(config)
	entries, err := os.ReadDir(dir)
	if err != nil {
		logf("wake: session registry unreadable, a send is drawn as addressed: %v", err)
		return core.SessionRecord{}, false
	}
	var found core.SessionRecord
	n, skipped := 0, false
	for _, e := range entries {
		if !core.IsSessionRecord(e.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		rec, ok := core.DecodeSessionRecord(raw)
		if err != nil || !ok {
			logf("wake: session record %s skipped: unreadable or incomplete (%v)", e.Name(), err)
			skipped = true
			continue
		}
		if rec.Name != "" && (socket != "" && rec.Socket == socket || socket == "" && rec.Name == name) {
			found, n = rec, n+1
		}
	}
	return found, n == 1 && (socket != "" || !skipped)
}
