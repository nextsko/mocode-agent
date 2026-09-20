package memory

import (
	"os"
)

// readFileIfExists reads a file, returning (nil, nil) when it is absent. The
// two-outcome return keeps callers from pattern-matching on os.IsNotExist at
// every site.
func readFileIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// statFile is os.Stat, named so the memory package reads uniformly.
func statFile(path string) (os.FileInfo, error) { return os.Stat(path) }
