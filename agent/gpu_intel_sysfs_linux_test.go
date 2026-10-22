//go:build linux

package agent

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henrygd/beszel/agent/utils"
	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func setupIntelSysfsTest(t *testing.T) (root, cardPath, hwmonPath string) {
	t.Helper()
	root = t.TempDir()
	oldRoot := drmSysfsRoot
	drmSysfsRoot = root
	t.Cleanup(func() {
		drmSysfsRoot = oldRoot
	})

	cardPath = filepath.Join(root, "card0")
	devicePath := filepath.Join(cardPath, "device")
	hwmonPath = filepath.Join(devicePath, "hwmon", "hwmon0")
	require.NoError(t, os.MkdirAll(hwmonPath, 0o755))
	return root, cardPath, hwmonPath
}

func writeIntelSysfsFile(t *testing.T, basePath, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(basePath, name), []byte(content), 0o644))
}

func setIntelSysfsTime(t *testing.T, now time.Time) {
	t.Helper()
	oldNow := intelSysfsNow
	intelSysfsNow = func() time.Time { return now }
	t.Cleanup(func() {
		intelSysfsNow = oldNow
	})
}

func TestIntelSysfsDetectsIntelCardWithEnergy(t *testing.T) {
	_, cardPath, hwmonPath := setupIntelSysfsTest(t)
	devicePath := filepath.Join(cardPath, "device")
	writeIntelSysfsFile(t, devicePath, "vendor", "0x8086\n")
	writeIntelSysfsFile(t, hwmonPath, "name", "xe\n")
	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "1000000\n")

	gm := &GPUManager{}
	assert.True(t, gm.hasIntelSysfs())

	cards, err := discoverIntelSysfsCards()
	require.NoError(t, err)
	require.Len(t, cards, 1)
	assert.Equal(t, cardPath, cards[0].cardPath)
	assert.Equal(t, hwmonPath, cards[0].hwmonDir)
}

func TestIntelSysfsRejectsNonIntelCard(t *testing.T) {
	_, cardPath, hwmonPath := setupIntelSysfsTest(t)
	devicePath := filepath.Join(cardPath, "device")
	writeIntelSysfsFile(t, devicePath, "vendor", "0x1002\n")
	writeIntelSysfsFile(t, hwmonPath, "name", "xe\n")
	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "1000000\n")

	gm := &GPUManager{}
	assert.False(t, gm.hasIntelSysfs())
}

func TestIntelSysfsRequiresEnergyInput(t *testing.T) {
	_, cardPath, hwmonPath := setupIntelSysfsTest(t)
	devicePath := filepath.Join(cardPath, "device")
	writeIntelSysfsFile(t, devicePath, "vendor", "0x8086\n")
	writeIntelSysfsFile(t, hwmonPath, "name", "xe\n")

	gm := &GPUManager{}
	assert.False(t, gm.hasIntelSysfs())
}

func TestIntelSysfsFirstSampleInitializesWithoutBogusPower(t *testing.T) {
	_, cardPath, hwmonPath := setupIntelSysfsTest(t)
	devicePath := filepath.Join(cardPath, "device")
	writeIntelSysfsFile(t, devicePath, "vendor", "0x8086\n")
	writeIntelSysfsFile(t, hwmonPath, "name", "xe\n")
	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "1000000\n")
	setIntelSysfsTime(t, time.Unix(100, 0))

	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	ok := gm.updateIntelSysfsGpuData(cardPath, hwmonPath)
	require.True(t, ok)

	gpu := gm.GpuDataMap["card0"]
	require.NotNil(t, gpu)
	assert.Equal(t, "Intel GPU card0", gpu.Name)
	assert.Equal(t, 0.0, gpu.Power)
	assert.Equal(t, 0.0, gpu.Count)
}

func TestIntelSysfsSecondSampleComputesWatts(t *testing.T) {
	_, cardPath, hwmonPath := setupIntelSysfsTest(t)
	devicePath := filepath.Join(cardPath, "device")
	writeIntelSysfsFile(t, devicePath, "vendor", "0x8086\n")
	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "1000000\n")

	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	oldNow := intelSysfsNow
	intelSysfsNow = func() time.Time { return time.Unix(100, 0) }
	t.Cleanup(func() { intelSysfsNow = oldNow })
	require.True(t, gm.updateIntelSysfsGpuData(cardPath, hwmonPath))

	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "6000000\n")
	intelSysfsNow = func() time.Time { return time.Unix(102, 0) }
	require.True(t, gm.updateIntelSysfsGpuData(cardPath, hwmonPath))

	gpu := gm.GpuDataMap["card0"]
	require.NotNil(t, gpu)
	assert.Equal(t, 2.5, gpu.Power)
	assert.Equal(t, 1.0, gpu.Count)
}

