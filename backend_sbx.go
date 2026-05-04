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

// SbxBackend implements the Backend interface using the top-level `sbx` CLI (Docker Sandbox MicroVM).
type SbxBackend struct {
	config *Config
}

// NewSbxBackend creates a new sbx backend instance.
func NewSbxBackend(config *Config) *SbxBackend {
	return &SbxBackend{config: config}
}

// Name returns the backend type name.
func (b *SbxBackend) Name() BackendType {
	return BackendSbx
}

// Run starts or attaches to an sbx sandbox for the given workspace.
func (b *SbxBackend) Run(opts BackendOptions) error {
	agentType := AgentType(opts.ProjectConfig.Agent)
	if agentType == "" {
		agentType = DefaultAgent
	}

	sandboxName := opts.ProjectConfig.SandboxName
	if sandboxName == "" {
		var err error
		sandboxName, err = GenerateSbxName(opts.WorkspaceDir, agentType)
		if err != nil {
			return fmt.Errorf("failed to generate sbx sandbox name: %w", err)
		}
		opts.ProjectConfig.SandboxName = sandboxName
	}

	allProfiles := mergeProfiles(opts.ProjectConfig.Profiles, opts.Profiles)

	zlog.Info("preparing to run sbx sandbox",
		zap.String("name", sandboxName),
		zap.String("workspace", opts.WorkspaceDir),
		zap.Strings("profiles", allProfiles))

	var sboxFileEnvs []string
	if opts.SboxFile != nil && opts.SboxFile.Config != nil {
		sboxFileEnvs = opts.SboxFile.Config.Envs
	}
	if err := PrepareSboxDirectory(opts.WorkspaceDir, opts.Config, opts.Config.Envs, opts.ProjectConfig.Envs, sboxFileEnvs, BackendSbx, agentType, opts); err != nil {
		return fmt.Errorf("failed to prepare .sbox directory: %w", err)
	}

	existingSandbox, err := FindSbxSandboxByName(sandboxName)
	if err != nil {
		zlog.Debug("failed to check for existing sbx sandbox", zap.Error(err))
	}

	if existingSandbox == nil {
		builder := NewTemplateBuilder(opts.Config, allProfiles, agentType)
		templateImage, err := builder.Build(opts.ForceRebuild)
		if err != nil {
			return fmt.Errorf("failed to build custom template: %w", err)
		}

		DefaultUI.Status("Creating sbx sandbox '%s'", sandboxName)
		zlog.Info("sbx sandbox does not exist, creating",
			zap.String("name", sandboxName),
			zap.String("workspace", opts.WorkspaceDir),
			zap.String("template", templateImage))

		var extraWorkspaceDirs []string
		if mainRepoRoot, ok := detectGitWorktree(opts.WorkspaceDir); ok {
			extraWorkspaceDirs = append(extraWorkspaceDirs, mainRepoRoot)
			zlog.Info("workspace is a git worktree, syncing main repo root into sbx sandbox",
				zap.String("main_repo_root", mainRepoRoot))
		}

		if err := CreateSbxSandbox(sandboxName, opts.WorkspaceDir, templateImage, agentType, opts.Debug, extraWorkspaceDirs...); err != nil {
			if strings.Contains(err.Error(), "already exists") {
				zlog.Info("sbx sandbox already exists (detected from create error)",
					zap.String("name", sandboxName))
			} else {
				return fmt.Errorf("failed to create sbx sandbox: %w", err)
			}
		}
	} else {
		zlog.Info("sbx sandbox already exists",
			zap.String("name", sandboxName),
			zap.String("status", existingSandbox.Status))
	}

	DefaultUI.Status("Starting sbx sandbox '%s'", sandboxName)

	args := []string{}
	if opts.Debug {
		args = append(args, "--debug")
	}
	args = append(args, "run", sandboxName)

	zlog.Debug("executing sbx command",
		zap.Strings("args", args),
		zap.Bool("debug", opts.Debug))

	cmd := exec.Command("sbx", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sbx run failed: %w", err)
	}

	zlog.Info("sbx sandbox exited successfully")
	return nil
}

