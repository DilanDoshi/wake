package ui

// esc esc's second step: one prompt chosen, what restoring its files would
// change, and Claude Code's choices - restore the conversation, the code and
// the conversation, or the code, or never mind (checkpointing.md).
//
// The preview is the dry run FrameRewindPreview asks for, and it alone decides
// what is offered: code only when claude says files would change, so an agent
// started without checkpoints, or a prompt past a /clear, shows claude's own
// reason instead. Restoring code writes files on disk, so it is armed like
// Wake's other destructive keys - the first ↵ arms, the second restores, and
// App.disarmed takes the arm back on any other input.

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	rewindPreviewFailed = "asking which files that rewind would change"

	// restoreFilesShown bounds the file rows: the pane clips a menu from the
	// bottom, and the choices sit above the list so they are the last to go.
	restoreFilesShown = 5
)

// rewindAction is one choice of the second step.
type rewindAction int

const (
	rewindConversation rewindAction = iota
	rewindBoth
	rewindCode
	rewindNeverMind
)

var rewindActionLabels = map[rewindAction]string{
	rewindConversation: "Restore conversation",
	rewindBoth:         "Restore code and conversation",
	rewindCode:         "Restore code",
	rewindNeverMind:    "Never mind",
}

// rewindRestore is the picker's second step; an empty Target is the first.
// Preview is the dry run's answer, nil while it is out.
type rewindRestore struct {
	Target  string
	Prompt  string
	Preview *core.FilesRewind
	Cursor  int
	Armed   bool
}

func (r rewindRestore) open() bool { return r.Target != "" }

// restoresCode is whether the preview says files would change.
func (r rewindRestore) restoresCode() bool {
	p := r.Preview
	return p != nil && p.Restorable && p.Error == "" && len(p.Files) > 0
}

// actions are the choices on offer, none while the preview is out. The cursor
// rests on the first, the one that writes nothing to disk.
func (r rewindRestore) actions() []rewindAction {
	switch {
	case r.Preview == nil:
		return nil
	case r.restoresCode():
		return []rewindAction{rewindConversation, rewindBoth, rewindCode, rewindNeverMind}
	}
	return []rewindAction{rewindConversation, rewindNeverMind}
}

// chooseRewind is ↵ on a prompt: it asks what restoring that prompt's files
// would change and waits for the answer in the second step.
func (a App) chooseRewind() (App, tea.Cmd) {
	p := a.rewind
	if p.Cursor < 0 || p.Cursor >= len(p.UUIDs) || a.endedAgent(p.Session) || a.rewindRunning(p.Session) {
		return a.closeRewind(), nil
	}
	p.Restore = rewindRestore{Target: p.UUIDs[p.Cursor], Prompt: p.Prompts[p.Cursor]}
	a.rewind = p
	return a, a.write(rewindPreviewFailed, rpc.Frame{Kind: rpc.FrameRewindPreview, SessionID: p.Session, RewindTarget: p.Restore.Target})
}

// restoreKey is the second step's keys. esc takes an arm back, or else returns
// to the list; ↵ before the preview has landed waits rather than queueing.
func (a App) restoreKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	r := a.rewind.Restore
	switch m.Type {
	case tea.KeyUp, tea.KeyDown:
		by := 1
		if m.Type == tea.KeyUp {
			by = -1
		}
		r.Cursor = clamp(r.Cursor+by, 0, max(len(r.actions())-1, 0))
		r.Armed = false
	case tea.KeyEsc:
		if !r.Armed {
			r = rewindRestore{}
		}
		r.Armed = false
	case tea.KeyEnter:
		next, cmd := a.confirmRestore()
		return next, cmd, true
	default:
		return a, nil, false
	}
	a.rewind.Restore = r
	return a, nil, true
}

// confirmRestore acts on the cursored choice. A code restore arms first.
func (a App) confirmRestore() (App, tea.Cmd) {
	r := a.rewind.Restore
	acts := r.actions()
	if r.Cursor >= len(acts) {
		return a, nil // the preview is still out
	}
	if a.endedAgent(a.rewind.Session) || a.rewindRunning(a.rewind.Session) {
		return a.closeRewind(), nil
	}
	switch acts[r.Cursor] {
	case rewindNeverMind:
		a.rewind.Restore = rewindRestore{}
		return a, nil
	case rewindConversation:
		return a.sendRewind(rpc.FrameRewind)
	}
	if !r.Armed {
		r.Armed = true
		a.rewind.Restore = r
		return a, nil
	}
	if acts[r.Cursor] == rewindBoth {
		return a.sendRewind(rpc.FrameRewindBoth)
	}
	return a.sendRewind(rpc.FrameRewindFiles)
}

// sendRewind writes kind for the chosen prompt and closes the picker, whether
// or not the write succeeds - confirmPicker's reasoning.
func (a App) sendRewind(kind string) (App, tea.Cmd) {
	p := a.rewind
	f := rpc.Frame{Kind: kind, SessionID: p.Session, RewindTarget: p.Restore.Target, RewindLastSeen: p.LastSeen}
	a = a.closeRewind()
	return a, a.write(rewindFailed, f)
}

