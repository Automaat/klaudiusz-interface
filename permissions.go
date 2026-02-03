package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ClaudeSettings represents .claude/settings.local.json structure
type ClaudeSettings struct {
	Permissions struct {
		Allow []string `json:"allow"`
	} `json:"permissions"`
	EnableAllProjectMCPServers bool `json:"enableAllProjectMcpServers,omitempty"`
}

// parseMCPPermissionRequest detects if Claude is asking for tool permission
// Returns permission request and true if detected, nil and false otherwise
func parseMCPPermissionRequest(response string, originalQuery string) (*PendingPermission, bool) {
	// Pattern: "Brak uprawnień do <Tool>" or similar
	patterns := []struct {
		regex       *regexp.Regexp
		extractTool func([]string) string
	}{
		{
			// "Brak uprawnień do Todoist" or "Brak uprawnień do <tool>"
			regex: regexp.MustCompile(`(?i)brak uprawnie[ńn] do ([A-Za-z]+)`),
			extractTool: func(matches []string) string {
				return strings.ToLower(matches[1])
			},
		},
		{
			// "Potrzebuję dostępu do <tool>" or similar
			regex: regexp.MustCompile(`(?i)potrzebuj[eę] dost[eę]pu do ([A-Za-z]+)`),
			extractTool: func(matches []string) string {
				return strings.ToLower(matches[1])
			},
		},
	}

	for _, pattern := range patterns {
		matches := pattern.regex.FindStringSubmatch(response)
		if len(matches) > 1 {
			toolName := pattern.extractTool(matches)
			toolPattern := "mcp__" + toolName + "__*"

			// Use cases.Title instead of deprecated strings.Title
			caser := cases.Title(language.Polish)
			titleName := caser.String(toolName)

			return &PendingPermission{
				ID:            "", // Will be set if needed
				ToolPattern:   toolPattern,
				ToolName:      titleName,
				OriginalQuery: originalQuery,
				Description:   "Dostęp do " + titleName,
			}, true
		}
	}

	return nil, false
}

// addPermissionToSettings adds tool permission to .claude/settings.local.json
func addPermissionToSettings(workingDir string, toolPattern string) error {
	settingsPath := filepath.Join(workingDir, ".claude", "settings.local.json")

	// Read existing settings
	var settings ClaudeSettings
	//nolint:gosec // Path is controlled by config, not user input
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrap(err, "failed to read settings file")
		}
		// File doesn't exist, create new
		settings = ClaudeSettings{}
		settings.Permissions.Allow = []string{}
	} else {
		if unmarshalErr := json.Unmarshal(data, &settings); unmarshalErr != nil {
			return errors.Wrap(unmarshalErr, "failed to parse settings JSON")
		}
	}

	// Check if permission already exists
	for _, existing := range settings.Permissions.Allow {
		if existing == toolPattern {
			// Already granted
			return nil
		}
	}

	// Add new permission
	settings.Permissions.Allow = append(settings.Permissions.Allow, toolPattern)

	// Write back
	updatedData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to marshal settings")
	}

	// Ensure .claude directory exists
	//nolint:gosec,mnd // Directory permissions 0755 are standard for config dirs
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return errors.Wrap(err, "failed to create .claude directory")
	}

	//nolint:gosec,mnd // File permissions 0644 are appropriate for config files
	if err := os.WriteFile(settingsPath, updatedData, 0o644); err != nil {
		return errors.Wrap(err, "failed to write settings file")
	}

	return nil
}
