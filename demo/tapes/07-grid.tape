# BEAT 7 — the grid. ^Y opens a conversation in a new column, ^B stacks one
# under the focused pane, ^W closes it. The room is column zero and stays.
#
# The roster moves on ⇧↑↓, which VHS has no spelling for — so a second agent is
# picked off the board, whose cursor is plain ↑↓ and whose ^Y/^B place the
# cursored row with the room's own keys. ⇥ walks the chat ring, which moves the
# keys just as visibly for a beat about layout.
Output .work/out/07-grid.mp4
Source .work/head.tape
Hide
Source .work/stage.tape
Type "@all we're adding per-key rate limiting to the gateway. maya take the middleware, omar the 429 state in the client, priya a --rate-limit flag, luca the changelog."
Enter
Sleep 12s
Ctrl+X
Sleep 3s
Type "a"
Sleep 500ms
Enter
Sleep 5s
Show
Sleep 1500ms

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
