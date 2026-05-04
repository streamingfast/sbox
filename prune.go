package sbox

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/destel/rill"
	"go.uber.org/zap"
)

// lastUsedFile is the name of the file written inside .sbox/ to track last-used time.
const lastUsedFile = "last-used"

// WriteLastUsed writes the current UTC time as an RFC3339 timestamp to
// <workspaceDir>/.sbox/last-used. The .sbox directory must already exist.
func WriteLastUsed(workspaceDir string) error {
	sboxDir := filepath.Join(workspaceDir, ".sbox")
	path := filepath.Join(sboxDir, lastUsedFile)
	content := time.Now().UTC().Format(time.RFC3339) + "\n"

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write last-used file: %w", err)
	}

	zlog.Debug("wrote last-used timestamp",
		zap.String("path", path),
		zap.String("timestamp", strings.TrimSpace(content)))
	return nil
}

// ReadLastUsed reads the last-used timestamp from <workspaceDir>/.sbox/last-used.
// Returns the zero value of time.Time if the file does not exist or cannot be parsed.
func ReadLastUsed(workspaceDir string) (time.Time, error) {
	path := filepath.Join(workspaceDir, ".sbox", lastUsedFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("failed to read last-used file: %w", err)
	}

	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse last-used timestamp %q: %w", strings.TrimSpace(string(data)), err)
	}

	return ts, nil
}

// PruneCandidate describes a sandbox that is a candidate for pruning.
type PruneCandidate struct {
	// SandboxName is the Docker sandbox name (may be empty for project-only orphans).
	SandboxName string

	// WorkspacePath is the absolute path to the workspace.
	WorkspacePath string

	// ProjectHash is the sbox project hash (used to remove project config).
	ProjectHash string

	// LastUsed is the last-used timestamp (zero if unknown).
	LastUsed time.Time

	// Reason is a human-readable explanation for why this is a candidate.
	Reason string

	// WorkspaceMissing is true when the workspace directory no longer exists.
	WorkspaceMissing bool
}

// PruneOptions controls which sandboxes are selected for pruning.
type PruneOptions struct {
	// Keep is the number of most-recently-used sandboxes to keep.
	// Defaults to 5 when zero.
	Keep int
}

// PruneError records an error that occurred while pruning a specific candidate.
type PruneError struct {
	Candidate PruneCandidate
	Err       error
}

func (e PruneError) Error() string {
	return fmt.Sprintf("prune %s: %v", e.Candidate.SandboxName, e.Err)
}

