//go:build integration
// +build integration

package sbox

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Integration tests for Docker profiles
// These tests actually build Docker images and are expensive to run.
// Run with: go test -tags=integration -v ./...

func TestIntegration_DockerAvailable(t *testing.T) {
	cmd := exec.Command("docker", "version")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker not available, skipping integration tests")
	}
}

func TestIntegration_GoProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testProfileBuild(t, "go", []string{
		"go version",
	}, []string{
		"go version go",
	})
}

func TestIntegration_RustProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test basic commands and also rustup operations which require write access
	// to /usr/local/rustup/tmp/ - this validates the chmod a+rwx fix
	testProfileBuild(t, "rust", []string{
		"rustc --version",
		"cargo --version",
		"rustup show", // This exercises rustup's write to tmp directory
	}, []string{
		"rustc",
		"cargo",
		"stable", // rustup show should display the stable toolchain
	})
}

func TestIntegration_DockerProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testProfileBuild(t, "docker", []string{
		"docker --version",
	}, []string{
		"Docker version",
	})
}

func TestIntegration_BashUtilsProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testProfileBuild(t, "bash-utils", []string{
		"jq --version",
		"yq --version",
		"git --version",
		"curl --version",
	}, []string{
		"jq-",
		"yq",
		"git version",
		"curl",
	})
}

// testProfileBuild builds a Docker image with the given profile and runs verification commands
// Commands are run as a non-root user to simulate the sandbox environment where user 'agent' runs
func testProfileBuild(t *testing.T, profileName string, commands []string, expectedOutputs []string) {
	t.Helper()

	profile, ok := GetProfile(profileName)
	if !ok {
		t.Fatalf("Profile %q not found", profileName)
	}

	// Create a temporary Dockerfile
	tempDir := t.TempDir()
	dockerfilePath := fmt.Sprintf("%s/Dockerfile", tempDir)

	// The Dockerfile creates user 'agent' to match the sandbox environment.
	// In the real sandbox, docker/sandbox-templates:claude-code runs as user 'agent'.
	dockerfile := fmt.Sprintf(`FROM debian:bookworm-slim

# Install basic dependencies
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

%s

# Create 'agent' user to match the sandbox environment
RUN useradd -m -s /bin/bash agent

# Switch to agent user (same as sandbox)
USER agent

# Set default command
CMD ["bash"]
`, profile.DockerfileSnippet)

	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// Build the image
	imageName := fmt.Sprintf("sbox-test-%s:%d", profileName, time.Now().Unix())
	t.Logf("Building image %s...", imageName)

	buildCmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfilePath, tempDir)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build Docker image: %v", err)
	}

	// Clean up image after test
	defer func() {
		cleanupCmd := exec.Command("docker", "rmi", "-f", imageName)
		cleanupCmd.Run() // Ignore errors on cleanup
	}()

	// Run verification commands
	for i, command := range commands {
		t.Logf("Running verification: %s", command)

		runCmd := exec.Command("docker", "run", "--rm", imageName, "bash", "-c", command)
		var stdout, stderr bytes.Buffer
		runCmd.Stdout = &stdout
		runCmd.Stderr = &stderr

		if err := runCmd.Run(); err != nil {
			t.Errorf("Command %q failed: %v\nStderr: %s", command, err, stderr.String())
			continue
		}

		output := stdout.String()
		if i < len(expectedOutputs) {
			if !strings.Contains(output, expectedOutputs[i]) {
				t.Errorf("Command %q output %q does not contain expected %q", command, output, expectedOutputs[i])
			}
		}
	}
}

