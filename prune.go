package sbox

import (
	"fmt"
	"os"
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

// FindPruneCandidates returns sandboxes that should be pruned according to opts.
//
// The selection algorithm:
//  1. Load all known projects from ~/.config/sbox/projects/.
//  2. Load all Docker sandboxes from `docker sandbox ls`.
//  3. Workspaces that no longer exist on disk are always candidates (stale).
//  4. Among workspaces that still exist, keep the opts.Keep most recently used;
//     the rest become candidates.
//  5. Docker sandboxes with no corresponding project entry are also candidates.
func FindPruneCandidates(opts PruneOptions) ([]PruneCandidate, error) {
	keep := opts.Keep
	if keep <= 0 {
		keep = 5
	}

	// Load all known projects.
	projects, err := ListProjects()
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
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
			sb, hasSb = sandboxByName[proj.Config.SandboxName]
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
	for i, entry := range active {
		if i < keep {
			continue // keep this one
		}

		sbName := ""
		if entry.hasSb {
			sbName = entry.sandbox.Name
		} else if entry.info.Config != nil {
			sbName = entry.info.Config.SandboxName
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
	for _, ds := range dockerSandboxes {
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
	return all, nil
}

// PruneAll removes all provided candidates and returns any errors encountered.
// Errors are collected rather than aborting on first failure.
func PruneAll(candidates []PruneCandidate) []PruneError {
	var errs []PruneError

	for _, c := range candidates {
		if err := pruneOne(c); err != nil {
			errs = append(errs, PruneError{Candidate: c, Err: err})
		}
	}

	return errs
}

func pruneOne(c PruneCandidate) error {
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
