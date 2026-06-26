package wechat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAsMessenger_NoManagerReturnsNil(t *testing.T) {
	t.Parallel()

	m := AsMessenger(nil)
	require.Nil(t, m.ActiveSender())
}
