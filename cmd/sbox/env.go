package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var EnvGroup = Group("env", "Manage environment variables for the sandbox",
	Command(envListE,
		"list",
		"List environment variables for this project",
	),
	Command(envAddE,
		"add <envs...>",
		"Add environment variables (NAME for host passthrough, NAME=VALUE for explicit)",
		MinimumNArgs(1),
		Flags(func(flags *pflag.FlagSet) {
			flags.Bool("global", false, "Add to global config (shared across all projects)")
		}),
	),
	Command(envRemoveE,
		"remove <names...>",
		"Remove environment variables by name",
		MinimumNArgs(1),
		Flags(func(flags *pflag.FlagSet) {
			flags.Bool("global", false, "Remove from global config")
		}),
	),
)

// envListE lists environment variables for the current project
func envListE(cmd *cobra.Command, args []string) error {
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
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

	var sboxEnvs []string
	if sboxFile != nil && sboxFile.Config != nil {
		sboxEnvs = sboxFile.Config.Envs
	}

	_, resolved := sbox.MergeEnvs(config.Envs, projectConfig.Envs, sboxEnvs)

	if len(resolved) == 0 {
		cmd.Println("No environment variables configured.")
		cmd.Println("Use 'sbox env add NAME=VALUE' or 'sbox env add --global NAME' to add one.")
		return nil
	}

	cmd.Println("Environment variables:")
	cmd.Println()
	printResolvedEnvs(cmd, resolved, "  ")

	return nil
}

// envAddE adds environment variables to the current project or global config
func envAddE(cmd *cobra.Command, args []string) error {
	global, _ := cmd.Flags().GetBool("global")

	if global {
		return envAddGlobal(cmd, args)
	}
	return envAddProject(cmd, args)
}

func envAddGlobal(cmd *cobra.Command, args []string) error {
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	envMap := make(map[string]int)
	for i, e := range config.Envs {
		envMap[sbox.EnvName(e)] = i
	}

	for _, arg := range args {
		name := sbox.EnvName(arg)
		if name == "" {
			return fmt.Errorf("invalid environment variable: %q", arg)
		}

		if idx, exists := envMap[name]; exists {
			config.Envs[idx] = arg
			cmd.Printf("Updated '%s' (global)\n", name)
		} else {
			config.Envs = append(config.Envs, arg)
			envMap[name] = len(config.Envs) - 1
			cmd.Printf("Added '%s' (global)\n", name)
		}
	}

	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.Println("Environment changes will take effect on next 'sbox run' (no --recreate needed).")
	return nil
}

func envAddProject(cmd *cobra.Command, args []string) error {
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	envMap := make(map[string]int)
	for i, e := range projectConfig.Envs {
		envMap[sbox.EnvName(e)] = i
	}

	for _, arg := range args {
		name := sbox.EnvName(arg)
		if name == "" {
			return fmt.Errorf("invalid environment variable: %q", arg)
		}

		if idx, exists := envMap[name]; exists {
			projectConfig.Envs[idx] = arg
			cmd.Printf("Updated '%s' (project)\n", name)
		} else {
			projectConfig.Envs = append(projectConfig.Envs, arg)
			envMap[name] = len(projectConfig.Envs) - 1
			cmd.Printf("Added '%s' (project)\n", name)
		}
	}

	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		return fmt.Errorf("failed to save project config: %w", err)
	}

	cmd.Println("Environment changes will take effect on next 'sbox run' (no --recreate needed).")
	return nil
}

// envRemoveE removes environment variables from the current project or global config
func envRemoveE(cmd *cobra.Command, args []string) error {
	global, _ := cmd.Flags().GetBool("global")

	if global {
		return envRemoveGlobal(cmd, args)
	}
	return envRemoveProject(cmd, args)
}

func envRemoveGlobal(cmd *cobra.Command, args []string) error {
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	removeSet := make(map[string]bool)
	for _, name := range args {
		removeSet[name] = true
	}

	var kept []string
	removed := 0
	for _, env := range config.Envs {
		if removeSet[sbox.EnvName(env)] {
			cmd.Printf("Removed '%s' (global)\n", sbox.EnvName(env))
			removed++
		} else {
			kept = append(kept, env)
		}
	}

	if removed == 0 {
		cmd.Println("No matching global environment variables found.")
		return nil
	}

	config.Envs = kept

	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.Println("Environment changes will take effect on next 'sbox run' (no --recreate needed).")
	return nil
}

func envRemoveProject(cmd *cobra.Command, args []string) error {
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	removeSet := make(map[string]bool)
	for _, name := range args {
		removeSet[name] = true
	}

	var kept []string
	removed := 0
	for _, env := range projectConfig.Envs {
		if removeSet[sbox.EnvName(env)] {
			cmd.Printf("Removed '%s' (project)\n", sbox.EnvName(env))
			removed++
		} else {
			kept = append(kept, env)
		}
	}

	if removed == 0 {
		cmd.Println("No matching project environment variables found.")
		return nil
	}

	projectConfig.Envs = kept

	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		return fmt.Errorf("failed to save project config: %w", err)
	}

	cmd.Println("Environment changes will take effect on next 'sbox run' (no --recreate needed).")
	return nil
}

// printResolvedEnvs prints resolved environment variables with source tags and host resolution hints.
// The prefix is prepended to each line for indentation.
func printResolvedEnvs(cmd *cobra.Command, resolved []sbox.ResolvedEnv, prefix string) {
	hasPassthrough := false
	hasUnset := false

	for _, r := range resolved {
		name := sbox.EnvName(r.Spec)
		sourceTag := fmt.Sprintf("  [%s]", r.Source)

		if strings.Contains(r.Spec, "=") {
			value := r.Spec[len(name)+1:]
			cmd.Printf("%s%s=%s%s\n", prefix, name, value, sourceTag)
		} else {
			hostValue, found := os.LookupEnv(name)
			if found {
				cmd.Printf("%s%s=%s  (from host*)%s\n", prefix, name, hostValue, sourceTag)
				hasPassthrough = true
			} else {
				cmd.Printf("%s%s  (not set on host, will be empty in sandbox)%s\n", prefix, name, sourceTag)
				hasUnset = true
			}
		}
	}

	if hasPassthrough || hasUnset {
		cmd.Println()
	}
	if hasPassthrough {
		cmd.Printf("%s* Value resolved from current host environment; may differ at 'sbox run' time.\n", prefix)
	}
	if hasUnset {
		cmd.Printf("%sHint: set missing variables on your host or use 'sbox env add NAME=VALUE' to set an explicit value.\n", prefix)
	}
}
