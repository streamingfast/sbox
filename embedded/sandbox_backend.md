# Sandbox Environment Context (Docker Sandbox Backend)

You are running inside an **sbox sandbox** using the **Docker Sandbox** backend - a MicroVM-based isolated environment for Claude Code (released February 2026).

## Environment Overview

Docker Sandbox runs your session inside a lightweight MicroVM, providing enhanced isolation while maintaining near-native performance. This is the most secure execution mode.

### User & Paths

- **User**: `agent` (non-root with sudo access)
- **Home directory**: `/home/agent`
- **Claude config**: `/home/agent/.claude`
- **Workspace**: Mounted at the same path as on the host (check `$PWD` or `$WORKSPACE_DIR`)

### Pre-installed Tools

- **Git**: Version control
- **Node.js & npm**: JavaScript runtime
- **rsync**: File synchronization
- **curl, wget**: HTTP clients
- **jq**: JSON processor
- **sudo**: Elevated privileges when needed

Additional tools may be available depending on configured profiles.

## Freedom & Limitations

### What You Can Do

- Full read/write access to the workspace directory
- Install packages with `sudo apt-get install`
- Run any command as root with `sudo`
- Access the internet (subject to firewall rules)
- Run Docker containers (Docker runs natively inside the MicroVM)

### Limitations

- **Network restrictions**: Some external services may be blocked by firewall
- **Resource limits**: MicroVM has allocated CPU/memory limits

### Important: Protected Directories

- **`.sbox/` directory is READ-ONLY**: This directory is managed by sbox for configuration, caching, and state management. Never write files to `.sbox/` - your changes may be overwritten or cause issues.

### Temporary Files & Cloned Repositories

When you need to clone repositories or download files for temporary reference (e.g., to read documentation, compare implementations, or run one-off scripts):

- **Use `/tmp/` or a subdirectory**: Clone to `/tmp/my-reference-repo/` instead of the workspace
- **Avoid cluttering the workspace**: The workspace should only contain project-relevant files
- **Clean up when done**: Remove temporary files after use to free disk space

```bash
# Good: Clone reference repo to temp directory
git clone --depth 1 https://github.com/example/repo.git /tmp/reference-repo
cat /tmp/reference-repo/README.md
rm -rf /tmp/reference-repo

# Bad: Cloning into workspace pollutes project
git clone https://github.com/example/repo.git ./reference-repo  # Don't do this
```

## Persistence

The MicroVM persists across restarts. Installed packages, files, and configurations remain until the sandbox is explicitly deleted by the user (`sbox stop --rm`).

| What | Persists |
|------|----------|
| Workspace files | Yes |
| Claude state (`~/.claude`) | Yes |
| Installed apt packages | Yes (until VM deleted) |
| Files outside workspace | Yes (until VM deleted) |

## Networking

Sandbox env has instructions about Firewall but their instruction does not tell how to do it. When you need network access to some host, dev will need to allow the sandbox network using `docker sandbox network proxy` command here its help:

```
Usage:  docker sandbox network proxy <sandbox> [OPTIONS]
Manage proxy configuration for a sandbox
Options:
--allow-cidr string    Remove an IP range in CIDR notation from the block or bypass lists (can be specified multiple times)
--allow-host string    Permit access to a domain or IP (can be specified multiple times)
--block-cidr string    Block access to an IP range in CIDR notation (can be specified multiple times)
--block-host string    Block access to a domain or IP (can be specified multiple times)
--bypass-cidr string   Bypass MITM proxy for an IP range in CIDR notation (can be specified multiple times)
--bypass-host string   Bypass MITM proxy for a domain or IP (can be specified multipletimes)
-D, --debug                Enable debug logging
--policy allow|deny    Set the default policy
```

Propose to the user the command(s) he should perform before you can retry.

## Docker Usage

Docker Sandbox includes a **native Docker daemon** running inside the MicroVM. This provides full Docker functionality without any special configuration.

### Running Docker Containers

```bash
# Docker runs natively - no sudo needed for docker commands
docker run hello-world

# Run a container with port mapping
docker run -d -p 8080:80 nginx

# Access the container
curl http://localhost:8080
```

### Docker Compose

```bash
# Docker Compose is available as a plugin
docker compose up -d
docker compose logs -f
docker compose down
```

### Testcontainers

Testcontainers work out of the box since Docker is running natively:

```java
// Java example
@Container
PostgreSQLContainer<?> postgres = new PostgreSQLContainer<>("postgres:15");
```

```go
// Go example
container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
    ContainerRequest: testcontainers.ContainerRequest{
        Image:        "postgres:15",
        ExposedPorts: []string{"5432/tcp"},
    },
    Started: true,
})
```

```javascript
// Node.js example
const container = await new GenericContainer("postgres:15")
    .withExposedPorts(5432)
    .start();
```

### Volume Mounts

When mounting volumes, use paths as they appear inside the sandbox:

```bash
# Mount current directory
docker run -v $(pwd):/app alpine ls /app

# Mount workspace subdirectory
docker run -v ${WORKSPACE_DIR}/data:/data alpine ls /data
```

## Common Patterns

### Installing packages
```bash
sudo apt-get update && sudo apt-get install -y <package>
```

### Running as root
```bash
sudo <command>
# Or for a root shell:
sudo -i
```

### Checking Docker status
```bash
docker info
docker ps -a
```
