package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseMCPPermissionRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		response      string
		originalQuery string
		wantPattern   string
		wantToolName  string
		wantDetected  bool
	}{
		{
			name:          "Polish - Brak uprawnień do Todoist",
			response:      "Brak uprawnień do Todoist. Potrzebuję dostępu aby dodać zadanie.",
			originalQuery: "dodaj zadanie",
			wantPattern:   "mcp__todoist__*",
			wantToolName:  "Todoist",
			wantDetected:  true,
		},
		{
			name:          "Polish - Brak uprawnien (no polish char)",
			response:      "Brak uprawnien do GitHub. Nie mogę wykonać operacji.",
			originalQuery: "sprawdź PR",
			wantPattern:   "mcp__github__*",
			wantToolName:  "Github",
			wantDetected:  true,
		},
		{
			name:          "Polish - Potrzebuję dostępu",
			response:      "Potrzebuję dostępu do Slack aby wysłać wiadomość.",
			originalQuery: "wyślij wiadomość",
			wantPattern:   "mcp__slack__*",
			wantToolName:  "Slack",
			wantDetected:  true,
		},
		{
			name:          "Polish - Potrzebuje dostepu (no polish chars)",
			response:      "Potrzebuje dostepu do Confluence.",
			originalQuery: "znajdź dokument",
			wantPattern:   "mcp__confluence__*",
			wantToolName:  "Confluence",
			wantDetected:  true,
		},
		{
			name:          "Case insensitive tool name",
			response:      "Brak uprawnień do JIRA. Dodaj uprawnienie.",
			originalQuery: "utwórz issue",
			wantPattern:   "mcp__jira__*",
			wantToolName:  "Jira",
			wantDetected:  true,
		},
		{
			name:          "No permission request",
			response:      "Oto lista zadań na dziś.",
			originalQuery: "pokaż zadania",
			wantDetected:  false,
		},
		{
			name:          "Empty response",
			response:      "",
			originalQuery: "test",
			wantDetected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			perm, detected := parseMCPPermissionRequest(tt.response, tt.originalQuery)

			if detected != tt.wantDetected {
				t.Errorf(
					"parseMCPPermissionRequest() detected = %v, want %v",
					detected,
					tt.wantDetected,
				)
			}

			if !detected {
				if perm != nil {
					t.Error("Expected nil permission when not detected")
				}

				return
			}

			if perm == nil {
				t.Fatal("Expected permission struct when detected")
			}

			if perm.ToolPattern != tt.wantPattern {
				t.Errorf(
					"ToolPattern = %q, want %q",
					perm.ToolPattern,
					tt.wantPattern,
				)
			}

			if perm.ToolName != tt.wantToolName {
				t.Errorf(
					"ToolName = %q, want %q",
					perm.ToolName,
					tt.wantToolName,
				)
			}

			if perm.OriginalQuery != tt.originalQuery {
				t.Errorf(
					"OriginalQuery = %q, want %q",
					perm.OriginalQuery,
					tt.originalQuery,
				)
			}

			expectedDesc := "Dostęp do " + tt.wantToolName
			if perm.Description != expectedDesc {
				t.Errorf(
					"Description = %q, want %q",
					perm.Description,
					expectedDesc,
				)
			}
		})
	}
}

