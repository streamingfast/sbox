# Multi-arch image containing the sbox binary
# Used as a source for COPY --from in template builds

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG TARGETARCH
ARG TARGETOS=linux

WORKDIR /src
COPY . .

ARG VERSION=dev

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.version=${VERSION} -extldflags '-static'" \
    -o /sbox ./cmd/sbox

FROM alpine:3.21

COPY --from=builder /sbox /sbox
RUN chmod +x /sbox
