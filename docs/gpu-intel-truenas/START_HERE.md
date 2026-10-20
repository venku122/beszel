# Intel Arc / Xe TrueNAS validation

The first reliability delivery is implemented and verified on an isolated
TrueNAS B570 lab. No production deployment, upstream merge or public image
publication occurred. Source commit: **4aadc51a7e6f**, branch
`fix/intel-sysfs-reliability`, based on upstream **ca7f75ef**.

## Verified improvement

Upstream lost card-power fields in 119 of 180 one-second realtime updates.
The development agent produced 1800 updates with **zero missing power fields**
over 30 minutes through a separate official Beszel 0.21.0 hub. This verifies the
agent-to-hub realtime data path. The follow-up captured rendered power and VRAM
charts; see [feature verification](08-feature-verification.md). Fifteen idle minutes preceded controlled Plex transcodes and return
to idle. Source was 4K HEVC Main10 HDR/DoVi, output H.264 1080p; VAAPI decode and
encode plus the hardware pipeline were confirmed. Mean card power rose from
30.38 W idle to 61.05 W under the sustained stream, returning to 30.44 W.
Driver-derived power was not calibrated against an external meter.

Recovery, counter/label accuracy, cache-consumer isolation and bounded input
handling are covered by regression fixtures. Linux amd64 and ARM64 full suites
and vet passed; ARM64 agent race tests passed. Linux amd64/ARM64 and Darwin
ARM64 builds passed with required Go 1.27.2. Independent review identified
four reproducible core issues, all corrected and retested.

Resource observations: upstream 0.1227%, development 0.1400% of one CPU core;
unequal windows and development transcodes prevent causal overhead attribution.
Development memory was 11.63–15.30 MiB, descriptors 6→6, threads 20→20 and
write_bytes 0 in process snapshots. Detailed intervals/caveats are in
[hardware benchmarks](04-hardware-benchmarks.md).

## Deployment and rollback

TrueNAS custom app `beszel-intel-lab` used image
`beszel-intel-lab:4aadc51a7e6f`, managed private storage and a dedicated identity.
The Mac lab hub uses port 18090 and its own named volume. Agent has read-only
/sys, zero capabilities, private PID namespace, no /dev/dri or Docker socket,
and no published ports. Official Intel image utilities are preserved.
Stop and restart rollback were tested; 20 post-restart updates again contained
card and package power. Final application state is recorded below.
Production Plex and the pre-existing Beszel container retained their original
IDs, images, state, start times and restart counts. The latter was already
`created`, not running; it was not started. No kernel, ZFS, network, storage or
production app settings changed.

See [deployment/rollback](06-deployment-rollback.md), [Compose](compose.yaml)
and [Dockerfile](Dockerfile). Use separate generated lab secrets, never the
production identity. Large binary/image/log artifacts remain outside Git in
Mayor `artifacts/beszel-intel-v2/`; packaged deliverables and checksums are in
its `release/` directory. No production Plex token is part of the evidence.

## Separate work and limits

- `fix/gpu-package-power-rollup`, **a69cee2c**: independent, tested historical
  package-power aggregation fix. Deployed to a separate patched Mac lab hub in the follow-up; 10m/20m records verified.
- `feat/intel-xe-observability`, **1b1b7053**: collector-side debug diagnostic
  prototype; actual/requested frequency and throttle status stay per tile/GT.
  Normal logging adds no frequency reads. No hub schema/frontend changes.
- `feat/intel-sysfs-suspend-aware`, **fe47c9b8**: explicit opt-in experimental PM mode,
  developed and reviewed separately; final source and hardware status below.
- `experiment/xe-drm-telemetry`, **e69b61c9**: standalone bounded probes.
  Read-only render mapping with no capabilities returned 10666115072 usable
  VRAM bytes. The ioctl used-memory field is privilege restricted. A separate default-off
  debugfs workaround now supplies global allocated VRAM on this kernel. Scoped
  fdinfo binds verified per-client engine/memory observations without host PID;
  complete device utilization remains unavailable.

This kernel exposes no Intel temperature sensor. A380 hardware is unavailable.
Legacy wire/UI zero/missing-value limitations remain; unsupported utilization
must not be interpreted as measured idle. Stable PCI identity survives DRM
renumbering within a process; cross-restart historical renaming was avoided.
Optional Windows build needs upstream generated LHM assets and failed without
those assets. A kernel-blocked sysfs read can still delay collector shutdown.

Runtime-PM wakeups and resource overhead were measured with explicit limits:
baseline/dev samplers themselves read energy/frequency, and a PM-only 120-second
window with the lab stopped stayed active. No proven sleep residency or power
saving is claimed. Suspended-state behavior is fixture-tested, not demonstrated
on a sleeping live B570. No PM transitions were forced.

## Review navigation

[Inventory](00-hardware-inventory.md) · [Upstream audit](01-code-audit.md) ·
[Design](02-design.md) · [Tests](03-test-results.md) ·
[Benchmarks](04-hardware-benchmarks.md) · [Adversarial review](05-adversarial-review.md) ·
[Deployment](06-deployment-rollback.md) · [PR scopes/readiness](07-upstream-pr-plan.md)

No PRs or discussions have been posted; substantial UI/schema work remains
subject to maintainer discussion. Source changes are split into local commits
and branches, with exportable patches for review.

## Final state, 2026-10-20

The core image **4aadc51a7e6f was restored** after experimental checks and
produced 10/10 final updates with power. The lab app was then **STOPPED** via
TrueNAS app management; its managed data, identity and local images remain.
The separate Mac hub remains running for inspection, with its data retained.
Restart only `beszel-intel-lab` to resume the validated core profile. Optional
PM/debug settings are absent from its final configuration. Plex playback and
transcode session counts were both zero at final cleanup; production container
state was checked again and matched the pre-test snapshot exactly.

Optional PM image 2c65b04ff769 produced 60/60 power updates while active; its
combined debug profile produced 30/30. Both GT diagnostic values matched direct
sysfs. A later thermal reason flag was asserted without a temperature sensor;
see the benchmark caveat rather than interpreting it as proven overheating.
Sleep avoidance/savings, A380 behavior and
complete utilization remain unverified. Production Plex token caches used by
this task were removed after testing; dedicated lab credentials remain private.

## Follow-up verification, 2026-10-20

See [each feature and workaround](08-feature-verification.md). The optional PM
profile ran for 30 minutes: 1801 active-only PM observations; 1450 canonical
realtime updates with no GPU omissions; mean CPU 0.1542% of one core, 12.45–16.62
MiB container memory. Controlled VAAPI transcoding raised card/package power
from 30.42/15.91 W to 59.69/37.37 W. It returned to idle afterward. The lab-stopped
PM-only baseline was also always active; no sleep or power-saving claim follows.

`feat/intel-xe-debugfs-memory` adds an optional bounded reader of existing
root-readable debugfs TTM allocation counters, using existing memory fields.
No additional privileges/mounts are required here. Directory pinning prevents
DRM minor reuse from redirecting reads. The combined lab branch is for testing;
independent reviewable feature commits remain separate. Hardware verification confirmed 54.75 MiB idle → 1178.625 MiB transcode →
54.75 MiB idle allocated VRAM. Existing graphs render these values. The combined
profile used 0.25054% of one CPU core over five minutes with debug logging;
no isolated cost or sleep-safety claim is made. See the feature report.

Follow-up final state: core 4aadc51a7e6f restored, 20/20 post-restore updates
verified, lab STOPPED again. Separate Mac hub runs patched a69cee2c with original
hub/container backup retained. Production exact-state comparison passed again.
