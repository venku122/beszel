# Upstream PR plan

## PR 1: Intel sysfs correctness and resilience

Branch fix/intel-sysfs-reliability, base ca7f75ef, source commit 4aadc51a7e6f.
Core: sample continuity, independent cache averages, valid interval counting,
bounded reads, recovery/backoff/cancellation, source-aware counter baselines,
within-process stable PCI identity. No schema/frontend or database changes.
Tests and independent review are complete; hardware evidence is recorded in
START_HERE and 04-hardware-benchmarks. No upstream PR/merge or production rollout
has been performed. Raw artifacts stay local rather than entering Git.

Suggested title: fix(agent): retain and recover Intel sysfs GPU samples safely

Suggested description:

Intel sysfs GPUs without temperature or memory attributes lose power readings
between three-second samples when the hub requests one-second updates. Preserve
fresh averages per consumer, omit invalid/stale samples, and rediscover devices
with cancellable capped retries. Validate energy labels/sources and intervals,
reject oversized/nonfinite inputs, and retain card identity through within-run
DRM re-enumeration. Existing GPU wire keys and historical series stay unchanged.

Validation: nonroot Linux amd64 and ARM64 full Go suites and vet, ARM64 agent
race suite, Linux amd64/ARM64 and Darwin ARM64 cross-builds, fixture regressions,
independent adversarial review and isolated TrueNAS B570 hardware comparison.
Known limitation: existing wire/UI zero and missing-value semantics remain.

## PR 2: Xe observations

Implemented on `feat/intel-xe-observability` at 1b1b7053 after PR1 hardware
validation, currently diagnostic-only. Per-tile/per-GT
frequencies and throttle flags must retain GT identity, never summed clock
values. B570 frequency/throttle files exist; temperature does not. No hub
schema/frontend work before maintainer discussion. Do not depend on open #2562.

## PR 3: optional suspend-aware polling

Implemented on `feat/intel-sysfs-suspend-aware` at fe47c9b8, depending on
PR1 and the Xe diagnostic prototype. Default legacy polling is retained.
Active-state hardware checks supplement synthetic suspended-state tests; no
live sleep-preservation evidence, so this remains experimental.
Runtime PM status is read
before hwmon and unknown states remain unknown. Skip hwmon while suspended,
re-prime on resume, and test missing/malformed status plus reset/gap cases.
Power/residency benefit cannot be claimed from already-active observations.

## PR 4: native DRM, conditional

Standalone read-only query PoC is separate from agent integration. Querying total
VRAM may be feasible with a render device mapping, but used VRAM requires
privileges according to the Linux v6.12 ABI. Host process visibility for Plex
fdinfo is not assumed inside private PID namespaces. No added default
capabilities, PID namespace, dependencies or full /proc polling for any PR.

Publish neither speculative features nor claims of unmeasured sleep safety.
A380 hardware testing remains unavailable. Maintainer discussion draft should
ask about zero/unavailable encoding and GT display conventions before proposing
frontend/database work. No external discussion has been posted automatically.

## Independent corrective PR: package power historical rollups

Branch `fix/gpu-package-power-rollup`, base ca7f75ef, no dependency on PR1.
Preserve existing GPU PowerPkg during accumulation and division, with a
reproduced failing regression and repeated-tier test. No new schema fields or
migration; this fixes disappearing historical package power, separately from
realtime collector continuity. The isolated Mac hub now verifies both 10m and 20m aggregation; production remains unchanged.

DRM probe source is isolated on `experiment/xe-drm-telemetry`, not bundled into
the reliability PR. Its hardware result proves usable VRAM total via a read-only
render mapping without capabilities. Used memory and host-complete engine
utilization do not meet default-deployment requirements and are not integrated.

## Additional experimental branch after hardware discovery

`feat/intel-xe-debugfs-memory` d57309ff depends only on core PR1 and is separate
from native DRM probes. Default-off, bounded, identity-pinned TTM allocation
counter reader reuses existing GPU memory fields. It requires existing readable
debugfs and remains kernel-specific; upstream acceptance needs maintainer input
on unstable diagnostic ABI and the meaning of allocated usable VRAM.
`lab/intel-xe-verified-features` 038cd368a7e5 combines the separate feature commits
for hardware validation, not one enormous upstream PR.
DRM PoC e69b61c9 adds scoped fdinfo cycle ratios; retain as a standalone tool.
It does not meet global-utilization/default-process-discovery acceptance.
