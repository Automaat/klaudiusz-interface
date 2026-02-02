// Package scheduler provides scheduled task execution for Claude CLI.
package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/Automaat/klaudiusz-interface/config"
)

func TestTaskRunner_SuccessfulExecution(t *testing.T) {
	mock := &mockExecutor{}

	task := config.ScheduledTask{
		Name:     "test_task",
		Interval: 100 * time.Millisecond,
		Type:     "command",
		Command:  "echo test",
		Timeout:  1 * time.Second,
		Enabled:  true,
	}

	runner := NewTaskRunner(task, mock)
	stopCh := make(chan struct{})

	runner.Start(stopCh)

	// Wait for execution
	time.Sleep(150 * time.Millisecond)

	close(stopCh)
	time.Sleep(50 * time.Millisecond) // Allow goroutine to stop

	stats := runner.Stats()
	if stats.Name != "test_task" {
		t.Errorf("wrong task name: got %q, want %q", stats.Name, "test_task")
	}

	if stats.RunCount == 0 {
		t.Error("expected at least 1 run")
	}

	if stats.LastRun == nil {
		t.Error("expected LastRun to be set")
	}

	if stats.LastError != "" {
		t.Errorf("expected no error, got %q", stats.LastError)
	}
}

func TestTaskRunner_ExecutionError(t *testing.T) {
	mock := &mockExecutor{
		executeError: errors.New("test error"),
	}

	task := config.ScheduledTask{
		Name:     "error_task",
		Interval: 100 * time.Millisecond,
		Type:     "command",
		Command:  "echo test",
		Timeout:  1 * time.Second,
		Enabled:  true,
	}

	runner := NewTaskRunner(task, mock)
	stopCh := make(chan struct{})

	runner.Start(stopCh)
	time.Sleep(150 * time.Millisecond)
	close(stopCh)
	time.Sleep(50 * time.Millisecond)

	stats := runner.Stats()
	if stats.LastError == "" {
		t.Error("expected error to be recorded")
	}

	if stats.RunCount == 0 {
		t.Error("expected at least 1 run")
	}
}

func TestTaskRunner_GracefulShutdown(t *testing.T) {
	mock := &mockExecutor{}

	task := config.ScheduledTask{
		Name:     "shutdown_task",
		Interval: 1 * time.Second, // Long interval
		Type:     "command",
		Command:  "echo test",
		Timeout:  1 * time.Second,
		Enabled:  true,
	}

	runner := NewTaskRunner(task, mock)
	stopCh := make(chan struct{})

	runner.Start(stopCh)

	// Stop before first tick
	time.Sleep(100 * time.Millisecond)
	close(stopCh)
	time.Sleep(50 * time.Millisecond)

	// Should stop without executing
	if mock.executeCalled > 0 {
		t.Errorf("expected no executions, got %d", mock.executeCalled)
	}
}

func TestTaskRunner_MultipleIntervals(t *testing.T) {
	mock := &mockExecutor{}

	task := config.ScheduledTask{
		Name:     "multi_task",
		Interval: 100 * time.Millisecond,
		Type:     "command",
		Command:  "echo test",
		Timeout:  50 * time.Millisecond,
		Enabled:  true,
	}

	runner := NewTaskRunner(task, mock)
	stopCh := make(chan struct{})

	runner.Start(stopCh)

	// Wait for multiple intervals
	time.Sleep(350 * time.Millisecond)

	close(stopCh)
	time.Sleep(50 * time.Millisecond)

	stats := runner.Stats()

	// Should run 3 times (at 100ms, 200ms, 300ms)
	if stats.RunCount < 3 {
		t.Errorf("expected at least 3 runs, got %d", stats.RunCount)
	}
}

// panicExecutor simulates a panic during execution
type panicExecutor struct{}

func (*panicExecutor) Execute(_ context.Context, _ config.ScheduledTask) error {
	panic("simulated panic")
}

func TestTaskRunner_PanicRecovery(t *testing.T) {
	mock := &panicExecutor{}

	task := config.ScheduledTask{
		Name:     "panic_task",
		Interval: 100 * time.Millisecond,
		Type:     "command",
		Command:  "echo test",
		Timeout:  1 * time.Second,
		Enabled:  true,
	}

	runner := NewTaskRunner(task, mock)
	stopCh := make(chan struct{})

	// Should not crash
	runner.Start(stopCh)
	time.Sleep(150 * time.Millisecond)
	close(stopCh)
	time.Sleep(50 * time.Millisecond)

	stats := runner.Stats()

	// Panic should be recorded as error
	if stats.LastError == "" {
		t.Error("expected panic to be recorded as error")
	}

	if stats.RunCount == 0 {
		t.Error("expected at least 1 run")
	}
}

func TestTaskRunner_Stats_NoExecution(t *testing.T) {
	mock := &mockExecutor{}

	task := config.ScheduledTask{
		Name:     "no_exec_task",
		Interval: 1 * time.Hour,
		Type:     "command",
		Command:  "echo test",
		Timeout:  1 * time.Second,
		Enabled:  true,
	}

	runner := NewTaskRunner(task, mock)

	stats := runner.Stats()

	if stats.LastRun != nil {
		t.Error("expected LastRun to be nil before first execution")
	}

	if stats.RunCount != 0 {
		t.Errorf("expected 0 runs, got %d", stats.RunCount)
	}

	if stats.LastError != "" {
		t.Errorf("expected no error, got %q", stats.LastError)
	}
}
