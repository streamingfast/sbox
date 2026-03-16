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
	Agent    AgentType
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
	// GrpcurlArch is the architecture suffix for grpcurl downloads (x86_64, arm64)
	GrpcurlArch string
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
			GrpcurlArch:    "x86_64",
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
			GrpcurlArch:    "arm64",
		}, nil
	case "x86_64", "amd64":
		return &TargetArch{
			GOARCH:         "amd64",
			DockerPlatform: "linux/amd64",
			GoDownloadArch: "amd64",
			YqArch:         "amd64",
			ProtocArch:     "x86_64",
			GrpcurlArch:    "x86_64",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported Docker architecture: %s", arch)
	}
}

// NewTemplateBuilder creates a new template builder
func NewTemplateBuilder(config *Config, profiles []string, agent AgentType) *TemplateBuilder {
	if agent == "" {
		agent = DefaultAgent
	}
	return &TemplateBuilder{
		Config:   config,
		Profiles: profiles,
		Agent:    agent,
	}
}

// TemplateHash computes a deterministic hash of the template configuration.
// This includes profiles and sbox entrypoint image to ensure rebuilds when either changes.
func (tb *TemplateBuilder) TemplateHash() string {
	// Resolve all profiles including dependencies
	resolved := tb.ResolveProfiles()

	// Sort profiles for deterministic hash
	sort.Strings(resolved)

	// Combine profiles, entrypoint image, and agent type for hash
	// Agent type must be included so Claude and OpenCode templates have different hashes
	entrypointImageStr := tb.entrypointImage()
	if entrypointImageStr == "" {
		entrypointImageStr = "local"
	}
	agentStr := string(tb.Agent)
	if agentStr == "" {
		agentStr = string(DefaultAgent)
	}
	combined := strings.Join(resolved, ",") + ";" + entrypointImageStr + ";" + agentStr
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])[:12]
}

// entrypointImage returns the full sbox binary image name to use.
// If SBOX_ENTRYPOINT_IMAGE is set:
//   - If it equals "local", returns "" (indicates local build mode)
//   - If it contains '/', use it as-is (full image name like "ghcr.io/org/image:tag")
//   - Otherwise, prepend "ghcr.io/streamingfast/sbox:" (tag only like "dev" -> "ghcr.io/streamingfast/sbox:dev")
//
// If not set, constructs image from SboxBinaryImage constant and version.
func (tb *TemplateBuilder) entrypointImage() string {
	envImage := os.Getenv("SBOX_ENTRYPOINT_IMAGE")
	if envImage != "" {
		if envImage == "local" {
			return ""
		}
		if strings.Contains(envImage, "/") {
			return envImage
		}
		return SboxBinaryImage + ":" + envImage
	}

	// Default: use version-based image
	return fmt.Sprintf("%s:%s", SboxBinaryImage, tb.sboxVersion())
}

// isLocalBuildMode returns true if we should build the sbox binary locally.
// This happens when SBOX_ENTRYPOINT_IMAGE=local is set.
func (tb *TemplateBuilder) isLocalBuildMode() bool {
	return os.Getenv("SBOX_ENTRYPOINT_IMAGE") == "local"
}

