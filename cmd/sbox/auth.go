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

var AuthCommand = Command(authE,
	"auth",
	"Configure API key for Claude (shared across all sandboxes)",
	Description(`
		Configures the Anthropic API key used by all sandbox sessions.

		Without flags, prompts for the API key and stores it in the
		global configuration as an environment variable passed to all sandboxes.

		With --status, shows whether an API key is configured.
		With --logout, removes the stored API key.
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("status", false, "Show authentication status")
		flags.Bool("logout", false, "Remove stored API key")
	}),
)

// authE handles authentication management
func authE(cmd *cobra.Command, args []string) error {
	showStatus, _ := cmd.Flags().GetBool("status")
	logout, _ := cmd.Flags().GetBool("logout")

	if showStatus {
		return authStatus(cmd)
	}

	if logout {
		return authLogout(cmd)
	}

	return authLogin(cmd)
}

// authStatus shows the current authentication status
func authStatus(cmd *cobra.Command) error {
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	for _, env := range config.Envs {
		if sbox.EnvName(env) == "ANTHROPIC_API_KEY" {
			if strings.Contains(env, "=") {
				cmd.Println("Status: Configured")
				cmd.Println("ANTHROPIC_API_KEY is set in global config and will be passed to all sandboxes.")
			} else {
				cmd.Println("Status: Configured (passthrough from host)")
				cmd.Println("ANTHROPIC_API_KEY will be resolved from host environment at launch time.")
			}
			return nil
		}
	}

	cmd.Println("Status: Not configured")
	cmd.Println("Run 'sbox auth' to configure your API key.")
	return nil
}

// authLogout removes the stored API key
func authLogout(cmd *cobra.Command) error {
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	found := false
	var remaining []string
	for _, env := range config.Envs {
		if sbox.EnvName(env) == "ANTHROPIC_API_KEY" {
			found = true
		} else {
			remaining = append(remaining, env)
		}
	}

	if !found {
		cmd.Println("No API key configured.")
		return nil
	}

	config.Envs = remaining
	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Clean up legacy credentials file if it exists
	credentialsPath := sbox.GetCredentialsPath(config)
	if err := os.Remove(credentialsPath); err != nil && !os.IsNotExist(err) {
		cmd.Printf("Warning: failed to remove legacy credentials file: %v\n", err)
	}
	if err := sbox.RemoveAuthToken(); err != nil && !os.IsNotExist(err) {
		cmd.Printf("Warning: failed to remove legacy token file: %v\n", err)
	}

	cmd.Println("API key removed from global config.")
	return nil
}

// authLogin configures the API key
func authLogin(cmd *cobra.Command) error {
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if already configured
	for _, env := range config.Envs {
		if sbox.EnvName(env) == "ANTHROPIC_API_KEY" {
			cmd.Println("API key is already configured. Use 'sbox auth --logout' first to reconfigure.")
			return nil
		}
	}

	cmd.Println("Enter your Anthropic API key (starts with sk-ant-):")
	cmd.Print("> ")

	var apiKey string
	if _, err := fmt.Scanln(&apiKey); err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	// Store as global env
	config.Envs = append(config.Envs, "ANTHROPIC_API_KEY="+apiKey)
	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.Println()
	cmd.Println("API key configured successfully.")
	cmd.Println("ANTHROPIC_API_KEY will be passed to all sandbox sessions.")
	return nil
}
