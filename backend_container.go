package sbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"go.uber.org/zap"
)

// DockerSocketEnvVar is the environment variable to override the Docker socket path
const DockerSocketEnvVar = "SBOX_DOCKER_SOCKET"

// ContainerBackend implements the Backend interface using standard Docker containers
type ContainerBackend struct {
	config *Config
}

// NewContainerBackend creates a new container backend instance
func NewContainerBackend(config *Config) *ContainerBackend {
	return &ContainerBackend{config: config}
}

// Name returns the backend type name
func (b *ContainerBackend) Name() BackendType {
	return BackendContainer
}

// volumeName generates a unique volume name for persisting .claude folder
func (b *ContainerBackend) volumeName(workspaceDir string) string {
	absPath, _ := filepath.Abs(workspaceDir)
	hash := sha256.Sum256([]byte(absPath))
	return "sbox-claude-" + hex.EncodeToString(hash[:])[:12]
}

// containerName generates the container name for a workspace
func (b *ContainerBackend) containerName(workspaceDir string) (string, error) {
	return GenerateSandboxName(workspaceDir)
}

// Run starts or attaches to a container for the given workspace
func (b *ContainerBackend) Run(opts BackendOptions) error {
	absPath, err := filepath.Abs(opts.WorkspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	containerName, err := b.containerName(absPath)
	if err != nil {
		return fmt.Errorf("failed to generate container name: %w", err)
	}

	// Merge project profiles with command-line profiles
	allProfiles := mergeProfiles(opts.ProjectConfig.Profiles, opts.Profiles)

	zlog.Info("preparing to run Docker container",
		zap.String("name", containerName),
		zap.String("workspace", absPath),
		zap.Strings("profiles", allProfiles))

	// Prepare .sbox directory with plugins, agents, and env vars
	var sboxFileEnvs []string
	if opts.SboxFile != nil && opts.SboxFile.Config != nil {
		sboxFileEnvs = opts.SboxFile.Config.Envs
	}
	if err := PrepareSboxDirectory(absPath, opts.Config, opts.Config.Envs, opts.ProjectConfig.Envs, sboxFileEnvs); err != nil {
		return fmt.Errorf("failed to prepare .sbox directory: %w", err)
	}

	// Check if container exists
	existing, err := b.Find(absPath)
	if err != nil {
		zlog.Debug("failed to check for existing container", zap.Error(err))
	}

	if existing != nil {
		// Container exists - check status and handle accordingly
		if existing.Status == "running" {
			fmt.Printf("Attaching to running container '%s'...\n", containerName)
			return b.attachContainer(containerName)
		}
		// Container exists but not running - start it
		fmt.Printf("Starting existing container '%s'...\n", containerName)
		return b.startContainer(containerName)
	}

	// Container doesn't exist - create and run it
	// Build custom template
	builder := NewTemplateBuilder(opts.Config, allProfiles)
	templateImage, err := builder.Build(opts.ForceRebuild)
	if err != nil {
		return fmt.Errorf("failed to build custom template: %w", err)
	}

	fmt.Printf("Creating container '%s'...\n", containerName)
	zlog.Info("container does not exist, creating",
		zap.String("name", containerName),
		zap.String("workspace", absPath),
		zap.String("template", templateImage))

	// Ensure volume exists for .claude persistence
	volumeName := b.volumeName(absPath)
	if err := b.ensureVolume(volumeName); err != nil {
		return fmt.Errorf("failed to create persistence volume: %w", err)
	}

	// Build docker run command
	args := b.buildRunArgs(containerName, absPath, templateImage, volumeName, opts)

	zlog.Debug("executing docker command",
		zap.String("cmd", "docker"),
		zap.Strings("args", args))

	fmt.Printf("Starting container '%s'...\n", containerName)

	cmd := exec.Command("docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker run failed: %w", err)
	}

	zlog.Info("Docker container exited successfully")
	return nil
}

