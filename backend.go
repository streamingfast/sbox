package sbox

import (
	"fmt"
)

// BackendType represents the container backend type
type BackendType string

const (
	// BackendSandbox uses Docker sandbox (MicroVM) for container execution
	BackendSandbox BackendType = "sandbox"
	// BackendContainer uses standard Docker containers for execution
	BackendContainer BackendType = "container"
)

// DefaultBackend is the default backend type when not specified
const DefaultBackend = BackendSandbox

// ValidBackendTypes contains all valid backend type values
var ValidBackendTypes = []BackendType{BackendSandbox, BackendContainer}

// ValidateBackend checks if a backend name is valid
func ValidateBackend(name string) error {
	switch BackendType(name) {
	case BackendSandbox, BackendContainer:
		return nil
	case "":
		return nil // Empty means use default
	default:
		return fmt.Errorf("invalid backend %q, valid values: %v", name, ValidBackendTypes)
	}
}

// ContainerInfo contains information about a managed container
type ContainerInfo struct {
	// ID is the container/sandbox ID
	ID string
	// Name is the container/sandbox name
	Name string
	// Status is the container status (e.g., "running", "stopped", "created")
	Status string
	// Image is the container image name
	Image string
	// Workspace is the mounted workspace path
	Workspace string
	// Backend is the backend type that manages this container
	Backend BackendType
}

// BackendOptions contains options for running a container
type BackendOptions struct {
	// WorkspaceDir is the workspace directory to mount
	WorkspaceDir string

	// Profiles are the additional profiles to use for this session
	Profiles []string

	// ForceRebuild forces rebuilding the custom template image
	ForceRebuild bool

	// Debug enables debug mode for backend commands
	Debug bool

	// Config is the global configuration
	Config *Config

	// ProjectConfig is the per-project configuration
	ProjectConfig *ProjectConfig

	// SboxFile is the loaded sbox.yaml file configuration (if any)
	SboxFile *SboxFileLocation

	// MountDockerSocket controls whether to mount the Docker socket
	// Note: Only applicable for container backend; sandbox backend has automatic Docker access
	MountDockerSocket bool
}

// Backend defines the interface for container execution backends
type Backend interface {
	// Name returns the backend type name
	Name() BackendType

	// Run starts or attaches to a container for the given workspace
	Run(opts BackendOptions) error

	// Shell opens a shell in the running container
	Shell(workspaceDir string) error

	// Stop stops the container, optionally removing it
	Stop(workspaceDir string, remove bool) (*ContainerInfo, error)

	// Find returns container info for the given workspace (any state)
	Find(workspaceDir string) (*ContainerInfo, error)

	// FindRunning returns container info only if running
	FindRunning(workspaceDir string) (*ContainerInfo, error)

	// List returns all containers managed by this backend
	List() ([]ContainerInfo, error)

	// Remove removes a container by ID
	Remove(containerID string) error
}

// GetBackend returns the appropriate backend implementation based on the backend type.
// If backendType is empty, it returns the default backend (sandbox).
func GetBackend(backendType string, config *Config) (Backend, error) {
	if backendType == "" {
		backendType = string(DefaultBackend)
	}

	if err := ValidateBackend(backendType); err != nil {
		return nil, err
	}

	switch BackendType(backendType) {
	case BackendSandbox:
		return NewSandboxBackend(config), nil
	case BackendContainer:
		return NewContainerBackend(config), nil
	default:
		return nil, fmt.Errorf("unknown backend type: %s", backendType)
	}
}

// ResolveBackendType determines the effective backend type from configuration sources.
// Priority order (highest to lowest):
// 1. CLI flag (cliBackend parameter)
// 2. sbox.yaml file (sboxFile.Config.Backend)
// 3. Project config (projectConfig.Backend)
// 4. Global config (config.DefaultBackend)
// 5. Hardcoded default (BackendSandbox)
func ResolveBackendType(cliBackend string, sboxFile *SboxFileLocation, projectConfig *ProjectConfig, config *Config) BackendType {
	// 1. CLI flag takes highest priority
	if cliBackend != "" {
		return BackendType(cliBackend)
	}

	// 2. sbox.yaml file
	if sboxFile != nil && sboxFile.Config != nil && sboxFile.Config.Backend != "" {
		return BackendType(sboxFile.Config.Backend)
	}

	// 3. Project config
	if projectConfig != nil && projectConfig.Backend != "" {
		return BackendType(projectConfig.Backend)
	}

	// 4. Global config
	if config != nil && config.DefaultBackend != "" {
		return BackendType(config.DefaultBackend)
	}

	// 5. Hardcoded default
	return DefaultBackend
}
