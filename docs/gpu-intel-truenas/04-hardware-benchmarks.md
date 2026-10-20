# Hardware benchmarks — 2026-10-19/20

Pinned upstream ca7f75ef and development agent 4aadc51a7e6f, identical pinned Intel image base and lab configuration, official separate 0.21.0 hub. Production Beszel container was already in created state and remained untouched.

## Realtime continuity

| Agent | Updates | Missing card-power fields | Largest update gap |
| --- | ---: | ---: | ---: |
| upstream | 180 | 119 | 1.084 s |
| development | 1800 | 0 | 1.075 s |

## Power under controlled workload

Values are driver energy-derived card/pkg measurements, not an external wall-power calibration. Do not sum card and package readings.

| Window | Samples | Card mean (range), W | Package mean, W |
| --- | ---: | --- | ---: |
| Development idle (first 15 min) | 900 | 30.38 (28.45–30.72) | 15.88 |
| Transcode 1 | 182 | 60.61 (30.49–62.27) | 37.76 |
| Transcode 2 (sustained segments) | 182 | 61.05 (30.41–62.39) | 38.07 |
| Return to idle (after 6s drain) | 391 | 30.44 (30.25–30.71) | 15.90 |

Both streams: 3840×2160 HEVC, 10-bit HDR/DoVi source; process confirms VAAPI decode, scale_vaapi=w=1920:h=1080, OpenCL tone mapping and h264_vaapi. Transcode sessions report hardware decode/encode=vaapi and full pipeline=1. First stream consumed 12 initial HLS segments while transcode remained active; second corrected consumer advanced through 147 segments over about 3 min. Both stopped through normal Plex API. No existing playback/transcode sessions at their starts. No service restart or config changes.

Active snapshot: GT0 actual/requested=2750 MHz, GT1=1500 MHz. All exposed throttle flags/status were zero in that snapshot; not a claim of continuous absence of throttling. Idle actual clocks were zero while requested clocks could be 400 MHz. No Intel temperature sensor is exposed.

## Agent resources

| Agent | Resource observation | CPU, % of one core | Memory range, MiB | Final memory, MiB | Threads/pids observed |
| --- | ---: | ---: | ---: | ---: | --- |
| upstream | 190.1 s | 0.1227 | 11.93–15.56 | 15.37 | [10, 20] |
| development | 1811.2 s | 0.1400 | 11.63–15.30 | 14.61 | [20, 20] |

Observed CPU delta: +0.0173 percentage points of one core. Windows differ in duration and the development window includes two transcodes; this is not a controlled estimate of CPU cost attributable solely to the patch. Continuity and recovery provide demonstrated value, but zero overhead is not claimed.

## Runtime power management and limitations

Both host samplers read runtime status/residency before energy, at a 3s cadence. They also read frequency/energy and may themselves wake the GPU. The upstream agent runs only during a shorter part of the 15min host baseline. These windows therefore cannot isolate collector-induced wakeups.
- baseline: 900.145 s, active +900145 ms, suspended +0 ms; energy means card 30.457 W/pkg 15.897 W.
- development-hardware: 1800.303 s, active +1793486 ms, suspended +6817 ms; energy means card 36.681 W/pkg 20.429 W.

No conclusion that default polling preserves sleep residency. No full host PID visibility, no trusted global utilization, no privileged used-VRAM accounting, no A380 hardware. Collector warning/error log count at INFO was zero in inspected logs; no dedicated collector error counter exists. Realtime missing-field checks are separate stronger continuity evidence. Kernel-blocked read latency and long-term behavior beyond this run remain limitations.

Raw sanitized telemetry/resource logs and binaries are local Mayor artifacts, outside Git. `sanitized-telemetry.csv` contains a decimated GPU-only sample. Descriptor/IO snapshots, rollback and final state are in START_HERE.md.

A separate PM-only window after stopping the lab sampled 121 times over about
120 seconds. Every status was active and suspended residency did not increase.
No forced PM transitions were attempted. This does not establish a collector
sleep benefit or identify why the host remained active.

Development process snapshots: open descriptors 6 → 6, threads 20 → 20,
RSS 11392 → 12732 kB, write_bytes 0 throughout the measured run. Memory did
increase by about 1.3 MiB between these two snapshots; bounded-window stability
is not proof of indefinite leak freedom. Stop/start rollback succeeded; 20
post-restart realtime updates contained card and package power.

## Optional-mode follow-up

The opt-in PM branch 2c65b04ff769 was deployed to the same isolated app after
core validation and rollback testing. Sixty one-second updates at normal INFO
logging all included card power (mean 30.47 W). This short active-state check is
not an extended stability test and does not replace the 30-minute core run.
Additional debug diagnostics were tested separately and involve frequency
reads; their effect must not be conflated with the normal optional mode.

The combined optional-mode/debug image produced 30 further realtime updates
with no missing card power. Nine debug records retained both GT identities;
a separate direct sysfs snapshot agreed: actual clocks 0/0 MHz, requested
2750/1500 MHz, throttle status 1 and thermal reason 1 on both GTs, PL1/PL2 0.
This differs from the earlier active-transcode snapshot with zero flags.
Without an Intel temperature sensor or corroborating diagnostic, this is a
reported driver reason, not a proven overheating diagnosis. No cooling, GPU,
power-limit or production configuration was changed in response.
[Sanitized raw snapshot](xe-diagnostic-sample.json).

A 121-sample PM-only observer across the optional-mode/debug follow-up saw only
active status, +120040 ms active and +0 ms suspended residency. The debug
segment deliberately adds frequency reads and restarts the lab agent, so this
window cannot quantify the PM-only mode's effect in isolation.

## Follow-up extended PM and feature verification

See [feature verification](08-feature-verification.md) for measured power, CPU,
memory, PM residency, historical rollups, scoped engines and optional VRAM.
Thirty-minute PM window: all active, no suspended residency. Mean CPU 0.15417%
of one core; container memory 12.45–16.62 MiB and ended below starting memory.
These are observed absolute values, not a controlled isolated cost estimate.
No production collector/application was disabled to isolate sleep.
