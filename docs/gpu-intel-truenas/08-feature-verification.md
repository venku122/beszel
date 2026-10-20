# Extended TrueNAS feature verification — 2026-10-20

This follow-up rechecks every implemented feature while preserving production.
The agent runs on TrueNAS; the separate Mac hub runs the existing-field package
power rollup patch. Experimental tools remain outside the normal agent image.

## Verified workarounds

- TrueNAS `app.update` preserves STOPPED state. Explicitly start the lab after
  updating its stopped configuration, then require hub status UP before logging
  a stability window. The initial pre-start SSE/resource attempts are excluded.
- The Python capture used a whitespace-bearing JSON subscription unlike the
  official client. The hub retains one topic key per system, so the browser got
  no realtime callbacks while that first topic owned the worker. Release both
  subscriptions and use compact JSON matching the official client. Concurrent
  browser/capture updates then work; no agent restart was needed. The intentional
  capture reconnect is distinguished from a collector gap. The broader hub
  topic-alias behavior remains an upstream follow-up.
- Xe busy and total cycle counters allow a per-client utilization ratio in the
  GPU clock domain. The standalone probe now supports this with capacity,
  reset/invalid/slow-read guards. It does not claim complete utilization.
- To test restricted client visibility, a separate probe can receive only one
  known process fdinfo directory read-only, using its existing UID, private PID
  namespace and zero capabilities. Actual hardware outcome is recorded below.

## Baseline and boundaries

With the lab stopped, 721 PM-only samples over 180 seconds all reported active:
+180000 ms active, +0 suspended. No energy/frequency/fdinfo or DRM query reads
were made by that observer. Policy permits autosuspend (`control=auto`, delay
1000 ms, d3cold_allowed=1), but this does not prove the driver can suspend now.
Four running applications have GPU device access; none was stopped or changed.
Kernel-journal access was denied. No privilege was added to obtain those logs.

No Intel temp attributes or Intel PMT telemetry class exist on this host.
Temperature remains unsupported through the allowed interfaces; a thermal-reason
bit is not an independent temperature reading. A380 is absent. No kernel, PCI,
power-management, networking, storage or production app settings were changed.

The complete Intel fixture suite ran on TrueNAS in an ordinary UID 65534
container, no devices/capabilities/network/host PID/sysfs mounts, static amd64
Go test executable, bounded tmpfs. All selected tests passed (56 pass lines,
including parent/subtest entries). This verifies suspended/unknown states,
resume priming, counter faults, rediscovery and both layers of sensor symlink
reassignment without resetting the real GPU.

## Hardware outcomes

### Extended PM and realtime run

Image 2c65b04ff769: 1801 PM observations over 1800.000 seconds, all active;
+1800001 ms active, +0 suspended. Canonical realtime: 1450 updates, zero omitted
GPUs, maximum gap 1.162 s. An earlier 460-second capture also retained power;
the intentional canonical-topic reconnect did not restart the collector.
Mean CPU 0.15417% of one core; memory 13049856–17428480 bytes, first 14200832,
last 13127680. PID/thread count 13–17. Workload and unequal baseline windows
prevent a causal overhead claim. Against the earlier 0.1400% core window this
is +0.0142 percentage points; against 0.1227% upstream, +0.0315 points.

| Canonical capture phase | Samples | Mean card W | Mean package W |
| --- | ---: | ---: | ---: |
| Idle subset | 454 | 30.424 | 15.912 |
| Controlled transcode | 183 | 59.688 | 37.368 |
| Return to idle, including tail | 813 | 30.572 | 15.982 |

A 15-minute idle period preceded the first transcode (the canonical subset starts
later). The permitted source was 3840×2160 HEVC 10-bit HDR/DoVi, output H.264
1920×1080; Plex reported full hardware pipeline and its controlled transcoder
command confirmed VAAPI decode, encode and scaling. Only the lab session was
stopped normally. No existing session was interrupted.

### Frequency and raw throttle flags

New diagnostic commit 1b1b7053 uses `throttle_status`, retaining per-GT identity.
Both direct files and agent logs show GT0 2750 MHz, GT1 1500 MHz under transcode;
actual idle clocks are 0 MHz. Requested clocks can retain a previous request.
Idle thermal/status flags can assert while active-workload flags clear. Without
a temperature reading this is raw driver status, not proven overheating or a
workload performance diagnosis. Frequency reads remain debug-only.

### Historical package power

Mac hub a69cee2c retains existing PowerPkg fields: first new 10m bucket reports
30.45/15.91 W card/package; next 39.51/22.56 W. The derived 20m bucket reports
34.98/19.24 W, consistent with those tier averages. Earlier official-hub 10m/20m
records omitted package power. No database migration or frontend change.
The official lab hub container and a coherent data-volume backup are retained.

### Native DRM query and scoped fdinfo

A read-only render-node query with capabilities dropped returns **10666115072
bytes usable VRAM** in about 110 microseconds; used memory is not exposed by
this ioctl without the kernel's additional privilege check. No privilege added.

A separate UID1000 probe receives exactly one known controlled Plex process's
fdinfo directory read-only. Private PID namespace, no DRM device, no capabilities,
no network. Two client IDs were observable, deduplicated with device identity.
GPU-clock busy/total-cycle ratios: VCS 27.55–30.78%, VECS 22.36–24.88% for one
client; CCS 91.37–95.12% for the OpenCL tone-mapping client. These are observed
per-client ratios, not complete device utilization. Resident VRAM 1171275776
and 7127040 bytes is reported per client, with shared accounting separately;
do not sum overlapping categories or infer global physical usage from them.

