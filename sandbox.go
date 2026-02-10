package sbox

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

// DefaultTemplateImage is the default Docker sandbox template image for Claude
const DefaultTemplateImage = "docker/sandbox-templates:claude-code"

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

	// Debug enables debug mode for docker sandbox commands
	Debug bool

	// Config is the global configuration
	Config *Config

	// ProjectConfig is the per-project configuration
	ProjectConfig *ProjectConfig

	// SboxFile is the loaded sbox.yaml file configuration (if any)
	SboxFile *SboxFileLocation
}

// SandboxCommands holds the docker commands for creating and running a sandbox.
// Used for display purposes (e.g., sbox info).
type SandboxCommands struct {
	// CreateArgs are the arguments for `docker sandbox create ...`
	CreateArgs []string
	// RunArgs are the arguments for `docker sandbox run ...`
	RunArgs []string
}

// BuildSandboxCommands builds both the create and run docker sandbox commands.
// This is the single source of truth for command construction, used for display purposes.
// Returns the argument lists (not including "docker" itself).
func BuildSandboxCommands(opts SandboxOptions) (*SandboxCommands, error) {
	// Generate sandbox name
	sandboxName := opts.ProjectConfig.SandboxName
	if sandboxName == "" {
		var err error
		sandboxName, err = GenerateSandboxName(opts.WorkspaceDir)
		if err != nil {
			return nil, fmt.Errorf("failed to generate sandbox name: %w", err)
		}
	}

	// Merge project profiles with command-line profiles
	allProfiles := mergeProfiles(opts.ProjectConfig.Profiles, opts.Profiles)

	// Build template image name if profiles are specified
	var templateImage string
	if len(allProfiles) > 0 {
		builder := NewTemplateBuilder(opts.Config, allProfiles)
		templateImage = builder.ImageName()
	}

	// Build create command args
	createArgs := []string{"sandbox", "create", "--name", sandboxName}

	// Add custom template if we have one and it's different from default
	if templateImage != "" && templateImage != DefaultTemplateImage {
		// --load-local-template is required for locally built images
		createArgs = append(createArgs, "--load-local-template", "--template", templateImage)
	}

	// Add the Claude agent and workspace path
	absPath, _ := filepath.Abs(opts.WorkspaceDir)
	createArgs = append(createArgs, "claude", absPath)

	// Build run command args (always the same simple form)
	runArgs := []string{"sandbox", "run", sandboxName}

	return &SandboxCommands{
		CreateArgs: createArgs,
		RunArgs:    runArgs,
	}, nil
}

