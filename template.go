package sbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"go.uber.org/zap"
)

// Version is set via ldflags at build time
// This is used to determine which sbox binary image to use
var Version = "dev"

// SboxBinaryImage is the container image containing the sbox binary
const SboxBinaryImage = "ghcr.io/streamingfast/sbox"

// TemplateBuilder handles building custom Docker images with profiles
type TemplateBuilder struct {
	Config   *Config
	Profiles []string
}

// TargetArch represents the target architecture for cross-compilation
type TargetArch struct {
	// GOARCH is the Go architecture name (amd64, arm64)
	GOARCH string
	// DockerPlatform is the Docker platform string (linux/amd64, linux/arm64)
	DockerPlatform string
	// GoDownloadArch is the architecture suffix for Go downloads (amd64, arm64)
	GoDownloadArch string
	// YqArch is the architecture suffix for yq downloads (amd64, arm64)
	YqArch string
	// ProtocArch is the architecture suffix for protoc downloads (x86_64, aarch_64)
	ProtocArch string
}

// GetTargetArch detects the target architecture from Docker's default platform.
// This ensures we build for the same architecture that Docker will run containers on.
func GetTargetArch() (*TargetArch, error) {
	// Query Docker for its default architecture
	cmd := exec.Command("docker", "info", "--format", "{{.Architecture}}")
	output, err := cmd.Output()
	if err != nil {
		// Fallback to amd64 if Docker query fails
		zlog.Warn("failed to detect Docker architecture, defaulting to amd64", zap.Error(err))
		return &TargetArch{
			GOARCH:         "amd64",
			DockerPlatform: "linux/amd64",
			GoDownloadArch: "amd64",
			YqArch:         "amd64",
			ProtocArch:     "x86_64",
		}, nil
	}

	arch := strings.TrimSpace(string(output))
	zlog.Debug("detected Docker architecture", zap.String("arch", arch))

	switch arch {
	case "aarch64", "arm64":
		return &TargetArch{
			GOARCH:         "arm64",
			DockerPlatform: "linux/arm64",
			GoDownloadArch: "arm64",
			YqArch:         "arm64",
			ProtocArch:     "aarch_64",
		}, nil
	case "x86_64", "amd64":
		return &TargetArch{
			GOARCH:         "amd64",
			DockerPlatform: "linux/amd64",
			GoDownloadArch: "amd64",
			YqArch:         "amd64",
			ProtocArch:     "x86_64",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported Docker architecture: %s", arch)
	}
}

// NewTemplateBuilder creates a new template builder
func NewTemplateBuilder(config *Config, profiles []string) *TemplateBuilder {
	return &TemplateBuilder{
		Config:   config,
		Profiles: profiles,
	}
}

// TemplateHash computes a deterministic hash of the template configuration.
// This includes profiles and sbox version to ensure rebuilds when either changes.
func (tb *TemplateBuilder) TemplateHash() string {
	// Resolve all profiles including dependencies
	resolved := tb.ResolveProfiles()

	// Sort profiles for deterministic hash
	sort.Strings(resolved)

	// Combine profiles with sbox version
	combined := strings.Join(resolved, ",") + ";" + tb.sboxVersion()
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])[:12]
}

// sboxVersion returns the sbox version to use for the template.
// In dev mode (SBOX_DEV=1), returns "dev" to indicate local build.
func (tb *TemplateBuilder) sboxVersion() string {
	if os.Getenv("SBOX_DEV") == "1" {
		return "dev"
	}
	return Version
}

// isDevMode returns true if SBOX_DEV=1 is set
func (tb *TemplateBuilder) isDevMode() bool {
	return os.Getenv("SBOX_DEV") == "1"
}

