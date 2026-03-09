package sbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// entrypointLogFile is the path for entrypoint debug logging
const entrypointLogFile = "/tmp/sbox-entrypoint.log"

// elog is the entrypoint file logger (initialized by initEntrypointLog)
var elog *slog.Logger

// initEntrypointLog initializes the file logger for entrypoint debugging.
// Logs are appended to /tmp/sbox-entrypoint.log
func initEntrypointLog() func() {
	f, err := os.OpenFile(entrypointLogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		// Can't log to file, use a no-op logger
		elog = slog.New(slog.NewTextHandler(io.Discard, nil))
		return func() {}
	}

	// Write separator for new run
	fmt.Fprintf(f, "\n========== sbox entrypoint new run at %s ==========\n", time.Now().Format(time.RFC3339))

	elog = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	return func() { f.Close() }
}

// logEnvironment logs relevant environment variables for debugging
func logEnvironment() {
	env := os.Environ()
	sort.Strings(env)

	// Log interesting env vars
	interesting := []string{
		"HOME", "USER", "PWD", "SHELL",
		"WORKSPACE_DIR",
		"CLAUDE", "CLAUDECODE", "IS_SANDBOX",
		"PATH",
	}

	elog.Info("environment snapshot")
	for _, key := range interesting {
		if val := os.Getenv(key); val != "" {
			elog.Debug("env", "key", key, "value", val)
		}
	}

	// Log all env vars at trace level (as debug with prefix)
	elog.Debug("full environment", "count", len(env))
	for _, e := range env {
		elog.Debug("env.full", "entry", e)
	}
}

// EntrypointConfigVersion is the current version of the entrypoint config format.
// Increment this when making breaking changes to the config structure.
const EntrypointConfigVersion = 1

// EntrypointConfig is the configuration file exchanged between `sbox run` (host)
// and `sbox entrypoint` (container). It's written to .sbox/entrypoint.yaml.
type EntrypointConfig struct {
	// Version is the config format version for compatibility checking
	Version int `yaml:"version"`

	// Plugins to install in the sandbox
	Plugins []EntrypointPlugin `yaml:"plugins,omitempty"`

	// Agents to install in the sandbox
	Agents []EntrypointAgent `yaml:"agents,omitempty"`

	// Agent specifies which AI agent to run ("claude" or "opencode")
	Agent string `yaml:"agent,omitempty"`
}

// EntrypointPlugin describes a plugin to be installed in the sandbox
type EntrypointPlugin struct {
	// Name is the plugin identifier (e.g., "test-plugin@official")
	Name string `yaml:"name"`

	// Path is the relative path within .sbox/ where the plugin files are stored
	// e.g., "plugins/official/test-plugin/abc123"
	Path string `yaml:"path"`

	// Version is the plugin version string
	Version string `yaml:"version,omitempty"`

	// PackageVersion is the package/commit hash
	PackageVersion string `yaml:"package_version,omitempty"`
}

// EntrypointAgent describes an agent to be installed in the sandbox
type EntrypointAgent struct {
	// Name is the agent name (filename without .json extension)
	Name string `yaml:"name"`

	// Path is the relative path within .sbox/ where the agent file is stored
	// e.g., "agents/my-agent.json"
	Path string `yaml:"path"`
}

// WriteEntrypointConfig writes the entrypoint configuration to .sbox/entrypoint.yaml
func WriteEntrypointConfig(workspaceDir string, config *EntrypointConfig) error {
	config.Version = EntrypointConfigVersion

	sboxDir := filepath.Join(workspaceDir, ".sbox")
	if err := os.MkdirAll(sboxDir, 0755); err != nil {
		return fmt.Errorf("failed to create .sbox directory: %w", err)
	}

	configPath := filepath.Join(sboxDir, "entrypoint.yaml")

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal entrypoint config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write entrypoint config: %w", err)
	}

	return nil
}

// ReadEntrypointConfig reads the entrypoint configuration from .sbox/entrypoint.yaml
// Returns an error if the config version is incompatible.
func ReadEntrypointConfig(workspaceDir string) (*EntrypointConfig, error) {
	configPath := filepath.Join(workspaceDir, ".sbox", "entrypoint.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read entrypoint config: %w", err)
	}

	var config EntrypointConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse entrypoint config: %w", err)
	}

	if config.Version == 0 {
		return nil, fmt.Errorf("entrypoint config missing version field")
	}

	if config.Version > EntrypointConfigVersion {
		return nil, fmt.Errorf("entrypoint config version %d is newer than supported version %d; please update sbox", config.Version, EntrypointConfigVersion)
	}

	// Future: handle migration from older versions if needed
	// For now, we only support the current version

	return &config, nil
}

// WriteEntrypointEnv writes resolved environment variables to .sbox/env
// Each line is in the format KEY=value (already resolved, no passthrough)
func WriteEntrypointEnv(workspaceDir string, envs []string) error {
	sboxDir := filepath.Join(workspaceDir, ".sbox")
	if err := os.MkdirAll(sboxDir, 0755); err != nil {
		return fmt.Errorf("failed to create .sbox directory: %w", err)
	}

	envPath := filepath.Join(sboxDir, "env")

	// Build content with resolved values
	var content string
	for _, env := range envs {
		content += env + "\n"
	}

	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}

	return nil
}

