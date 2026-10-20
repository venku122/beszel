//go:build linux

package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/henrygd/beszel/agent/utils"
	"github.com/henrygd/beszel/internal/entities/system"
)

var (
	drmSysfsRoot  = "/sys/class/drm"
	intelSysfsNow = time.Now
)

type intelSysfsEnergySnapshot struct {
	microjoules uint64
	timestamp   time.Time
}

type intelSysfsSampleStatus uint8

const (
	intelSysfsFailed intelSysfsSampleStatus = iota
	intelSysfsPriming
	intelSysfsMeasured
)

type intelSysfsCard struct {
	cardPath string
	hwmonDir string
}

// hasIntelSysfs returns true if any Intel DRM card exposes an hwmon energy counter.
func (gm *GPUManager) hasIntelSysfs() bool {
	cards, err := discoverIntelSysfsCards()
	return err == nil && len(cards) > 0
}

// startIntelSysfsCollector starts Intel GPU collection via sysfs.
func (gm *GPUManager) startIntelSysfsCollector() bool {
	gm.Lock()
	defer gm.Unlock()
	if gm.collectorStopped {
		return false
	}
	if gm.collectorCancel != nil {
		return true
	}
	ctx, cancel := context.WithCancel(context.Background())
	gm.collectorCancel = cancel
	gm.collectorDone = make(chan struct{})
	go func() {
		defer close(gm.collectorDone)
		gm.collectIntelSysfsStatsContext(ctx)
	}()
	return true
}

