# Sandbox Environment Context (Docker Container Backend)

You are running inside an **sbox sandbox** using the **Docker Container** backend - a standard Docker container environment for Claude Code.

## Environment Overview

The Container backend runs your session inside a regular Docker container. This mode offers compatibility with all Docker environments and uses named volumes for persistence.

### User & Paths

- **User**: `agent` (non-root with sudo access)
- **Home directory**: `/home/agent`
- **Claude config**: `/home/agent/.claude` (persisted via named volume)
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
- Run Docker commands (if `--docker-socket` was enabled)

### Limitations

- **Installed packages are ephemeral**: Lost when container is recreated
- **Docker access requires explicit flag**: Must use `sbox run --docker-socket`
- **Network restrictions**: Some external services may be blocked by firewall
- **Docker socket permissions**: Requires `sudo` for Docker commands

## Persistence

| What | Persists Across Restarts | Persists Across Recreate |
|------|-------------------------|-------------------------|
| Workspace files | Yes | Yes |
| Claude state (`~/.claude`) | Yes (named volume) | Yes (named volume) |
| Installed apt packages | Yes | No |
| Files outside workspace | Yes | No |

To persist environment variables:
```bash
echo 'export MY_VAR=value' >> /etc/profile.d/sbox-env.sh
# Use login shell to load: bash -l -c "command"
```

## Docker Usage

Docker access requires the `--docker-socket` flag when starting the sandbox. The Docker socket from the host is mounted into the container.

**Important**: Docker commands require `sudo` because the mounted socket has root permissions.

### Checking Docker Access

```bash
# Verify Docker is available
sudo docker info

# If you see "permission denied", Docker socket was not mounted
# Ask the user to restart with: sbox run --docker-socket
```

### Running Docker Containers

```bash
# Always use sudo for Docker commands
sudo docker run hello-world

# Run a container with port mapping
sudo docker run -d -p 8080:80 nginx

# Access the container (localhost works because of host network)
curl http://localhost:8080
```

### Docker Compose

```bash
# Docker Compose is available as a plugin
sudo docker compose up -d
sudo docker compose logs -f
sudo docker compose down
```

### Testcontainers

Testcontainers require the Docker socket to be mounted. Configure testcontainers to use sudo or set up Docker group permissions:

**Option 1: Configure DOCKER_HOST (if socket is accessible)**
```bash
export DOCKER_HOST=unix:///var/run/docker.sock
```

**Option 2: Use testcontainers with root privileges**

For languages that spawn Docker commands, you may need to run tests as root:
```bash
sudo -E go test ./...  # -E preserves environment
sudo -E npm test
```

**Java example:**
```java
// May need to run with sudo or configure Docker socket permissions
@Container
PostgreSQLContainer<?> postgres = new PostgreSQLContainer<>("postgres:15");
```

**Go example:**
```go
// Run test with: sudo -E go test ./...
container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
    ContainerRequest: testcontainers.ContainerRequest{
        Image:        "postgres:15",
        ExposedPorts: []string{"5432/tcp"},
    },
    Started: true,
})
```

**Node.js example:**
```javascript
// Run test with: sudo -E npm test
const container = await new GenericContainer("postgres:15")
    .withExposedPorts(5432)
    .start();
```

### Volume Mounts - Important!

When mounting volumes from Docker containers, you must use **host paths**, not container paths. The Docker daemon runs on the host, not inside this container.

```bash
# WRONG - uses container path (won't work)
sudo docker run -v $(pwd):/app alpine ls /app

# CORRECT - the workspace is mounted at the same path, so this works
# because $(pwd) returns the same path as on the host
sudo docker run -v ${WORKSPACE_DIR}:/app alpine ls /app
```

If you need to mount paths outside the workspace, you'll need to know the host path.

## Common Patterns

### Installing packages (ephemeral)
```bash
sudo apt-get update && sudo apt-get install -y <package>
```

### Running as root
```bash
sudo <command>
# Or for a root shell:
sudo -i
```

### Checking if Docker socket is available
```bash
if [ -S /var/run/docker.sock ]; then
    echo "Docker socket is mounted"
    sudo docker ps
else
    echo "Docker socket not available - restart with: sbox run --docker-socket"
fi
```

### Setting up Docker group (alternative to sudo)
```bash
# Add agent to docker group (if it exists)
sudo usermod -aG docker agent
# Then start a new shell to pick up the group
newgrp docker
# Now docker commands work without sudo
docker ps
```