// RunSandbox executes the Docker sandbox with all configured mounts and settings.
// If the sandbox doesn't exist, it creates it first using `docker sandbox create`.
// Then runs the sandbox using `docker sandbox run <name>`.
func RunSandbox(opts SandboxOptions) error {
	// Get sandbox name (should already be set by caller)
	sandboxName := opts.ProjectConfig.SandboxName
	if sandboxName == "" {
		var err error
		sandboxName, err = GenerateSandboxName(opts.WorkspaceDir)
		if err != nil {
			return fmt.Errorf("failed to generate sandbox name: %w", err)
		}
		opts.ProjectConfig.SandboxName = sandboxName
	}

	// Merge project profiles with command-line profiles
	allProfiles := mergeProfiles(opts.ProjectConfig.Profiles, opts.Profiles)

	zlog.Info("preparing to run Docker sandbox",
		zap.String("name", sandboxName),
		zap.String("workspace", opts.WorkspaceDir),
		zap.Strings("profiles", allProfiles))

	// Prepare .sbox directory with plugins, agents, and env vars
	// This happens every time since host config may have changed
	var sboxFileEnvs []string
	if opts.SboxFile != nil && opts.SboxFile.Config != nil {
		sboxFileEnvs = opts.SboxFile.Config.Envs
	}
	if err := PrepareSboxDirectory(opts.WorkspaceDir, opts.Config, opts.Config.Envs, opts.ProjectConfig.Envs, sboxFileEnvs, BackendSandbox); err != nil {
		return fmt.Errorf("failed to prepare .sbox directory: %w", err)
	}

	// Check if sandbox exists
	existingSandbox, err := FindDockerSandboxByName(sandboxName)
	if err != nil {
		zlog.Debug("failed to check for existing sandbox", zap.Error(err))
	}

	// Create sandbox if it doesn't exist
	if existingSandbox == nil {
		// Build custom template only when creating a new sandbox
		// Template is now always required for sbox entrypoint
		builder := NewTemplateBuilder(opts.Config, allProfiles)
		templateImage, err := builder.Build(opts.ForceRebuild)
		if err != nil {
			return fmt.Errorf("failed to build custom template: %w", err)
		}

		fmt.Printf("Creating sandbox '%s'...\n", sandboxName)
		zlog.Info("sandbox does not exist, creating",
			zap.String("name", sandboxName),
			zap.String("workspace", opts.WorkspaceDir),
			zap.String("template", templateImage))

		if err := CreateDockerSandbox(sandboxName, opts.WorkspaceDir, templateImage, opts.Debug); err != nil {
			// Check if the error is "already exists" - this can happen if our sandbox
			// lookup failed but the sandbox actually exists
			if strings.Contains(err.Error(), "already exists") {
				zlog.Info("sandbox already exists (detected from create error)",
					zap.String("name", sandboxName))
				fmt.Printf("Sandbox '%s' already exists\n", sandboxName)
			} else {
				return fmt.Errorf("failed to create sandbox: %w", err)
			}
		} else {
			fmt.Printf("Sandbox '%s' created\n", sandboxName)
		}
	} else {
		fmt.Printf("Using existing sandbox '%s'\n", sandboxName)
		zlog.Info("sandbox already exists",
			zap.String("name", sandboxName),
			zap.String("sandbox_id", existingSandbox.ID),
			zap.String("status", existingSandbox.Status))
	}

	// Build run command: docker sandbox [--debug] run <name>
	// Note: Extra args are not supported for existing sandboxes
	args := []string{"sandbox"}
	if opts.Debug {
		args = append(args, "--debug")
	}
	args = append(args, "run", sandboxName)

	zlog.Debug("executing docker command",
		zap.String("cmd", "docker"),
		zap.Strings("args", args),
		zap.Bool("debug", opts.Debug))

	fmt.Printf("Starting sandbox '%s'...\n", sandboxName)

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

	// NOTE: CLAUDE.md is now copied to .sbox/ and installed by the entrypoint
	// Volume mounts don't work with Docker sandbox MicroVMs
	_ = claudeMDPath // Unused, kept for backwards compatibility in function signature

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

// shouldMountDockerSocket is deprecated - Docker is automatically available in MicroVM sandboxes.
// This function is kept for backwards compatibility but always returns false.
// The docker_socket config setting is ignored.
func shouldMountDockerSocket(config *Config, projectConfig *ProjectConfig, explicit bool) bool {
	// With MicroVM sandboxes, Docker is automatically available inside the sandbox
	// with its own daemon. No explicit mounting is needed.
	return false
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
	// Name is the sandbox name
	Name string
	// Status is the sandbox status (e.g., "running", "stopped")
	Status string
	// Image is the sandbox image
	Image string
	// Workspace is the mounted workspace path
	Workspace string
}

// FindRunningSandbox finds a running Docker sandbox for the given workspace.
// Returns sandbox info if found and running, nil if not running.
// Note: With Docker Sandbox MicroVMs, sandboxes don't appear in `docker ps`.
// We use `docker sandbox ls` instead.
func FindRunningSandbox(workspaceDir string) (*SandboxContainer, error) {
	sandbox, err := FindDockerSandbox(workspaceDir)
	if err != nil {
		return nil, err
	}

	if sandbox == nil {
		return nil, nil
	}

	// Only return if the sandbox is running
	if sandbox.Status != "running" {
		zlog.Debug("sandbox found but not running",
			zap.String("sandbox_id", sandbox.ID),
			zap.String("status", sandbox.Status))
		return nil, nil
	}

	return &SandboxContainer{
		ID:    sandbox.ID,
		Name:  sandbox.Name,
		Image: sandbox.Image,
	}, nil
}

// ConnectShell connects to a running sandbox's bash shell.
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

	sandbox, err := FindRunningSandbox(absPath)
	if err != nil {
		return fmt.Errorf("failed to find running sandbox: %w", err)
	}

	if sandbox == nil {
		return fmt.Errorf("no sandbox is running for workspace: %s\nStart a sandbox first with: sbox run", absPath)
	}

	zlog.Info("connecting to sandbox shell",
		zap.String("sandbox_id", sandbox.ID),
		zap.String("sandbox_name", sandbox.Name),
		zap.String("workspace", absPath))

	// Execute docker sandbox exec -it <sandbox> bash
	cmd := exec.Command("docker", "sandbox", "exec", "-it", sandbox.ID, "bash")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker sandbox exec failed: %w", err)
	}

	return nil
}

