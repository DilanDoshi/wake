package ui

// `!cmd`: run a shell command and put its output in the conversation.
//
// # Why Wake has to do this at all
//
// Because the bang is an interactive-TUI feature and Wake does not drive the
// interactive TUI. Measured through Wake's exact argv, `!cat note.txt` reached
// the model as *text* - no local-stdout frame, no interception, and the model
// answered as though it had been asked about the file. Silently doing something
// plausible is the worst of the available failures, so Wake intercepts the bang
// itself.
//
// # Why the output goes in as a user frame
//
// Because that is the shape the CLI itself uses for the same job. A slash
// command survives stream-json and returns its output as a user frame the
// airlock already strips its markers off and the DM already renders as an
// echoed turn. So there is no new frame type, no new event kind, no airlock
// change and no daemon work: an interception, a local exec, and an injected
// event. Nothing in this file names a wire word - the markers live inside the
// decoder, and Wake writes the stripped form directly.
//
// # Why this file spawns a process, next to a rule saying the UI does not
//
// CLAUDE.md's non-negotiable is that the UI never touches *an agent's* process:
// spawning, killing and supervising claude belong to the daemon, or the daemon
// boundary is decoration. A bang is not an agent. It is the operator's own
// shell line, typed into their own composer, whose whole value is that it runs
// where their conversation is and returns before they have looked away. Routing
// it through the daemon would need a frame kind, a fan-out decision and an
// answer to "whose client asked" for a result that belongs to exactly one
// window. That is the trade, and it is worth naming rather than assuming: the
// day the daemon is not on the same machine as the TUI, this is the line that
// has to move, and `!cmd` will be measurably wrong before it is subtly wrong -
// it will run in the wrong repository.
//
// # Where it runs
//
// Where the *session* runs, not where the daemon runs. An agent's conversation
// is about its own repository, and a bang answered from somewhere else is about
// a different one. See App.bangDir for how much of that this build can honour
// and where it stops.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	// bangPrefix is what makes a draft a local command rather than a message.
	bangPrefix = "!"

	// bangTimeout bounds one command.
	//
	// Not optional. Bubble Tea runs every tea.Cmd on its own goroutine, so a
	// `!sleep 600` does not hang the UI - it parks a goroutine holding a
	// process for ten minutes, and a person who typed it and moved on has no
	// way to reach either. Thirty seconds clears every command anybody types
	// into a chat box and is far short of "this is still here at lunchtime".
	bangTimeout = 30 * time.Second

	// bangMaxBytes bounds one command's output.
	//
	// A DM's scrollback is unbounded for the life of the session by a
	// deliberate decision, so what a bang writes is pinned until the
	// conversation ends. 64KB is a screenful of screenfuls and the same order
	// as the airlock's own starting line buffer.
	bangMaxBytes = 64 * 1024

	// bangWaitDelay bounds the wait *after* the command is over.
	//
	// Killing a process does not close a descriptor somebody else is holding.
	// `!(sleep 600 &) ; echo started` exits immediately and leaves a child on
	// the other end of the pipe os/exec is copying from, and with WaitDelay
	// zero that copy ends at EOF - which never comes. Wait would never return
	// and the bang's goroutine would live as long as whatever the operator
	// backgrounded. With it, os/exec closes the pipes and Wait reports
	// exec.ErrWaitDelay, which bangText says out loud rather than swallowing.
	//
	// Two seconds is far longer than a finished command needs to flush and far
	// shorter than an operator should wait for output that has already been
	// written. It is internal/core's waitDelay for the same reason.
	bangWaitDelay = 2 * time.Second

	// bangShell is what runs the line, with the flag that takes a whole line.
	// The point of a bang is that it is a shell line - `!ls | wc -l` has to
	// mean what it looks like - and a bare exec of the first word would be a
	// different, more surprising feature.
	bangShell     = "/bin/sh"
	bangShellFlag = "-c"

	// The three things that can be true of a finished command besides its
	// output. Each is a line appended under that output, because a bound that
	// silently cut something is a wrong transcript rather than a short one.
	bangTruncated = "… truncated at %d bytes"
	bangTimedOut  = "… timed out after %s"
	bangExited    = "… exited %d"

	// bangAbandoned is the command that ended while something it started did
	// not, and still holds the pipe its output was arriving on. Reported
	// because what is above it may be incomplete, which is exactly the thing a
	// reader cannot tell by looking.
	bangAbandoned = "… ended, but something it started held its output open past %s"

	// bangFailed is anything else: a shell that could not be started at all,
	// or an exit that carries no status because a signal ended it.
	bangFailed = "… %v"

	// bangReclaimed is the group kill after Run finding something still in the
	// command's process group. Said for bangAbandoned's reason - a bound that
	// silently cut something is a wrong transcript rather than a short one - and
	// it is the only line a job whose output was redirected produces at all.
	//
	// It says what Wake *did*, not what it concluded: a kill(2) to a group
	// answers only that the group had a member, which a zombie satisfies too.
	// So this claims no more than that something was there and is gone, and
	// deliberately fires on every backgrounded command - `!npm run dev &` really
	// is killed when the bang returns, and that is the thing to say out loud.
	bangReclaimed = "… killed what was still in the command's process group"

	// bangCleanupFailed is the reclaim that could not be done at all, which is
	// the one outcome that leaves something running with nobody holding it.
	bangCleanupFailed = "… failed to reclaim what the command started: %v"
)

