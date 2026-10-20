# Phase 1 design

Keep existing GPU wire fields and cardN hub keys. Internal sample freshness must
replace the temperature/memory suspension inference specifically for Intel
sysfs; preserve behavior of other collectors. Hold a valid sample across short
realtime gaps, omit failed/stale samples, and only count valid power intervals.
First samples, resets and nonpositive time deltas are not measured zero power.
Zero energy delta over a valid interval is measured idle zero.

Rediscover periodically and after failures, retry with bounded backoff, and
cancel on agent shutdown. Keep all shared state under the GPU manager mutex.
Sensor reads must have byte bounds and reject nonfinite/negative values.
Honor energy labels; retain unlabeled energy2 mapping only as documented legacy
Xe compatibility. Never interpret a labeled memory counter as package power.

No schema, frontend, database, default privilege or production changes.
Identity migration and optional frequency/PM/DRM work follow verified Phase 1.

Implemented refinements: each cache snapshot stores its own carried average;
package power uses its own valid interval count and source-specific baseline.
Discovery metadata is inspected each poll (bounded by DRM sysfs contents), with
3s successful cadence and 6/12/24/48/60s failures, capped at one minute.
First observed cardN identity is retained through PCI device symlink resolution
within a process. At most 64 lifetime hardware identities are retained; no
persistent migration or restart identity guarantee is introduced.
Counter decreases re-prime conservatively (counter width is unknown; do not
invent wrap arithmetic). Reject nonpositive or >15s intervals and >2000 W
samples as conservative corruption guards, without clipping to power limits.
Freshness expires at 10s; kernel-blocked reads remain an OS limitation.
