package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
	"go.uber.org/zap"
	"golang.org/x/term"
)

var RunCommand = Command(runE,
	"run [prompt]",
	"Launch Claude in Docker sandbox, container, or directly on the host with configured mounts and profiles",
	Description(`
		Launches an AI agent running Claude Code with:
		- Shared ~/.claude/agents and ~/.claude/plugins directories
		- Concatenated CLAUDE.md/AGENTS.md hierarchy from parent directories
		- Persistent credentials across sessions
		- Optional Docker socket access
		- Custom profiles for additional tool installations (Go, Rust, etc.)

		Backend types:
		- sandbox (default): Uses Docker sandbox MicroVM for enhanced isolation
		- container: Uses standard Docker container with named volume persistence
		- host: Runs the agent directly on the host machine (no Docker, no isolation)

		Agent types:
		- claude (default): Uses Claude Code AI agent
		- opencode: Uses OpenCode AI agent

		An optional prompt can be provided as a positional argument. The agent
		launches interactively with the prompt pre-seeded:

		  sbox run "implement the feature described in TASK.md"

		Extra arguments after -- are forwarded verbatim to the agent:

		  sbox run -- --resume <session-id>

		An optional prompt can be provided as a positional argument. The agent
		launches interactively with the prompt pre-seeded:

		  sbox run "implement the feature described in TASK.md"

		When --watch is specified, after the interactive session exits the command
		stays alive and watches for changes to files matching the given regex patterns.
		Any matching file change triggers a new session. Watches are inactive while
		the agent is running.

		Both prompt and watch can be combined:

		  sbox run "my prompt" -- --resume <session-id>
	`),
	ArbitraryArgs(),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("docker-socket", false, "Mount Docker socket into sandbox/container")
		flags.StringSlice("profile", nil, "Additional profiles to use for this session")
		flags.Bool("recreate", false, "Force rebuild of custom template image and recreate sandbox/container (pulls latest base image)")
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
		flags.Bool("debug", false, "Enable debug mode for docker commands")
		flags.String("backend", "", "Backend type: 'sandbox' (default) or 'container'")
		flags.String("agent", "", "Agent type: 'claude' (default) or 'opencode'")
		flags.Duration("startup-delay", -1, "Delay agent startup inside the sandbox (0 = wait forever, e.g. 30s, 5m)")
		flags.StringArray("watch", nil, "Watch files matching regex pattern and relaunch on change (can be specified multiple times)")
	}),
)

