package core

// How a shell line and its output read as conversation text: one preformatted
// block, the same whether Wake drew it live or read it back off a transcript.

import "strings"

// ShellPrefix is what marks a line as a shell command, in a draft and in the
// block that records it.
const ShellPrefix = "!"

const (
	// fenceRune opens and closes a preformatted block, and fenceMin is the
	// shortest markdown accepts.
	fenceRune = "`"
	fenceMin  = 3
)

// ShellBlock is one `!cmd` and what it printed, as a block. Either half may be
// empty: Claude Code records the command and its output as separate lines.
func ShellBlock(command, output string) string {
	var lines []string
	if command != "" {
		lines = append(lines, ShellPrefix+command)
	}
	if out := strings.TrimRight(output, "\n"); out != "" {
		lines = append(lines, out)
	}
	return Fenced(strings.Join(lines, "\n"))
}

// Fenced fences body as preformatted text, with a fence long enough to survive
// a body that contains one. Unfenced, markdown joins a command's lines into one
// paragraph; fenced too short, `!cat README.md` ends its own block early and
// spills the rest of the file back into markdown.
func Fenced(body string) string {
	fence := strings.Repeat(fenceRune, max(fenceMin, longestRun(body, fenceRune)+1))
	return fence + "\n" + strings.TrimRight(body, "\n") + "\n" + fence
}

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