// ResolveProfiles returns the full list of profiles including all dependencies.
// Dependencies are listed before the profiles that depend on them.
func (tb *TemplateBuilder) ResolveProfiles() []string {
	seen := make(map[string]bool)
	var result []string

	var resolve func(name string)
	resolve = func(name string) {
		if seen[name] {
			return
		}

		profile, ok := GetProfile(name)
		if !ok {
			// Unknown profile, include it anyway (will error later)
			seen[name] = true
			result = append(result, name)
			return
		}

		// First resolve dependencies
		for _, dep := range profile.Dependencies {
			resolve(dep)
		}

		// Then add this profile
		seen[name] = true
		result = append(result, name)
	}

	for _, name := range tb.Profiles {
		resolve(name)
	}

	return result
}

// ImageName returns the Docker image name for this template configuration.
// Always returns a custom sbox-template image name since we need the sbox entrypoint.
func (tb *TemplateBuilder) ImageName() string {
	return fmt.Sprintf("sbox-template:%s", tb.TemplateHash())
}

// ImageExists checks if the custom image already exists
func (tb *TemplateBuilder) ImageExists() bool {
	imageName := tb.ImageName()

	cmd := exec.Command("docker", "image", "inspect", imageName)
	err := cmd.Run()
	return err == nil
}

// GenerateDockerfile creates a Dockerfile with sbox entrypoint and all selected profiles.
// If targetArch is nil, a basic Dockerfile without architecture-specific variables is generated
// (used for non-build operations like display).
func (tb *TemplateBuilder) GenerateDockerfile(targetArch *TargetArch) (string, error) {
	var sb strings.Builder

	sb.WriteString("# Auto-generated by sbox\n")

	// Multi-stage build: first stage gets the sbox binary
	if tb.isDevMode() {
		// In dev mode, we copy the binary from the build context
		sb.WriteString("# Dev mode: using local sbox binary\n")
		sb.WriteString(fmt.Sprintf("FROM %s\n\n", DefaultTemplateImage))
	} else {
		// In release mode, copy from the sbox binary image
		sboxImage := fmt.Sprintf("%s:%s", SboxBinaryImage, tb.sboxVersion())
		sb.WriteString(fmt.Sprintf("FROM %s AS sbox-bin\n\n", sboxImage))
		sb.WriteString(fmt.Sprintf("FROM %s\n\n", DefaultTemplateImage))
	}

	// Set architecture variables for profile snippets
	if targetArch != nil {
		sb.WriteString("# Architecture variables for multi-arch support\n")
		sb.WriteString(fmt.Sprintf("ARG TARGETARCH=%s\n", targetArch.GOARCH))
		sb.WriteString(fmt.Sprintf("ARG GO_ARCH=%s\n", targetArch.GoDownloadArch))
		sb.WriteString(fmt.Sprintf("ARG YQ_ARCH=%s\n", targetArch.YqArch))
		sb.WriteString(fmt.Sprintf("ARG PROTOC_ARCH=%s\n\n", targetArch.ProtocArch))
	}

	sb.WriteString("# Switch to root to install sbox and packages\n")
	sb.WriteString("USER root\n\n")

	// Copy sbox binary
	if tb.isDevMode() {
		sb.WriteString("# Copy sbox binary (dev mode - from build context)\n")
		sb.WriteString("COPY sbox /usr/local/bin/sbox\n")
	} else {
		sb.WriteString("# Copy sbox binary from release image\n")
		sb.WriteString("COPY --from=sbox-bin /sbox /usr/local/bin/sbox\n")
	}
	sb.WriteString("RUN chmod +x /usr/local/bin/sbox\n\n")

	// Add profiles if any
	resolvedProfiles := tb.ResolveProfiles()
	if len(resolvedProfiles) > 0 {
		for _, profileName := range resolvedProfiles {
			profile, ok := GetProfile(profileName)
			if !ok {
				return "", fmt.Errorf("unknown profile: %s", profileName)
			}

			sb.WriteString(fmt.Sprintf("# Profile: %s\n", profileName))
			sb.WriteString(fmt.Sprintf("# %s\n", profile.Description))
			sb.WriteString(profile.DockerfileSnippet)
			sb.WriteString("\n")
		}
	}

	// Pre-create the sbox env file so the agent user can write to it at runtime.
	// We use /etc/profile.d/ so it gets sourced by login shells (bash -l).
	sb.WriteString("# Create sbox persistent env file (writable by agent)\n")
	sb.WriteString("RUN touch /etc/profile.d/sbox-env.sh && chmod 666 /etc/profile.d/sbox-env.sh\n\n")

	sb.WriteString("# Switch back to agent user\n")
	sb.WriteString("USER agent\n\n")

	// NOTE: We cannot use ENTRYPOINT - docker sandbox has its own entrypoint that
	// manages container lifecycle. Setting ENTRYPOINT causes SIGKILL (exit 137).
	//
	// Instead, we replace the claude binary with our wrapper that does setup
	// before exec'ing the real claude. This ensures plugins/agents are loaded
	// BEFORE claude starts.
	sb.WriteString("# Create claude wrapper script\n")
	sb.WriteString("USER root\n")
	sb.WriteString(`COPY <<'WRAPPER_EOF' /usr/local/bin/claude-wrapper
#!/bin/bash
# sbox wrapper for claude - does setup before starting claude
# PWD/WORKSPACE_DIR is set to workspace by docker sandbox
exec /usr/local/bin/sbox entrypoint
WRAPPER_EOF
RUN chmod +x /usr/local/bin/claude-wrapper
`)
	sb.WriteString("# Replace claude with our wrapper\n")
	sb.WriteString(`RUN CLAUDE_PATH=$(which claude) && \
    if [ -n "$CLAUDE_PATH" ]; then \
        mv "$CLAUDE_PATH" "${CLAUDE_PATH}-real" && \
        ln -s /usr/local/bin/claude-wrapper "$CLAUDE_PATH"; \
    fi
`)
	sb.WriteString("USER agent\n\n")

	sb.WriteString("# CMD for sbox entrypoint - docker sandbox may override this\n")
	sb.WriteString("CMD [\"sbox\", \"entrypoint\"]\n")

	return sb.String(), nil
}

