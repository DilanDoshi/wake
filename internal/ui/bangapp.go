package ui

// How a bang reaches a conversation, and how the answer gets back into one.
//
// This is the only part of the feature that knows what a conversation is, which
// is why it is its own file: bang.go runs a command and knows nothing about
// sessions, and a window that holds many DMs and a room is where a result has
// to find the one it was typed into.
//
// Two call sites in App: App.submit, which asks before it treats a draft as a
// message, and App.Update, which folds bangResultMsg rather than letting it fall
// through to the composer. Both are wired.

import (
	tea "github.com/charmbracelet/bubbletea"
)

// bang takes over a submission that is a local command, and reports whether it
// took it.
//
// The draft is cleared on dispatch rather than on completion, which is the
// difference between a composer and a modal: a command can take thirty seconds
// and the operator is not made to watch it. What they type next is theirs, and
// bangResult must not walk over it - see the note there about copies.
//
// A session that has ended is not a reason to refuse. A bang never reaches the
// agent, the socket or the daemon, so `!git status` is exactly as answerable in
// a conversation whose agent is gone as in one whose agent is working - and a
// window somebody is reading after a crash is a window where that question gets
// asked. This is deliberately unlike App.submit, which has nowhere to send a
// message and says so.
func (a App) bang(text string) (App, tea.Cmd, bool) {
	cmdline, ok := isBang(text)
	if !ok {
		return a, nil, false
	}
	a = a.clearDraft()
	return a, runBang(a.conversationID(), a.bangDir(), cmdline, bangTimeout), true
}

// bangResult puts a finished command into the conversation it was typed into.
//
// Addressed, and checked, for the reason apply attributes a frame: a window now
// holds a room and every DM somebody has opened, and a result that landed in
// whichever one happened to be on screen would be an answer about one
// repository shown under another.
//
// It appends through the conversation the App is holding *now*, not one
// captured when the command started. Composers share a text area by pointer and
// a DM is a value: folding into a stale copy would put the transcript back to
// what it was thirty seconds ago and take whatever was typed in the meantime
// with it.
func (a App) bangResult(m bangResultMsg) App {
	if m.ID == "" {
		// Typed into the room, which is addressed to nobody - so it goes back
		// there, unattributed, exactly as the operator's own messages do.
		a = a.withRoom(a.room.Append(bangEvent(m), Agent{}))
		return a
	}
	dm, ok := a.dms[m.ID]
	if !ok {
		return a
	}
	return a.withDM(m.ID, dm.Append(bangEvent(m)))
}

// bangDir is the directory a bang runs in: the one its agent runs in.
//
// It used to be the empty string, which os/exec reads as "the directory this
// process is in". That was right for `wake` and `wake new`, because cmd/wake
// spawns a session with Dir set to the client's working directory - and wrong
// for `wake attach <who>` from anywhere else, and wrong again the moment a
// window holds agents from several repositories at once, which is what the room
// is for. rpc.SessionStatus.Dir closed the reason it could not be fixed; this is
// the fix.
//
// Two situations still get the empty string back and they are not the same one.
// The **room** has no directory, and that is the honest answer: a bang typed
// into the group chat is addressed to no agent, so it runs where `wake` was
// started. An **agent no report has described yet** also has none - fan-out
// starts before the spawn confirmation is enqueued - and there it is a silent
// revert to the behaviour this function exists to close, in a window that may
// hold several repositories. It is narrow (one report closes it, and a bang
// needs a DM open, which needs an agent the roster listed) and there is nothing
// better to answer, so it is named here rather than papered over.
func (a App) bangDir() string {
	agent, ok := a.fleet.Agent(a.conversationID())
	if !ok {
		return ""
	}
	return agent.Cwd
}
