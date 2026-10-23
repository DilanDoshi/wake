//go:build !darwin && !linux

package daemon

import "time"

// watchRoutes has no kernel watch to wait on here, so only another agent's turn
// going through resumes an agent an unreachable API stopped.
func watchRoutes() (<-chan time.Time, func()) { return nil, func() {} }
