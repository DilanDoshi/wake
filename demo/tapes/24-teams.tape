# BEAT 24 — teams. /team files an agent under a name you choose; the roster
# sections itself by team, and @team addresses every live member at once.
Output .work/out/24-teams.mp4
Source .work/head.tape
Hide
Source .work/stage.tape
Show
Sleep 1500ms
Type "/team @maya backend"
Sleep 500ms
Enter
Sleep 1800ms
Type "/team @nora backend"
Sleep 400ms
Enter
Sleep 1500ms
Type "/team @alex backend"
Sleep 400ms
Enter
Sleep 2s

# The rest of the fleet is sorted off camera; the roster's sections are the point.
Hide
Set TypingSpeed 8ms
Type "/team @omar frontend"
Enter
Sleep 900ms
Type "/team @priya frontend"
Enter
Sleep 900ms
Type "/team @luca docs"
Enter
Sleep 1500ms
Set TypingSpeed 55ms
Show
Sleep 2500ms

# One message, three agents: only backend's live members answer.
Type "@backend the limiter ships tomorrow, run your suites against staging and flag anything flaky."
Sleep 800ms
Enter
Sleep 16s
