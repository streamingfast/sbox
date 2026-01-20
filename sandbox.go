package sbox

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// SandboxOptions holds options for running the Docker sandbox
type SandboxOptions struct {
	// WorkspaceDir is the workspace directory to mount
	WorkspaceDir string

	// MountDockerSocket controls whether to mount the Docker socket
	MountDockerSocket bool

	// Profiles are the additional profiles to use for this session
	Profiles []string

	// ForceRebuild forces rebuilding the custom template image
	ForceRebuild bool

	// Config is the global configuration
	Config *Config

	// ProjectConfig is the per-project configuration
	ProjectConfig *ProjectConfig
}

// BuildDockerCommand builds the docker sandbox command arguments without executing.
// This is useful for displaying what command would be run.
// Returns the full argument list (not including "docker" itself).
func BuildDockerCommand(opts SandboxOptions) ([]string, error) {
	// Merge project profiles with command-line profiles
	allProfiles := mergeProfiles(opts.ProjectConfig.Profiles, opts.Profiles)

	// Build custom template name if profiles are specified (without actually building)
	var templateImage string
	if len(allProfiles) > 0 {
		builder := NewTemplateBuilder(opts.Config, allProfiles)
		templateImage = builder.ImageName()
	}

	// Prepare concatenated CLAUDE.md file path
	claudeMDPath, err := PrepareMDForSandbox(opts.WorkspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare CLAUDE.md: %w", err)
	}

	// Build Claude CLI flags (agents, plugins, etc.)
	claudeFlags, err := BuildClaudeFlags(opts.Config.ClaudeHome)
	if err != nil {
		claudeFlags = &ClaudeFlags{}
	}

	// Build volume mount arguments
	volumeMounts, err := buildVolumeMounts(opts, claudeMDPath, claudeFlags)
	if err != nil {
		return nil, fmt.Errorf("failed to build volume mounts: %w", err)
	}

	// Build docker command
	args := []string{"sandbox", "run"}

	// Add custom template if we have one
	if templateImage != "" && templateImage != "docker/sandbox-templates:claude-code" {
		args = append(args, "--template", templateImage)
	}

	// Add Docker socket flag if needed
	if shouldMountDockerSocket(opts.Config, opts.ProjectConfig, opts.MountDockerSocket) {
		args = append(args, "--mount-docker-socket")
	}

	// Add volume mounts
	args = append(args, volumeMounts...)

	// Add the Claude image and CLI flags
	args = append(args, "claude")

	// Add Claude CLI flags after the image name
	claudeArgs := buildClaudeCLIArgs(claudeFlags)
	args = append(args, claudeArgs...)

	return args, nil
}

// RunSandbox executes the Docker sandbox with all configured mounts and settings
func RunSandbox(opts SandboxOptions) error {
	// Merge project profiles with command-line profiles
	allProfiles := mergeProfiles(opts.ProjectConfig.Profiles, opts.Profiles)

	zlog.Info("preparing to run Docker sandbox",
		zap.String("workspace", opts.WorkspaceDir),
		zap.Bool("mount_docker_socket", opts.MountDockerSocket),
		zap.Strings("profiles", allProfiles))

	// Build custom template if profiles are specified
	var templateImage string
	if len(allProfiles) > 0 {
		builder := NewTemplateBuilder(opts.Config, allProfiles)
		var err error
		templateImage, err = builder.Build(opts.ForceRebuild)
		if err != nil {
			return fmt.Errorf("failed to build custom template: %w", err)
		}
	}

	// Prepare concatenated CLAUDE.md file
	claudeMDPath, err := PrepareMDForSandbox(opts.WorkspaceDir)
	if err != nil {
		return fmt.Errorf("failed to prepare CLAUDE.md: %w", err)
	}

	// Build Claude CLI flags (agents, plugins, etc.)
	claudeFlags, err := BuildClaudeFlags(opts.Config.ClaudeHome)
	if err != nil {
		zlog.Warn("failed to build Claude flags, continuing without them", zap.Error(err))
		claudeFlags = &ClaudeFlags{}
	}

	// Build volume mount arguments
	volumeMounts, err := buildVolumeMounts(opts, claudeMDPath, claudeFlags)
	if err != nil {
		return fmt.Errorf("failed to build volume mounts: %w", err)
	}

	// Build docker command
	args := []string{"sandbox", "run"}

	// Add custom template if we built one
	if templateImage != "" && templateImage != "docker/sandbox-templates:claude-code" {
		args = append(args, "--template", templateImage)
	}

	// Add Docker socket flag if needed
	if shouldMountDockerSocket(opts.Config, opts.ProjectConfig, opts.MountDockerSocket) {
		args = append(args, "--mount-docker-socket")
	}

	// Add auth token as environment variable if available
	if token, err := GetAuthToken(); err == nil && token != "" {
		args = append(args, "-e", "CLAUDE_CODE_OAUTH_TOKEN="+token)
		zlog.Debug("using stored auth token")
	}

	// Add volume mounts
	args = append(args, volumeMounts...)

	// Add the Claude image and CLI flags
	args = append(args, "claude")

	// Add Claude CLI flags after the image name
	claudeArgs := buildClaudeCLIArgs(claudeFlags)
	args = append(args, claudeArgs...)

	zlog.Debug("executing docker command",
		zap.String("cmd", "docker"),
		zap.Strings("args", args))

	// Execute docker sandbox run
	cmd := exec.Command("docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker sandbox failed: %w", err)
	}

	zlog.Info("Docker sandbox exited successfully")
	return nil
}

