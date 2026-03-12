package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

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

	runErr := backend.Run(opts)

	// In loop mode the sandbox should not keep running after the loop ends
	// (whether by completion, error, or Ctrl+C). Stop it so it doesn't
	// continue consuming resources in the background.
	ui.Status("Stopping sandbox after loop exit...")
	if _, err := backend.Stop(workspaceDir, false); err != nil {
		zlog.Warn("failed to stop sandbox after loop", zap.Error(err))
	}

	return runErr
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
