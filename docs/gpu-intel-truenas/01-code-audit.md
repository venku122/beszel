# Audit against upstream ca7f75ef

Latest release v0.21.0 (2026-10-02); go.mod requires Go 1.27.2.
Clone reused safely via sibling worktree; original PoC branch untouched.

- PR [2020](https://github.com/henrygd/beszel/pull/2020) merged Aug 16:
  Intel sysfs energy collector already present; package averaging fix retained.
- PR [2223](https://github.com/henrygd/beszel/pull/2223) merged Aug 16:
  Xe nvtop support/image utilities already present; do not duplicate it or
  require its host PID deployment for the sysfs collector.
- PR [2256](https://github.com/henrygd/beszel/pull/2256) merged Aug 24:
  continuity fix only exempts GPUs with Engines != nil; sysfs Xe still blanks.
- PR [2313](https://github.com/henrygd/beszel/pull/2313) merged Sep 24:
  SKIP_GPU sensor/fan exclusion already present; preserve it.
- PR [2562](https://github.com/henrygd/beszel/pull/2562) open at inspection:
  proposed custom metrics; no dependency or schema adoption here.

Inspected gpu.go, gpu_intel_sysfs_linux.go and tests, sensors.go, fans.go,
internal/entities/system/system.go, hub/systems/system_realtime.go,
internal/records/records.go, alerts/alerts.go, frontend gpu-charts.tsx,
Makefile, internal/dockerfile_agent_intel and GitHub docker/release workflows.

Confirmed problems: permanent exit after repeated failures; discovery reads
energy just to test presence; unbounded optional reads; missing label checks;
first/invalid intervals counted as zero power; stale carry behavior inferred
from unrelated temperature/memory availability. Regression tests demonstrated
continuity failure, nonfinite values, counter-label/source mistakes and
cross-cache carry errors before fixes.

Serialization preserves existing field numbers/types and database. Temperature
is not a GPU chart field; sensors are a separate path. Usage is a scalar with
zero default even when unsupported. Power zero is omitted by existing encoding;
frontend absent-value coercion and rollup missing-value semantics remain an
upstream follow-up, not a claim of end-to-end unavailable-metric correctness.

Official pinned image is henrygd/beszel-agent-intel:0.21.0 (not an -intel tag
on beszel-agent). amd64, /agent entrypoint, Alpine 3.24.2; includes
intel_gpu_top, nvtop, smartctl and zpool. Lab keeps these utilities intact.

Additional independent hub defect: `AverageSystemStatsSlice` accumulates and
averages GPU Power, but omits PowerPkg. A regression reproduces 20/40 W package
samples becoming 0 W in a historical tier. Separate branch
`fix/gpu-package-power-rollup` adds the two existing-field operations and tests
repeat aggregation. No migration, new field or collector dependency. The lab
hub remains the official 0.21.0 image, so this fix is unit-tested rather than
claimed deployed. Realtime hardware validation is unaffected by that rollup bug.