// ReadEntrypointEnv reads environment variables from .sbox/env
// Returns a slice of KEY=value strings
func ReadEntrypointEnv(workspaceDir string) ([]string, error) {
	envPath := filepath.Join(workspaceDir, ".sbox", "env")

	data, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No env file is OK
		}
		return nil, fmt.Errorf("failed to read env file: %w", err)
	}

	var envs []string
	for _, line := range splitLines(string(data)) {
		line = trimSpace(line)
		if line != "" && !hasPrefix(line, "#") {
			envs = append(envs, line)
		}
	}

	return envs, nil
}

// Helper functions to avoid importing strings package for simple operations
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// SandboxPersistentEnvFile is the path to the persistent environment file in the sandbox.
// We use /etc/profile.d/ rather than /etc/sandbox-persistent.sh because:
//   - /etc/sandbox-persistent.sh is managed by the Docker sandbox infrastructure (CLAUDE_ENV_FILE)
//     and may have permissions that prevent the agent user from writing to it
//   - /etc/profile.d/*.sh files are sourced by login shells (bash -l) which is the recommended
//     invocation pattern in the sandbox CLAUDE.md
const SandboxPersistentEnvFile = "/etc/profile.d/sbox-env.sh"

// ReadWorkspacePath returns the workspace directory.
// Docker sandbox sets PWD and WORKSPACE_DIR to the workspace path.
// Returns empty string if workspace cannot be determined.
func ReadWorkspacePath() string {
	// WORKSPACE_DIR is explicitly set by docker sandbox
	if dir := os.Getenv("WORKSPACE_DIR"); dir != "" {
		return dir
	}
	// Fallback to PWD
	if dir := os.Getenv("PWD"); dir != "" {
		return dir
	}
	// Last resort: get current working directory
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return ""
}

// DefaultSandboxClaudeHome is the default Claude home directory inside the sandbox.
// Docker sandbox may mount the host's .claude at the host path instead.
// Use FindClaudeHome() to locate the actual directory.
const DefaultSandboxClaudeHome = "/home/agent/.claude"

// FindClaudeHome locates Claude's config directory inside the sandbox.
// Docker sandbox mounts the host's .claude at the host path (e.g., /Users/username/.claude).
// We search common locations to find where it actually is.
func FindAgentHome(agentType AgentType) string {
	spec := GetAgentSpec(agentType)
	configDirName := spec.ConfigDirName()

	// Check environment variable first (for backwards compatibility with CLAUDE_CONFIG_DIR)
	envVar := strings.ToUpper(spec.BinaryName()) + "_CONFIG_DIR"
	if dir := os.Getenv(envVar); dir != "" {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}

	// Check default location
	defaultHome := filepath.Join("/home/agent", configDirName)
	if _, err := os.Stat(defaultHome); err == nil {
		return defaultHome
	}

	// Search /Users (macOS host mounts)
	entries, _ := os.ReadDir("/Users")
	for _, e := range entries {
		if e.IsDir() {
			agentDir := filepath.Join("/Users", e.Name(), configDirName)
			if _, err := os.Stat(agentDir); err == nil {
				return agentDir
			}
		}
	}

	// Search /home (Linux host mounts)
	entries, _ = os.ReadDir("/home")
	for _, e := range entries {
		if e.IsDir() && e.Name() != "agent" { // Skip agent, already checked
			agentDir := filepath.Join("/home", e.Name(), configDirName)
			if _, err := os.Stat(agentDir); err == nil {
				return agentDir
			}
		}
	}

	// Fallback to default
	return defaultHome
}

// SboxEntrypointMarkerFile is written when sbox entrypoint runs successfully.
// Claude can check this file to verify the entrypoint ran.
const SboxEntrypointMarkerFile = "/tmp/sbox-entrypoint-ran"

// ClaudeCacheDir is the subdirectory in .sbox/ where we cache the .claude folder
// for persistence across sandbox recreations.
const ClaudeCacheDir = "claude-cache"

