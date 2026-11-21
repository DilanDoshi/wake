# Usage-limit reset investigation handoff

## Symptom

After an Enterprise Premium usage window reset, three Wake fleets resumed their
limited agents together. The quota appeared exhausted again within about three
seconds, despite normally supporting hours of parallel agent use.

## Current code finding

Each daemon owns an independent usage-limit watcher in
`internal/daemon/usagelimit.go`. When a usage-limit API error arrives, the
daemon records the signed-in Claude account and organization IDs. While any
session is stopped by a limit, it watches Claude's config file. A change causes
`resumeOnLogin` to read the current account and resume every ready limited
session whose recorded `core.Account` differs. The comparison includes both
account and organization IDs. Separately, a timer resumes sessions after the
reset time parsed from Claude's usage-limit error text, with a one-minute
slack.

Because each fleet has its own watcher, one config-file change can trigger a
burst across all three fleets. This is a plausible explanation, but the actual
incident path is unconfirmed: no fleet logs or before/after config identities
have been inspected. A new window reset alone does not prove the config-change
branch fired.

## Evidence to collect

- Logs from all three daemons for the incident interval. Resume logs identify
  the cause as either `on a login to another account` or `on its reset`.
- Number of resume log lines and timestamps per fleet; identify whether they
  cluster within seconds.
- Account and organization IDs from `.claude.json` immediately before and after
  the reset/config rewrite, if available. Do not include OAuth tokens or other
  credentials in the handoff.
- Which Claude Code build and Wake daemon build each fleet was running.
- Whether the usage error supplied a reset time, and whether that time matched
  the Enterprise Premium window reset.

## Investigation questions

1. Did the account or organization UUID actually change during the reset?
2. Did each daemon log a config-change resume, a timer resume, or both?
3. How many limited agents resumed per daemon, and did their prompts trigger
   substantial autonomous work immediately?
4. Did Claude report a fresh usage-limit error on those resumed turns, or did a
   separate account/org-wide limit or concurrent-request limit reject them?
5. Should a config identity change be sufficient to resume every limited
   session, or should it require additional evidence/explicit operator action?

## Relevant implementation and tests

- `internal/daemon/usagelimit.go`: limit capture, account identity comparison,
  file watcher, timer, and one-time resume queueing.
- `internal/daemon/configwatch_darwin.go` and platform siblings: config change
  detection.
- `internal/core/account.go`: account/org identity decoding and reset-text
  parsing.
- `internal/daemon/usagelimit_test.go` and
  `internal/daemon/usagelimit_e2e_test.go`: current unit and fake-process
  coverage, including different-account login and timer paths.

Current tests establish intended mechanics, not the real Enterprise Premium
reset/config behavior. Do not conclude that the config-change path caused this
incident until the logs or identity change corroborate it.

## Local investigation, November 20, 2026

Times are PST. Every automatic usage-limit resume found in the November 20
daemon logs was attributed to an account change or a retry after one. There
were zero `on its reset` entries. The time of the reported three-second incident
was unspecified. The latest documented bursts were:

| Time | sing-dev | sing-dev-2 | wake-dev | Account switch |
| --- | ---: | ---: | ---: | --- |
| 17:50:34 | 2 | 18 | 4 | 9 to 4 |
| 19:00:39 | 3 | 19 | 4 | 4 to 8 |
| 20:58:22 | 4 | 14 | 4 | 8 to 10 |

At 20:58:22, the switcher rewrote Claude's config in the same second as all 22
Wake resume messages. The switcher's saved mapping shows different account
UUIDs and the same organization UUID for accounts 8 and 10. The current
Claude config matches account 10. The retained Claude config backups start
after the switch.

The 22 resumed session IDs produced 135 distinct assistant messages with
model usage in the first two minutes, deduplicated by message ID. Their
reported usage included about 12.0M cache-write input tokens, 46.1M cache-read
input tokens, and 146k output tokens. These fields measure model activity.
The account's starting utilization and the quota charged per response are
unknown. The first local limit error matching the new 1:50 a.m. reset appeared
about 97 seconds after the switch. The switcher's 21:52 cached reading for
account 10 showed 100% five-hour usage, 22% weekly usage, and 2% weekly Fable
usage.

Five standalone Claude terminals were live outside Wake. The Singularity
session `3f93cf13` on `ttys001` (PID 949) made three Fable responses from
21:00:12 to 21:02:05 and hit the same 1:50 a.m. reset at 21:02:06. It was idle
at inspection. The other four were idle or waiting and had no model usage
recorded on November 20. Two local Codex turns used the separately metered
OpenAI provider. The process and transcript audit covers this Mac.

The running Wake daemons reported `0.1.14+fa87e32` for sing-dev,
`0.1.16+aa6f3f0` for sing-dev-2, and `0.1.15+5347ff9` for wake-dev.
Claude Code versions observed in the latest burst were 2.1.314, 2.1.322,
2.1.325, 2.1.328, and 2.1.329.

For a controlled next window, park all three fleets with two presses of
`Ctrl-Q` in each fleet. Resume one named agent and compare current-session
usage before and after one bounded turn. Leave the standalone Claude terminal
idle during the check. No sessions were stopped or resumed during this audit.
