// Package session 提供会话管理功能。
//
// 本文件包含 session 包的单元测试。
package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test_HashID 测试 HashID 函数。
func Test_HashID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want int // 期望的哈希字符串长度
	}{
		{
			name: "标准 UUID",
			id:   "550e8400-e29b-41d4-a716-446655440000",
			want: 16, // xxh3 输出的十六进制长度
		},
		{
			name: "空字符串",
			id:   "",
			want: 16,
		},
		{
			name: "短 ID",
			id:   "abc",
			want: 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HashID(tt.id)
			assert.NotEmpty(t, got)
			// 验证是十六进制字符串
			assert.Regexp(t, "^[0-9a-f]+$", got)
		})
	}
}

// Test_HashID_Deterministic 测试 HashID 的确定性。
func Test_HashID_Deterministic(t *testing.T) {
	id := "test-session-id"
	hash1 := HashID(id)
	hash2 := HashID(id)
	assert.Equal(t, hash1, hash2)
}

// Test_HashID_Unique 测试不同 ID 生成不同哈希。
func Test_HashID_Unique(t *testing.T) {
	id1 := "session-1"
	id2 := "session-2"
	hash1 := HashID(id1)
	hash2 := HashID(id2)
	assert.NotEqual(t, hash1, hash2)
}

// Test_HasIncompleteTodos 测试 HasIncompleteTodos 函数。
func Test_HasIncompleteTodos(t *testing.T) {
	tests := []struct {
		name  string
		todos []Todo
		want  bool
	}{
		{
			name:  "空列表",
			todos: []Todo{},
			want:  false,
		},
		{
			name: "所有已完成",
			todos: []Todo{
				{Content: "task 1", Status: TodoStatusCompleted},
				{Content: "task 2", Status: TodoStatusCompleted},
			},
			want: false,
		},
		{
			name: "有未完成任务",
			todos: []Todo{
				{Content: "task 1", Status: TodoStatusCompleted},
				{Content: "task 2", Status: TodoStatusPending},
			},
			want: true,
		},
		{
			name: "有进行中任务",
			todos: []Todo{
				{Content: "task 1", Status: TodoStatusInProgress},
			},
			want: true,
		},
		{
			name: "全部待处理",
			todos: []Todo{
				{Content: "task 1", Status: TodoStatusPending},
				{Content: "task 2", Status: TodoStatusPending},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasIncompleteTodos(tt.todos)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test_TodoStatus_Constants 测试 TodoStatus 常量。
func Test_TodoStatus_Constants(t *testing.T) {
	assert.Equal(t, TodoStatus("pending"), TodoStatusPending)
	assert.Equal(t, TodoStatus("in_progress"), TodoStatusInProgress)
	assert.Equal(t, TodoStatus("completed"), TodoStatusCompleted)
}
