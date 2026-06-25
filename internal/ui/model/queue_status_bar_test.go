package model

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/package-register/mocode/internal/ui/components"
)

func TestQueueStatusBar_View_Empty(t *testing.T) {
	qsb := NewQueueStatusBar()

	// 空队列应该返回空字符串
	qsb.Update(components.QueueStatus{})
	assert.Equal(t, "", qsb.View())
}

func TestQueueStatusBar_View_WithMessages(t *testing.T) {
	qsb := NewQueueStatusBar()

	// 有消息时应该显示
	qsb.Update(components.QueueStatus{
		SteerDepth:    2,
		SteerCapacity: 5,
	})

	view := qsb.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "2/5")
}

func TestQueueStatusBar_Visible(t *testing.T) {
	qsb := NewQueueStatusBar()

	qsb.Update(components.QueueStatus{
		SteerDepth:    1,
		SteerCapacity: 5,
	})

	// 可见时应该显示
	qsb.SetVisible(true)
	assert.NotEmpty(t, qsb.View())

	// 不可见时应该隐藏
	qsb.SetVisible(false)
	assert.Equal(t, "", qsb.View())
}

func TestQueueStatusBar_HasMessages(t *testing.T) {
	qsb := NewQueueStatusBar()

	// 初始状态
	assert.False(t, qsb.HasMessages())

	// 有消息
	qsb.Update(components.QueueStatus{SteerDepth: 1})
	assert.True(t, qsb.HasMessages())
}

func TestStatusBarWithQueue_View(t *testing.T) {
	status := NewStatus(nil)
	status.SetLine("test status")

	sbwq := NewStatusBarWithQueue(status)

	// 没有队列消息时只显示状态
	view := sbwq.View()
	assert.Contains(t, view, "test status")

	// 有队列消息时显示两者
	sbwq.UpdateQueueStatus(components.QueueStatus{
		SteerDepth:    1,
		SteerCapacity: 5,
	})
	view = sbwq.View()
	assert.Contains(t, view, "test status")
	assert.Contains(t, view, "1/5")
}

func TestStatusBarWithQueue_ShowQueue(t *testing.T) {
	status := NewStatus(nil)
	status.SetLine("test")

	sbwq := NewStatusBarWithQueue(status)
	sbwq.UpdateQueueStatus(components.QueueStatus{
		SteerDepth:    1,
		SteerCapacity: 5,
	})

	// 显示队列
	sbwq.SetShowQueue(true)
	view := sbwq.View()
	assert.Contains(t, view, "1/5")

	// 隐藏队列
	sbwq.SetShowQueue(false)
	view = sbwq.View()
	assert.NotContains(t, view, "1/5")
}

func TestStatusBarWithQueue_HasQueueMessages(t *testing.T) {
	status := NewStatus(nil)
	sbwq := NewStatusBarWithQueue(status)

	// 初始状态
	assert.False(t, sbwq.HasQueueMessages())

	// 有消息
	sbwq.UpdateQueueStatus(components.QueueStatus{SteerDepth: 1})
	assert.True(t, sbwq.HasQueueMessages())
}

func TestQueueStatusComponent_View(t *testing.T) {
	qsc := NewQueueStatusComponent()

	// 空队列
	qsc.Update(components.QueueStatus{})
	assert.Equal(t, "", qsc.View())

	// 有消息
	qsc.Update(components.QueueStatus{
		SteerDepth:    1,
		SteerCapacity: 5,
	})
	assert.NotEmpty(t, qsc.View())
}

func TestQueueStatusComponent_Focused(t *testing.T) {
	qsc := NewQueueStatusComponent()

	qsc.Update(components.QueueStatus{
		SteerDepth:    1,
		SteerCapacity: 5,
	})

	// 未聚焦
	qsc.SetFocused(false)
	view1 := qsc.View()

	// 聚焦
	qsc.SetFocused(true)
	view2 := qsc.View()

	// 聚焦时应该有边框
	assert.NotEqual(t, view1, view2)
}

func TestQueueStatusComponent_HasMessages(t *testing.T) {
	qsc := NewQueueStatusComponent()

	// 初始状态
	assert.False(t, qsc.HasMessages())

	// 有消息
	qsc.Update(components.QueueStatus{SteerDepth: 1})
	assert.True(t, qsc.HasMessages())
}

func TestFormatQueueStatus_Empty(t *testing.T) {
	status := components.QueueStatus{}
	result := FormatQueueStatus(status)
	assert.Equal(t, "", result)
}

func TestFormatQueueStatus_SteerOnly(t *testing.T) {
	status := components.QueueStatus{
		SteerDepth:    2,
		SteerCapacity: 5,
	}
	result := FormatQueueStatus(status)
	assert.Contains(t, result, "⚡")
	assert.Contains(t, result, "2/5")
}

func TestFormatQueueStatus_FollowUpOnly(t *testing.T) {
	status := components.QueueStatus{
		FollowupDepth:    3,
		FollowupCapacity: 20,
	}
	result := FormatQueueStatus(status)
	assert.Contains(t, result, "📨")
	assert.Contains(t, result, "3/20")
}

func TestFormatQueueStatus_BothQueues(t *testing.T) {
	status := components.QueueStatus{
		SteerDepth:       1,
		SteerCapacity:    5,
		FollowupDepth:    2,
		FollowupCapacity: 20,
	}
	result := FormatQueueStatus(status)
	assert.Contains(t, result, "⚡")
	assert.Contains(t, result, "1/5")
	assert.Contains(t, result, "📨")
	assert.Contains(t, result, "2/20")
}

func TestDefaultQueueStatusBarStyle(t *testing.T) {
	style := DefaultQueueStatusBarStyle()

	assert.NotNil(t, style.ContainerStyle)
	assert.NotNil(t, style.TextStyle)
	assert.NotNil(t, style.IconStyle)
}

func TestQueueStatusBar_Width(t *testing.T) {
	qsb := NewQueueStatusBar()

	// 设置宽度
	qsb.SetWidth(80)
	assert.Equal(t, 80, qsb.width)
}

func TestQueueStatusComponent_Size(t *testing.T) {
	qsc := NewQueueStatusComponent()

	// 设置大小
	qsc.SetSize(80, 24)
	assert.Equal(t, 80, qsc.width)
	assert.Equal(t, 24, qsc.height)
}
