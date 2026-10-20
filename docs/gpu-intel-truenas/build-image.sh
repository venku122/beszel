#!/bin/sh
# Build only committed source; never reuse an executable from another branch.
set -eu
cd "$(git rev-parse --show-toplevel)"
if [ -n "$(git status --porcelain --untracked-files=all)" ]; then
    printf '%s\n' 'Refusing image build: dirty source tree. Commit or preserve changes first.' >&2
    exit 1
fi
source_sha=$(git rev-parse HEAD)
commit=$(printf '%.12s' "$source_sha")
# Platform-specific digest pins the linux/amd64 compiler image. Update it deliberately when go.mod changes.
go_image='golang:1.27.2@sha256:2d658507a011f1eadc0114d7a421b312b2e77d9042a24036a26e267cc9231677'
go_version=$(git show "$source_sha:go.mod" | awk '$1 == "go" {print $2; exit}')
case "$go_version" in
    1.27.2) ;;
    *) printf '%s\n' 'Unsupported go.mod toolchain: update the pinned Go image first.' >&2; exit 1 ;;
esac
context=$(mktemp -d)
trap 'rm -rf "$context"' EXIT HUP INT TERM
mkdir "$context/source" "$context/output" "$context/image"
# An immutable source snapshot also prevents edits during compilation from
# producing an executable that falsely claims the recorded commit.
git archive --format=tar "$source_sha" > "$context/source.tar"
tar -xf "$context/source.tar" -C "$context/source"
docker run --rm --platform linux/amd64 \
    -v "$context/source:/src:ro" -v "$context/output:/output" \
    -v beszel-intel-build-mod:/go/pkg/mod -v beszel-intel-build-cache:/root/.cache/go-build \
    -w /src -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=amd64 -e GOTOOLCHAIN=local \
    -e "BESZEL_REQUIRED_GO=$go_version" "$go_image" sh -eu -c '
        test "$(go env GOVERSION)" = "go$BESZEL_REQUIRED_GO"
        go build -mod=readonly -buildvcs=false -trimpath -ldflags="-w -s" \
            -o /output/agent ./internal/cmd/agent
    '
binary_sha=$(shasum -a 256 "$context/output/agent" | awk '{print $1}')
cp "$context/output/agent" "$context/image/agent"
cp "$context/source/docs/gpu-intel-truenas/Dockerfile" "$context/image/Dockerfile"
image="beszel-intel-lab:$commit"
docker build --platform linux/amd64 \
    --label "org.opencontainers.image.revision=$source_sha" \
    --label "dev.beszel.agent.binary.sha256=$binary_sha" \
    --label "dev.beszel.agent.go.version=$go_version" \
    --label "dev.beszel.agent.go.image=$go_image" \
    -t "$image" "$context/image"
mkdir -p build
cp "$context/output/agent" build/beszel-agent-linux-amd64
cat > "build/beszel-intel-lab-$commit.provenance.json.tmp" <<EOF
{"image":"$image","source_sha":"$source_sha","binary_sha256":"$binary_sha","go_version":"$go_version","go_image":"$go_image","source_dirty":false,"goos":"linux","goarch":"amd64"}
EOF
# Use portable file names rather than the image tag's colon.
mv "build/beszel-intel-lab-$commit.provenance.json.tmp" "build/beszel-intel-lab-$commit.provenance.json"
printf 'Built %s from %s; binary SHA-256 %s\n' "$image" "$source_sha" "$binary_sha"
