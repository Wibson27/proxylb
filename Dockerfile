# Build stage: compile the Go binary in a full Go environment.
# The builder image has the Go compiler, linker, and standard library.
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy dependency manifests first. Docker caches each layer — if
# go.mod/go.sum haven't changed, the expensive `go mod download`
# is skipped on subsequent builds.
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build.
# CGO_ENABLED=0 produces a statically linked binary — no libc dependency.
# This is required for the FROM scratch runtime image which has no
# shared libraries at all.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o proxylb .

# Runtime stage: minimal image with just the binary.
# FROM scratch has zero OS, zero shell, zero libraries.
# Attack surface: the Go binary and the config file. Nothing else.
FROM scratch

COPY --from=builder /build/proxylb /proxylb
COPY config.yaml /config.yaml

EXPOSE 8080

ENTRYPOINT ["/proxylb"]
