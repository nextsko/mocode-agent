//go:build !linux

package shell

import (
	"context"
	"fmt"
	"io"
)

func startTTYBackgroundProcess(
	_ context.Context,
	_ string,
	_ []string,
	_ []BlockFunc,
	_ string,
	_ io.Writer,
) (backgroundRunner, error) {
	return nil, fmt.Errorf("tty background jobs are not supported on this platform")
}
