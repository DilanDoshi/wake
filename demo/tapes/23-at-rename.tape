# @-menu scene 4: /name keeps claude's own name in step. Live.
#
# `/name @maya buyer` in the room renames her in Wake and, once she is idle,
# sends her real claude a bare /rename. Then in otto's conversation a real
# /list-agents - a passthrough - lists `buyer`, not `maya`.
Output .work/out/23-at-rename.mp4
Source .work/head.tape
Hide
Source tapes/_atmenu-take.tape
Set TypingSpeed 8ms
Type "wake new otto"
Enter
Sleep 8s
Type "/new maya in /tmp/shop"
Enter
Sleep 7s
Show
Sleep 1500ms
Set TypingSpeed 60ms
# `/new` drafted @maya in the room; the rename replaces it.
Ctrl+U
Type "/name @maya buyer"
Sleep 500ms
Enter
Sleep 5s
# Two steps of ⇥ bring the keys to otto's conversation.
Tab
Sleep 500ms
Tab
Sleep 1s
Type "/list-agents"
Sleep 500ms
Enter
Sleep 6s