// noteFilesRewind folds a KindFilesRewindReceipt: a preview fills the second
// step it was asked for and nothing else; a restore is reported.
func (a App) noteFilesRewind(sessionID string, ev core.Event) App {
	if ev.Kind != core.KindFilesRewindReceipt || ev.Files == nil {
		return a
	}
	f := *ev.Files
	if !f.Preview {
		return a.restoreReported(sessionID, f)
	}
	r := a.rewind.Restore
	if !a.rewind.Open() || a.rewind.Session != sessionID || r.Target != f.Target || r.Preview != nil {
		return a
	}
	r.Preview = &f
	a.rewind.Restore = r
	return a
}

// restoreReported says what a restore did. A both whose restore succeeded is
// remembered, so a conversation rewind refused after it says the files are
// already back (noteRewind).
func (a App) restoreReported(sessionID string, f core.FilesRewind) App {
	who := agentPrefix + a.agentName(sessionID)
	if !f.Restorable || f.Error != "" {
		reason := refusalReason(f)
		if f.Both {
			notice.Report("%s's files were not restored, and the conversation was left as it was: %s", who, reason)
		} else {
			notice.Report("%s's files were not restored: %s", who, reason)
		}
		return a
	}
	if f.Skipped > 0 {
		notice.Report("%s's files were restored · %s left as they are", who, plural(f.Skipped, "linked file"))
	} else {
		notice.Report("%s's files were restored", who)
	}
	if f.Both {
		a.rewindAfterRestore = sessionID
	}
	return a
}

// restoreView draws the second step: the prompt, what its files would do, a
// warning when another live agent works in the same directory, the choices,
// and then the files themselves - last, so a short pane clips them first.
func (a App) restoreView(width int) string {
	p := a.rewind
	r := p.Restore
	agent, _ := a.fleet.Agent(p.Session)
	rows := []string{detailRow(fmt.Sprintf("rewind %s%s to %q", agentPrefix, agent.Name, firstLine(r.Prompt)), width)}
	if r.Preview == nil {
		return strings.Join(append(rows, detailRow("checking files…", width)), "\n")
	}
	rows = append(rows, detailRow(restoreSummary(*r.Preview), width))
	if shared := a.sharedDirectory(agent); shared != "" {
		rows = append(rows, ErrorStyle.MaxWidth(width).Render("⚠ "+shared))
	}
	for i, act := range r.actions() {
		label := rewindActionLabels[act]
		if i == r.Cursor && r.Armed {
			label = "↵ again restores " + plural(len(r.Preview.Files), "file") + " · esc cancel"
		}
		rows = append(rows, optionRow(label, width, i == r.Cursor, false, AccentStyle))
	}
	if r.restoresCode() {
		rows = append(rows, restoreFileRows(r.Preview.Files, agent.Cwd, width)...)
	}
	return strings.Join(rows, "\n")
}

func restoreSummary(f core.FilesRewind) string {
	switch {
	case !f.Restorable || f.Error != "":
		return "code can't be restored: " + refusalReason(f)
	case len(f.Files) == 0:
		return "no file changes since this prompt"
	}
	return fmt.Sprintf("%s would change · +%d −%d", plural(len(f.Files), "file"), f.Insertions, f.Deletions)
}

// restoreFileRows lists the files a restore would change, relative to the
// agent's directory where they sit under it, bounded at restoreFilesShown.
func restoreFileRows(files []string, cwd string, width int) []string {
	rows := make([]string, 0, restoreFilesShown+1)
	for i, path := range files {
		if i == restoreFilesShown {
			rows = append(rows, detailRow(fmt.Sprintf("· +%d more", len(files)-i), width))
			break
		}
		if rel, err := filepath.Rel(cwd, path); err == nil && cwd != "" && !strings.HasPrefix(rel, "..") {
			path = rel
		}
		rows = append(rows, detailRow("· "+path, width))
	}
	return rows
}

// sharedDirectory names the other live agents running in agent's directory,
// whose edits a restore can put back too - working ones marked. Not the
// manager: it runs with no tools, so it has no edits to lose.
func (a App) sharedDirectory(agent Agent) string {
	if agent.Cwd == "" {
		return ""
	}
	var names []string
	for _, other := range a.fleet.Agents() {
		if other.ID == agent.ID || other.Cwd != agent.Cwd || other.Name == core.ManagerName ||
			other.State == rpc.StateEnded || other.State == rpc.StateParked || other.State == rpc.StateOrphaned {
			continue
		}
		name := agentPrefix + other.Name
		if turnInFlight(other.State) {
			name += " (working)"
		}
		names = append(names, name)
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " also runs in this directory"
	}
	return strings.Join(names, ", ") + " also run in this directory"
}

// refusalReason is claude's own reason for refusing a file rewind.
func refusalReason(f core.FilesRewind) string {
	if f.Error == "" {
		return "claude gave no reason"
	}
	return f.Error
}

// rewindRefused folds the daemon's error about sessionID. A preview it could
// not write will never answer, so a step waiting on one returns to the list;
// and a both whose conversation half did not follow is forgotten, so no later
// refusal is worded as its.
func (a App) rewindRefused(sessionID string) App {
	if a.rewind.Session == sessionID && a.rewind.Restore.open() && a.rewind.Restore.Preview == nil {
		a.rewind.Restore = rewindRestore{}
	}
	if a.rewindAfterRestore == sessionID {
		a.rewindAfterRestore = ""
	}
	return a
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
