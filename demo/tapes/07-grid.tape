# BEAT 7 — mention one, open one. `@priya` reaches priya alone and her answer
# comes back into the room; the roster's cursor (⇧↓, sent as Ctrl+G through
# shiftkeys.py) and ↵ open her conversation, which is Claude Code's. Then the
# grid: ^Y opens a conversation in a new column, ^B stacks one under the
# focused pane, ^W closes it. The room is column zero and stays. A second agent
# is picked off the board, whose ^Y/^B place the cursored row with the room's
# own keys, and ⇥ walks the chat ring.
Output .work/out/07-grid.mp4
Source .work/head.tape
Hide
Source .work/stage.tape
Show
Sleep 1500ms
Type "@priya does --rate-limit take a number or a plan name?"
Sleep 700ms
Enter
Sleep 6s

# Down the roster to priya, and open her conversation.
Ctrl+G
Sleep 500ms
Ctrl+G
Sleep 500ms
Ctrl+G
Sleep 900ms
Enter
Sleep 3500ms

# The session's own commands, advertised on its init frame — not Wake's guesses.
Type "/co"
Sleep 3s
Backspace 3
Sleep 800ms
Type "/board"
Enter
Sleep 2s
Down
Sleep 800ms
Ctrl+Y
Sleep 3500ms
Type "/board"
Enter
Sleep 2s
Down
Sleep 600ms
Down
Sleep 800ms
Ctrl+B
Sleep 3500ms
Tab
Sleep 1200ms
Tab
Sleep 1500ms
Ctrl+W
Sleep 2500ms
