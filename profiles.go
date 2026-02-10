package sbox

// Profile represents a development profile that can be installed in the sandbox
type Profile struct {
	// Name is the unique identifier for this profile
	Name string

	// Description provides a human-readable explanation of what this profile provides
	Description string

	// Dependencies lists other profiles that must be installed before this one
	Dependencies []string

	// DockerfileSnippet contains the Dockerfile commands to install this profile's tools
	DockerfileSnippet string
}

// BuiltinProfiles contains all available built-in profiles
var BuiltinProfiles = map[string]Profile{
	"go": {
		Name:        "go",
		Description: "Go programming language toolchain (latest stable version)",
		DockerfileSnippet: `# Go toolchain
RUN apt-get update && apt-get install -y wget && \
    wget -q https://go.dev/dl/go1.24.4.linux-${GO_ARCH}.tar.gz && \
    tar -C /usr/local -xzf go1.24.4.linux-${GO_ARCH}.tar.gz && \
    rm go1.24.4.linux-${GO_ARCH}.tar.gz && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/workspace/.go"
ENV PATH="${GOPATH}/bin:${PATH}"
`,
	},
	"rust": {
		Name:        "rust",
		Description: "Rust programming language toolchain (stable)",
		DockerfileSnippet: `# Rust toolchain (installed system-wide for all users)
ENV RUSTUP_HOME="/usr/local/rustup"
ENV CARGO_HOME="/usr/local/cargo"
ENV PATH="/usr/local/cargo/bin:${PATH}"

RUN apt-get update && apt-get install -y curl build-essential && \
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --no-modify-path && \
    chmod -R a+rwx /usr/local/rustup /usr/local/cargo && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
`,
	},
	"docker": {
		Name:        "docker",
		Description: "Docker CLI tools for container management",
		DockerfileSnippet: `# Docker CLI
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    gnupg \
    lsb-release && \
    mkdir -p /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian \
    $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null && \
    apt-get update && apt-get install -y docker-ce-cli docker-compose-plugin && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
`,
	},
	"bash-utils": {
		Name:        "bash-utils",
		Description: "Common shell utilities (jq, yq, curl, wget, git)",
		DockerfileSnippet: `# Bash utilities
RUN apt-get update && apt-get install -y \
    jq \
    curl \
    wget \
    git \
    vim \
    nano \
    htop \
    tree \
    zip \
    unzip && \
    wget -qO /usr/local/bin/yq https://github.com/mikefarah/yq/releases/latest/download/yq_linux_${YQ_ARCH} && \
    chmod +x /usr/local/bin/yq && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
`,
	},
	"substreams": {
		Name:         "substreams",
		Description:  "Substreams and Firehose Core CLI tools for blockchain data",
		Dependencies: []string{"rust"},
		DockerfileSnippet: `# Substreams CLI (from official Docker image)
COPY --from=ghcr.io/streamingfast/substreams:latest /app/substreams /usr/local/bin/substreams

# Firehose Core CLI (from official Docker image)
COPY --from=ghcr.io/streamingfast/firehose-core:latest /app/firecore /usr/local/bin/firecore

# buf CLI and protoc (protobuf compiler)
RUN apt-get update && apt-get install -y curl unzip && \
    curl -sSL "https://github.com/bufbuild/buf/releases/latest/download/buf-$(uname -s)-$(uname -m)" -o /usr/local/bin/buf && \
    chmod +x /usr/local/bin/buf && \
    PROTOC_VERSION=$(curl -sSL https://api.github.com/repos/protocolbuffers/protobuf/releases/latest | grep '"tag_name"' | sed 's/.*"v\(.*\)".*/\1/') && \
    curl -sSL "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-${PROTOC_ARCH}.zip" -o /tmp/protoc.zip && \
    unzip -o /tmp/protoc.zip -d /usr/local bin/protoc 'include/*' && \
    rm /tmp/protoc.zip && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
`,
	},
	"javascript": {
		Name:        "javascript",
		Description: "JavaScript/TypeScript development tools (pnpm, yarn)",
		DockerfileSnippet: `# JavaScript package managers (pnpm, yarn)
# Note: Node.js and npm are already installed in the base image
RUN npm install -g pnpm yarn
`,
	},
}

// GetProfile retrieves a profile by name
func GetProfile(name string) (Profile, bool) {
	profile, ok := BuiltinProfiles[name]
	return profile, ok
}

// ListProfiles returns a sorted list of all available profile names
func ListProfiles() []string {
	names := make([]string, 0, len(BuiltinProfiles))
	for name := range BuiltinProfiles {
		names = append(names, name)
	}
	return names
}