func TestIntelSysfsSecondEnergyCounterMapsToPowerPkg(t *testing.T) {
	_, cardPath, hwmonPath := setupIntelSysfsTest(t)
	devicePath := filepath.Join(cardPath, "device")
	writeIntelSysfsFile(t, devicePath, "vendor", "0x8086\n")
	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "1000000\n")
	writeIntelSysfsFile(t, hwmonPath, "energy2_input", "2000000\n")

	oldNow := intelSysfsNow
	t.Cleanup(func() { intelSysfsNow = oldNow })
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	intelSysfsNow = func() time.Time { return time.Unix(100, 0) }
	require.True(t, gm.updateIntelSysfsGpuData(cardPath, hwmonPath))

	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "2000000\n")
	writeIntelSysfsFile(t, hwmonPath, "energy2_input", "8000000\n")
	intelSysfsNow = func() time.Time { return time.Unix(102, 0) }
	require.True(t, gm.updateIntelSysfsGpuData(cardPath, hwmonPath))

	gpu := gm.GpuDataMap["card0"]
	require.NotNil(t, gpu)
	assert.Equal(t, 0.5, gpu.Power)
	assert.Equal(t, 3.0, gpu.PowerPkg)
}

func TestIntelSysfsCounterResetSkipsOneSample(t *testing.T) {
	gm := &GPUManager{}
	power, ok := gm.calculateIntelSysfsPower("card0", 5000000, time.Unix(100, 0))
	assert.False(t, ok)
	assert.Equal(t, 0.0, power)

	power, ok = gm.calculateIntelSysfsPower("card0", 1000000, time.Unix(101, 0))
	assert.False(t, ok)
	assert.Equal(t, 0.0, power)

	power, ok = gm.calculateIntelSysfsPower("card0", 3000000, time.Unix(103, 0))
	assert.True(t, ok)
	assert.Equal(t, 1.0, power)
}

func TestIntelSysfsTempInputMapsToCelsius(t *testing.T) {
	_, cardPath, hwmonPath := setupIntelSysfsTest(t)
	devicePath := filepath.Join(cardPath, "device")
	writeIntelSysfsFile(t, devicePath, "vendor", "0x8086\n")
	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "1000000\n")
	writeIntelSysfsFile(t, hwmonPath, "temp1_input", "43500\n")
	setIntelSysfsTime(t, time.Unix(100, 0))

	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	require.True(t, gm.updateIntelSysfsGpuData(cardPath, hwmonPath))

	gpu := gm.GpuDataMap["card0"]
	require.NotNil(t, gpu)
	assert.Equal(t, 43.5, gpu.Temperature)
}

func TestIntelSysfsMissingOptionalFilesDoNotFail(t *testing.T) {
	_, cardPath, hwmonPath := setupIntelSysfsTest(t)
	devicePath := filepath.Join(cardPath, "device")
	writeIntelSysfsFile(t, devicePath, "vendor", "0x8086\n")
	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "1000000\n")
	setIntelSysfsTime(t, time.Unix(100, 0))

	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	require.True(t, gm.updateIntelSysfsGpuData(cardPath, hwmonPath))

	gpu := gm.GpuDataMap["card0"]
	require.NotNil(t, gpu)
	assert.Equal(t, 0.0, gpu.Usage)
	assert.Equal(t, 0.0, gpu.MemoryUsed)
	assert.Equal(t, 0.0, gpu.MemoryTotal)
	assert.Equal(t, 0.0, gpu.Temperature)
}

func TestIntelSysfsMapsOpportunisticMemoryAndUsage(t *testing.T) {
	_, cardPath, hwmonPath := setupIntelSysfsTest(t)
	devicePath := filepath.Join(cardPath, "device")
	writeIntelSysfsFile(t, devicePath, "vendor", "0x8086\n")
	writeIntelSysfsFile(t, devicePath, "gpu_busy_percent", "37\n")
	writeIntelSysfsFile(t, devicePath, "mem_info_lmem_used", "1073741824\n")
	writeIntelSysfsFile(t, devicePath, "mem_info_lmem_total", "2147483648\n")
	writeIntelSysfsFile(t, hwmonPath, "energy1_input", "1000000\n")
	setIntelSysfsTime(t, time.Unix(100, 0))

	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	require.True(t, gm.updateIntelSysfsGpuData(cardPath, hwmonPath))

	gpu := gm.GpuDataMap["card0"]
	require.NotNil(t, gpu)
	assert.Equal(t, 0.0, gpu.Usage) // no valid interval yet
	assert.Equal(t, utils.BytesToMegabytes(1073741824), gpu.MemoryUsed)
	assert.Equal(t, utils.BytesToMegabytes(2147483648), gpu.MemoryTotal)
}

