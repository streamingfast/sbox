package sbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectHash(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantLen   int
		wantSame  bool
		compareTo string
	}{
		{
			name:    "generates 12 char hash",
			path:    "/tmp/test-project",
			wantLen: 12,
		},
		{
			name:      "same path produces same hash",
			path:      "/tmp/same-project",
			wantSame:  true,
			compareTo: "/tmp/same-project",
		},
		{
			name:      "different paths produce different hashes",
			path:      "/tmp/project-a",
			wantSame:  false,
			compareTo: "/tmp/project-b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := ProjectHash(tt.path)
			if err != nil {
				t.Fatalf("ProjectHash(%q) error = %v", tt.path, err)
			}

			if tt.wantLen > 0 && len(hash) != tt.wantLen {
				t.Errorf("ProjectHash(%q) = %q, want length %d, got %d", tt.path, hash, tt.wantLen, len(hash))
			}

			if tt.compareTo != "" {
				hash2, err := ProjectHash(tt.compareTo)
				if err != nil {
					t.Fatalf("ProjectHash(%q) error = %v", tt.compareTo, err)
				}

				if tt.wantSame && hash != hash2 {
					t.Errorf("ProjectHash(%q) = %q, want same as ProjectHash(%q) = %q", tt.path, hash, tt.compareTo, hash2)
				}
				if !tt.wantSame && hash == hash2 {
					t.Errorf("ProjectHash(%q) = %q, want different from ProjectHash(%q) = %q", tt.path, hash, tt.compareTo, hash2)
				}
			}
		})
	}
}

func TestDiscoverMDFiles(t *testing.T) {
	// Create a temporary directory structure
	tempDir := t.TempDir()

	// Create nested directory structure
	level1 := filepath.Join(tempDir, "level1")
	level2 := filepath.Join(level1, "level2")
	level3 := filepath.Join(level2, "level3")

	if err := os.MkdirAll(level3, 0755); err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}

	// Create CLAUDE.md files at different levels
	rootClaude := filepath.Join(tempDir, "CLAUDE.md")
	level1Claude := filepath.Join(level1, "CLAUDE.md")
	level3Claude := filepath.Join(level3, "CLAUDE.md")

	// Create AGENTS.md at level2
	level2Agents := filepath.Join(level2, "AGENTS.md")

	testFiles := map[string]string{
		rootClaude:   "# Root CLAUDE.md",
		level1Claude: "# Level 1 CLAUDE.md",
		level2Agents: "# Level 2 AGENTS.md",
		level3Claude: "# Level 3 CLAUDE.md",
	}

	for path, content := range testFiles {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", path, err)
		}
	}

	// Test discovery from level3
	files, err := DiscoverMDFiles(level3)
	if err != nil {
		t.Fatalf("DiscoverMDFiles(%q) error = %v", level3, err)
	}

	// Should find files in order from root to level3
	if len(files) != 4 {
		t.Errorf("DiscoverMDFiles(%q) found %d files, want 4", level3, len(files))
	}

	// Verify order: root -> level1 -> level2 agents -> level3
	expectedOrder := []string{rootClaude, level1Claude, level2Agents, level3Claude}
	for i, expected := range expectedOrder {
		if i >= len(files) {
			break
		}
		if files[i] != expected {
			t.Errorf("DiscoverMDFiles files[%d] = %q, want %q", i, files[i], expected)
		}
	}
}

