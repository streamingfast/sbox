// Package sbox provides a Docker sandbox wrapper for Claude Code with enhanced
// sharing capabilities and profile support.
package sbox

import (
	"github.com/streamingfast/logging"
)

var zlog, _ = logging.PackageLogger("sbox", "github.com/streamingfast/sbox")
