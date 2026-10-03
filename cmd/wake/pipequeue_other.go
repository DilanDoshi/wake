//go:build !unix

package main

import "os"

// queued cannot ask the pipe off unix, so a filled read keeps chunker's own
// assumption that more is coming: no report is de-framed, at the cost of a ⎋
// that exactly ends a filled read waiting for the next byte, as Bubble Tea's does.
func queued(*os.File) bool { return true }
