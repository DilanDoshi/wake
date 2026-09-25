# Workflow scene 4: s saves the run's script as /count-lines, and a new
# session runs it.
#
# The save is a file write the daemon owns; the notice names the path. A saved
# workflow runs as /<name> in *future* sessions (Claude Code's docs, and Wake's
# own notice), so the fake reads .claude/workflows once, into its startup init
# (workflow findings §5): iris, which saved it, never offers it, and rhea,
# started after the save, does - `@rhea /count` offers `/count-lines`, and
# sending it launches the workflow by name (workflow-saved-command.jsonl).
Output .work/out/16-workflow-save.mp4
Source .work/head.tape
Hide
Source tapes/_workflow-take.tape
Set TypingSpeed 8ms
Type "wake new iris"
Enter
Sleep 7s
Type "count the lines in every .txt file here, no rush"
Enter
Sleep 2s
Show
Sleep 1200ms
Set TypingSpeed 60ms
Type "/workflows"
Sleep 400ms
Enter
Sleep 2500ms
Type "s"
Sleep 2000ms
Tab
Sleep 1400ms
Tab
Sleep 1200ms
Enter
Sleep 3500ms
Escape
Sleep 800ms
# A new session, from the room: its init is the first to see the saved file.
Tab
Sleep 800ms
Type "/new rhea in /tmp/tally"
Sleep 400ms
Enter
Sleep 6s
# /new drafts `@rhea ` into the room; the command completes against rhea's own.
Type "/count"
Sleep 2500ms
Tab
Sleep 900ms
Enter
Sleep 9s