// sboxVersion returns the sbox version to use for the template.
// If the version is "dev" (no ldflags override), falls back to "latest".
// For semantic versions (e.g., "1.3.1"), ensures "v" prefix is added (e.g., "v1.3.1").
func (tb *TemplateBuilder) sboxVersion() string {
	if Version == "dev" {
		return "latest"
	}

	// Ensure semantic versions have "v" prefix (images are tagged with "v1.3.1" not "1.3.1")
	version := Version
	if len(version) > 0 && version[0] >= '0' && version[0] <= '9' {
		version = "v" + version
	}

	return version
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

	// Determine base template image based on agent type
	baseTemplate := GetBaseTemplateForAgent(tb.Agent)

	// Multi-stage build: first stage gets the sbox binary
	if tb.isLocalBuildMode() {
		// In local build mode, we copy the binary from the build context
		sb.WriteString("# Local build mode: using locally built sbox binary\n")
		sb.WriteString(fmt.Sprintf("FROM %s\n\n", baseTemplate))
	} else {
		// In release mode, copy from the sbox binary image
		sboxImage := tb.entrypointImage()
		sb.WriteString(fmt.Sprintf("FROM %s AS sbox-bin\n\n", sboxImage))
		sb.WriteString(fmt.Sprintf("FROM %s\n\n", baseTemplate))
	}

	// Set architecture variables for profile snippets
	if targetArch != nil {
		sb.WriteString("# Architecture variables for multi-arch support\n")
		sb.WriteString(fmt.Sprintf("ARG TARGETARCH=%s\n", targetArch.GOARCH))
		sb.WriteString(fmt.Sprintf("ARG GO_ARCH=%s\n", targetArch.GoDownloadArch))
		sb.WriteString(fmt.Sprintf("ARG YQ_ARCH=%s\n", targetArch.YqArch))
		sb.WriteString(fmt.Sprintf("ARG PROTOC_ARCH=%s\n", targetArch.ProtocArch))
		sb.WriteString(fmt.Sprintf("ARG GRPCURL_ARCH=%s\n\n", targetArch.GrpcurlArch))
	}

	sb.WriteString("# Switch to root to install sbox and packages\n")
	sb.WriteString("USER root\n\n")

	// Install rsync for claude cache synchronization
	sb.WriteString("# Install rsync for cache synchronization\n")
	sb.WriteString("RUN apt-get update && apt-get install -y --no-install-recommends rsync && rm -rf /var/lib/apt/lists/*\n\n")

	// Copy sbox binary
	if tb.isLocalBuildMode() {
		sb.WriteString("# Copy sbox binary (local build mode - from build context)\n")
		sb.WriteString("COPY sbox /usr/local/bin/sbox\n")
	} else {
		sb.WriteString("# Copy sbox binary from entrypoint image\n")
		sb.WriteString("COPY --from=sbox-bin /usr/local/bin/sbox /usr/local/bin/sbox\n")
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

	// Create agent config directory with proper ownership (as root)
	spec := GetAgentSpec(tb.Agent)
	configDir := spec.ConfigDirName()
	sb.WriteString(fmt.Sprintf("# Ensure %s directory exists with proper ownership\n", configDir))
	sb.WriteString(fmt.Sprintf("RUN mkdir -p /home/agent/%s && chown -R agent:agent /home/agent/%s\n\n", configDir, configDir))

	sb.WriteString("# Switch back to agent user\n")
	sb.WriteString("USER agent\n\n")

	// NOTE: We cannot use ENTRYPOINT - docker sandbox has its own entrypoint that
	// manages container lifecycle. Setting ENTRYPOINT causes SIGKILL (exit 137).
	//
	// Instead, we replace the agent binary with our wrapper that does setup
	// before exec'ing the real agent. This ensures plugins/agents are loaded
	// BEFORE the agent starts.
	wrapperName := spec.WrapperName()
	binaryName := spec.BinaryName()

	sb.WriteString(fmt.Sprintf("# Create %s wrapper script\n", binaryName))
	sb.WriteString("USER root\n")
	sb.WriteString(fmt.Sprintf(`COPY <<'WRAPPER_EOF' /usr/local/bin/%s
#!/bin/bash
# sbox wrapper for %s - does setup before starting %s
# PWD/WORKSPACE_DIR is set to workspace by docker sandbox
exec /usr/local/bin/sbox entrypoint
WRAPPER_EOF
RUN chmod +x /usr/local/bin/%s
`, wrapperName, binaryName, binaryName, wrapperName))
	sb.WriteString(fmt.Sprintf("# Replace %s with our wrapper\n", binaryName))
	sb.WriteString(fmt.Sprintf(`RUN AGENT_PATH=$(which %s) && \
    if [ -n "$AGENT_PATH" ]; then \
        mv "$AGENT_PATH" "${AGENT_PATH}-real" && \
        ln -s /usr/local/bin/%s "$AGENT_PATH"; \
    fi
`, binaryName, wrapperName))
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
		entrypointImage := tb.entrypointImage()
		if entrypointImage == "" {
			entrypointImage = "local"
		}
		zlog.Debug("using existing custom image",
			zap.String("image", imageName),
			zap.Strings("profiles", tb.Profiles),
			zap.String("entrypoint_image", entrypointImage))
		return imageName, nil
	}

	// Detect target architecture from Docker
	targetArch, err := GetTargetArch()
	if err != nil {
		return "", fmt.Errorf("failed to detect target architecture: %w", err)
	}

	entrypointImage := tb.entrypointImage()
	if entrypointImage == "" {
		entrypointImage = "local"
	}
	zlog.Info("building custom template image",
		zap.String("image", imageName),
		zap.Strings("profiles", tb.Profiles),
		zap.String("entrypoint_image", entrypointImage),
		zap.String("platform", targetArch.DockerPlatform),
		zap.Bool("local_build_mode", tb.isLocalBuildMode()))

	// When force rebuilding, pull the latest base image to get newest agent version
	if forceRebuild {
		baseTemplate := GetBaseTemplateForAgent(tb.Agent)
		agentName := tb.Agent.Capitalize()
		DefaultUI.Status("Pulling latest base image to get newest %s version", agentName)
		pullCmd := exec.Command("docker", "pull", baseTemplate)
		pullCmd.Stdout = os.Stdout
		pullCmd.Stderr = os.Stderr
		if err := pullCmd.Run(); err != nil {
			zlog.Warn("failed to pull base image, continuing with cached version",
				zap.String("image", baseTemplate),
				zap.Error(err))
			// Non-fatal: continue with cached base image
		} else {
			DefaultUI.Status("Base image updated successfully")
		}
	}

	// Create temporary directory for Dockerfile and binary
	tempDir, err := os.MkdirTemp("", "sbox-template-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// In local build mode, cross-compile sbox binary
	if tb.isLocalBuildMode() {
		if err := tb.buildLocalBinary(tempDir, targetArch); err != nil {
			return "", fmt.Errorf("failed to build local binary: %w", err)
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
	buildArgs := []string{"build", "--platform", targetArch.DockerPlatform, "-t", imageName, "-f", dockerfilePath, tempDir}
	cmd := exec.Command("docker", buildArgs...)
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

// buildLocalBinary cross-compiles sbox for the target architecture and places it in the build context
func (tb *TemplateBuilder) buildLocalBinary(buildDir string, targetArch *TargetArch) error {
	DefaultUI.Status("Building sbox binary for %s (local build mode)", targetArch.DockerPlatform)

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

	return "", fmt.Errorf("cannot find sbox source directory; ensure SBOX_ENTRYPOINT_IMAGE=local is run from the sbox repo directory")
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
