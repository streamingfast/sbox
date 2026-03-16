package sbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentType represents the AI agent to run in the sandbox
type AgentType string

const (
	// AgentClaude uses Claude Code as the AI agent
	AgentClaude AgentType = "claude"
	// AgentOpenCode uses OpenCode as the AI agent
	AgentOpenCode AgentType = "opencode"
)

// DefaultAgent is the default agent type when not specified
const DefaultAgent = AgentClaude

// ValidAgentTypes contains all valid agent type values
var ValidAgentTypes = []AgentType{AgentClaude, AgentOpenCode}

// Capitalize returns a capitalized display name for the agent type.
func (at AgentType) Capitalize() string {
	switch at {
	case AgentClaude:
		return "Claude"
	case AgentOpenCode:
		return "OpenCode"
	default:
		return string(at)
	}
}

// AgentSpec defines the interface for agent-specific implementations
type AgentSpec interface {
	// BinaryName returns the name of the agent binary (e.g., "claude", "opencode")
	BinaryName() string

	// WrapperName returns the name of the wrapper script
	WrapperName() string

	// TemplateImage returns the Docker sandbox template image for this agent
	TemplateImage() string

	// ConfigDirName returns the name of the config directory (e.g., ".claude", ".opencode")
	ConfigDirName() string

	// FindBinary locates the agent binary in standard locations
	// Returns the path to the real binary (e.g., claude-real, opencode-real)
	FindBinary() (string, error)

	// ExecArgs returns the command-line arguments for the agent
	ExecArgs(pluginDirs []string) []string

	// UpdateArgs returns the command-line arguments to update the agent.
	// Returns nil if the agent does not support managed updates.
	UpdateArgs() []string

	// DisableAutoUpdateEnv returns environment variables to set in order to
	// prevent the agent from auto-updating itself (we manage updates via sbox).
	// Returns nil if the agent has no built-in auto-updater.
	DisableAutoUpdateEnv() map[string]string
}

// GetAgentSpec returns the agent specification for the given agent type
func GetAgentSpec(agentType AgentType) AgentSpec {
	switch agentType {
	case AgentClaude:
		return &ClaudeAgent{}
	case AgentOpenCode:
		return &OpenCodeAgent{}
	default:
		return &ClaudeAgent{}
	}
}

// ValidateAgent checks if an agent name is valid
func ValidateAgent(name string) error {
	switch AgentType(name) {
	case AgentClaude, AgentOpenCode:
		return nil
	case "":
		return nil // Empty means use default
	default:
		return fmt.Errorf("invalid agent %q, valid values: %v", name, ValidAgentTypes)
	}
}

// ClaudeAgent implements AgentSpec for Claude Code
type ClaudeAgent struct{}

func (a *ClaudeAgent) BinaryName() string {
	return "claude"
}

func (a *ClaudeAgent) WrapperName() string {
	return "claude-wrapper"
}

func (a *ClaudeAgent) TemplateImage() string {
	return "docker/sandbox-templates:claude-code"
}

func (a *ClaudeAgent) ConfigDirName() string {
	return ".claude"
}

func (a *ClaudeAgent) FindBinary() (string, error) {
	// Check common locations - look for claude-real first (renamed by our wrapper)
	paths := []string{
		"/home/agent/.local/bin/claude-real",
		"/usr/local/bin/claude-real",
		"/usr/bin/claude-real",
		// Fallback to claude in case wrapper wasn't installed
		"/home/agent/.local/bin/claude",
		"/usr/local/bin/claude",
		"/usr/bin/claude",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Try PATH - look for claude-real first
	pathEnv := os.Getenv("PATH")
	for _, dir := range strings.Split(pathEnv, ":") {
		p := filepath.Join(dir, "claude-real")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	for _, dir := range strings.Split(pathEnv, ":") {
		p := filepath.Join(dir, "claude")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("claude not found in known locations or PATH")
}

func (a *ClaudeAgent) ExecArgs(pluginDirs []string) []string {
	// Build argv: claude --dangerously-skip-permissions [--plugin-dir DIR]...
	argv := []string{"claude", "--dangerously-skip-permissions"}
	for _, dir := range pluginDirs {
		argv = append(argv, "--plugin-dir", dir)
	}
	return argv
}

func (a *ClaudeAgent) UpdateArgs() []string {
	return []string{"update"}
}

func (a *ClaudeAgent) DisableAutoUpdateEnv() map[string]string {
	return map[string]string{"DISABLE_AUTOUPDATER": "1"}
}

// OpenCodeAgent implements AgentSpec for OpenCode
type OpenCodeAgent struct{}

func (a *OpenCodeAgent) BinaryName() string {
	return "opencode"
}

func (a *OpenCodeAgent) WrapperName() string {
	return "opencode-wrapper"
}

func (a *OpenCodeAgent) TemplateImage() string {
	return "docker/sandbox-templates:opencode"
}

func (a *OpenCodeAgent) ConfigDirName() string {
	return ".config/opencode"
}

func (a *OpenCodeAgent) FindBinary() (string, error) {
	// Check common locations - look for opencode-real first (renamed by our wrapper)
	paths := []string{
		"/home/agent/.local/bin/opencode-real",
		"/usr/local/bin/opencode-real",
		"/usr/bin/opencode-real",
		// Fallback to opencode in case wrapper wasn't installed
		"/home/agent/.local/bin/opencode",
		"/usr/local/bin/opencode",
		"/usr/bin/opencode",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Try PATH
	pathEnv := os.Getenv("PATH")
	for _, dir := range strings.Split(pathEnv, ":") {
		p := filepath.Join(dir, "opencode-real")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	for _, dir := range strings.Split(pathEnv, ":") {
		p := filepath.Join(dir, "opencode")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("opencode not found in known locations or PATH")
}

func (a *OpenCodeAgent) ExecArgs(pluginDirs []string) []string {
	// Build argv: opencode [workspace_path]
	// OpenCode doesn't have --dangerously-skip-permissions or --plugin-dir flags
	// The workspace path is passed as args by the caller
	argv := []string{"opencode"}
	// TODO: OpenCode plugin support - needs investigation of how OpenCode handles plugins
	return argv
}

func (a *OpenCodeAgent) UpdateArgs() []string {
	// TODO: OpenCode update mechanism not yet supported
	return nil
}

func (a *OpenCodeAgent) DisableAutoUpdateEnv() map[string]string {
	// TODO: OpenCode auto-update disable mechanism not yet known
	return nil
}

// ResolveAgentType determines the effective agent type from configuration sources.
// Priority order (highest to lowest):
// 1. CLI flag (cliAgent parameter)
// 2. sbox.yaml file (sboxFile.Config.Agent)
// 3. Project config (projectConfig.Agent)
// 4. Global config (config.DefaultAgent)
// 5. Hardcoded default (AgentClaude)
func ResolveAgentType(cliAgent string, sboxFile *SboxFileLocation, projectConfig *ProjectConfig, config *Config) AgentType {
	// 1. CLI flag takes highest priority
	if cliAgent != "" {
		return AgentType(cliAgent)
	}

	// 2. sbox.yaml file
	if sboxFile != nil && sboxFile.Config != nil && sboxFile.Config.Agent != "" {
		return AgentType(sboxFile.Config.Agent)
	}

	// 3. Project config
	if projectConfig != nil && projectConfig.Agent != "" {
		return AgentType(projectConfig.Agent)
	}

	// 4. Global config
	if config != nil && config.DefaultAgent != "" {
		return AgentType(config.DefaultAgent)
	}

	// 5. Hardcoded default
	return DefaultAgent
}