// StopSandbox stops the running Docker sandbox for the given workspace.
// If remove is true, the sandbox is also removed after stopping.
// Returns the stopped sandbox info, or nil if no sandbox was running.
func StopSandbox(workspaceDir string, remove bool) (*SandboxContainer, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// First check if there's a running sandbox
	sandbox, err := FindRunningSandbox(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find running sandbox: %w", err)
	}

	if sandbox == nil {
		zlog.Debug("no running sandbox to stop", zap.String("workspace", absPath))
		return nil, nil
	}

	zlog.Info("stopping sandbox",
		zap.String("sandbox_id", sandbox.ID),
		zap.String("sandbox_name", sandbox.Name),
		zap.String("workspace", absPath))

	// Stop the sandbox using docker sandbox stop
	stopCmd := exec.Command("docker", "sandbox", "stop", sandbox.ID)
	var stderr bytes.Buffer
	stopCmd.Stderr = &stderr

	if err := stopCmd.Run(); err != nil {
		return nil, fmt.Errorf("docker sandbox stop failed: %w (stderr: %s)", err, stderr.String())
	}

	zlog.Info("sandbox stopped",
		zap.String("sandbox_id", sandbox.ID),
		zap.String("sandbox_name", sandbox.Name))

	// Remove the sandbox if requested using docker sandbox rm
	if remove {
		zlog.Info("removing sandbox",
			zap.String("sandbox_id", sandbox.ID),
			zap.String("sandbox_name", sandbox.Name))

		rmCmd := exec.Command("docker", "sandbox", "rm", sandbox.ID)
		rmCmd.Stderr = &stderr

		if err := rmCmd.Run(); err != nil {
			return nil, fmt.Errorf("docker sandbox rm failed: %w (stderr: %s)", err, stderr.String())
		}

		zlog.Info("sandbox removed",
			zap.String("sandbox_id", sandbox.ID),
			zap.String("sandbox_name", sandbox.Name))
	}

	return sandbox, nil
}

// ListDockerSandboxes returns all Docker sandboxes from docker sandbox ls
func ListDockerSandboxes() ([]DockerSandbox, error) {
	cmd := exec.Command("docker", "sandbox", "ls")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		zlog.Debug("docker sandbox ls command failed",
			zap.Error(err),
			zap.String("stderr", stderr.String()))
		return nil, fmt.Errorf("docker sandbox ls failed: %w", err)
	}

	return parseSandboxLsOutput(stdout.String())
}

// parseSandboxLsOutput parses the table output from `docker sandbox ls`.
// The output is a fixed-width column table with headers:
//
//	SANDBOX   AGENT   STATUS   WORKSPACE
func parseSandboxLsOutput(output string) ([]DockerSandbox, error) {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) < 2 {
		return nil, nil
	}

	header := lines[0]

	// Find column start positions from the header
	type col struct {
		name  string
		start int
	}
	colNames := []string{"SANDBOX", "AGENT", "STATUS", "WORKSPACE"}
	var cols []col
	for _, name := range colNames {
		idx := strings.Index(header, name)
		if idx < 0 {
			return nil, fmt.Errorf("docker sandbox ls: missing column %q in header: %s", name, header)
		}
		cols = append(cols, col{name: name, start: idx})
	}

	// Extract field from a line using column positions
	extractField := func(line string, colIdx int) string {
		start := cols[colIdx].start
		if start >= len(line) {
			return ""
		}
		var end int
		if colIdx+1 < len(cols) {
			end = cols[colIdx+1].start
		} else {
			end = len(line)
		}
		if end > len(line) {
			end = len(line)
		}
		return strings.TrimSpace(line[start:end])
	}

	var sandboxes []DockerSandbox
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}

		name := extractField(line, 0)
		workspace := extractField(line, 3)
		// "-" means no workspace
		if workspace == "-" {
			workspace = ""
		}

		sandboxes = append(sandboxes, DockerSandbox{
			ID:        name, // In new format, sandbox name is the ID
			Name:      name,
			Image:     extractField(line, 1), // AGENT column
			Status:    extractField(line, 2),
			Workspace: workspace,
		})
	}

	return sandboxes, nil
}

