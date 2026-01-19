package sbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Config holds global configuration for sbox
type Config struct {
	// ClaudeHome is the path to Claude's home directory (default: ~/.claude)
	ClaudeHome string `yaml:"claude_home"`

	// SboxDataDir is the path to sbox's data directory (default: ~/.sbox)
	SboxDataDir string `yaml:"sbox_data_dir"`

	// DockerSocket controls Docker socket access: "auto"|"always"|"never" (default: "auto")
	DockerSocket string `yaml:"docker_socket"`

	// DefaultProfiles are the default profiles to use for new projects
	DefaultProfiles []string `yaml:"default_profiles"`
}

// ProjectConfig holds per-project configuration settings
type ProjectConfig struct {
	// WorkspacePath is the absolute path to the project workspace
	// This is stored to allow listing projects by path
	WorkspacePath string `yaml:"workspace_path"`

	// Profiles are the active profiles for this project
	Profiles []string `yaml:"profiles"`

	// Volumes are additional volumes to mount in the sandbox
	// Format: "hostpath:sandboxpath[:ro]"
	// Paths starting with ./ or ../ are relative to the .sbox file location
	Volumes []string `yaml:"volumes"`

	// DockerSocket overrides the global docker_socket setting for this project
	// Values: "auto", "always", "never", or empty to use global setting
	DockerSocket string `yaml:"docker_socket"`
}

// SboxFileConfig represents the configuration from a .sbox file
// This is the user-facing format that gets loaded from disk
type SboxFileConfig struct {
	// Profiles to auto-enable for this project
	Profiles []string `yaml:"profiles"`

	// Volumes to mount in the sandbox
	// Paths starting with ./ or ../ are relative to the .sbox file location
	Volumes []string `yaml:"volumes"`

	// DockerSocket setting override
	DockerSocket string `yaml:"docker_socket"`
}

// SboxFileLocation contains info about a loaded .sbox file
type SboxFileLocation struct {
	// Path is the absolute path to the .sbox file
	Path string

	// Dir is the directory containing the .sbox file
	Dir string

	// Config is the parsed configuration
	Config *SboxFileConfig
}

// LoadConfig loads the global sbox configuration from ~/.config/sbox/config.yaml
// Creates the ~/.config/sbox directory if it doesn't exist
// Returns sensible defaults if config file doesn't exist
func LoadConfig() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	// Default configuration - use XDG-style ~/.config/sbox
	config := &Config{
		ClaudeHome:      filepath.Join(homeDir, ".claude"),
		SboxDataDir:     filepath.Join(homeDir, ".config", "sbox"),
		DockerSocket:    "auto",
		DefaultProfiles: []string{},
	}

	// Ensure ~/.config/sbox directory exists
	if err := os.MkdirAll(config.SboxDataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sbox data directory: %w", err)
	}

	// Try to load config file
	configPath := filepath.Join(config.SboxDataDir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config doesn't exist, return defaults
			zlog.Debug("no config file found, using defaults",
				zap.String("config_path", configPath))
			return config, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Ensure paths are absolute and expanded
	config.ClaudeHome = expandPath(config.ClaudeHome)
	config.SboxDataDir = expandPath(config.SboxDataDir)

	zlog.Debug("loaded config",
		zap.String("config_path", configPath),
		zap.String("claude_home", config.ClaudeHome),
		zap.String("sbox_data_dir", config.SboxDataDir),
		zap.String("docker_socket", config.DockerSocket))

	return config, nil
}

// SaveConfig saves the global configuration to ~/.sbox/config.yaml
func SaveConfig(config *Config) error {
	configPath := filepath.Join(config.SboxDataDir, "config.yaml")

	// Ensure directory exists
	if err := os.MkdirAll(config.SboxDataDir, 0755); err != nil {
		return fmt.Errorf("failed to create sbox data directory: %w", err)
	}

	// Serialize to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	zlog.Debug("saved config", zap.String("config_path", configPath))
	return nil
}

