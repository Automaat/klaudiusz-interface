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

	session.mu.Lock()
	initialized := session.ClaudeInitialized
	session.mu.Unlock()

	args := []string{"-p"}

	if initialized {
		args = append(args, "--resume", session.ID)
	} else {
		args = append(args, "--session-id", session.ID)
	}

	args = append(args, prompt)

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

	// Mark session as initialized after first successful execution
	if !initialized {
		session.mu.Lock()
		session.ClaudeInitialized = true
		session.mu.Unlock()
	}

	return strings.TrimSpace(stdout.String()), nil
}