// mergeProfiles combines project and command-line profiles, removing duplicates
func mergeProfiles(projectProfiles, cmdProfiles []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, p := range projectProfiles {
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	for _, p := range cmdProfiles {
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return result
}

// buildVolumeMounts constructs the list of -v arguments for Docker volume mounts
func buildVolumeMounts(opts SandboxOptions, claudeMDPath string, claudeFlags *ClaudeFlags) ([]string, error) {
	var mounts []string

	// Note: Agents are now passed via --agents CLI flag (see buildClaudeCLIArgs)
	// We no longer mount ~/.claude/agents since the JSON format works better
	// and avoids issues with mounting inside Docker sandbox

	// Mount plugins cache directory once, then use --plugin-dir for each plugin
	// This avoids potential Docker limitations with many individual mounts
	pluginPaths, err := GetInstalledPluginPaths(opts.Config.ClaudeHome)
	if err != nil {
		zlog.Warn("failed to get installed plugins, skipping plugin sharing", zap.Error(err))
	} else if pluginPaths != nil {
		// Mount the entire cache directory
		mounts = append(mounts, "-v", fmt.Sprintf("%s:%s:ro", pluginPaths.HostCachePath, PluginCacheMountPath))
		zlog.Debug("mounting plugins cache directory",
			zap.String("host_path", pluginPaths.HostCachePath),
			zap.String("container_path", PluginCacheMountPath))

		// Add each plugin's container path for --plugin-dir
		claudeFlags.PluginDirs = append(claudeFlags.PluginDirs, pluginPaths.ContainerPaths...)
	}

	// Mount concatenated CLAUDE.md
	mounts = append(mounts, "-v", fmt.Sprintf("%s:/home/agent/.claude/CLAUDE.md:ro", claudeMDPath))
	zlog.Debug("mounting concatenated CLAUDE.md", zap.String("path", claudeMDPath))

	// Mount ~/.claude/credentials.json if it exists (for persistent auth)
	credentialsPath := filepath.Join(opts.Config.ClaudeHome, "credentials.json")
	if _, err := os.Stat(credentialsPath); err == nil {
		mounts = append(mounts, "-v", fmt.Sprintf("%s:/home/agent/.claude/credentials.json", credentialsPath))
		zlog.Debug("mounting credentials file", zap.String("path", credentialsPath))
	} else {
		zlog.Debug("credentials file not found, skipping", zap.String("path", credentialsPath))
	}

	// Mount ~/.claude/settings.json if it exists (MCP server configuration)
	settingsPath := filepath.Join(opts.Config.ClaudeHome, "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		mounts = append(mounts, "-v", fmt.Sprintf("%s:/home/agent/.claude/settings.json:ro", settingsPath))
		zlog.Debug("mounting MCP settings file", zap.String("path", settingsPath))
	} else {
		zlog.Debug("MCP settings file not found, skipping", zap.String("path", settingsPath))
	}

	// Mount ~/.claude/settings.local.json if it exists (local MCP overrides)
	settingsLocalPath := filepath.Join(opts.Config.ClaudeHome, "settings.local.json")
	if _, err := os.Stat(settingsLocalPath); err == nil {
		mounts = append(mounts, "-v", fmt.Sprintf("%s:/home/agent/.claude/settings.local.json:ro", settingsLocalPath))
		zlog.Debug("mounting local MCP settings file", zap.String("path", settingsLocalPath))
	} else {
		zlog.Debug("local MCP settings file not found, skipping", zap.String("path", settingsLocalPath))
	}

	// Mount ~/.ssh if it exists
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	sshPath := filepath.Join(homeDir, ".ssh")
	if _, err := os.Stat(sshPath); err == nil {
		mounts = append(mounts, "-v", fmt.Sprintf("%s:/home/agent/.ssh:ro", sshPath))
		zlog.Debug("mounting SSH directory", zap.String("path", sshPath))
	} else {
		zlog.Debug("SSH directory not found, skipping", zap.String("path", sshPath))
	}

	// Mount additional volumes from project config
	for _, vol := range opts.ProjectConfig.Volumes {
		hostPath, containerPath, readOnly, err := ParseVolumeSpec(vol)
		if err != nil {
			return nil, fmt.Errorf("invalid volume specification: %w", err)
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
		mounts = append(mounts, "-v", mountSpec)
		zlog.Debug("mounting additional volume",
			zap.String("host_path", hostPath),
			zap.String("container_path", containerPath),
			zap.Bool("read_only", readOnly))
	}

	zlog.Info("built volume mounts",
		zap.Int("mount_count", len(mounts)/2))

	return mounts, nil
}

// shouldMountDockerSocket determines whether to mount the Docker socket
// based on configuration, project config, and explicit flag
func shouldMountDockerSocket(config *Config, projectConfig *ProjectConfig, explicit bool) bool {
	// Explicit flag takes precedence
	if explicit {
		zlog.Debug("mounting Docker socket: explicit flag set")
		return true
	}

	// Project config override takes next precedence
	if projectConfig != nil && projectConfig.DockerSocket != "" {
		switch projectConfig.DockerSocket {
		case "always":
			zlog.Debug("mounting Docker socket: project config set to 'always'")
			return true
		case "never":
			zlog.Debug("not mounting Docker socket: project config set to 'never'")
			return false
		case "auto":
			zlog.Debug("docker socket setting: project config set to 'auto', using global config")
			// Fall through to global config
		default:
			zlog.Debug("unknown project docker_socket setting, using global config",
				zap.String("docker_socket", projectConfig.DockerSocket))
		}
	}

	// Check global config setting
	switch config.DockerSocket {
	case "always":
		zlog.Debug("mounting Docker socket: global config set to 'always'")
		return true
	case "never":
		zlog.Debug("not mounting Docker socket: global config set to 'never'")
		return false
	case "auto":
		// Default to mounting for convenience
		zlog.Debug("mounting Docker socket: global config set to 'auto' (default)")
		return true
	default:
		// Unknown setting, default to auto behavior
		zlog.Debug("mounting Docker socket: unknown global config setting, defaulting to 'auto'",
			zap.String("docker_socket", config.DockerSocket))
		return true
	}
}

// GetSandboxContainerName returns the expected container name for a project's sandbox.
// Docker sandbox uses a naming convention based on the workspace path.
func GetSandboxContainerName(workspaceDir string) (string, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Docker sandbox names containers based on workspace path
	// The format is typically: claude-sandbox-<hash> or similar
	// We need to find the running container that has the workspace mounted

	zlog.Debug("looking for sandbox container",
		zap.String("workspace", absPath))

	return absPath, nil
}

// IsInsideSandbox checks if we're currently running inside a Docker sandbox container
func IsInsideSandbox() bool {
	// Check for /.dockerenv file (exists in Docker containers)
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Check if running as 'agent' user (sandbox default user)
	if os.Getenv("USER") == "agent" || os.Getenv("HOME") == "/home/agent" {
		return true
	}

	return false
}

// SandboxContainer contains information about a running sandbox container
type SandboxContainer struct {
	// ID is the container ID
	ID string
	// Name is the container name (e.g., "claude-sandbox-2026-01-16-101831")
	Name string
	// Image is the container image name
	Image string
}

// DockerSandbox contains information about a Docker sandbox (from docker sandbox ls)
type DockerSandbox struct {
	// ID is the sandbox ID (used with docker sandbox rm)
	ID string
	// Status is the sandbox status (e.g., "running", "stopped")
	Status string
	// Workspace is the mounted workspace path
	Workspace string
}

// VolumeMount represents a single volume mount configuration
type VolumeMount struct {
	Source      string
	Destination string
	ReadOnly    bool
}

// FindRunningSandbox finds a running Docker sandbox container for the given workspace.
// Returns container info if found, nil if not running.
func FindRunningSandbox(workspaceDir string) (*SandboxContainer, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Try to resolve symlinks to get the real path for comparison
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If symlink resolution fails, just use the absolute path
		realPath = absPath
	}

	zlog.Debug("looking for sandbox container",
		zap.String("workspace", absPath),
		zap.String("real_path", realPath))

	// Try docker ps
	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		zlog.Debug("docker ps command failed",
			zap.Error(err),
			zap.String("stderr", stderr.String()))
		return nil, nil // No containers found or docker not accessible
	}

	output := stdout.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}

		containerID := parts[0]
		containerName := parts[1]
		imageName := ""
		if len(parts) > 2 {
			imageName = parts[2]
		}

		// Skip containers that aren't sandbox-related
		// Check for sandbox-related image or container name
		isSandbox := strings.Contains(imageName, "sandbox") ||
			strings.Contains(imageName, "claude") ||
			strings.Contains(containerName, "sandbox") ||
			strings.Contains(containerName, "claude") ||
			strings.Contains(imageName, "sbox-template")
		if !isSandbox {
			continue
		}

		// Check if this container has the workspace mounted
		inspectCmd := exec.Command("docker", "inspect", containerID, "--format", "{{range .Mounts}}{{.Source}}|{{.Destination}}\n{{end}}")
		var inspectOut bytes.Buffer
		inspectCmd.Stdout = &inspectOut

		if err := inspectCmd.Run(); err != nil {
			zlog.Debug("failed to inspect container",
				zap.String("container_id", containerID),
				zap.Error(err))
			continue
		}

		mounts := inspectOut.String()
		mountLines := strings.Split(strings.TrimSpace(mounts), "\n")

		for _, mountLine := range mountLines {
			if mountLine == "" {
				continue
			}

			mountParts := strings.Split(mountLine, "|")
			if len(mountParts) != 2 {
				continue
			}

			source := mountParts[0]
			// Also resolve symlinks in mount source for comparison
			sourceReal, err := filepath.EvalSymlinks(source)
			if err != nil {
				sourceReal = source
			}

			// Check if the workspace path matches (either exact or real path)
			if source == absPath || source == realPath ||
				sourceReal == absPath || sourceReal == realPath {
				zlog.Debug("found sandbox container for workspace",
					zap.String("container_id", containerID),
					zap.String("container_name", containerName),
					zap.String("workspace", absPath),
					zap.String("mount_source", source))
				return &SandboxContainer{
					ID:    containerID,
					Name:  containerName,
					Image: imageName,
				}, nil
			}
		}
	}

	zlog.Debug("no running sandbox found for workspace",
		zap.String("workspace", absPath),
		zap.String("real_path", realPath))
	return nil, nil
}