// GetProjectConfig loads the per-project configuration based on workspace path
// Computes a project hash from the absolute path (first 12 chars of SHA256)
// Loads from ~/.sbox/projects/<hash>/config.yaml if exists
// Returns defaults otherwise
func GetProjectConfig(workspacePath string) (*ProjectConfig, string, error) {
	// Convert to absolute path
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Compute project hash (first 12 chars of SHA256)
	hash := sha256.Sum256([]byte(absPath))
	projectHash := hex.EncodeToString(hash[:])[:12]

	// Default project config
	projectConfig := &ProjectConfig{
		Profiles: []string{},
	}

	// Load global config to get data directory
	globalConfig, err := LoadConfig()
	if err != nil {
		return nil, "", fmt.Errorf("failed to load global config: %w", err)
	}

	// Try to load project config
	projectDir := filepath.Join(globalConfig.SboxDataDir, "projects", projectHash)
	configPath := filepath.Join(projectDir, "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Project config doesn't exist, return defaults
			zlog.Debug("no project config found, using defaults",
				zap.String("workspace", absPath),
				zap.String("project_hash", projectHash),
				zap.String("config_path", configPath))
			return projectConfig, projectHash, nil
		}
		return nil, "", fmt.Errorf("failed to read project config: %w", err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, projectConfig); err != nil {
		return nil, "", fmt.Errorf("failed to parse project config: %w", err)
	}

	zlog.Debug("loaded project config",
		zap.String("workspace", absPath),
		zap.String("project_hash", projectHash),
		zap.String("config_path", configPath),
		zap.Strings("profiles", projectConfig.Profiles))

	return projectConfig, projectHash, nil
}

// SaveProjectConfig saves the per-project configuration
func SaveProjectConfig(workspacePath string, projectConfig *ProjectConfig) error {
	// Convert to absolute path
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Ensure workspace path is stored in the config
	projectConfig.WorkspacePath = absPath

	// Compute project hash
	hash := sha256.Sum256([]byte(absPath))
	projectHash := hex.EncodeToString(hash[:])[:12]

	// Load global config to get data directory
	globalConfig, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}

	// Ensure project directory exists
	projectDir := filepath.Join(globalConfig.SboxDataDir, "projects", projectHash)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	configPath := filepath.Join(projectDir, "config.yaml")

	// Serialize to YAML
	data, err := yaml.Marshal(projectConfig)
	if err != nil {
		return fmt.Errorf("failed to serialize project config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write project config file: %w", err)
	}

	zlog.Debug("saved project config",
		zap.String("workspace", absPath),
		zap.String("project_hash", projectHash),
		zap.String("config_path", configPath))

	return nil
}

// RemoveProjectData removes all stored data for a project.
// This includes the project config and any cached files.
func RemoveProjectData(workspacePath string) error {
	// Convert to absolute path
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Compute project hash
	hash := sha256.Sum256([]byte(absPath))
	projectHash := hex.EncodeToString(hash[:])[:12]

	// Load global config to get data directory
	globalConfig, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}

	// Remove project directory
	projectDir := filepath.Join(globalConfig.SboxDataDir, "projects", projectHash)

	zlog.Debug("removing project data",
		zap.String("workspace", absPath),
		zap.String("project_hash", projectHash),
		zap.String("project_dir", projectDir))

	if err := os.RemoveAll(projectDir); err != nil {
		return fmt.Errorf("failed to remove project directory: %w", err)
	}

	zlog.Info("removed project data",
		zap.String("workspace", absPath),
		zap.String("project_hash", projectHash))

	return nil
}

// ProjectInfo contains information about a known project
type ProjectInfo struct {
	// Hash is the project hash (directory name)
	Hash string

	// WorkspacePath is the absolute path to the workspace
	WorkspacePath string

	// Config is the project configuration
	Config *ProjectConfig

	// IsRunning indicates if a sandbox is currently running for this project
	IsRunning bool

	// ContainerID is the container ID if the sandbox is running
	ContainerID string
}

// ListProjects returns information about all known projects
func ListProjects() ([]ProjectInfo, error) {
	globalConfig, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load global config: %w", err)
	}

	projectsDir := filepath.Join(globalConfig.SboxDataDir, "projects")

	// Check if projects directory exists
	if _, err := os.Stat(projectsDir); os.IsNotExist(err) {
		return nil, nil // No projects yet
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read projects directory: %w", err)
	}

	var projects []ProjectInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectHash := entry.Name()
		configPath := filepath.Join(projectsDir, projectHash, "config.yaml")

		data, err := os.ReadFile(configPath)
		if err != nil {
			zlog.Debug("failed to read project config",
				zap.String("hash", projectHash),
				zap.Error(err))
			continue
		}

		var config ProjectConfig
		if err := yaml.Unmarshal(data, &config); err != nil {
			zlog.Debug("failed to parse project config",
				zap.String("hash", projectHash),
				zap.Error(err))
			continue
		}

		info := ProjectInfo{
			Hash:          projectHash,
			WorkspacePath: config.WorkspacePath,
			Config:        &config,
		}

		projects = append(projects, info)
	}

	return projects, nil
}