// FindPruneCandidates returns sandboxes that should be pruned according to opts,
// along with the sandboxes that are being kept.
//
// The selection algorithm:
//  1. Load all known projects from ~/.config/sbox/projects/.
//  2. Load all Docker sandboxes from `docker sandbox ls`.
//  3. Workspaces that no longer exist on disk are always candidates (stale).
//  4. Among workspaces that still exist, keep the opts.Keep most recently used;
//     the rest become candidates.
//  5. Docker sandboxes with no corresponding project entry are also candidates.
//
// Returns (candidates, kept, err). candidates are sandboxes to prune; kept are
// sandboxes being retained.
func FindPruneCandidates(opts PruneOptions) (candidates []PruneCandidate, kept []PruneCandidate, err error) {
	keep := opts.Keep
	if keep <= 0 {
		keep = 5
	}

	// Load all known projects.
	projects, err := ListProjects()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list projects: %w", err)
	}

	// Load all Docker sandboxes.
	dockerSandboxes, err := ListDockerSandboxes()
	if err != nil {
		// Non-fatal: docker may not be available, or there are simply no sandboxes.
		zlog.Warn("failed to list docker sandboxes (continuing without sandbox list)", zap.Error(err))
		dockerSandboxes = nil
	}

	// Build a lookup map: workspace path -> DockerSandbox.
	sandboxByWorkspace := make(map[string]DockerSandbox, len(dockerSandboxes))
	sandboxByName := make(map[string]DockerSandbox, len(dockerSandboxes))
	for _, ds := range dockerSandboxes {
		if ds.Workspace != "" {
			sandboxByWorkspace[ds.Workspace] = ds
		}
		sandboxByName[ds.Name] = ds
	}

	// Track which docker sandboxes are accounted for by a project entry.
	accountedSandboxNames := make(map[string]bool)

	type projectEntry struct {
		info     ProjectInfo
		lastUsed time.Time
		sandbox  DockerSandbox
		hasSb    bool
	}

	// projectResult holds the outcome of inspecting a single project.
	type projectResult struct {
		staleCandidate *PruneCandidate  // non-nil if workspace is missing
		activeEntry    *projectEntry    // non-nil if workspace exists
		sandboxName    string           // sandbox name to mark as accounted for
	}

	// Inspect projects concurrently (2×CPU goroutines) — stat calls can be slow on
	// network-mounted or remote filesystems.
	concurrency := 2 * runtime.NumCPU()
	projectStream := rill.FromSlice(projects, nil)
	resultStream := rill.Map(projectStream, concurrency, func(proj ProjectInfo) (projectResult, error) {
		ws := proj.WorkspacePath
		if ws == "" {
			return projectResult{}, nil
		}

		// Find associated Docker sandbox (read-only map access — safe without lock).
		sb, hasSb := sandboxByWorkspace[ws]
		if !hasSb && proj.Config != nil && proj.Config.SandboxName != "" {
			if candidate, ok := sandboxByName[proj.Config.SandboxName]; ok && candidate.Workspace == ws {
				// Only claim the sandbox by name when its recorded workspace still
				// matches this project. If it differs, the sandbox name was reused
				// for a new workspace and we must not touch it.
				sb, hasSb = candidate, true
			}
		}

		res := projectResult{}
		if hasSb {
			res.sandboxName = sb.Name
		}

		// Check if workspace still exists.
		if _, statErr := os.Stat(ws); os.IsNotExist(statErr) {
			sbName := ""
			if hasSb {
				sbName = sb.Name
			} else if proj.Config != nil {
				sbName = proj.Config.SandboxName
			}
			res.staleCandidate = &PruneCandidate{
				SandboxName:      sbName,
				WorkspacePath:    ws,
				ProjectHash:      proj.Hash,
				Reason:           "workspace directory no longer exists",
				WorkspaceMissing: true,
			}
			return res, nil
		}

		// Read last-used timestamp.
		lastUsed, readErr := ReadLastUsed(ws)
		if readErr != nil {
			zlog.Warn("failed to read last-used timestamp, treating as zero",
				zap.String("workspace", ws),
				zap.Error(readErr))
		}

		res.activeEntry = &projectEntry{
			info:     proj,
			lastUsed: lastUsed,
			sandbox:  sb,
			hasSb:    hasSb,
		}
		return res, nil
	})

	var stale []PruneCandidate
	var active []projectEntry

	for res, err := range rill.ToSeq2(resultStream) {
		if err != nil {
			zlog.Warn("error inspecting project (skipping)", zap.Error(err))
			continue
		}
		if res.sandboxName != "" {
			accountedSandboxNames[res.sandboxName] = true
		}
		if res.staleCandidate != nil {
			stale = append(stale, *res.staleCandidate)
		} else if res.activeEntry != nil {
			active = append(active, *res.activeEntry)
		}
	}

	// Sort active entries by last-used descending (most recent first).
	slices.SortFunc(active, func(a, b projectEntry) int {
		// Entries with zero time sort to the end (oldest).
		aZero := a.lastUsed.IsZero()
		bZero := b.lastUsed.IsZero()
		switch {
		case aZero && bZero:
			return 0
		case aZero:
			return 1
		case bZero:
			return -1
		}
		if a.lastUsed.After(b.lastUsed) {
			return -1
		}
		if b.lastUsed.After(a.lastUsed) {
			return 1
		}
		return 0
	})

	// Keep the first `keep` entries; the rest are candidates.
	var old []PruneCandidate
	var keptEntries []PruneCandidate
	for i, entry := range active {
		sbName := ""
		if entry.hasSb {
			sbName = entry.sandbox.Name
		} else if entry.info.Config != nil {
			sbName = entry.info.Config.SandboxName
		}

		if i < keep {
			keptEntries = append(keptEntries, PruneCandidate{
				SandboxName:   sbName,
				WorkspacePath: entry.info.WorkspacePath,
				ProjectHash:   entry.info.Hash,
				LastUsed:      entry.lastUsed,
			})
			continue // keep this one
		}

		reason := fmt.Sprintf("outside keep=%d most recently used", keep)
		old = append(old, PruneCandidate{
			SandboxName:   sbName,
			WorkspacePath: entry.info.WorkspacePath,
			ProjectHash:   entry.info.Hash,
			LastUsed:      entry.lastUsed,
			Reason:        reason,
		})
	}

	// Also include Docker sandboxes with no project entry at all.
	// Only consider sandboxes whose name starts with "sbox-" to avoid touching
	// sandboxes created by unrelated tools that also use docker sandbox.
	for _, ds := range dockerSandboxes {
		if !strings.HasPrefix(ds.Name, "sbox-") {
			continue
		}
		if accountedSandboxNames[ds.Name] {
			continue
		}
		// Orphaned sandbox: no project entry.
		wsMissing := ds.Workspace != ""
		if wsMissing {
			_, statErr := os.Stat(ds.Workspace)
			wsMissing = os.IsNotExist(statErr)
		}

		reason := "no project entry found for sandbox"
		if wsMissing {
			reason = "no project entry found and workspace directory no longer exists"
		}

		old = append(old, PruneCandidate{
			SandboxName:      ds.Name,
			WorkspacePath:    ds.Workspace,
			Reason:           reason,
			WorkspaceMissing: wsMissing,
		})
	}

	// Combine: stale first, then old (sorted oldest-last-used first).
	all := append(stale, old...)
	return all, keptEntries, nil
}

