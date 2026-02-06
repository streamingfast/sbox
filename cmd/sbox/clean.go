package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var CleanCommand = Command(cleanE,
	"clean",
	"Clean up sandbox containers, images, and cached data",
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("all", false, "Remove all cached images and project data")
		flags.Bool("images", false, "Remove cached profile images only")
	}),
)

// cleanE cleans up sandbox resources
func cleanE(cmd *cobra.Command, args []string) error {
	cleanAll, _ := cmd.Flags().GetBool("all")
	cleanImages, _ := cmd.Flags().GetBool("images")

	if !cleanAll && !cleanImages {
		// Default to cleaning images
		cleanImages = true
	}

	if cleanImages || cleanAll {
		cmd.Println("Cleaning cached template images...")
		if err := sbox.CleanTemplates(); err != nil {
			return fmt.Errorf("failed to clean templates: %w", err)
		}
		cmd.Println("Template images cleaned")
	}

	if cleanAll {
		cmd.Println("Cleaning project data...")
		config, err := sbox.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		projectsDir := config.SboxDataDir + "/projects"
		if err := os.RemoveAll(projectsDir); err != nil {
			return fmt.Errorf("failed to clean projects: %w", err)
		}
		cmd.Println("Project data cleaned")
	}

	cmd.Println("Cleanup complete")
	return nil
}
