package sbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"go.uber.org/zap"
)

// HostBackend implements the Backend interface by running the agent directly on
// the host machine with no Docker or MicroVM isolation.
type HostBackend struct {
	config *Config
}

// NewHostBackend creates a new host backend instance.
func NewHostBackend(config *Config) *HostBackend {
	return &HostBackend{config: config}
}

// Name returns the backend type name.
func (b *HostBackend) Name() BackendType {
	return BackendHost
}

// Run starts the agent directly on the host. It prepares the .sbox/ directory
// (for CLAUDE.md, env, etc.) then execs the agent binary found in the host PATH.
// Loop mode and prompt mode are handled in-process, mirroring the entrypoint logic.
func (b *HostBackend) Run(opts BackendOptions) error {
	agentType := AgentType(opts.ProjectConfig.Agent)
	if agentType == "" {
		agentType = DefaultAgent
	}

	zlog.Info("running agent on host (no Docker)",
		zap.String("workspace", opts.WorkspaceDir),
		zap.String("agent", string(agentType)))

	// Prepare .sbox/ directory — CLAUDE.md, env, entrypoint config, etc.
	var sboxFileEnvs []string
	if opts.SboxFile != nil && opts.SboxFile.Config != nil {
		sboxFileEnvs = opts.SboxFile.Config.Envs
	}
	if err := PrepareSboxDirectory(opts.WorkspaceDir, opts.Config, opts.Config.Envs, opts.ProjectConfig.Envs, sboxFileEnvs, BackendHost, agentType, opts); err != nil {
		return fmt.Errorf("failed to prepare .sbox directory: %w", err)
	}

	// Load and apply env vars from .sbox/env into the current process so they
	// are inherited by the agent (same as entrypoint does inside sandbox).
	if err := hostLoadEnv(opts.WorkspaceDir); err != nil {
		zlog.Warn("failed to load .sbox/env", zap.Error(err))
		// Non-fatal — continue with current environment
	}

	spec := GetAgentSpec(agentType)

	// Collect plugin directories for --plugin-dir flags
	pluginDirs := hostCollectPluginDirs(opts.WorkspaceDir, agentType)

	// Extra args forwarded from `sbox run -- ...`
	extraArgs := opts.AgentArgs

	// Loop mode: run the agent in a loop until goal is confirmed.
	if opts.LoopMode && opts.Prompt != "" {
		zlog.Info("entering host loop mode",
			zap.String("prompt_length_chars", fmt.Sprintf("%d", len(opts.Prompt))),
			zap.Int("max_iterations", opts.MaxIterations))

		cfg := &EntrypointConfig{
			Prompt:            opts.Prompt,
			LoopMode:          true,
			MaxIterations:     opts.MaxIterations,
			LoopConfirmations: opts.LoopConfirmations,
			AgentArgs:         extraArgs,
		}
		return runLoop(cfg, agentType, extraArgs, pluginDirs, opts.WorkspaceDir)
	}

	// Single prompt mode: run agent once with stream transformer.
	if opts.Prompt != "" {
		zlog.Info("running host agent in single-prompt mode", zap.Int("prompt_len", len(opts.Prompt)))
		args := append(spec.PromptArgs(), extraArgs...)
		args = append(args, opts.Prompt)
		return runAgentWithStreamTransformer(agentType, args, pluginDirs)
	}

	// Interactive mode: run agent as child process with signal forwarding.
	return hostRunAgent(agentType, extraArgs, pluginDirs, opts.WorkspaceDir)
}

// Shell is not supported for the host backend.
func (b *HostBackend) Shell(workspaceDir string) error {
	return fmt.Errorf("'sbox shell' is not supported for the host backend")
}

// Stop is not supported for the host backend.
func (b *HostBackend) Stop(workspaceDir string, remove bool) (*ContainerInfo, error) {
	return nil, fmt.Errorf("'sbox stop' is not supported for the host backend")
}

// Find is not supported for the host backend — there is no container to find.
func (b *HostBackend) Find(workspaceDir string) (*ContainerInfo, error) {
	return nil, nil
}

// FindRunning is not supported for the host backend.
func (b *HostBackend) FindRunning(workspaceDir string) (*ContainerInfo, error) {
	return nil, nil
}

// List returns an empty slice — there are no containers managed by this backend.
func (b *HostBackend) List() ([]ContainerInfo, error) {
	return nil, nil
}

// Remove is a no-op for the host backend.
func (b *HostBackend) Remove(containerID string) error {
	return fmt.Errorf("'sbox remove' is not supported for the host backend")
}

// Cleanup removes the .sbox/ directory for this workspace.
func (b *HostBackend) Cleanup(workspaceDir string) error {
	sboxDir := filepath.Join(workspaceDir, ".sbox")
	if err := os.RemoveAll(sboxDir); err != nil {
		return fmt.Errorf("failed to remove .sbox directory: %w", err)
	}
	return nil
}

// SaveCache is a no-op for the host backend — agent config lives on the host natively.
func (b *HostBackend) SaveCache(workspaceDir string, agentType AgentType) error {
	return nil
}

// hostLoadEnv reads .sbox/env and sets the key=value pairs in the current process.
// This mirrors the entrypoint's loadEntrypointEnv but skips the persistent-file write
// since we are already on the host (no sandbox persistent env file).
func hostLoadEnv(workspaceDir string) error {
	envs, err := ReadEntrypointEnv(workspaceDir)
	if err != nil {
		return err
	}
	for _, env := range envs {
		before, after, found := strings.Cut(env, "=")
		if !found {
			continue
		}
		os.Setenv(before, after)
		zlog.Debug("host: loaded env var", zap.String("key", before))
	}
	return nil
}

// hostCollectPluginDirs returns the list of plugin directories under .sbox/plugins/
// that should be passed to the agent via --plugin-dir flags.
func hostCollectPluginDirs(workspaceDir string, agentType AgentType) []string {
	// Read the entrypoint config that was just written by PrepareSboxDirectory.
	epConfig, err := ReadEntrypointConfig(workspaceDir)
	if err != nil {
		zlog.Debug("host: could not read entrypoint config for plugins", zap.Error(err))
		return nil
	}

	var dirs []string
	for _, plugin := range epConfig.Plugins {
		dir := filepath.Join(workspaceDir, ".sbox", plugin.Path)
		if _, err := os.Stat(dir); err == nil {
			dirs = append(dirs, dir)
			zlog.Debug("host: adding plugin dir", zap.String("name", plugin.Name), zap.String("path", dir))
		}
	}
	return dirs
}

// hostRunAgent runs the agent as a child process on the host with signal forwarding.
// This is the interactive mode equivalent for the host backend.
func hostRunAgent(agentType AgentType, extraArgs []string, pluginDirs []string, workspaceDir string) error {
	spec := GetAgentSpec(agentType)

	binaryPath, err := spec.FindBinary()
	if err != nil {
		return fmt.Errorf("%s not found: %w — make sure it is installed and available in PATH", spec.BinaryName(), err)
	}

	argv := spec.ExecArgs(pluginDirs)
	argv = append(argv, extraArgs...)

	zlog.Info("host: starting agent",
		zap.String("agent", string(agentType)),
		zap.String("path", binaryPath),
		zap.Strings("args", argv))

	cmd := exec.Command(binaryPath, argv[1:]...) // argv[0] is the binary name
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	// Forward signals to the child process
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			_ = cmd.Process.Signal(sig)
		}
	}()

	err = cmd.Wait()
	signal.Stop(sigCh)
	close(sigCh)

	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}
