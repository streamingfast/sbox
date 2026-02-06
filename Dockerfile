# Multi-arch image containing the sbox binary
# Used as a source for COPY --from in template builds

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETARCH
ARG TARGETOS=linux

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.version=${VERSION} -extldflags '-static'" \
    -o /usr/local/bin/sbox ./cmd/sbox

FROM alpine:3.21

COPY --from=builder /usr/local/bin/sbox /usr/local/bin/sbox
RUN chmod +x /usr/local/bin/sbox
