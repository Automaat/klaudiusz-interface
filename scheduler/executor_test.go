// Package scheduler provides scheduled task execution for Claude CLI.
package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Automaat/klaudiusz-interface/config"
)

func TestBuildArgs_Skill(t *testing.T) {
	executor := &ClaudeTaskExecutor{}

	task := config.ScheduledTask{
		Type:    "skill",
		Command: "commit",
		Args:    "-m 'test'",
	}

	args, err := executor.buildArgs(task)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	expected := []string{"--no-session-persistence", "--skill", "commit", "-m 'test'"}
	if len(args) != len(expected) {
		t.Fatalf("wrong number of args: got %d, want %d", len(args), len(expected))
	}

	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, arg, expected[i])
		}
	}
}

func TestBuildArgs_SkillNoArgs(t *testing.T) {
	executor := &ClaudeTaskExecutor{}

	task := config.ScheduledTask{
		Type:    "skill",
		Command: "commit",
		Args:    "",
	}

	args, err := executor.buildArgs(task)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	expected := []string{"--no-session-persistence", "--skill", "commit"}
	if len(args) != len(expected) {
		t.Fatalf("wrong number of args: got %d, want %d", len(args), len(expected))
	}

	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, arg, expected[i])
		}
	}
}

func TestBuildArgs_Command(t *testing.T) {
	executor := &ClaudeTaskExecutor{}

	task := config.ScheduledTask{
		Type:    "command",
		Command: "analyze this codebase",
	}

	args, err := executor.buildArgs(task)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	expected := []string{"--no-session-persistence", "analyze this codebase"}
	if len(args) != len(expected) {
		t.Fatalf("wrong number of args: got %d, want %d", len(args), len(expected))
	}

	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, arg, expected[i])
		}
	}
}

func TestBuildArgs_InvalidType(t *testing.T) {
	executor := &ClaudeTaskExecutor{}

	task := config.ScheduledTask{
		Type:    "invalid",
		Command: "test",
	}

	_, err := executor.buildArgs(task)
	if err == nil {
		t.Fatal("expected error for invalid task type")
	}
}

func TestBuildArgs_SkipPermissions(t *testing.T) {
	executor := &ClaudeTaskExecutor{}

	task := config.ScheduledTask{
		Type:            "skill",
		Command:         "commit",
		SkipPermissions: true,
	}

	args, err := executor.buildArgs(task)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	expected := []string{
		"--no-session-persistence",
		"--dangerously-skip-permissions",
		"--skill",
		"commit",
	}

	if len(args) != len(expected) {
		t.Fatalf("wrong number of args: got %d, want %d", len(args), len(expected))
	}

	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, arg, expected[i])
		}
	}
}

func TestBuildArgs_SkipPermissionsCommand(t *testing.T) {
	executor := &ClaudeTaskExecutor{}

	task := config.ScheduledTask{
		Type:            "command",
		Command:         "delete old files",
		SkipPermissions: true,
	}

	args, err := executor.buildArgs(task)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	expected := []string{
		"--no-session-persistence",
		"--dangerously-skip-permissions",
		"delete old files",
	}

	if len(args) != len(expected) {
		t.Fatalf("wrong number of args: got %d, want %d", len(args), len(expected))
	}

	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, arg, expected[i])
		}
	}
}

