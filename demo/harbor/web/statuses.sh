#!/bin/sh
# Every status the gateway sends needs its own case in messageFor.
fail=0
for status in 401 429 500; do
  if grep -q "case $status:" errors.ts; then
    echo "ok    $status"
  else
    echo "FAIL  $status  no case in messageFor, so the raw message shows"
    fail=1
  fi
done
exit $fail