// FindSboxFile searches for a .sbox file starting from the given directory
// and walking up the directory tree. Returns nil if no .sbox file is found.
func FindSboxFile(startDir string) (*SboxFileLocation, error) {
	absPath, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	currentDir := absPath
	for {
		sboxPath := filepath.Join(currentDir, ".sbox")
		if _, err := os.Stat(sboxPath); err == nil {
			// Found .sbox file
			data, err := os.ReadFile(sboxPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read .sbox file: %w", err)
			}

			var config SboxFileConfig
			if err := yaml.Unmarshal(data, &config); err != nil {
				return nil, fmt.Errorf("failed to parse .sbox file: %w", err)
			}

			zlog.Debug("found .sbox file",
				zap.String("path", sboxPath),
				zap.Strings("profiles", config.Profiles),
				zap.Strings("volumes", config.Volumes),
				zap.String("docker_socket", config.DockerSocket))

			return &SboxFileLocation{
				Path:   sboxPath,
				Dir:    currentDir,
				Config: &config,
			}, nil
		}

		// Move to parent directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir || parentDir == "." || parentDir == "/" {
			break
		}
		currentDir = parentDir
	}

	zlog.Debug("no .sbox file found", zap.String("start_dir", absPath))
	return nil, nil
}

// ResolveVolumePath resolves a volume path relative to a base directory.
// Paths starting with ./ or ../ are resolved relative to baseDir.
// Paths starting with ~ are expanded to the user's home directory.
// Other paths are returned as-is (absolute paths).
func ResolveVolumePath(volumePath, baseDir string) (string, error) {
	// Handle ~ expansion
	if strings.HasPrefix(volumePath, "~/") || volumePath == "~" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		if volumePath == "~" {
			return homeDir, nil
		}
		return filepath.Join(homeDir, volumePath[2:]), nil
	}

	// Handle relative paths
	if strings.HasPrefix(volumePath, "./") || strings.HasPrefix(volumePath, "../") {
		return filepath.Join(baseDir, volumePath), nil
	}

	// Absolute path or other - return as-is
	return volumePath, nil
}

// ParseVolumeSpec parses a volume specification into host path, container path, and options.
// Format: "hostpath:containerpath[:ro]"
func ParseVolumeSpec(spec string) (hostPath, containerPath string, readOnly bool, err error) {
	parts := strings.Split(spec, ":")

	switch len(parts) {
	case 2:
		return parts[0], parts[1], false, nil
	case 3:
		if parts[2] == "ro" {
			return parts[0], parts[1], true, nil
		}
		return "", "", false, fmt.Errorf("invalid volume option %q (expected 'ro')", parts[2])
	default:
		return "", "", false, fmt.Errorf("invalid volume specification %q (expected 'hostpath:containerpath[:ro]')", spec)
	}
}

// MergeProjectConfig merges configuration from a .sbox file into a ProjectConfig.
// The .sbox file settings override/extend the ProjectConfig.
func MergeProjectConfig(projectConfig *ProjectConfig, sboxFile *SboxFileLocation) (*ProjectConfig, error) {
	if sboxFile == nil || sboxFile.Config == nil {
		return projectConfig, nil
	}

	sboxConfig := sboxFile.Config
	merged := &ProjectConfig{
		Profiles:     projectConfig.Profiles,
		Volumes:      projectConfig.Volumes,
		DockerSocket: projectConfig.DockerSocket,
	}

	// Merge profiles (combine both lists, removing duplicates)
	profileSet := make(map[string]bool)
	for _, p := range merged.Profiles {
		profileSet[p] = true
	}
	for _, p := range sboxConfig.Profiles {
		if !profileSet[p] {
			merged.Profiles = append(merged.Profiles, p)
			profileSet[p] = true
		}
	}

	// Merge volumes (resolve relative paths and add to list)
	for _, vol := range sboxConfig.Volumes {
		hostPath, containerPath, readOnly, err := ParseVolumeSpec(vol)
		if err != nil {
			return nil, fmt.Errorf("invalid volume in .sbox file: %w", err)
		}

		// Resolve the host path relative to .sbox file location
		resolvedHostPath, err := ResolveVolumePath(hostPath, sboxFile.Dir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve volume path: %w", err)
		}

		// Rebuild the volume spec with resolved path
		resolvedSpec := resolvedHostPath + ":" + containerPath
		if readOnly {
			resolvedSpec += ":ro"
		}
		merged.Volumes = append(merged.Volumes, resolvedSpec)
	}

	// Override docker_socket if set in .sbox file
	if sboxConfig.DockerSocket != "" {
		merged.DockerSocket = sboxConfig.DockerSocket
	}

	zlog.Debug("merged project config with .sbox file",
		zap.Strings("profiles", merged.Profiles),
		zap.Strings("volumes", merged.Volumes),
		zap.String("docker_socket", merged.DockerSocket))

	return merged, nil
}

// expandPath expands ~ to home directory and makes path absolute
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(homeDir, path[1:])
		}
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absPath
}
