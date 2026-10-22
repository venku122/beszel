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
- Discovery checks regular-file and effective-user permission metadata without
  opening energy sensors.
  Readability is established when polling, not guaranteed by startup detection.
  Priming, valid measurements and failed intervals are distinct. Failures use
  capped backoff; long gaps re-prime without a permanent retry trap. Warnings
  occur on first failure and at most once per minute per device, with INFO
  recovery only after a valid measurement. Unavailable readings are not zero.
- Development images compile an immutable, clean committed source snapshot
  using a digest-pinned Go image matching go.mod. Full source and binary SHA256
  are recorded in image labels and build/*.provenance.json. Existing binaries
  are never trusted; dirty trees and compile failures stop the workflow.

## Second review corrections — 2026-10-21

- Package-power availability now follows the valid package count in each
  consumer's interval. A missing final package reading no longer discards
  earlier valid historical measurements. Realtime carry still clears package
  power after an unavailable sample; unavailable intervals never dilute averages.
- Failed hwmon path revalidation rejects the sample, including ENOENT during
  removal. Rejected reads clear freshness and counter baselines, so a recreated
  sensor must prime before reporting power.
- Primary energy failures invalidate baselines immediately and skip optional
  package, temperature, utilization, memory and name reads.

The new regressions failed against 9b0a076 before implementation: historical
package power was zero, removed hwmon readings committed, and primary failure
blocked on an optional FIFO. Corrected regressions passed 20 repetitions on
Linux/amd64. Independent review found no production-code blocker and identified
one FIFO fixture cleanup race, corrected with bounded rendezvous and worker
completion checks.

Fresh Go 1.27.2 container checks (UID65534):

```sh
go test -tags='testing no_ui' ./agent -run 'TestIntelSysfs|TestGPU' -count=1 -timeout=90s
go test -tags='testing no_ui' ./agent -run 'TestIntelSysfs(PackageMissing|PackageAvailability|DisappearingHwmon|PrimaryFailure)' -count=20 -timeout=60s
go test -p 1 -count=1 -tags='testing no_ui' ./...
go vet ./agent/...
go test -race -count=1 -tags='testing no_ui' ./agent/...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-w -s' -o /tmp/beszel-agent-second-review ./internal/cmd/agent
```

Focused tests, race checks and the cross-build passed on Linux/ARM64; repeated
regressions, the serialized full suite and vet passed on Linux/amd64. The final
FIFO cleanup adjustment also passed the focused race suite. These corrections
have **not** been deployed or tested on real hardware. The October 20 results
below belong to source e89dbb63ae04, not this revised runtime. No TrueNAS or
production services were changed during this pass. Existing kernel-blocked-read,
sleep-state and wire/UI availability limitations remain.

## Regression evidence

The new FIFO fixtures first reproduced hub mutex stalls and shutdown waits on
886716f4. Tests now cover responsive hub reads, one-second shutdown, late-result
rejection, no replacement worker, cancellation before a second blocked card,
and energy-observation freshness. Duplicate-PCI polling, full device removal /
recreation with stable names, unusable intervals, unreadable regular files,
rate-limited warnings and measured recovery have dedicated regressions.
Three build-workflow tests reject stale executable reuse and dirty modified /
staged / untracked source, and stop on compilation failure.

Go checks run in Go 1.27.2 Linux containers as UID65534:

```sh
go test -tags='testing no_ui' ./...
go vet ./agent/...
go test -race -tags='testing no_ui' ./agent/...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o build/beszel-agent-linux-arm64 ./internal/cmd/agent
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o build/beszel-agent-darwin-arm64 ./internal/cmd/agent
```

The Mac ran `python3 docs/gpu-intel-truenas/test-build-image.py` and
`sh docs/gpu-intel-truenas/build-image.sh`; the latter compiles Linux/amd64
inside its pinned Go container from an immutable source snapshot.

Linux/amd64 passed the full suite and vet; Linux/ARM64 ran agent race tests and cross-builds. All 47 selected Intel
parent/subtest entries also passed on TrueNAS as UID65534 with synthetic files,
read-only root, bounded tmpfs, no devices/capabilities/network or host PID access.
Independent review found no remaining blocker after the timing/permission fixes.

## Prior hardware evidence (before these corrections)

TrueNAS SCALE 25.10, Linux 6.12.33, Xe B570. Upstream omitted card power in 119/180
one-second realtime updates; the original reliability source retained 1800/1800
over 30 minutes. Controlled 4K HEVC Main10 HDR/DoVi → H264 1080p confirmed VAAPI
decode/encode and return to idle. This establishes the original continuity fix,
not a new hardware acceptance result. A380 and live suspension remain untested;
no Intel temperature sensor is exposed. Wire/UI availability limitations remain.

## Isolated deployment and rollback

[Compose](compose.yaml) uses a dedicated lab identity and managed volume,
read-only /sys, all capabilities dropped, private PID namespace, no Docker
socket/render node, and no public ports. Never share production state or keys.
[Mac hub](hub.compose.yaml) stays LAN-only on port 18090. Create/update only the
TrueNAS Custom App `beszel-intel-lab`. app.update preserves STOPPED state; start
only this lab explicitly when needed. Stop only this lab to roll back; preserve
its managed storage. No production service, kernel, PCI or PM changes are needed.

Build from a clean committed checkout with `sh docs/gpu-intel-truenas/build-image.sh`.
Only /agent changes in the pinned official Intel image. Transfer with docker
save/load; do not publish the development image. Check source/binary provenance
before starting a measurement. Large logs, images and binaries remain outside Git.

## Fresh corrective hardware acceptance — 2026-10-20

Tested source **e89dbb63ae04a130b251bd3897700aef3fb681b5**, incorporating upstream
**0fb738de**. Image `beszel-intel-lab:e89dbb63ae04`; actual deployed /agent SHA256
`99191d040afe8059b7878eecd62ba36b8abaa1bacfd78245a71309164dcb547e` matches
both the image label and provenance manifest. Through 9b0a076, later
edits changed only documentation/lock comments. The October 21 corrections above
change runtime behavior and have fixture validation only. No experimental
telemetry code was added. Older experimental branches need rebasing onto this corrected core.

With `GPU_COLLECTOR=amd_sysfs,intel_sysfs`, **1800/1800** one-second observations
contained both AMD Cezanne and Intel B570. No Intel card/package power fields
were missing. Maximum update gap **1.085 s**. A fresh mixed subscription received
20/20 more updates, proving collection continued beyond the initial capture.

| Phase | Updates | Mean Intel card W | Mean package W |
| --- | ---: | ---: | ---: |
| Idle, at least 15 minutes | 903 | 30.465 | 15.901 |
| Controlled transcode | 182 | 61.027 | 38.056 |
| Return to idle, including tail | 715 | 30.624 | 16.018 |

Plex confirmed VAAPI decode/encode and full hardware pipeline; the specific
controlled transcoder command confirmed H.264 1080p scaling from 4K HEVC10-bit
HDR/DoVi. The test session stopped normally after about 182 seconds /145 HLS
segments. No pre-existing session was interrupted. Power is driver-derived,
not externally calibrated. AMD memory/power data remained present; these values
retain the existing AMD collector's accounting semantics.

Thirty-minute container resources: **0.17467% of one CPU core**, memory
**12.11–15.28 MiB**, first 12.11/last 14.69 MiB; PID/thread count **21–21**.
Agent process descriptors **6→6**, disk write_bytes **0→0**, no restart, and
zero GPU warning/error lines in the complete bounded log tail (4096-line cap).
This is a mixed-collector result, including brief snapshot commands; compared
with the earlier Intel-only 0.1400% result, delta is +0.03467 percentage points.
Different source/baseline, collector selection and workload windows prevent a
causal overhead claim. This window alone does not prove indefinite leak freedom.

PM-only lab-stopped baseline: 721 observations over 180s, all active,
+180000ms active/+0 suspended. Mixed run: 1801 observations over 1800s, all active,
+1800000ms active/+0 suspended. Other GPU clients stayed untouched. There is no
live sleep-preservation or power-saving proof. No GPU reset/module operation
was performed; disruptive recovery and alias scenarios remain fixture-only.

Final cleanup restored Intel-only selection on the corrected image: one startup
priming sample was unavailable, then 19/19 had power; the lab was STOPPED. Production Plex and the existing Beszel
container retain their original IDs/images/states/start times/restart counts.
Operational Plex token caches are removed; dedicated lab identity/storage and
images remain private. Detailed sanitized logs and provenance stay outside Git
in Mayor `artifacts/beszel-intel-v2/review-2026-10-20/`; exportable binaries,
image, patches and checksums are in `artifacts/beszel-intel-v2/review-release/`.

## Test isolation observation

A final native Linux/ARM64 default-parallel suite run failed
`TestAgentWebSocketIntegration/fingerprint_mismatch_should_be_rejected` during
cleanup: `remove /dev/shm/beszel_health: no such file or directory`. The shared
health-file cleanup uses os.Remove without ignoring absence, and package test
processes share /dev/shm. This failure is not a GPU assertion and is retained in
`review-native-all-final.log`. The isolated case passed 10/10. A serialized full
suite (`go test -p 1 -count=1 -tags='testing no_ui' ./...`) was run as a test
isolation workaround and passed in full. No unrelated health/runtime source
was changed to hide the failure. The missing file is consistent with shared
health-file use across parallel package test processes.
