package sbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// AgentFrontmatter represents the YAML frontmatter of an agent markdown file
type AgentFrontmatter struct {
	Name        string `yaml:"name" json:"-"` // Used as key, not in JSON value
	Description string `yaml:"description" json:"description"`
	Model       string `yaml:"model,omitempty" json:"model,omitempty"`
	Color       string `yaml:"color,omitempty" json:"-"` // Not supported in --agents JSON
}

// AgentDefinition represents the JSON format expected by --agents flag
type AgentDefinition struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model,omitempty"`
}

// ClaudeFlags holds the CLI flags to pass to the claude command
type ClaudeFlags struct {
	// AgentsJSON is the JSON string for --agents flag
	AgentsJSON string

	// PluginDirs are paths for --plugin-dir flags (inside container)
	PluginDirs []string

	// SettingsJSON is the JSON string for --settings flag
	SettingsJSON string
}

// BuildClaudeFlags constructs CLI flags for the claude command based on user's
// ~/.claude configuration. This allows sharing agents and plugins without
// mounting the ~/.claude directory inside the container.
func BuildClaudeFlags(claudeHome string) (*ClaudeFlags, error) {
	flags := &ClaudeFlags{}

	// Build agents JSON from markdown files
	agentsJSON, err := buildAgentsJSON(claudeHome)
	if err != nil {
		zlog.Warn("failed to build agents JSON, skipping", zap.Error(err))
	} else if agentsJSON != "" {
		flags.AgentsJSON = agentsJSON
	}

	// Note: plugins require volume mounting + --plugin-dir
	// We'll handle this separately in the volume mounting code

	return flags, nil
}

// buildAgentsJSON reads all agent markdown files from ~/.claude/agents
// and converts them to the JSON format expected by --agents flag
func buildAgentsJSON(claudeHome string) (string, error) {
	agentsDir := filepath.Join(claudeHome, "agents")

	// Check if agents directory exists
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		zlog.Debug("agents directory not found, skipping", zap.String("path", agentsDir))
		return "", nil
	}

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return "", err
	}

	agents := make(map[string]AgentDefinition)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		agentPath := filepath.Join(agentsDir, entry.Name())
		agent, name, err := parseAgentMarkdown(agentPath)
		if err != nil {
			zlog.Warn("failed to parse agent file, skipping",
				zap.String("file", entry.Name()),
				zap.Error(err))
			continue
		}

		if name == "" {
			// Use filename without extension as fallback
			name = strings.TrimSuffix(entry.Name(), ".md")
		}

		agents[name] = *agent
		zlog.Debug("loaded agent from markdown",
			zap.String("name", name),
			zap.String("file", entry.Name()))
	}

	if len(agents) == 0 {
		return "", nil
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(agents)
	if err != nil {
		return "", err
	}

	zlog.Info("built agents JSON for --agents flag",
		zap.Int("agent_count", len(agents)))

	return string(jsonBytes), nil
}

// parseAgentMarkdown parses an agent markdown file with YAML frontmatter
// Returns the agent definition and name extracted from frontmatter
func parseAgentMarkdown(path string) (*AgentDefinition, string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}

	// Split frontmatter from content
	frontmatter, prompt, err := splitFrontmatter(string(content))
	if err != nil {
		return nil, "", err
	}

	// Parse frontmatter YAML
	var fm AgentFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		return nil, "", err
	}

	agent := &AgentDefinition{
		Description: fm.Description,
		Prompt:      strings.TrimSpace(prompt),
		Model:       fm.Model,
	}

	return agent, fm.Name, nil
}

// splitFrontmatter splits a markdown file with YAML frontmatter (--- delimited)
// into the frontmatter and body content
func splitFrontmatter(content string) (frontmatter, body string, err error) {
	content = strings.TrimSpace(content)

	// Check for frontmatter delimiter
	if !strings.HasPrefix(content, "---") {
		// No frontmatter, entire content is body
		return "", content, nil
	}

	// Find the closing delimiter
	rest := content[3:] // Skip opening "---"
	endIdx := strings.Index(rest, "\n---")
	if endIdx == -1 {
		// No closing delimiter found, treat as no frontmatter
		return "", content, nil
	}

	frontmatter = strings.TrimSpace(rest[:endIdx])
	body = strings.TrimSpace(rest[endIdx+4:]) // Skip "\n---"

	return frontmatter, body, nil
}

// PluginCacheMountPath is the container path where the plugins cache is mounted
const PluginCacheMountPath = "/mnt/claude-plugins"

// InstalledPlugins represents the installed_plugins.json structure
type InstalledPlugins struct {
	Version int                                `json:"version"`
	Plugins map[string][]InstalledPluginEntry `json:"plugins"`
}