// Retry discovery with capped backoff instead of permanently losing telemetry
// after a reset or temporary sensor disappearance. Every wait is cancellable.
func (gm *GPUManager) collectIntelSysfsStatsContext(ctx context.Context) {
	delay := 3 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		hasData := gm.pollIntelSysfs()
		if hasData {
			delay = 3 * time.Second
		} else {
			delay = min(max(delay*2, 5*time.Second), time.Minute)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Discovery is metadata-only: it must not read energy just to detect hardware.
// This also recovers hwmon paths changed during driver reinitialization.
func (gm *GPUManager) pollIntelSysfs() bool {
	if gm.intelSysfsStopped() {
		return false
	}
	cards, err := discoverIntelSysfsCards()
	if err != nil {
		gm.reportIntelSysfsHealth("", intelSysfsFailed, time.Now())
		return false
	}
	if len(cards) == 0 {
		gm.reportIntelSysfsHealth("", intelSysfsFailed, time.Now())
	}
	seen := make(map[string]bool, len(cards))
	hasData := false
	for _, card := range cards {
		if gm.intelSysfsStopped() {
			return false
		}
		device := intelSysfsDevice(card.cardPath)
		gm.Lock()
		id := gm.intelSysfsID(card.cardPath, device)
		gm.Unlock()
		if id == "" {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		status := gm.sampleIntelSysfsGpuData(card.cardPath, card.hwmonDir)
		if gm.intelSysfsStopped() {
			return false
		}
		gm.reportIntelSysfsHealth(id, status, time.Now())
		if status == intelSysfsMeasured {
			gm.reportIntelSysfsHealth("", status, time.Now())
		}
		if status != intelSysfsFailed {
			hasData = true
		}
	}
	gm.Lock()
	defer gm.Unlock()
	for id := range gm.intelSysfsSamples {
		if !seen[id] {
			gm.intelSysfsSamples[id] = time.Time{}
			// Drop removed devices and their cache entries to bound rediscovery state.
			delete(gm.GpuDataMap, id)
			delete(gm.lastAvgData, id)
			delete(gm.intelSysfsSamples, id)
			delete(gm.intelSysfsPackageCounts, id)
			delete(gm.intelSysfsPackageValid, id)
			delete(gm.intelSysfsSensorPaths, id)
			for _, snapshots := range gm.lastSnapshots {
				delete(snapshots, id)
			}
			for key := range gm.intelSysfsEnergySnapshots {
				if key == id || strings.HasPrefix(key, id+":") {
					delete(gm.intelSysfsEnergySnapshots, key)
				}
			}
		}
	}
	return hasData
}

func discoverIntelSysfsCards() ([]intelSysfsCard, error) {
	paths, err := filepath.Glob(filepath.Join(drmSysfsRoot, "card*"))
	if err != nil {
		return nil, err
	}

	var cards []intelSysfsCard
	seenDevices := make(map[string]bool)
	for _, cardPath := range paths {
		if strings.Contains(filepath.Base(cardPath), "-") || !isIntelGpu(cardPath) {
			continue
		}
		hwmonDir := findIntelEnergyHwmon(filepath.Join(cardPath, "device"))
		if hwmonDir == "" {
			continue
		}
		device := intelSysfsDevice(cardPath)
		if seenDevices[device] {
			continue
		}
		seenDevices[device] = true
		cards = append(cards, intelSysfsCard{cardPath: cardPath, hwmonDir: hwmonDir})
	}
	return cards, nil
}

func isIntelGpu(cardPath string) bool {
	vendor, err := utils.ReadStringFileLimited(filepath.Join(cardPath, "device/vendor"), 64)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(vendor), "0x8086")
}

func findIntelEnergyHwmon(devicePath string) string {
	hwmons, _ := filepath.Glob(filepath.Join(devicePath, "hwmon/hwmon*"))
	var fallback string
	for _, hwmonDir := range hwmons {
		if !sysfsFileExists(filepath.Join(hwmonDir, "energy1_input")) {
			continue
		}
		if name, err := utils.ReadStringFileLimited(filepath.Join(hwmonDir, "name"), 64); err == nil && strings.EqualFold(strings.TrimSpace(name), "xe") {
			return hwmonDir
		}
		if fallback == "" {
			fallback = hwmonDir
		}
	}
	return fallback
}

func sysfsFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// updateIntelSysfsGpuData reads GPU metrics from sysfs and updates the GPU data map.
// Returns true for initial/recovery priming or a valid measured interval.
// Rejected intervals are failures; priming never emits measured zero.
func (gm *GPUManager) updateIntelSysfsGpuData(cardPath, hwmonDir string) bool {
	return gm.sampleIntelSysfsGpuData(cardPath, hwmonDir) != intelSysfsFailed
}

func (gm *GPUManager) sampleIntelSysfsGpuData(cardPath, hwmonDir string) intelSysfsSampleStatus {
	// All filesystem operations, including metadata and optional sensors, occur
	// outside the manager lock. A kernel-stalled read must not freeze hub stats.
	if gm.intelSysfsStopped() {
		return intelSysfsFailed
	}
	started := time.Now()
	devicePath := filepath.Join(cardPath, "device")
	device := intelSysfsDevice(cardPath)
	sensorPath, sensorErr := filepath.EvalSymlinks(hwmonDir)
	if sensorErr != nil {
		sensorPath = hwmonDir
	}
	name := getIntelSysfsGpuName(cardPath)
	energy, energyErr := readSysfsUint(filepath.Join(hwmonDir, "energy1_input"))
	observed := time.Now()
	now := intelSysfsNow()
	pkgPath, pkgEnergy, pkgErr := readIntelSysfsPackageCounter(hwmonDir)
	temp := readIntelSysfsTemperature(hwmonDir)
	usage, usageErr := readOptionalSysfsFloat(filepath.Join(devicePath, "gpu_busy_percent"))
	memUsed, memUsedErr := readFirstOptionalSysfsFloat(
		filepath.Join(devicePath, "mem_info_vram_used"), filepath.Join(devicePath, "mem_info_lmem_used"), filepath.Join(devicePath, "mem_info_local_mem_used"))
	memTotal, memTotalErr := readFirstOptionalSysfsFloat(
		filepath.Join(devicePath, "mem_info_vram_total"), filepath.Join(devicePath, "mem_info_lmem_total"), filepath.Join(devicePath, "mem_info_local_mem_total"))
	// Reject a path reassigned during sampling. Revalidation also stays outside
	// the mutex, and stopped collectors can never commit late sensor results.
	if device != intelSysfsDevice(cardPath) {
		return intelSysfsFailed
	}
	afterSensor, err := filepath.EvalSymlinks(hwmonDir)
	if err == nil && afterSensor != sensorPath {
		return intelSysfsFailed
	}
	gm.Lock()
	defer gm.Unlock()
	if gm.collectorStopped || time.Since(started) > 10*time.Second {
		return intelSysfsFailed
	}
	id := gm.intelSysfsID(cardPath, device)
	if id == "" {
		return intelSysfsFailed
	}
	if gm.intelSysfsSensorPaths == nil {
		gm.intelSysfsSensorPaths = make(map[string]string)
	}
	if gm.intelSysfsSensorPaths[id] != sensorPath {
		delete(gm.intelSysfsEnergySnapshots, id)
		for key := range gm.intelSysfsEnergySnapshots {
			if strings.HasPrefix(key, id+":") {
				delete(gm.intelSysfsEnergySnapshots, key)
			}
		}
		gm.intelSysfsSensorPaths[id] = sensorPath
	}
	if gm.intelSysfsSamples == nil {
		gm.intelSysfsSamples = make(map[string]time.Time)
		gm.intelSysfsPackageCounts = make(map[string]uint32)
		gm.intelSysfsPackageValid = make(map[string]bool)
	}
	gm.intelSysfsSamples[id] = time.Time{}
	gm.intelSysfsPackageValid[id] = false

	if energyErr != nil {
		delete(gm.intelSysfsEnergySnapshots, id)
		// No interval may bridge an unreadable counter.
		for key := range gm.intelSysfsEnergySnapshots {
			if strings.HasPrefix(key, id+":") {
				delete(gm.intelSysfsEnergySnapshots, key)
			}
		}
		return intelSysfsFailed
	}

	previous, hadBaseline := gm.intelSysfsEnergySnapshots[id]
	power, hasPower := gm.calculateIntelSysfsPower(id, energy, now)
	powerPkg, hasPowerPkg := gm.calculateIntelSysfsPackagePower(id, pkgPath, pkgEnergy, pkgErr, now)

	gpu, ok := gm.GpuDataMap[id]
	if !ok {
		if gm.intelSysfsNames == nil {
			gm.intelSysfsNames = make(map[string]string)
		}
		if stable := gm.intelSysfsNames[device]; stable != "" {
			name = stable
		} else {
			gm.intelSysfsNames[device] = name
		}
		gpu = &system.GPUData{Name: name}
		gm.GpuDataMap[id] = gpu
	}

	gpu.Temperature, gpu.MemoryUsed, gpu.MemoryTotal = 0, 0, 0
	if usageErr == nil && hasPower {
		gpu.Usage += usage
	}
	if memUsedErr == nil {
		gpu.MemoryUsed = utils.BytesToMegabytes(memUsed)
	}
	if memTotalErr == nil {
		gpu.MemoryTotal = utils.BytesToMegabytes(memTotal)
	}
	if temp > 0 {
		gpu.Temperature = temp
	}
	if hasPower {
		gpu.Power += power
		slog.Debug("Computed Intel sysfs GPU power", "card", id, "watts", power)
	}
	if hasPower && hasPowerPkg {
		gpu.PowerPkg += powerPkg
		gm.intelSysfsPackageCounts[id]++
		gm.intelSysfsPackageValid[id] = true
	}
	if hasPower {
		gpu.Count++
		gm.intelSysfsSamples[id] = observed
	}
	if hasPower {
		return intelSysfsMeasured
	}
	if !hadBaseline || now.Sub(previous.timestamp) > 15*time.Second {
		return intelSysfsPriming
	}
	return intelSysfsFailed
}

func (gm *GPUManager) calculateIntelSysfsPower(cardID string, microjoules uint64, timestamp time.Time) (float64, bool) {
	if gm.intelSysfsEnergySnapshots == nil {
		gm.intelSysfsEnergySnapshots = make(map[string]intelSysfsEnergySnapshot)
	}

	last, ok := gm.intelSysfsEnergySnapshots[cardID]
	gm.intelSysfsEnergySnapshots[cardID] = intelSysfsEnergySnapshot{microjoules: microjoules, timestamp: timestamp}
	if !ok {
		return 0, false
	}
	if microjoules < last.microjoules {
		slog.Debug("Intel sysfs energy counter reset", "card", cardID)
		return 0, false
	}
	elapsed := timestamp.Sub(last.timestamp).Seconds()
	if elapsed <= 0 || elapsed > 15 {
		return 0, false
	}
	delta := microjoules - last.microjoules
	watts := float64(delta) / 1_000_000.0 / elapsed
	// A conservative sanity ceiling, not the configured or rated power limit.
	// Reject corrupted samples without clamping them into plausible readings.
	if watts > 2000 {
		return 0, false
	}
	return watts, true
}

func (gm *GPUManager) readIntelSysfsPowerPkg(cardID, hwmonDir string, timestamp time.Time) (float64, bool) {
	selected, energy, err := readIntelSysfsPackageCounter(hwmonDir)
	return gm.calculateIntelSysfsPackagePower(cardID, selected, energy, err, timestamp)
}

func readIntelSysfsPackageCounter(hwmonDir string) (string, uint64, error) {
	energyPaths, _ := filepath.Glob(filepath.Join(hwmonDir, "energy*_input"))
	selected, fallback := "", ""
	for _, path := range energyPaths {
		if filepath.Base(path) == "energy1_input" {
			continue
		}
		label, err := utils.ReadStringFileLimited(strings.TrimSuffix(path, "_input")+"_label", 64)
		if err == nil {
			if strings.TrimSpace(label) == "pkg" {
				selected = path
				break
			}
		} else if os.IsNotExist(err) && filepath.Base(path) == "energy2_input" {
			fallback = path
		}
	}
	if selected == "" {
		selected = fallback
	}
	if selected == "" {
		return "", 0, os.ErrNotExist
	}
	energy, err := readSysfsUint(selected)
	return selected, energy, err
}

func (gm *GPUManager) calculateIntelSysfsPackagePower(cardID, selected string, energy uint64, err error, timestamp time.Time) (float64, bool) {
	key := cardID + ":pkg:" + selected
	// A changed/absent source invalidates its old interval, even if its values
	// happen to be monotonic relative to the previous sensor's counter.
	for old := range gm.intelSysfsEnergySnapshots {
		if strings.HasPrefix(old, cardID+":pkg:") && old != key {
			delete(gm.intelSysfsEnergySnapshots, old)
		}
	}
	if selected == "" {
		return 0, false
	}
	if err != nil {
		delete(gm.intelSysfsEnergySnapshots, key)
		return 0, false
	}
	return gm.calculateIntelSysfsPower(key, energy, timestamp)
}

func readIntelSysfsTemperature(hwmonDir string) float64 {
	tempPaths, _ := filepath.Glob(filepath.Join(hwmonDir, "temp*_input"))
	for _, path := range tempPaths {
		temp, err := readOptionalSysfsFloat(path)
		if err == nil && temp > 0 {
			return temp / 1000.0
		}
	}
	return 0
}

func readSysfsUint(path string) (uint64, error) {
	val, err := readIntelSysfsNumber(path)
	if err != nil {
		slog.Debug("Failed to read sysfs value", "path", path, "error", err)
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(val), 10, 64)
}

func readOptionalSysfsFloat(path string) (float64, error) {
	val, err := readIntelSysfsNumber(path)
	if err != nil {
		return 0, err
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return 0, fmt.Errorf("invalid sysfs number")
	}
	return number, nil
}

func readFirstOptionalSysfsFloat(paths ...string) (float64, error) {
	for _, path := range paths {
		val, err := readOptionalSysfsFloat(path)
		if err == nil {
			return val, nil
		}
	}
	return 0, fmt.Errorf("no sysfs values found")
}

func getIntelSysfsGpuName(cardPath string) string {
	devicePath := filepath.Join(cardPath, "device")
	if product, err := utils.ReadStringFileLimited(filepath.Join(devicePath, "product_name"), 128); err == nil && strings.TrimSpace(product) != "" {
		return strings.TrimSpace(product)
	}
	if name, err := utils.ReadStringFileLimited(filepath.Join(devicePath, "name"), 128); err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return fmt.Sprintf("Intel GPU %s", filepath.Base(cardPath))
}

// intelSysfsID preserves the first observed cardN key across re-enumeration in
// this agent lifetime. Resolve the PCI device symlink, not the DRM card symlink.
// History across process restarts retains legacy behavior (no identity migration).
// Caller holds gm's mutex. The alias table has an explicit lifetime bound.
func intelSysfsDevice(cardPath string) string {
	device, err := filepath.EvalSymlinks(filepath.Join(cardPath, "device"))
	if err != nil {
		device = filepath.Join(cardPath, "device")
	}
	return device
}

// Pure state operation: caller holds the manager mutex; no filesystem access.
func (gm *GPUManager) intelSysfsID(cardPath, device string) string {
	if id := gm.intelSysfsIDs[device]; id != "" {
		return id
	}
	if len(gm.intelSysfsIDs) >= 64 {
		return ""
	}
	if gm.intelSysfsIDs == nil {
		gm.intelSysfsIDs = make(map[string]string)
	}
	id := filepath.Base(cardPath)
	for _, used := range gm.intelSysfsIDs {
		if used == id {
			id += "-" + filepath.Base(device)
			break
		}
	}
	gm.intelSysfsIDs[device] = id
	return id
}

// Read one bounded sysfs number, rejecting truncation instead of accepting its
// numeric prefix. The extra byte detects values longer than the ABI permits.
func readIntelSysfsNumber(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65))
	if err != nil {
		return "", err
	}
	if len(data) > 64 {
		return "", fmt.Errorf("sysfs number exceeds 64 bytes")
	}
	return string(data), nil
}

