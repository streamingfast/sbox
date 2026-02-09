package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"go.uber.org/zap"
)

var ShellCommand = Command(shellE,
	"shell",
	"Connect to a running sandbox or container for this project",
	Description(`
		Opens a bash shell inside the running Docker sandbox or container for the
		current project. Errors if no sandbox/container is running for this project.

		The backend type is determined from the project's configuration
		(sbox.yaml, project config, or global default).

		This is equivalent to running:
		- For sandbox backend: docker sandbox exec -it <name> bash
		- For container backend: docker exec -it <name> bash
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
	}),
)

// shellE opens a shell in the running sandbox/container
func shellE(cmd *cobra.Command, args []string) error {
	ctx, err := LoadWorkspaceContext(cmd)
	if err != nil {
		return err
	}

	zlog.Debug("resolved backend type", zap.String("backend", string(ctx.BackendType)))

	return ctx.Backend.Shell(ctx.WorkspaceDir)
}
