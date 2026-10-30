package daemon

// Naming the session a SendMessage reached when its `to` is an address rather
// than a name: the `uds:` socket a reply copies from the message it answers, or
// a `name [ref]`. Claude Code's session registry says who holds it, so the room
// draws the message once - the receiver's stream draws a fleet agent's - and
// under a name rather than a path.

import (
	"os"
	"path/filepath"

	"github.com/DilanDoshi/wake/internal/core"
)

// addressed is ev with its send's recipient named, or ev as it came for a bare
// name, or an address the registry does not name.
func (s *server) addressed(ev core.Event) core.Event {
	if ev.Tool == nil || ev.Tool.Send == nil {
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
	if a, ok := s.conversationRow(rec.ID); ok {
		send.ToID = a.id
	}
	tool := *ev.Tool
	tool.Send = &send
	ev.Tool = &tool
	return ev
}

// registered is the one record holding socket, or else the one named name: none
// when two carry the name, since only a ref Wake cannot read tells them apart. A
// missing directory or an unreadable record costs that send its name, no more.
func registered(config, socket, name string) (core.SessionRecord, bool) {
	if config == "" {
		return core.SessionRecord{}, false
	}
	files, _ := filepath.Glob(core.SessionRecords(config))
	var found core.SessionRecord
	n := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		rec, ok := core.DecodeSessionRecord(raw)
		if ok && (socket != "" && rec.Socket == socket || socket == "" && rec.Name == name) {
			found, n = rec, n+1
		}
	}
	return found, n == 1
}