// ContainerPruneCandidate describes a Docker container that is a candidate for pruning.
type ContainerPruneCandidate struct {
	// ContainerID is the Docker container ID.
	ContainerID string

	// ContainerName is the Docker container name (e.g. "sbox-claude-myproject").
	ContainerName string

	// WorkspacePath is the absolute path to the workspace directory.
	WorkspacePath string

	// LastUsed is the last-used timestamp (zero if unknown).
	LastUsed time.Time

	// Status is the container state (e.g. "running", "exited").
	Status string

	// VolumeName is the first sbox- named volume associated with the container, or "".
	VolumeName string

	// WorkspaceMissing is true when the workspace directory no longer exists.
	WorkspaceMissing bool
}

// FindContainerPruneCandidates returns Docker containers that should be pruned
// according to opts, along with the containers that are being kept.
//
// The selection algorithm:
//  1. List all Docker containers whose name starts with "sbox-" via ContainerBackend.List().
//  2. Containers with no workspace path or whose workspace no longer exists on disk
//     are always candidates (stale).
//  3. Among containers with existing workspaces, keep the opts.Keep most recently used;
//     the rest become candidates.
//
// Returns (candidates, kept, err).
func FindContainerPruneCandidates(opts PruneOptions) (candidates []ContainerPruneCandidate, kept []ContainerPruneCandidate, err error) {
	keep := opts.Keep
	if keep <= 0 {
		keep = 5
	}

	// Build a set of MicroVM sandbox names so we don't treat their underlying
	// Docker containers as regular containers to prune.
	sandboxNames := make(map[string]bool)
	if sandboxes, lsErr := ListDockerSandboxes(); lsErr == nil {
		for _, sb := range sandboxes {
			if sb.Name != "" {
				sandboxNames[sb.Name] = true
			}
		}
	}

	backend := NewContainerBackend(nil)
	containers, err := backend.List()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var stale []ContainerPruneCandidate
	var active []ContainerPruneCandidate

	for _, info := range containers {
		// Skip containers that are MicroVM sandboxes — those are handled by
		// FindPruneCandidates (the sandbox pruner), not here.
		if sandboxNames[info.Name] {
			continue
		}
		c := ContainerPruneCandidate{
			ContainerID:   info.ID,
			ContainerName: info.Name,
			WorkspacePath: info.Workspace,
			Status:        info.Status,
		}

		if info.Workspace == "" {
			c.WorkspaceMissing = true
			stale = append(stale, c)
			continue
		}

		if _, statErr := os.Stat(info.Workspace); os.IsNotExist(statErr) {
			c.WorkspaceMissing = true
			stale = append(stale, c)
			continue
		}

		// Workspace exists — read last-used timestamp.
		lastUsed, readErr := ReadLastUsed(info.Workspace)
		if readErr != nil {
			zlog.Warn("failed to read last-used timestamp for container, treating as zero",
				zap.String("container", info.Name),
				zap.String("workspace", info.Workspace),
				zap.Error(readErr))
		}
		c.LastUsed = lastUsed
		active = append(active, c)
	}

	// Sort active containers by last-used descending (most recent first).
	slices.SortFunc(active, func(a, b ContainerPruneCandidate) int {
		aZero := a.LastUsed.IsZero()
		bZero := b.LastUsed.IsZero()
		switch {
		case aZero && bZero:
			return 0
		case aZero:
			return 1
		case bZero:
			return -1
		}
		if a.LastUsed.After(b.LastUsed) {
			return -1
		}
		if b.LastUsed.After(a.LastUsed) {
			return 1
		}
		return 0
	})

	// Keep the first `keep` entries; the rest are candidates.
	var old []ContainerPruneCandidate
	var keptEntries []ContainerPruneCandidate
	for i, c := range active {
		if i < keep {
			keptEntries = append(keptEntries, c)
		} else {
			old = append(old, c)
		}
	}

	// Inspect candidate and stale containers for their volume names.
	for i := range stale {
		stale[i].VolumeName = getContainerVolumeName(stale[i].ContainerID)
	}
	for i := range old {
		old[i].VolumeName = getContainerVolumeName(old[i].ContainerID)
	}

	all := append(stale, old...)
	return all, keptEntries, nil
}

