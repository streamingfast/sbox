// Package sbox provides a Docker sandbox wrapper for Claude Code with enhanced
// sharing capabilities and profile support.
package sbox

import (
	"sync"

	"github.com/streamingfast/logging"
	"go.uber.org/zap"
)

var zlog *zap.Logger

func init() {
	logging.Register("github.com/streamingfast/sbox", &zlog)
}

var setupLoggingOnce sync.Once

// SetupLogging initializes the logging system. It should be called once
// at the start of command execution. By default, nothing is logged unless
// the DLOG environment variable is set.
//
// The logging system uses the streamingfast/logging library which supports:
// - DLOG environment variable to enable debug/trace logging
// - Pattern-based filtering to target specific packages
// - Dynamic log level switching via HTTP
func SetupLogging() {
	setupLoggingOnce.Do(func() {
		logging.InstantiateLoggers()
	})
}
