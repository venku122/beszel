#!/bin/sh
# Run from repository root after cross-building build/beszel-agent-linux-amd64.
set -eu
commit=$(git rev-parse --short=12 HEAD)
context=$(mktemp -d)
trap 'rm -rf "$context"' EXIT
cp build/beszel-agent-linux-amd64 "$context/agent"
cp docs/gpu-intel-truenas/Dockerfile "$context/Dockerfile"
docker build --platform linux/amd64 -t "beszel-intel-lab:$commit" "$context"
printf 'Built beszel-intel-lab:%s\n' "$commit"
