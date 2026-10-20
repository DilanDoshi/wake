//go:build !darwin && !linux

package daemon

// watchConfig has no kernel watch to wait on here, so a login elsewhere is not
// seen; the reset timer still resumes a limited agent.
func watchConfig(string) (<-chan struct{}, func()) { return nil, func() {} }