func TestBuildArgs_NoSkipPermissions(t *testing.T) {
	executor := &ClaudeTaskExecutor{}

	task := config.ScheduledTask{
		Type:            "skill",
		Command:         "commit",
		SkipPermissions: false,
	}

	args, err := executor.buildArgs(task)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	// Should NOT contain --dangerously-skip-permissions
	for _, arg := range args {
		if arg == "--dangerously-skip-permissions" {
			t.Error("unexpected --dangerously-skip-permissions flag when SkipPermissions=false")
		}
	}

	// Should contain --no-session-persistence and --skill
	foundNoSession := false
	foundSkill := false

	for _, arg := range args {
		if arg == "--no-session-persistence" {
			foundNoSession = true
		}

		if arg == "--skill" {
			foundSkill = true
		}
	}

	if !foundNoSession {
		t.Error("missing --no-session-persistence flag")
	}

	if !foundSkill {
		t.Error("missing --skill flag")
	}
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		contains string // Path should contain this string
	}{
		{
			name:     "empty path",
			input:    "",
			wantErr:  false,
			contains: "",
		},
		{
			name:     "bare tilde",
			input:    "~",
			wantErr:  false,
			contains: "/",
		},
		{
			name:     "tilde with path",
			input:    "~/test",
			wantErr:  false,
			contains: "test",
		},
		{
			name:     "absolute path",
			input:    "/tmp/test",
			wantErr:  false,
			contains: "/tmp/test",
		},
		{
			name:     "relative path",
			input:    "test",
			wantErr:  false,
			contains: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expandPath(tt.input)

			if tt.wantErr && err == nil {
				t.Fatal("expected error but got none")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.contains != "" && result != tt.input {
				// For empty input, result should be empty
				if tt.input == "" && result != "" {
					t.Fatalf("expected empty result, got %q", result)
				}
			}
		})
	}
}

// mockExecutor for testing SchedulerManager
type mockExecutor struct {
	executeCalled atomic.Int64
	executeError  error
}

func (m *mockExecutor) Execute(_ context.Context, _ config.ScheduledTask) error {
	m.executeCalled.Add(1)
	return m.executeError
}

func TestSchedulerManager_Lifecycle(t *testing.T) {
	tasks := []config.ScheduledTask{
		{
			Name:     "test_task",
			Interval: 100 * time.Millisecond,
			Type:     "command",
			Command:  "echo test",
			Timeout:  1 * time.Second,
			Enabled:  true,
		},
	}

	mock := &mockExecutor{}
	sm := NewSchedulerManager(tasks, mock)

	// Start scheduler
	sm.Start()

	// Wait for at least 2 executions
	time.Sleep(250 * time.Millisecond)

	// Stop scheduler
	sm.Stop()

	if mock.executeCalled.Load() < 2 {
		t.Errorf("expected at least 2 executions, got %d", mock.executeCalled.Load())
	}

	// Verify stats
	stats := sm.Stats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 task stat, got %d", len(stats))
	}

	if stats[0].Name != "test_task" {
		t.Errorf("wrong task name: got %q, want %q", stats[0].Name, "test_task")
	}

	if stats[0].RunCount < 2 {
		t.Errorf("expected at least 2 runs, got %d", stats[0].RunCount)
	}
}

func TestSchedulerManager_DisabledTask(t *testing.T) {
	tasks := []config.ScheduledTask{
		{
			Name:     "disabled_task",
			Interval: 100 * time.Millisecond,
			Type:     "command",
			Command:  "echo test",
			Timeout:  1 * time.Second,
			Enabled:  false,
		},
	}

	mock := &mockExecutor{}
	sm := NewSchedulerManager(tasks, mock)

	sm.Start()
	time.Sleep(250 * time.Millisecond)
	sm.Stop()

	if mock.executeCalled.Load() > 0 {
		t.Errorf("expected no executions for disabled task, got %d", mock.executeCalled.Load())
	}
}

func TestSchedulerManager_MultipleTasksConcurrent(t *testing.T) {
	tasks := []config.ScheduledTask{
		{
			Name:     "task1",
			Interval: 100 * time.Millisecond,
			Type:     "command",
			Command:  "echo task1",
			Timeout:  1 * time.Second,
			Enabled:  true,
		},
		{
			Name:     "task2",
			Interval: 150 * time.Millisecond,
			Type:     "command",
			Command:  "echo task2",
			Timeout:  1 * time.Second,
			Enabled:  true,
		},
	}

	mock := &mockExecutor{}
	sm := NewSchedulerManager(tasks, mock)

	sm.Start()
	time.Sleep(350 * time.Millisecond)
	sm.Stop()

	// task1 should run ~3 times, task2 ~2 times = ~5 total
	if mock.executeCalled.Load() < 4 {
		t.Errorf("expected at least 4 executions, got %d", mock.executeCalled.Load())
	}

	stats := sm.Stats()
	if len(stats) != 2 {
		t.Fatalf("expected 2 task stats, got %d", len(stats))
	}
}
