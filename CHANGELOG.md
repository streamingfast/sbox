# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

### Added

- `substreams` profile for blockchain development with Substreams and Firehose Core CLIs
  - Automatically includes `rust` profile as a dependency
  - Installs `substreams` CLI from `ghcr.io/streamingfast/substreams:latest`
  - Installs `firecore` CLI from `ghcr.io/streamingfast/firehose-core:latest`
  - Installs `buf` CLI (latest) and `protoc` (latest) for protobuf development
- Profile dependency support - profiles can now declare dependencies on other profiles
- Agent sharing via `--agents` CLI flag (converts `~/.claude/agents/*.md` to JSON)
- Plugin sharing via `--plugin-dir` flag (each installed plugin is mounted separately)
- `--recreate` flag for `sbox run` to remove existing sandbox before running
- Mount mismatch detection with `--recreate` suggestion
- Docker command display in `sbox info`
- `--all` flag to `sbox info` to list all known projects
- `--workspace/-w` flag to `sbox info` to specify workspace directory
- `sbox auth` command for shared authentication across all sandboxes
  - Runs `claude setup-token` to generate OAuth credentials
  - Stores `.credentials.json` in `~/.config/sbox/` directory (separate from user's Claude installation)
  - Credentials automatically mounted to `/home/agent/.claude/.credentials.json` in all sandbox sessions
  - `--status` flag to check authentication status
  - `--logout` flag to remove stored credentials

### Changed

- `sbox info` now shows current project info by default (use `--all` for all projects)

### Fixed

- `--rebuild` now also recreates the sandbox so the newly built image is actually used
  - Previously rebuilding the image didn't update the running/stopped sandbox which kept using the old image
- Rust profile now installs to system-wide location (`/usr/local/cargo`, `/usr/local/rustup`)
  - Previously installed to `/root/.cargo` which wasn't accessible to the sandbox `agent` user
  - Fixed `chmod` to include write permissions (`a+rwx`) so `rustup` operations work (uses `/usr/local/rustup/tmp/`)
  - `cargo`, `rustc`, and `rustup` commands now work correctly inside the sandbox
- Integration tests now run as `agent` user to match the sandbox environment and catch permission issues
- Authentication now works correctly across sandbox sessions
  - Previously stored token as environment variable which wasn't reliably recognized by Claude
  - Now creates proper `.credentials.json` file in `~/.config/sbox/credentials.json`
  - File is mounted into sandbox at `/home/agent/.claude/.credentials.json`
  - Keeps sbox authentication separate from user's existing Claude installation

### Removed

- `sbox status` command (consolidated into `sbox info`)