// RunEntrypoint executes the entrypoint logic inside the sandbox.
// It reads the configuration from .sbox/, sets up plugins/agents/env,
// then execs claude with the provided arguments.
func RunEntrypoint(args []string) error {
	// Initialize file logger (note: log file won't be closed since we exec)
	_ = initEntrypointLog()

	elog.Info("=== RunEntrypoint starting ===", "args", args)
	logEnvironment()

	zlog.Info("running sbox entrypoint")

	// Read workspace path from env var set by wrapper
	workspaceDir := ReadWorkspacePath()
	elog.Info("workspace lookup", "WORKSPACE_DIR", os.Getenv("WORKSPACE_DIR"), "PWD", os.Getenv("PWD"), "result", workspaceDir)

	if workspaceDir == "" {
		// No workspace found, just exec default agent directly
		elog.Warn("workspace directory not found, exec default agent directly")
		zlog.Info("workspace directory not found, starting default agent directly")
		return execAgent(DefaultAgent, args, nil)
	}

	elog.Info("found workspace directory", "path", workspaceDir)
	zlog.Info("found workspace directory", zap.String("path", workspaceDir))

	// Check for dev override binary. If .sbox/sbox-dev exists, exec it
	// to continue the entrypoint with a newer version of sbox. This enables
	// fast iteration without rebuilding the Docker image.
	if err := maybeExecDevBinary(workspaceDir, args); err != nil {
		// maybeExecDevBinary only returns on error (exec replaces the process on success)
		return err
	}

	// Check .sbox directory contents
	sboxDir := filepath.Join(workspaceDir, ".sbox")
	if entries, err := os.ReadDir(sboxDir); err == nil {
		var files []string
		for _, e := range entries {
			files = append(files, e.Name())
		}
		elog.Info(".sbox directory contents", "path", sboxDir, "files", files)
	} else {
		elog.Warn(".sbox directory not readable", "path", sboxDir, "error", err)
	}

	// Read entrypoint config
	configPath := filepath.Join(sboxDir, "entrypoint.yaml")
	elog.Info("reading entrypoint config", "path", configPath)

	config, err := ReadEntrypointConfig(workspaceDir)
	if err != nil {
		// If no config, just exec default agent directly (backwards compatibility)
		if errors.Is(err, os.ErrNotExist) {
			elog.Warn("no entrypoint config found, exec default agent directly", "path", configPath)
			zlog.Info("no entrypoint config found, starting default agent directly")
			return execAgent(DefaultAgent, args, nil)
		}
		elog.Error("failed to read entrypoint config", "error", err)
		return fmt.Errorf("failed to read entrypoint config: %w", err)
	}

	// Default to claude if no agent specified
	agentType := config.Agent
	if agentType == "" {
		agentType = string(AgentClaude)
	}

	elog.Info("loaded entrypoint config",
		"version", config.Version,
		"plugins", len(config.Plugins),
		"agents", len(config.Agents),
		"agent", agentType)
	zlog.Info("loaded entrypoint config",
		zap.Int("version", config.Version),
		zap.Int("plugins", len(config.Plugins)),
		zap.Int("agents", len(config.Agents)),
		zap.String("agent", agentType))

	// Log plugin details
	for i, p := range config.Plugins {
		elog.Debug("plugin", "index", i, "name", p.Name, "path", p.Path)
	}
	// Log agent details
	for i, a := range config.Agents {
		elog.Debug("agent", "index", i, "name", a.Name, "path", a.Path)
	}

	// Find agent home
	agent := AgentType(agentType)
	agentHome := FindAgentHome(agent)
	elog.Info("agent home directory", "path", agentHome, "agent", agentType)

	// Restore agent cache if present (for persistence across recreations)
	elog.Info("checking for agent cache to restore")
	if err := restoreClaudeCache(workspaceDir, agentHome); err != nil {
		elog.Warn("failed to restore agent cache", "error", err)
		// Non-fatal - continue anyway
	}

	// Setup CLAUDE.md/AGENTS.md (copy from .sbox to agent home)
	elog.Info("setting up CLAUDE.md/AGENTS.md")
	if err := setupRules(workspaceDir, agent); err != nil {
		elog.Error("failed to setup rules file", "error", err)
		// Non-fatal - continue anyway
	}

	// Setup plugins
	elog.Info("setting up plugins", "count", len(config.Plugins))
	if err := setupPlugins(workspaceDir, config.Plugins); err != nil {
		elog.Error("failed to setup plugins", "error", err)
		return fmt.Errorf("failed to setup plugins: %w", err)
	}

	// Setup agents
	elog.Info("setting up agents", "count", len(config.Agents))
	if err := setupAgents(workspaceDir, agent, config.Agents); err != nil {
		elog.Error("failed to setup agents", "error", err)
		return fmt.Errorf("failed to setup agents: %w", err)
	}

	// Setup OpenCode config and auth files
	if agent == AgentOpenCode {
		// Setup opencode.json (config file)
		opencodeConfigSrc := filepath.Join(workspaceDir, ".sbox", "opencode.json")
		if _, err := os.Stat(opencodeConfigSrc); err == nil {
			opencodeConfigDst := filepath.Join(agentHome, "opencode.json")
			elog.Info("copying opencode.json to agent home", "src", opencodeConfigSrc, "dst", opencodeConfigDst)
			if err := copyFile(opencodeConfigSrc, opencodeConfigDst); err != nil {
				elog.Warn("failed to copy opencode.json", "error", err)
				zlog.Warn("failed to copy opencode.json to agent home", zap.Error(err))
				// Non-fatal - continue anyway
			} else {
				elog.Info("successfully copied opencode.json")
				zlog.Info("copied opencode.json to agent home", zap.String("dst", opencodeConfigDst))
			}
		} else {
			elog.Debug("opencode.json not found in .sbox, skipping", "path", opencodeConfigSrc)
			zlog.Debug("opencode.json not found in .sbox, skipping", zap.String("path", opencodeConfigSrc))
		}

		// Setup tui.json (TUI config file)
		opencodeTUIConfigSrc := filepath.Join(workspaceDir, ".sbox", "tui.json")
		if _, err := os.Stat(opencodeTUIConfigSrc); err == nil {
			opencodeTUIConfigDst := filepath.Join(agentHome, "tui.json")
			elog.Info("copying tui.json to agent home", "src", opencodeTUIConfigSrc, "dst", opencodeTUIConfigDst)
			if err := copyFile(opencodeTUIConfigSrc, opencodeTUIConfigDst); err != nil {
				elog.Warn("failed to copy tui.json", "error", err)
				zlog.Warn("failed to copy tui.json to agent home", zap.Error(err))
				// Non-fatal - continue anyway
			} else {
				elog.Info("successfully copied tui.json")
				zlog.Info("copied tui.json to agent home", zap.String("dst", opencodeTUIConfigDst))
			}
		} else {
			elog.Debug("tui.json not found in .sbox, skipping", "path", opencodeTUIConfigSrc)
			zlog.Debug("tui.json not found in .sbox, skipping", zap.String("path", opencodeTUIConfigSrc))
		}

		// Setup auth.json (authentication file)
		opencodeAuthSrc := filepath.Join(workspaceDir, ".sbox", "opencode-auth.json")
		if _, err := os.Stat(opencodeAuthSrc); err == nil {
			// OpenCode auth goes in XDG data directory: ~/.local/share/opencode/auth.json
			authDir := filepath.Join("/home/agent", ".local", "share", "opencode")
			if err := os.MkdirAll(authDir, 0755); err != nil {
				elog.Warn("failed to create auth directory", "dir", authDir, "error", err)
				zlog.Warn("failed to create opencode auth directory", zap.String("dir", authDir), zap.Error(err))
			} else {
				opencodeAuthDst := filepath.Join(authDir, "auth.json")
				elog.Info("copying opencode auth.json to data directory", "src", opencodeAuthSrc, "dst", opencodeAuthDst)
				if err := copyFile(opencodeAuthSrc, opencodeAuthDst); err != nil {
					elog.Warn("failed to copy opencode auth.json", "error", err)
					zlog.Warn("failed to copy opencode auth.json", zap.Error(err))
					// Non-fatal - continue anyway
				} else {
					elog.Info("successfully copied opencode auth.json")
					zlog.Info("copied opencode auth.json to data directory", zap.String("dst", opencodeAuthDst))
				}
			}
		} else {
			elog.Debug("opencode auth.json not found in .sbox, skipping", "path", opencodeAuthSrc)
			zlog.Debug("opencode auth.json not found in .sbox, skipping", zap.String("path", opencodeAuthSrc))
		}
	}

	// Load environment variables
	elog.Info("loading environment variables")
	if err := loadEntrypointEnv(workspaceDir); err != nil {
		elog.Error("failed to load environment", "error", err)
		return fmt.Errorf("failed to load environment: %w", err)
	}

	// Collect plugin directories for --plugin-dir flags
	var pluginDirs []string
	for _, plugin := range config.Plugins {
		// Plugin path is relative to .sbox/, e.g. "plugins/claude-plugins-official/code-simplifier/1.0.0"
		pluginDir := filepath.Join(workspaceDir, ".sbox", plugin.Path)
		if _, err := os.Stat(pluginDir); err == nil {
			pluginDirs = append(pluginDirs, pluginDir)
			elog.Info("adding plugin directory", "name", plugin.Name, "path", pluginDir)
		} else {
			elog.Warn("plugin directory not found", "name", plugin.Name, "path", pluginDir)
		}
	}

	elog.Info("=== setup complete, exec agent ===", "agent", agentType, "pluginDirs", pluginDirs)

	// For OpenCode, if no args provided, pass the workspace directory
	// OpenCode expects: opencode [project]
	agentTypeEnum := AgentType(agentType)
	if agentTypeEnum == AgentOpenCode && len(args) == 0 {
		args = []string{workspaceDir}
		elog.Info("adding workspace path as first arg for OpenCode", "workspace", workspaceDir)
	}

	// Exec the specified agent (replaces current process)
	return execAgent(agentTypeEnum, args, pluginDirs)
}

