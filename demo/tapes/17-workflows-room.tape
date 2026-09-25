# Workflow scene 5: /workflows in the room lists every agent's runs.
#
# iris's run finished, juno's failed, otto's is still going; the room's view
# groups them under each agent's name and opens any one of them.
Output .work/out/17-workflows-room.mp4
Source .work/head.tape
Hide
Source tapes/_workflow-take.tape
Set TypingSpeed 8ms
Type "wake new iris"
Enter
Sleep 7s
Ctrl+W
Sleep 1s
Type "/new juno in /tmp/tally"
Enter
Sleep 6s
Escape
Sleep 500ms
Type "/new otto in /tmp/tally"
Enter
Sleep 6s
Escape
Sleep 500ms
Type "@iris count the lines in every .txt file here"
Enter
Sleep 500ms
Type "@juno migrate the fixtures to v2"
Enter
Sleep 14s
Type "@otto lint every package"
Enter
Sleep 6s
Show
Sleep 2s
Set TypingSpeed 60ms
Type "/workflows"
Sleep 400ms
Enter
Sleep 4s
Down
Sleep 900ms
Down
Sleep 1200ms
Enter
Sleep 4s
Escape
Sleep 2000ms
Up
Sleep 1200ms
Enter
Sleep 4s
Escape
Sleep 1500ms
Escape
Sleep 2s
