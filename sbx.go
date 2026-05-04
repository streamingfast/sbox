package sbox

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

// SbxSandbox contains information about an sbx sandbox (from sbx ls).
type SbxSandbox struct {
	// ID is the sandbox ID (used with sbx rm)
	ID string
	// Name is the sandbox name
	Name string
	// Status is the sandbox status (e.g., "running", "stopped")
	Status string
	// Ports contains any exposed port mappings
	Ports string
	// Workspace is the mounted workspace path
	Workspace string
}

// ListSbxSandboxes returns all sbx sandboxes from `sbx ls`.
func ListSbxSandboxes() ([]SbxSandbox, error) {
	cmd := exec.Command("sbx", "ls")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		zlog.Debug("sbx ls command failed",
			zap.Error(err),
			zap.String("stderr", stderr.String()))
		return nil, fmt.Errorf("sbx ls failed: %w", err)
	}

	return parseSbxLsOutput(stdout.String())
}

// parseSbxLsOutput parses the table output from `sbx ls`.
// The output is a fixed-width column table with headers:
//
//	SANDBOX   AGENT   STATUS   PORTS   WORKSPACE
func parseSbxLsOutput(output string) ([]SbxSandbox, error) {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) < 2 {
		return nil, nil
	}

	header := lines[0]

	type col struct {
		name  string
		start int
	}
	colNames := []string{"SANDBOX", "AGENT", "STATUS", "PORTS", "WORKSPACE"}
	var cols []col
	for _, name := range colNames {
		idx := strings.Index(header, name)
		if idx < 0 {
			return nil, fmt.Errorf("sbx ls: missing column %q in header: %s", name, header)
		}
		cols = append(cols, col{name: name, start: idx})
	}

	extractField := func(line string, colIdx int) string {
		start := cols[colIdx].start
		if start >= len(line) {
			return ""
		}
		var end int
		if colIdx+1 < len(cols) {
			end = cols[colIdx+1].start
		} else {
			end = len(line)
		}
		if end > len(line) {
			end = len(line)
		}
		return strings.TrimSpace(line[start:end])
	}

	var sandboxes []SbxSandbox
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}

		name := extractField(line, 0)
		workspace := extractField(line, 4)
		if workspace == "-" {
			workspace = ""
		}

		sandboxes = append(sandboxes, SbxSandbox{
			ID:        name,
			Name:      name,
			Status:    extractField(line, 2),
			Ports:     extractField(line, 3),
			Workspace: workspace,
		})
	}

	return sandboxes, nil
}

// GenerateSbxName generates an sbx sandbox name from a workspace path and agent type.
// Format: <agent>-<basename of workspace> (no sbox- prefix, matching sbx naming convention).
// The name is sanitized to only contain letters, numbers, and hyphens.
func GenerateSbxName(workspaceDir string, agent AgentType) (string, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	basename := filepath.Base(absPath)

	reg := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	sanitized := reg.ReplaceAllString(basename, "-")

	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}

	sanitized = strings.Trim(sanitized, "-")

	if sanitized == "" {
		sanitized = "workspace"
	}

	agentName := string(agent)
	if agentName == "" {
		agentName = string(DefaultAgent)
	}

	return agentName + "-" + sanitized, nil
}

// FindSbxSandboxByName finds an sbx sandbox by its name.
// Returns the sandbox if found, nil otherwise.
func FindSbxSandboxByName(name string) (*SbxSandbox, error) {
	sandboxes, err := ListSbxSandboxes()
	if err != nil {
		return nil, err
	}

	for _, sb := range sandboxes {
		if sb.Name == name {
			zlog.Debug("found sbx sandbox by name",
				zap.String("sandbox_name", name))
			return &sb, nil
		}
	}

	return nil, nil
}

// FindSbxSandbox finds an sbx sandbox for the given workspace directory.
// First tries to find by expected sandbox name, then falls back to workspace path.
func FindSbxSandbox(workspaceDir string) (*SbxSandbox, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		realPath = absPath
	}

	sandboxes, err := ListSbxSandboxes()
	if err != nil {
		return nil, err
	}

	projectConfig, _, err := GetProjectConfig(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load project config: %w", err)
	}

	agentType := AgentType(projectConfig.Agent)
	if agentType == "" {
		agentType = DefaultAgent
	}

	expectedName, _ := GenerateSbxName(workspaceDir, agentType)
	for _, sb := range sandboxes {
		if sb.Name == expectedName {
			zlog.Debug("found sbx sandbox by name",
				zap.String("sandbox_name", expectedName),
				zap.String("workspace", absPath))
			return &sb, nil
		}
	}

	for _, sb := range sandboxes {
		sbReal, err := filepath.EvalSymlinks(sb.Workspace)
		if err != nil {
			sbReal = sb.Workspace
		}

		if sb.Workspace == absPath || sb.Workspace == realPath ||
			sbReal == absPath || sbReal == realPath {
			zlog.Debug("found sbx sandbox by workspace",
				zap.String("sandbox_name", sb.Name),
				zap.String("workspace", absPath))
			return &sb, nil
		}
	}

	return nil, nil
}

// CreateSbxSandbox creates a new sbx sandbox with the given name and options.
// Uses `sbx create` command.
func CreateSbxSandbox(name string, workspaceDir string, templateImage string, agent AgentType, debug bool) error {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	if agent == "" {
		agent = DefaultAgent
	}
	spec := GetAgentSpec(agent)
	agentBinary := spec.BinaryName()

	args := []string{}
	if debug {
		args = append(args, "--debug")
	}
	args = append(args, "create", "--name", name)

	if templateImage != "" && templateImage != DefaultTemplateImage {
		args = append(args, "--template", templateImage)
	}

	args = append(args, agentBinary, absPath)

	zlog.Info("creating sbx sandbox",
		zap.String("name", name),
		zap.String("workspace", absPath),
		zap.String("template", templateImage),
		zap.Bool("debug", debug),
		zap.Strings("args", args))

	cmd := exec.Command("sbx", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sbx create failed: %w", err)
	}

	zlog.Info("sbx sandbox created", zap.String("name", name))
	return nil
}

// StopSbxSandboxByName stops a running sbx sandbox by name.
func StopSbxSandboxByName(name string) error {
	zlog.Info("stopping sbx sandbox by name", zap.String("name", name))

	cmd := exec.Command("sbx", "stop", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sbx stop failed: %w (stderr: %s)", err, stderr.String())
	}

	zlog.Info("sbx sandbox stopped", zap.String("name", name))
	return nil
}

// RemoveSbxSandboxByName removes an sbx sandbox by name.
func RemoveSbxSandboxByName(name string) error {
	zlog.Info("removing sbx sandbox by name", zap.String("name", name))

	cmd := exec.Command("sbx", "rm", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sbx rm failed: %w (stderr: %s)", err, stderr.String())
	}

	zlog.Info("sbx sandbox removed", zap.String("name", name))
	return nil
}

// RemoveSbxSandbox removes an sbx sandbox by ID.
func RemoveSbxSandbox(sandboxID string) error {
	return RemoveSbxSandboxByName(sandboxID)
}