// setupRules copies .sbox/CLAUDE.md to agent home/CLAUDE.md or AGENTS.md
// depending on the agent type (Claude uses CLAUDE.md, OpenCode uses AGENTS.md)
func setupRules(workspaceDir string, agentType AgentType) error {
	srcPath := filepath.Join(workspaceDir, ".sbox", "CLAUDE.md")

	// Check if source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		zlog.Debug("no rules file in .sbox, skipping")
		return nil
	}

	agentHome := FindAgentHome(agentType)

	// Determine destination filename based on agent type
	// Claude uses CLAUDE.md for backward compatibility
	// OpenCode uses AGENTS.md as the standard name
	var dstFilename string
	if agentType == AgentOpenCode {
		dstFilename = "AGENTS.md"
	} else {
		dstFilename = "CLAUDE.md"
	}
	dstPath := filepath.Join(agentHome, dstFilename)

	// Ensure agent home exists
	if err := os.MkdirAll(agentHome, 0755); err != nil {
		return fmt.Errorf("failed to create agent home: %w", err)
	}

	if err := copyFile(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy %s: %w", dstFilename, err)
	}

	zlog.Info("installed agent instructions file",
		zap.String("agent", string(agentType)),
		zap.String("filename", dstFilename),
		zap.String("src", srcPath),
		zap.String("dst", dstPath))

	return nil
}

// setupPlugins is now a no-op - plugins are loaded via --plugin-dir flag
// The plugin directories in .sbox/plugins/ are passed directly to claude
func setupPlugins(workspaceDir string, plugins []EntrypointPlugin) error {
	// Nothing to do - plugins are loaded via --plugin-dir from .sbox/plugins/
	zlog.Debug("plugins will be loaded via --plugin-dir", zap.Int("count", len(plugins)))
	return nil
}

