package shell

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"mvdan.cc/sh/v3/interp"
)

const sleepUsage = `sleep - delay for a specified amount of time

Usage:
  sleep NUMBER[SUFFIX]...

SUFFIX may be:
  s  seconds (default)
  m  minutes
  h  hours
  d  days

NUMBER need not be an integer. Multiple arguments are summed.
`

// handleSleep implements the POSIX sleep command for cross-platform support
// (notably Windows, where no native sleep binary is available).
func handleSleep(ctx context.Context, args []string, stderr io.Writer) error {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "sleep: missing operand")
		fmt.Fprint(stderr, sleepUsage)
		return interp.NewExitStatus(1)
	}

	var total time.Duration
	for _, arg := range args[1:] {
		if arg == "--help" {
			fmt.Fprint(stderr, sleepUsage)
			return nil
		}
		d, err := parseSleepDuration(arg)
		if err != nil {
			fmt.Fprintf(stderr, "sleep: invalid time interval %q\n", arg)
			return interp.NewExitStatus(1)
		}
		total += d
	}

	select {
	case <-time.After(total):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// parseSleepDuration parses a duration string in the form "N[SUFFIX]"
// where SUFFIX is one of s, m, h, d (defaulting to seconds).
func parseSleepDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	mult := time.Second
	last := s[len(s)-1]
	switch last {
	case 's':
		mult = time.Second
		s = strings.TrimSuffix(s, "s")
	case 'm':
		mult = time.Minute
		s = strings.TrimSuffix(s, "m")
	case 'h':
		mult = time.Hour
		s = strings.TrimSuffix(s, "h")
	case 'd':
		mult = 24 * time.Hour
		s = strings.TrimSuffix(s, "d")
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, fmt.Errorf("negative duration")
	}
	return time.Duration(v * float64(mult)), nil
}
