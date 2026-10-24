# An API claude cannot reach: what the wire carries and what the OS says

Recorded 2026-10-23 against **Claude Code 2.1.308**, model haiku, for BUG-55 (an agent the network
dropped mid-turn stayed stopped).

## Provenance

- **Headless**, in a sterile `HOME`, with a dummy API key and `ANTHROPIC_BASE_URL` pointing at a host
  that does not resolve, so no request left the machine. One `say hi` turn. Scrubbed by
  `scripts/scrub-fixtures.py`: `testdata/stream/api-error-unreachable.jsonl`.
- **The owner's own transcripts** for how often and in what forms it happens. Aggregates only; no line
  from them is committed.

## The wire

- claude retries the request ten times as `system`/`api_retry` frames (lines 2-11), each with
  `error_status` null and `error` `"unknown"`, backing off from about half a second to about forty
  seconds. The ten delays sum to about 179 seconds.
- Then one synthetic assistant frame (line 12): `is_api_error_message` true, the model `<synthetic>`,
  `error` `"server_error"`, and the text `API Error: Can't reach the API server — check your internet or
  DNS (ENOTFOUND)`. **No HTTP status rides it on the stream.**
- Then a `result` that ended the turn, with `terminal_reason` `api_error`. The process stays up.

## On disk, over 30 days of the owner's sessions

Every failed turn that is not a usage limit or a login, by its `error` and the shape of its text:

- `server_error` with **no** `apiErrorStatus`: can't reach the API server (ENOTFOUND, by far the most),
  the computer went to sleep mid-response, the response stopped arriving, connection dropped
  (ECONNRESET), connection refused (ECONNREFUSED), request timed out, connection lost mid-response, no
  response from the API (waited 9m), connection lost while the computer was asleep.
- `server_error` **with** status 529 once: an overload.
- `invalid_request` (a safeguard), `model_not_found` (404) and `oauth_org_not_allowed` (403): a request
  the API refused, which a retry does not fix.

After each `server_error` the session's next turn was the owner's own: a typed `continue`, sometimes an
hour and a half later, or a background task's notification starting a turn.

## What the OS says

- **macOS routing socket**, read passively on a connected machine for about five minutes: host routes
  added and deleted, and lookup misses. None is an address gained, a link coming up or a gateway
  route, which is what `netwatch_darwin.go` counts. A throwaway build counting every message saw the first in 18 seconds,
  so the kqueue wait and the reads work against the real socket.
- **Linux netlink**, in a container with `NET_ADMIN`: adding an address and dropping and restoring the
  default route each reached the watch; adding a route to a network did not. With addresses and routes
  ignored, a link brought back up still reached it, and the same link going down did not.
- What Wi-Fi rejoining and a wake from sleep send on this machine is a human's check:
  `docs/live-testing.md`, "A lost connection".