// setupAgents copies agents from .sbox/agents/ to agent home/agents/
func setupAgents(workspaceDir string, agentType AgentType, agents []EntrypointAgent) error {
	if len(agents) == 0 {
		zlog.Debug("no agents to setup")
		return nil
	}

	agentHome := FindAgentHome(agentType)
	zlog.Info("using agent home directory for agents", zap.String("path", agentHome), zap.String("agent", string(agentType)))

	// Ensure agent home exists with proper permissions
	if err := os.MkdirAll(agentHome, 0755); err != nil {
		return fmt.Errorf("failed to create agent home directory: %w", err)
	}

	agentsDir := filepath.Join(agentHome, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	for _, agent := range agents {
		srcPath := filepath.Join(workspaceDir, ".sbox", agent.Path)
		// Use the original filename from Path to preserve extension (.md or .json)
		dstPath := filepath.Join(agentsDir, filepath.Base(agent.Path))

		zlog.Debug("copying agent",
			zap.String("name", agent.Name),
			zap.String("src", srcPath),
			zap.String("dst", dstPath))

		if err := copyFile(srcPath, dstPath); err != nil {
			zlog.Warn("failed to copy agent, skipping",
				zap.String("name", agent.Name),
				zap.Error(err))
			continue
		}

		zlog.Info("agent installed",
			zap.String("name", agent.Name),
			zap.String("path", dstPath))
	}

	return nil
}

// SboxDevBinaryEnvVar is set when running via the dev override binary to prevent
// infinite recursion (the dev binary would find itself and try to exec again).
const SboxDevBinaryEnvVar = "SBOX_DEV_ENTRYPOINT"

// maybeExecDevBinary checks for .sbox/sbox-dev and execs it if present.
// Returns nil if no dev binary is found (caller should continue normally).
// On successful exec, this function never returns (process is replaced).
// Returns an error only if the binary exists but cannot be executed.
func maybeExecDevBinary(workspaceDir string, args []string) error {
	// Don't recurse: if we ARE the dev binary, skip this check
	if os.Getenv(SboxDevBinaryEnvVar) == "1" {
		elog.Info("running as dev binary, skipping dev override check")
		return nil
	}

	devBinaryPath := filepath.Join(workspaceDir, ".sbox", "sbox-dev")
	if _, err := os.Stat(devBinaryPath); err != nil {
		return nil // No dev binary, continue normally
	}

	elog.Info("found dev override binary, exec'ing it", "path", devBinaryPath)
	zlog.Info("found sbox-dev override binary, replacing entrypoint",
		zap.String("path", devBinaryPath))
	fmt.Fprintf(os.Stderr, "sbox: using dev override binary at %s\n", devBinaryPath)

	// Build argv: sbox-dev entrypoint [args...]
	argv := append([]string{"sbox-dev", "entrypoint"}, args...)

	// Set env var to prevent the dev binary from recursing
	env := os.Environ()
	env = append(env, SboxDevBinaryEnvVar+"=1")

	err := syscall.Exec(devBinaryPath, argv, env)

	// syscall.Exec only returns on error
	if errors.Is(err, syscall.ENOEXEC) {
		return fmt.Errorf(
			"sbox-dev binary at %s has wrong architecture (exec format error); "+
				"rebuild it for the sandbox platform (linux/%s) with: "+
				"GOOS=linux GOARCH=%s go build -o .sbox/sbox-dev ./cmd/sbox",
			devBinaryPath, runtime.GOARCH, runtime.GOARCH,
		)
	}

	return fmt.Errorf("failed to exec dev binary %s: %w", devBinaryPath, err)
}

// loadEntrypointEnv reads .sbox/env and sets env vars in the current process.
// It also attempts to write exports to the persistent env file for login shells.
func loadEntrypointEnv(workspaceDir string) error {
	envs, err := ReadEntrypointEnv(workspaceDir)
	if err != nil {
		return err
	}

	if len(envs) == 0 {
		zlog.Debug("no environment variables to load")
		return nil
	}

	// Parse all env vars first and set them in the current process.
	// This ensures they're available to the exec'd claude process regardless
	// of whether we can write the persistent file.
	type envEntry struct {
		key, value string
	}
	var entries []envEntry
	for _, env := range envs {
		idx := strings.Index(env, "=")
		if idx < 0 {
			continue // Skip invalid entries
		}
		key := env[:idx]
		value := env[idx+1:]

		os.Setenv(key, value)
		entries = append(entries, envEntry{key, value})
		zlog.Debug("loaded environment variable", zap.String("key", key))
	}

	// Write to persistent env file so vars are available in login shells (bash -l).
	// This is non-fatal: if we can't write the file, env vars are still set in
	// the current process and will be inherited by claude via syscall.Exec.
	f, err := os.OpenFile(SandboxPersistentEnvFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		zlog.Warn("could not open persistent env file, env vars will only be available via process environment",
			zap.String("path", SandboxPersistentEnvFile),
			zap.Error(err))
	} else {
		defer f.Close()

		if _, err := f.WriteString("\n# sbox entrypoint environment variables\n"); err != nil {
			zlog.Warn("failed to write to persistent env file", zap.Error(err))
		} else {
			for _, e := range entries {
				exportLine := fmt.Sprintf("export %s=%q\n", e.key, e.value)
				if _, err := f.WriteString(exportLine); err != nil {
					zlog.Warn("failed to write env var to persistent file",
						zap.String("key", e.key), zap.Error(err))
					break
				}
			}
		}
	}

	zlog.Info("loaded environment variables",
		zap.Int("count", len(entries)))

	return nil
}

// execAgent replaces the current process with the specified agent using syscall.Exec
// Uses the AgentSpec interface to get agent-specific configuration
func execAgent(agentType AgentType, args []string, pluginDirs []string) error {
	spec := GetAgentSpec(agentType)

	// Find the agent binary
	binaryPath, err := spec.FindBinary()
	if err != nil {
		if elog != nil {
			elog.Error("failed to find agent binary", "agent", agentType, "error", err)
		}
		fmt.Fprintf(os.Stderr, "\nERROR: %s binary not found in the sandbox.\n", spec.BinaryName())
		fmt.Fprintf(os.Stderr, "%s must be installed in the sandbox to use --agent=%s\n", spec.BinaryName(), agentType)
		fmt.Fprintf(os.Stderr, "Searched locations: /home/agent/.local/bin/%s, /usr/local/bin/%s, /usr/bin/%s\n\n",
			spec.BinaryName(), spec.BinaryName(), spec.BinaryName())
		return fmt.Errorf("failed to find %s: %w", spec.BinaryName(), err)
	}

	// Build argv with agent-specific arguments plus additional args
	argv := spec.ExecArgs(pluginDirs)
	argv = append(argv, args...)

	if elog != nil {
		elog.Info("executing agent (syscall.Exec)", "agent", agentType, "path", binaryPath, "argv", argv)
	}
	zlog.Info("executing agent",
		zap.String("agent", string(agentType)),
		zap.String("path", binaryPath),
		zap.Strings("args", argv))

	// Exec replaces current process (log file will be closed automatically)
	return syscall.Exec(binaryPath, argv, os.Environ())
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// PrepareSboxDirectory populates the .sbox/ directory in the workspace with
// plugins, agents, env vars, CLAUDE.md, and the entrypoint config. This is called by
// `sbox run` before starting the sandbox.
func PrepareSboxDirectory(workspaceDir string, config *Config, globalEnvs, projectEnvs, sboxFileEnvs []string, backend BackendType, agent AgentType) error {
	sboxDir := filepath.Join(workspaceDir, ".sbox")

	zlog.Info("preparing .sbox directory",
		zap.String("workspace", workspaceDir),
		zap.String("sbox_dir", sboxDir),
		zap.String("backend", string(backend)),
		zap.String("agent", string(agent)))

	// Create .sbox directory
	if err := os.MkdirAll(sboxDir, 0755); err != nil {
		return fmt.Errorf("failed to create .sbox directory: %w", err)
	}

	// Prepare merged CLAUDE.md file with backend-specific context
	if err := prepareRules(workspaceDir, sboxDir, backend); err != nil {
		zlog.Warn("failed to prepare rules file", zap.Error(err))
		// Continue - rules file is optional
	}

	entrypointConfig := &EntrypointConfig{
		Agent: string(agent),
	}

	// Copy plugins and agents from agent-specific home directory
	agentHome := config.GetAgentHome(agent)
	zlog.Info("preparing plugins and agents",
		zap.String("agent", string(agent)),
		zap.String("agent_home", agentHome))

	// Copy plugins
	plugins, err := preparePlugins(agentHome, sboxDir)
	if err != nil {
		zlog.Warn("failed to prepare plugins", zap.Error(err))
		// Continue - plugins are optional
	} else {
		entrypointConfig.Plugins = plugins
	}

	// Copy agents
	agents, err := prepareAgents(agentHome, sboxDir)
	if err != nil {
		zlog.Warn("failed to prepare agents", zap.Error(err))
		// Continue - agents are optional
	} else {
		entrypointConfig.Agents = agents
	}

	// Prepare OpenCode files (config and auth)
	if agent == AgentOpenCode {
		if err := prepareOpencodeConfig(agentHome, sboxDir); err != nil {
			zlog.Warn("failed to prepare opencode.json", zap.Error(err))
			// Non-fatal - continue anyway
		}
		if err := prepareOpencodeAuth(sboxDir); err != nil {
			zlog.Warn("failed to prepare opencode auth.json", zap.Error(err))
			// Non-fatal - continue anyway
		}
		if err := prepareOpencodeTUIConfig(agentHome, sboxDir); err != nil {
			zlog.Warn("failed to prepare tui.json", zap.Error(err))
			// Non-fatal - continue anyway
		}
	}

	// Write entrypoint config
	if err := WriteEntrypointConfig(workspaceDir, entrypointConfig); err != nil {
		return fmt.Errorf("failed to write entrypoint config: %w", err)
	}

	// Prepare and write environment variables
	resolvedEnvs := resolveEnvs(globalEnvs, projectEnvs, sboxFileEnvs)
	if err := WriteEntrypointEnv(workspaceDir, resolvedEnvs); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}

	zlog.Info("prepared .sbox directory",
		zap.Int("plugins", len(entrypointConfig.Plugins)),
		zap.Int("agents", len(entrypointConfig.Agents)),
		zap.Int("envs", len(resolvedEnvs)))

	return nil
}

// preparePlugins copies installed plugins to .sbox/plugins/
func preparePlugins(claudeHome, sboxDir string) ([]EntrypointPlugin, error) {
	pluginsJSONPath := filepath.Join(claudeHome, "plugins", "installed_plugins.json")
	hostCachePath := filepath.Join(claudeHome, "plugins", "cache")

	// Check if installed_plugins.json exists
	if _, err := os.Stat(pluginsJSONPath); os.IsNotExist(err) {
		zlog.Debug("installed_plugins.json not found, skipping plugins")
		return nil, nil
	}

	// Read the file
	content, err := os.ReadFile(pluginsJSONPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read installed_plugins.json: %w", err)
	}

	// Parse JSON
	var installedPlugins InstalledPlugins
	if err := json.Unmarshal(content, &installedPlugins); err != nil {
		return nil, fmt.Errorf("failed to parse installed_plugins.json: %w", err)
	}

	var plugins []EntrypointPlugin

	for pluginName, entries := range installedPlugins.Plugins {
		for _, entry := range entries {
			// Verify the plugin directory exists
			if _, err := os.Stat(entry.InstallPath); os.IsNotExist(err) {
				zlog.Warn("plugin install path not found, skipping",
					zap.String("plugin", pluginName),
					zap.String("path", entry.InstallPath))
				continue
			}

			// Check if path is under cache directory
			if !strings.HasPrefix(entry.InstallPath, hostCachePath) {
				zlog.Warn("plugin install path not under cache directory, skipping",
					zap.String("plugin", pluginName),
					zap.String("path", entry.InstallPath))
				continue
			}

			// Get relative path from cache directory
			relativePath := strings.TrimPrefix(entry.InstallPath, hostCachePath)
			relativePath = strings.TrimPrefix(relativePath, "/")

			// Destination path in .sbox
			dstPath := filepath.Join(sboxDir, "plugins", relativePath)

			zlog.Debug("copying plugin",
				zap.String("plugin", pluginName),
				zap.String("src", entry.InstallPath),
				zap.String("dst", dstPath))

			// Copy plugin directory
			if err := copyDir(entry.InstallPath, dstPath); err != nil {
				zlog.Warn("failed to copy plugin, skipping",
					zap.String("plugin", pluginName),
					zap.Error(err))
				continue
			}

			plugins = append(plugins, EntrypointPlugin{
				Name:           pluginName,
				Path:           filepath.Join("plugins", relativePath),
				Version:        entry.Version,
				PackageVersion: entry.GitCommitSha,
			})

			zlog.Info("copied plugin to .sbox",
				zap.String("plugin", pluginName),
				zap.String("path", dstPath))
		}
	}

	return plugins, nil
}

// prepareAgents copies agent files to .sbox/agents/
func prepareAgents(claudeHome, sboxDir string) ([]EntrypointAgent, error) {
	agentsDir := filepath.Join(claudeHome, "agents")

	// Check if agents directory exists
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		zlog.Debug("agents directory not found, skipping agents")
		return nil, nil
	}

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents directory: %w", err)
	}

	var agents []EntrypointAgent
	dstAgentsDir := filepath.Join(sboxDir, "agents")

	if err := os.MkdirAll(dstAgentsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create agents directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Support both .md and .json agent files
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".json") {
			continue
		}

		srcPath := filepath.Join(agentsDir, name)
		dstPath := filepath.Join(dstAgentsDir, name)

		// Get agent name (filename without extension)
		agentName := strings.TrimSuffix(name, filepath.Ext(name))

		zlog.Debug("copying agent",
			zap.String("name", agentName),
			zap.String("src", srcPath),
			zap.String("dst", dstPath))

		if err := copyFile(srcPath, dstPath); err != nil {
			zlog.Warn("failed to copy agent, skipping",
				zap.String("name", agentName),
				zap.Error(err))
			continue
		}

		agents = append(agents, EntrypointAgent{
			Name: agentName,
			Path: filepath.Join("agents", name),
		})

		zlog.Info("copied agent to .sbox",
			zap.String("agent", agentName),
			zap.String("path", dstPath))
	}

	return agents, nil
}

