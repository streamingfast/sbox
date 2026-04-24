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

// HostBackendContextMD contains instructions for Claude about the host (no Docker) environment.
//
//go:embed embedded/host_backend.md
var HostBackendContextMD string

// GetBackendContextMD returns the appropriate context markdown for the given backend type.
func GetBackendContextMD(backend BackendType) string {
	switch backend {
	case BackendContainer:
		return ContainerBackendContextMD
	case BackendSandbox:
		return SandboxBackendContextMD
	case BackendHost:
		return HostBackendContextMD
	default:
		return SandboxBackendContextMD
	}
}
