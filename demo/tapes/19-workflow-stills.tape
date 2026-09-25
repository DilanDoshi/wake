# The before/after stills for the PR: the same fleet, mid-run, filmed once with
# `main`'s binary and once with this branch's. Run it twice:
#
#   WF_BIN=<dir holding main's wake> vhs tapes/19-workflow-stills.tape   # before
#   vhs tapes/19-workflow-stills.tape                                   # after
#
# and move the PNGs aside between runs - VHS takes no variable in a path.
# Only the branch has a row to open, so only its view still means anything.
Output .work/out/19-workflow-stills.mp4
Source .work/head.tape
Hide
Source tapes/_workflow-take.tape
Set TypingSpeed 8ms
Type "wake new iris"
Enter
Sleep 7s
Tab
Sleep 1s
Type "@iris count the lines in every .txt file here, take your time"
Enter
Sleep 12s
Show
Sleep 800ms
Screenshot .work/out/stills-fleet.png
Sleep 1s
Set TypingSpeed 40ms
Type "/board"
Enter
Sleep 1200ms
Down
Sleep 800ms
Enter
Sleep 2500ms
Screenshot .work/out/stills-view.png
Sleep 500ms
