package sbox

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// DiscoverMDFiles walks up the directory tree from startDir to find all
// CLAUDE.md and AGENTS.md files. Returns paths in order from root to startDir.
func DiscoverMDFiles(startDir string) ([]string, error) {
	// Convert to absolute path
	absPath, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	var foundFiles []string
	currentDir := absPath

	// Walk up directory tree until we hit root
	for {
		// Check for CLAUDE.md
		claudePath := filepath.Join(currentDir, "CLAUDE.md")
		if _, err := os.Stat(claudePath); err == nil {
			foundFiles = append(foundFiles, claudePath)
			zlog.Debug("found CLAUDE.md", zap.String("path", claudePath))
		}

		// Check for AGENTS.md
		agentsPath := filepath.Join(currentDir, "AGENTS.md")
		if _, err := os.Stat(agentsPath); err == nil {
			foundFiles = append(foundFiles, agentsPath)
			zlog.Debug("found AGENTS.md", zap.String("path", agentsPath))
		}

		// Move to parent directory
		parentDir := filepath.Dir(currentDir)

		// Stop if we've reached the root or can't go higher
		if parentDir == currentDir || parentDir == "." || parentDir == "/" {
			break
		}

		currentDir = parentDir
	}

	// Reverse the list so files are ordered from root to startDir
	for i, j := 0, len(foundFiles)-1; i < j; i, j = i+1, j-1 {
		foundFiles[i], foundFiles[j] = foundFiles[j], foundFiles[i]
	}

	zlog.Info("discovered MD files",
		zap.String("start_dir", absPath),
		zap.Int("count", len(foundFiles)))

	return foundFiles, nil
}

// ConcatenateMDFiles reads and concatenates the specified MD files with
// delimiters showing the source path of each file. The embedded backend-specific
// context instructions are prepended to help Claude understand its environment.
func ConcatenateMDFiles(files []string, backend BackendType) (string, error) {
	var sb strings.Builder

	// Get backend-specific context
	contextMD := GetBackendContextMD(backend)

	// Prepend embedded backend context instructions
	sb.WriteString("# ==================================================\n")
	sb.WriteString(fmt.Sprintf("# Source: sbox (embedded %s backend instructions)\n", backend))
	sb.WriteString("# ==================================================\n\n")
	sb.WriteString(contextMD)
	if len(contextMD) > 0 && contextMD[len(contextMD)-1] != '\n' {
		sb.WriteString("\n")
	}

	zlog.Debug("prepended backend context", zap.String("backend", string(backend)), zap.Int("bytes", len(contextMD)))

	if len(files) == 0 {
		return sb.String(), nil
	}

	for _, filePath := range files {
		// Read file content
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read %s: %w", filePath, err)
		}

		// Add separator and source path
		sb.WriteString("\n\n")
		sb.WriteString("# ==================================================\n")
		sb.WriteString(fmt.Sprintf("# Source: %s\n", filePath))
		sb.WriteString("# ==================================================\n\n")

		// Add content
		sb.Write(content)

		// Ensure content ends with newline
		if len(content) > 0 && content[len(content)-1] != '\n' {
			sb.WriteString("\n")
		}

		zlog.Debug("concatenated MD file",
			zap.String("path", filePath),
			zap.Int("bytes", len(content)))
	}

	result := sb.String()
	zlog.Info("concatenated MD files",
		zap.Int("file_count", len(files)),
		zap.Int("total_bytes", len(result)))

	return result, nil
}

// ProjectHash computes a unique short hash for a project based on its absolute path.
// Returns a URL-safe base64 encoded hash (12 chars) suitable for directory names.
func ProjectHash(workspaceDir string) (string, error) {
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	hash := sha256.Sum256([]byte(absPath))
	// Use URL-safe base64 encoding and take first 12 characters
	encoded := base64.URLEncoding.EncodeToString(hash[:])
	return encoded[:12], nil
}

// PrepareMDForSandbox discovers all CLAUDE.md and AGENTS.md files in the
// workspace directory hierarchy, concatenates them with backend-specific context,
// and writes the result to a per-project location: ~/.config/sbox/projects/<hash>/claude.md
func PrepareMDForSandbox(workspaceDir string, backend BackendType) (string, error) {
	// Compute project hash
	projectHash, err := ProjectHash(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("failed to compute project hash: %w", err)
	}

	// Discover MD files
	files, err := DiscoverMDFiles(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("failed to discover MD files: %w", err)
	}

	// Concatenate files with backend-specific context
	content, err := ConcatenateMDFiles(files, backend)
	if err != nil {
		return "", fmt.Errorf("failed to concatenate MD files: %w", err)
	}

	// Load config to get sbox data directory
	config, err := LoadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	// Write to per-project location: ~/.config/sbox/projects/<hash>/claude.md
	projectDir := filepath.Join(config.SboxDataDir, "projects", projectHash)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create project directory: %w", err)
	}

	outputPath := filepath.Join(projectDir, "claude.md")
	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write concatenated MD file: %w", err)
	}

	zlog.Info("prepared MD file for sandbox",
		zap.String("workspace", workspaceDir),
		zap.String("project_hash", projectHash),
		zap.String("output_path", outputPath),
		zap.Int("source_files", len(files)),
		zap.Int("bytes", len(content)))

	return outputPath, nil
}
