package errcoll

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewCreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "errors")
	c, err := New(dir)
	require.NoError(t, err)
	defer c.Stop()

	require.DirExists(t, dir)
}

func TestRecordAndStopFlushes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := New(dir)
	require.NoError(t, err)

	c.Record(ErrorRecord{
		ToolName: "fetch",
		Error:    "network unreachable",
		Category: CategoryToolExecution,
	})

	c.Stop()

	entries := readAllRecords(t, dir)
	require.Len(t, entries, 1)
	require.Equal(t, "fetch", entries[0].ToolName)
	require.Equal(t, "network unreachable", entries[0].Error)
	require.Equal(t, CategoryToolExecution, entries[0].Category)
	require.NotZero(t, entries[0].Timestamp)
	require.NotEmpty(t, entries[0].Platform)
}

func TestDailyRotation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := New(dir)
	require.NoError(t, err)
	defer c.Stop()

	day1 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)

	c.Record(ErrorRecord{Timestamp: day1, ToolName: "fetch", Error: "err1", Category: CategoryToolExecution})
	c.Record(ErrorRecord{Timestamp: day2, ToolName: "bash", Error: "err2", Category: CategoryCrossPlatform})

	// Give the goroutine time to process both records.
	time.Sleep(100 * time.Millisecond)

	require.FileExists(t, filepath.Join(dir, "errors-20250101.jsonl"))
	require.FileExists(t, filepath.Join(dir, "errors-20250102.jsonl"))

	day1Records := readRecordsFromFile(t, filepath.Join(dir, "errors-20250101.jsonl"))
	day2Records := readRecordsFromFile(t, filepath.Join(dir, "errors-20250102.jsonl"))

	require.Len(t, day1Records, 1)
	require.Len(t, day2Records, 1)
	require.Equal(t, "fetch", day1Records[0].ToolName)
	require.Equal(t, "bash", day2Records[0].ToolName)
}

func TestContextHelpers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := New(dir)
	require.NoError(t, err)
	defer c.Stop()

	ctx := WithContext(context.Background(), c)
	require.Equal(t, c, FromContext(ctx))

	plain := FromContext(context.Background())
	require.Nil(t, plain)
	require.Nil(t, FromContext(nil))
}

func TestRecordedMarker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	require.False(t, IsRecorded(ctx))

	marked := MarkRecorded(ctx)
	require.True(t, IsRecorded(marked))
}

func TestSynchronousFallbackWhenChannelFull(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := New(dir)
	require.NoError(t, err)

	// Fill the channel without consuming, then record one more. The extra
	// record must still be persisted via synchronous fallback.
	for i := 0; i < channelBuffer; i++ {
		select {
		case c.ch <- ErrorRecord{ToolName: "fill", Error: "fill"}:
		default:
			t.Fatal("channel should not be full yet")
		}
	}

	c.Record(ErrorRecord{ToolName: "overflow", Error: "overflow", Category: CategoryToolExecution})

	c.Stop()

	entries := readAllRecords(t, dir)
	var foundOverflow bool
	for _, e := range entries {
		if e.ToolName == "overflow" {
			foundOverflow = true
			break
		}
	}
	require.True(t, foundOverflow, "overflow record should have been written synchronously")
}

func readAllRecords(t *testing.T, dir string) []ErrorRecord {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var all []ErrorRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		all = append(all, readRecordsFromFile(t, filepath.Join(dir, e.Name()))...)
	}
	return all
}

func readRecordsFromFile(t *testing.T, path string) []ErrorRecord {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var records []ErrorRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec ErrorRecord
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		records = append(records, rec)
	}
	return records
}
