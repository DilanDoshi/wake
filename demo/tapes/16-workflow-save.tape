# Workflow scene 4: s saves the run's script as /count-lines, and it runs again.
#
# The save is a file write the daemon owns; the notice names the path. The
# fake advertises a saved .claude/workflows/<name>.js in the next turn's init
# (workflow findings §5), which the run's own follow-up turn provides - so
# `/count` then offers `/count-lines`, and sending it launches the workflow by
# name, the way workflow-saved-command.jsonl records it.
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
Sleep 4s
Escape
Sleep 9s
Type "/count"
Sleep 2500ms
Tab
Sleep 900ms
Enter
Sleep 8s