// runE launches the Docker sandbox with configured settings
func runE(cmd *cobra.Command, args []string) error {
	zlog.Debug("starting sbox run command")

	// Extract optional interactive prompt from first positional arg.
	// args[0] is the prompt if it doesn't start with '-'; remaining args are
	// forwarded verbatim to the agent (same as args after -- in the old model).
	var interactivePrompt string
	var agentExtraArgs []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		interactivePrompt = strings.TrimSpace(args[0])
		agentExtraArgs = args[1:]
	} else {
		agentExtraArgs = args
	}

	// If no prompt was provided via argument, check if stdin is piped and read it as the prompt.
	if interactivePrompt == "" && !term.IsTerminal(int(os.Stdin.Fd())) {
		stdinBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		if trimmed := strings.TrimSpace(string(stdinBytes)); trimmed != "" {
			interactivePrompt = trimmed
			zlog.Debug("using stdin as prompt", zap.Int("length", len(interactivePrompt)))
		}
	}

	// Get workspace directory (default to current directory)
	workspaceDir, err := cmd.Flags().GetString("workspace")
	if err != nil {
		return fmt.Errorf("failed to get workspace flag: %w", err)
	}
	if workspaceDir == "" {
		workspaceDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Load global configuration
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load project configuration
	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	// Find and merge sbox.yaml file configuration
	sboxFile, err := sbox.FindSboxFile(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load sbox.yaml file: %w", err)
	}
	projectConfig, err = sbox.MergeProjectConfig(projectConfig, sboxFile)
	if err != nil {
		return fmt.Errorf("failed to merge sbox.yaml config: %w", err)
	}

	// Get flags
	dockerSocket, err := cmd.Flags().GetBool("docker-socket")
	if err != nil {
		return fmt.Errorf("failed to get docker-socket flag: %w", err)
	}

	profiles, err := cmd.Flags().GetStringSlice("profile")
	if err != nil {
		return fmt.Errorf("failed to get profile flag: %w", err)
	}

	recreate, err := cmd.Flags().GetBool("recreate")
	if err != nil {
		return fmt.Errorf("failed to get recreate flag: %w", err)
	}

	debug, err := cmd.Flags().GetBool("debug")
	if err != nil {
		return fmt.Errorf("failed to get debug flag: %w", err)
	}

	backendFlag, err := cmd.Flags().GetString("backend")
	if err != nil {
		return fmt.Errorf("failed to get backend flag: %w", err)
	}

	// Validate backend flag if provided
	if backendFlag != "" {
		if err := sbox.ValidateBackend(backendFlag); err != nil {
			return err
		}
	}

	agentFlag, err := cmd.Flags().GetString("agent")
	if err != nil {
		return fmt.Errorf("failed to get agent flag: %w", err)
	}

	// Validate agent flag if provided
	if agentFlag != "" {
		if err := sbox.ValidateAgent(agentFlag); err != nil {
			return err
		}
	}

	startupDelay, err := cmd.Flags().GetDuration("startup-delay")
	if err != nil {
		return fmt.Errorf("failed to get startup-delay flag: %w", err)
	}

	watchPatternStrs, _ := cmd.Flags().GetStringArray("watch")
	watchPatterns, err := compileWatchPatterns(watchPatternStrs)
	if err != nil {
		return err
	}

	// Resolve which backend to use (CLI > sbox.yaml > project > global > default)
	backendType := sbox.ResolveBackendType(backendFlag, sboxFile, projectConfig, config)
	zlog.Debug("resolved backend type", zap.String("backend", string(backendType)))

	// Warn when --profile is used with the host backend (profiles install tools inside
	// Docker images; they have no effect when running directly on the host).
	if backendType == sbox.BackendHost && len(profiles) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: --profile is not supported for the host backend and will be ignored\n")
	}

	// Resolve which agent to use (CLI > sbox.yaml > project > global > default)
	agentType := sbox.ResolveAgentType(agentFlag, sboxFile, projectConfig, config)
	zlog.Debug("resolved agent type", zap.String("agent", string(agentType)))

	// Persist the resolved backend and agent to project config so shell/stop/info can find it
	projectConfig.Backend = string(backendType)
	projectConfig.Agent = string(agentType)

	// Get the backend implementation
	backend, err := sbox.GetBackend(string(backendType), config)
	if err != nil {
		return fmt.Errorf("failed to get backend: %w", err)
	}

	// Generate sandbox name first (needed for lookup)
	// Regenerate if empty OR if the agent has changed (detected by checking if the name contains the current agent)
	needsRegeneration := projectConfig.SandboxName == ""
	if !needsRegeneration && projectConfig.SandboxName != "" {
		// Check if the sandbox name matches the current agent
		expectedPrefix := "sbox-" + string(agentType) + "-"
		if !strings.HasPrefix(projectConfig.SandboxName, expectedPrefix) {
			zlog.Info("agent changed, regenerating sandbox name",
				zap.String("old_name", projectConfig.SandboxName),
				zap.String("agent", string(agentType)))
			needsRegeneration = true
		}
	}

	if needsRegeneration {
		sandboxName, err := sbox.GenerateSandboxName(workspaceDir, agentType)
		if err != nil {
			return fmt.Errorf("failed to generate sandbox name: %w", err)
		}
		projectConfig.SandboxName = sandboxName
		zlog.Debug("generated sandbox name", zap.String("name", sandboxName))
	}

	if recreate {
		// Remove existing container/sandbox so a fresh one is created
		// Use backend's Find() method to check for existing instance
		existing, err := backend.Find(workspaceDir)
		if err != nil {
			zlog.Debug("failed to check for existing container/sandbox", zap.Error(err))
		}

		if existing != nil {
			// Save agent cache before removing (for persistence across recreate)
			if existing.Status == "running" {
				if err := backend.SaveCache(workspaceDir, agentType); err != nil {
					zlog.Warn("failed to save cache", zap.Error(err))
					// Non-fatal - continue with recreate
				}
			}

			ui := sbox.DefaultUI
			ui.Status("Removing existing %s '%s' (%s)", backendType, existing.Name, existing.ID)
			if err := backend.Remove(existing.ID); err != nil {
				return fmt.Errorf("failed to remove existing %s: %w", backendType, err)
			}
			ui.Status("Existing %s removed", backendType)
		}
	}

	// Save project config to register this project (for sbox info)
	// This must happen BEFORE running the sandbox so other terminals can see it
	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		zlog.Warn("failed to save project config", zap.Error(err))
		// Non-fatal: continue running sandbox even if we can't save config
	}

	// Build backend options
	opts := sbox.BackendOptions{
		WorkspaceDir:      workspaceDir,
		MountDockerSocket: dockerSocket,
		Profiles:          profiles,
		ForceRebuild:      recreate,
		Debug:             debug,
		Config:            config,
		ProjectConfig:     projectConfig,
		SboxFile:          sboxFile,
		AgentArgs:         agentExtraArgs,
		InteractivePrompt: interactivePrompt,
	}

	// -1 is the default (unset), any other value means the flag was provided
	if startupDelay >= 0 {
		opts.StartupDelay = &startupDelay
	}

	// Run using the selected backend
	sbox.DefaultUI.Label("Backend", string(backend.Name()))
	if interactivePrompt != "" {
		sbox.DefaultUI.Label("Prompt", interactivePrompt)
	}
	if len(watchPatternStrs) > 0 {
		sbox.DefaultUI.Label("Watching", strings.Join(watchPatternStrs, ", "))
	}

	// Setup signal handling for graceful shutdown in watch mode.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if len(watchPatterns) > 0 {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			select {
			case <-sigCh:
				cancel()
			case <-ctx.Done():
			}
			signal.Stop(sigCh)
		}()
	}

	runErr := backend.Run(opts)

	// No watch patterns — original single-run behavior.
	if len(watchPatterns) == 0 {
		return runErr
	}

	// Watch mode: after each session, wait for a watched file to change then
	// relaunch. Exit on error, context cancellation, or signal.
	for {
		if runErr != nil {
			return runErr
		}
		if ctx.Err() != nil {
			return nil
		}

		sbox.DefaultUI.Status("Watching for file changes... (Ctrl+C to exit)")

		changedFile, watchErr := watchForChanges(ctx, workspaceDir, watchPatterns)
		if watchErr != nil {
			// Context cancelled by signal — clean exit.
			return nil
		}

		sbox.DefaultUI.Status("File changed: %s — relaunching...", changedFile)
		runErr = backend.Run(opts)
	}
}