// PruneOneContainer stops and removes a Docker container and its associated named volume.
// Errors from individual steps are collected; the first error encountered is returned
// but later steps still run regardless.
func PruneOneContainer(c ContainerPruneCandidate) error {
	var firstErr error

	setErr := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

	// 1. Stop container if running.
	if c.Status == "running" {
		stopCmd := exec.Command("docker", "stop", c.ContainerID)
		if err := stopCmd.Run(); err != nil {
			zlog.Warn("failed to stop container (may already be stopped)",
				zap.String("container", c.ContainerName),
				zap.Error(err))
			// Not setting firstErr here — stop failure is non-fatal if rm succeeds.
		}
	}

	// 2. Remove container.
	var rmStderr bytes.Buffer
	rmCmd := exec.Command("docker", "rm", c.ContainerID)
	rmCmd.Stderr = &rmStderr
	if err := rmCmd.Run(); err != nil {
		zlog.Warn("failed to remove container",
			zap.String("container", c.ContainerName),
			zap.Error(err))
		setErr(fmt.Errorf("docker rm %q: %w (stderr: %s)", c.ContainerName, err, rmStderr.String()))
	}

	// 3. Remove associated named volume if present.
	if c.VolumeName != "" {
		var volStderr bytes.Buffer
		volCmd := exec.Command("docker", "volume", "rm", c.VolumeName)
		volCmd.Stderr = &volStderr
		if err := volCmd.Run(); err != nil {
			zlog.Warn("failed to remove container volume",
				zap.String("volume", c.VolumeName),
				zap.Error(err))
			setErr(fmt.Errorf("docker volume rm %q: %w (stderr: %s)", c.VolumeName, err, volStderr.String()))
		}
	}

	return firstErr
}

// getContainerVolumeName inspects a container and returns the first named volume
// whose name starts with "sbox-", or "" if none is found.
func getContainerVolumeName(containerID string) string {
	cmd := exec.Command("docker", "inspect", containerID, "--format", "{{range .Mounts}}{{if eq .Type \"volume\"}}{{.Name}}\n{{end}}{{end}}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return ""
	}

	for line := range strings.SplitSeq(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "sbox-") {
			return line
		}
	}
	return ""
}

