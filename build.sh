#!/usr/bin/env bash
# Builds the game for the browser:
#
#   web/main.wasm      the Go engine, compiled to WebAssembly
#   web/wasm_exec.js   Go's JavaScript runtime support, copied from the
#                      toolchain so it always matches the compiler
#
# Usage:
#   ./build.sh          build
#   ./build.sh test     run the tests, then build
#   ./build.sh run      build and start the server on http://localhost:8080
set -euo pipefail
cd "$(dirname "$0")"

command -v go >/dev/null || { echo "Go is not installed: https://go.dev/dl/" >&2; exit 1; }

if [[ "${1:-}" == "test" ]]; then
  go vet ./...
  GOOS=js GOARCH=wasm go vet ./cmd/wasm
  go test ./...
fi

# GOOS=js GOARCH=wasm selects the browser target.
# -s -w drops the symbol table and debug info.
# -trimpath keeps local directory names out of the binary.
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o web/main.wasm ./cmd/wasm

# The file moved between Go releases.
goroot="$(go env GOROOT)"
for candidate in "$goroot/lib/wasm/wasm_exec.js" "$goroot/misc/wasm/wasm_exec.js"; do
  if [[ -f "$candidate" ]]; then
    cp "$candidate" web/wasm_exec.js
    break
  fi
done
[[ -f web/wasm_exec.js ]] || { echo "wasm_exec.js not found under $goroot" >&2; exit 1; }

raw=$(wc -c < web/main.wasm)
packed=$(gzip -9 -c web/main.wasm | wc -c)
printf 'web/main.wasm  %d KB (%d KB gzipped), %s\n' $((raw / 1024)) $((packed / 1024)) "$(go version | cut -d' ' -f3)"

if [[ "${1:-}" == "run" ]]; then
  exec go run ./cmd/server -addr "${ADDR:-:8080}"
fi
