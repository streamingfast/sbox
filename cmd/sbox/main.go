package main

import (
	"fmt"
	"os"

	. "github.com/streamingfast/cli"
	"github.com/streamingfast/logging"
	"github.com/streamingfast/sbox"
	"go.uber.org/zap"
)

// Version is set via ldflags at build time
var version = "dev"

var zlog, _ = logging.PackageLogger("sbox", "github.com/streamingfast/sbox/cmd/main")

func init() {
	logging.InstantiateLoggers(logging.WithDefaultLevel(zap.DPanicLevel))

	// Sync version with the sbox package for template building
	sbox.Version = version
}

func main() {
	Run(
		"sbox <command>",
		"Docker sandbox wrapper for Claude Code with enhanced sharing and profiles",

		ConfigureVersion(version),
		ConfigureViper("SBOX"),

		// Default command (no subcommand = run)
		Execute(runE),

		RunCommand,
		ProfileGroup,
		EnvGroup,
		ConfigCommand,
		CleanCommand,
		ShellCommand,
		AuthCommand,
		InfoCommand,
		StopCommand,
		EntrypointCommand,

		OnCommandError(func(err error) {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			zlog.Debug("command error", zap.Error(err))
			os.Exit(1)
		}),
	)
}
