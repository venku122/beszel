# Build and test results

Go 1.27.2 official Linux container, module requirement respected. Native Mac Go
1.27.1 was not used to compile the implementation. No go.mod/go.sum changes.

Commands run inside Linux, from repository root:

```sh
go test -tags='testing no_ui' ./agent/... -count=1
go test -tags='testing no_ui' ./... -count=1
go vet ./agent/...
go test -race -tags='testing no_ui' ./agent/... -count=1
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-w -s' \
  -o build/beszel-agent-linux-amd64 ./internal/cmd/agent
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -o build/beszel-agent-linux-arm64 ./internal/cmd/agent
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath \
  -o build/beszel-agent-darwin-arm64 ./internal/cmd/agent
```

Above completed successfully on Linux ARM64; separate full Linux amd64 run is
recorded in the final status. Race uses the container's C compiler, not CGO=0.
Containers run UID/GID 65534 to exercise upstream permission tests. A root run
failed existing TestDirectoryIsWritable/non-writable_directory; no test was
skipped or weakened. Full tests require internal/site/dist even with no_ui;
real UI assets were built in a separate artifact copy with npm ci, lingui
compile and vite build, then only dist was copied. Repository localization
sources and generated localization files were left untouched. Installed Bun
1.3.10 could not parse upstream lockfile v3; npm ci used existing package-lock.

Optional Windows cross-build failed on upstream missing embedded generated
`lhm/bin/Release/net48` assets. Linux amd64 file verified ELF x86-64, static.

Regression failures were observed before fixes: realtime continuity,
misidentified package labels, invalid optional floats, long intervals/spikes,
package source switching, and mixing averages across cache consumers.
Fixtures additionally cover measured idle zero, recovery, cancellation,
multiple GPUs, re-enumeration, hwmon replacement, simultaneous consumers,
missing optional inputs and oversized numeric prefixes.

Local (not Git) raw logs: Mayor artifacts/beszel-intel-v2/validation-final.log,
validation-amd64-final.log, linux-agent-tests.log, linux-arm64-agent-tests.log,
linux-amd64-agent-nonroot.log. Initial failed logs intentionally retained.

Final Linux amd64 run completed successfully: both test commands above and
`go vet ./agent/...`, nonroot, Go 1.27.2 under Docker Desktop amd64 emulation.
Native Linux ARM64 race tests completed successfully. Hardware baseline through
the actual hub realtime subscription confirms power omission between upstream
samples; development comparison follows in hardware benchmarks.

Reproducible Linux container wrapper (run from repository root; actual full runs
used named cache volumes, with the same UID and Go image):

```sh
docker run --rm --platform linux/amd64 --user 65534:65534 \
  -e GOCACHE=/tmp/go-cache -v "$PWD:/src" -w /src golang:1.27.2 \
  sh -c 'go test -tags="testing no_ui" ./agent/... && go test -tags="testing no_ui" ./... && go vet ./agent/...'
```

The independent `fix/gpu-package-power-rollup` branch reproduced missing package
power in historical rollups, then passed the full Linux test suite after adding
accumulation/division. Its regression includes a second aggregation tier.
The separate Xe diagnostic branch passed full Linux tests, vet, focused race
checks and amd64 build. The optional PM branch has its own validation record;
these experiments are not bundled into the reliability source patch.

Optional PM branch 2c65b04ff769: full Linux ARM64 `./agent/...` and `./...`
tests, vet, full agent race suite and Linux amd64/Darwin ARM64 builds passed.
Logs: suspend-validation-final2.log. Regressions include invalid/missing PM,
sleep/resume re-priming, normalized collector configuration, failed shadow
creation, outer hwmon alias reassignment and secondary device-link reassignment.
The separate Xe branch includes tests proving debug-disabled diagnostic paths
open no runtime-PM file, preserving the default read behavior.

UI embed preparation used these operations (run from repo root, with Node/npm):

```sh
npm ci --ignore-scripts --prefix internal/site
site_copy=$(mktemp -d)
tar -C internal/site --exclude=node_modules --exclude=dist -cf - . |
  tar -C "$site_copy" -xf -
ln -s "$PWD/internal/site/node_modules" "$site_copy/node_modules"
(cd "$site_copy" && ./node_modules/.bin/lingui compile && ./node_modules/.bin/vite build)
mkdir -p internal/site/dist build
cp -R "$site_copy/dist/." internal/site/dist/
# Remove only the temporary site_copy directory after successful preparation.
```

No localization extraction ran in the source checkout; the temporary copy's
compiled translations were not copied back. Dist output is ignored by Git.

## Follow-up verified feature branches

Combined source 038cd368a7e5, Go 1.27.2 Linux container, UID65534:

```sh
go test -tags='testing no_ui' ./...
go vet ./agent/...
go test -race -tags='testing no_ui' ./agent/...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-w -s' -o build/beszel-agent-linux-amd64 ./internal/cmd/agent
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o build/beszel-agent-darwin-arm64 ./internal/cmd/agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -tags='testing no_ui' -c -o build/beszel-intel-tests-linux-amd64 ./agent
```

All passed. Clean validation log: `verified-clean-validation.log`. An earlier
attempt started while cherry-pick conflicts were unresolved; its interleaved
log is excluded from evidence. The clean rerun occurred after resolution.
TrueNAS executed the combined static binary's Intel sysfs/Xe/PM fixtures with
`-test.run='TestIntelSysfs|TestIntelXe|TestSuspendAware' -test.timeout=60s`.
PASS as UID65534, no devices/caps/network, private PID, readonly root and bounded
tmpfs. This includes the deterministic pinned-minor reassignment regression.
The standalone fdinfo PoC has seven passing Python tests; it is not bundled in
the default agent image. Source/report crosslinks describe partial visibility.