func (gm *GPUManager) intelSysfsStopped() bool {
	gm.Lock()
	defer gm.Unlock()
	return gm.collectorStopped
}

// At most one health entry per bounded stable identity plus the discovery entry.
// Log outside the manager lock; warn on first failure and at most once a minute,
// and only report recovery after an actual valid interval (never just priming).
func (gm *GPUManager) reportIntelSysfsHealth(id string, status intelSysfsSampleStatus, now time.Time) {
	gm.Lock()
	if gm.collectorStopped {
		gm.Unlock()
		return
	}
	last, failed := gm.intelSysfsFailures[id]
	warn := status == intelSysfsFailed && (!failed || now.Sub(last) >= time.Minute)
	recovered := status == intelSysfsMeasured && failed
	if warn {
		if gm.intelSysfsFailures == nil {
			gm.intelSysfsFailures = make(map[string]time.Time)
		}
		gm.intelSysfsFailures[id] = now
	}
	if recovered {
		delete(gm.intelSysfsFailures, id)
	}
	gm.Unlock()
	if warn {
		slog.Warn("Intel sysfs power unavailable", "card", id, "state", "failed", "reason", "no usable power sample")
	}
	if recovered {
		slog.Info("Intel sysfs power recovered", "card", id, "state", "recovered")
	}
}
