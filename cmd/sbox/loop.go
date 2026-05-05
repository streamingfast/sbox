package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
	"go.uber.org/zap"
)

var LoopCommand = Command(loopE,
	"loop [prompt]",
	"Run an agent in a loop until the goal described by the prompt is completed",
	Description(`
		Runs the agent repeatedly with the given prompt until the goal is completed.
		Works like 'sbox run' (same --backend and --agent support) but operates
		in a non-interactive loop mode.

		The prompt can be provided as:
		- An argument: sbox loop "fix the tests"
		- Via stdin:    echo "fix the tests" | sbox loop
		- Interactively: sbox loop (prompts you to enter the goal)

		The agent receives the prompt augmented with loop instructions. It must
		assess whether the goal is already reached. If not, it works toward the goal.
		When the goal is completed, the agent writes a .sbox/loop.completion file.

		After each agent run, the completion file is checked:
		- If present with content: the agent believes the goal is completed
		- If absent or empty: the agent is still working

		The loop stops only after the agent confirms completion twice in a row,
		ensuring the goal is truly achieved.

		When --watch is specified, after the loop completes the command stays alive
		and watches for changes to files matching the given regex patterns. Any
		matching file change triggers a new loop run with the same prompt. Watches
		are inactive while the loop is running.
	`),
	MaximumNArgs(1),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("docker-socket", false, "Mount Docker socket into sandbox/container")
		flags.StringSlice("profile", nil, "Additional profiles to use for this session")
		flags.Bool("recreate", false, "Force rebuild of custom template image and recreate sandbox/container (pulls latest base image)")
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
		flags.Bool("debug", false, "Enable debug mode for docker commands")
		flags.String("backend", "", "Backend type: 'sandbox' (default) or 'container'")
		flags.String("agent", "", "Agent type: 'claude' (default) or 'opencode'")
		flags.Int("max-iterations", 0, "Maximum number of loop iterations (0 = unlimited)")
		flags.Int("confirmations", 0, "Number of consecutive goal completions required (default: 2, override via sbox.yaml or global config)")
		flags.StringArray("watch", nil, "Watch files matching regex pattern and relaunch loop on change (can be specified multiple times)")
	}),
)

func loopE(cmd *cobra.Command, args []string) error {
	userPrompt, err := resolveLoopPrompt(args)
	if err != nil {
		return err
	}

	zlog.Debug("starting sbox loop command", zap.String("prompt", userPrompt))

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

	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	sboxFile, err := sbox.FindSboxFile(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load sbox.yaml file: %w", err)
	}
	projectConfig, err = sbox.MergeProjectConfig(projectConfig, sboxFile)
	if err != nil {
		return fmt.Errorf("failed to merge sbox.yaml config: %w", err)
	}

	dockerSocket, _ := cmd.Flags().GetBool("docker-socket")
	profiles, _ := cmd.Flags().GetStringSlice("profile")
	recreate, _ := cmd.Flags().GetBool("recreate")
	debug, _ := cmd.Flags().GetBool("debug")
	backendFlag, _ := cmd.Flags().GetString("backend")
	agentFlag, _ := cmd.Flags().GetString("agent")
	maxIterations, _ := cmd.Flags().GetInt("max-iterations")
	confirmationsFlag, _ := cmd.Flags().GetInt("confirmations")
	watchPatternStrs, _ := cmd.Flags().GetStringArray("watch")

	// Compile watch patterns
	watchPatterns, err := compileWatchPatterns(watchPatternStrs)
	if err != nil {
		return err
	}

	if backendFlag != "" {
		if err := sbox.ValidateBackend(backendFlag); err != nil {
			return err
		}
	}
	if agentFlag != "" {
		if err := sbox.ValidateAgent(agentFlag); err != nil {
			return err
		}
	}

	backendType := sbox.ResolveBackendType(backendFlag, sboxFile, projectConfig, config)
	agentType := sbox.ResolveAgentType(agentFlag, sboxFile, projectConfig, config)

	// Warn when --profile is used with the host backend (not applicable).
	if backendType == sbox.BackendHost && len(profiles) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: --profile is not supported for the host backend and will be ignored\n")
	}

	projectConfig.Backend = string(backendType)
	projectConfig.Agent = string(agentType)

	backend, err := sbox.GetBackend(string(backendType), config)
	if err != nil {
		return fmt.Errorf("failed to get backend: %w", err)
	}

	// Generate sandbox name (same logic as run command)
	needsRegeneration := projectConfig.SandboxName == ""
	if !needsRegeneration && projectConfig.SandboxName != "" {
		expectedPrefix := "sbox-" + string(agentType) + "-"
		if !strings.HasPrefix(projectConfig.SandboxName, expectedPrefix) {
			needsRegeneration = true
		}
	}
	if needsRegeneration {
		sandboxName, err := sbox.GenerateSandboxName(workspaceDir, agentType)
		if err != nil {
			return fmt.Errorf("failed to generate sandbox name: %w", err)
		}
		projectConfig.SandboxName = sandboxName
	}

	if recreate {
		existing, err := backend.Find(workspaceDir)
		if err != nil {
			zlog.Debug("failed to check for existing container/sandbox", zap.Error(err))
		}
		if existing != nil {
			if existing.Status == "running" {
				if err := backend.SaveCache(workspaceDir, agentType); err != nil {
					zlog.Warn("failed to save cache", zap.Error(err))
				}
			}
			sbox.DefaultUI.Status("Removing existing %s '%s' (%s)", backendType, existing.Name, existing.ID)
			if err := backend.Remove(existing.ID); err != nil {
				return fmt.Errorf("failed to remove existing %s: %w", backendType, err)
			}
		}
	}

	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		zlog.Warn("failed to save project config", zap.Error(err))
	}

	// Resolve loop confirmations: CLI flag > sbox.yaml > global config > default (2)
	loopConfirmations := sbox.ResolveLoopConfirmations(confirmationsFlag, sboxFile, config)

	ui := sbox.DefaultUI
	ui.Label("Backend", string(backend.Name()))
	ui.Label("Goal", userPrompt)
	if maxIterations > 0 {
		ui.Label("Max iterations", fmt.Sprintf("%d", maxIterations))
	}
	if loopConfirmations != 2 {
		ui.Label("Confirmations", fmt.Sprintf("%d", loopConfirmations))
	}
	if len(watchPatternStrs) > 0 {
		ui.Label("Watching", strings.Join(watchPatternStrs, ", "))
	}

	// The entrypoint handles all loop iterations internally — sandbox stays warm.
	opts := sbox.BackendOptions{
		WorkspaceDir:      workspaceDir,
		MountDockerSocket: dockerSocket,
		Profiles:          profiles,
		ForceRebuild:      recreate,
		Debug:             debug,
		Config:            config,
		ProjectConfig:     projectConfig,
		SboxFile:          sboxFile,
		Prompt:            userPrompt,
		LoopMode:          true,
		MaxIterations:     maxIterations,
		LoopConfirmations: loopConfirmations,
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

	stopSandbox := func() {
		if backendType != sbox.BackendHost {
			ui.Status("Stopping sandbox after loop exit...")
			if _, err := backend.Stop(workspaceDir, false); err != nil {
				zlog.Warn("failed to stop sandbox after loop", zap.Error(err))
			}
		}
	}

	runErr := backend.Run(opts)

	// No watch patterns — original single-run behavior.
	if len(watchPatterns) == 0 {
		stopSandbox()
		return runErr
	}

	// Watch mode: after each completed loop, wait for a watched file to change
	// then relaunch. Exit on error, context cancellation, or signal.
	for {
		if runErr != nil {
			stopSandbox()
			return runErr
		}
		if ctx.Err() != nil {
			stopSandbox()
			return nil
		}

		ui.Status("Watching for file changes... (Ctrl+C to exit)")

		changedFile, watchErr := watchForChanges(ctx, workspaceDir, watchPatterns)
		if watchErr != nil {
			// Context cancelled by signal — clean exit.
			stopSandbox()
			return nil
		}

		ui.Status("File changed: %s — relaunching loop...", changedFile)
		runErr = backend.Run(opts)
	}
}

