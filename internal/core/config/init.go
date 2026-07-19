package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/package-register/mocode/internal/util/fsext"
)

func Init(workingDir string, debug bool) (*ConfigStore, error) {
	store, err := Load(workingDir, debug)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func ProjectNeedsInitialization(store *ConfigStore) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("config not loaded")
	}

	someContextFileExists, err := contextPathsExist(store.WorkingDir())
	if err != nil {
		return false, fmt.Errorf("failed to check for context files: %w", err)
	}
	if someContextFileExists {
		return false, nil
	}

	// If the working directory has no non-ignored files, skip initialization step
	empty, err := dirHasNoVisibleFiles(store.WorkingDir())
	if err != nil {
		return false, fmt.Errorf("failed to check if directory is empty: %w", err)
	}
	if empty {
		return false, nil
	}

	return false, nil
}

func contextPathsExist(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}

	// Create a slice of lowercase filenames for lookup with slices.Contains
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, strings.ToLower(entry.Name()))
		}
	}

	// Check if any of the default context paths exist in the directory
	for _, path := range defaultContextPaths {
		// Extract just the filename from the path
		_, filename := filepath.Split(path)
		filename = strings.ToLower(filename)

		if slices.Contains(files, filename) {
			return true, nil
		}
	}

	return false, nil
}

// dirHasNoVisibleFiles returns true if the directory has no files/dirs after applying ignore rules.
func dirHasNoVisibleFiles(dir string) (bool, error) {
	files, _, err := fsext.ListDirectory(dir, nil, 1, 1)
	if err != nil {
		return false, err
	}
	return len(files) == 0, nil
}

func MarkProjectInitialized(store *ConfigStore) error {
	if store == nil {
		return fmt.Errorf("config not loaded")
	}
	return nil
}

func HasInitialDataConfig(store *ConfigStore) bool {
	if store == nil {
		return false
	}
	cfgPath := GlobalConfigData()
	if _, err := os.Stat(cfgPath); err != nil {
		return false
	}
	return store.Config().IsConfigured()
}
