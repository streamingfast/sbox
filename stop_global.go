package sbox

import (
	"bytes"
	"fmt"
	"os/exec"

	"go.uber.org/zap"
)

// StopOptions controls which sandboxes/containers are selected for stopping.
type StopOptions struct {
	// Keep is the number of most-recently-used running entries to keep running.
	// Defaults to 3 when zero.
	Keep int
}

// FindSandboxStopCandidates returns running sandboxes that should be stopped according
// to opts, along with the running sandboxes that are being kept. Only considers
// sandboxes with status "running".
//
// The selection algorithm:
//  1. Load all known projects from ~/.config/sbox/projects/.
//  2. Load all Docker sandboxes from `docker sandbox ls`.
//  3. Filter to only running sandboxes.
//  4. Keep the opts.Keep most recently used running; the rest become candidates to stop.
//  5. Orphaned running sandboxes (no project entry, name starts with "sbox-") are candidates.
//
// Returns (candidates, kept, err).
func FindSandboxStopCandidates(opts StopOptions) (candidates []PruneCandidate, kept []PruneCandidate, err error) {
	keep := opts.Keep
	if keep <= 0 {
		keep = 3
	}

	// Re-use FindPruneCandidates with a very large keep value to get the full candidate pool,
	// including orphans. We then re-filter by running status.
	allCandidates, allKept, err := FindPruneCandidates(PruneOptions{Keep: 1 << 30})
	if err != nil {
		return nil, nil, err
	}

	// Load Docker sandbox status map.
	dockerSandboxes, err := ListDockerSandboxes()
	if err != nil {
		zlog.Warn("failed to list docker sandboxes for stop candidates", zap.Error(err))
		dockerSandboxes = nil
	}

	sandboxStatus := make(map[string]string, len(dockerSandboxes))
	for _, ds := range dockerSandboxes {
		sandboxStatus[ds.Name] = ds.Status
	}

	// Build a combined ordered list (from kept, ordered most-recently-used first) of
	// entries that have a running sandbox. allKept is already sorted most-recent first.
	// allCandidates has stale first then old — we want to consider all active+running.
	type entry struct {
		candidate PruneCandidate
		isRunning bool
	}

	// Collect all entries, keeping only those with a running sandbox.
	var runningEntries []PruneCandidate
	// allKept is sorted most-recent first — these are the "active" ones.
	for _, c := range allKept {
		if c.SandboxName != "" && sandboxStatus[c.SandboxName] == "running" {
			runningEntries = append(runningEntries, c)
		}
	}
	// Also include candidates that might be running (e.g., orphans).
	for _, c := range allCandidates {
		if c.SandboxName != "" && sandboxStatus[c.SandboxName] == "running" {
			runningEntries = append(runningEntries, c)
		}
	}

	// Keep the first `keep` entries running; the rest are stop candidates.
	for i, c := range runningEntries {
		if i < keep {
			kept = append(kept, c)
		} else {
			candidates = append(candidates, c)
		}
	}

	return candidates, kept, nil
}

// FindContainerStopCandidates returns running containers that should be stopped
// according to opts, along with the running containers that are being kept.
//
// The selection algorithm:
//  1. List all Docker containers whose name starts with "sbox-" via ContainerBackend.List().
//  2. Filter to only running containers.
//  3. Keep the opts.Keep most recently used running; the rest become candidates to stop.
//
// Returns (candidates, kept, err).
func FindContainerStopCandidates(opts StopOptions) (candidates []ContainerPruneCandidate, kept []ContainerPruneCandidate, err error) {
	keep := opts.Keep
	if keep <= 0 {
		keep = 3
	}

	// Get all container candidates (with a huge keep to get all active).
	allCandidates, allKept, err := FindContainerPruneCandidates(PruneOptions{Keep: 1 << 30})
	if err != nil {
		return nil, nil, err
	}

	// Collect running containers. allKept is sorted most-recent first.
	var runningEntries []ContainerPruneCandidate
	for _, c := range allKept {
		if c.Status == "running" {
			runningEntries = append(runningEntries, c)
		}
	}
	for _, c := range allCandidates {
		if c.Status == "running" {
			runningEntries = append(runningEntries, c)
		}
	}

	// Keep the first `keep` entries running; the rest are stop candidates.
	for i, c := range runningEntries {
		if i < keep {
			kept = append(kept, c)
		} else {
			candidates = append(candidates, c)
		}
	}

	return candidates, kept, nil
}

// StopOneSandboxCandidate stops the Docker sandbox for a single PruneCandidate.
// It does NOT remove the sandbox or its project data — only stops it.
func StopOneSandboxCandidate(c PruneCandidate) error {
	if c.SandboxName == "" {
		return fmt.Errorf("no sandbox name for candidate (workspace: %s)", c.WorkspacePath)
	}
	return StopDockerSandboxByName(c.SandboxName)
}

// StopOneContainerCandidate stops a Docker container without removing it.
func StopOneContainerCandidate(c ContainerPruneCandidate) error {
	if c.ContainerID == "" {
		return fmt.Errorf("no container ID for candidate (name: %s)", c.ContainerName)
	}

	zlog.Info("stopping container",
		zap.String("container", c.ContainerName),
		zap.String("id", c.ContainerID))

	var stderr bytes.Buffer
	cmd := exec.Command("docker", "stop", c.ContainerID)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker stop %q: %w (stderr: %s)", c.ContainerName, err, stderr.String())
	}

	zlog.Info("container stopped",
		zap.String("container", c.ContainerName))
	return nil
}

// FindSbxStopCandidates returns running sbx sandboxes that should be stopped according
// to opts, along with the running sandboxes that are being kept.
//
// Returns (candidates, kept, err).
func FindSbxStopCandidates(opts StopOptions) (candidates []PruneCandidate, kept []PruneCandidate, err error) {
	keep := opts.Keep
	if keep <= 0 {
		keep = 3
	}

	allCandidates, allKept, err := FindSbxPruneCandidates(PruneOptions{Keep: 1 << 30})
	if err != nil {
		return nil, nil, err
	}

	sbxSandboxes, err := ListSbxSandboxes()
	if err != nil {
		zlog.Warn("failed to list sbx sandboxes for stop candidates", zap.Error(err))
		sbxSandboxes = nil
	}

	sandboxStatus := make(map[string]string, len(sbxSandboxes))
	for _, sb := range sbxSandboxes {
		sandboxStatus[sb.Name] = sb.Status
	}

	var runningEntries []PruneCandidate
	for _, c := range allKept {
		if c.SandboxName != "" && sandboxStatus[c.SandboxName] == "running" {
			runningEntries = append(runningEntries, c)
		}
	}
	for _, c := range allCandidates {
		if c.SandboxName != "" && sandboxStatus[c.SandboxName] == "running" {
			runningEntries = append(runningEntries, c)
		}
	}

	for i, c := range runningEntries {
		if i < keep {
			kept = append(kept, c)
		} else {
			candidates = append(candidates, c)
		}
	}

	return candidates, kept, nil
}

// StopOneSbxCandidate stops the sbx sandbox for a single PruneCandidate.
// It does NOT remove the sandbox or its project data — only stops it.
func StopOneSbxCandidate(c PruneCandidate) error {
	if c.SandboxName == "" {
		return fmt.Errorf("no sandbox name for sbx candidate (workspace: %s)", c.WorkspacePath)
	}
	return StopSbxSandboxByName(c.SandboxName)
}
