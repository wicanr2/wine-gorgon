#!/bin/sh
# 跑 nerun 對 PTO2 英文原版。原版目錄唯讀掛進去，輸出寫到 out/。
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
GAME=${PTO2_GAME:-/home/anr2/cht/pto2-remake/private/original/PTO-Paci/PTO2WIN}
OUT=${OUT:-$ROOT/out}
mkdir -p "$OUT"
test -d "$GAME"
exec docker run --rm --network none \
  --memory 4g --cpus 2 --pids-limit 512 \
  --log-opt max-size=10m --log-opt max-file=3 \
  --user "$(id -u):$(id -g)" \
  -v "$ROOT":/src -w /src \
  -v "$GAME":/game:ro -v "$OUT":/out \
  -v wine-gorgon-gomod:/gopath/pkg/mod -v wine-gorgon-gocache:/gocache \
  -e GOFLAGS=-mod=mod -e GOPATH=/gopath -e GOCACHE=/gocache -e HOME=/tmp \
  golang:1.26.7-bookworm \
  go run ./cmd/nerun -data /game "$@" /game/TEKE2WIN.EXE
