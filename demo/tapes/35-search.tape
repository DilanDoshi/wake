# BEAT 35 — the sidebar's search bar. A click on it, a few letters, and the roster
# narrows in place inside its team boxes, a team with no match dropping out; ↓
# walks the matches and ↵ opens one; a click elsewhere lets go of the bar and the
# filter stands, with its count, until esc clears it. The click is a mouse report
# written by shiftkeys.py (gestures/35-search.json). Record it with WANT_AGENTS=22.
Output .work/out/35-search.mp4
Source .work/head.tape
Hide
Type "export WAKE_GESTURES=35-search"
Enter
Source .work/stage-fleet.tape
# The staging's last notice times out before the camera is on.
Sleep 12s
Show
Sleep 2500ms

# Click the bar, type: five teams become the ones with a match.
Ctrl+L
Sleep 1500ms
Set TypingSpeed 220ms
Type "limits"
Sleep 3500ms

# Walk the matches (a team header is a stop) and open one.
Down
Sleep 1s
Down
Sleep 1s
Enter
Sleep 4s

# A click elsewhere lets go of the bar; the filter stands.
Ctrl+N
Sleep 3500ms

# Back to the bar, and esc clears the filter.
Ctrl+L
Sleep 1500ms
Escape
Sleep 3s
