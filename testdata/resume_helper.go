package main

import (
	"fmt"
	"os"
)

func main() {
	// Check flags
	hasResume := false
	hasSessionID := false

	for _, arg := range os.Args[1:] {
		if arg == "--resume" {
			hasResume = true
		}
		if arg == "--session-id" {
			hasSessionID = true
		}
	}

	if hasResume && !hasSessionID {
		// Simulate resume failure
		fmt.Fprintln(os.Stderr, "No conversation found with ID: test-session")
		os.Exit(1)
	}

	if hasSessionID {
		// Simulate success
		fmt.Println("Success")
		os.Exit(0)
	}

	fmt.Fprintln(os.Stderr, "Unexpected args")
	os.Exit(1)
}
