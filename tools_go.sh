#!/bin/sh
# 在容器裡跑 Go。主機沒有 Go，也不該有——[HARD] 建置與測試一律走 docker。
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec docker run --rm --network none \
  --memory 4g --cpus 2 --pids-limit 512 \
  --log-opt max-size=10m --log-opt max-file=3 \
  --user "$(id -u):$(id -g)" \
  -v "$ROOT":/src -w /src \
  -v wine-gorgon-gomod:/gopath/pkg/mod -v wine-gorgon-gocache:/gocache \
  -e GOFLAGS=-mod=mod -e GOPATH=/gopath -e GOCACHE=/gocache -e HOME=/tmp \
  golang:1.26.7-bookworm "$@"