// A sysfs-only GPU has neither engine data, temperature nor memory on Xe.
func TestIntelSysfsRealtimeContinuity(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	setIntelSysfsTime(t, time.Unix(100, 0))
	require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
	gm.GetCurrentData(1)
	writeIntelSysfsFile(t, hwmon, "energy1_input", "61000000")
	setIntelSysfsTime(t, time.Unix(103, 0))
	require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
	require.Equal(t, 20.0, gm.GetCurrentData(1)["card0"].Power)
	require.Equal(t, 20.0, gm.GetCurrentData(1)["card0"].Power)
}

func TestIntelSysfsUnknownEnergyLabelIsNotPackage(t *testing.T) {
	_, _, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, hwmon, "energy2_input", "1000000")
	writeIntelSysfsFile(t, hwmon, "energy2_label", "memory")
	gm := &GPUManager{}
	gm.readIntelSysfsPowerPkg("card0", hwmon, time.Unix(100, 0))
	writeIntelSysfsFile(t, hwmon, "energy2_input", "3000000")
	_, ok := gm.readIntelSysfsPowerPkg("card0", hwmon, time.Unix(102, 0))
	require.False(t, ok)
}

func TestIntelSysfsRejectsInvalidOptionalNumbers(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-1", string(make([]byte, 100000))} {
		t.Run(value[:min(len(value), 8)], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "value")
			require.NoError(t, os.WriteFile(path, []byte(value), 0600))
			_, err := readOptionalSysfsFloat(path)
			require.Error(t, err)
		})
	}
}

func TestIntelSysfsMissingAndRecovery(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, filepath.Join(card, "device"), "vendor", "0x8086")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	setIntelSysfsTime(t, time.Unix(100, 0))
	require.True(t, gm.pollIntelSysfs())
	require.Empty(t, gm.GetCurrentData(1), "first counter is not measured zero power")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "31000000")
	setIntelSysfsTime(t, time.Unix(103, 0))
	require.True(t, gm.pollIntelSysfs())
	require.Equal(t, 10.0, gm.GetCurrentData(1)["card0"].Power)
	require.NoError(t, os.Remove(filepath.Join(hwmon, "energy1_input")))
	require.False(t, gm.pollIntelSysfs())
	require.Empty(t, gm.GetCurrentData(1))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "61000000")
	setIntelSysfsTime(t, time.Unix(106, 0))
	require.True(t, gm.pollIntelSysfs())
	require.Empty(t, gm.GetCurrentData(1))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "91000000")
	setIntelSysfsTime(t, time.Unix(109, 0))
	require.True(t, gm.pollIntelSysfs())
	require.Equal(t, 10.0, gm.GetCurrentData(1)["card0"].Power)
}

func TestIntelSysfsStaleIsOmitted(t *testing.T) {
	gm := &GPUManager{GpuDataMap: map[string]*system.GPUData{"card0": {Name: "Intel", Count: 1, Power: 30}}, intelSysfsSamples: map[string]time.Time{"card0": time.Now().Add(-time.Minute)}}
	require.Empty(t, gm.GetCurrentData(1))
}

func TestIntelSysfsRejectsImpossibleIntervals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		energy uint64
		dt     time.Duration
	}{
		{"backward-time", 2e6, -time.Second}, {"zero-time", 2e6, 0},
		{"long-gap", 2e6, time.Hour}, {"spike", uint64(1e15), time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gm := &GPUManager{}
			start := time.Now()
			gm.calculateIntelSysfsPower("card0", 1e6, start)
			_, valid := gm.calculateIntelSysfsPower("card0", tc.energy, start.Add(tc.dt))
			require.False(t, valid)
		})
	}
}

func TestIntelSysfsMeasuredZero(t *testing.T) {
	gm := &GPUManager{}
	now := time.Now()
	gm.calculateIntelSysfsPower("card0", 1e6, now)
	watts, ok := gm.calculateIntelSysfsPower("card0", 1e6, now.Add(3*time.Second))
	require.True(t, ok)
	require.Zero(t, watts)
}

func TestIntelSysfsPackageMissingDoesNotDilutePower(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	for i := 0; i < 4; i++ {
		setIntelSysfsTime(t, time.Unix(int64(100+i*3), 0))
		writeIntelSysfsFile(t, hwmon, "energy1_input", strconv.Itoa(1000000+i*30000000))
		if i == 2 {
			require.NoError(t, os.Remove(filepath.Join(hwmon, "energy2_input")))
		} else {
			writeIntelSysfsFile(t, hwmon, "energy2_input", strconv.Itoa(1000000+i*15000000))
		}
		require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
	}
	require.Equal(t, 5.0, gm.GetCurrentData(1)["card0"].PowerPkg, "keep the earlier valid package interval; a reappeared counter only primes")
}

