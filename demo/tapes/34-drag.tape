# BEAT 34 — drag an agent out of the sidebar and drop it where it should go. A
# click opens one in a new column; a drag shows its name at the pointer and the
# drop zone outlined where the pane will land: the lower half of a pane opens
# below it, anywhere else a new column; esc cancels. VHS cannot send the mouse, so
# wake runs under shiftkeys.py with gestures/34-drag.json, which writes the SGR
# reports a terminal would. Filmed on the big fleet; record it with WANT_AGENTS=22.
Output .work/out/34-drag.mp4
Source .work/head.tape
Hide
Type "export WAKE_GESTURES=34-drag"
Enter
Source .work/stage-fleet.tape
# The staging's last notice times out before the camera is on.
Sleep 12s
Show
Sleep 2500ms

# A click opens an agent in a new column beside the room.
Ctrl+L
Sleep 3500ms

# A drag onto the lower half of a pane opens it below that pane...
Ctrl+N
Sleep 3500ms

# ...and onto the room's upper half, in a new column.
Ctrl+O
Sleep 3500ms

# esc cancels a drag in flight, and is nothing else there: letting go after it
# opens nothing.
Ctrl+X
Sleep 2s
Escape
Sleep 1500ms
Ctrl+Y
Sleep 3s