func TestConcatenateMDFiles(t *testing.T) {
	tempDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(tempDir, "file1.md")
	file2 := filepath.Join(tempDir, "file2.md")

	require.NoError(t, os.WriteFile(file1, []byte("Content 1"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("Content 2"), 0644))

	// Test concatenation with sandbox backend
	result, err := ConcatenateMDFiles([]string{file1, file2}, BackendSandbox)
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Embedded sandbox backend context should be first
	assert.Contains(t, result, "sbox (embedded sandbox backend instructions)")
	assert.Contains(t, result, "Docker Sandbox Backend")

	// Check for source markers and content
	assert.Contains(t, result, "Source: "+file1)
	assert.Contains(t, result, "Source: "+file2)
	assert.Contains(t, result, "Content 1")
	assert.Contains(t, result, "Content 2")
}

func TestConcatenateMDFiles_ContainerBackend(t *testing.T) {
	tempDir := t.TempDir()

	file1 := filepath.Join(tempDir, "file1.md")
	require.NoError(t, os.WriteFile(file1, []byte("Content 1"), 0644))

	// Test concatenation with container backend
	result, err := ConcatenateMDFiles([]string{file1}, BackendContainer)
	require.NoError(t, err)

	// Should have container backend context
	assert.Contains(t, result, "sbox (embedded container backend instructions)")
	assert.Contains(t, result, "Docker Container Backend")
	assert.Contains(t, result, "Content 1")
}

func TestConcatenateMDFiles_Empty(t *testing.T) {
	result, err := ConcatenateMDFiles([]string{}, BackendSandbox)
	require.NoError(t, err)

	// Even with no files, we should have the embedded backend context
	assert.Contains(t, result, "sbox (embedded sandbox backend instructions)")
	assert.Contains(t, result, "Docker Sandbox Backend")
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Save and restore HOME to test with clean state
	origHome := os.Getenv("HOME")
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// Check defaults
	if config.DockerSocket != "auto" {
		t.Errorf("config.DockerSocket = %q, want %q", config.DockerSocket, "auto")
	}

	expectedClaudeHome := filepath.Join(tempDir, ".claude")
	if config.ClaudeHome != expectedClaudeHome {
		t.Errorf("config.ClaudeHome = %q, want %q", config.ClaudeHome, expectedClaudeHome)
	}

	expectedSboxDir := filepath.Join(tempDir, ".config", "sbox")
	if config.SboxDataDir != expectedSboxDir {
		t.Errorf("config.SboxDataDir = %q, want %q", config.SboxDataDir, expectedSboxDir)
	}
}

func TestGetProjectConfig_Defaults(t *testing.T) {
	// Save and restore HOME
	origHome := os.Getenv("HOME")
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	workspaceDir := filepath.Join(tempDir, "my-project")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("Failed to create workspace dir: %v", err)
	}

	projectConfig, projectHash, err := GetProjectConfig(workspaceDir)
	if err != nil {
		t.Fatalf("GetProjectConfig() error = %v", err)
	}

	if projectHash == "" {
		t.Error("GetProjectConfig returned empty project hash")
	}

	if len(projectConfig.Profiles) != 0 {
		t.Errorf("projectConfig.Profiles = %v, want empty slice", projectConfig.Profiles)
	}
}

func TestSaveAndLoadProjectConfig(t *testing.T) {
	// Save and restore HOME
	origHome := os.Getenv("HOME")
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	workspaceDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("Failed to create workspace dir: %v", err)
	}

	// Save project config with profiles
	projectConfig := &ProjectConfig{
		Profiles: []string{"go", "rust"},
	}

	if err := SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		t.Fatalf("SaveProjectConfig() error = %v", err)
	}

	// Load it back
	loadedConfig, _, err := GetProjectConfig(workspaceDir)
	if err != nil {
		t.Fatalf("GetProjectConfig() error = %v", err)
	}

	if len(loadedConfig.Profiles) != 2 {
		t.Fatalf("loadedConfig.Profiles has %d items, want 2", len(loadedConfig.Profiles))
	}

	if loadedConfig.Profiles[0] != "go" || loadedConfig.Profiles[1] != "rust" {
		t.Errorf("loadedConfig.Profiles = %v, want [go, rust]", loadedConfig.Profiles)
	}
}