func TestIntelSysfsDiscoveryRejectsDirectory(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, filepath.Join(card, "device"), "vendor", "0x8086")
	// Metadata-only discovery must reject a non-file attribute.
	require.NoError(t, os.Mkdir(filepath.Join(hwmon, "energy1_input"), 0755))
	cards, err := discoverIntelSysfsCards()
	require.NoError(t, err)
	require.Empty(t, cards)
}

func TestIntelSysfsCancellation(t *testing.T) {
	old := drmSysfsRoot
	drmSysfsRoot = t.TempDir()
	defer func() { drmSysfsRoot = old }()
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	gm.startIntelSysfsCollector()
	done := make(chan struct{})
	go func() { gm.stopCollectors(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector did not cancel")
	}
}

func TestIntelSysfsCardRenumberKeepsSeries(t *testing.T) {
	root := t.TempDir()
	old := drmSysfsRoot
	drmSysfsRoot = root
	defer func() { drmSysfsRoot = old }()
	pci := filepath.Join(root, "0000:12:00.0")
	hwmon := filepath.Join(pci, "hwmon/hwmon0")
	require.NoError(t, os.MkdirAll(hwmon, 0755))
	writeIntelSysfsFile(t, pci, "vendor", "0x8086")
	for _, card := range []string{"card0", "card2"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, card), 0755))
		require.NoError(t, os.Symlink(pci, filepath.Join(root, card, "device")))
	}
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	setIntelSysfsTime(t, time.Unix(100, 0))
	require.True(t, gm.updateIntelSysfsGpuData(filepath.Join(root, "card0"), hwmon))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "31000000")
	setIntelSysfsTime(t, time.Unix(103, 0))
	require.True(t, gm.updateIntelSysfsGpuData(filepath.Join(root, "card2"), hwmon))
	require.Len(t, gm.GpuDataMap, 1)
	require.Equal(t, 10.0, gm.GetCurrentData(1)["card0"].Power)
}

func TestIntelSysfsPackageSourceChangeNeedsBaseline(t *testing.T) {
	_, _, hwmon := setupIntelSysfsTest(t)
	gm := &GPUManager{}
	writeIntelSysfsFile(t, hwmon, "energy2_input", "1000000")
	gm.readIntelSysfsPowerPkg("card0", hwmon, time.Unix(100, 0))
	writeIntelSysfsFile(t, hwmon, "energy2_label", "memory")
	writeIntelSysfsFile(t, hwmon, "energy3_label", "pkg")
	writeIntelSysfsFile(t, hwmon, "energy3_input", "31000000")
	_, ok := gm.readIntelSysfsPowerPkg("card0", hwmon, time.Unix(103, 0))
	require.False(t, ok, "different package source must not share a counter baseline")
}

func TestIntelSysfsContinuityUsesOwnCacheAverage(t *testing.T) {
	gm := &GPUManager{GpuDataMap: map[string]*system.GPUData{"card0": {Name: "Intel", Count: 1, Power: 10}}, intelSysfsSamples: map[string]time.Time{"card0": time.Now()}}
	gm.GetCurrentData(1)
	gm.GpuDataMap["card0"].Count = 2
	gm.GpuDataMap["card0"].Power = 40
	require.Equal(t, 30.0, gm.GetCurrentData(1)["card0"].Power)
	require.Equal(t, 20.0, gm.GetCurrentData(60)["card0"].Power)
	require.Equal(t, 30.0, gm.GetCurrentData(1)["card0"].Power)
}

func TestIntelSysfsPackageExplicitLabelWins(t *testing.T) {
	_, _, hwmon := setupIntelSysfsTest(t)
	gm := &GPUManager{}
	writeIntelSysfsFile(t, hwmon, "energy2_input", "1000000")
	writeIntelSysfsFile(t, hwmon, "energy3_label", "pkg")
	writeIntelSysfsFile(t, hwmon, "energy3_input", "1000000")
	gm.readIntelSysfsPowerPkg("card0", hwmon, time.Unix(100, 0))
	writeIntelSysfsFile(t, hwmon, "energy2_input", "61000000")
	writeIntelSysfsFile(t, hwmon, "energy3_input", "16000000")
	watts, ok := gm.readIntelSysfsPowerPkg("card0", hwmon, time.Unix(103, 0))
	require.True(t, ok)
	require.Equal(t, 5.0, watts)
}

func TestIntelSysfsMultipleCards(t *testing.T) {
	root, card, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, filepath.Join(card, "device"), "vendor", "0x8086")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	second := filepath.Join(root, "card1/device/hwmon/hwmon1")
	require.NoError(t, os.MkdirAll(second, 0755))
	writeIntelSysfsFile(t, filepath.Join(root, "card1/device"), "vendor", "0x8086")
	writeIntelSysfsFile(t, second, "energy1_input", "2000000")
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	setIntelSysfsTime(t, time.Unix(100, 0))
	require.True(t, gm.pollIntelSysfs())
	writeIntelSysfsFile(t, hwmon, "energy1_input", "31000000")
	writeIntelSysfsFile(t, second, "energy1_input", "62000000")
	setIntelSysfsTime(t, time.Unix(103, 0))
	require.True(t, gm.pollIntelSysfs())
	data := gm.GetCurrentData(1)
	require.Len(t, data, 2)
	require.Equal(t, 10.0, data["card0"].Power)
	require.Equal(t, 20.0, data["card1"].Power)
}

