# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

### Added

- Agent sharing via `--agents` CLI flag (converts `~/.claude/agents/*.md` to JSON)
- Plugin sharing via `--plugin-dir` flag (each installed plugin is mounted separately)
- `--recreate` flag for `sbox run` to remove existing sandbox before running
- Mount mismatch detection with `--recreate` suggestion
- Docker command display in `sbox info`
- `--all` flag to `sbox info` to list all known projects
- `--workspace/-w` flag to `sbox info` to specify workspace directory
- `sbox auth` command for shared authentication across all sandboxes
  - Generates long-lived OAuth token (valid for 1 year)
  - Token automatically passed to all sandbox sessions
  - `--status` flag to check authentication status
  - `--logout` flag to remove stored token

### Changed

- `sbox info` now shows current project info by default (use `--all` for all projects)

### Removed

- `sbox status` command (consolidated into `sbox info`)