// Build builds the custom Docker image with sbox entrypoint and selected profiles.
// Returns the image name to use.
func (tb *TemplateBuilder) Build(forceRebuild bool) (string, error) {
	imageName := tb.ImageName()

	// Check if image already exists
	if !forceRebuild && tb.ImageExists() {
		zlog.Debug("using existing custom image",
			zap.String("image", imageName),
			zap.Strings("profiles", tb.Profiles),
			zap.String("sbox_version", tb.sboxVersion()))
		return imageName, nil
	}

	// Detect target architecture from Docker
	targetArch, err := GetTargetArch()
	if err != nil {
		return "", fmt.Errorf("failed to detect target architecture: %w", err)
	}

	zlog.Info("building custom template image",
		zap.String("image", imageName),
		zap.Strings("profiles", tb.Profiles),
		zap.String("sbox_version", tb.sboxVersion()),
		zap.String("platform", targetArch.DockerPlatform),
		zap.Bool("dev_mode", tb.isDevMode()))

	// Create temporary directory for Dockerfile and binary
	tempDir, err := os.MkdirTemp("", "sbox-template-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// In dev mode, cross-compile sbox binary
	if tb.isDevMode() {
		if err := tb.buildDevBinary(tempDir, targetArch); err != nil {
			return "", fmt.Errorf("failed to build dev binary: %w", err)
		}
	}

	// Generate Dockerfile
	dockerfile, err := tb.GenerateDockerfile(targetArch)
	if err != nil {
		return "", fmt.Errorf("failed to generate Dockerfile: %w", err)
	}

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return "", fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	zlog.Debug("generated Dockerfile",
		zap.String("path", dockerfilePath),
		zap.Int("size", len(dockerfile)))

	// Build the image with explicit platform
	cmd := exec.Command("docker", "build", "--platform", targetArch.DockerPlatform, "-t", imageName, "-f", dockerfilePath, tempDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker build failed: %w", err)
	}

	zlog.Info("custom template image built successfully",
		zap.String("image", imageName),
		zap.String("platform", targetArch.DockerPlatform))

	return imageName, nil
}

// buildDevBinary cross-compiles sbox for the target architecture and places it in the build context
func (tb *TemplateBuilder) buildDevBinary(buildDir string, targetArch *TargetArch) error {
	fmt.Printf("Building sbox binary for %s (dev mode)...\n", targetArch.DockerPlatform)

	binaryPath := filepath.Join(buildDir, "sbox")

	// Find the sbox source directory (assumes we're in the sbox repo or can find it)
	// Try to find go.mod to locate the module root
	srcDir, err := findSboxSourceDir()
	if err != nil {
		return fmt.Errorf("failed to find sbox source directory: %w", err)
	}

	zlog.Debug("cross-compiling sbox",
		zap.String("src_dir", srcDir),
		zap.String("output", binaryPath),
		zap.String("goarch", targetArch.GOARCH))

	// Cross-compile for target architecture
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/sbox")
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH="+targetArch.GOARCH,
		"CGO_ENABLED=0",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}

	fmt.Println("sbox binary built successfully")
	return nil
}

