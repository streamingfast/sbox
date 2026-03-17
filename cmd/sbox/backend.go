package main

import (
	"fmt"

	"github.com/spf13/cobra"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var BackendGroup = Group("backend", "Manage container backend configuration",
	Command(backendListE,
		"list",
		"Show available backends",
		Description(`
			Lists all available backend types that can be used with sbox.
		`),
	),
	Command(backendSetE,
		"set <backend>",
		"Set the default backend globally",
		Description(`
			Sets the default container backend for all new sbox sessions.
			Valid values: sandbox, container

			This can be overridden:
			  - Per-project with sbox.yaml
			  - Per-session with --backend flag
		`),
		ExactArgs(1),
	),
	Command(backendShowE,
		"show",
		"Show current default backend",
		Description(`
			Shows the currently configured default container backend.
		`),
	),
)

func backendListE(cmd *cobra.Command, args []string) error {
	cmd.Println("Available backends:")
	for _, backend := range sbox.ValidBackendTypes {
		marker := ""
		if backend == sbox.DefaultBackend {
			marker = " (default)"
		}
		cmd.Printf("  - %s%s\n", backend.Capitalize(), marker)
	}
	return nil
}

func backendSetE(cmd *cobra.Command, args []string) error {
	backendName := args[0]

	// Validate backend name
	if err := sbox.ValidateBackend(backendName); err != nil {
		return err
	}

	// Load config
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Set default backend
	config.DefaultBackend = backendName

	// Save config
	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.Printf("Default backend set to: %s\n", sbox.BackendType(backendName).Capitalize())
	return nil
}

func backendShowE(cmd *cobra.Command, args []string) error {
	// Load config
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	backend := config.DefaultBackend
	if backend == "" {
		backend = string(sbox.DefaultBackend)
	}

	cmd.Printf("Default backend: %s\n", sbox.BackendType(backend).Capitalize())
	return nil
}