// FindSbxPruneCandidates returns sbx sandboxes that should be pruned according to opts,
// along with the sandboxes that are being kept.
//
// The selection algorithm mirrors FindPruneCandidates but uses `sbx ls` instead of
// `docker sandbox ls`. Orphan detection uses project config cross-referencing (sbx
// sandbox names have no common prefix to filter on).
//
// FIXME: sbx stores microVMs in a different folder than docker sandbox.
// Dangling sandbox detection via folder scan is not yet implemented.
//
// Returns (candidates, kept, err).
func FindSbxPruneCandidates(opts PruneOptions) (candidates []PruneCandidate, kept []PruneCandidate, err error) {
	keep := opts.Keep
	if keep <= 0 {
		keep = 5
	}

	projects, err := ListProjects()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list projects: %w", err)
	}

	sbxSandboxes, err := ListSbxSandboxes()
	if err != nil {
		zlog.Warn("failed to list sbx sandboxes (continuing without sandbox list)", zap.Error(err))
		sbxSandboxes = nil
	}

	sandboxByWorkspace := make(map[string]SbxSandbox, len(sbxSandboxes))
	sandboxByName := make(map[string]SbxSandbox, len(sbxSandboxes))
	for _, sb := range sbxSandboxes {
		if sb.Workspace != "" {
			sandboxByWorkspace[sb.Workspace] = sb
		}
		sandboxByName[sb.Name] = sb
	}

	accountedSandboxNames := make(map[string]bool)

	type projectEntry struct {
		info     ProjectInfo
		lastUsed time.Time
		sandbox  SbxSandbox
		hasSb    bool
	}

	type projectResult struct {
		staleCandidate *PruneCandidate
		activeEntry    *projectEntry
		sandboxName    string
	}

	concurrency := 2 * runtime.NumCPU()
	projectStream := rill.FromSlice(projects, nil)
	resultStream := rill.Map(projectStream, concurrency, func(proj ProjectInfo) (projectResult, error) {
		ws := proj.WorkspacePath
		if ws == "" {
			return projectResult{}, nil
		}

		// Only consider projects using the sbx backend.
		if proj.Config == nil || proj.Config.Backend != string(BackendSbx) {
			return projectResult{}, nil
		}

		sb, hasSb := sandboxByWorkspace[ws]
		if !hasSb && proj.Config != nil && proj.Config.SandboxName != "" {
			if candidate, ok := sandboxByName[proj.Config.SandboxName]; ok && candidate.Workspace == ws {
				sb, hasSb = candidate, true
			}
		}

		res := projectResult{}
		if hasSb {
			res.sandboxName = sb.Name
		}

		if _, statErr := os.Stat(ws); os.IsNotExist(statErr) {
			sbName := ""
			if hasSb {
				sbName = sb.Name
			} else if proj.Config != nil {
				sbName = proj.Config.SandboxName
			}
			res.staleCandidate = &PruneCandidate{
				SandboxName:      sbName,
				WorkspacePath:    ws,
				ProjectHash:      proj.Hash,
				Reason:           "workspace directory no longer exists",
				WorkspaceMissing: true,
			}
			return res, nil
		}

		lastUsed, readErr := ReadLastUsed(ws)
		if readErr != nil {
			zlog.Warn("failed to read last-used timestamp, treating as zero",
				zap.String("workspace", ws),
				zap.Error(readErr))
		}

		res.activeEntry = &projectEntry{
			info:     proj,
			lastUsed: lastUsed,
			sandbox:  sb,
			hasSb:    hasSb,
		}
		return res, nil
	})

	var stale []PruneCandidate
	var active []projectEntry

	for res, err := range rill.ToSeq2(resultStream) {
		if err != nil {
			zlog.Warn("error inspecting project (skipping)", zap.Error(err))
			continue
		}
		if res.sandboxName != "" {
			accountedSandboxNames[res.sandboxName] = true
		}
		if res.staleCandidate != nil {
			stale = append(stale, *res.staleCandidate)
		} else if res.activeEntry != nil {
			active = append(active, *res.activeEntry)
		}
	}

	slices.SortFunc(active, func(a, b projectEntry) int {
		aZero := a.lastUsed.IsZero()
		bZero := b.lastUsed.IsZero()
		switch {
		case aZero && bZero:
			return 0
		case aZero:
			return 1
		case bZero:
			return -1
		}
		if a.lastUsed.After(b.lastUsed) {
			return -1
		}
		if b.lastUsed.After(a.lastUsed) {
			return 1
		}
		return 0
	})

	var old []PruneCandidate
	var keptEntries []PruneCandidate
	for i, entry := range active {
		sbName := ""
		if entry.hasSb {
			sbName = entry.sandbox.Name
		} else if entry.info.Config != nil {
			sbName = entry.info.Config.SandboxName
		}

		if i < keep {
			keptEntries = append(keptEntries, PruneCandidate{
				SandboxName:   sbName,
				WorkspacePath: entry.info.WorkspacePath,
				ProjectHash:   entry.info.Hash,
				LastUsed:      entry.lastUsed,
			})
			continue
		}

		reason := fmt.Sprintf("outside keep=%d most recently used", keep)
		old = append(old, PruneCandidate{
			SandboxName:   sbName,
			WorkspacePath: entry.info.WorkspacePath,
			ProjectHash:   entry.info.Hash,
			LastUsed:      entry.lastUsed,
			Reason:        reason,
		})
	}

	// Orphaned sbx sandboxes: present in `sbx ls` but no project entry claims them.
	// We cross-reference by workspace path recorded in the project config since sbx
	// sandbox names have no unique prefix to filter on.
	// FIXME: sbx stores microVMs in a different folder than docker sandbox; dangling
	// sandboxes not visible in `sbx ls` (e.g. whose workspace was deleted) cannot be
	// detected here until folder-based cleanup is implemented.
	for _, sb := range sbxSandboxes {
		if accountedSandboxNames[sb.Name] {
			continue
		}
		wsMissing := sb.Workspace != ""
		if wsMissing {
			_, statErr := os.Stat(sb.Workspace)
			wsMissing = os.IsNotExist(statErr)
		}

		reason := "no sbx project entry found for sandbox"
		if wsMissing {
			reason = "no sbx project entry found and workspace directory no longer exists"
		}

		old = append(old, PruneCandidate{
			SandboxName:      sb.Name,
			WorkspacePath:    sb.Workspace,
			Reason:           reason,
			WorkspaceMissing: wsMissing,
		})
	}

	all := append(stale, old...)
	return all, keptEntries, nil
}

