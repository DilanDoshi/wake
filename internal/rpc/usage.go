package rpc

import "time"

// SessionUsage is the signed-in account's five-hour session window as the
// fleet's newest API response reported it - the bar under the roster. It is
// the account's rather than any one session's, so it rides Status.
type SessionUsage struct {
	// Percent is how much of the window is used, rounded to a whole percent.
	Percent int       `json:"percent"`
	Resets  time.Time `json:"resets"`

	// Warned and Limited are Claude's own verdict on the window.
	Warned  bool `json:"warned,omitempty"`
	Limited bool `json:"limited,omitempty"`

	// Lapsed is the reset passing with no reading since: the figure is stale,
	// and the next API response says what replaced it.
	Lapsed bool `json:"lapsed,omitempty"`
}
