#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

cd "${REPO_ROOT}"

RELEASE_DIR="${REPO_ROOT}/release"
mkdir -p "${RELEASE_DIR}"

echo "Running tests..."
go test ./...

echo "Running vet..."
go vet ./...

echo "Building Linux amd64 core binary..."
GOOS=linux GOARCH=amd64 go build -o "${RELEASE_DIR}/snispf_linux_amd64" ./cmd/snispf

echo "Build complete:"
ls -lh "${RELEASE_DIR}/snispf_linux_amd64"
