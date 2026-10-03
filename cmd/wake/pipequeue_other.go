//go:build !unix

package main

import "os"

// queued has no FIONREAD to ask off unix, so a read that filled its room keeps
// chunker's own assumption: more is coming.
func queued(*os.File) bool { return true }
