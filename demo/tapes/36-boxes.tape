# BEAT 36 — the sidebar's team boxes, on the big fleet: a team is a box with its
# header for a top edge, the agents above the first team a box under the pinned
# all. /team moves an agent from one box to another; a click on a header picks that
# team's channel and its box wears the pick. The clicks are mouse reports written
# by shiftkeys.py (gestures/36-boxes.json). Record it with WANT_AGENTS=22.
Output .work/out/36-boxes.mp4
Source .work/head.tape
Hide
Type "export WAKE_GESTURES=36-boxes"
Enter
Source .work/stage-fleet.tape
# The staging's last notice times out before the camera is on.
Sleep 12s
Show
Sleep 3500ms

# An agent joins a team and its box takes it in.
Set TypingSpeed 90ms
Type "/team @triage payments"
Sleep 600ms
Enter
Sleep 3s
Type "/team @scratch-spike infra"
Sleep 600ms
Enter
Sleep 3500ms

# A click on a header picks the channel; its box wears the pick.
Ctrl+L
Sleep 4s
Ctrl+N
Sleep 4s
Ctrl+O
Sleep 3500ms
