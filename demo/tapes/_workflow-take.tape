# Sourced, never recorded: one fresh take for the workflow scenes (13-18).
#
# Everything a take touches is minted under /tmp: a scratch HOME, a socket
# directory no earlier take has used (the roster lives beside the socket, so a
# reused one inherits the last take's orphans), and the project at /tmp/tally,
# which the banner and status bar draw - so no frame can name this machine.
# The terminal's own variables are dropped so a fresh HOME does not earn the
# first-run terminal-setup notice. WF_BIN, when set, puts another `wake` first
# on PATH - how the before/after stills film `main`'s binary.
#
# The socket path is left in .work/take-socket for record.sh's fleet check.

Set TypingSpeed 1ms
Type `source ./.work/env.sh && take=$(mktemp -d /tmp/wake-take.XXXXXX) && export HOME=$take/home WAKE_SOCKET=$take/wake.sock PATH=${WF_BIN:+$WF_BIN:}$PATH && echo $WAKE_SOCKET > ./.work/take-socket && unset TERM_PROGRAM TERM_PROGRAM_VERSION GHOSTTY_RESOURCES_DIR GHOSTTY_SURFACE_ID GHOSTTY_BIN GHOSTTY_SHELL_FEATURES KITTY_WINDOW_ID WEZTERM_PANE TMUX STY && mkdir -p $HOME/.claude/projects && rm -rf /tmp/tally && mkdir /tmp/tally && cd /tmp/tally && seq 3 > a.txt && seq 5 > b.txt && seq 12 > c.txt && seq 8 > d.txt && git init -q -b main && git -c user.name=tally -c user.email=dev@tally.dev add -A && git -c user.name=tally -c user.email=dev@tally.dev commit -qm init && clear`
Enter
Sleep 2s