// prepareOpencodeConfig creates or updates opencode.json with full permissions for sandbox use.
// If the user has an existing config, it loads it and ensures permissions are set to allow all.
// If no config exists, it creates a default one with full permissions.
// All other user config fields (like "model") are preserved.
func prepareOpencodeConfig(agentHome, sboxDir string) error {
	opencodeConfigPath := filepath.Join(agentHome, "opencode.json")
	dstPath := filepath.Join(sboxDir, "opencode.json")

	// Use generic map to preserve all user fields
	config := make(map[string]interface{})

	// Try to read existing config
	if data, err := os.ReadFile(opencodeConfigPath); err == nil {
		// Parse existing config as generic JSON
		if err := json.Unmarshal(data, &config); err != nil {
			zlog.Warn("failed to parse existing opencode.json, using default",
				zap.String("path", opencodeConfigPath),
				zap.Error(err))
			// Fall through to use default config (empty map)
			config = make(map[string]interface{})
		} else {
			zlog.Info("loaded existing opencode.json", zap.String("path", opencodeConfigPath))
		}
	} else if !os.IsNotExist(err) {
		// File exists but couldn't be read
		zlog.Warn("failed to read opencode.json, using default",
			zap.String("path", opencodeConfigPath),
			zap.Error(err))
	} else {
		zlog.Info("no opencode.json found, creating default", zap.String("path", opencodeConfigPath))
	}

	// Ensure schema is set if not present
	if _, hasSchema := config["$schema"]; !hasSchema {
		config["$schema"] = "https://opencode.ai/config.json"
	}

	// Always set full permissions for sandbox/container environment
	// This overrides any existing permission settings
	config["permission"] = map[string]string{
		"*": "allow",
	}

	// Write config to .sbox/
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal opencode config: %w", err)
	}

	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write opencode config: %w", err)
	}

	zlog.Info("prepared opencode.json with full permissions",
		zap.String("dst", dstPath),
		zap.Int("fields", len(config)))

	return nil
}

