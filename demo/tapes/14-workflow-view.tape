# Workflow scene 2: the run level, an agent's detail, and back out.
#
# The sidebar's ⇧↓ cannot be typed by VHS (every byte of a typed escape
# sequence reaches the tty on its own read, so it lands as a bare ⎋), so the
# tape reaches the same row through /board's rows view, which draws it with
# the sidebar's own row function and opens it the same way (viewingPicked).
Output .work/out/14-workflow-view.mp4
Source .work/head.tape
Hide
Source tapes/_workflow-take.tape
Set TypingSpeed 8ms
Type "wake new iris"
Enter
Sleep 7s
Ctrl+W
Sleep 1s
Type "@iris count the lines in every .txt file here, take your time"
Enter
Sleep 2s
Show
Sleep 1500ms
Set TypingSpeed 60ms
Type "/board"
Sleep 400ms
Enter
Sleep 1800ms
# The cursor opens on iris; one step is her workflow's row.
Down
Sleep 1500ms
Enter
Sleep 3500ms
# Into the agents column, down to c.txt, and into its detail.
Right
Sleep 1500ms
Down
Sleep 700ms
Down
Sleep 1200ms
Enter
Sleep 3500ms
# Expand the Activity to each call's input and result.
Enter
Sleep 3500ms
Escape
Sleep 2500ms
Escape
Sleep 1000ms
Escape
Sleep 5s
