package daemon

// Resuming the agents an unreachable API stopped (owner's 2026-10-23 ruling).
//
// claude retries a request the API does not answer for about three minutes, then
// ends the turn with a server_error and the agent sits idle. The usage-limit
// resume carries these stops too (usagelimit.go): one "continue" each, once the
// API answers another agent's turn after the stop, or the OS reports a network
// change after it. Both are waits; nothing polls the API.

import "time"

// watchNetwork is the OS's network watch, a var only so a test can stand in for
// a change no test can make the kernel report.
var watchNetwork = watchRoutes

// apiAnswered records when the API answered a turn, from any agent, and wakes
// the resume goroutine while an answer could resume somebody.
func (s *server) apiAnswered(at time.Time) {
	s.limits.mu.Lock()
	defer s.limits.mu.Unlock()
	if at.After(s.limits.answered) {
		s.limits.answered = at
	}
	if s.limits.running && s.limits.unanswered {
		s.nudgeLimits()
	}
}

// resumeUnreachable resumes each ready agent an unreachable API stopped before
// the network last changed, or before the API last answered unless it waits for
// the network alone.
func (s *server) resumeUnreachable(changed time.Time) {
	s.limits.mu.Lock()
	answered := s.limits.answered
	s.limits.mu.Unlock()
	waiting := false
	for a, stop := range s.limited() {
		if !stop.unreachable {
			continue
		}
		switch {
		case stop.ready && stop.at.Before(changed):
			s.resume(a, stop.at, "an unreachable API, on a network change")
		case stop.ready && !stop.netOnly && stop.at.Before(answered):
			s.resume(a, stop.at, "an unreachable API, on another turn going through")
		default:
			waiting = waiting || !stop.netOnly
		}
	}
	s.armAnswers(answered, waiting)
}

// armAnswers says whether an answer could still resume somebody; one that landed
// after the pass read seen wakes the goroutine for another pass.
func (s *server) armAnswers(seen time.Time, waiting bool) {
	s.limits.mu.Lock()
	defer s.limits.mu.Unlock()
	s.limits.unanswered = waiting
	if waiting && s.limits.answered.After(seen) {
		s.nudgeLimits()
	}
}