// prepareOpencodeTUIConfig copies tui.json from the agent home to .sbox/ if it exists.
func prepareOpencodeTUIConfig(agentHome, sboxDir string) error {
	srcPath := filepath.Join(agentHome, "tui.json")
	dstPath := filepath.Join(sboxDir, "tui.json")

	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		zlog.Debug("tui.json not found, skipping", zap.String("path", srcPath))
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to stat tui.json: %w", err)
	}

	if err := copyFile(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy tui.json: %w", err)
	}

	zlog.Info("copied tui.json to .sbox",
		zap.String("src", srcPath),
		zap.String("dst", dstPath))

	return nil
}

// prepareOpencodeAuth copies the OpenCode auth.json file if it exists.
// Auth file is located at ~/.local/share/opencode/auth.json on the host.
func prepareOpencodeAuth(sboxDir string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// OpenCode auth is stored in XDG data directory
	authPath := filepath.Join(homeDir, ".local", "share", "opencode", "auth.json")
	dstPath := filepath.Join(sboxDir, "opencode-auth.json")

	// Check if auth file exists
	if _, err := os.Stat(authPath); os.IsNotExist(err) {
		zlog.Debug("opencode auth.json not found, skipping", zap.String("path", authPath))
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to stat auth file: %w", err)
	}

	// Copy auth file as-is
	if err := copyFile(authPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy auth file: %w", err)
	}

	zlog.Info("copied opencode auth.json to .sbox",
		zap.String("src", authPath),
		zap.String("dst", dstPath))

	return nil
}

