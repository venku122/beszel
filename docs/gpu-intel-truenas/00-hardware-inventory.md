# Hardware inventory — 2026-10-19

Read-only SSH inventory, before implementation. TrueNAS SCALE 25.10.0,
Linux 6.12.33-production+truenas, x86_64. Intel B570 (8086:e20c,
ASRock subsystem 1849:6022), PCI 0000:12:00.0, `xe`, currently card0.
AMD Cezanne is card1 (`amdgpu`); A380 is not present.

| Interface | Exists | Readable by SSH user | Requires privileges | Observable wakeup |
| --- | --- | --- | --- | --- |
| Energy | yes, energy1/card and energy2/pkg | yes | no | inconclusive: already active |
| Temperature | no Intel temp inputs | n/a | n/a | n/a |
| Frequency | tile0/gt0 and gt1/freq0 | yes | no | unmeasured |
| Throttle reasons | both GTs | yes | no | unmeasured |
| Runtime PM | status + residency | yes | no | active before/after hwmon |
| DRM device query | yes, MEM_REGIONS verified | SSH denied; isolated read-only mapping succeeds | render mapping needed, no extra capabilities | inconclusive: already active |
| DRM fdinfo | per-client counters verified | own probe client yes; Plex from ordinary lab no | process ownership/namespace visibility required | inconclusive |

Before hwmon: runtime_active_time=2929897068 ms,
runtime_suspended_time=45770630 ms, runtime_status=active.
After hwmon: active. This does not prove wakeup safety.
Energy: 85153883374938 µJ card, 44732814925903 µJ pkg (sequential samples).
A final metadata-only existence check confirmed gpu_busy_percent and all
mem_info_vram/lmem/local_mem used/total candidates are absent on this device.
Core sysfs therefore cannot provide utilization or VRAM on this kernel.
GT0 act/cur/rpn/rpe/rp0/min/max MHz: 0/400/400/400/2750/400/2750.
GT1: 0/400/400/400/1500/400/1500. All observed throttle flags 0.
Power limits are configuration, not live power: power1_rated_max=35000000 µW,
power2_crit=320000000 µW; power1_max and power2_max both 0.
Kernel config files exist in /boot; not changed.

Portainer inspection: existing Beszel agent image 0.18.7-alpine,
container state `created` (not running); no GPU_COLLECTOR override.
No existing Beszel lab container found. Existing production configuration,
including its capabilities and mounts, was left untouched. This differs from
older reports that described a running May PoC. No production polling confound
was observed at this inspection; other GPU clients remain to be measured.
SSH user cannot access Docker socket or open renderD128/renderD129 read/write.
Portainer API used for metadata with existing repository client and scoped
self-signed TLS handling; no system TLS settings changed.

Kernel config confirms CONFIG_DRM_XE=m and CONFIG_DRM_I915=m;
xe is loaded, i915 was not listed. CONFIG_PM, PM_SLEEP and PCI enabled.
renderD128 maps to the B570's PCI device; renderD129 to AMD. Both are
0660 root:render. Ordinary lab container has no /dev/dri, no Docker socket,
CapEff=0 and a private PID namespace; selected Plex server host PID is absent.
Core collection therefore needs neither render-node access nor host PID access.

The full 301-sample / 900-second host baseline had +900145 ms active residency
and +0 ms suspended residency. This is an already-active baseline, not a
sleep/wakeup experiment or evidence that polling permits runtime suspension.

Experimental query (kept on `experiment/xe-drm-telemetry`): read-only renderD128
mapping with CapDrop ALL succeeded, returning 10666115072 usable VRAM bytes.
This is device total, not used VRAM. Used accounting is privilege-restricted.
Own-client fdinfo exposes cycles/total-cycles rcs, ccs, bcs, vcs, vecs; vcs/vecs
capacity=2. Plex's test transcode has two clients readable inside Plex's existing
UID/namespace, not from the ordinary lab agent. No global utilization inference.
The one-shot query took 78 microseconds on an already-active GPU; wakeups remain
unmeasured. The temporary probe container was removed after its read-only run.
