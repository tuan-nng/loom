# loom — dev tasks. Run `just` (or `just --list`) to see all recipes.

# git-derived version string embedded via -ldflags (README "Build"); falls
# back to "dev" outside a git checkout, matching cli.Version's own default.
version := `git describe --tags --always --dirty 2>/dev/null || echo dev`

ldflags := "-X loom/internal/cli.Version=" + version

default:
    @just --list

# Build ./loom in the repo root, embedding the git version.
build:
    go build -ldflags "{{ldflags}}" -o loom ./cmd/loom

# Install loom onto $GOBIN (or $GOPATH/bin), embedding the git version.
install:
    go install -ldflags "{{ldflags}}" ./cmd/loom
    @bin="$(go env GOBIN)"; [ -n "$bin" ] || bin="$(go env GOPATH)/bin"; echo "installed loom {{version}} -> $bin/loom"

# Run the full check suite (README "Testing"): build, vet, test.
test:
    go build ./...
    go vet ./...
    go test ./...

# go vet only.
vet:
    go vet ./...

# Rewrite files with gofmt.
fmt:
    gofmt -l -w .

# List files that need gofmt, without modifying them (CI-friendly).
fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    out="$(gofmt -l .)"
    if [ -n "$out" ]; then
        echo "gofmt needed on:"
        echo "$out"
        exit 1
    fi

# Run loom straight from source (dev loop), forwarding extra args.
run *args:
    go run ./cmd/loom {{args}}

# Remove the local build artifact.
clean:
    rm -f loom