func TestAddPermissionToSettings(t *testing.T) {
	t.Parallel()

	t.Run("Create new settings file", func(t *testing.T) {
		t.Parallel()

		// Create temp directory
		tmpDir := t.TempDir()

		// Add permission
		toolPattern := "mcp__todoist__*"

		err := addPermissionToSettings(tmpDir, toolPattern)
		if err != nil {
			t.Fatalf("addPermissionToSettings() error = %v", err)
		}

		// Verify file was created
		settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")
		if _, statErr := os.Stat(settingsPath); os.IsNotExist(statErr) {
			t.Fatal("Settings file was not created")
		}

		// Read and verify content
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings: %v", err)
		}

		var settings ClaudeSettings
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("Failed to parse settings: %v", err)
		}

		if len(settings.Permissions.Allow) != 1 {
			t.Errorf("Expected 1 permission, got %d", len(settings.Permissions.Allow))
		}

		if settings.Permissions.Allow[0] != toolPattern {
			t.Errorf(
				"Permission = %q, want %q",
				settings.Permissions.Allow[0],
				toolPattern,
			)
		}
	})

	t.Run("Add to existing settings", func(t *testing.T) {
		t.Parallel()

		// Create temp directory
		tmpDir := t.TempDir()

		// Create existing settings
		settingsDir := filepath.Join(tmpDir, ".claude")
		if err := os.MkdirAll(settingsDir, 0o755); err != nil {
			t.Fatalf("Failed to create .claude dir: %v", err)
		}

		existingSettings := ClaudeSettings{}
		existingSettings.Permissions.Allow = []string{"mcp__github__*"}
		data, _ := json.MarshalIndent(existingSettings, "", "  ")

		settingsPath := filepath.Join(settingsDir, "settings.local.json")
		if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
			t.Fatalf("Failed to write existing settings: %v", err)
		}

		// Add new permission
		toolPattern := "mcp__todoist__*"

		err := addPermissionToSettings(tmpDir, toolPattern)
		if err != nil {
			t.Fatalf("addPermissionToSettings() error = %v", err)
		}

		// Read and verify
		data, err = os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings: %v", err)
		}

		var settings ClaudeSettings
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("Failed to parse settings: %v", err)
		}

		if len(settings.Permissions.Allow) != 2 {
			t.Errorf("Expected 2 permissions, got %d", len(settings.Permissions.Allow))
		}

		// Check both permissions exist
		hasGithub := false
		hasTodoist := false

		for _, perm := range settings.Permissions.Allow {
			if perm == "mcp__github__*" {
				hasGithub = true
			}

			if perm == toolPattern {
				hasTodoist = true
			}
		}

		if !hasGithub {
			t.Error("Missing existing github permission")
		}

		if !hasTodoist {
			t.Error("Missing new todoist permission")
		}
	})

	t.Run("Skip duplicate permission", func(t *testing.T) {
		t.Parallel()

		// Create temp directory
		tmpDir := t.TempDir()

		toolPattern := "mcp__todoist__*"

		// Add permission first time
		err := addPermissionToSettings(tmpDir, toolPattern)
		if err != nil {
			t.Fatalf("First addPermissionToSettings() error = %v", err)
		}

		// Add same permission again
		err = addPermissionToSettings(tmpDir, toolPattern)
		if err != nil {
			t.Fatalf("Second addPermissionToSettings() error = %v", err)
		}

		// Verify only one permission exists
		settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")

		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings: %v", err)
		}

		var settings ClaudeSettings
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("Failed to parse settings: %v", err)
		}

		if len(settings.Permissions.Allow) != 1 {
			t.Errorf("Expected 1 permission, got %d", len(settings.Permissions.Allow))
		}
	})

	t.Run("Handle invalid JSON in existing file", func(t *testing.T) {
		t.Parallel()

		// Create temp directory
		tmpDir := t.TempDir()

		// Create invalid JSON file
		settingsDir := filepath.Join(tmpDir, ".claude")
		if err := os.MkdirAll(settingsDir, 0o755); err != nil {
			t.Fatalf("Failed to create .claude dir: %v", err)
		}

		settingsPath := filepath.Join(settingsDir, "settings.local.json")
		if err := os.WriteFile(settingsPath, []byte("invalid json{"), 0o644); err != nil {
			t.Fatalf("Failed to write invalid JSON: %v", err)
		}

		// Try to add permission
		err := addPermissionToSettings(tmpDir, "mcp__todoist__*")
		if err == nil {
			t.Error("Expected error for invalid JSON, got nil")
		}
	})
}
