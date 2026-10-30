# Sourced, never recorded: one fresh take for the workflow scenes (13-18).
#
# Everything a take touches is minted under /tmp: a scratch HOME, a socket
# directory no earlier take has used (the roster lives beside the socket, so a
# reused one inherits the last take's orphans), and the project inside that
# HOME, which the banner and status bar draw as ~/tally - so no frame can name
# this machine, and no take removes a directory another one made. The socket
# sits in a directory named tally, which names the manager: manager-tally.
# HOME is the take's physical path: /tmp is a link, claude reports where it
# really runs, and only a directory under HOME as spelled is drawn as ~.
# WF_BIN, when set, puts another `wake` first on PATH - how the before/after
# stills film `main`'s binary.
#
# The socket path is left in .work/take-socket for record.sh's fleet check.

Set TypingSpeed 1ms
Type `source ./.work/env.sh && take=$(mktemp -d /tmp/wake-take.XXXXXX) && mkdir $take/tally && export HOME=$(cd -P $take && pwd)/home WAKE_SOCKET=$take/tally/wake.sock PATH=${WF_BIN:+$WF_BIN:}$PATH && echo $WAKE_SOCKET > ./.work/take-socket && mkdir -p $HOME/.claude/projects $HOME/tally && cd $HOME/tally && seq 3 > a.txt && seq 5 > b.txt && seq 12 > c.txt && seq 8 > d.txt && git init -q -b main && git -c user.name=tally -c user.email=dev@tally.dev add -A && git -c user.name=tally -c user.email=dev@tally.dev commit -qm init && clear || exit 1`
Enter
Sleep 2s
