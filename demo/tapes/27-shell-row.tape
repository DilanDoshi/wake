# BEAT 27 - a background shell. An agent that ends its turn with a shell still
# running is not done: the roster says `shell` under its name and keeps the check
# back, and the conversation keeps its done line off, until the shell ends.
#
# quinn (demo/scenarios/quinn.json) starts `make ci gate` in the background, says
# so, and goes idle with the shell running 26 s. The shell's ending is scripted
# without the follow-up turn claude would open on its notification.
#
# The same take filmed twice, for the before/after:
#
#   WF_BIN=<dir holding main's wake> vhs tapes/27-shell-row.tape   # before
#   vhs tapes/27-shell-row.tape                                    # after
#
# and move the mp4 aside between runs - VHS takes no variable in a path.
Output .work/out/27-shell-row.mp4
Source .work/head.tape
Hide
Source tapes/_workflow-take.tape
Set TypingSpeed 8ms
Type "wake new quinn"
Enter
Sleep 7s
# ⇥ hands the keys to the room, keeping quinn's conversation beside it.
Tab
Sleep 1s
Show
Sleep 1500ms
Set TypingSpeed 45ms
Type "@quinn run the gate in the background"
Sleep 500ms
Enter
# The turn ends within a few seconds; the shell goes on for 26.
Sleep 22s
# The shell ends: its row goes, and the check comes back.
Sleep 12s
