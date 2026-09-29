# @-menu scene 1: a peer, then the machine's other Claude sessions. Live.
#
# maya and otto are the fleet; api-refactor and web-polish are real idle claude
# sessions another terminal would run. A real /list-agents opens the scene (an
# agent's first input is what makes claude send its init). In maya's
# conversation `@o` offers the fleet's otto at once; `@a` offers api-refactor
# with its directory, from the daemon's bare one-shot `claude` a moment after
# the menu opens. ⇥ inserts the name, never the tag.
Output .work/out/20-at-peers.mp4
Source .work/head.tape
Hide
Source tapes/_atmenu-take.tape
Set TypingSpeed 8ms
Type "wake new maya"
Enter
Sleep 8s
Type "/new otto in /tmp/shop"
Enter
Sleep 7s
# `/new` leaves the keys in the room; two steps of ⇥ bring them to maya's pane.
Tab
Sleep 500ms
Tab
Sleep 1s
Type "/list-agents"
Enter
Sleep 5s
Show
Sleep 1500ms
Set TypingSpeed 200ms
Type "@o"
Sleep 2500ms
Backspace 2
Sleep 600ms
Type "@a"
Sleep 4s
Tab
Sleep 1500ms
Set TypingSpeed 45ms
Type "is the refactor done? maya needs the cart types"
Sleep 3s
