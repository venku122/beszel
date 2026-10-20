# Intel sysfs reliability review

This branch changes collector reliability, not GPU schema or experimental Xe
telemetry. Full previous investigation is preserved separately on
[archive/intel-truenas-evidence](https://github.com/venku122/beszel/tree/archive/intel-truenas-evidence/docs/gpu-intel-truenas).
Those reports describe additional feature branches not included in this PR.

## Corrections following adversarial review

- Every Intel filesystem operation occurs outside the GPU manager lock. Only
  validated state is committed under lock. A stalled Intel read cannot prevent
  hub GPU requests or an independent AMD collector from using that lock.
- Shutdown cancels the single Intel worker and waits at most one second. A
  kernel-stalled read cannot be cancelled by Go context; it may remain blocked
  until process exit or kernel return. No replacement reader or growing task
  queue is created. Stopped workers discard late results and skip later cards.
- Sensor freshness starts at the energy observation, not a delayed commit.
- Discovery and polling deduplicate physical devices, preserving the original
  card key and display name across disappear/reappear within this process.
  Identity/name caches are bounded to 64 devices; no cross-restart migration.
- Discovery checks regular-file metadata without opening energy sensors.
  Readability is established when polling, not guaranteed by startup detection.
  Priming, valid measurements and failed intervals are distinct. Failures use
  capped backoff; long gaps re-prime without a permanent retry trap. Warnings
  occur on first failure and at most once per minute per device, with INFO
  recovery only after a valid measurement. Unavailable readings are not zero.
- Development images compile an immutable, clean committed source snapshot
  using a digest-pinned Go image matching go.mod. Full source and binary SHA256
  are recorded in image labels and build/*.provenance.json. Existing binaries
  are never trusted; dirty trees and compile failures stop the workflow.

## Regression evidence

The new FIFO fixtures first reproduced hub mutex stalls and shutdown waits on
886716f4. Tests now cover responsive hub reads, one-second shutdown, late-result
rejection, no replacement worker, cancellation before a second blocked card,
and energy-observation freshness. Duplicate-PCI polling, full device removal /
recreation with stable names, unusable intervals, unreadable regular files,
rate-limited warnings and measured recovery have dedicated regressions.
Three build-workflow tests reject stale executable reuse and dirty modified /
staged / untracked source, and stop on compilation failure.

Commands run in a Go 1.27.2 Linux container as UID65534:

```sh
go test -tags='testing no_ui' ./...
go vet ./agent/...
go test -race -tags='testing no_ui' ./agent/...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-w -s' -o build/beszel-agent-linux-amd64 ./internal/cmd/agent
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o build/beszel-agent-linux-arm64 ./internal/cmd/agent
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o build/beszel-agent-darwin-arm64 ./internal/cmd/agent
python3 docs/gpu-intel-truenas/test-build-image.py
```

Current corrective revision validation and hardware measurements are recorded
below after completion; previous measurements alone do not validate new code.

## Prior hardware evidence (before these corrections)

TrueNAS SCALE25.10, Linux6.12.33, Xe B570. Upstream omitted card power in 119/180
one-second realtime updates; the original reliability source retained 1800/1800
over 30 minutes. Controlled 4K HEVC Main10 HDR/DoVi → H264 1080p confirmed VAAPI
decode/encode and return to idle. This establishes the original continuity fix,
not a new hardware acceptance result. A380 and live suspension remain untested;
no Intel temperature sensor is exposed. Wire/UI availability limitations remain.

## Isolated deployment and rollback

[Compose](compose.yaml) uses a dedicated lab identity and managed volume,
read-only /sys, all capabilities dropped, private PID namespace, no Docker
socket/render node, and no public ports. Never share production state or keys.
[Mac hub](hub.compose.yaml) stays LAN-only on port18090. Create/update only the
TrueNAS Custom App `beszel-intel-lab`. app.update preserves STOPPED state; start
only this lab explicitly when needed. Stop only this lab to roll back; preserve
its managed storage. No production service, kernel, PCI or PM changes are needed.

Build from a clean committed checkout with `sh docs/gpu-intel-truenas/build-image.sh`.
Only /agent changes in the pinned official Intel image. Transfer with docker
save/load; do not publish the development image. Check source/binary provenance
before starting a measurement. Large logs, images and binaries remain outside Git.
