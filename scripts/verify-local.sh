#!/bin/sh
set -eu

go test ./...
go build -o "${TMPDIR:-/tmp}/logistics-search" ./cmd/logistics-search
echo "tests passed; binary built at ${TMPDIR:-/tmp}/logistics-search"