// ConnectShell connects to a running sandbox container's bash shell.
// Returns an error if no sandbox is running for the given workspace.
func ConnectShell(workspaceDir string) error {
	// Check if we're already inside a sandbox
	if IsInsideSandbox() {
		return fmt.Errorf("you are already inside a sandbox container\nUse 'bash' to open a new shell, or exit and run 'sbox shell' from the host")
	}

	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	container, err := FindRunningSandbox(absPath)
	if err != nil {
		return fmt.Errorf("failed to find running sandbox: %w", err)
	}

	if container == nil {
		return fmt.Errorf("no sandbox container is running for workspace: %s\nStart a sandbox first with: sbox run", absPath)
	}

	zlog.Info("connecting to sandbox shell",
		zap.String("container_id", container.ID),
		zap.String("container_name", container.Name),
		zap.String("workspace", absPath))

	// Execute docker exec -it <container> bash
	cmd := exec.Command("docker", "exec", "-it", container.ID, "bash")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker exec failed: %w", err)
	}

	return nil
}

// StopSandbox stops the running Docker sandbox container for the given workspace.
// If remove is true, the container is also removed after stopping.
// Returns the stopped container info, or nil if no container was running.
func StopSandbox(workspaceDir string, remove bool) (*SandboxContainer, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	container, err := FindRunningSandbox(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find running sandbox: %w", err)
	}

	if container == nil {
		zlog.Debug("no running sandbox to stop", zap.String("workspace", absPath))
		return nil, nil
	}

	zlog.Info("stopping sandbox container",
		zap.String("container_id", container.ID),
		zap.String("container_name", container.Name),
		zap.String("workspace", absPath))

	// Stop the container
	stopCmd := exec.Command("docker", "stop", container.ID)
	var stderr bytes.Buffer
	stopCmd.Stderr = &stderr

	if err := stopCmd.Run(); err != nil {
		return nil, fmt.Errorf("docker stop failed: %w (stderr: %s)", err, stderr.String())
	}

	zlog.Info("sandbox container stopped",
		zap.String("container_id", container.ID),
		zap.String("container_name", container.Name))

	// Remove the container if requested
	if remove {
		zlog.Info("removing sandbox container",
			zap.String("container_id", container.ID),
			zap.String("container_name", container.Name))

		rmCmd := exec.Command("docker", "rm", container.ID)
		rmCmd.Stderr = &stderr

		if err := rmCmd.Run(); err != nil {
			return nil, fmt.Errorf("docker rm failed: %w (stderr: %s)", err, stderr.String())
		}

		zlog.Info("sandbox container removed",
			zap.String("container_id", container.ID),
			zap.String("container_name", container.Name))
	}

	return container, nil
}

