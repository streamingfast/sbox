.PHONY: build build-image clean test

# Build the sbox binary for the current platform
build:
	go build -o sbox ./cmd/sbox

# Build the sbox binary image for local development
# This creates ghcr.io/streamingfast/sbox:dev for local use
build-image:
	docker build \
		-t ghcr.io/streamingfast/sbox:dev \
		-f Dockerfile .

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -f sbox
	docker rmi ghcr.io/streamingfast/sbox:dev 2>/dev/null || true