// findSboxSourceDir attempts to find the sbox source directory
func findSboxSourceDir() (string, error) {
	// First, try the current working directory
	cwd, err := os.Getwd()
	if err == nil {
		if isSboxSourceDir(cwd) {
			return cwd, nil
		}
	}

	// Try to find via the running binary's location
	execPath, err := os.Executable()
	if err == nil {
		// Walk up from executable to find go.mod
		dir := filepath.Dir(execPath)
		for i := 0; i < 5; i++ {
			if isSboxSourceDir(dir) {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// Try GOPATH
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		homeDir, _ := os.UserHomeDir()
		gopath = filepath.Join(homeDir, "go")
	}
	sboxPath := filepath.Join(gopath, "src", "github.com", "streamingfast", "sbox")
	if isSboxSourceDir(sboxPath) {
		return sboxPath, nil
	}

	// As a fallback for typical development setups, check if we're on a Mac
	// and the code might be in a common location
	if runtime.GOOS == "darwin" {
		homeDir, _ := os.UserHomeDir()
		commonPaths := []string{
			filepath.Join(homeDir, "work", "sf", "sbox"),
			filepath.Join(homeDir, "code", "sbox"),
			filepath.Join(homeDir, "projects", "sbox"),
		}
		for _, p := range commonPaths {
			if isSboxSourceDir(p) {
				return p, nil
			}
		}
	}

	return "", fmt.Errorf("cannot find sbox source directory; ensure SBOX_DEV=1 is run from the sbox repo directory")
}

// isSboxSourceDir checks if a directory looks like the sbox source directory
func isSboxSourceDir(dir string) bool {
	// Check for go.mod
	goModPath := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		return false
	}

	// Check for cmd/sbox
	cmdPath := filepath.Join(dir, "cmd", "sbox")
	if _, err := os.Stat(cmdPath); err != nil {
		return false
	}

	return true
}

// CleanTemplates removes all cached sbox template images
func CleanTemplates() error {
	zlog.Info("cleaning cached template images")

	// List all sbox-template images
	cmd := exec.Command("docker", "images", "--filter", "reference=sbox-template:*", "--format", "{{.Repository}}:{{.Tag}}")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list template images: %w", err)
	}

	images := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, image := range images {
		if image == "" {
			continue
		}

		zlog.Debug("removing template image", zap.String("image", image))
		rmCmd := exec.Command("docker", "rmi", image)
		if err := rmCmd.Run(); err != nil {
			zlog.Warn("failed to remove image",
				zap.String("image", image),
				zap.Error(err))
		}
	}

	return nil
}
