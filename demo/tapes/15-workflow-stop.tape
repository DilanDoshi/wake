# Workflow scene 3: x arms a stop, ↵ confirms it.
#
# otto's `lint-sweep` lints eight packages in sequence (demo/scenarios/otto.json).
# ↵ writes stop_task; the fake answers the way workflow-stop.jsonl records it -
# task_updated{killed}, task_notification{stopped}, then the receipt - so the
# row leaves the sidebar and `● Workflow "lint-sweep" halted` lands.
Output .work/out/15-workflow-stop.mp4
Source .work/head.tape
Hide
Source tapes/_workflow-take.tape
Set TypingSpeed 8ms
Type "wake new otto"
Enter
Sleep 7s
Ctrl+W
Sleep 1s
Type "@otto lint every package"
Enter
Sleep 3s
Show
Sleep 1500ms
Set TypingSpeed 60ms
Type "/board"
Sleep 400ms
Enter
Sleep 1500ms
Down
Sleep 1200ms
Enter
Sleep 5s
Type "x"
Sleep 2500ms
Enter
Sleep 3500ms
Escape
Sleep 5s
