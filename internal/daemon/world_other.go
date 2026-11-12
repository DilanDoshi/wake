//go:build !unix

package daemon

import (
	"context"
	"errors"
)

func processRows(context.Context) ([]procRow, error) {
	return nil, errors.New("no process listing on this platform")
}
