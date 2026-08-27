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
    vhs "$tape" >/dev/null 2>&1 || { echo "$name: vhs failed"; exit 1; }

    # shellcheck disable=SC1091
    source "$here/.work/env.sh"
    got=$("$here/.work/bin/wake" status 2>/dev/null | grep -c ' <> ' || true)

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
