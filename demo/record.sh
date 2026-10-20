#!/usr/bin/env bash
# Records a beat and checks the fleet actually came up before keeping the take.
#
# Staging types `/new` six times into a live TUI, and under load a spawn that
# has not landed leaves the next line concatenating onto a refused draft — so a
# take can come out with three agents instead of seven and look fine until you
# watch it. The daemon outlives the recording, so the count is checkable - and
# so it outlives this script unless the script ends it. Every exit does, but a
# SIGKILL: that leaves the take's record, and the next run ends what it names.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
want=${WANT_AGENTS:-7}
tries=${TRIES:-3}

staged=0
owner="$here/.work/record.pid"

# One recorder per checkout: the take record and the staged socket are shared,
# so a second recorder would end the first one's fleet. The claim is a file made
# only if it is not there; one a dead recorder left is taken over.
claim() {
  local pid
  mkdir -p "$here/.work"
  ( set -C; echo $$ > "$owner" ) 2>/dev/null && return 0
  pid=$(cat "$owner" 2>/dev/null) || pid=
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    echo "record.sh: another recording is running in this checkout (pid $pid)" >&2
    return 1
  fi
  rm -f "$owner"
  ( set -C; echo $$ > "$owner" ) 2>/dev/null
}

# The recorder inherits the operator's real fleet, and `wake stop` cannot be
# undone, so a socket is counted or ended only when what it resolves to - not how
# it is spelled - is a scratch one: a wake.sock (no real fleet's is called that)
# in a directory that is really under /tmp/wake-demo or /tmp/wake-take.*.
scratch() {
  local dir tmp
  case $1 in *..*) return 1 ;; esac
  [ "${1##*/}" = wake.sock ] && [ ! -L "$1" ] || return 1
  case $1 in /tmp/wake-demo/*|/tmp/wake-take.*) ;; *) return 1 ;; esac
  dir=$(cd -P "${1%/*}" 2>/dev/null && pwd) || return 0 # no directory, no daemon to reach
  tmp=$(cd -P /tmp && pwd)
  case $dir in "$tmp"/wake-demo|"$tmp"/wake-demo/*|"$tmp"/wake-take.*) return 0 ;; esac
  return 1
}

end_fleet() {
  [ -n "$1" ] || { echo "record.sh: no socket to end a fleet on" >&2; return 1; }
  scratch "$1" || { echo "record.sh: not ending a fleet on $1: not a scratch socket" >&2; return 1; }
  WAKE_SOCKET=$1 "$here/.work/bin/wake" stop >/dev/null 2>&1 || { echo "record.sh: could not end the fleet on $1" >&2; return 1; }
}

# The fleet a tape minted a socket for. Its record stays until the fleet is down,
# so a stop that failed is tried again.
end_take() {
  local sock
  [ -f "$here/.work/take-socket" ] || return 0
  sock=$(cat "$here/.work/take-socket") || sock=
  [ -n "$sock" ] || { echo "record.sh: .work/take-socket is empty, so the fleet it should name is not ended" >&2; return 1; }
  end_fleet "$sock" && rm -f "$here/.work/take-socket"
}

# What the last take left. The staged fleet is a socket every checkout shares, so
# it is ended only by a run whose tape staged it. Runs on every exit, and a
# signal that arrives meanwhile must not cut it short.
end_left_fleets() {
  trap '' INT TERM
  local failed=0
  end_take || failed=1
  if [ "$staged" = 1 ] && [ -f "$here/.work/env.sh" ]; then
    # shellcheck disable=SC1091
    end_fleet "$(. "$here/.work/env.sh" && echo "${WAKE_SOCKET:-}")" || failed=1
  fi
  rm -f "$owner"
  [ "$failed" = 0 ] || exit 1
}
# After the claim, so a refused run never ends a fleet it does not own.
claim || exit 1
trap end_left_fleets EXIT
trap 'exit 130' INT TERM

for tape in "$@"; do
  name=$(basename "$tape" .tape)
  for attempt in $(seq 1 "$tries"); do
    end_take || exit 1 # a killed run's fleet, before its record is forgotten
    grep -qE '^[[:space:]]*Source[[:space:]]+(\./)?\.work/stage\.tape' "$tape" 2>/dev/null && staged=1
    vhs "$tape" >/dev/null 2>&1 || { echo "$name: vhs failed"; exit 1; }

    # shellcheck disable=SC1091
    source "$here/.work/env.sh"
    # A workflow scene mints a fresh socket per take (tapes/_workflow-take.tape)
    # and leaves its path behind; its fleet is checked there, then ended, since
    # nothing will ever reuse that socket to stop it.
    fresh=""
    [ -f "$here/.work/take-socket" ] && fresh=$(cat "$here/.work/take-socket")
    if [ -n "$fresh" ]; then
      scratch "$fresh" || { echo "$name: the tape's take socket $fresh is not a scratch socket"; exit 1; }
      export WAKE_SOCKET="$fresh"
    fi
    got=$("$here/.work/bin/wake" status 2>/dev/null | grep -c ' <> ' || true)
    end_take || true # a failure is said, and the exit hook tries again

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
