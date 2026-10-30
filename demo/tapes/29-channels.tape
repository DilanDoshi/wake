# BEAT 29 — channels. A roster team header is a channel: ↵ on it makes that
# team's thread the room's base, titled with the team, until `── all ──` or
# another team is picked. The roster walks on ⇧↑↓, sent here as Ctrl+G/Ctrl+K
# through shiftkeys.py (setup.sh's env.sh).
Output .work/out/29-channels.mp4
Source .work/head.tape
Hide
Source .work/stage.tape
Source tapes/_teams.tape
Type "@all we're adding per-key rate limiting to the gateway. maya take the middleware, omar the 429 state in the client, priya a --rate-limit flag, luca the changelog."
Enter
Sleep 12s
Show
Sleep 2s

# The roster's cursor starts on maya; up one is backend's header, and ↵ makes
# it the channel: the room keeps backend's three and the manager's seat, and
# drops everyone else.
Ctrl+K
Sleep 1200ms
Enter
Sleep 3500ms

# A team send, answered inside the channel.
Type "@backend the limiter ships tomorrow, run your suites against staging and flag anything flaky."
Sleep 800ms
Enter
Sleep 11s

# Another team's channel, then everyone again.
Ctrl+G
Sleep 500ms
Ctrl+G
Sleep 500ms
Ctrl+G
Sleep 500ms
Ctrl+G
Sleep 1200ms
Enter
Sleep 4s
Ctrl+K
Sleep 400ms
Ctrl+K
Sleep 400ms
Ctrl+K
Sleep 400ms
Ctrl+K
Sleep 400ms
Ctrl+K
Sleep 400ms
Ctrl+K
Sleep 1200ms
Enter
Sleep 3s
