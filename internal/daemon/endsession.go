package daemon

import "github.com/DilanDoshi/wake/internal/rpc"

// endSession queues the graceful stop behind the session's pending input, then
// waits up to quitGrace for Claude to finish. If it does not, killing the
// process group ends Claude and the child processes that stayed in its group.
func (s *server) endSession(c *client, f rpc.Frame) {
	s.withAgent(c, f, func(a *agent) error {
		a.mu.Lock()
		if a.endAsked {
			a.mu.Unlock()
			return nil
		}
		a.endAsked = true
		a.mu.Unlock()
		a.markStopQueued(true)
		s.forgetReady(a.id)
		stop := f
		stop.Kind = rpc.FrameStop
		if err := a.submit(c, stop); err != nil {
			a.markStopQueued(false)
			a.mu.Lock()
			a.endAsked = false
			a.mu.Unlock()
			s.noteReady(a)
			return err
		}
		s.start(func() {
			if len(waitForAgents([]*agent{a}, quitGrace)) == 0 {
				return
			}
			logf("wake: session %s did not end within %v of /quit; ending its process group", a.id, quitGrace)
			a.kill()
		})
		return nil
	})
}
