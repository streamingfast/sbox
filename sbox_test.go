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

	if err := os.WriteFile(file1, []byte("Content 1"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("Content 2"), 0644); err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	// Test concatenation
	result, err := ConcatenateMDFiles([]string{file1, file2})
	if err != nil {
		t.Fatalf("ConcatenateMDFiles error = %v", err)
	}

	// Check that result contains both contents and source markers
	if result == "" {
		t.Error("ConcatenateMDFiles returned empty string")
	}

	// Check for source markers
	if !containsString(result, "Source: "+file1) {
		t.Errorf("ConcatenateMDFiles result missing source marker for %s", file1)
	}
	if !containsString(result, "Source: "+file2) {
		t.Errorf("ConcatenateMDFiles result missing source marker for %s", file2)
	}
	if !containsString(result, "Content 1") {
		t.Error("ConcatenateMDFiles result missing Content 1")
	}
	if !containsString(result, "Content 2") {
		t.Error("ConcatenateMDFiles result missing Content 2")
	}
}

func TestConcatenateMDFiles_Empty(t *testing.T) {
	result, err := ConcatenateMDFiles([]string{})
	if err != nil {
		t.Fatalf("ConcatenateMDFiles error = %v", err)
	}
	if result != "" {
		t.Errorf("ConcatenateMDFiles([]) = %q, want empty string", result)
	}
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
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("Failed to create workspace: %v", err)
	}

	claudeMD := filepath.Join(workspaceDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte("# Test CLAUDE.md\nSome content"), 0644); err != nil {
		t.Fatalf("Failed to create CLAUDE.md: %v", err)
	}

	// Prepare MD for sandbox
	outputPath, err := PrepareMDForSandbox(workspaceDir)
	if err != nil {
		t.Fatalf("PrepareMDForSandbox() error = %v", err)
	}

	// Check output file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("PrepareMDForSandbox() output file does not exist: %s", outputPath)
	}

	// Check output path is per-project (contains project hash)
	projectHash, _ := ProjectHash(workspaceDir)
	if !containsString(outputPath, projectHash) {
		t.Errorf("PrepareMDForSandbox() output path %q should contain project hash %q", outputPath, projectHash)
	}

	// Read and verify content
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if !containsString(string(content), "Test CLAUDE.md") {
		t.Error("Output file missing expected content")
	}
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
		Path: "/tmp/.sbox",
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
			output: `SANDBOX ID                                                         TEMPLATE                               NAME                               WORKSPACE                                                  STATUS    CREATED
9bce5b789ffd7460195a5c3d7aac9e5dc181c04f1c50135392e3f2d220a765c5   docker/sandbox-templates:claude-code   claude-sandbox-2026-01-27-103821   /Users/maoueh/work/sf/substreams-eth-uni-v4-demo-candles   running   2026-01-27 15:38:21
`,
			expected: []DockerSandbox{
				{
					ID:        "9bce5b789ffd7460195a5c3d7aac9e5dc181c04f1c50135392e3f2d220a765c5",
					Image:     "docker/sandbox-templates:claude-code",
					Name:      "claude-sandbox-2026-01-27-103821",
					Workspace: "/Users/maoueh/work/sf/substreams-eth-uni-v4-demo-candles",
					Status:    "running",
				},
			},
		},
		{
			name:     "empty output",
			output:   "SANDBOX ID   TEMPLATE   NAME   WORKSPACE   STATUS   CREATED\n",
			expected: nil,
		},
		{
			name: "multiple sandboxes",
			output: `SANDBOX ID                                                         TEMPLATE                               NAME                               WORKSPACE                                                  STATUS    CREATED
9bce5b789ffd7460195a5c3d7aac9e5dc181c04f1c50135392e3f2d220a765c5   docker/sandbox-templates:claude-code   claude-sandbox-2026-01-27-103821   /Users/maoueh/work/sf/substreams-eth-uni-v4-demo-candles   running   2026-01-27 15:38:21
abcd1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd   docker/sandbox-templates:claude-code   claude-sandbox-2026-01-27-120000   /Users/maoueh/work/sf/other-project                       stopped   2026-01-27 17:00:00
`,
			expected: []DockerSandbox{
				{
					ID:        "9bce5b789ffd7460195a5c3d7aac9e5dc181c04f1c50135392e3f2d220a765c5",
					Image:     "docker/sandbox-templates:claude-code",
					Name:      "claude-sandbox-2026-01-27-103821",
					Workspace: "/Users/maoueh/work/sf/substreams-eth-uni-v4-demo-candles",
					Status:    "running",
				},
				{
					ID:        "abcd1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd",
					Image:     "docker/sandbox-templates:claude-code",
					Name:      "claude-sandbox-2026-01-27-120000",
					Workspace: "/Users/maoueh/work/sf/other-project",
					Status:    "stopped",
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
