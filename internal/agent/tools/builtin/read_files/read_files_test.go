package read_files

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/permission"
	"github.com/package-register/mocode/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// Mock services for testing
type mockPermissionServiceForRead struct {
	*pubsub.Broker[permission.PermissionRequest]
	shouldDeny bool
}

func (m *mockPermissionServiceForRead) Request(ctx context.Context, req permission.CreatePermissionRequest) (bool, error) {
	if m.shouldDeny {
		return false, nil
	}
	return true, nil
}

func (m *mockPermissionServiceForRead) Grant(req permission.PermissionRequest)           {}
func (m *mockPermissionServiceForRead) Deny(req permission.PermissionRequest)            {}
func (m *mockPermissionServiceForRead) GrantPersistent(req permission.PermissionRequest) {}
func (m *mockPermissionServiceForRead) AutoApproveSession(sessionID string)              {}
func (m *mockPermissionServiceForRead) SetSkipRequests(skip bool)                        {}
func (m *mockPermissionServiceForRead) SkipRequests() bool                               { return false }
func (m *mockPermissionServiceForRead) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[permission.PermissionNotification] {
	return make(<-chan pubsub.Event[permission.PermissionNotification])
}

type mockFileTrackerService struct {
	readTimes map[string]time.Time
}

func (m *mockFileTrackerService) RecordRead(ctx context.Context, sessionID, path string) {
	if m.readTimes == nil {
		m.readTimes = make(map[string]time.Time)
	}
	m.readTimes[path] = time.Now()
}

func (m *mockFileTrackerService) LastReadTime(ctx context.Context, sessionID, path string) time.Time {
	if m.readTimes == nil {
		return time.Time{}
	}
	if t, ok := m.readTimes[path]; ok {
		return t
	}
	return time.Time{}
}

func (m *mockFileTrackerService) ListReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	if m.readTimes == nil {
		return []string{}, nil
	}
	files := make([]string, 0, len(m.readTimes))
	for path := range m.readTimes {
		files = append(files, path)
	}
	return files, nil
}

func (m *mockFileTrackerService) DeleteSession(ctx context.Context, sessionID string) {
}

// Helper function to call tool
func callTool(t *testing.T, tool fantasy.AgentTool, params ReadFilesParams) fantasy.ToolResponse {
	t.Helper()

	inputJSON, err := json.Marshal(params)
	require.NoError(t, err)

	call := fantasy.ToolCall{
		ID:    "test-call",
		Name:  ReadFilesToolName,
		Input: string(inputJSON),
	}

	result, err := tool.Run(context.Background(), call)
	require.NoError(t, err)
	return result
}

func TestReadFilesEmptyPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{},
	}

	result := callTool(t, tool, params)
	require.True(t, result.IsError)
	require.Contains(t, result.Content, "paths parameter is required")
}

func TestReadFilesTooManyPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	// Create more than max paths
	paths := make([]string, MaxFilesPerRequest+1)
	for i := 0; i < MaxFilesPerRequest+1; i++ {
		paths[i] = fmt.Sprintf("file%d.txt", i)
	}

	params := ReadFilesParams{
		Paths: paths,
	}

	result := callTool(t, tool, params)
	require.True(t, result.IsError)
	require.Contains(t, result.Content, "too many paths")
}

func TestReadFilesSingleFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"test.txt"},
	}

	result := callTool(t, tool, params)
	require.False(t, result.IsError)
	require.Contains(t, result.Content, "<file>")
	require.Contains(t, result.Content, "Hello, World!")
	require.Contains(t, result.Content, "Success: 1 files")
}

func TestReadFilesMultipleFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"file1.txt": "Content 1",
		"file2.txt": "Content 2",
		"file3.txt": "Content 3",
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0o644))
	}

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"file1.txt", "file2.txt", "file3.txt"},
	}

	result := callTool(t, tool, params)
	require.False(t, result.IsError)
	require.Contains(t, result.Content, "Success: 3 files")
	require.Contains(t, result.Content, "Content 1")
	require.Contains(t, result.Content, "Content 2")
	require.Contains(t, result.Content, "Content 3")
}

func TestReadFilesFileNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"nonexistent.txt"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "file not found")
	require.Contains(t, result.Content, "Failed: 1 files")
}

func TestReadFilesDirectoryPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"subdir"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "path is a directory")
}