// buildRunArgs constructs the docker run command arguments
func (b *ContainerBackend) buildRunArgs(containerName, workspaceDir, image, volumeName string, opts BackendOptions) []string {
	args := []string{"run", "-it", "--name", containerName}

	// Mount workspace directory
	args = append(args, "-v", fmt.Sprintf("%s:%s", workspaceDir, workspaceDir))

	// Mount persistence volume for .claude folder
	args = append(args, "-v", fmt.Sprintf("%s:/home/agent/.claude", volumeName))

	// Set working directory
	args = append(args, "-w", workspaceDir)

	// Set workspace env var (used by entrypoint)
	args = append(args, "-e", fmt.Sprintf("WORKSPACE_DIR=%s", workspaceDir))

	// Mount SSH directory if it exists (read-only)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		sshPath := filepath.Join(homeDir, ".ssh")
		if _, err := os.Stat(sshPath); err == nil {
			args = append(args, "-v", fmt.Sprintf("%s:/home/agent/.ssh:ro", sshPath))
		}
	}

	// Mount Docker socket if requested
	if opts.MountDockerSocket {
		socketPath := getDockerSocketPath()
		if socketPath != "" {
			args = append(args, "-v", fmt.Sprintf("%s:/var/run/docker.sock", socketPath))
			zlog.Debug("mounting docker socket", zap.String("host_path", socketPath))
		} else {
			zlog.Warn("docker socket requested but no socket found")
		}
	}

	// Mount additional volumes from project config
	for _, vol := range opts.ProjectConfig.Volumes {
		hostPath, containerPath, readOnly, err := ParseVolumeSpec(vol)
		if err != nil {
			zlog.Warn("invalid volume specification, skipping", zap.String("spec", vol), zap.Error(err))
			continue
		}

		// Check if host path exists
		if _, err := os.Stat(hostPath); err != nil {
			zlog.Warn("volume host path not found, skipping",
				zap.String("host_path", hostPath),
				zap.String("container_path", containerPath),
				zap.Error(err))
			continue
		}

		mountSpec := fmt.Sprintf("%s:%s", hostPath, containerPath)
		if readOnly {
			mountSpec += ":ro"
		}
		args = append(args, "-v", mountSpec)
	}

	// Mount settings files if they exist
	if b.config != nil {
		settingsPath := filepath.Join(b.config.ClaudeHome, "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			args = append(args, "-v", fmt.Sprintf("%s:/home/agent/.claude/settings.json:ro", settingsPath))
		}

		settingsLocalPath := filepath.Join(b.config.ClaudeHome, "settings.local.json")
		if _, err := os.Stat(settingsLocalPath); err == nil {
			args = append(args, "-v", fmt.Sprintf("%s:/home/agent/.claude/settings.local.json:ro", settingsLocalPath))
		}
	}

	// Add the image
	args = append(args, image)

	return args
}

// ensureVolume creates a Docker volume if it doesn't exist
func (b *ContainerBackend) ensureVolume(volumeName string) error {
	// Check if volume exists
	cmd := exec.Command("docker", "volume", "inspect", volumeName)
	if err := cmd.Run(); err == nil {
		// Volume already exists
		zlog.Debug("volume already exists", zap.String("volume", volumeName))
		return nil
	}

	// Create volume
	zlog.Info("creating persistence volume", zap.String("volume", volumeName))
	cmd = exec.Command("docker", "volume", "create", volumeName)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker volume create failed: %w (stderr: %s)", err, stderr.String())
	}

	return nil
}

// attachContainer attaches to a running container
func (b *ContainerBackend) attachContainer(containerName string) error {
	cmd := exec.Command("docker", "attach", containerName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker attach failed: %w", err)
	}
	return nil
}

// startContainer starts a stopped container
func (b *ContainerBackend) startContainer(containerName string) error {
	cmd := exec.Command("docker", "start", "-ai", containerName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker start failed: %w", err)
	}
	return nil
}

// Shell opens a shell in the running container
func (b *ContainerBackend) Shell(workspaceDir string) error {
	// Check if we're already inside a container
	if IsInsideSandbox() {
		return fmt.Errorf("you are already inside a container\nUse 'bash' to open a new shell, or exit and run 'sbox shell' from the host")
	}

	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	info, err := b.FindRunning(absPath)
	if err != nil {
		return fmt.Errorf("failed to find running container: %w", err)
	}

	if info == nil {
		return fmt.Errorf("no container is running for workspace: %s\nStart a container first with: sbox run", absPath)
	}

	zlog.Info("connecting to container shell",
		zap.String("container_id", info.ID),
		zap.String("container_name", info.Name),
		zap.String("workspace", absPath))

	cmd := exec.Command("docker", "exec", "-it", info.ID, "bash")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker exec failed: %w", err)
	}

	return nil
}