// PruneOneSbx removes an sbx sandbox, its .sbox/ directory, and its project config
// entry for a single candidate. Errors from individual steps are collected and the
// first one is returned; later steps still run regardless.
func PruneOneSbx(c PruneCandidate) error {
	var firstErr error

	setErr := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

	if c.SandboxName != "" {
		if err := RemoveSbxSandboxByName(c.SandboxName); err != nil {
			zlog.Warn("failed to remove sbx sandbox (may already be gone)",
				zap.String("sandbox", c.SandboxName),
				zap.Error(err))
			setErr(fmt.Errorf("remove sbx sandbox %q: %w", c.SandboxName, err))
		}
	}

	if !c.WorkspaceMissing && c.WorkspacePath != "" {
		sboxDir := filepath.Join(c.WorkspacePath, ".sbox")
		if err := os.RemoveAll(sboxDir); err != nil {
			zlog.Warn("failed to remove .sbox directory",
				zap.String("path", sboxDir),
				zap.Error(err))
			setErr(fmt.Errorf("remove .sbox dir %q: %w", sboxDir, err))
		}
	}

	if c.ProjectHash != "" {
		config, err := LoadConfig()
		if err != nil {
			setErr(fmt.Errorf("load config: %w", err))
		} else {
			projectDir := filepath.Join(config.SboxDataDir, "projects", c.ProjectHash)
			if err := os.RemoveAll(projectDir); err != nil {
				zlog.Warn("failed to remove project config directory",
					zap.String("path", projectDir),
					zap.Error(err))
				setErr(fmt.Errorf("remove project config %q: %w", projectDir, err))
			}
		}
	}

	return firstErr
}
// entry for a single candidate. Errors from individual steps are collected and
// the first one is returned; later steps still run regardless.
func PruneOne(c PruneCandidate) error {
	var firstErr error

	setErr := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

	// 1. Remove Docker sandbox.
	if c.SandboxName != "" {
		if err := RemoveDockerSandboxByName(c.SandboxName); err != nil {
			// Only log a warning; the sandbox may already have been removed.
			zlog.Warn("failed to remove docker sandbox (may already be gone)",
				zap.String("sandbox", c.SandboxName),
				zap.Error(err))
			setErr(fmt.Errorf("remove sandbox %q: %w", c.SandboxName, err))
		}
	}

	// 2. Remove .sbox/ directory inside the workspace (only if workspace still exists).
	if !c.WorkspaceMissing && c.WorkspacePath != "" {
		sboxDir := filepath.Join(c.WorkspacePath, ".sbox")
		if err := os.RemoveAll(sboxDir); err != nil {
			zlog.Warn("failed to remove .sbox directory",
				zap.String("path", sboxDir),
				zap.Error(err))
			setErr(fmt.Errorf("remove .sbox dir %q: %w", sboxDir, err))
		}
	}

	// 3. Remove project config entry.
	if c.ProjectHash != "" {
		config, err := LoadConfig()
		if err != nil {
			setErr(fmt.Errorf("load config: %w", err))
		} else {
			projectDir := filepath.Join(config.SboxDataDir, "projects", c.ProjectHash)
			if err := os.RemoveAll(projectDir); err != nil {
				zlog.Warn("failed to remove project config directory",
					zap.String("path", projectDir),
					zap.Error(err))
				setErr(fmt.Errorf("remove project config %q: %w", projectDir, err))
			}
		}
	}

	return firstErr
}
