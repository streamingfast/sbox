package sbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// hostPIDFile is the name of the file written inside .sbox/ that holds the
// PID of a running host-backend agent process.
const hostPIDFile = "host.pid"

// hostPIDFilePath returns the path to the PID file for a given workspace.
func hostPIDFilePath(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".sbox", hostPIDFile)
}

// writeHostPID writes the given PID to .sbox/host.pid.
func writeHostPID(workspaceDir string, pid int) error {
	path := hostPIDFilePath(workspaceDir)
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0644)
}

// readHostPID reads the PID from .sbox/host.pid.
// Returns (0, nil) if the file does not exist.
func readHostPID(workspaceDir string) (int, error) {
	path := hostPIDFilePath(workspaceDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read host PID file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse host PID file %q: %w", strings.TrimSpace(string(data)), err)
	}
	return pid, nil
}

// removeHostPID removes .sbox/host.pid, ignoring "not found" errors.
func removeHostPID(workspaceDir string) {
	path := hostPIDFilePath(workspaceDir)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		zlog.Warn("failed to remove host PID file", zap.String("path", path), zap.Error(err))
	}
}

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

	// Extra args forwarded from `sbox run -- ...`
	extraArgs := opts.AgentArgs

	// On the host backend, plugins are already installed natively in the agent's
	// own config directory (e.g. ~/.claude/plugins). No --plugin-dir forwarding
	// is needed or desired — the agent discovers them on its own.

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
		return runLoop(cfg, agentType, extraArgs, nil, opts.WorkspaceDir)
	}

	// Single prompt mode: run agent once with stream transformer.
	if opts.Prompt != "" {
		zlog.Info("running host agent in single-prompt mode", zap.Int("prompt_len", len(opts.Prompt)))
		args := append(spec.PromptArgs(), extraArgs...)
		args = append(args, opts.Prompt)
		return runAgentWithStreamTransformer(agentType, args, nil)
	}

	// Interactive mode: run agent as child process with signal forwarding.
	return hostRunAgent(agentType, extraArgs, nil, opts.WorkspaceDir)
}

// Shell is not supported for the host backend.
func (b *HostBackend) Shell(workspaceDir string) error {
	return fmt.Errorf("'sbox shell' is not supported for the host backend")
}

// Stop sends SIGTERM to the host agent process recorded in .sbox/host.pid, then
// waits up to 5 seconds before escalating to SIGKILL. The PID file is removed
// after the process is gone. When remove is true the .sbox/ directory is also
// cleaned up via Cleanup.
func (b *HostBackend) Stop(workspaceDir string, remove bool) (*ContainerInfo, error) {
	pid, err := readHostPID(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("stop host agent: %w", err)
	}
	if pid == 0 {
		// No PID file — nothing to stop.
		return nil, nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		// On Unix, FindProcess never fails; the error path is here for completeness.
		removeHostPID(workspaceDir)
		return nil, fmt.Errorf("find host agent process (pid=%d): %w", pid, err)
	}

	// Check if the process is actually alive before sending signals.
	if signalErr := proc.Signal(syscall.Signal(0)); signalErr != nil {
		// Process no longer exists — clean up stale PID file.
		zlog.Debug("host agent process not found, removing stale PID file",
			zap.Int("pid", pid), zap.Error(signalErr))
		removeHostPID(workspaceDir)
		return nil, nil
	}

	zlog.Info("sending SIGTERM to host agent", zap.Int("pid", pid))
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		removeHostPID(workspaceDir)
		return nil, fmt.Errorf("send SIGTERM to host agent (pid=%d): %w", pid, err)
	}

	// Poll for up to 5 seconds, then escalate to SIGKILL.
	const pollInterval = 100 * time.Millisecond
	const killTimeout = 5 * time.Second
	deadline := time.Now().Add(killTimeout)

	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		if signalErr := proc.Signal(syscall.Signal(0)); signalErr != nil {
			// Process is gone.
			break
		}
	}

	// If process is still alive after timeout, escalate.
	if signalErr := proc.Signal(syscall.Signal(0)); signalErr == nil {
		zlog.Warn("host agent did not exit after SIGTERM, sending SIGKILL", zap.Int("pid", pid))
		_ = proc.Signal(syscall.SIGKILL)
		// Brief final wait.
		time.Sleep(500 * time.Millisecond)
	}

	removeHostPID(workspaceDir)

	info := &ContainerInfo{
		ID:   strconv.Itoa(pid),
		Name: fmt.Sprintf("host-agent (pid %d)", pid),
	}
	return info, nil
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

	// Write PID file so `sbox stop` can find this process.
	if pidErr := writeHostPID(workspaceDir, cmd.Process.Pid); pidErr != nil {
		zlog.Warn("failed to write host PID file (sbox stop may not work)",
			zap.Int("pid", cmd.Process.Pid), zap.Error(pidErr))
	} else {
		zlog.Debug("wrote host PID file", zap.Int("pid", cmd.Process.Pid))
	}
	defer removeHostPID(workspaceDir)

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