func TestIntelSysfsConcurrentConsumers(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		for range 100 {
			gm.updateIntelSysfsGpuData(card, hwmon)
		}
	}()
	for key := range uint16(4) {
		go func() {
			defer wg.Done()
			for range 100 {
				gm.GetCurrentData(key)
			}
		}()
	}
	wg.Wait()
}

func TestIntelSysfsHwmonReplacementReprimes(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	setIntelSysfsTime(t, time.Unix(100, 0))
	gm.updateIntelSysfsGpuData(card, hwmon)
	newHwmon := filepath.Join(card, "device/hwmon/hwmon5")
	require.NoError(t, os.Mkdir(newHwmon, 0755))
	writeIntelSysfsFile(t, newHwmon, "energy1_input", "31000000")
	setIntelSysfsTime(t, time.Unix(103, 0))
	gm.updateIntelSysfsGpuData(card, newHwmon)
	require.Empty(t, gm.GetCurrentData(1))
}

func TestIntelSysfsOversizedNumericPrefixRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "number")
	require.NoError(t, os.WriteFile(path, []byte("1"+strings.Repeat(" ", 100)+"corruption"), 0600))
	_, err := readOptionalSysfsFloat(path)
	require.Error(t, err)
	_, err = readSysfsUint(path)
	require.Error(t, err)
}

// A FIFO deterministically holds an optional kernel-style sensor read open.
// Writer rendezvous proves the collector reached open/read, without sleep guesses.
func blockedIntelCollector(t *testing.T) (*GPUManager, func()) {
	t.Helper()
	_, card, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, filepath.Join(card, "device"), "vendor", "0x8086")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	gm := &GPUManager{GpuDataMap: map[string]*system.GPUData{"amd-card1": {Name: "AMD", Temperature: 50, Count: 1}}}
	require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
	gm.intelSysfsEnergySnapshots["card0"] = intelSysfsEnergySnapshot{microjoules: 1000000, timestamp: time.Now().Add(-3 * time.Second)}
	writeIntelSysfsFile(t, hwmon, "energy1_input", "91000000")
	fifo := filepath.Join(hwmon, "temp1_input")
	require.NoError(t, unix.Mkfifo(fifo, 0600))
	require.True(t, gm.startIntelSysfsCollector())
	fd := -1
	require.Eventually(t, func() bool {
		var err error
		fd, err = unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		return err == nil
	}, time.Second, time.Millisecond)
	release := sync.OnceFunc(func() { require.NoError(t, unix.Close(fd)) })
	t.Cleanup(func() {
		release()
		gm.stopCollectors()
		select {
		case <-gm.collectorDone:
		case <-time.After(time.Second):
			t.Error("released FIFO worker did not exit")
		}
	})
	return gm, release
}
func TestIntelSysfsBlockedReadDoesNotFreezeHub(t *testing.T) {
	gm, _ := blockedIntelCollector(t)
	done := make(chan map[string]system.GPUData, 1)
	go func() { done <- gm.GetCurrentData(1) }()
	select {
	case data := <-done:
		require.Contains(t, data, "amd-card1")
	case <-time.After(200 * time.Millisecond):
		t.Error("blocked Intel sensor freezes hub GPU collection")
	}
}
func TestIntelSysfsBlockedReadDoesNotFreezeShutdown(t *testing.T) {
	gm, release := blockedIntelCollector(t)
	done := make(chan struct{})
	go func() { gm.stopCollectors(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("shutdown waits indefinitely for kernel-stalled Intel read")
	}
	release()
	select {
	case <-gm.collectorDone:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after read released")
	}
	require.False(t, gm.startIntelSysfsCollector(), "stopped manager must not create replacement blocked workers")
	require.NotContains(t, gm.GetCurrentData(1), "card0", "late read committed after cancellation")
}

func intelPCIFixture(t *testing.T) (root, pci, hwmon string) {
	t.Helper()
	root, _, _ = setupIntelSysfsTest(t)
	require.NoError(t, os.RemoveAll(filepath.Join(root, "card0")))
	pci = filepath.Join(root, "0000:12:00.0")
	hwmon = filepath.Join(pci, "hwmon/hwmon0")
	require.NoError(t, os.MkdirAll(hwmon, 0755))
	writeIntelSysfsFile(t, pci, "vendor", "0x8086")
	return
}
func intelCardAlias(t *testing.T, root, pci, name string) {
	t.Helper()
	card := filepath.Join(root, name)
	require.NoError(t, os.Mkdir(card, 0755))
	require.NoError(t, os.Symlink(pci, filepath.Join(card, "device")))
}
func TestIntelSysfsPollDeduplicatesPhysicalGPU(t *testing.T) {
	root, pci, hwmon := intelPCIFixture(t)
	intelCardAlias(t, root, pci, "card0")
	intelCardAlias(t, root, pci, "card2")
	now := time.Unix(100, 0)
	old := intelSysfsNow
	t.Cleanup(func() { intelSysfsNow = old })
	intelSysfsNow = func() time.Time { r := now; now = now.Add(3 * time.Second); return r }
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	gm.pollIntelSysfs()
	require.Zero(t, gm.GpuDataMap["card0"].Count, "one physical GPU must prime once per cycle")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "91000000")
	gm.pollIntelSysfs()
	require.Equal(t, 1.0, gm.GpuDataMap["card0"].Count)
	require.Equal(t, 30.0, gm.GetCurrentData(1)["card0"].Power)
}
func TestIntelSysfsDisappearRecreatePreservesName(t *testing.T) {
	root, pci, hwmon := intelPCIFixture(t)
	intelCardAlias(t, root, pci, "card0")
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	setIntelSysfsTime(t, time.Unix(100, 0))
	gm.pollIntelSysfs()
	require.Equal(t, "Intel GPU card0", gm.GpuDataMap["card0"].Name)
	require.NoError(t, os.RemoveAll(filepath.Join(root, "card0")))
	gm.pollIntelSysfs()
	require.Empty(t, gm.GpuDataMap)
	intelCardAlias(t, root, pci, "card2")
	setIntelSysfsTime(t, time.Unix(103, 0))
	gm.pollIntelSysfs()
	require.Equal(t, "Intel GPU card0", gm.GpuDataMap["card0"].Name)
	writeIntelSysfsFile(t, hwmon, "energy1_input", "91000000")
	setIntelSysfsTime(t, time.Unix(106, 0))
	gm.pollIntelSysfs()
	require.Equal(t, "Intel GPU card0", gm.GetCurrentData(1)["card0"].Name)
}

