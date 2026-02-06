package main

import (
	"fmt"

	"github.com/spf13/cobra"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var ConfigCommand = Command(configE,
	"config [key] [value]",
	"View or edit configuration settings",
	Description(`
		Without arguments, displays the current configuration.
		With a key, displays that setting's value.
		With key and value, sets the configuration option.
	`),
)

// configE views or edits configuration
func configE(cmd *cobra.Command, args []string) error {
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(args) == 0 {
		// Show all configuration
		cmd.Println("Global configuration:")
		cmd.Printf("  claude_home: %s\n", config.ClaudeHome)
		cmd.Printf("  sbox_data_dir: %s\n", config.SboxDataDir)
		cmd.Printf("  docker_socket: %s\n", config.DockerSocket)
		cmd.Printf("  default_profiles: %v\n", config.DefaultProfiles)
		return nil
	}

	key := args[0]

	if len(args) == 1 {
		// Show specific key
		switch key {
		case "claude_home":
			cmd.Println(config.ClaudeHome)
		case "sbox_data_dir":
			cmd.Println(config.SboxDataDir)
		case "docker_socket":
			cmd.Println(config.DockerSocket)
		case "default_profiles":
			cmd.Printf("%v\n", config.DefaultProfiles)
		default:
			return fmt.Errorf("unknown config key: %s", key)
		}
		return nil
	}

	// Set value
	value := args[1]
	switch key {
	case "claude_home":
		config.ClaudeHome = value
	case "docker_socket":
		if value != "auto" && value != "always" && value != "never" {
			return fmt.Errorf("docker_socket must be one of: auto, always, never")
		}
		config.DockerSocket = value
	default:
		return fmt.Errorf("cannot set config key: %s (read-only or unknown)", key)
	}

	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.Printf("Set %s = %s\n", key, value)
	return nil
}
