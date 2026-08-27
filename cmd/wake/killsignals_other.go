//go:build !unix

package main

import "os"

// killSignals off unix, where SIGHUP and SIGQUIT do not exist. Wake is a unix
// program - this exists so the cross-compile step keeps compiling the rest of
// the tree, which is the same reason bangproc_other.go does.
var killSignals = []os.Signal{os.Interrupt}
