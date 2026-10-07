# BEAT 28 - a newer wake. The room asks whether a newer release is out when it
# opens, and again on a keystroke an hour on - never on a timer. Once one is known
# the strip ends with `↑ wake X` for as long as the room runs, after the notice
# line has timed out; upgrading and restarting stay the operator's.
#
# Filmed against the real GitHub with builds stamped as an older release:
#
#   go build -ldflags "-X github.com/DilanDoshi/wake/internal/version.Version=0.1.7" -o <dir>/wake ./cmd/wake
#   WF_BIN=<dir> vhs tapes/28-update-marker.tape
#
# main's binary stamped the same way is the before: the line comes and goes, and
# nothing stays. Move the mp4 aside between runs - VHS takes no variable in a path.
Output .work/out/28-update-marker.mp4
Source .work/head.tape
Hide
Source tapes/_workflow-take.tape
Set TypingSpeed 8ms
Type "wake new juno"
Enter
Sleep 1500ms
Show
# The check answers within a second or two; the line stays up ten seconds.
Sleep 16s
# Long after the line has gone, the room still says a newer release is out.
Set TypingSpeed 45ms
Type "@juno still on 0.1.7 here"
Sleep 800ms
Enter
Sleep 6s
