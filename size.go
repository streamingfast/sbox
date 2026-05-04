package sbox

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// DiskSizeInfo holds disk usage information for a container or sandbox.
type DiskSizeInfo struct {
	// ContainerSize is the size of the container's writable layer in bytes (-1 if unknown).
	ContainerSize int64

	// ImageSize is the size of the image (read-only layers) in bytes (-1 if unknown).
	// This is shared across all containers using the same image; it is informational only.
	ImageSize int64

	// VolumeSize is the size of the associated named volume in bytes (-1 if unknown).
	VolumeSize int64
}

// HasAnySize returns true if at least one size field is known (>= 0).
func (d DiskSizeInfo) HasAnySize() bool {
	return d.ContainerSize >= 0 || d.VolumeSize >= 0
}

// Total returns the total of known sizes (container writable layer + volume).
// This is the most useful number: how much unique disk space this project is using.
func (d DiskSizeInfo) Total() int64 {
	total := int64(0)
	if d.ContainerSize >= 0 {
		total += d.ContainerSize
	}
	if d.VolumeSize >= 0 {
		total += d.VolumeSize
	}
	return total
}

// FormatBytes formats a byte count into a human-readable string using SI units (KB, MB, GB).
// Returns "unknown" for negative values.
func FormatBytes(bytes int64) string {
	if bytes < 0 {
		return "unknown"
	}
	const (
		KB = 1000
		MB = 1000 * 1000
		GB = 1000 * 1000 * 1000
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.0f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.0f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// GetContainerDiskSize returns disk size information for a Docker container.
// Uses `docker inspect --size <containerID>` to get SizeRootFs and SizeRw.
// Returns a DiskSizeInfo with -1 for fields that could not be determined.
func GetContainerDiskSize(containerID string) DiskSizeInfo {
	result := DiskSizeInfo{ContainerSize: -1, ImageSize: -1, VolumeSize: -1}

	if containerID == "" {
		return result
	}

	cmd := exec.Command("docker", "inspect", "--size", containerID, "--format", "{{.SizeRootFs}} {{.SizeRw}}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		zlog.Debug("failed to get container disk size", zap.String("container", containerID), zap.Error(err))
		return result
	}

	parts := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(parts) >= 2 {
		if rootFs, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			result.ImageSize = rootFs
		}
		if rw, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			result.ContainerSize = rw
		}
	}

	return result
}

// GetVolumeDiskSize returns the disk size of a named Docker volume in bytes.
// Uses `docker system df -v` to find the volume size.
// Returns -1 if the volume size cannot be determined.
func GetVolumeDiskSize(volumeName string) int64 {
	if volumeName == "" {
		return -1
	}

	cmd := exec.Command("docker", "system", "df", "-v", "--format", "{{range .Volumes}}{{.Name}} {{.Size}}\n{{end}}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// Try fallback: parse table output
		return getVolumeSizeFallback(volumeName)
	}

	output := stdout.String()
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == volumeName {
			return parseDockerSize(parts[1])
		}
	}

	return getVolumeSizeFallback(volumeName)
}

// getVolumeSizeFallback uses table output from `docker system df -v` to find volume size.
func getVolumeSizeFallback(volumeName string) int64 {
	cmd := exec.Command("docker", "system", "df", "-v")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		zlog.Debug("failed to run docker system df -v", zap.Error(err))
		return -1
	}

	// Parse the "Local Volumes space usage:" section.
	inVolumesSection := false
	var headerLine string
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if strings.Contains(line, "Local Volumes space usage") {
			inVolumesSection = true
			continue
		}
		if inVolumesSection {
			if headerLine == "" {
				if strings.HasPrefix(line, "VOLUME") {
					headerLine = line
				}
				continue
			}
			if line == "" {
				break
			}
			// Simple approach: first field is volume name
			parts := strings.Fields(line)
			if len(parts) >= 3 && parts[0] == volumeName {
				// SIZE is usually the 3rd column (VOLUME NAME, LINKS, SIZE)
				return parseDockerSize(parts[2])
			}
		}
	}

	return -1
}

// GetSandboxDiskSize returns the disk size of a Docker sandbox in bytes.
// Tries `docker sandbox inspect` for size information.
// Returns -1 if the size cannot be determined.
func GetSandboxDiskSize(sandboxName string) int64 {
	if sandboxName == "" {
		return -1
	}

	// Try docker sandbox inspect with a size format.
	// The actual field name may vary; we try common candidates.
	for _, field := range []string{"{{.Size}}", "{{.DiskSize}}", "{{.SizeOnDisk}}"} {
		cmd := exec.Command("docker", "sandbox", "inspect", sandboxName, "--format", field)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			continue
		}
		val := strings.TrimSpace(stdout.String())
		if val != "" && val != "<no value>" {
			if size, err := strconv.ParseInt(val, 10, 64); err == nil && size > 0 {
				return size
			}
			// Also try as a human-readable string
			if size := parseDockerSize(val); size > 0 {
				return size
			}
		}
	}

	zlog.Debug("docker sandbox inspect did not return size", zap.String("sandbox", sandboxName))
	return -1
}

// GetSbxSandboxDiskSize returns the disk size of an sbx sandbox in bytes.
// Tries `sbx inspect` for size information. Returns -1 if the size cannot be determined.
func GetSbxSandboxDiskSize(sandboxName string) int64 {
	if sandboxName == "" {
		return -1
	}

	for _, field := range []string{"{{.Size}}", "{{.DiskSize}}", "{{.SizeOnDisk}}"} {
		cmd := exec.Command("sbx", "inspect", sandboxName, "--format", field)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			continue
		}
		val := strings.TrimSpace(stdout.String())
		if val != "" && val != "<no value>" {
			if size, err := strconv.ParseInt(val, 10, 64); err == nil && size > 0 {
				return size
			}
			if size := parseDockerSize(val); size > 0 {
				return size
			}
		}
	}

	zlog.Debug("sbx inspect did not return size", zap.String("sandbox", sandboxName))
	return -1
}
// or "" if none is found. This is a public wrapper around the internal getContainerVolumeName.
func GetContainerVolumeNameByID(containerID string) string {
	return getContainerVolumeName(containerID)
}

// parseDockerSize parses a Docker size string like "1.23GB", "456MB", "12KB", "789B"
// and returns the byte count. Returns -1 on parse failure.
func parseDockerSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "0B" || s == "0" {
		return 0
	}

	// Handle Docker's "B" suffix for bytes
	multipliers := []struct {
		suffix string
		factor float64
	}{
		{"TB", 1e12},
		{"GB", 1e9},
		{"MB", 1e6},
		{"KB", 1e3},
		{"B", 1},
	}

	sUpper := strings.ToUpper(s)
	for _, m := range multipliers {
		if strings.HasSuffix(sUpper, m.suffix) {
			numStr := s[:len(s)-len(m.suffix)]
			val, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
			if err != nil {
				return -1
			}
			return int64(val * m.factor)
		}
	}

	// Try plain integer
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return -1
	}
	return val
}
