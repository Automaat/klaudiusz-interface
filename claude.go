package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

func executeClaude(
	ctx context.Context,
	prompt string,
	session *Session,
	claudePath string,
	workingDir string,
	maxPromptLength int,
) (string, error) {
	if len(prompt) > maxPromptLength {
		return "", errors.Newf(
			"prompt too long: %d characters (max %d)",
			len(prompt),
			maxPromptLength,
		)
	}

	// Expand ~ in paths
	var err error

	claudePath, err = expandPath(claudePath)
	if err != nil {
		return "", errors.Wrap(err, "expand claude path")
	}

	workingDir, err = expandPath(workingDir)
	if err != nil {
		return "", errors.Wrap(err, "expand working directory")
	}

	// Lock session to prevent concurrent Claude CLI executions
	session.execMu.Lock()
	defer session.execMu.Unlock()

	// Try --resume first (works if session exists), fall back to --session-id if not found
	output, err := executeClaudeWithArgs(
		ctx,
		[]string{"-p", "--resume", session.ID, prompt},
		claudePath,
		workingDir,
	)
	if err == nil {
		return output, nil
	}

	// If session not found, create it with --session-id
	if strings.Contains(err.Error(), "No conversation found") {
		return executeClaudeWithArgs(
			ctx,
			[]string{"-p", "--session-id", session.ID, prompt},
			claudePath,
			workingDir,
		)
	}

	return "", err
}

func executeClaudeWithArgs(
	ctx context.Context,
	args []string,
	claudePath string,
	workingDir string,
) (string, error) {
	// #nosec G204 -- claudePath is from configuration, not user input
	cmd := exec.CommandContext(ctx, claudePath, args...)
	cmd.Dir = workingDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		duration := time.Since(start)
		log.Printf("Claude execution failed after %v: %v", duration, err)

		return "", errors.Wrapf(err, "claude execution failed: %s", stderr.String())
	}

	duration := time.Since(start)
	log.Printf("Claude execution succeeded in %v", duration)

	return strings.TrimSpace(stdout.String()), nil
}

// expandPath expands ~ to home directory
func expandPath(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "get home directory")
	}

	return filepath.Join(home, path[2:]), nil
}