func TestGetProfile(t *testing.T) {
	// Test builtin profiles exist
	profiles := []string{"go", "rust", "docker", "bash-utils"}
	for _, name := range profiles {
		profile, ok := GetProfile(name)
		if !ok {
			t.Errorf("GetProfile(%q) not found", name)
			continue
		}
		if profile.Name != name {
			t.Errorf("GetProfile(%q).Name = %q, want %q", name, profile.Name, name)
		}
		if profile.Description == "" {
			t.Errorf("GetProfile(%q).Description is empty", name)
		}
		if profile.DockerfileSnippet == "" {
			t.Errorf("GetProfile(%q).DockerfileSnippet is empty", name)
		}
	}

	// Test non-existent profile
	_, ok := GetProfile("nonexistent")
	if ok {
		t.Error("GetProfile(\"nonexistent\") should return false")
	}
}

func TestListProfiles(t *testing.T) {
	profiles := ListProfiles()
	if len(profiles) < 4 {
		t.Errorf("ListProfiles() returned %d profiles, want at least 4", len(profiles))
	}

	// Check that expected profiles are included
	expected := map[string]bool{"go": false, "rust": false, "docker": false, "bash-utils": false}
	for _, p := range profiles {
		if _, ok := expected[p]; ok {
			expected[p] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("ListProfiles() missing expected profile %q", name)
		}
	}
}

func TestPrepareMDForSandbox(t *testing.T) {
	// Save and restore HOME
	origHome := os.Getenv("HOME")
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	// Create workspace with CLAUDE.md
	workspaceDir := filepath.Join(tempDir, "workspace")
	require.NoError(t, os.MkdirAll(workspaceDir, 0755))

	claudeMD := filepath.Join(workspaceDir, "CLAUDE.md")
	require.NoError(t, os.WriteFile(claudeMD, []byte("# Test CLAUDE.md\nSome content"), 0644))

	// Test with sandbox backend
	outputPath, err := PrepareMDForSandbox(workspaceDir, BackendSandbox)
	require.NoError(t, err)

	// Check output file exists
	_, err = os.Stat(outputPath)
	require.NoError(t, err, "output file should exist")

	// Check output path is per-project (contains project hash)
	projectHash, _ := ProjectHash(workspaceDir)
	assert.Contains(t, outputPath, projectHash)

	// Read and verify content
	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	// Should contain user content
	assert.Contains(t, string(content), "Test CLAUDE.md")

	// Should contain sandbox backend context
	assert.Contains(t, string(content), "Docker Sandbox Backend")
	assert.Contains(t, string(content), "MicroVM")
}

func TestPrepareMDForSandbox_ContainerBackend(t *testing.T) {
	// Save and restore HOME
	origHome := os.Getenv("HOME")
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	// Create workspace with CLAUDE.md
	workspaceDir := filepath.Join(tempDir, "workspace")
	require.NoError(t, os.MkdirAll(workspaceDir, 0755))

	claudeMD := filepath.Join(workspaceDir, "CLAUDE.md")
	require.NoError(t, os.WriteFile(claudeMD, []byte("# Test CLAUDE.md\nSome content"), 0644))

	// Test with container backend
	outputPath, err := PrepareMDForSandbox(workspaceDir, BackendContainer)
	require.NoError(t, err)

	// Read and verify content
	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	// Should contain user content
	assert.Contains(t, string(content), "Test CLAUDE.md")

	// Should contain container backend context
	assert.Contains(t, string(content), "Docker Container Backend")
	assert.Contains(t, string(content), "--docker-socket")
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		wantFrontmatter   string
		wantBody          string
		wantHasFrontmatter bool
	}{
		{
			name: "valid frontmatter",
			content: `---
name: test-agent
description: "Test agent"
---

This is the body content.`,
			wantFrontmatter:   "name: test-agent\ndescription: \"Test agent\"",
			wantBody:          "This is the body content.",
			wantHasFrontmatter: true,
		},
		{
			name:              "no frontmatter",
			content:           "Just body content without frontmatter.",
			wantFrontmatter:   "",
			wantBody:          "Just body content without frontmatter.",
			wantHasFrontmatter: false,
		},
		{
			name:              "unclosed frontmatter",
			content:           "---\nname: test\nno closing delimiter",
			wantFrontmatter:   "",
			wantBody:          "---\nname: test\nno closing delimiter",
			wantHasFrontmatter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter, body, err := splitFrontmatter(tt.content)
			if err != nil {
				t.Fatalf("splitFrontmatter() error = %v", err)
			}

			if tt.wantHasFrontmatter && frontmatter == "" {
				t.Error("splitFrontmatter() expected frontmatter but got empty")
			}
			if !tt.wantHasFrontmatter && frontmatter != "" {
				t.Errorf("splitFrontmatter() expected no frontmatter but got %q", frontmatter)
			}

			if !containsString(body, tt.wantBody) {
				t.Errorf("splitFrontmatter() body = %q, want to contain %q", body, tt.wantBody)
			}
		})
	}
}

