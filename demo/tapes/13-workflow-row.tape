# Workflow scene 1: a workflow starts and its sidebar row fills in.
#
# iris runs `count-lines` (demo/scenarios/iris.json): four counting agents in
# parallel, then a sum. The row `⎿ ◈ count-lines d/n` counts agents done over
# agents started, and leaves when the run ends - which lands a green
# `● Workflow "count-lines" finished` in the conversation *and* the room.
Output .work/out/13-workflow-row.mp4
Source .work/head.tape
Hide
Source tapes/_workflow-take.tape
Set TypingSpeed 8ms
Type "wake new iris"
Enter
Sleep 7s
# ⇥ hands the keys to the room, keeping iris's conversation beside it.
Tab
Sleep 1s
Show
Sleep 1500ms
Set TypingSpeed 45ms
Type "@iris count the lines in every .txt file here"
Sleep 500ms
Enter
Sleep 16s