// isBang reports whether text is a local command, and returns the command.
//
// Only a leading bang, and only with something after it. An inline `!` is
// punctuation - "does it work? !" - and a bare one is a typo, not a request to
// run the empty command. Leading blanks are skipped because a draft that
// starts with a space is still a draft that starts with a bang.
func isBang(text string) (string, bool) {
	trimmed := strings.TrimLeft(text, " \t")
	if !strings.HasPrefix(trimmed, bangPrefix) {
		return "", false
	}
	cmdline := strings.TrimSpace(strings.TrimPrefix(trimmed, bangPrefix))
	return cmdline, cmdline != ""
}

// bangResultMsg is one finished command, addressed to the conversation it was
// typed into.
//
// It carries the command as well as the output because the two are one thing to
// a reader: the transcript is a record of what the operator did, and output
// with no line above it is an unattributable block of text three messages later.
type bangResultMsg struct {
	ID   string
	Cmd  string
	Text string
}

// runBang runs one command off the draw goroutine and reports what it said.
//
// It never returns an error the caller has to format: a bang that failed is
// still output, and the operator wants the same treatment for a non-zero exit
// as for a zero one. What did not happen is the only thing worth
// distinguishing, and each of those cases says so in its own words.
func runBang(id, dir, cmdline string, timeout time.Duration) tea.Cmd {
	return func() tea.Msg {
		return bangResultMsg{
			ID:   id,
			Cmd:  cmdline,
			Text: bangRun(dir, cmdline, timeout, bangWaitDelay),
		}
	}
}

// bangRun is the whole execution, separated from the tea.Cmd so a test can run
// it synchronously and so both bounds can be driven from one place.
//
// Two mechanisms, and they are not redundant. A timeout and the cleanup after
// Run kill the process *group*, reclaiming a pipeline or background job;
// WaitDelay bounds the wait for pipes those processes may still hold. Either
// alone leaves one of the two failures.
func bangRun(dir, cmdline string, timeout, waitDelay time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out := &bangOutput{limit: bangMaxBytes}
	cmd := exec.CommandContext(ctx, bangShell, bangShellFlag, cmdline)
	cmd.Dir = dir
	// One writer for both, and os/exec reads that as one descriptor and one
	// copying goroutine rather than two - the same thing CombinedOutput relies
	// on. A command's diagnosis and its answer are one thing to somebody
	// reading a conversation, and interleaved is what a terminal would have
	// shown them anyway.
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = waitDelay
	bangSetGroup(cmd)
	cmd.Cancel = func() error { return bangKillGroup(cmd) }

	err := cmd.Run()
	cleanupErr := bangKillGroup(cmd)
	// Contained here because this is where a child's bytes first become a Wake
	// string, and it is the one such point for both readers: the `!cmd` block
	// and the /mcp panel, which is this same run with a different renderer.
	// Everything bangText adds under it is Wake's own words. docs/notes/bugs.md
	// BUG-9.
	text := bangText(core.Contained(out.String()), out.truncated, err, ctx.Err() != nil, timeout, waitDelay)
	switch {
	// KillGroup answers nil only when the group still had a member to signal,
	// and Run has already reaped the shell - so whatever was killed is
	// something the command left behind rather than the command itself.
	case cleanupErr == nil:
		return bangNote(text, bangReclaimed)
	case !errors.Is(cleanupErr, os.ErrProcessDone):
		return bangNote(text, fmt.Sprintf(bangCleanupFailed, cleanupErr))
	}
	return text
}