// GenerateSandboxName generates a sandbox name from a workspace path.
// Format: sbox-claude-<basename of workspace>
// The name is sanitized to only contain letters, numbers, hyphens, and underscores.
func GenerateSandboxName(workspaceDir string) (string, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	basename := filepath.Base(absPath)

	// Sanitize: keep only letters, numbers, hyphens, and underscores
	reg := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	sanitized := reg.ReplaceAllString(basename, "-")

	// Remove consecutive hyphens
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}

	// Trim leading/trailing hyphens
	sanitized = strings.Trim(sanitized, "-")

	if sanitized == "" {
		sanitized = "workspace"
	}

	return "sbox-claude-" + sanitized, nil
}

// FindDockerSandboxByName finds a Docker sandbox by its name.
// Returns the sandbox if found, nil otherwise.
func FindDockerSandboxByName(name string) (*DockerSandbox, error) {
	sandboxes, err := ListDockerSandboxes()
	if err != nil {
		return nil, err
	}

	for _, sb := range sandboxes {
		if sb.Name == name {
			zlog.Debug("found docker sandbox by name",
				zap.String("sandbox_name", name),
				zap.String("sandbox_id", sb.ID))
			return &sb, nil
		}
	}

	return nil, nil
}

// CreateDockerSandbox creates a new Docker sandbox with the given name and options.
// Uses `docker sandbox create` command.
func CreateDockerSandbox(name string, workspaceDir string, templateImage string, debug bool) error {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Build create command: docker sandbox [--debug] create --name <name> [--load-local-template] [--template <image>] claude <workspace>
	args := []string{"sandbox"}
	if debug {
		args = append(args, "--debug")
	}
	args = append(args, "create", "--name", name)

	// Add custom template if specified and different from default
	if templateImage != "" && templateImage != DefaultTemplateImage {
		// --load-local-template is required for locally built images
		args = append(args, "--load-local-template", "--template", templateImage)
	}

	// Add the agent and workspace
	args = append(args, "claude", absPath)

	zlog.Info("creating docker sandbox",
		zap.String("name", name),
		zap.String("workspace", absPath),
		zap.String("template", templateImage),
		zap.Bool("debug", debug),
		zap.Strings("args", args))

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker sandbox create failed: %w", err)
	}

	zlog.Info("docker sandbox created",
		zap.String("name", name))
	return nil
}

// FindDockerSandbox finds a Docker sandbox for the given workspace directory.
// First tries to find by sandbox name (if stored in project config), then by workspace path.
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

	// First, try to find by the expected sandbox name
	expectedName, _ := GenerateSandboxName(workspaceDir)
	for _, sb := range sandboxes {
		if sb.Name == expectedName {
			zlog.Debug("found docker sandbox by name",
				zap.String("sandbox_id", sb.ID),
				zap.String("sandbox_name", expectedName),
				zap.String("workspace", absPath))
			return &sb, nil
		}
	}

	// Fall back to matching by workspace path
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

// RemoveDockerSandboxByName removes a Docker sandbox by name using docker sandbox rm.
// This is useful when the sandbox lookup fails but we know the name.
func RemoveDockerSandboxByName(name string) error {
	zlog.Info("removing docker sandbox by name",
		zap.String("name", name))

	cmd := exec.Command("docker", "sandbox", "rm", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker sandbox rm failed: %w (stderr: %s)", err, stderr.String())
	}

	zlog.Info("docker sandbox removed",
		zap.String("name", name))
	return nil
}

