# Workflow scene 6: a failed run's red ending, and its error in the view.
#
# juno's `migrate-fixtures` (demo/scenarios/juno.json) finishes every agent and
# then throws, the shape of workflow-failed.jsonl: task_updated carries
# patch.error, the ending line is red with the error's first line, and the
# run level shows that line under its header.
Output .work/out/18-workflow-failed.mp4
Source .work/head.tape
Hide
Source tapes/_workflow-take.tape
Set TypingSpeed 8ms
Type "wake new juno"
Enter
Sleep 7s
Tab
Sleep 1s
Type "@juno migrate the fixtures to v2"
Enter
Sleep 1500ms
Show
Sleep 15s
# Back into juno's conversation, and its one run.
Tab
Sleep 1200ms
Set TypingSpeed 60ms
Type "/workflows"
Sleep 400ms
Enter
Sleep 4s
Right
Sleep 1200ms
Escape
Sleep 800ms
Down
Sleep 800ms
Down
Sleep 1200ms
Right
Sleep 1500ms
Enter
Sleep 4s
