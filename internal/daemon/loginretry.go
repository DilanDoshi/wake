package daemon

// Retrying a login's resume that reached a process still on the old login
// (BUG-64, the owner's 2026-11-07 go).
//
// Claude's config file names the new account before every running claude has
// taken the new login: a resume sent the moment the account changes can find the
// login not written yet ("Not logged in", 2026-10-21) or the process still
// holding the old account's token (its limit again, 2026-11-06). Either way the
// turn fails for want of time, not because the new account is limited, so the
// daemon tries again a few times on its one timer before leaving the stop to its
// reset or the next login. decisions.md (2026-11-07) holds the numbers' evidence.

import (
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// loginRetries are the waits before each retry, after the failure before it:
// past the ten seconds a process held the old login on 2026-11-06, then a
// minute, then five - within the six minutes a hand-sent continue took to work.
// A var only so tests can run them at their own pace.
var loginRetries = []time.Duration{15 * time.Second, time.Minute, 5 * time.Minute}

// loginTry is a resume a login made: the reset of the stop it answered, and how
// many retries came before it. The zero value is any other resume.
type loginTry struct {
	made  bool
	reset time.Time
	tries int
}

// failed is the stop a login's resume leaves when its turn fails the way a
// process still on the old login fails - "Not logged in", or the limit it
// answered, naming the same reset - with the next retry armed while any is left.
// The stop's account is the one signed in now, so only a further login resumes
// it early. Any other failure is the turn's own.
func (l loginTry) failed(ev core.Event, now time.Time, account core.Account) (usageStop, bool) {
	if !l.made {
		return usageStop{}, false
	}
	switch ev.Notice {
	case core.NoticeAPIError:
	case core.NoticeUsageLimit:
		if !limitReset(ev.Text, now).Equal(l.reset) {
			return usageStop{}, false // the new account's own limit
		}
	default:
		return usageStop{}, false
	}
	stop := usageStop{at: now, account: account, tries: l.tries + 1}
	if l.reset.After(now) {
		stop.reset = l.reset
	}
	if stop.tries <= len(loginRetries) {
		stop.retry = now.Add(loginRetries[stop.tries-1])
	}
	return stop, true
}
