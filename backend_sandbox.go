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

// SandboxBackend implements the Backend interface using Docker sandbox (MicroVM)
type SandboxBackend struct {
	config *Config
}

// NewSandboxBackend creates a new sandbox backend instance
func NewSandboxBackend(config *Config) *SandboxBackend {
	return &SandboxBackend{config: config}
}

// Name returns the backend type name
func (b *SandboxBackend) Name() BackendType {
	return BackendSandbox
}

// Run starts or attaches to a sandbox for the given workspace
func (b *SandboxBackend) Run(opts BackendOptions) error {
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
	if err := PrepareSboxDirectory(opts.WorkspaceDir, opts.Config, opts.Config.Envs, opts.ProjectConfig.Envs, sboxFileEnvs); err != nil {
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

// Shell opens a shell in the running sandbox
func (b *SandboxBackend) Shell(workspaceDir string) error {
	// Check if we're already inside a sandbox
	if IsInsideSandbox() {
		return fmt.Errorf("you are already inside a sandbox container\nUse 'bash' to open a new shell, or exit and run 'sbox shell' from the host")
	}

	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	info, err := b.FindRunning(absPath)
	if err != nil {
		return fmt.Errorf("failed to find running sandbox: %w", err)
	}

	if info == nil {
		return fmt.Errorf("no sandbox is running for workspace: %s\nStart a sandbox first with: sbox run", absPath)
	}

	zlog.Info("connecting to sandbox shell",
		zap.String("sandbox_id", info.ID),
		zap.String("sandbox_name", info.Name),
		zap.String("workspace", absPath))

	// Execute docker sandbox exec -it <sandbox> bash
	cmd := exec.Command("docker", "sandbox", "exec", "-it", info.ID, "bash")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker sandbox exec failed: %w", err)
	}

	return nil
}

// Stop stops the sandbox, optionally removing it
func (b *SandboxBackend) Stop(workspaceDir string, remove bool) (*ContainerInfo, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// First check if there's a running sandbox
	info, err := b.FindRunning(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find running sandbox: %w", err)
	}

	if info == nil {
		zlog.Debug("no running sandbox to stop", zap.String("workspace", absPath))
		return nil, nil
	}

	zlog.Info("stopping sandbox",
		zap.String("sandbox_id", info.ID),
		zap.String("sandbox_name", info.Name),
		zap.String("workspace", absPath))

	// Stop the sandbox using docker sandbox stop
	stopCmd := exec.Command("docker", "sandbox", "stop", info.ID)
	var stderr bytes.Buffer
	stopCmd.Stderr = &stderr

	if err := stopCmd.Run(); err != nil {
		return nil, fmt.Errorf("docker sandbox stop failed: %w (stderr: %s)", err, stderr.String())
	}

	zlog.Info("sandbox stopped",
		zap.String("sandbox_id", info.ID),
		zap.String("sandbox_name", info.Name))

	// Remove the sandbox if requested using docker sandbox rm
	if remove {
		zlog.Info("removing sandbox",
			zap.String("sandbox_id", info.ID),
			zap.String("sandbox_name", info.Name))

		rmCmd := exec.Command("docker", "sandbox", "rm", info.ID)
		rmCmd.Stderr = &stderr

		if err := rmCmd.Run(); err != nil {
			return nil, fmt.Errorf("docker sandbox rm failed: %w (stderr: %s)", err, stderr.String())
		}

		zlog.Info("sandbox removed",
			zap.String("sandbox_id", info.ID),
			zap.String("sandbox_name", info.Name))
	}

	return info, nil
}

// Find returns container info for the given workspace (any state)
func (b *SandboxBackend) Find(workspaceDir string) (*ContainerInfo, error) {
	sandbox, err := FindDockerSandbox(workspaceDir)
	if err != nil {
		return nil, err
	}

	if sandbox == nil {
		return nil, nil
	}

	return &ContainerInfo{
		ID:        sandbox.ID,
		Name:      sandbox.Name,
		Status:    sandbox.Status,
		Image:     sandbox.Image,
		Workspace: sandbox.Workspace,
		Backend:   BackendSandbox,
	}, nil
}

// FindRunning returns container info only if running
func (b *SandboxBackend) FindRunning(workspaceDir string) (*ContainerInfo, error) {
	info, err := b.Find(workspaceDir)
	if err != nil {
		return nil, err
	}

	if info == nil {
		return nil, nil
	}

	// Only return if the sandbox is running
	if info.Status != "running" {
		zlog.Debug("sandbox found but not running",
			zap.String("sandbox_id", info.ID),
			zap.String("status", info.Status))
		return nil, nil
	}

	return info, nil
}

// List returns all sandboxes managed by this backend
func (b *SandboxBackend) List() ([]ContainerInfo, error) {
	sandboxes, err := ListDockerSandboxes()
	if err != nil {
		return nil, err
	}

	var infos []ContainerInfo
	for _, sb := range sandboxes {
		infos = append(infos, ContainerInfo{
			ID:        sb.ID,
			Name:      sb.Name,
			Status:    sb.Status,
			Image:     sb.Image,
			Workspace: sb.Workspace,
			Backend:   BackendSandbox,
		})
	}

	return infos, nil
}

// Remove removes a sandbox by ID
func (b *SandboxBackend) Remove(containerID string) error {
	return RemoveDockerSandbox(containerID)
}

// Cleanup removes all backend-specific resources for a workspace
func (b *SandboxBackend) Cleanup(workspaceDir string) error {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Remove .sbox directory
	sboxDir := filepath.Join(absPath, ".sbox")
	if err := os.RemoveAll(sboxDir); err != nil {
		return fmt.Errorf("failed to remove .sbox directory: %w", err)
	}

	zlog.Info(".sbox directory removed", zap.String("path", sboxDir))
	return nil
}

// SaveCache saves the .claude state to .sbox/claude-cache/ for persistence
func (b *SandboxBackend) SaveCache(workspaceDir string) error {
	return SaveClaudeCache(workspaceDir, b)
}

