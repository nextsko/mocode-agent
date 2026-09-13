package shell

import (
	"os"
	"runtime"
	"strconv"
)

// envBool reports the boolean value of the named environment variable, or def
// when the variable is unset or unparseable.
func envBool(key string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

// goCoreUtilsEnabled reports whether the upstream Go coreutils middleware is
// active. It is read lazily (rather than in init) so tests can override it with
// t.Setenv("MOCODE_CORE_UTILS", ...). Defaults to Windows-only.
func goCoreUtilsEnabled() bool {
	return envBool("MOCODE_CORE_UTILS", runtime.GOOS == "windows")
}

// pipeUtilsEnabled reports whether this package's stream-utility middleware
// (head, tail, wc, tee, sort, uniq, cut, tr) is active. It defaults to the same
// platform rule as goCoreUtilsEnabled (Windows-only) and can be overridden with
// MOCODE_PIPE_UTILS.
func pipeUtilsEnabled() bool {
	return envBool("MOCODE_PIPE_UTILS", runtime.GOOS == "windows")
}
