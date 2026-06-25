package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/styles"
	"github.com/package-register/mocode/internal/ui/util"
)

func newTestToast() *Toast {
	com := common.DefaultCommon(nil)
	return NewToast(com)
}

func TestToast_NewAndInitialState(t *testing.T) {
	toast := newTestToast()
	assert.NotNil(t, toast)
	assert.False(t, toast.IsVisible())
}

func TestToast_ShowSuccess(t *testing.T) {
	toast := newTestToast()
	msg := util.InfoMsg{Type: util.InfoTypeSuccess, Msg: "File saved successfully"}

	toast.Show(msg)
	assert.True(t, toast.IsVisible())
	assert.Equal(t, util.InfoTypeSuccess, toast.msg.Type)
	assert.Equal(t, "File saved successfully", toast.msg.Msg)
}

func TestToast_ShowWarn(t *testing.T) {
	toast := newTestToast()
	msg := util.InfoMsg{Type: util.InfoTypeWarn, Msg: "Disk space low"}

	toast.Show(msg)
	assert.True(t, toast.IsVisible())
	assert.Equal(t, util.InfoTypeWarn, toast.msg.Type)
}

func TestToast_ShowError(t *testing.T) {
	toast := newTestToast()
	msg := util.InfoMsg{Type: util.InfoTypeError, Msg: "Connection failed"}

	toast.Show(msg)
	assert.True(t, toast.IsVisible())
	assert.Equal(t, util.InfoTypeError, toast.msg.Type)
}

func TestToast_ShowInfo(t *testing.T) {
	toast := newTestToast()
	msg := util.InfoMsg{Type: util.InfoTypeInfo, Msg: "Update available"}

	toast.Show(msg)
	assert.True(t, toast.IsVisible())
	assert.Equal(t, util.InfoTypeInfo, toast.msg.Type)
}

func TestToast_ShowUpdate(t *testing.T) {
	toast := newTestToast()
	msg := util.InfoMsg{Type: util.InfoTypeUpdate, Msg: "New version installed"}

	toast.Show(msg)
	assert.True(t, toast.IsVisible())
	assert.Equal(t, util.InfoTypeUpdate, toast.msg.Type)
}

func TestToast_Hide(t *testing.T) {
	toast := newTestToast()
	msg := util.InfoMsg{Type: util.InfoTypeSuccess, Msg: "Quick!"}

	toast.Show(msg)
	assert.True(t, toast.IsVisible())

	toast.Hide()
	assert.False(t, toast.IsVisible())
	assert.True(t, toast.msg.IsEmpty())
}

func TestToast_HideIdempotent(t *testing.T) {
	toast := newTestToast()
	// Hiding when already hidden should not panic
	toast.Hide()
	assert.False(t, toast.IsVisible())
}

func TestToast_ShowThenHideThenShow(t *testing.T) {
	toast := newTestToast()

	msg1 := util.InfoMsg{Type: util.InfoTypeSuccess, Msg: "First"}
	toast.Show(msg1)
	assert.True(t, toast.IsVisible())
	assert.Equal(t, "First", toast.msg.Msg)

	toast.Hide()
	assert.False(t, toast.IsVisible())

	msg2 := util.InfoMsg{Type: util.InfoTypeError, Msg: "Second"}
	toast.Show(msg2)
	assert.True(t, toast.IsVisible())
	assert.Equal(t, util.InfoTypeError, toast.msg.Type)
	assert.Equal(t, "Second", toast.msg.Msg)
}

func TestToast_ShowResetsTimer(t *testing.T) {
	toast := newTestToast()

	// Show first message — timer should be set
	msg1 := util.InfoMsg{Type: util.InfoTypeInfo, Msg: "First message"}
	toast.Show(msg1)
	assert.NotNil(t, toast.timer, "timer should be set after Show")

	// Show second message — timer should still be set (old one replaced)
	msg2 := util.InfoMsg{Type: util.InfoTypeInfo, Msg: "Second message"}
	toast.Show(msg2)
	assert.NotNil(t, toast.timer, "timer should still be set after re-Show")
}

func TestToast_AutoHideAfterTTL(t *testing.T) {
	toast := newTestToast()
	shortTTL := 100 * time.Millisecond

	msg := util.InfoMsg{Type: util.InfoTypeSuccess, Msg: "Quick toast", TTL: shortTTL}
	toast.Show(msg)
	assert.True(t, toast.IsVisible())

	// Wait for auto-hide
	time.Sleep(shortTTL * 3)
	assert.False(t, toast.IsVisible())
}

func TestToast_DefaultTTL(t *testing.T) {
	toast := newTestToast()

	msg := util.InfoMsg{Type: util.InfoTypeSuccess, Msg: "Default TTL"}
	toast.Show(msg)
	assert.True(t, toast.IsVisible())

	// Default is 5s, so it should still be visible after a short wait
	time.Sleep(100 * time.Millisecond)
	assert.True(t, toast.IsVisible())
}

func TestToast_EmptyMessageIgnored(t *testing.T) {
	toast := newTestToast()

	// Show with empty messsage should not crash
	toast.Show(util.InfoMsg{})
	assert.False(t, toast.IsVisible())
}

func TestToast_ReShowWhileVisible(t *testing.T) {
	toast := newTestToast()

	msg1 := util.InfoMsg{Type: util.InfoTypeSuccess, Msg: "First"}
	toast.Show(msg1)
	assert.Equal(t, "First", toast.msg.Msg)

	// Show a different message while first is still visible
	msg2 := util.InfoMsg{Type: util.InfoTypeWarn, Msg: "Second"}
	toast.Show(msg2)
	assert.Equal(t, "Second", toast.msg.Msg)
	assert.Equal(t, util.InfoTypeWarn, toast.msg.Type)
}

func TestToast_RendersForAllTypes(t *testing.T) {
	st := styles.ThemeForProvider("")
	tests := []struct {
		name    string
		msgType util.InfoType
	}{
		{"success", util.InfoTypeSuccess},
		{"info", util.InfoTypeInfo},
		{"update", util.InfoTypeUpdate},
		{"warn", util.InfoTypeWarn},
		{"error", util.InfoTypeError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toast := newTestToast()
			msg := util.InfoMsg{Type: tt.msgType, Msg: "Test message"}

			toast.Show(msg)

			// After Show, indicator and text should be cached
			assert.NotEmpty(t, toast.indicator, "indicator should be set for %s", tt.name)
			assert.NotEmpty(t, toast.text, "text should be set for %s", tt.name)

			// Verify indicator style matches type
			switch tt.msgType {
			case util.InfoTypeSuccess:
				assert.Equal(t, st.Toast.SuccessIndicator.Render(), toast.indicator)
			case util.InfoTypeInfo:
				assert.Equal(t, st.Toast.InfoIndicator.Render(), toast.indicator)
			case util.InfoTypeUpdate:
				assert.Equal(t, st.Toast.UpdateIndicator.Render(), toast.indicator)
			case util.InfoTypeWarn:
				assert.Equal(t, st.Toast.WarnIndicator.Render(), toast.indicator)
			case util.InfoTypeError:
				assert.Equal(t, st.Toast.ErrorIndicator.Render(), toast.indicator)
			}
		})
	}
}

func TestToast_HideClearsCachedRender(t *testing.T) {
	toast := newTestToast()

	msg := util.InfoMsg{Type: util.InfoTypeSuccess, Msg: "Cached content"}
	toast.Show(msg)
	assert.NotEmpty(t, toast.indicator)
	assert.NotEmpty(t, toast.text)

	toast.Hide()
	assert.Empty(t, toast.indicator)
	assert.Empty(t, toast.text)
}
