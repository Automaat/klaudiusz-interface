// Package scheduler provides scheduled task execution for Claude CLI.
package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/Automaat/klaudiusz-interface/config"
)

// TaskRunner manages execution of a single scheduled task
type TaskRunner struct {
	task      config.ScheduledTask
	executor  TaskExecutor
	ticker    *time.Ticker
	lastRun   atomic.Int64 // Unix timestamp
	runCount  atomic.Int64
	lastError atomic.Value // string
}

// NewTaskRunner creates a new task runner
func NewTaskRunner(task config.ScheduledTask, executor TaskExecutor) *TaskRunner {
	return &TaskRunner{
		task:     task,
		executor: executor,
	}
}

// Start begins the ticker loop in a goroutine
func (tr *TaskRunner) Start(parentStopCh chan struct{}) {
	tr.ticker = time.NewTicker(tr.task.Interval)

	go func() {
		defer tr.ticker.Stop()

		log.Printf(
			"[SCHEDULER] Task '%s' started (interval=%v, type=%s)",
			tr.task.Name,
			tr.task.Interval,
			tr.task.Type,
		)

		for {
			select {
			case <-tr.ticker.C:
				tr.execute()

			case <-parentStopCh:
				log.Printf("[SCHEDULER] Task '%s' stopped", tr.task.Name)
				return
			}
		}
	}()
}

// execute runs the task with panic recovery
func (tr *TaskRunner) execute() {
	start := time.Now()
	tr.lastRun.Store(start.Unix())

	defer func() {
		tr.runCount.Add(1)

		if r := recover(); r != nil {
			errMsg := fmt.Sprintf("panic: %v", r)
			tr.lastError.Store(errMsg)
			log.Printf("[SCHEDULER] Task '%s' panicked: %v", tr.task.Name, r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), tr.task.Timeout)
	defer cancel()

	log.Printf("[SCHEDULER] Task '%s' executing...", tr.task.Name)

	err := tr.executor.Execute(ctx, tr.task)

	duration := time.Since(start)

	if err != nil {
		tr.lastError.Store(err.Error())
		log.Printf(
			"[SCHEDULER] Task '%s' failed (duration=%v): %v",
			tr.task.Name,
			duration,
			err,
		)
	} else {
		tr.lastError.Store("")
		log.Printf(
			"[SCHEDULER] Task '%s' completed (duration=%v)",
			tr.task.Name,
			duration,
		)
	}
}

// Stats returns execution statistics
func (tr *TaskRunner) Stats() TaskStats {
	lastRunUnix := tr.lastRun.Load()

	var lastRun *time.Time

	if lastRunUnix > 0 {
		t := time.Unix(lastRunUnix, 0)
		lastRun = &t
	}

	var lastErr string

	if errVal := tr.lastError.Load(); errVal != nil {
		var ok bool

		lastErr, ok = errVal.(string)
		if !ok {
			lastErr = ""
		}
	}

	return TaskStats{
		Name:      tr.task.Name,
		LastRun:   lastRun,
		RunCount:  tr.runCount.Load(),
		LastError: lastErr,
	}
}

// TaskStats contains execution statistics for a task
type TaskStats struct {
	Name      string
	LastRun   *time.Time
	RunCount  int64
	LastError string
}
