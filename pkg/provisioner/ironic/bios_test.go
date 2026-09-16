package ironic

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/go-logr/logr"
	metal3api "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/hardwareutils/bmc"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/clients"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/testserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetFirmwareSettings(t *testing.T) {
	nodeUUID := "158c5d59-9ace-9631-ed51-d842a45f1c52"
	iTrue := true
	iFalse := false
	minLength := 0
	maxLength := 16
	lowerBound := 0
	upperBound := 20
	maxBound := math.MaxInt

	cases := []struct {
		name                string
		nodeUUID            string
		expectedSettingsMap metal3api.SettingsMap
		expectedSchemaMap   map[string]metal3api.SettingSchema
		includeSchema       bool
		ironic              *testserver.IronicMock
		expectedError       string
	}{
		{
			name:     "no-schema",
			nodeUUID: nodeUUID,
			expectedSettingsMap: metal3api.SettingsMap{
				"L2Cache":            "10x256 KB",
				"NumCores":           "10",
				"ProcVirtualization": "Enabled",
			},
			expectedSchemaMap: map[string]metal3api.SettingSchema{},
			ironic:            testserver.NewIronic(t).BIOSSettings(nodeUUID),
			includeSchema:     false,
			expectedError:     "",
		},
		{
			name:     "include-schema",
			nodeUUID: nodeUUID,
			expectedSettingsMap: metal3api.SettingsMap{
				"L2Cache":            "10x256 KB",
				"NumCores":           "10",
				"ProcVirtualization": "Enabled",
			},
			expectedSchemaMap: map[string]metal3api.SettingSchema{
				"L2Cache": {
					AttributeType:   "String",
					AllowableValues: []string{},
					LowerBound:      nil,
					UpperBound:      nil,
					MinLength:       &minLength,
					MaxLength:       &maxLength,
					ReadOnly:        &iTrue,
					Unique:          nil,
				},
				"NumCores": {
					AttributeType:   "Integer",
					AllowableValues: []string{},
					LowerBound:      &lowerBound,
					UpperBound:      &upperBound,
					MinLength:       nil,
					MaxLength:       nil,
					ReadOnly:        &iTrue,
					Unique:          nil,
				},
				"ProcVirtualization": {
					AttributeType:   "Enumeration",
					AllowableValues: []string{"Enabled", "Disabled"},
					LowerBound:      nil,
					UpperBound:      nil,
					MinLength:       nil,
					MaxLength:       nil,
					ReadOnly:        &iFalse,
					Unique:          nil,
				},
			},
			ironic:        testserver.NewIronic(t).BIOSDetailSettings(nodeUUID),
			includeSchema: true,
			expectedError: "",
		},
		{
			name:                "error404",
			nodeUUID:            nodeUUID,
			expectedSettingsMap: metal3api.SettingsMap(nil),
			expectedSchemaMap:   map[string]metal3api.SettingSchema(nil),
			ironic:              testserver.NewIronic(t).NoBIOS(nodeUUID),
			includeSchema:       false,
			expectedError:       "could not get BIOS settings for node .* got 404 instead.*",
		},
		{
			name:                "not-registered",
			expectedSettingsMap: metal3api.SettingsMap(nil),
			expectedSchemaMap:   map[string]metal3api.SettingSchema(nil),
			ironic:              testserver.NewIronic(t).NoBIOS(nodeUUID),
			includeSchema:       false,
			expectedError:       "could not get BIOS settings: host not registered",
		},
		{
			name:     "int64-max-bound-does-not-block-other-settings",
			nodeUUID: nodeUUID,
			expectedSettingsMap: metal3api.SettingsMap{
				"Numlock":             "On",
				"CoreDisableMask_0_0": "0",
				"SgxEpoch0":           "0",
			},
			expectedSchemaMap: map[string]metal3api.SettingSchema{
				"Numlock": {
					AttributeType:   "Enumeration",
					AllowableValues: []string{"On", "Off"},
					ReadOnly:        &iFalse,
				},
				"CoreDisableMask_0_0": {
					AttributeType: "Integer",
					LowerBound:    &lowerBound,
					UpperBound:    &maxBound,
					ReadOnly:      &iFalse,
				},
				"SgxEpoch0": {
					AttributeType: "Integer",
					LowerBound:    &lowerBound,
					UpperBound:    &maxBound,
					ReadOnly:      &iFalse,
				},
			},
			ironic: testserver.NewIronic(t).BIOSSettingsRaw(nodeUUID, `{
				"bios": [
					{
						"name": "Numlock",
						"value": "On",
						"attribute_type": "Enumeration",
						"allowable_values": ["On", "Off"],
						"read_only": false
					},
					{
						"name": "CoreDisableMask_0_0",
						"value": "0",
						"attribute_type": "Integer",
						"lower_bound": 0,
						"upper_bound": 9223372036854775807,
						"read_only": false
					},
					{
						"name": "SgxEpoch0",
						"value": "0",
						"attribute_type": "Integer",
						"lower_bound": 0,
						"upper_bound": 9223372036854775807,
						"read_only": false
					}
				]
			}`),
			includeSchema: true,
			expectedError: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.ironic.Start()
			defer tc.ironic.Stop()

			host := makeHost()
			host.Name = "node-1"
			host.Status.Provisioning.ID = tc.nodeUUID

			auth := clients.AuthConfig{Type: clients.NoAuth}

			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher, tc.ironic.Endpoint(), auth)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}

			settingsMap, schemaMap, err := prov.GetFirmwareSettings(t.Context(), tc.includeSchema)

			assert.Equal(t, tc.expectedSettingsMap, settingsMap)
			assert.Equal(t, tc.expectedSchemaMap, schemaMap)

			if tc.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Regexp(t, tc.expectedError, err.Error())
			}
		})
	}
}

