# BEAT 25 — done. An agent whose turn you watched finish gets a check on the
# roster, the strip counts them, and its conversation keeps the done line. New
# work clears the mark until that turn ends too.
Output .work/out/25-done.mp4
Source .work/head.tape
Hide
Source .work/stage.tape
Type "@all we're adding per-key rate limiting to the gateway. maya take the middleware, omar the 429 state in the client, priya a --rate-limit flag, luca the changelog."
Enter
Sleep 2500ms
Show
Sleep 9s

# omar is the one still waiting on you. Approve his write and watch him finish.
Ctrl+X
Sleep 3500ms
Type "a"
Sleep 1200ms
Enter
Sleep 6s

# More work: the check clears while he works, and comes back when he's done.
Type "also handle a 503 the same way"
Sleep 600ms
Enter
Sleep 10s