// Shell opens a shell in the running sbx sandbox.
func (b *SbxBackend) Shell(workspaceDir string) error {
	if IsInsideSandbox() {
		return fmt.Errorf("you are already inside a sandbox container\nUse 'bash' to open a new shell, or exit and run 'sbox shell' from the host")
	}

	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	info, err := b.FindRunning(absPath)
	if err != nil {
		return fmt.Errorf("failed to find running sbx sandbox: %w", err)
	}

	if info == nil {
		return fmt.Errorf("no sbx sandbox is running for workspace: %s\nStart a sandbox first with: sbox run", absPath)
	}

	zlog.Info("connecting to sbx sandbox shell",
		zap.String("sandbox_name", info.Name),
		zap.String("workspace", absPath))

	cmd := exec.Command("sbx", "exec", "-it", info.ID, "bash")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sbx exec failed: %w", err)
	}

	return nil
}

// Stop stops the sbx sandbox, optionally removing it.
func (b *SbxBackend) Stop(workspaceDir string, remove bool) (*ContainerInfo, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	info, err := b.FindRunning(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find running sbx sandbox: %w", err)
	}

	if info == nil && remove {
		info, err = b.Find(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to find sbx sandbox: %w", err)
		}
		if info != nil {
			zlog.Debug("found stopped sbx sandbox to remove",
				zap.String("sandbox_name", info.Name),
				zap.String("status", info.Status))
		}
	}

	if info == nil {
		zlog.Debug("no sbx sandbox to stop or remove", zap.String("workspace", absPath))
		return nil, nil
	}

	var stderr bytes.Buffer

	if info.Status == "running" {
		zlog.Info("stopping sbx sandbox",
			zap.String("sandbox_name", info.Name),
			zap.String("workspace", absPath))

		stopCmd := exec.Command("sbx", "stop", info.ID)
		stopCmd.Stderr = &stderr

		if err := stopCmd.Run(); err != nil {
			return nil, fmt.Errorf("sbx stop failed: %w (stderr: %s)", err, stderr.String())
		}

		zlog.Info("sbx sandbox stopped", zap.String("sandbox_name", info.Name))
	} else {
		zlog.Debug("sbx sandbox already stopped",
			zap.String("sandbox_name", info.Name),
			zap.String("status", info.Status))
	}

	if remove {
		zlog.Info("removing sbx sandbox", zap.String("sandbox_name", info.Name))

		rmCmd := exec.Command("sbx", "rm", info.ID)
		rmCmd.Stderr = &stderr

		if err := rmCmd.Run(); err != nil {
			return nil, fmt.Errorf("sbx rm failed: %w (stderr: %s)", err, stderr.String())
		}

		zlog.Info("sbx sandbox removed", zap.String("sandbox_name", info.Name))
	}

	return info, nil
}

// Find returns container info for the given workspace (any state).
func (b *SbxBackend) Find(workspaceDir string) (*ContainerInfo, error) {
	sandbox, err := FindSbxSandbox(workspaceDir)
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
		Workspace: sandbox.Workspace,
		Backend:   BackendSbx,
	}, nil
}

// FindRunning returns container info only if the sbx sandbox is running.
func (b *SbxBackend) FindRunning(workspaceDir string) (*ContainerInfo, error) {
	info, err := b.Find(workspaceDir)
	if err != nil {
		return nil, err
	}

	if info == nil {
		return nil, nil
	}

	if info.Status != "running" {
		zlog.Debug("sbx sandbox found but not running",
			zap.String("sandbox_name", info.Name),
			zap.String("status", info.Status))
		return nil, nil
	}

	return info, nil
}

// List returns all sbx sandboxes managed by this backend.
func (b *SbxBackend) List() ([]ContainerInfo, error) {
	sandboxes, err := ListSbxSandboxes()
	if err != nil {
		return nil, err
	}

	var infos []ContainerInfo
	for _, sb := range sandboxes {
		infos = append(infos, ContainerInfo{
			ID:        sb.ID,
			Name:      sb.Name,
			Status:    sb.Status,
			Workspace: sb.Workspace,
			Backend:   BackendSbx,
		})
	}

	return infos, nil
}

// Remove removes an sbx sandbox by ID.
func (b *SbxBackend) Remove(containerID string) error {
	return RemoveSbxSandbox(containerID)
}

// Cleanup removes all backend-specific resources for a workspace.
func (b *SbxBackend) Cleanup(workspaceDir string) error {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	sboxDir := filepath.Join(absPath, ".sbox")
	if err := os.RemoveAll(sboxDir); err != nil {
		return fmt.Errorf("failed to remove .sbox directory: %w", err)
	}

	zlog.Info(".sbox directory removed", zap.String("path", sboxDir))
	return nil
}

// SaveCache saves the agent state to .sbox/<agent>-cache/ for persistence.
func (b *SbxBackend) SaveCache(workspaceDir string, agentType AgentType) error {
	return SaveAgentCache(workspaceDir, agentType, b)
}