// ListDockerSandboxes returns all Docker sandboxes from docker sandbox ls
func ListDockerSandboxes() ([]DockerSandbox, error) {
	cmd := exec.Command("docker", "sandbox", "ls", "--format", "{{.ID}}\t{{.Status}}\t{{.Workspace}}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		zlog.Debug("docker sandbox ls command failed",
			zap.Error(err),
			zap.String("stderr", stderr.String()))
		return nil, fmt.Errorf("docker sandbox ls failed: %w", err)
	}

	var sandboxes []DockerSandbox
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		sandboxes = append(sandboxes, DockerSandbox{
			ID:        parts[0],
			Status:    parts[1],
			Workspace: parts[2],
		})
	}

	return sandboxes, nil
}

// FindDockerSandbox finds a Docker sandbox for the given workspace directory.
// Returns the sandbox if found, nil otherwise.
func FindDockerSandbox(workspaceDir string) (*DockerSandbox, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Try to resolve symlinks for comparison
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		realPath = absPath
	}

	sandboxes, err := ListDockerSandboxes()
	if err != nil {
		return nil, err
	}

	for _, sb := range sandboxes {
		// Resolve symlinks in sandbox workspace for comparison
		sbReal, err := filepath.EvalSymlinks(sb.Workspace)
		if err != nil {
			sbReal = sb.Workspace
		}

		if sb.Workspace == absPath || sb.Workspace == realPath ||
			sbReal == absPath || sbReal == realPath {
			zlog.Debug("found docker sandbox for workspace",
				zap.String("sandbox_id", sb.ID),
				zap.String("workspace", absPath))
			return &sb, nil
		}
	}

	return nil, nil
}

