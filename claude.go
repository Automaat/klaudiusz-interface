package main

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

func executeClaude(ctx context.Context, prompt string, session *Session) (string, error) {
	const maxPromptLength = 100000
	if len(prompt) > maxPromptLength {
		return "", errors.Newf(
			"prompt too long: %d characters (max %d)",
			len(prompt),
			maxPromptLength,
		)
	}

	// Try --resume first (works if session exists), fall back to --session-id if not found
	output, err := executeClaudeWithArgs(ctx, []string{"-p", "--resume", session.ID, prompt})
	if err == nil {
		return output, nil
	}

	// If session not found, create it with --session-id
	if strings.Contains(err.Error(), "No conversation found") {
		return executeClaudeWithArgs(ctx, []string{"-p", "--session-id", session.ID, prompt})
	}

	return "", err
}

func executeClaudeWithArgs(ctx context.Context, args []string) (string, error) {
	// #nosec G204 -- ClaudePath is from configuration, not user input
	cmd := exec.CommandContext(ctx, ClaudePath, args...)
	cmd.Dir = WorkingDir

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
