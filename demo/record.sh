#!/usr/bin/env bash
# Records a beat and checks the fleet actually came up before keeping the take.
#
# Staging types `/new` six times into a live TUI, and under load a spawn that
# has not landed leaves the next line concatenating onto a refused draft — so a
# take can come out with three agents instead of seven and look fine until you
# watch it. The daemon outlives the recording, so the count is checkable.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
want=${WANT_AGENTS:-7}
tries=${TRIES:-3}

for tape in "$@"; do
  name=$(basename "$tape" .tape)
  for attempt in $(seq 1 "$tries"); do
    rm -f "$here/.work/take-socket"
    vhs "$tape" >/dev/null 2>&1 || { echo "$name: vhs failed"; exit 1; }

    # shellcheck disable=SC1091
    source "$here/.work/env.sh"
    # A workflow scene mints a fresh socket per take (tapes/_workflow-take.tape)
    # and leaves its path behind; its fleet is checked there, then ended, since
    # nothing will ever reuse that socket to stop it.
    fresh=""
    [ -f "$here/.work/take-socket" ] && fresh=$(cat "$here/.work/take-socket")
    [ -n "$fresh" ] && export WAKE_SOCKET="$fresh"
    got=$("$here/.work/bin/wake" status 2>/dev/null | grep -c ' <> ' || true)
    [ -n "$fresh" ] && "$here/.work/bin/wake" stop >/dev/null 2>&1 || true

    if [ "$got" -ge "$want" ]; then
      echo "$name: ok ($got agents)"
      break
    fi
    echo "$name: only $got of $want agents staged — retaking ($attempt/$tries)"
    if [ "$attempt" = "$tries" ]; then
      echo "$name: FAILED to stage a full fleet"; exit 1
    fi
  done
done
