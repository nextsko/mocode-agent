package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/transport/admin"
)

// TestAdminKnowledgeEndpoint starts the real admin server on an ephemeral
// port and fetches /api/knowledge with its bearer token.
func TestAdminKnowledgeEndpoint(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("MOCODE_GLOBAL_CONFIG", "")
	t.Setenv("MOCODE_GLOBAL_DATA", "")
	ws, cleanup, err := setupLocalWorkspaceAt(ctx, t.TempDir(), false, false)
	require.NoError(t, err)
	defer cleanup()

	srv := admin.New(ws)
	url, err := srv.Start(ctx, 0)
	require.NoError(t, err)
	defer func() { _ = srv.Stop(ctx) }()
	require.NotEmpty(t, srv.Token())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/knowledge", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Contains(t, payload, "flywheel")
	assert.Contains(t, payload, "memory_nodes")
	assert.True(t, strings.Contains(string(body), "working_dir"))
}
