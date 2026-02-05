package ess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFranklin(t *testing.T) {
	t.Run("Login Flow", func(t *testing.T) {
		// Mock Server
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				// Verify payload
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "user@example.com", r.Form.Get("account"))

				// Return success token
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result": map[string]interface{}{
						"token": "fake-token-123",
					},
				})
				return
			}
			http.Error(w, "not found", 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			username:  "user@example.com",
			password:  "pass",
			gatewayID: "GW123",
		}

		err := f.login(context.Background())
		require.NoError(t, err, "login should succeed")

		assert.Equal(t, "fake-token-123", f.tokenStr, "token should match")
	})

	t.Run("AutoFetchGatewayID", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result": map[string]interface{}{
						"token": "tok",
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getHomeGatewayList" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result": []map[string]interface{}{
						{"id": "AUTO-GW-123"},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:   ts.Client(),
			baseURL:  ts.URL,
			username: "u",
			password: "p",
			// No gatewayID
		}

		err := f.login(context.Background())
		require.NoError(t, err, "login should succeed")
		assert.Equal(t, "AUTO-GW-123", f.gatewayID, "Should auto-fetch gateway ID")
	})

	t.Run("GetStatus", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"totalCap": 30.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"globalGridChargeMax": 15.0, "gridFeedMaxFlag": 2, "gridMaxFlag": 2},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 138224.0, "workMode": 2.0, "soc": 88.5}, // Matches current SOC -> Standby
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 138224.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				runtimeData := map[string]interface{}{
					"soc":   88.5,
					"p_fhp": 1500.0,
					"mode":  138224.0, // Self consumption ID
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result": map[string]interface{}{
						"runtimeData":     runtimeData,
						"currentWorkMode": 2,
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			username:  "u",
			password:  "p",
			gatewayID: "g",
			settings:  types.Settings{MinBatterySOC: 10},
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err, "GetStatus should succeed")

		assert.Equal(t, 88.5, status.BatterySOC, "BatterySOC should match")
		assert.Equal(t, 30.0, status.BatteryCapacityKWH, "BatteryCapacityKWH should match")
		assert.True(t, status.CanExportSolar, "CanExportSolar should be true")
		assert.True(t, status.CanExportBattery, "CanExportBattery should be true")
		assert.True(t, status.CanImportBattery, "CanImportBattery should be true")
		assert.True(t, status.ElevatedMinBatterySOC, "ElevatedMinBatterySOC should be true")
	})

	t.Run("SetModes", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 11111.0, "workMode": 1.0}, // TOU
					{"id": 22222.0, "workMode": 2.0}, // Self-consumption
					{"id": 33333.0, "workMode": 3.0}, // Backup
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"list": list},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				callOrder = append(callOrder, "updateTouModeV2")
				require.NoError(t, r.ParseForm())
				// We expect SetModes(BatteryModeLoad) -> soc=MinBatterySOC (e.g. 20)
				// This test setup is specific to how SetModes is implemented
				// For Load/SelfConsumption, it sets mode 2 (self-consumption).
				assert.Equal(t, "2", r.Form.Get("workMode"), "workMode should be 2")
				assert.Equal(t, "22222", r.Form.Get("currendId"), "currendId should match")

				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{}})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			username:  "u",
			password:  "p",
			gatewayID: "g",
		}

		// Set settings so MinBatterySOC is set
		err := f.ApplySettings(context.Background(), types.Settings{MinBatterySOC: 20})
		require.NoError(t, err, "ApplySettings should succeed")

		err = f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeAny)
		require.NoError(t, err, "SetModes should succeed")

		// Verify the expected call was made
		require.Len(t, callOrder, 1, "updateTouModeV2 should be called")
		assert.Equal(t, "updateTouModeV2", callOrder[0])
	})

	t.Run("SetModes Charge", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 10.0, "workMode": 1.0},
					{"id": 20.0, "workMode": 2.0, "editSocFlag": true},
					{"id": 30.0, "workMode": 3.0},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"list": list},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 0, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				// We expect it to enable generic grid charging (flag=2)
				var data map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				assert.EqualValues(t, 2, data["gridMaxFlag"], "gridMaxFlag should be 2")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				callOrder = append(callOrder, "updateTouModeV2")
				require.NoError(t, r.ParseForm())
				// ChargeAny sets SOC to 100
				assert.Equal(t, "100", r.Form.Get("soc"), "soc should be 100")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			username:  "u",
			password:  "p",
			gatewayID: "g",
		}

		// SetModes(ChargeAny)
		err := f.ApplySettings(context.Background(), types.Settings{GridChargeBatteries: true})
		require.NoError(t, err, "ApplySettings should succeed")
		err = f.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny)
		require.NoError(t, err, "SetModes should succeed")

		// Verify both calls were made
		require.Len(t, callOrder, 2, "both updateTouModeV2 and setPowerControlV2 should be called")
		assert.Equal(t, "updateTouModeV2", callOrder[0], "updateTouModeV2 should be called first")
		assert.Equal(t, "setPowerControlV2", callOrder[1], "setPowerControlV2 should be called second")
	})

	t.Run("SetModes Both Mode and PowerControl Updates", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 20.0, "workMode": 2.0, "electricityType": 1.0, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateSocV2" {
				callOrder = append(callOrder, "updateSocV2")
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "100", r.Form.Get("soc"), "soc should be 100 for ChargeAny")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": nil})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				var data map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				// Should set gridFeedMaxFlag to 1 (solar only export)
				assert.EqualValues(t, 1, data["gridFeedMaxFlag"], "gridFeedMaxFlag should be 1 for SolarModeAny with GridExportSolar=true")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			username:  "u",
			password:  "p",
			gatewayID: "g",
		}

		// Set settings to enable grid export for solar
		err := f.ApplySettings(context.Background(), types.Settings{
			MinBatterySOC:   20,
			GridExportSolar: true,
		})
		require.NoError(t, err)

		// This should update both SOC (to 100 for charging) AND power control (to enable solar export)
		err = f.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny)
		require.NoError(t, err, "SetModes should succeed")

		// Verify both API calls were made
		require.Len(t, callOrder, 2, "both updateSocV2 and setPowerControlV2 should be called")
		assert.Equal(t, "updateSocV2", callOrder[0], "updateSocV2 should be called first")
		assert.Equal(t, "setPowerControlV2", callOrder[1], "setPowerControlV2 should be called second")
	})

	t.Run("SetModes NoChange", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			http.Error(w, "should not be called: "+r.URL.Path+" "+r.Method, 500)
		}))
		defer ts.Close()
		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			tokenStr:    "valid-token",
			tokenExpiry: time.Now().Add(time.Hour),
		}
		err := f.SetModes(context.Background(), types.BatteryModeNoChange, types.SolarModeNoChange)
		require.NoError(t, err, "SetModes should succeed (noop)")
	})

	t.Run("SetModes Partial NoChange", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 20.0, "workMode": 2.0, "soc": 55.0},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 1, "gridFeedMaxFlag": 2},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				var data map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				// Should set gridFeedMaxFlag to 3 (no export) since SolarModeAny with GridExportSolar=false (default)
				assert.EqualValues(t, 3, data["gridFeedMaxFlag"], "gridFeedMaxFlag should be 3 for no export")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			username:  "u",
			password:  "p",
			gatewayID: "g",
		}

		err := f.SetModes(context.Background(), types.BatteryModeNoChange, types.SolarModeAny)
		require.NoError(t, err, "SetModes should succeed")

		// Verify only setPowerControlV2 was called (BatteryModeNoChange doesn't update mode/SOC)
		require.Len(t, callOrder, 1, "only setPowerControlV2 should be called")
		assert.Equal(t, "setPowerControlV2", callOrder[0])
	})

	t.Run("SetModes UpdateSOC Only", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 20.0, "workMode": 2.0, "electricityType": 1.0, "soc": 55.0, "canEditReserveSOC": true},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateSocV2" {
				callOrder = append(callOrder, "updateSocV2")
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "20", r.Form.Get("soc"), "soc should be updated to MinBatterySOC")
				assert.Equal(t, "2", r.Form.Get("workMode"))
				assert.Equal(t, "1", r.Form.Get("electricityType"))

				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": nil})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				t.Error("Should not call updateTouModeV2")
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			username:  "u",
			password:  "p",
			gatewayID: "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{MinBatterySOC: 20})
		require.NoError(t, err)

		err = f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange)
		require.NoError(t, err, "SetModes should succeed")

		// Verify only updateSocV2 was called (not updateTouModeV2)
		require.Len(t, callOrder, 1, "only updateSocV2 should be called")
		assert.Equal(t, "updateSocV2", callOrder[0])
	})

	t.Run("SetPowerControl", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 0, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				var data map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))

				assert.EqualValues(t, 0, data["gridMaxFlag"], "gridMaxFlag should be 0 (disabled)")
				assert.EqualValues(t, 2, data["gridFeedMaxFlag"], "gridFeedMaxFlag should be 2 (enabled)")
				assert.EqualValues(t, 5.5, data["gridFeedMax"], "gridFeedMax should be 5.5")

				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200.0, "success": true, "result": map[string]interface{}{}})
				return
			}
			http.Error(w, "not found", 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			username:  "u",
			password:  "p",
			gatewayID: "g",
		}

		config := types.PowerControlConfig{
			GridChargeEnabled: false,
			GridExportEnabled: true,
			GridExportMax:     5.5,
		}

		err := f.SetPowerControl(context.Background(), config)
		require.NoError(t, err, "SetPowerControl should succeed")

		// Verify setPowerControlV2 was called
		require.Len(t, callOrder, 1, "setPowerControlV2 should be called")
		assert.Equal(t, "setPowerControlV2", callOrder[0])
	})

	t.Run("GetEnergyHistory", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result":  map[string]interface{}{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result": map[string]interface{}{
						"zoneInfo": "America/Chicago",
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				return
			}
			if r.URL.Path == "/api-energy/power/getFhpPowerByDay" {
				dayTime := r.URL.Query().Get("dayTime")
				// We expect the day in America/Chicago.
				// Start is 2026-02-01 18:00 UTC -> 2026-02-01 12:00 CST.
				assert.Equal(t, "2026-02-01", dayTime, "dayTime should match")

				// Return mock data with 3 timestamps to define 2 intervals in the 12:00 hour
				// 12:00:00 -> 12:15:00 (15 min = 0.25h)
				// 12:15:00 -> 13:00:00 (45 min = 0.75h)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200.0,
					"success": true,
					"result": map[string]interface{}{
						"deviceTimeArray": []string{
							"2026-02-01 12:00:00",
							"2026-02-01 12:15:00",
							"2026-02-01 13:00:00",
						},
						// SocArray length must match
						"socArray": []float64{50.0, 50.0, 50.0},
						// SolarToHome:
						// 1st interval: 4.0 kW * 0.25 h = 1.0 kWh
						// 2nd interval: 0.0 kW * 0.75 h = 0.0 kWh
						// Total = 1.0
						"powerSolarHomeArray": []float64{4.0, 0.0, 0.0},

						// BatteryToHome:
						// 1st interval: 8.0 kW * 0.25 h = 2.0 kWh
						// 2nd interval: 4.0 kW * 0.75 h = 3.0 kWh
						// Total = 5.0
						"powerFhpHomeArray": []float64{8.0, 4.0, 0.0},

						// Arrays must be same length (3)
						"powerSolarGirdArray": []float64{0.0, 0.0, 0.0},
						"powerSolarFhpArray":  []float64{0.0, 0.0, 0.0},
						"powerGirdFhpArray":   []float64{0.0, 0.0, 0.0},
						"powerGirdHomeArray":  []float64{0.0, 0.0, 0.0},
						"powerFhpGirdArray":   []float64{0.0, 0.0, 0.0},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			username:  "u",
			password:  "p",
			gatewayID: "g",
		}

		// Requesting 12:00 to 13:00 in Chicago time
		// 12:00 CST is 18:00 UTC
		start, _ := time.Parse(time.RFC3339, "2026-02-01T18:00:00Z")
		end, _ := time.Parse(time.RFC3339, "2026-02-01T19:00:00Z")

		stats, err := f.GetEnergyHistory(context.Background(), start, end)
		require.NoError(t, err, "GetEnergyHistory should succeed")
		require.Len(t, stats, 1, "should have 1 stat for the hour")

		s := stats[0]
		// HomeKWH = SolarToHome + GridToHome + BatToHome
		// SolarToHome = 1.0
		// BatToHome = 5.0
		// GridToHome = 0
		// Total Home = 6.0
		assert.InDelta(t, 6.0, s.HomeKWH, 0.01, "HomeKWH mismatch")

		assert.InDelta(t, 1.0, s.SolarKWH, 0.01, "SolarKWH mismatch")
		assert.InDelta(t, 5.0, s.BatteryUsedKWH, 0.01, "BatteryUsedKWH mismatch")
	})
}
