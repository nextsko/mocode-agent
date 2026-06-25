package memory

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceToolsConcurrentAccess(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	t.Cleanup(func() {
		_ = db.Close()
	})

	svc, err := NewService(ServiceConfig{DB: db})
	require.NoError(t, err)

	concrete, ok := svc.(*service)
	require.True(t, ok)

	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				tools := concrete.Tools()
				require.NotEmpty(t, tools)
			}
		}()
	}

	wg.Wait()

	tools := concrete.Tools()
	require.Len(t, tools, 5)
}
