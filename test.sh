#!/bin/bash
set -euo pipefail

go test ./...

golangci-lint run \
    --enable-all \
    --disable nonamedreturns,testableexamples,testpackage,wsl,nlreturn,ireturn,gocritic,gofumpt \
    --config <(echo '{"linters-settings":{"lll": {"line-length": 150}}}')