func TestIntelSysfsFreshnessUsesEnergyObservation(t *testing.T) {
	gm, release := blockedIntelCollector(t)
	// The counter was observed before the optional sensor FIFO rendezvous.
	beforeRelease := time.Now()
	release()
	require.Eventually(t, func() bool { gm.Lock(); defer gm.Unlock(); return !gm.intelSysfsSamples["card0"].IsZero() }, time.Second, time.Millisecond)
	gm.Lock()
	observed := gm.intelSysfsSamples["card0"]
	gm.Unlock()
	require.True(t, observed.Before(beforeRelease), "optional reads must not extend energy freshness")
}

func TestIntelSysfsShutdownDoesNotVisitNextBlockedCard(t *testing.T) {
	root, card, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, filepath.Join(card, "device"), "vendor", "0x8086")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	first := filepath.Join(hwmon, "temp1_input")
	require.NoError(t, unix.Mkfifo(first, 0600))
	secondHwmon := filepath.Join(root, "card1/device/hwmon/hwmon1")
	require.NoError(t, os.MkdirAll(secondHwmon, 0755))
	writeIntelSysfsFile(t, filepath.Join(root, "card1/device"), "vendor", "0x8086")
	writeIntelSysfsFile(t, secondHwmon, "energy1_input", "1000000")
	second := filepath.Join(secondHwmon, "temp1_input")
	require.NoError(t, unix.Mkfifo(second, 0600))
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	require.True(t, gm.startIntelSysfsCollector())
	fd := -1
	require.Eventually(t, func() bool {
		var err error
		fd, err = unix.Open(first, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		return err == nil
	}, time.Second, time.Millisecond)
	gm.stopCollectors()
	require.NoError(t, unix.Close(fd))
	select {
	case <-gm.collectorDone:
	case <-time.After(time.Second):
		// Unstick a regressed implementation so its goroutine cannot escape the test.
		if fd, err := unix.Open(second, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
			unix.Close(fd)
		}
		<-gm.collectorDone
		t.Fatal("cancelled collector visited second blocking sensor")
	}
}

func TestIntelSysfsInvalidIntervalIsNotSuccessfulSampling(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	setIntelSysfsTime(t, time.Unix(100, 0))
	require.True(t, gm.updateIntelSysfsGpuData(card, hwmon), "initial baseline is healthy priming")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000000000")
	setIntelSysfsTime(t, time.Unix(103, 0))
	require.False(t, gm.updateIntelSysfsGpuData(card, hwmon), "rejected spike is failed sampling")
	require.Empty(t, gm.GetCurrentData(1))
	// A slow recovery probe must re-prime instead of remaining stuck in backoff.
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000003000000")
	setIntelSysfsTime(t, time.Unix(163, 0))
	require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
	require.Empty(t, gm.GetCurrentData(1))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000006000000")
	setIntelSysfsTime(t, time.Unix(166, 0))
	require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
	require.Equal(t, 1.0, gm.GetCurrentData(1)["card0"].Power)
}

