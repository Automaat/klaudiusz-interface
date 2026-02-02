// Package scheduler provides scheduled task execution for Claude CLI.
package scheduler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/Automaat/klaudiusz-interface/config"
)

// TaskExecutor executes scheduled tasks
type TaskExecutor interface {
	Execute(ctx context.Context, task config.ScheduledTask) error
}

// ClaudeTaskExecutor executes tasks using Claude CLI
type ClaudeTaskExecutor struct {
	claudePath string
	workingDir string
}

// NewClaudeTaskExecutor creates a new Claude task executor
func NewClaudeTaskExecutor(claudePath, workingDir string) *ClaudeTaskExecutor {
	return &ClaudeTaskExecutor{
		claudePath: claudePath,
		workingDir: workingDir,
	}
}

// Execute runs the task using Claude CLI
func (e *ClaudeTaskExecutor) Execute(ctx context.Context, task config.ScheduledTask) error {
	args, err := e.buildArgs(task)
	if err != nil {
		return errors.Wrap(err, "build args")
	}

	// Expand paths
	claudePath, err := expandPath(e.claudePath)
	if err != nil {
		return errors.Wrap(err, "expand claude path")
	}

	workingDir := e.workingDir
	if task.WorkingDir != "" {
		workingDir = task.WorkingDir
	}

	workingDir, err = expandPath(workingDir)
	if err != nil {
		return errors.Wrap(err, "expand working dir")
	}

	// Validate claude path (security)
	claudePath = filepath.Clean(claudePath)
	if !filepath.IsAbs(claudePath) {
		return errors.New("claude path must be absolute")
	}

	// #nosec G204 -- claudePath from config, validated with Clean/IsAbs
	cmd := exec.CommandContext(ctx, claudePath, args...)
	cmd.Dir = workingDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "execute claude: %s", string(output))
	}

	return nil
}

// buildArgs constructs CLI arguments based on task type
func (*ClaudeTaskExecutor) buildArgs(task config.ScheduledTask) ([]string, error) {
	args := []string{"--no-session-persistence"}

	if task.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	switch task.Type {
	case "skill":
		args = append(args, "--skill", task.Command)
		if task.Args != "" {
			args = append(args, task.Args)
		}

	case "command":
		args = append(args, task.Command)

	default:
		return nil, errors.Newf("invalid task type: %s", task.Type)
	}

	return args, nil
}

// expandPath expands ~ to home directory
func expandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}

	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Wrap(err, "get home directory")
		}

		return home, nil
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Wrap(err, "get home directory")
		}

		return filepath.Join(home, path[2:]), nil
	}

	if !filepath.IsAbs(path) {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return "", errors.Wrap(err, "resolve absolute path")
		}

		return absPath, nil
	}

	return path, nil
}
