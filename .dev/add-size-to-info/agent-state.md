# Feature: add-size-to-info — Agent State

## Status

Complete. Build passes, all tests pass.

## Design

Added disk size information to `sbox info` and `sbox info --all` output.

### New Package Functions (`size.go`)

- `DiskSizeInfo` struct: `ContainerSize`, `ImageSize`, `VolumeSize` (all int64, -1 = unknown)
  - `HasAnySize() bool` — true if at least one field is >= 0
  - `Total() int64` — sum of known sizes (container writable + volume)
- `FormatBytes(bytes int64) string` — formats to human-readable SI units (KB, MB, GB); returns "unknown" for -1
- `GetContainerDiskSize(containerID string) DiskSizeInfo` — runs `docker inspect --size <id> --format '{{.SizeRootFs}} {{.SizeRw}}'`
  - `SizeRootFs` → `ImageSize` (read-only image layers, shared)
  - `SizeRw` → `ContainerSize` (writable layer, unique to this container)
- `GetVolumeDiskSize(volumeName string) int64` — parses `docker system df -v` output for the named volume size
- `GetSandboxDiskSize(sandboxName string) int64` — tries `docker sandbox inspect <name>` with `{{.Size}}`, `{{.DiskSize}}`, `{{.SizeOnDisk}}` format fields; returns -1 if unavailable
- `GetContainerVolumeNameByID(containerID string) string` — public wrapper around internal `getContainerVolumeName`
- `parseDockerSize(s string) int64` — parses Docker size strings like "1.23GB", "456MB", "12KB"

### Updated `cmd/sbox/info.go`

Added `printDiskSize(cmd, backendName, info, prefix)` function:
- For container backend: calls `GetContainerDiskSize`, then `GetContainerVolumeNameByID` + `GetVolumeDiskSize`
  - Shows `Size: X MB (container) + Y GB (volume)` when both are known
  - Shows just `Size: X MB` or `Size: Y GB (volume)` when only one is known
  - Omits Size line entirely when no size is available
- For sandbox backend: calls `GetSandboxDiskSize`
  - Shows `Size: X GB` when available
  - Omits Size line when unavailable

`printContainerStatus` updated to call `printDiskSize` after printing Status.

## Task Checklist

- [x] Create `size.go` with `DiskSizeInfo`, `FormatBytes`, `GetContainerDiskSize`, `GetVolumeDiskSize`, `GetSandboxDiskSize`, `GetContainerVolumeNameByID`, `parseDockerSize`
- [x] Update `cmd/sbox/info.go`: add `printDiskSize`, call it from `printContainerStatus`
- [x] `go test ./...` passes
- [x] `go build ./...` passes
- [x] CHANGELOG.md updated
- [x] Agent-state.md written