func TestIntegration_MultipleProfiles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test building an image with multiple profiles
	profiles := []string{"go", "bash-utils"}

	tempDir := t.TempDir()
	dockerfilePath := fmt.Sprintf("%s/Dockerfile", tempDir)

	var snippets strings.Builder
	for _, name := range profiles {
		profile, ok := GetProfile(name)
		if !ok {
			t.Fatalf("Profile %q not found", name)
		}
		snippets.WriteString(profile.DockerfileSnippet)
		snippets.WriteString("\n")
	}

	dockerfile := fmt.Sprintf(`FROM debian:bookworm-slim

# Install basic dependencies
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

%s

CMD ["bash"]
`, snippets.String())

	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	imageName := fmt.Sprintf("sbox-test-multi:%d", time.Now().Unix())
	t.Logf("Building multi-profile image %s...", imageName)

	buildCmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfilePath, tempDir)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build multi-profile Docker image: %v", err)
	}

	defer func() {
		cleanupCmd := exec.Command("docker", "rmi", "-f", imageName)
		cleanupCmd.Run()
	}()

	// Verify both go and jq are available
	verifyCommands := []struct {
		cmd      string
		contains string
	}{
		{"go version", "go version go"},
		{"jq --version", "jq-"},
	}

	for _, v := range verifyCommands {
		runCmd := exec.Command("docker", "run", "--rm", imageName, "bash", "-c", v.cmd)
		var stdout bytes.Buffer
		runCmd.Stdout = &stdout

		if err := runCmd.Run(); err != nil {
			t.Errorf("Command %q failed: %v", v.cmd, err)
			continue
		}

		if !strings.Contains(stdout.String(), v.contains) {
			t.Errorf("Command %q output %q does not contain %q", v.cmd, stdout.String(), v.contains)
		}
	}
}

func TestIntegration_SubstreamsProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Substreams profile has rust as a dependency, so we need to test the full
	// resolved profile chain using the TemplateBuilder
	config := &Config{}
	builder := NewTemplateBuilder(config, []string{"substreams"})

	// Verify dependencies are resolved correctly
	resolved := builder.ResolveProfiles()
	if len(resolved) != 2 || resolved[0] != "rust" || resolved[1] != "substreams" {
		t.Fatalf("Expected resolved profiles [rust, substreams], got %v", resolved)
	}

	// Build a test image with the full profile chain
	tempDir := t.TempDir()
	dockerfilePath := fmt.Sprintf("%s/Dockerfile", tempDir)

	var snippets strings.Builder
	for _, name := range resolved {
		profile, ok := GetProfile(name)
		if !ok {
			t.Fatalf("Profile %q not found", name)
		}
		snippets.WriteString(fmt.Sprintf("# Profile: %s\n", name))
		snippets.WriteString(profile.DockerfileSnippet)
		snippets.WriteString("\n")
	}

	dockerfile := fmt.Sprintf(`FROM debian:bookworm-slim

# Install basic dependencies
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

%s

# Create 'agent' user to match the sandbox environment
RUN useradd -m -s /bin/bash agent

# Switch to agent user (same as sandbox)
USER agent

CMD ["bash"]
`, snippets.String())

	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	imageName := fmt.Sprintf("sbox-test-substreams:%d", time.Now().Unix())
	t.Logf("Building substreams profile image %s...", imageName)

	buildCmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfilePath, tempDir)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build Docker image: %v", err)
	}

	// Clean up image after test
	defer func() {
		cleanupCmd := exec.Command("docker", "rmi", "-f", imageName)
		cleanupCmd.Run()
	}()

	// Verify all tools are available
	verifyCommands := []struct {
		cmd      string
		contains string
	}{
		// Rust dependency tools
		{"rustc --version", "rustc"},
		{"cargo --version", "cargo"},
		// Substreams tools
		{"substreams --version", "substreams"},
		{"firecore --version", "firecore"},
		// Protobuf tools
		{"buf --version", ""},
		{"protoc --version", "libprotoc"},
	}

	for _, v := range verifyCommands {
		t.Logf("Running verification: %s", v.cmd)

		runCmd := exec.Command("docker", "run", "--rm", imageName, "bash", "-c", v.cmd)
		var stdout, stderr bytes.Buffer
		runCmd.Stdout = &stdout
		runCmd.Stderr = &stderr

		if err := runCmd.Run(); err != nil {
			t.Errorf("Command %q failed: %v\nStdout: %s\nStderr: %s", v.cmd, err, stdout.String(), stderr.String())
			continue
		}

		output := stdout.String() + stderr.String() // Some tools output version to stderr
		if !strings.Contains(output, v.contains) {
			t.Errorf("Command %q output %q does not contain expected %q", v.cmd, output, v.contains)
		}
	}
}

// TestIntegration_EndToEnd tests the full sbox workflow
func TestIntegration_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if docker sandbox is available
	cmd := exec.Command("docker", "sandbox", "--help")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker sandbox not available, skipping end-to-end test")
	}

	// This test would run the full sbox workflow but requires Docker Desktop with sandbox support
	t.Log("Docker sandbox is available - end-to-end test would run here")
	// In a real environment, we would:
	// 1. Build sbox binary
	// 2. Run sbox in a test workspace
	// 3. Verify mounts are correct
	// 4. Test sbox shell command
	// 5. Test sbox clean command
}
