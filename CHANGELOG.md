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

### Changed

- `sbox info` now shows current project info by default (use `--all` for all projects)

### Removed

- `sbox status` command (consolidated into `sbox info`)