func TestParseBIOSSettingsJSONOverflowingBound(t *testing.T) {
	// Ironic/Redfish may return integer schema bounds at signed 64-bit
	// maximum. A larger bound must not prevent decoding the rest of the list.
	payload := []byte(`{
		"bios": [
			{
				"name": "Numlock",
				"value": "On",
				"attribute_type": "Enumeration",
				"allowable_values": ["On", "Off"],
				"read_only": false
			},
			{
				"name": "CoreDisableMask_0_0",
				"value": "0",
				"attribute_type": "Integer",
				"lower_bound": 0,
				"upper_bound": 9223372036854775807,
				"read_only": false
			},
			{
				"name": "HugeBound",
				"value": 1,
				"attribute_type": "Integer",
				"lower_bound": 0,
				"upper_bound": 9223372036854775808,
				"read_only": false
			}
		]
	}`)

	settingsList, err := parseBIOSSettingsJSON(payload)
	require.NoError(t, err)

	settings, schema := firmwareSettingsFromBIOS(settingsList, true, logr.Discard())

	assert.Equal(t, "On", settings["Numlock"])
	assert.Equal(t, "0", settings["CoreDisableMask_0_0"])
	assert.Equal(t, "1", settings["HugeBound"])

	require.Contains(t, schema, "Numlock")
	assert.Equal(t, "Enumeration", schema["Numlock"].AttributeType)
	assert.Equal(t, []string{"On", "Off"}, schema["Numlock"].AllowableValues)

	require.NotNil(t, schema["CoreDisableMask_0_0"].UpperBound)
	assert.Equal(t, math.MaxInt, *schema["CoreDisableMask_0_0"].UpperBound)
	require.NotNil(t, schema["CoreDisableMask_0_0"].LowerBound)
	assert.Equal(t, 0, *schema["CoreDisableMask_0_0"].LowerBound)

	// 2^63 does not fit in int64; omit only that bound.
	assert.Nil(t, schema["HugeBound"].UpperBound)
	require.NotNil(t, schema["HugeBound"].LowerBound)
	assert.Equal(t, 0, *schema["HugeBound"].LowerBound)
}

func TestIntBoundFromJSON(t *testing.T) {
	log := logr.Discard()

	t.Run("nil", func(t *testing.T) {
		assert.Nil(t, intBoundFromJSON(log, "n", "upper_bound", nil))
	})

	t.Run("max-int64", func(t *testing.T) {
		n := json.Number("9223372036854775807")
		got := intBoundFromJSON(log, "n", "upper_bound", &n)
		require.NotNil(t, got)
		assert.Equal(t, math.MaxInt, *got)
	})

	t.Run("overflows-int64", func(t *testing.T) {
		n := json.Number("9223372036854775808")
		assert.Nil(t, intBoundFromJSON(log, "n", "upper_bound", &n))
	})

	t.Run("gophercloud-rounded-float", func(t *testing.T) {
		n := json.Number("9223372036854776000")
		assert.Nil(t, intBoundFromJSON(log, "n", "upper_bound", &n))
	})
}