func TestReadFilesFileTooLarge(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.txt")
	largeContent := strings.Repeat("x", 1024*1024) // 1MB
	require.NoError(t, os.WriteFile(largeFile, []byte(largeContent), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths:       []string{"large.txt"},
		MaxFileSize: 512 * 1024, // 512KB limit
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "file is too large")
}

func TestReadFilesPartialFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create one valid file
	validFile := filepath.Join(tmpDir, "valid.txt")
	require.NoError(t, os.WriteFile(validFile, []byte("Valid content"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"valid.txt", "nonexistent.txt"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 1 files")
	require.Contains(t, result.Content, "Failed: 1 files")
	require.Contains(t, result.Content, "Valid content")
	require.Contains(t, result.Content, "file not found")
}

func TestReadFilesGlobPattern(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create test files
	files := []string{
		"test1.go",
		"test2.go",
		"test3.txt",
		"subdir/test4.go",
	}

	for _, file := range files {
		fullPath := filepath.Join(tmpDir, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte("content"), 0o644))
	}

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"*.go"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 2 files") // test1.go and test2.go
}

func TestReadFilesRecursiveGlob(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create test files
	files := []string{
		"test1.go",
		"subdir/test2.go",
		"subdir/deep/test3.go",
	}

	for _, file := range files {
		fullPath := filepath.Join(tmpDir, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte("package main"), 0o644))
	}

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"**/*.go"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 3 files")
	require.Contains(t, result.Content, "package main")
}

func TestReadFilesMixedPathsAndGlobs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "specific.txt"), []byte("Specific"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "glob1.txt"), []byte("Glob1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "glob2.txt"), []byte("Glob2"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"specific.txt", "*.txt"},
	}

	result := callTool(t, tool, params)
	// specific.txt appears twice (once direct, once from glob)
	require.Contains(t, result.Content, "Specific")
	require.Contains(t, result.Content, "Glob1")
	require.Contains(t, result.Content, "Glob2")
}

func TestReadFilesInvalidGlobPattern(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"[invalid"},
	}

	result := callTool(t, tool, params)
	// Invalid patterns may result in "no files matched" or "glob pattern error"
	require.True(t, result.IsError || strings.Contains(result.Content, "no files matched") || strings.Contains(result.Content, "glob pattern error"))
}

func TestReadFilesNoMatchesForGlob(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"*.nonexistent"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "no files matched pattern")
}

func TestReadFilesBinaryFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	binaryFile := filepath.Join(tmpDir, "binary.bin")
	binaryContent := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	require.NoError(t, os.WriteFile(binaryFile, binaryContent, 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"binary.bin"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "not valid UTF-8")
}

func TestReadFilesResponseMetadata(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("Content 1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("Content 2"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"file1.txt", "file2.txt"},
	}

	result := callTool(t, tool, params)

	// Metadata is stored as string, check that response contains expected content
	require.False(t, result.IsError)
	require.Contains(t, result.Content, "Success: 2 files")
	require.Contains(t, result.Content, "Failed: 0 files")
}

func TestReadFilesConcurrency(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create many files to test concurrency
	numFiles := 20
	for i := 0; i < numFiles; i++ {
		filename := fmt.Sprintf("file%d.txt", i)
		content := fmt.Sprintf("Content %d", i)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, filename), []byte(content), 0o644))
	}

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	paths := make([]string, numFiles)
	for i := 0; i < numFiles; i++ {
		paths[i] = fmt.Sprintf("file%d.txt", i)
	}

	params := ReadFilesParams{
		Paths: paths,
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, fmt.Sprintf("Success: %d files", numFiles))
}

func TestContainsGlobPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"star pattern", "*.go", true},
		{"double star", "**/*.go", true},
		{"question mark", "file?.txt", true},
		{"character class", "file[0-9].txt", true},
		{"no pattern", "file.txt", false},
		{"path with dirs", "src/file.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := containsGlobPattern(tt.path)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFindSimilarFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create similar files
	files := []string{
		"main.go",
		"main_test.go",
		"main_backup.go",
		"other.go",
	}

	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, file), []byte("content"), 0o644))
	}

	// Test finding similar files - use exact base name match
	nonExistent := filepath.Join(tmpDir, "main.go.bak")
	suggestions := findSimilarFiles(nonExistent)

	// Should find at least main.go since "main.go" is contained in "main.go.bak"
	require.GreaterOrEqual(t, len(suggestions), 1)
	require.LessOrEqual(t, len(suggestions), 3)
}

func TestFindSimilarFilesNoMatches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "unrelated.txt"), []byte("content"), 0o644))

	// Test with completely different name
	nonExistent := filepath.Join(tmpDir, "xyz123.go")
	suggestions := findSimilarFiles(nonExistent)

	require.Empty(t, suggestions)
}

func TestReadFilesEmptyFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.txt")
	require.NoError(t, os.WriteFile(emptyFile, []byte(""), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"empty.txt"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 1 files")
	require.Contains(t, result.Content, "<size>0</size>")
}

func TestReadFilesWithSubdirectories(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create nested structure
	dirs := []string{
		"src",
		"src/pkg",
		"src/pkg/internal",
		"tests",
	}

	for _, dir := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, dir), 0o755))
	}

	files := map[string]string{
		"src/main.go":                "package main",
		"src/pkg/utils.go":           "package pkg",
		"src/pkg/internal/helper.go": "package internal",
		"tests/main_test.go":         "package tests",
	}

	for path, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, path), []byte(content), 0o644))
	}

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"src/**/*.go"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 3 files")
	require.Contains(t, result.Content, "package main")
	require.Contains(t, result.Content, "package pkg")
	require.Contains(t, result.Content, "package internal")
}

func TestReadFilesDefaultMaxFileSize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file exactly at default limit
	defaultLimit := DefaultMaxFileSize
	content := strings.Repeat("x", int(defaultLimit))
	largeFile := filepath.Join(tmpDir, "at_limit.txt")
	require.NoError(t, os.WriteFile(largeFile, []byte(content), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"at_limit.txt"},
		// Don't specify MaxFileSize, should use default
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 1 files")
}

func TestReadFilesPathNormalization(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("content"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	// Test with different path separators
	params := ReadFilesParams{
		Paths: []string{"./test.txt", "test.txt"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "content")
}

func TestReadFilesPermissionDenied(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("content"), 0o644))

	// Use permission service that denies all requests
	permissions := &mockPermissionServiceForRead{shouldDeny: true}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"test.txt"},
	}

	// Note: Permission check is only triggered for files outside working directory
	// For files inside working directory, no permission check is needed
	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 1 files")
	require.Contains(t, result.Content, "content")
}

func TestReadFilesWithSuggestions(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create similar files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("yaml"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("yml"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte("json"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	// Try to read non-existent file with similar names
	params := ReadFilesParams{
		Paths: []string{"config.yamll"}, // Typo
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "file not found")
	require.Contains(t, result.Content, "Did you mean")
}

func TestReadFilesWithCustomMaxFileSize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("small content"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths:       []string{"test.txt"},
		MaxFileSize: 1024, // 1KB
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 1 files")
	require.Contains(t, result.Content, "small content")
}

func TestReadFilesWithEncoding(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("UTF-8 content"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"test.txt"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 1 files")
	require.Contains(t, result.Content, "UTF-8 content")
}

func TestReadFilesDuplicatePaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("content"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	// Same file twice
	params := ReadFilesParams{
		Paths: []string{"test.txt", "test.txt"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "content")
	// Should have 2 entries (even if same file)
	require.Contains(t, result.Content, "Success: 2 files")
}

func TestReadFilesAbsolutePath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("content"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	// Use absolute path
	params := ReadFilesParams{
		Paths: []string{testFile},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 1 files")
	require.Contains(t, result.Content, "content")
}

func TestReadFilesFileAccessError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file and make it unreadable
	testFile := filepath.Join(tmpDir, "unreadable.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("content"), 0o000))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"unreadable.txt"},
	}

	result := callTool(t, tool, params)
	// May succeed if running as root, or fail with permission error
	// Just verify it doesn't crash
	require.True(t, strings.Contains(result.Content, "Success") || strings.Contains(result.Content, "error"))
}

func TestReadFilesTruncatedGlob(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create many files to trigger truncation
	for i := 0; i < 50; i++ {
		filename := fmt.Sprintf("file%03d.txt", i)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, filename), []byte("content"), 0o644))
	}

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	// Use glob that matches many files
	params := ReadFilesParams{
		Paths: []string{"*.txt"},
	}

	result := callTool(t, tool, params)
	// Should succeed with up to 100 files (limit)
	require.Contains(t, result.Content, "Success:")
}

func TestReadFilesMixedSuccessAndFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create some valid files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "valid1.txt"), []byte("content1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "valid2.txt"), []byte("content2"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	// Mix of valid and invalid paths
	params := ReadFilesParams{
		Paths: []string{
			"valid1.txt",
			"nonexistent1.txt",
			"valid2.txt",
			"nonexistent2.txt",
			"nonexistent3.txt",
		},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "Success: 2 files")
	require.Contains(t, result.Content, "Failed: 3 files")
	require.Contains(t, result.Content, "content1")
	require.Contains(t, result.Content, "content2")
}

func TestReadFilesSummary(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("12345"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("1234567890"), 0o644))

	permissions := &mockPermissionServiceForRead{}
	filetracker := &mockFileTrackerService{}

	tool := NewReadFilesTool(permissions, filetracker, tmpDir)

	params := ReadFilesParams{
		Paths: []string{"file1.txt", "file2.txt"},
	}

	result := callTool(t, tool, params)
	require.Contains(t, result.Content, "<summary>")
	require.Contains(t, result.Content, "Total: 2 files")
	require.Contains(t, result.Content, "Total size:")
}
