// Package scheduler provides scheduled task execution for Claude CLI.
package scheduler

import (
	"log"

	"github.com/Automaat/klaudiusz-interface/config"
)

// SchedulerManager manages all scheduled tasks
type SchedulerManager struct {
	runners  []*TaskRunner
	stopCh   chan struct{}
	executor TaskExecutor
}

// NewSchedulerManager creates a new scheduler manager
func NewSchedulerManager(tasks []config.ScheduledTask, executor TaskExecutor) *SchedulerManager {
	runners := make([]*TaskRunner, 0, len(tasks))

	for _, task := range tasks {
		if !task.Enabled {
			log.Printf("[SCHEDULER] Task '%s' disabled, skipping", task.Name)
			continue
		}

		runner := NewTaskRunner(task, executor)
		runners = append(runners, runner)
	}

	return &SchedulerManager{
		runners:  runners,
		stopCh:   make(chan struct{}),
		executor: executor,
	}
}

// Start launches all task runners
func (sm *SchedulerManager) Start() {
	if len(sm.runners) == 0 {
		log.Println("[SCHEDULER] No enabled tasks to start")
		return
	}

	log.Printf("[SCHEDULER] Starting %d tasks", len(sm.runners))

	for _, runner := range sm.runners {
		runner.Start(sm.stopCh)
	}
}

// Stop gracefully shuts down all task runners
func (sm *SchedulerManager) Stop() {
	if sm.stopCh == nil {
		return
	}

	log.Println("[SCHEDULER] Stopping all tasks")
	close(sm.stopCh)
}

// Stats returns statistics for all tasks
func (sm *SchedulerManager) Stats() []TaskStats {
	stats := make([]TaskStats, 0, len(sm.runners))

	for _, runner := range sm.runners {
		stats = append(stats, runner.Stats())
	}

	return stats
}