// Stop stops the container, optionally removing it
func (b *ContainerBackend) Stop(workspaceDir string, remove bool) (*ContainerInfo, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	info, err := b.FindRunning(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find running container: %w", err)
	}

	if info == nil {
		zlog.Debug("no running container to stop", zap.String("workspace", absPath))
		return nil, nil
	}

	zlog.Info("stopping container",
		zap.String("container_id", info.ID),
		zap.String("container_name", info.Name),
		zap.String("workspace", absPath))

	// Stop the container
	stopCmd := exec.Command("docker", "stop", info.ID)
	var stderr bytes.Buffer
	stopCmd.Stderr = &stderr

	if err := stopCmd.Run(); err != nil {
		return nil, fmt.Errorf("docker stop failed: %w (stderr: %s)", err, stderr.String())
	}

	zlog.Info("container stopped",
		zap.String("container_id", info.ID),
		zap.String("container_name", info.Name))

	// Remove the container if requested
	if remove {
		zlog.Info("removing container",
			zap.String("container_id", info.ID),
			zap.String("container_name", info.Name))

		rmCmd := exec.Command("docker", "rm", info.ID)
		rmCmd.Stderr = &stderr

		if err := rmCmd.Run(); err != nil {
			return nil, fmt.Errorf("docker rm failed: %w (stderr: %s)", err, stderr.String())
		}

		zlog.Info("container removed",
			zap.String("container_id", info.ID),
			zap.String("container_name", info.Name))
	}

	return info, nil
}

// Find returns container info for the given workspace (any state)
func (b *ContainerBackend) Find(workspaceDir string) (*ContainerInfo, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	containerName, err := b.containerName(absPath)
	if err != nil {
		return nil, err
	}

	// Use docker ps -a to find the container by name
	cmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=^%s$", containerName), "--format", "{{json .}}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		zlog.Debug("docker ps command failed",
			zap.Error(err),
			zap.String("stderr", stderr.String()))
		return nil, nil // Container likely doesn't exist
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return nil, nil // No container found
	}

	// Parse the JSON output
	var containerData struct {
		ID     string `json:"ID"`
		Names  string `json:"Names"`
		Image  string `json:"Image"`
		State  string `json:"State"`
		Status string `json:"Status"`
	}

	if err := json.Unmarshal([]byte(output), &containerData); err != nil {
		return nil, fmt.Errorf("failed to parse docker ps output: %w", err)
	}

	return &ContainerInfo{
		ID:        containerData.ID,
		Name:      containerData.Names,
		Status:    containerData.State,
		Image:     containerData.Image,
		Workspace: absPath,
		Backend:   BackendContainer,
	}, nil
}

// FindRunning returns container info only if running
func (b *ContainerBackend) FindRunning(workspaceDir string) (*ContainerInfo, error) {
	info, err := b.Find(workspaceDir)
	if err != nil {
		return nil, err
	}

	if info == nil {
		return nil, nil
	}

	// Only return if the container is running
	if info.Status != "running" {
		zlog.Debug("container found but not running",
			zap.String("container_id", info.ID),
			zap.String("status", info.Status))
		return nil, nil
	}

	return info, nil
}