// RemoveDockerSandbox removes a Docker sandbox using docker sandbox rm.
// This properly cleans up the sandbox including its stored configuration.
func RemoveDockerSandbox(sandboxID string) error {
	zlog.Info("removing docker sandbox",
		zap.String("sandbox_id", sandboxID))

	cmd := exec.Command("docker", "sandbox", "rm", sandboxID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker sandbox rm failed: %w (stderr: %s)", err, stderr.String())
	}

	zlog.Info("docker sandbox removed",
		zap.String("sandbox_id", sandboxID))
	return nil
}

// GetSandboxMounts retrieves the current volume mounts for a running sandbox container.
func GetSandboxMounts(containerID string) ([]VolumeMount, error) {
	cmd := exec.Command("docker", "inspect", containerID, "--format", "{{range .Mounts}}{{.Source}}|{{.Destination}}|{{.RW}}\n{{end}}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker inspect failed: %w", err)
	}

	var mounts []VolumeMount
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			continue
		}

		mounts = append(mounts, VolumeMount{
			Source:      parts[0],
			Destination: parts[1],
			ReadOnly:    parts[2] == "false",
		})
	}

	return mounts, nil
}

// GetExpectedMounts returns the volume mounts that would be configured for a new sandbox.
// This parses the buildVolumeMounts output to extract mount specifications.
func GetExpectedMounts(opts SandboxOptions, claudeMDPath string) ([]VolumeMount, error) {
	// Create a temporary ClaudeFlags to pass to buildVolumeMounts
	// We don't actually use the flags here, just need the volume list
	tempFlags := &ClaudeFlags{}
	volumeArgs, err := buildVolumeMounts(opts, claudeMDPath, tempFlags)
	if err != nil {
		return nil, err
	}

	var mounts []VolumeMount
	for i := 0; i < len(volumeArgs); i += 2 {
		if volumeArgs[i] != "-v" {
			continue
		}
		if i+1 >= len(volumeArgs) {
			break
		}

		spec := volumeArgs[i+1]
		mount, err := parseVolumeMount(spec)
		if err != nil {
			continue
		}
		mounts = append(mounts, mount)
	}

	return mounts, nil
}