// InstalledPluginEntry represents a single plugin installation entry
type InstalledPluginEntry struct {
	Scope        string `json:"scope"`
	ProjectPath  string `json:"projectPath,omitempty"`
	InstallPath  string `json:"installPath"`
	Version      string `json:"version"`
	InstalledAt  string `json:"installedAt"`
	LastUpdated  string `json:"lastUpdated"`
	GitCommitSha string `json:"gitCommitSha,omitempty"`
	IsLocal      bool   `json:"isLocal,omitempty"`
}

// PluginPaths holds the host cache path and container paths for installed plugins
type PluginPaths struct {
	// HostCachePath is the path to ~/.claude/plugins/cache on the host
	HostCachePath string
	// ContainerPaths are the paths inside the container for each plugin (for --plugin-dir)
	ContainerPaths []string
}

// GetInstalledPluginPaths reads installed_plugins.json and returns plugin path configurations.
// We mount the entire cache directory once, then use --plugin-dir for each plugin's relative path.
// Returns nil if no plugins are installed or cache doesn't exist.
func GetInstalledPluginPaths(claudeHome string) (*PluginPaths, error) {
	pluginsJSONPath := filepath.Join(claudeHome, "plugins", "installed_plugins.json")
	hostCachePath := filepath.Join(claudeHome, "plugins", "cache")

	// Check if the cache directory exists
	if _, err := os.Stat(hostCachePath); os.IsNotExist(err) {
		zlog.Debug("plugins cache directory not found, skipping", zap.String("path", hostCachePath))
		return nil, nil
	}

	// Check if installed_plugins.json exists
	if _, err := os.Stat(pluginsJSONPath); os.IsNotExist(err) {
		zlog.Debug("installed_plugins.json not found, skipping", zap.String("path", pluginsJSONPath))
		return nil, nil
	}

	// Read the file
	content, err := os.ReadFile(pluginsJSONPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read installed_plugins.json: %w", err)
	}

	// Parse JSON
	var plugins InstalledPlugins
	if err := json.Unmarshal(content, &plugins); err != nil {
		return nil, fmt.Errorf("failed to parse installed_plugins.json: %w", err)
	}

	var containerPaths []string

	for pluginName, entries := range plugins.Plugins {
		for _, entry := range entries {
			// Verify the plugin directory exists
			if _, err := os.Stat(entry.InstallPath); os.IsNotExist(err) {
				zlog.Warn("plugin install path not found, skipping",
					zap.String("plugin", pluginName),
					zap.String("path", entry.InstallPath))
				continue
			}

			// Convert host path to container path by replacing the cache prefix
			if !strings.HasPrefix(entry.InstallPath, hostCachePath) {
				zlog.Warn("plugin install path not under cache directory, skipping",
					zap.String("plugin", pluginName),
					zap.String("path", entry.InstallPath),
					zap.String("cache_path", hostCachePath))
				continue
			}

			// Get the relative path from cache directory
			relativePath := strings.TrimPrefix(entry.InstallPath, hostCachePath)
			containerPath := PluginCacheMountPath + relativePath

			containerPaths = append(containerPaths, containerPath)

			zlog.Debug("found installed plugin",
				zap.String("plugin", pluginName),
				zap.String("host_path", entry.InstallPath),
				zap.String("container_path", containerPath))
		}
	}

	if len(containerPaths) == 0 {
		return nil, nil
	}

	zlog.Info("found installed plugins for mounting",
		zap.Int("plugin_count", len(containerPaths)))

	return &PluginPaths{
		HostCachePath:  hostCachePath,
		ContainerPaths: containerPaths,
	}, nil
}

// buildClaudeCLIArgs constructs the CLI arguments to pass to the claude command
// These arguments come after "claude" in the docker sandbox run command
func buildClaudeCLIArgs(flags *ClaudeFlags) []string {
	var args []string

	// Add --agents flag if we have agents JSON
	if flags.AgentsJSON != "" {
		args = append(args, "--agents", flags.AgentsJSON)
		zlog.Debug("adding --agents flag", zap.Int("json_length", len(flags.AgentsJSON)))
	}

	// Add --plugin-dir flags for each plugin directory
	for _, dir := range flags.PluginDirs {
		args = append(args, "--plugin-dir", dir)
		zlog.Debug("adding --plugin-dir flag", zap.String("path", dir))
	}

	// Add --settings flag if we have settings JSON
	if flags.SettingsJSON != "" {
		args = append(args, "--settings", flags.SettingsJSON)
		zlog.Debug("adding --settings flag", zap.Int("json_length", len(flags.SettingsJSON)))
	}

	return args
}
