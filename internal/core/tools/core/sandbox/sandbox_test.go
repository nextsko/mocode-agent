package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCappedBufferDropsMiddle(t *testing.T) {
	var b cappedBuffer
	chunk := strings.Repeat("a", halfCap)
	b.Write([]byte(chunk))                              // head full
	b.Write([]byte(strings.Repeat("b", halfCap+300)))   // tail fills, oldest b's roll out
	b.Write([]byte(strings.Repeat("c", 200)))           // tail keeps last halfCap bytes
	s := b.String()
	require.True(t, strings.HasPrefix(s, "aaaa"))
	require.True(t, strings.HasSuffix(s, "cccc"))
	require.Contains(t, s, "bytes omitted")
}

func TestRunInDir_Timeout(t *testing.T) {
	if lookupRuntime("python", "python3") == "" && lookupRuntime("node") == "" {
		t.Skip("no runtime available")
	}
	runtime := lookupRuntime("python", "python3")
	code := "import time; time.sleep(10)"
	if runtime == "" {
		runtime = lookupRuntime("node")
		code = "setTimeout(() => {}, 10000)"
	}
	dir := t.TempDir()
	argv := []string{runtime, "-c", code}
	if strings.HasSuffix(runtime, "node") {
		argv = []string{runtime, "-e", code}
	}
	res, err := runInDir(context.Background(), dir, argv, nil, 1*time.Second)
	require.NoError(t, err)
	require.Contains(t, res.Output, "timed out")
}

func TestRunInDir_Success(t *testing.T) {
	runtime := lookupRuntime("python", "python3")
	if runtime == "" {
		t.Skip("no python available")
	}
	res, err := runInDir(context.Background(), t.TempDir(), []string{runtime, "-c", "print('hello sandbox')"}, nil, 10*time.Second)
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	require.Contains(t, res.Output, "hello sandbox")
}
