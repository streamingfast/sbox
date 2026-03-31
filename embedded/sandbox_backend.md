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

### The `.sbox/` Directory

`.sbox/` is a **bidirectional exchange directory** between the host and the sandbox. Both sides can read and write it — it is not read-only.

**Host → sandbox** (the user places files here for you to use):
- Screenshots, design mockups, or other reference images
- Files needed during active development that are transient by nature

**Sandbox → host** (you may write here when it makes sense):
- A library repository cloned in order to investigate or fix a bug that is in a dependency. In this scenario, placing it under `.sbox/` is legitimate so the user can inspect or continue work on it from the host.

**Do NOT use `.sbox/` for:**
- Temporary log output, scratch files, or test binaries
- Repositories cloned purely for reference or one-off testing (use `/tmp/` instead)
- Any file that is not meaningfully shared between you and the user

### Temporary Files

For anything that is only needed by you and has no value to the user on the host side, use `/tmp/` or any other directory you own:

```bash
# Clone a reference repo temporarily — clean up when done
git clone --depth 1 https://github.com/example/repo.git /tmp/reference-repo
cat /tmp/reference-repo/README.md
rm -rf /tmp/reference-repo

# Avoid writing ephemeral files into the workspace or .sbox/
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
