# BEAT 26 — a question, answered in place. The room says priya has a question;
# it is answered in her own conversation, and the room's line resolves to the
# answer rather than going stale above a second one. It takes several answers:
# a digit ticks an option's box, and Submit walks on to the review.
Output .work/out/26-question.mp4
Source .work/head.tape
Hide
Source .work/stage.tape
Show
Sleep 1500ms
Type "@priya ship the --rate-limit flag in tonight's release"
Sleep 700ms
Enter
Sleep 5s

# ^X goes to whoever needs you. The question is a card in her pane.
Ctrl+X
Sleep 4s
Type "1"
Sleep 1200ms
Type "2"
Sleep 1500ms
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 1000ms
Enter
Sleep 2500ms

# The review step: every answer about to travel, then Submit.
Enter
Sleep 9s
