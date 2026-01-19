package sbox

// Profile represents a development profile that can be installed in the sandbox
type Profile struct {
	// Name is the unique identifier for this profile
	Name string

	// Description provides a human-readable explanation of what this profile provides
	Description string

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
    wget -q https://go.dev/dl/go1.24.4.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.24.4.linux-amd64.tar.gz && \
    rm go1.24.4.linux-amd64.tar.gz && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/workspace/.go"
ENV PATH="${GOPATH}/bin:${PATH}"
`,
	},
	"rust": {
		Name:        "rust",
		Description: "Rust programming language toolchain (stable)",
		DockerfileSnippet: `# Rust toolchain
RUN apt-get update && apt-get install -y curl build-essential && \
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

ENV PATH="/root/.cargo/bin:${PATH}"
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
    wget -qO /usr/local/bin/yq https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 && \
    chmod +x /usr/local/bin/yq && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
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
