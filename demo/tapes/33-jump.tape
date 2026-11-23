# BEAT 33 — the agent finder. ⌃F opens a box over the composer: a few letters of
# a name narrow it, ↵ puts the agent in a new column, ⌃B opens it below the pane
# you are in and ⌃D in that pane. /jump <letters> opens it with the filter typed.
# Filmed on the big fleet (21 agents, three of them waiting on an ask), where
# walking the roster is too slow and a finder is not. Record it with
# WANT_AGENTS=22.
Output .work/out/33-jump.mp4
Source .work/head.tape
Hide
Source .work/stage-fleet.tape
# The staging's last notice times out before the camera is on.
Sleep 12s
Show
Sleep 2500ms

# The box with nothing typed: every agent, the ones waiting on you first.
Ctrl+F
Sleep 3s

# Three letters, one agent.
Set TypingSpeed 220ms
Type "blu"
Sleep 3s
Escape
Sleep 1500ms

# The same box from a command, filter and all; walk the matches, open one.
Set TypingSpeed 90ms
Type "/jump pay"
Sleep 600ms
Enter
Sleep 2500ms
Down
Sleep 900ms
Down
Sleep 900ms
Enter
Sleep 4s

# From that conversation: ⌃B opens the next one below it...
Ctrl+F
Sleep 1s
Set TypingSpeed 220ms
Type "gw"
Sleep 2s
Ctrl+B
Sleep 4s

# ...and ⌃D opens one in the pane that has the keys.
Ctrl+F
Sleep 1s
Type "docs"
Sleep 2s
Ctrl+D
Sleep 4s