func TestIntelSysfsFailureWarningsAndRecovery(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, filepath.Join(card, "device"), "vendor", "0x8086")
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	writeIntelSysfsFile(t, hwmon, "energy1_input", "NaN")
	for range 20 {
		require.False(t, gm.pollIntelSysfs())
	}
	require.Equal(t, 1, strings.Count(log.String(), "level=WARN"), "persistent failure logs must be rate limited")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	setIntelSysfsTime(t, time.Unix(100, 0))
	gm.pollIntelSysfs()
	require.NotContains(t, log.String(), "recovered", "priming is not measured recovery")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "31000000")
	setIntelSysfsTime(t, time.Unix(103, 0))
	gm.pollIntelSysfs()
	require.Equal(t, 1, strings.Count(log.String(), "Intel sysfs power recovered"))
}

func TestIntelSysfsWarningIntervalAndRecoveryState(t *testing.T) {
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	defer slog.SetDefault(old)
	gm := &GPUManager{}
	now := time.Now()
	gm.reportIntelSysfsHealth("card0", intelSysfsFailed, now)
	gm.reportIntelSysfsHealth("card0", intelSysfsFailed, now.Add(59*time.Second))
	require.Equal(t, 1, strings.Count(log.String(), "level=WARN"))
	gm.reportIntelSysfsHealth("card0", intelSysfsFailed, now.Add(time.Minute))
	require.Equal(t, 2, strings.Count(log.String(), "level=WARN"))
	gm.reportIntelSysfsHealth("card0", intelSysfsPriming, now.Add(61*time.Second))
	require.NotContains(t, log.String(), "recovered")
	gm.reportIntelSysfsHealth("card0", intelSysfsMeasured, now.Add(64*time.Second))
	require.Equal(t, 1, strings.Count(log.String(), "Intel sysfs power recovered"))
}
func TestIntelSysfsUnreadableCandidateIsNotHealthy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read permissionless fixtures")
	}
	_, card, hwmon := setupIntelSysfsTest(t)
	writeIntelSysfsFile(t, filepath.Join(card, "device"), "vendor", "0x8086")
	path := filepath.Join(hwmon, "energy1_input")
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	require.NoError(t, os.Chmod(path, 0000))
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	require.False(t, gm.hasIntelSysfs(), "metadata permission checks must reject an unreadable candidate without opening it")
	require.False(t, gm.pollIntelSysfs())
	require.Empty(t, gm.GetCurrentData(1))
}

func TestIntelSysfsSlowPackageAcquisitionIsUnavailable(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	writeIntelSysfsFile(t, hwmon, "energy2_input", "1000000")
	setIntelSysfsTime(t, time.Unix(100, 0))
	gm.updateIntelSysfsGpuData(card, hwmon)
	pkg := filepath.Join(hwmon, "energy2_input")
	require.NoError(t, os.Remove(pkg))
	require.NoError(t, unix.Mkfifo(pkg, 0600))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "91000000")
	setIntelSysfsTime(t, time.Unix(103, 0))
	done := make(chan bool, 1)
	go func() { done <- gm.updateIntelSysfsGpuData(card, hwmon) }()
	fd := -1
	require.Eventually(t, func() bool {
		var err error
		fd, err = unix.Open(pkg, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		return err == nil
	}, time.Second, time.Millisecond)
	// Exercise actual syscall timing uncertainty, rather than mocking the clock.
	<-time.NewTimer(300 * time.Millisecond).C
	_, err := unix.Write(fd, []byte("91000000"))
	require.NoError(t, err)
	require.NoError(t, unix.Close(fd))
	require.True(t, <-done, "card interval remains valid")
	require.Zero(t, gm.GetCurrentData(1)["card0"].PowerPkg, "slow package acquisition must be unavailable, not a spike")
}

