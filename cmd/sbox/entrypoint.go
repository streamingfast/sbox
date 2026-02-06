package main

import (
	"github.com/spf13/cobra"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var EntrypointCommand = Command(entrypointE,
	"entrypoint",
	"Internal command: sandbox entrypoint (not for direct use)",
	Description(`
		This command is used as the entrypoint inside Docker sandbox containers.
		It sets up plugins, agents, and environment variables before starting claude.

		Do not run this command directly - it is invoked automatically when
		the sandbox starts.
	`),
)

// entrypointE is the internal entrypoint command run inside the sandbox
func entrypointE(cmd *cobra.Command, args []string) error {
	return sbox.RunEntrypoint(args)
}