// compileWatchPatterns compiles the given regex pattern strings into *regexp.Regexp values.
func compileWatchPatterns(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pat := range patterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("invalid --watch pattern %q: %w", pat, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// watchForChanges watches the workspace directory tree for write/create events on
// files whose workspace-relative path matches any of the given patterns. It returns
// the relative path of the first matching changed file, or a non-nil error if ctx
// is cancelled.
func watchForChanges(ctx context.Context, workspaceDir string, patterns []*regexp.Regexp) (string, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return "", fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer watcher.Close()

	skipDirs := map[string]bool{
		".git":         true,
		".sbox":        true,
		"node_modules": true,
		"vendor":       true,
	}

	// Walk and watch all non-skipped directories inside the workspace.
	if walkErr := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if path != workspaceDir && skipDirs[base] {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	}); walkErr != nil {
		return "", fmt.Errorf("failed to set up workspace watch: %w", walkErr)
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return "", fmt.Errorf("file watcher closed unexpectedly")
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			rel, relErr := filepath.Rel(workspaceDir, event.Name)
			if relErr != nil {
				continue
			}
			for _, pat := range patterns {
				if pat.MatchString(rel) {
					return rel, nil
				}
			}

		case watchErr := <-watcher.Errors:
			zlog.Warn("file watcher error", zap.Error(watchErr))
		}
	}
}

// resolveLoopPrompt gets the prompt from args, stdin, or interactively.
func resolveLoopPrompt(args []string) (string, error) {
	// 1. From argument
	if len(args) > 0 {
		prompt := strings.TrimSpace(args[0])
		if prompt != "" {
			return prompt, nil
		}
	}

	// 2. From stdin (piped)
	stat, _ := os.Stdin.Stat()
	if stat.Mode()&os.ModeCharDevice == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read from stdin: %w", err)
		}
		prompt := strings.TrimSpace(string(data))
		if prompt != "" {
			return prompt, nil
		}
	}

	// 3. Interactive prompt
	fmt.Print("Enter your goal: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		prompt := strings.TrimSpace(scanner.Text())
		if prompt != "" {
			return prompt, nil
		}
	}

	return "", fmt.Errorf("no prompt provided")
}
