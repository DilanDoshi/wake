# BEAT 32 — a friend on the usage bar. A bare /friends opens a picker over the
# composer; its cursor previews each friend in the roster's corner, and ↵ keeps
# one. VHS draws no Kitty images, so these are the half-block sprites
# (WAKE_FRIEND_ART=blocks, setup.sh's env.sh), which do not animate. page.py
# cuts the window's bottom out of the frame (pagecopy.FRIENDS).
Output .work/out/32-friends.mp4
Source .work/head.tape
# Fewer captures keep the take true to time on a loaded machine; a menu that
# moves every two seconds loses nothing at 12.
Set Framerate 12
Hide
Source .work/stage.tape
Show
Sleep 1500ms
Type "/friends"
Sleep 600ms
Enter
Sleep 2500ms
Down
Sleep 2000ms
Down
Sleep 2000ms
Down
Sleep 2000ms
Up
Sleep 1500ms
Enter
Sleep 3s
