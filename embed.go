package sbox

import (
	_ "embed"
)

// SandboxBackendContextMD contains instructions for Claude about the Docker Sandbox (MicroVM) environment.
//
//go:embed embedded/sandbox_backend.md
var SandboxBackendContextMD string

// ContainerBackendContextMD contains instructions for Claude about the Docker Container environment.
//
//go:embed embedded/container_backend.md
var ContainerBackendContextMD string

// GetBackendContextMD returns the appropriate context markdown for the given backend type.
func GetBackendContextMD(backend BackendType) string {
	switch backend {
	case BackendContainer:
		return ContainerBackendContextMD
	case BackendSandbox:
		return SandboxBackendContextMD
	default:
		return SandboxBackendContextMD
	}
}
