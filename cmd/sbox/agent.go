package main

import (
	"fmt"

	"github.com/spf13/cobra"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var AgentGroup = Group("agent", "Manage AI agent configuration",
	Command(agentListE,
		"list",
		"Show available AI agents",
		Description(`
			Lists all available AI agent types that can be used with sbox.
		`),
	),
	Command(agentSetE,
		"set <agent>",
		"Set the default AI agent globally",
		Description(`
			Sets the default AI agent for all new sbox sessions.
			Valid values: claude, opencode

			This can be overridden:
			  - Per-project with sbox.yaml
			  - Per-session with --agent flag
		`),
		ExactArgs(1),
	),
	Command(agentShowE,
		"show",
		"Show current default AI agent",
		Description(`
			Shows the currently configured default AI agent.
		`),
	),
)

func agentListE(cmd *cobra.Command, args []string) error {
	cmd.Println("Available agents:")
	for _, agent := range sbox.ValidAgentTypes {
		marker := ""
		if agent == sbox.DefaultAgent {
			marker = " (default)"
		}
		cmd.Printf("  - %s%s\n", agent.Capitalize(), marker)
	}
	return nil
}

func agentSetE(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	// Validate agent name
	if err := sbox.ValidateAgent(agentName); err != nil {
		return err
	}

	// Load config
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Set default agent
	config.DefaultAgent = agentName

	// Save config
	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.Printf("Default agent set to: %s\n", sbox.AgentType(agentName).Capitalize())
	return nil
}

func agentShowE(cmd *cobra.Command, args []string) error {
	// Load config
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	agent := config.DefaultAgent
	if agent == "" {
		agent = string(sbox.DefaultAgent)
	}

	cmd.Printf("Default agent: %s\n", sbox.AgentType(agent).Capitalize())
	return nil
}