// resolveEnvs merges and resolves environment variables from all sources.
// Passthrough variables (NAME without =) are resolved from the current environment.
// Returns a slice of KEY=value strings ready to write to .sbox/env.
func resolveEnvs(globalEnvs, projectEnvs, sboxFileEnvs []string) []string {
	merged, _ := MergeEnvs(globalEnvs, projectEnvs, sboxFileEnvs)

	var resolved []string
	for _, env := range merged {
		if idx := strings.Index(env, "="); idx >= 0 {
			// Already has value
			resolved = append(resolved, env)
		} else {
			// Passthrough - resolve from host environment
			value := os.Getenv(env)
			if value != "" {
				resolved = append(resolved, env+"="+value)
				zlog.Debug("resolved passthrough env",
					zap.String("name", env))
			} else {
				zlog.Debug("passthrough env not set on host, skipping",
					zap.String("name", env))
			}
		}
	}

	return resolved
}

// SaveClaudeCache saves the .claude folder from inside a running sandbox to .sbox/claude-cache/.
// This is called by `sbox stop` before stopping the sandbox to preserve credentials.
// Uses rsync inside the container to sync to the workspace's .sbox/claude-cache/ directory.
func SaveClaudeCache(workspaceDir string, backend Backend) error {
	cachePath := filepath.Join(workspaceDir, ".sbox", ClaudeCacheDir)

	zlog.Info("saving claude cache",
		zap.String("workspace", workspaceDir),
		zap.String("cache_path", cachePath))

	// Ensure cache directory exists (it's in the workspace, accessible from both host and container)
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Find the running container
	info, err := backend.FindRunning(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to find running container: %w", err)
	}
	if info == nil {
		return fmt.Errorf("no running container found")
	}

	// Build the exec command based on backend type
	var execPrefix []string
	if backend.Name() == BackendSandbox {
		execPrefix = []string{"docker", "sandbox", "exec", info.ID}
	} else {
		execPrefix = []string{"docker", "exec", info.ID}
	}

	claudeHome := "/home/agent/.claude"

	// Use rsync inside the container to sync .claude to .sbox/claude-cache/
	// The .sbox directory is mounted in the workspace, so changes are visible on host
	// --archive preserves permissions, timestamps, etc.
	// --delete ensures cache is an exact mirror (removes stale files from cache)
	rsyncArgs := append(execPrefix, "rsync", "-a", "--delete", claudeHome+"/", cachePath+"/")

	cmd := exec.Command(rsyncArgs[0], rsyncArgs[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		zlog.Warn("rsync failed",
			zap.Error(err),
			zap.String("output", string(output)))
		return fmt.Errorf("rsync failed: %w", err)
	}

	zlog.Info("claude cache saved successfully",
		zap.String("cache_path", cachePath))

	return nil
}

// restoreClaudeCache restores the .claude folder from .sbox/claude-cache/ if present.
// This allows credentials and settings to persist across sandbox recreations.
// Uses rsync to efficiently sync the cache to the claude home directory.
func restoreClaudeCache(workspaceDir, claudeHome string) error {
	cachePath := filepath.Join(workspaceDir, ".sbox", ClaudeCacheDir)

	// Check if cache exists and has content
	entries, err := os.ReadDir(cachePath)
	if os.IsNotExist(err) || len(entries) == 0 {
		elog.Debug("no claude cache found or empty", "path", cachePath)
		zlog.Debug("no claude cache found, skipping restore")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	elog.Info("restoring claude cache", "src", cachePath, "dst", claudeHome)
	zlog.Info("restoring claude cache from .sbox",
		zap.String("cache_path", cachePath),
		zap.String("claude_home", claudeHome))

	// Ensure claude home exists
	if err := os.MkdirAll(claudeHome, 0755); err != nil {
		return fmt.Errorf("failed to create claude home: %w", err)
	}

	// Use rsync to restore cache to claude home
	// --archive preserves permissions, timestamps, etc.
	// We don't use --delete here to preserve any existing data
	cmd := exec.Command("rsync", "-a", cachePath+"/", claudeHome+"/")
	output, err := cmd.CombinedOutput()
	if err != nil {
		elog.Error("rsync restore failed", "error", err, "output", string(output))
		return fmt.Errorf("rsync failed: %w", err)
	}

	elog.Info("claude cache restored successfully")
	zlog.Info("claude cache restored successfully")
	return nil
}

// prepareRules uses PrepareMDForSandbox to discover and concatenate MD files,
// then copies the result to .sbox/CLAUDE.md. These rules apply to all agent types.
func prepareRules(workspaceDir, sboxDir string, backend BackendType) error {
	// Use existing function to discover and concatenate CLAUDE.md and AGENTS.md files
	// with backend-specific context
	srcPath, err := PrepareMDForSandbox(workspaceDir, backend)
	if err != nil {
		return fmt.Errorf("failed to prepare MD files: %w", err)
	}

	// Copy to .sbox/CLAUDE.md (shared for all agents)
	dstPath := filepath.Join(sboxDir, "CLAUDE.md")
	if err := copyFile(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy rules file to .sbox: %w", err)
	}

	zlog.Info("prepared rules file in .sbox",
		zap.String("src", srcPath),
		zap.String("dst", dstPath))

	return nil
}
