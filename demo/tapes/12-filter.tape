# BEAT 12 — the filter. A lone `@name` in the room narrows the group chat to
# that agent's thread for as long as it is the composer's target: their lines,
# the manager's, every broadcast, and what you said to them. It is a view, not
# a mode — backspacing the name widens the room again, and sending clears the
# draft and widens it too. The header reads `group chat › @name` while narrowed.
Output .work/out/12-filter.mp4
Source .work/head.tape
Hide
Source .work/stage.tape
Type "@all we're adding per-key rate limiting to the gateway. maya take the middleware, omar the 429 state in the client, priya a --rate-limit flag, luca the changelog."
Enter
Sleep 12s
Show
Sleep 2s

# Narrow to omar. Held so the room visibly drops everybody else.
Type "@omar"
Sleep 3500ms

# Un-resolve the name and the room widens as you type.
Backspace 5
Sleep 1500ms

# Narrow to priya, then ask her something in her own thread.
Type "@priya"
Sleep 3s
Type " does --rate-limit take a number or a plan name?"
Sleep 800ms
Enter
Sleep 5s

# Back into her thread to read the answer beside the question.
Type "@priya"
Sleep 4s
