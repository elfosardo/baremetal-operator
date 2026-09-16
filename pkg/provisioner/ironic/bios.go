package ironic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/baremetal/v1/nodes"
	metal3api "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
)

// ironicBIOSSetting is an Ironic BIOS setting decoded without Gophercloud's
// Extract() path. Gophercloud first unmarshals JSON into any, so numbers become
// float64. Bounds at signed 64-bit maximum (for example Cisco CoreDisableMask
// and SgxEpoch* attributes) round to 9223372036854776000 and then fail:
//
//	json: cannot unmarshal number 9223372036854776000 into Go struct field BIOSSetting.bios.upper_bound of type int
//
// Decoding the raw payload with json.Number preserves exact integer bounds.
// Bounds that still do not fit in int are omitted so one unusable schema field
// cannot block HostFirmwareSettings for every other setting.
type ironicBIOSSetting struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
	//nolint:tagliatelle
	AttributeType string `json:"attribute_type"`
	//nolint:tagliatelle
	AllowableValues []string `json:"allowable_values"`
	//nolint:tagliatelle
	LowerBound *json.Number `json:"lower_bound"`
	//nolint:tagliatelle
	UpperBound *json.Number `json:"upper_bound"`
	//nolint:tagliatelle
	MinLength *int `json:"min_length"`
	//nolint:tagliatelle
	MaxLength *int `json:"max_length"`
	//nolint:tagliatelle
	ReadOnly *bool `json:"read_only"`
	Unique   *bool `json:"unique"`
}

func listNodeBIOSSettings(ctx context.Context, client *gophercloud.ServiceClient, nodeID string, includeSchema bool) ([]ironicBIOSSetting, error) {
	url := client.ServiceURL("nodes", nodeID, "bios")
	if includeSchema {
		query, err := nodes.ListBIOSSettingsOpts{Detail: true}.ToListBIOSSettingsOptsQuery()
		if err != nil {
			return nil, err
		}
		url += query
	}

	// Decode into json.RawMessage so integer bounds are not rounded through float64.
	var raw json.RawMessage
	resp, err := client.Get(ctx, url, &raw, &gophercloud.RequestOpts{
		OkCodes: []int{http.StatusOK},
	})
	_, _, err = gophercloud.ParseResponse(resp, err)
	if err != nil {
		return nil, err
	}

	return parseBIOSSettingsJSON(raw)
}

func parseBIOSSettingsJSON(data []byte) ([]ironicBIOSSetting, error) {
	var parsed struct {
		Settings []ironicBIOSSetting `json:"bios"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("could not decode BIOS settings: %w", err)
	}
	return parsed.Settings, nil
}

func firmwareSettingsFromBIOS(settingsList []ironicBIOSSetting, includeSchema bool, log logr.Logger) (metal3api.SettingsMap, map[string]metal3api.SettingSchema) {
	settings := make(metal3api.SettingsMap, len(settingsList))
	schema := make(map[string]metal3api.SettingSchema, len(settingsList))

	for _, v := range settingsList {
		settings[v.Name] = biosValueToString(v.Value)

		if includeSchema {
			schema[v.Name] = metal3api.SettingSchema{
				AttributeType:   v.AttributeType,
				AllowableValues: v.AllowableValues,
				LowerBound:      intBoundFromJSON(log, v.Name, "lower_bound", v.LowerBound),
				UpperBound:      intBoundFromJSON(log, v.Name, "upper_bound", v.UpperBound),
				MinLength:       v.MinLength,
				MaxLength:       v.MaxLength,
				ReadOnly:        v.ReadOnly,
				Unique:          v.Unique,
			}
		}
	}

	return settings, schema
}

func biosValueToString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func intBoundFromJSON(log logr.Logger, setting, field string, n *json.Number) *int {
	if n == nil {
		return nil
	}
	i64, err := n.Int64()
	if err == nil && i64 >= int64(math.MinInt) && i64 <= int64(math.MaxInt) {
		i := int(i64)
		return &i
	}
	log.Info("omitting BIOS integer bound that cannot be represented as int",
		"setting", setting, "field", field, "value", n.String())
	return nil
}