// List returns all containers managed by this backend
func (b *ContainerBackend) List() ([]ContainerInfo, error) {
	// List all containers with the sbox-claude- prefix
	cmd := exec.Command("docker", "ps", "-a", "--filter", "name=^sbox-claude-", "--format", "{{json .}}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		zlog.Debug("docker ps command failed",
			zap.Error(err),
			zap.String("stderr", stderr.String()))
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}

	var infos []ContainerInfo
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		var containerData struct {
			ID     string `json:"ID"`
			Names  string `json:"Names"`
			Image  string `json:"Image"`
			State  string `json:"State"`
			Status string `json:"Status"`
		}

		if err := json.Unmarshal([]byte(line), &containerData); err != nil {
			zlog.Debug("failed to parse container data", zap.String("line", line), zap.Error(err))
			continue
		}

		// Try to extract workspace from container mounts
		workspace := b.getContainerWorkspace(containerData.ID)

		infos = append(infos, ContainerInfo{
			ID:        containerData.ID,
			Name:      containerData.Names,
			Status:    containerData.State,
			Image:     containerData.Image,
			Workspace: workspace,
			Backend:   BackendContainer,
		})
	}

	return infos, nil
}

// getContainerWorkspace inspects a container to find its workspace mount
func (b *ContainerBackend) getContainerWorkspace(containerID string) string {
	cmd := exec.Command("docker", "inspect", containerID, "--format", "{{range .Mounts}}{{if eq .Type \"bind\"}}{{.Source}}:{{.Destination}}\n{{end}}{{end}}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return ""
	}

	// Look for a bind mount where source == destination (workspace pattern)
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) == 2 && parts[0] == parts[1] && strings.HasPrefix(parts[0], "/") {
			return parts[0]
		}
	}

	return ""
}

// Remove removes a container by ID
func (b *ContainerBackend) Remove(containerID string) error {
	zlog.Info("removing docker container",
		zap.String("container_id", containerID))

	// First try to stop if running
	stopCmd := exec.Command("docker", "stop", containerID)
	_ = stopCmd.Run() // Ignore error - container might not be running

	// Remove the container
	cmd := exec.Command("docker", "rm", containerID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker rm failed: %w (stderr: %s)", err, stderr.String())
	}

	zlog.Info("docker container removed",
		zap.String("container_id", containerID))
	return nil
}

// RemoveVolume removes the persistence volume for a workspace
func (b *ContainerBackend) RemoveVolume(workspaceDir string) error {
	volumeName := b.volumeName(workspaceDir)

	zlog.Info("removing persistence volume", zap.String("volume", volumeName))

	cmd := exec.Command("docker", "volume", "rm", volumeName)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker volume rm failed: %w (stderr: %s)", err, stderr.String())
	}

	return nil
}

// Cleanup removes all backend-specific resources for a workspace
func (b *ContainerBackend) Cleanup(workspaceDir string) error {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Remove .sbox directory
	sboxDir := filepath.Join(absPath, ".sbox")
	if err := os.RemoveAll(sboxDir); err != nil {
		zlog.Warn("failed to remove .sbox directory", zap.String("path", sboxDir), zap.Error(err))
		// Continue to remove volume
	} else {
		zlog.Info(".sbox directory removed", zap.String("path", sboxDir))
	}

	// Remove persistence volume
	if err := b.RemoveVolume(workspaceDir); err != nil {
		zlog.Warn("failed to remove persistence volume", zap.Error(err))
		// Non-fatal - volume might not exist
	}

	return nil
}

// SaveCache is a no-op for container backend - uses named volumes for persistence
func (b *ContainerBackend) SaveCache(workspaceDir string) error {
	// Container backend uses named volumes mounted at /home/agent/.claude
	// which automatically persists across container restarts
	zlog.Debug("container backend uses volumes, no cache save needed")
	return nil
}

// getDockerSocketPath returns the Docker socket path to mount.
// Priority:
//  1. SBOX_DOCKER_SOCKET environment variable (explicit override)
//  2. Platform-specific default paths
func getDockerSocketPath() string {
	// Check for explicit override
	if envPath := os.Getenv(DockerSocketEnvVar); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
		zlog.Warn("SBOX_DOCKER_SOCKET path does not exist", zap.String("path", envPath))
	}

	// Platform-specific defaults
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		// macOS: Docker Desktop uses different locations
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			candidates = append(candidates, filepath.Join(homeDir, ".docker", "run", "docker.sock"))
		}
		candidates = append(candidates, "/var/run/docker.sock")
	case "linux":
		candidates = []string{"/var/run/docker.sock"}
	default:
		// Fallback for other platforms
		candidates = []string{"/var/run/docker.sock"}
	}

	// Return first existing socket
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
