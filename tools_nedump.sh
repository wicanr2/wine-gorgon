#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
GAME=${PTO2_GAME:-/home/anr2/cht/pto2-remake/private/original/PTO-Paci/PTO2WIN}
exec docker run --rm --network none --memory 2g --cpus 1 --pids-limit 256 \
  --log-opt max-size=10m --log-opt max-file=3 --user "$(id -u):$(id -g)" \
  -v "$ROOT":/src -w /src -v "$GAME":/game:ro \
  -v wine-gorgon-gomod:/gopath/pkg/mod -v wine-gorgon-gocache:/gocache \
  -e GOFLAGS=-mod=mod -e GOPATH=/gopath -e GOCACHE=/gocache -e HOME=/tmp \
  golang:1.26.7-bookworm go run ./cmd/nedump "$@" /game/TEKE2WIN.EXE
