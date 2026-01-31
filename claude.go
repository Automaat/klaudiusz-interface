package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/cockroachdb/errors"
)

func executeClaude(ctx context.Context, prompt string) (string, error) {
	const maxPromptLength = 100000
	if len(prompt) > maxPromptLength {
		return "", errors.Newf(
			"prompt too long: %d characters (max %d)",
			len(prompt),
			maxPromptLength,
		)
	}

	args := []string{"-p", prompt}

	// #nosec G204 -- ClaudePath is from configuration, not user input
	cmd := exec.CommandContext(ctx, ClaudePath, args...)
	cmd.Dir = WorkingDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", errors.Wrapf(err, "claude execution failed: %s", stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}
