package app

import (
	"context"
	"testing"
	"time"

	"backend-server/internal/interfaces"
	"backend-server/internal/shared/concurrency"
)

// MockLogger 模拟日志器
type MockLogger struct{}

func (m *MockLogger) Debug(args ...interface{})                                  {}
func (m *MockLogger) Info(args ...interface{})                                   {}
func (m *MockLogger) Warn(args ...interface{})                                   {}
func (m *MockLogger) Error(args ...interface{})                                  {}
func (m *MockLogger) Fatal(args ...interface{})                                  {}
func (m *MockLogger) Debugf(template string, args ...interface{})                {}
func (m *MockLogger) Infof(template string, args ...interface{})                 {}
func (m *MockLogger) Warnf(template string, args ...interface{})                 {}
func (m *MockLogger) Errorf(template string, args ...interface{})                {}
func (m *MockLogger) Fatalf(template string, args ...interface{})                {}
func (m *MockLogger) WithField(key string, value interface{}) interfaces.Logger  { return m }
func (m *MockLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return m }

// TestGoroutineManager 测试goroutine管理器
func TestGoroutineManager(t *testing.T) {
	logger := &MockLogger{}
	gm := concurrency.NewGoroutineManager(logger)

	// 启动一个测试goroutine
	gm.Go("test-goroutine", func(ctx context.Context) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
			return
		}
	})

	// 检查统计信息
	total, running := gm.Stats()
	if total != 1 {
		t.Errorf("Expected total goroutines to be 1, got %d", total)
	}
	if running != 1 {
		t.Errorf("Expected running goroutines to be 1, got %d", running)
	}

	// 等待goroutine完成
	time.Sleep(200 * time.Millisecond)

	// 关闭管理器
	err := gm.Shutdown(1 * time.Second)
	if err != nil {
		t.Errorf("Expected no error during shutdown, got %v", err)
	}
}

// TestWorkerPool 测试工作池
func TestWorkerPool(t *testing.T) {
	logger := &MockLogger{}
	wp := concurrency.NewWorkerPool(2, 10, logger, nil)

	// 启动工作池
	wp.Start()

	// 创建一个简单的任务
	task := &TestTask{id: "test-task-1"}

	// 提交任务
	err := wp.Submit(task)
	if err != nil {
		t.Errorf("Expected no error when submitting task, got %v", err)
	}

	// 等待任务完成
	time.Sleep(100 * time.Millisecond)

	// 检查统计信息
	stats := wp.Stats()
	if stats["total_tasks"] != 1 {
		t.Errorf("Expected total tasks to be 1, got %d", stats["total_tasks"])
	}

	// 停止工作池
	err = wp.Stop(1 * time.Second)
	if err != nil {
		t.Errorf("Expected no error during stop, got %v", err)
	}
}

// TestTask 测试任务
type TestTask struct {
	id string
}

func (t *TestTask) Execute(ctx context.Context) error {
	time.Sleep(50 * time.Millisecond)
	return nil
}

func (t *TestTask) GetID() string {
	return t.id
}

func (t *TestTask) GetPriority() int {
	return 1
}

// TestChatManagerTask 测试聊天管理器任务
func TestChatManagerTask(t *testing.T) {
	// 这里可以添加更复杂的测试，包括模拟transport和app
	// 由于依赖较多，这里只做基本的结构测试

	task := &ChatManagerTask{
		deviceID: "test-device",
		app:      nil, // 在实际测试中需要mock
	}

	if task.GetID() != "chat-manager-test-device" {
		t.Errorf("Expected task ID to be 'chat-manager-test-device', got %s", task.GetID())
	}

	if task.GetPriority() != 1 {
		t.Errorf("Expected priority to be 1, got %d", task.GetPriority())
	}
}
