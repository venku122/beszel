# Independent adversarial review

A separate review agent examined the diff and tests twice, without editing
files. This is source review, not an independent hardware/test certification.

Confirmed and fixed with reproductions:

1. Package energy source switch could delta unrelated counters. Baselines now
   include source path and remove old-source intervals.
2. Realtime cache consumer could carry another cache's average. Store each
   consumer's own previous average in its snapshot.
3. Legacy unlabeled energy2 could override an explicitly labeled pkg counter.
   Recognized labels now take precedence.
4. Bounded numeric reads could accept truncated prefixes. Read at most 65
   bytes and reject length >64 before parsing.

Re-review found no additional blocking source issue. Shared collector state is
protected by the manager mutex; retries are capped/cancellable; removed device
state is cleaned up, alias table bounded to 64 identities. No new dependencies,
capabilities, host PID namespace, socket mounts, or process scans in collector.

Remaining risks: kernel-blocked sysfs reads can block the manager mutex and
shutdown despite byte limits. Identity across restarts remains legacy cardN;
no migration is claimed. Zero/unavailable values retain existing wire/frontend
limitations (see code audit). No A380 is available for hardware regression.
A suspended-GPU fixture belongs to the separate optional PM feature, not proof
of sleep safety for default legacy polling.

Separate reviews:

- Historical package-power rollup fix: no blocker after regression reproduction
  and full tests. Existing denominator semantics when records omit a GPU remain
  outside its narrow scope.
- Xe prototype: no blocker. Observed active-state guard follows hwmon, so it
  cannot establish sleep preservation. The 64-domain cap bounds value reads;
  glob discovery still enumerates the kernel-owned hierarchy. Tests additionally
  cover missing PM, >64 domains and INFO doing no diagnostic file opens.
- Optional suspend mode: review found raw collector selection disagreed with
  whitespace/case normalization and cached sensor class aliases could follow
  a reused hwmon slot. Fixed with normalized selection and pinned physical
  paths. A second review found secondary `device/` fallback links could also
  redirect reads. Optional temperature shadows now flatten pinned attributes,
  optional fan caches pin the selected fallback directory. Both alias layers
  have CPU→Xe reassignment fixtures. Re-review found no remaining blocker.
  Physical paths can disappear/reappear; PM checks cannot eliminate autosuspend
  races. These fixture findings were not observed as live TrueNAS incidents.
- Standalone DRM probes: review checked query ABI/permission handling. Per-client
  sample timing was corrected and excessive observation-span/nonpositive timing
  tests added. Probe engine visibility is not complete device utilization.
