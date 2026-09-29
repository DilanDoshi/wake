# Sourced, never recorded: one fresh, live take for the @-menu scenes (20-23).
#
# Live, not scripted (CLAUDE.md, 2026-09-28): the agents are the real `claude`,
# which answers every command these scenes send - /list-agents, /rename - as a
# local command, with no login and no model turn. A scratch HOME, a socket no
# earlier take has used, and a /tmp project keep the machine out of every frame;
# PATH is rebuilt to the real claude and this tree's wake alone.
#
# Two idle real sessions, api-refactor and web-polish, stand in for another
# terminal's claude: they register at start with no turn. `sleep 917` holds
# their stdin; after the take, `pkill -f 'sleep 917'` ends both.
#
# `unset WAKE_SOCKET` comes first and the guard refuses a socket that is not
# this take's own - the recorder inherits the operator's real fleet.

Set TypingSpeed 1ms
Type `unset WAKE_SOCKET; real=$(whence -ap claude | grep -v cmux-cli-shims | head -1) && take=$(mktemp -d /tmp/wake-take.XXXXXX) && mkdir -p $take/bin && ln -s $PWD/.work/bin/wake $take/bin/wake && ln -s $real $take/bin/claude && unset -m 'CMUX*' 'CLAUDE*' 'ANTHROPIC*' && export PATH=$take/bin:/usr/bin:/bin:/usr/sbin:/sbin HOME=$take/home WAKE_SOCKET=$take/wake.sock && case $WAKE_SOCKET in /tmp/wake-take.*) ;; *) echo REFUSED; exit 1;; esac && echo $WAKE_SOCKET > ./.work/take-socket && unset TERM_PROGRAM TERM_PROGRAM_VERSION GHOSTTY_RESOURCES_DIR GHOSTTY_SURFACE_ID GHOSTTY_BIN GHOSTTY_SHELL_FEATURES KITTY_WINDOW_ID WEZTERM_PANE TMUX STY && mkdir -p $HOME/.claude/projects && rm -rf /tmp/shop /tmp/shop-api /tmp/shop-web && mkdir -p /tmp/shop/src/cart /tmp/shop/docs /tmp/shop-api /tmp/shop-web && (cd /tmp/shop-api && sleep 917 | claude --print --input-format stream-json --output-format stream-json --verbose --name api-refactor >/dev/null 2>&1 &) && (cd /tmp/shop-web && sleep 917 | claude --print --input-format stream-json --output-format stream-json --verbose --name web-polish >/dev/null 2>&1 &) && cd /tmp/shop && echo "package cart" > src/cart/checkout.go && echo "package cart" > src/cart/cart_test.go && echo "# Checkout" > docs/checkout-notes.md && echo "# Shop" > README.md && git init -q -b main && git -c user.name=shop -c user.email=dev@shop.dev add -A && git -c user.name=shop -c user.email=dev@shop.dev commit -qm init && clear`
Enter
Sleep 3s