func TestParseAgentMarkdown(t *testing.T) {
	tempDir := t.TempDir()

	// Create a test agent file
	agentContent := `---
color: cyan
description: "Test agent for testing"
model: opus
name: test-agent
---

You are a test agent. Do testing things.

## Instructions
- Test stuff
- More testing
`
	agentPath := filepath.Join(tempDir, "test-agent.md")
	if err := os.WriteFile(agentPath, []byte(agentContent), 0644); err != nil {
		t.Fatalf("Failed to create test agent file: %v", err)
	}

	agent, name, err := parseAgentMarkdown(agentPath)
	if err != nil {
		t.Fatalf("parseAgentMarkdown() error = %v", err)
	}

	if name != "test-agent" {
		t.Errorf("parseAgentMarkdown() name = %q, want %q", name, "test-agent")
	}

	if agent.Description != "Test agent for testing" {
		t.Errorf("parseAgentMarkdown() description = %q, want %q", agent.Description, "Test agent for testing")
	}

	if agent.Model != "opus" {
		t.Errorf("parseAgentMarkdown() model = %q, want %q", agent.Model, "opus")
	}

	if !containsString(agent.Prompt, "You are a test agent") {
		t.Errorf("parseAgentMarkdown() prompt should contain 'You are a test agent', got %q", agent.Prompt)
	}
}