Two directory scans took about 1.03–1.89 ms; Python probe CPU 2.34–2.85 ms per
pair, excluding startup/container orchestration. No host-wide process scan.
One pair reported one FD read error while both DRM clients remained observable;
cause unproven, partial visibility remains explicit. Linux 6.12 Xe fdinfo reads
take a runtime PM reference, so this probe was run only during active work.
Seven Python tests cover cycle arithmetic, invalid/reset/capacity/bounds and
memory conversion, including overflow. Default agent does not include Python.

### Optional global allocated VRAM workaround

The existing read-only /sys mount exposes Xe `vram0_mm` to the lab agent's
existing root user, even with CapEff zero. Linux 6.12 TTM `size`/`usage` count
manager-wide usable/allocated bytes, including driver allocations. This is a
separate default-off experimental feature (`INTEL_XE_DEBUGFS_MEMORY=true`),
not an ioctl workaround that broadens privileges. Restricted/non-root debugfs
remains unsupported; do not loosen permissions. No extra mounts or capabilities.

Feature source d57309ff; combined validation branch 038cd368a7e5 includes PM
fe47c9b8 and diagnostic 1b1b7053. All Linux Go tests, agent vet/race and Linux
amd64/Darwin ARM64 builds passed. Independent review caught a DRM-minor reuse
race; pinned directory-relative reads and identity revalidation fix it, with a
reassignment fixture. The combined Intel fixture binary also passed on TrueNAS (67 parent/subtest
pass entries) as UID65534 with no devices/caps/network, using synthetic files only.

The second controlled transcode lasted about 183 seconds, consuming 146 HLS
segments, with VAAPI decode/encode confirmed. Global allocated VRAM rose from
57409536 bytes (54.75 MiB) to 1235873792 bytes (1178.625 MiB), then returned to
57409536 bytes after normal session stop. Usable total remained 10666115072
bytes (10172 MiB), identical to the independent query. Existing wire fields
`mu`/`mt` and the rendered VRAM graph agreed with these values. Direct/sysfs
and diagnostic clocks agreed at 2750/1500 MHz active, 0/0 after return to idle.
Effective capabilities remained zero; six agent descriptors at both snapshots,
no restarts, no extra device/host PID/socket access. This is a short feature
check, distinct from the earlier 30-minute PM stability window.

Screenshots `active-vram-power-chart.png` and `active-power-chart.png` show
real lab telemetry. The legacy utilization plot's zero is unsupported encoding,
not an idle measurement. No frontend/schema modifications were necessary.

Sanitized logs, container proofs and screenshots remain in Mayor
`artifacts/beszel-intel-v2/verification-2026-10-20/`; binary/image artifacts stay
outside Git. No actual GPU reset, driver reload or sleep was forced.

## Implementation and acceptance boundaries

TTM/Xe byte semantics and PM implications were checked against Linux 6.12:
[VRAM manager](https://github.com/torvalds/linux/blob/v6.12/drivers/gpu/drm/xe/xe_ttm_vram_mgr.c),
[TTM resource accounting](https://github.com/torvalds/linux/blob/v6.12/drivers/gpu/drm/ttm/ttm_resource.c),
[Xe client accounting](https://github.com/torvalds/linux/blob/v6.12/drivers/gpu/drm/xe/xe_drm_client.c),
[DRM usage statistics](https://docs.kernel.org/gpu/drm-usage-stats.html).
The counter's allocated-memory semantics are broader than application memory.
An unstable debugfs ABI is a material upstream acceptance risk; keep opt-in and
separate from the core reliability change. No maintainer message/PR was sent.
The scoped fdinfo mount is an explicit PoC alternative, not a default deployment
change or process auto-discovery solution. No full-system utilization claim.

Unavailable temperature, absent A380 and unobserved live suspension cannot be
resolved within the agreed permission/workload boundaries. Synthetic tests cover
suspended/unknown/resume behavior and disruptive failure scenarios. No production
process, kernel, PCI or power-management configuration was changed to force proof.

## Combined profile resource window

353 realtime observations over 352 seconds: two startup samples unavailable
while energy primed, then **zero omissions** after the first valid GPU reading;
maximum update gap 1.032 seconds. The capture was intentionally stopped before
rollback, not a collector termination. Thirty-one resource samples over 300.131
seconds measured **0.25054% of one core**, container memory
16240640–17321984 bytes (about 15.49–16.52 MiB).
Compared with the longer PM-only profile's 0.15417%, this is +0.09637 percentage
points. Debug logging/frequency reads, debugfs sampling, workload proportions and
unequal windows differ; this is not an isolated debugfs overhead measurement.
Descriptors remained six in idle/active/final process snapshots; restart count
zero. A normal INFO-mode memory-only cost comparison remains future work.

## Final rollback and production proof

Restored core image 4aadc51a7e6f with PM/debug/debugfs-memory settings absent;
20/20 fresh realtime updates contained card/package power. Stopped only
`beszel-intel-lab` through TrueNAS app management. Dedicated storage and images
remain; the patched Mac lab hub remains available with its retained original
container and coherent backup. Production Plex and existing Beszel container
IDs, images, states, start times and restart counts exactly matched this turn's
pre-test snapshot. Plex playback and transcode counts were zero; temporary
probe containers were removed. Task-created operational production-token caches
were removed after final checks. No production deployment or upstream action.