// bangText assembles what the conversation is told, output first and the reason
// it is not the whole story underneath it.
//
// The order of the cases is the order of the questions. A timeout outranks the
// exit status it produced, because the status of a process Wake killed is
// Wake's own answer echoed back. An abandoned pipe outranks the same way: the
// command's own exit was clean and the delay is the thing worth saying.
func bangText(out string, truncated bool, err error, timedOut bool, timeout, waitDelay time.Duration) string {
	if truncated {
		out = bangNote(out, fmt.Sprintf(bangTruncated, bangMaxBytes))
	}
	switch {
	case timedOut:
		return bangNote(out, fmt.Sprintf(bangTimedOut, timeout))
	case errors.Is(err, exec.ErrWaitDelay):
		return bangNote(out, fmt.Sprintf(bangAbandoned, waitDelay))
	case err != nil:
		return bangNote(out, bangFailure(err))
	}
	return out
}

// bangFailure names a non-zero exit by its status, and anything else by what Go
// said. A status below zero is a process a signal ended, which has no exit code
// to print - "exited -1" would be a number nobody can look up.
func bangFailure(err error) string {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		return fmt.Sprintf(bangExited, exit.ExitCode())
	}
	return fmt.Sprintf(bangFailed, err)
}

// bangNote puts one line under a command's output, with no blank row between
// them and none left dangling when there was no output at all.
func bangNote(out, line string) string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return line
	}
	return out + "\n" + line
}

// bangEvent is how the result enters a transcript: the same echoed user turn
// the CLI produces for a slash command, so it renders under the muted label
// rather than as something the operator said.
//
// The command line rides *inside* the block with its output, which is the one
// deliberate difference from the frame the CLI sends. A DM renders an echoed
// user turn as markdown, and markdown joins consecutive lines into a paragraph:
// unfenced, three files from `!ls` arrive as one line of three words, which is
// a wrong transcript of the kind this project keeps having to name. Fenced,
// what the command printed is what is shown, and the command line above it is
// safe from being read as emphasis or a heading.
func bangEvent(m bangResultMsg) core.Event {
	return core.Event{
		Kind:      core.KindUserText,
		SessionID: m.ID,
		Echoed:    true,
		Text:      bangBlock(bangPrefix + m.Cmd + "\n" + m.Text),
	}
}

// bangBlock fences body as preformatted text, with a fence long enough to
// survive a body that contains one. `!cat README.md` is an ordinary thing to
// type and a fence inside a fence would end the block early, spilling the rest
// of the file back into markdown - the failure this fence exists to prevent,
// arrived at from the inside.
func bangBlock(body string) string {
	fence := strings.Repeat(bangFenceRune, max(bangFenceMin, longestRun(body, bangFenceRune)+1))
	return fence + "\n" + strings.TrimRight(body, "\n") + "\n" + fence
}

const (
	// bangFenceRune opens and closes a preformatted block, and bangFenceMin is
	// the shortest markdown accepts.
	bangFenceRune = "`"
	bangFenceMin  = 3
)

// longestRun is the longest unbroken repetition of s in body.
func longestRun(body, s string) int {
	longest, run := 0, 0
	for _, r := range body {
		if string(r) == s {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	return longest
}