// A failed final package sample must not erase measurements in a slower
// consumer's window, or leak the previous window into realtime updates.
func TestIntelSysfsPackageAvailabilityPerConsumer(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	for i := 0; i < 3; i++ {
		setIntelSysfsTime(t, time.Unix(int64(100+i*3), 0))
		writeIntelSysfsFile(t, hwmon, "energy1_input", strconv.Itoa(1000000+i*90000000))
		writeIntelSysfsFile(t, hwmon, "energy2_input", strconv.Itoa(1000000+i*48000000))
		require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
		if i == 1 {
			require.Equal(t, 16.0, gm.GetCurrentData(1)["card0"].PowerPkg)
			require.Equal(t, 16.0, gm.GetCurrentData(60)["card0"].PowerPkg)
		}
	}
	require.Equal(t, 16.0, gm.GetCurrentData(1)["card0"].PowerPkg)
	require.Equal(t, 16.0, gm.GetCurrentData(1)["card0"].PowerPkg, "carry a fresh sample between polls")
	require.NoError(t, os.Remove(filepath.Join(hwmon, "energy2_input")))
	setIntelSysfsTime(t, time.Unix(109, 0))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "271000000")
	require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
	require.Zero(t, gm.GetCurrentData(1)["card0"].PowerPkg, "realtime has consumed the earlier valid package sample")
	require.Zero(t, gm.GetCurrentData(1)["card0"].PowerPkg)
	require.Equal(t, 16.0, gm.GetCurrentData(60)["card0"].PowerPkg, "historical interval still contains one valid package sample")
	require.Zero(t, gm.GetCurrentData(60)["card0"].PowerPkg, "do not carry package power when the latest sample is unavailable")
}

func TestIntelSysfsDisappearingHwmonRejectsSample(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	setIntelSysfsTime(t, time.Unix(100, 0))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
	setIntelSysfsTime(t, time.Unix(103, 0))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "91000000")
	fifo := filepath.Join(hwmon, "temp1_input")
	require.NoError(t, unix.Mkfifo(fifo, 0600))
	done := make(chan intelSysfsSampleStatus, 1)
	go func() { done <- gm.sampleIntelSysfsGpuData(card, hwmon) }()
	fd := -1
	require.Eventually(t, func() bool {
		var err error
		fd, err = unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		return err == nil
	}, time.Second, time.Millisecond)
	release := sync.OnceFunc(func() { require.NoError(t, unix.Close(fd)) })
	defer release()
	require.NoError(t, os.RemoveAll(hwmon))
	release()
	select {
	case status := <-done:
		require.Equal(t, intelSysfsFailed, status)
		require.Zero(t, gm.GpuDataMap["card0"].Count, "removed sensor readings must not be committed")
	case <-time.After(time.Second):
		t.Fatal("sensor worker did not finish")
	}
	require.NoError(t, os.MkdirAll(hwmon, 0755))
	setIntelSysfsTime(t, time.Unix(106, 0))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "181000000")
	require.Equal(t, intelSysfsPriming, gm.sampleIntelSysfsGpuData(card, hwmon), "recreated sensor needs a new baseline")
	require.Empty(t, gm.GetCurrentData(1))
	setIntelSysfsTime(t, time.Unix(109, 0))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "271000000")
	require.Equal(t, intelSysfsMeasured, gm.sampleIntelSysfsGpuData(card, hwmon))
	require.Equal(t, 30.0, gm.GetCurrentData(1)["card0"].Power)
}

func TestIntelSysfsPrimaryFailureSkipsOptionalReads(t *testing.T) {
	_, card, hwmon := setupIntelSysfsTest(t)
	gm := &GPUManager{GpuDataMap: make(map[string]*system.GPUData)}
	setIntelSysfsTime(t, time.Unix(100, 0))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "1000000")
	require.True(t, gm.updateIntelSysfsGpuData(card, hwmon))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "invalid")
	fifo := filepath.Join(hwmon, "temp1_input")
	require.NoError(t, unix.Mkfifo(fifo, 0600))
	done := make(chan intelSysfsSampleStatus, 1)
	go func() {
		done <- gm.sampleIntelSysfsGpuData(card, hwmon)
		close(done)
	}()
	// If the assertion fails before the reader reaches open(), rendezvous
	// with it later and release it before fixture/global-clock cleanup.
	t.Cleanup(func() {
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				if fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
					assert.NoError(t, unix.Close(fd))
				}
			case <-deadline.C:
				t.Error("optional sensor worker did not exit during cleanup")
				return
			}
		}
	})
	select {
	case status := <-done:
		require.Equal(t, intelSysfsFailed, status)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("primary energy failure accessed a blocking optional sensor")
	}
	require.NoError(t, os.Remove(fifo))
	setIntelSysfsTime(t, time.Unix(103, 0))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "91000000")
	require.Equal(t, intelSysfsPriming, gm.sampleIntelSysfsGpuData(card, hwmon))
	require.Empty(t, gm.GetCurrentData(1), "recovery must prime rather than bridge the failed interval")
	setIntelSysfsTime(t, time.Unix(106, 0))
	writeIntelSysfsFile(t, hwmon, "energy1_input", "181000000")
	require.Equal(t, intelSysfsMeasured, gm.sampleIntelSysfsGpuData(card, hwmon))
	require.Equal(t, 30.0, gm.GetCurrentData(1)["card0"].Power)
}