// parseVolumeMount parses a volume mount specification like "host:container:ro"
func parseVolumeMount(spec string) (VolumeMount, error) {
	parts := strings.Split(spec, ":")

	if len(parts) < 2 {
		return VolumeMount{}, fmt.Errorf("invalid volume spec: %s", spec)
	}

	mount := VolumeMount{
		Source:      parts[0],
		Destination: parts[1],
		ReadOnly:    false,
	}

	if len(parts) >= 3 && parts[2] == "ro" {
		mount.ReadOnly = true
	}

	return mount, nil
}

// MountMismatch represents a difference between expected and actual mounts
type MountMismatch struct {
	// Missing mounts that should exist but don't
	Missing []VolumeMount
	// Extra mounts that exist but shouldn't (usually not a problem)
	Extra []VolumeMount
}

// CheckMountMismatch compares expected mounts with actual sandbox mounts.
// Returns nil if there's no significant mismatch.
func CheckMountMismatch(containerID string, expectedMounts []VolumeMount) (*MountMismatch, error) {
	actualMounts, err := GetSandboxMounts(containerID)
	if err != nil {
		return nil, err
	}

	// Build a map of actual mounts by destination
	actualByDest := make(map[string]VolumeMount)
	for _, m := range actualMounts {
		actualByDest[m.Destination] = m
	}

	var missing []VolumeMount
	for _, expected := range expectedMounts {
		actual, exists := actualByDest[expected.Destination]
		if !exists {
			missing = append(missing, expected)
			continue
		}

		// Also check if source path differs (symlink resolved comparison)
		expectedReal, _ := filepath.EvalSymlinks(expected.Source)
		actualReal, _ := filepath.EvalSymlinks(actual.Source)
		if expectedReal == "" {
			expectedReal = expected.Source
		}
		if actualReal == "" {
			actualReal = actual.Source
		}

		if expected.Source != actual.Source && expectedReal != actualReal {
			missing = append(missing, expected)
		}
	}

	if len(missing) == 0 {
		return nil, nil
	}

	return &MountMismatch{
		Missing: missing,
	}, nil
}