func TestBuildAgentsJSON(t *testing.T) {
	tempDir := t.TempDir()

	// Create agents directory
	agentsDir := filepath.Join(tempDir, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("Failed to create agents directory: %v", err)
	}

	// Create test agent files
	agent1 := `---
name: agent-one
description: "First test agent"
model: sonnet
---

You are agent one.
`
	agent2 := `---
name: agent-two
description: "Second test agent"
---

You are agent two.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "agent-one.md"), []byte(agent1), 0644); err != nil {
		t.Fatalf("Failed to create agent-one.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "agent-two.md"), []byte(agent2), 0644); err != nil {
		t.Fatalf("Failed to create agent-two.md: %v", err)
	}

	// Build agents JSON
	jsonStr, err := buildAgentsJSON(tempDir)
	if err != nil {
		t.Fatalf("buildAgentsJSON() error = %v", err)
	}

	if jsonStr == "" {
		t.Fatal("buildAgentsJSON() returned empty string")
	}

	// Check that JSON contains expected keys
	if !containsString(jsonStr, "agent-one") {
		t.Error("buildAgentsJSON() JSON missing 'agent-one'")
	}
	if !containsString(jsonStr, "agent-two") {
		t.Error("buildAgentsJSON() JSON missing 'agent-two'")
	}
	if !containsString(jsonStr, "First test agent") {
		t.Error("buildAgentsJSON() JSON missing 'First test agent' description")
	}
}

func TestBuildClaudeCLIArgs(t *testing.T) {
	flags := &ClaudeFlags{
		AgentsJSON: `{"test": {"description": "test"}}`,
		PluginDirs: []string{"/mnt/plugins1", "/mnt/plugins2"},
		SettingsJSON: `{"key": "value"}`,
	}

	args := buildClaudeCLIArgs(flags)

	// Should have: --agents, --plugin-dir (x2), --settings
	expectedCount := 8 // 2 + 4 + 2
	if len(args) != expectedCount {
		t.Errorf("buildClaudeCLIArgs() returned %d args, want %d", len(args), expectedCount)
	}

	// Check --agents flag
	foundAgents := false
	for i, arg := range args {
		if arg == "--agents" && i+1 < len(args) {
			foundAgents = true
			if args[i+1] != flags.AgentsJSON {
				t.Errorf("--agents value = %q, want %q", args[i+1], flags.AgentsJSON)
			}
		}
	}
	if !foundAgents {
		t.Error("buildClaudeCLIArgs() missing --agents flag")
	}

	// Check --plugin-dir flags
	pluginDirCount := 0
	for _, arg := range args {
		if arg == "--plugin-dir" {
			pluginDirCount++
		}
	}
	if pluginDirCount != 2 {
		t.Errorf("buildClaudeCLIArgs() has %d --plugin-dir flags, want 2", pluginDirCount)
	}
}

func TestGetInstalledPluginPaths(t *testing.T) {
	tempDir := t.TempDir()

	// Create plugins directory structure with actual plugin directories
	pluginsDir := filepath.Join(tempDir, "plugins")
	cacheDir := filepath.Join(pluginsDir, "cache")
	require.NoError(t, os.MkdirAll(pluginsDir, 0755))

	// Create the actual plugin install paths (they must exist)
	plugin1Path := filepath.Join(cacheDir, "official", "test-plugin", "abc123")
	plugin2Path := filepath.Join(cacheDir, "official", "other-plugin", "def456")
	require.NoError(t, os.MkdirAll(plugin1Path, 0755))
	require.NoError(t, os.MkdirAll(plugin2Path, 0755))

	// Create installed_plugins.json
	installedPlugins := InstalledPlugins{
		Version: 2,
		Plugins: map[string][]InstalledPluginEntry{
			"test-plugin@official": {
				{
					Scope:       "local",
					ProjectPath: "/Users/testuser/project",
					InstallPath: plugin1Path,
					Version:     "abc123",
					InstalledAt: "2026-01-15T00:00:00.000Z",
					LastUpdated: "2026-01-19T00:00:00.000Z",
					IsLocal:     true,
				},
			},
			"other-plugin@official": {
				{
					Scope:       "global",
					InstallPath: plugin2Path,
					Version:     "def456",
					InstalledAt: "2026-01-15T00:00:00.000Z",
					LastUpdated: "2026-01-19T00:00:00.000Z",
				},
			},
		},
	}

	jsonBytes, err := json.MarshalIndent(installedPlugins, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), jsonBytes, 0644))

	// Call the function
	paths, err := GetInstalledPluginPaths(tempDir)
	require.NoError(t, err)
	require.NotNil(t, paths)

	// Check host cache path
	assert.Equal(t, cacheDir, paths.HostCachePath)

	// Check container paths
	require.Len(t, paths.ContainerPaths, 2)
	for _, cp := range paths.ContainerPaths {
		assert.True(t, strings.HasPrefix(cp, PluginCacheMountPath))
	}
}

func TestGetInstalledPluginPaths_NoFile(t *testing.T) {
	tempDir := t.TempDir()

	// No plugins directory exists
	paths, err := GetInstalledPluginPaths(tempDir)
	require.NoError(t, err)
	assert.Nil(t, paths)
}

func TestGetInstalledPluginPaths_NoCacheDir(t *testing.T) {
	tempDir := t.TempDir()

	// Create plugins directory but no cache
	pluginsDir := filepath.Join(tempDir, "plugins")
	require.NoError(t, os.MkdirAll(pluginsDir, 0755))

	// Create installed_plugins.json
	installedPlugins := InstalledPlugins{
		Version: 2,
		Plugins: map[string][]InstalledPluginEntry{},
	}
	jsonBytes, err := json.MarshalIndent(installedPlugins, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), jsonBytes, 0644))

	// Call the function - should return nil since cache dir doesn't exist
	paths, err := GetInstalledPluginPaths(tempDir)
	require.NoError(t, err)
	assert.Nil(t, paths)
}

func TestGetInstalledPluginPaths_MissingPluginDir(t *testing.T) {
	tempDir := t.TempDir()

	// Create plugins directory with cache
	pluginsDir := filepath.Join(tempDir, "plugins")
	cacheDir := filepath.Join(pluginsDir, "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create installed_plugins.json with a path that doesn't exist
	installedPlugins := InstalledPlugins{
		Version: 2,
		Plugins: map[string][]InstalledPluginEntry{
			"missing-plugin@official": {
				{
					Scope:       "local",
					InstallPath: filepath.Join(cacheDir, "nonexistent", "plugin"),
					Version:     "abc123",
					InstalledAt: "2026-01-15T00:00:00.000Z",
					LastUpdated: "2026-01-19T00:00:00.000Z",
				},
			},
		},
	}

	jsonBytes, err := json.MarshalIndent(installedPlugins, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), jsonBytes, 0644))

	// Call the function - should return nil (no valid plugins found)
	paths, err := GetInstalledPluginPaths(tempDir)
	require.NoError(t, err)
	assert.Nil(t, paths)
}

func TestEnvName(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{"FOO", "FOO"},
		{"FOO=bar", "FOO"},
		{"FOO=", "FOO"},
		{"FOO=bar=baz", "FOO"},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			assert.Equal(t, tt.want, EnvName(tt.spec))
		})
	}
}

func TestProjectConfigEnvs(t *testing.T) {
	// Save and restore HOME
	origHome := os.Getenv("HOME")
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	workspaceDir := filepath.Join(tempDir, "env-project")
	require.NoError(t, os.MkdirAll(workspaceDir, 0755))

	// Save project config with envs
	projectConfig := &ProjectConfig{
		Envs: []string{"FOO=bar", "BAZ"},
	}
	require.NoError(t, SaveProjectConfig(workspaceDir, projectConfig))

	// Load it back
	loaded, _, err := GetProjectConfig(workspaceDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"FOO=bar", "BAZ"}, loaded.Envs)
}

func TestMergeProjectConfigEnvs(t *testing.T) {
	projectConfig := &ProjectConfig{
		Envs: []string{"FOO=bar", "SHARED=old"},
	}

	sboxFile := &SboxFileLocation{
		Path: "/tmp/sbox.yaml",
		Dir:  "/tmp",
		Config: &SboxFileConfig{
			Envs: []string{"BAZ=qux", "SHARED=new_ignored"},
		},
	}

	merged, err := MergeProjectConfig(projectConfig, sboxFile)
	require.NoError(t, err)

	// BAZ should be added, SHARED should NOT be duplicated (project wins)
	assert.Equal(t, []string{"FOO=bar", "SHARED=old", "BAZ=qux"}, merged.Envs)
}

func TestMergeEnvs(t *testing.T) {
	tests := []struct {
		name       string
		global     []string
		project    []string
		sboxFile   []string
		wantMerged []string
		wantSources []string // parallel to wantMerged
	}{
		{
			name:       "empty",
			wantMerged: nil,
		},
		{
			name:        "global only",
			global:      []string{"FOO=bar"},
			wantMerged:  []string{"FOO=bar"},
			wantSources: []string{"global"},
		},
		{
			name:        "project overrides global",
			global:      []string{"FOO=global", "BAZ=keep"},
			project:     []string{"FOO=project"},
			wantMerged:  []string{"FOO=project", "BAZ=keep"},
			wantSources: []string{"project", "global"},
		},
		{
			name:        "sbox overrides global, project overrides sbox",
			global:      []string{"A=global"},
			sboxFile:    []string{"A=sbox", "B=sbox"},
			project:     []string{"B=project", "C=project"},
			wantMerged:  []string{"A=sbox", "B=project", "C=project"},
			wantSources: []string{".sbox", "project", "project"},
		},
		{
			name:        "passthrough preserved",
			global:      []string{"TOKEN"},
			project:     []string{"DEBUG=1"},
			wantMerged:  []string{"TOKEN", "DEBUG=1"},
			wantSources: []string{"global", "project"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged, resolved := MergeEnvs(tt.global, tt.project, tt.sboxFile)
			assert.Equal(t, tt.wantMerged, merged)

			if tt.wantSources != nil {
				require.Len(t, resolved, len(tt.wantSources))
				for i, r := range resolved {
					assert.Equal(t, tt.wantSources[i], r.Source, "source mismatch for %s", r.Spec)
				}
			}
		})
	}
}

func TestResolveProfiles(t *testing.T) {
	tests := []struct {
		name     string
		profiles []string
		want     []string
	}{
		{
			name:     "no profiles",
			profiles: nil,
			want:     nil,
		},
		{
			name:     "single profile without dependencies",
			profiles: []string{"go"},
			want:     []string{"go"},
		},
		{
			name:     "profile with dependencies",
			profiles: []string{"substreams"},
			want:     []string{"rust", "substreams"},
		},
		{
			name:     "multiple profiles with shared dependency",
			profiles: []string{"substreams", "rust"},
			want:     []string{"rust", "substreams"}, // rust only appears once
		},
		{
			name:     "dependency already listed first",
			profiles: []string{"rust", "substreams"},
			want:     []string{"rust", "substreams"},
		},
		{
			name:     "multiple independent profiles",
			profiles: []string{"go", "docker"},
			want:     []string{"go", "docker"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{}
			builder := NewTemplateBuilder(config, tt.profiles)
			got := builder.ResolveProfiles()

			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestSubstreamsProfile(t *testing.T) {
	profile, ok := GetProfile("substreams")
	require.True(t, ok, "substreams profile should exist")

	assert.Equal(t, "substreams", profile.Name)
	assert.Contains(t, profile.Description, "Substreams")
	assert.Contains(t, profile.Dependencies, "rust")
	assert.Contains(t, profile.DockerfileSnippet, "ghcr.io/streamingfast/substreams")
	assert.Contains(t, profile.DockerfileSnippet, "ghcr.io/streamingfast/firehose-core")
	assert.Contains(t, profile.DockerfileSnippet, "buf")
	assert.Contains(t, profile.DockerfileSnippet, "protoc")
}

func TestParseSandboxLsOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []DockerSandbox
	}{
		{
			name: "single sandbox",
			output: `SANDBOX                        AGENT    STATUS    WORKSPACE
claude-sbox                    claude   running   /Users/maoueh/work/sf/sbox
`,
			expected: []DockerSandbox{
				{
					ID:        "claude-sbox",
					Name:      "claude-sbox",
					Image:     "claude",
					Status:    "running",
					Workspace: "/Users/maoueh/work/sf/sbox",
				},
			},
		},
		{
			name:     "empty output",
			output:   "SANDBOX   AGENT   STATUS   WORKSPACE\n",
			expected: nil,
		},
		{
			name: "multiple sandboxes",
			output: `SANDBOX                        AGENT    STATUS    WORKSPACE
claude-sbox                    claude   running   /Users/maoueh/work/sf/sbox
claude-substreams-benchmarks   claude   stopped   -
sbox-claude-substreams-rs      claude   running   -
`,
			expected: []DockerSandbox{
				{
					ID:        "claude-sbox",
					Name:      "claude-sbox",
					Image:     "claude",
					Status:    "running",
					Workspace: "/Users/maoueh/work/sf/sbox",
				},
				{
					ID:        "claude-substreams-benchmarks",
					Name:      "claude-substreams-benchmarks",
					Image:     "claude",
					Status:    "stopped",
					Workspace: "",
				},
				{
					ID:        "sbox-claude-substreams-rs",
					Name:      "sbox-claude-substreams-rs",
					Image:     "claude",
					Status:    "running",
					Workspace: "",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandboxes, err := parseSandboxLsOutput(tt.output)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, sandboxes)
		})
	}
}
