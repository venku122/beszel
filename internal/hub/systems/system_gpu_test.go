//go:build testing

package systems

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/henrygd/beszel/internal/common"
	"github.com/henrygd/beszel/internal/hub/transport"
	"testing"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateRecordsGPUUtilization(t *testing.T) {
	sys, app := newTestSystemWithHub(t)
	for _, tc := range []struct {
		name  string
		gpu   bool
		usage float64
	}{
		{"no GPU", false, 0},
		{"active GPU", true, 42.5},
		{"idle GPU", true, 0},
		{"GPU removed", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &system.CombinedData{Info: system.Info{GpuPct: tc.usage, Cpu: 12.5}}
			if tc.gpu {
				data.Stats.GPUData = map[string]system.GPUData{"0": {Name: "GPU", Usage: tc.usage}}
			}
			_, err := sys.createRecords(data)
			require.NoError(t, err)
			record, err := app.FindRecordById("systems", sys.Id)
			require.NoError(t, err)
			var info map[string]any
			require.NoError(t, record.UnmarshalJSONField("info", &info))
			assert.Equal(t, 12.5, info["cpu"])
			if tc.gpu {
				assert.Equal(t, tc.usage, info["g"])
			} else {
				assert.NotContains(t, info, "g")
			}
		})
	}
}

// The collector's consumer-window regression verifies 16 W survives a missing
// final package sample. Verify that value also survives wire decoding and actual
// hub persistence, while an unavailable package reading omits the JSON field.
func TestCreateRecordsGPUPackagePowerTransport(t *testing.T) {
	for _, tc := range []struct {
		name    string
		power   float64
		present bool
	}{
		{"valid interval before final missing sample", 16, true},
		{"unavailable interval", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sys, app := newTestSystemWithHub(t)
			payload, err := cbor.Marshal(system.CombinedData{Stats: system.Stats{GPUData: map[string]system.GPUData{"card0": {Name: "Intel Arc B570", Power: 30, PowerPkg: tc.power}}}})
			require.NoError(t, err)
			wire, err := cbor.Marshal(common.AgentResponse{Data: payload})
			require.NoError(t, err)
			var response common.AgentResponse
			require.NoError(t, cbor.Unmarshal(wire, &response))
			var received system.CombinedData
			require.NoError(t, transport.UnmarshalResponse(response, common.GetData, &received))
			_, err = sys.createRecords(&received)
			require.NoError(t, err)
			records, err := app.FindAllRecords("system_stats")
			require.NoError(t, err)
			require.Len(t, records, 1)
			var stats struct {
				GPU map[string]map[string]any `json:"g"`
			}
			require.NoError(t, records[0].UnmarshalJSONField("stats", &stats))
			require.Equal(t, 30.0, stats.GPU["card0"]["p"])
			if tc.present {
				require.Equal(t, 16.0, stats.GPU["card0"]["pp"])
			} else {
				require.NotContains(t, stats.GPU["card0"], "pp")
			}
		})
	}
}
