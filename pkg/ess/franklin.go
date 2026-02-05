package ess

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/levenlabs/go-lflag"
)

// Franklin implements the System interface for FranklinWH.
// It interacts with the FranklinWH API to monitor and control the energy storage system.
type Franklin struct {
	client      *http.Client
	baseURL     string
	username    string
	password    string
	md5Password string
	gatewayID   string
	tokenStr    string
	tokenExpiry time.Time
	mu          sync.Mutex
	settings    types.Settings
}

type franklinMode struct {
	ID                int
	Name              string
	WorkMode          int
	OldIndex          int
	ElectricityType   int
	ReserveSOC        float64
	CanEditReserveSOC bool
}

// configuredFranklin sets up the FranklinWH system.
func configuredFranklin() *Franklin {
	f := &Franklin{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: "https://energy.franklinwh.com",
	}

	username := lflag.String("franklin-username", "", "FranklinWH Email/Username")
	password := lflag.String("franklin-password", "", "FranklinWH Password")
	md5Password := lflag.String("franklin-md5-password", "", "FranklinWH MD5 Password")
	gatewayID := lflag.String("franklin-gateway-id", "", "FranklinWH Gateway ID (from app)")
	token := lflag.String("franklin-token", "", "FranklinWH Access Token (optional override)")

	lflag.Do(func() {
		f.username = *username
		f.password = *password
		f.md5Password = *md5Password
		f.gatewayID = *gatewayID
		f.tokenStr = *token
	})

	return f
}

// Validate ensures that the required credentials are set.
func (f *Franklin) Validate() error {
	if f.tokenStr == "" && (f.username == "" || (f.password == "" && f.md5Password == "")) {
		return fmt.Errorf("franklin credentials (token or username/password) are required")
	}

	// we use to require login to validate but that means for every start up we
	// need to login even if we don't end up using franklin at all
	//return f.login(context.TODO())
	return nil
}

type loginResult struct {
	UserID  int    `json:"userId"`
	Token   string `json:"token"`
	Version string `json:"version"`
}

// login handles authentication using MD5 hashed password.
func (f *Franklin) login(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.tokenStr != "" && time.Now().Before(f.tokenExpiry) {
		return nil
	}

	// Create MD5 hash of the password
	var pwdHash string
	if f.md5Password != "" {
		pwdHash = f.md5Password
	} else {
		hasher := md5.New()
		hasher.Write([]byte(f.password))
		pwdHash = hex.EncodeToString(hasher.Sum(nil))
	}

	data := url.Values{}
	data.Set("account", f.username)
	data.Set("password", pwdHash)
	data.Set("type", "0")

	req, err := f.newPostFormRequest(ctx, "hes-gateway/terminal/initialize/appUserOrInstallerLogin", data)
	if err != nil {
		return err
	}

	var res loginResult
	if err := f.doRequest(req, &res); err != nil {
		slog.ErrorContext(ctx, "franklin login failed", "error", err)
		return fmt.Errorf("login failed: %w", err)
	}
	slog.DebugContext(ctx, "franklin login success")

	f.tokenStr = res.Token
	// TODO: what is the actual expiry of the token?
	f.tokenExpiry = time.Now().Add(1 * time.Hour) // Token expiry assumption

	if f.gatewayID == "" {
		id, err := f.getDefaultGatewayID(ctx)
		if err != nil {
			return fmt.Errorf("failed to get default gateway id: %w", err)
		}
		f.gatewayID = id
		slog.InfoContext(ctx, "automatically selected gateway", slog.String("id", f.gatewayID))
	}

	return nil
}

func (f *Franklin) newPostFormRequest(ctx context.Context, endpoint string, data url.Values) (*http.Request, error) {
	u, err := url.Parse(f.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, endpoint)
	if err != nil {
		return nil, err
	}

	body := strings.NewReader(data.Encode())
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func (f *Franklin) newGetRequest(ctx context.Context, endpoint string, params url.Values) (*http.Request, error) {
	u, err := url.Parse(f.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, endpoint)
	if err != nil {
		return nil, err
	}

	u.RawQuery = params.Encode()
	return http.NewRequestWithContext(ctx, "GET", u.String(), nil)
}

func (f *Franklin) newPostQueryRequest(ctx context.Context, endpoint string, params url.Values) (*http.Request, error) {
	u, err := url.Parse(f.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, endpoint)
	if err != nil {
		return nil, err
	}

	u.RawQuery = params.Encode()
	return http.NewRequestWithContext(ctx, "POST", u.String(), nil)
}

func (f *Franklin) newPostJSONRequest(ctx context.Context, endpoint string, data interface{}) (*http.Request, error) {
	u, err := url.Parse(f.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, endpoint)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

type franklinResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
	Success bool            `json:"success"`
}

func (f *Franklin) doRequest(req *http.Request, dest interface{}) error {
	req.Header.Set("logintoken", f.tokenStr)

	// TODO: should we set user-agent, softwareversion, lang, optsystemversion, opttime, optdevicename, optsource, optdevice

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// TODO: handle token expiry and automatically renew
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var fr franklinResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&fr); err != nil {
		slog.ErrorContext(req.Context(), "failed to decode franklin response", slog.Any("error", err), slog.String("body", string(body)))
		return err
	}

	if !fr.Success && fr.Code != 200 {
		if fr.Message == "" {
			slog.ErrorContext(req.Context(), "franklin api unknown error", slog.String("body", string(body)))
			return fmt.Errorf("franklin unknown error")
		}
		slog.ErrorContext(req.Context(), "franklin api error", slog.String("message", fr.Message))
		return fmt.Errorf("franklin api error: %s", fr.Message)
	}

	if dest != nil {
		if err := json.Unmarshal(fr.Result, dest); err != nil {
			slog.ErrorContext(req.Context(), "failed to decode franklin result", slog.Any("error", err))
			return fmt.Errorf("failed to decode franklin result: %w", err)
		}
	} else {
		slog.DebugContext(req.Context(), "franklin request success (no destination)", slog.String("url", req.URL.String()))
	}

	return nil
}

func (f *Franklin) getRuntimeData(ctx context.Context) (deviceCompositeInfoResult, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)
	// 0 is set on the first call and subsequent calls should set to 1
	params.Set("refreshFlag", "0")

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/getDeviceCompositeInfo", params)
	if err != nil {
		return deviceCompositeInfoResult{}, err
	}

	var res deviceCompositeInfoResult
	if err := f.doRequest(req, &res); err != nil {
		return deviceCompositeInfoResult{}, fmt.Errorf("getDeviceCompositeInfo failed: %w", err)
	}

	// solar inverters/combiners use power so it can be negative, just set it to 0
	if res.RuntimeData.PowerSolar < 0 {
		res.RuntimeData.PowerSolar = 0
	}

	slog.DebugContext(ctx, "franklin runtime data",
		slog.Float64("soc", res.RuntimeData.SOC),
		slog.Float64("solar_kw", res.RuntimeData.PowerSolar),
		slog.Float64("grid_kw", res.RuntimeData.PowerGrid),
		slog.Float64("load_kw", res.RuntimeData.PowerLoad),
		slog.Float64("battery_kw", res.RuntimeData.PowerBattery),
	)

	return res, nil
}

func (f *Franklin) getDefaultGatewayID(ctx context.Context) (string, error) {
	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/getHomeGatewayList", nil)
	if err != nil {
		return "", err
	}

	var list []homeGateway
	if err := f.doRequest(req, &list); err != nil {
		return "", err
	}

	if len(list) == 1 {
		return list[0].ID, nil
	}
	return "", fmt.Errorf("found %d gateways, expected 1", len(list))
}

func (f *Franklin) getDeviceInfo(ctx context.Context) (deviceInfoV2Result, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)
	params.Set("lang", "en_US")

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/getDeviceInfoV2", params)
	if err != nil {
		return deviceInfoV2Result{}, err
	}

	var res deviceInfoV2Result
	if err := f.doRequest(req, &res); err != nil {
		return deviceInfoV2Result{}, err
	}

	return res, nil
}

// GetStatus returns the status of the franklin system
func (f *Franklin) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	slog.DebugContext(ctx, "getting franklin system status")
	if err := f.login(ctx); err != nil {
		return types.SystemStatus{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	rd, err := f.getRuntimeData(ctx)
	if err != nil {
		return types.SystemStatus{}, err
	}

	di, err := f.getDeviceInfo(ctx)
	if err != nil {
		return types.SystemStatus{}, err
	}

	modes, err := f.getAvailableModes(ctx)
	if err != nil {
		return types.SystemStatus{}, err
	}

	pc, err := f.getPowerControl(ctx)
	if err != nil {
		return types.SystemStatus{}, err
	}

	return types.SystemStatus{
		BatterySOC:            rd.RuntimeData.SOC,
		EachBatterySOC:        rd.RuntimeData.EachSOC,
		BatteryKW:             rd.RuntimeData.PowerBattery,
		EachBatteryKW:         rd.RuntimeData.PowerEachBattery,
		SolarKW:               rd.RuntimeData.PowerSolar,
		GridKW:                rd.RuntimeData.PowerGrid,
		HomeKW:                rd.RuntimeData.PowerLoad,
		BatteryCapacityKWH:    di.TotalBatteryCapacityKWH,
		EmergencyMode:         modes.currentMode.WorkMode == 3,
		CanExportSolar:        pc.GridFeedMaxFlag == GridFeedMaxFlagSolarOnly || pc.GridFeedMaxFlag == GridFeedMaxFlagBatteryAndSolar,
		CanExportBattery:      pc.GridFeedMaxFlag == GridFeedMaxFlagBatteryAndSolar,
		CanImportBattery:      pc.GridMaxFlag == GridMaxFlagChargeFromGrid,
		ElevatedMinBatterySOC: modes.currentMode.ReserveSOC > 0 && modes.currentMode.ReserveSOC > f.settings.MinBatterySOC,

		// TODO: get this from hes-gateway/common/getPowerCapConfigList
		MaxBatteryChargeKW:    8 * float64(len(rd.RuntimeData.EachSOC)),
		MaxBatteryDischargeKW: 10 * float64(len(rd.RuntimeData.EachSOC)),
	}, nil
}

func (f *Franklin) getPowerControl(ctx context.Context) (getPowerControlSettingResult, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/tou/getPowerControlSetting", params)
	if err != nil {
		return getPowerControlSettingResult{}, err
	}

	var res getPowerControlSettingResult
	if err := f.doRequest(req, &res); err != nil {
		return getPowerControlSettingResult{}, err
	}

	slog.DebugContext(
		ctx,
		"franklin power control",
		slog.Int("gridMaxFlag", int(res.GridMaxFlag)),
		slog.Int("gridFeedMaxFlag", int(res.GridFeedMaxFlag)),
		slog.Float64("gridMax", res.GridMax),
		slog.Float64("gridFeedMax", res.GridFeedMax),
	)

	return res, nil
}

// SetPowerControl sets the power control configuration for the franklin system
func (f *Franklin) SetPowerControl(ctx context.Context, cfg types.PowerControlConfig) error {
	slog.DebugContext(ctx, "SetPowerControl called", "config", cfg)
	if err := f.login(ctx); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	pc, err := f.getPowerControl(ctx)
	if err != nil {
		return err
	}

	var updated bool
	if cfg.GridChargeEnabled {
		if pc.GridMaxFlag != 2 {
			pc.GridMaxFlag = 2
			updated = true
		}
	} else {
		// Default to disabled?
		if pc.GridMaxFlag != 0 {
			pc.GridMaxFlag = 0 // Assuming 0 is disabled
			updated = true
		}
	}

	if cfg.GridExportEnabled {
		if cfg.GridExportMax > 0 {
			pc.GridFeedMax = cfg.GridExportMax
		}
		if pc.GridFeedMaxFlag != 2 { // 2: Battery+Solar export ? Or 1: Solar only?
			// Test expects 2 for enabled
			pc.GridFeedMaxFlag = 2
			updated = true
		}
	} else {
		if pc.GridFeedMaxFlag != 3 { // 3: No export
			pc.GridFeedMaxFlag = 3
			updated = true
		}
	}

	if updated {
		return f.setPowerControl(ctx, pc)
	}
	return nil
}

func (f *Franklin) setPowerControl(ctx context.Context, pc getPowerControlSettingResult) error {
	data := map[string]interface{}{
		"gatewayId": f.gatewayID,
		// TODO: what is -1 here?
		"gridMax":     pc.GridMax,
		"gridMaxFlag": pc.GridMaxFlag,
	}
	// if we have no export, we don't set the gridFeedMax
	if pc.GridFeedMaxFlag != 3 {
		if pc.GridFeedMax < 0 {
			pc.GridFeedMax = -1.0
		}
		data["gridFeedMax"] = pc.GridFeedMax
	}
	data["gridFeedMaxFlag"] = pc.GridFeedMaxFlag

	slog.DebugContext(
		ctx,
		"setting power control",
		slog.Float64("gridMax", pc.GridMax),
		slog.Int("gridMaxFlag", int(pc.GridMaxFlag)),
		slog.Float64("gridFeedMax", pc.GridFeedMax),
		slog.Int("gridFeedMaxFlag", int(pc.GridFeedMaxFlag)),
	)

	req, err := f.newPostJSONRequest(ctx, "hes-gateway/terminal/tou/setPowerControlV2", data)
	if err != nil {
		return err
	}

	// TODO: powerControlTipMsg
	return f.doRequest(req, &struct{}{})
}

type availableModes struct {
	list              []franklinMode
	selfConsumption   franklinMode
	currentMode       franklinMode
	stormHedgeEnabled int
}

func (f *Franklin) getAvailableModes(ctx context.Context) (availableModes, error) {
	params := url.Values{}
	params.Set("showType", "1")
	params.Set("gatewayId", f.gatewayID)

	req, err := f.newPostQueryRequest(ctx, "hes-gateway/terminal/tou/getGatewayTouListV2", params)
	if err != nil {
		return availableModes{}, err
	}

	var res gatewayTouListV2Result
	if err := f.doRequest(req, &res); err != nil {
		return availableModes{}, err
	}

	var sc franklinMode
	var current franklinMode
	modes := make([]franklinMode, len(res.List))
	for i, item := range res.List {
		m := franklinMode{
			ID:                item.ID,
			Name:              item.Name,
			WorkMode:          item.WorkMode,
			OldIndex:          item.OldIndex,
			ElectricityType:   item.ElectricityType,
			ReserveSOC:        item.ReserveSOC,
			CanEditReserveSOC: item.CanEditReserveSOC,
		}
		switch item.WorkMode {
		case 1: // time-of-use
			modes[i] = m
		case 2: // self consumption
			modes[i] = m
			sc = modes[i]
		case 3: // backup
			modes[i] = m
		default:
			slog.WarnContext(
				ctx,
				"unknown work mode",
				slog.Int("id", item.ID),
				slog.String("name", item.Name),
				slog.Int("workMode", item.WorkMode),
				slog.Int("oldIndex", item.OldIndex),
			)
		}
		if item.ID == res.CurrentID {
			current = franklinMode{
				ID:              item.ID,
				Name:            item.Name,
				WorkMode:        item.WorkMode,
				OldIndex:        item.OldIndex,
				ElectricityType: item.ElectricityType,
				ReserveSOC:      item.ReserveSOC,
			}
		}
	}

	return availableModes{
		list:              modes,
		selfConsumption:   sc,
		stormHedgeEnabled: res.StormHedgeEnabled,
		currentMode:       current,
	}, nil
}

func (f *Franklin) ApplySettings(ctx context.Context, settings types.Settings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings = settings
	return nil
}

// SetModes sets the battery and solar modes for the franklin system
func (f *Franklin) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error {
	slog.DebugContext(ctx, "SetModes called", slog.Any("batteryMode", bat), slog.Any("solarMode", sol))
	if err := f.login(ctx); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if bat == types.BatteryModeNoChange && sol == types.SolarModeNoChange {
		return nil
	}

	modes, err := f.getAvailableModes(ctx)
	if err != nil {
		return err
	}

	if modes.currentMode.WorkMode == 3 {
		slog.InfoContext(ctx, "device is in backup mode, skipping set modes")
		return errors.New("device is in backup mode")
	}

	if modes.selfConsumption == (franklinMode{}) {
		slog.ErrorContext(ctx, "self consumption mode not available", slog.Any("modes", modes))
		return errors.New("self consumption mode not available")
	}
	sc := modes.selfConsumption
	alreadySC := sc.ID == modes.currentMode.ID

	data := url.Values{}
	data.Set("gatewayId", f.gatewayID)
	data.Set("currendId", fmt.Sprint(sc.ID)) // yes, this is misspelled
	data.Set("workMode", fmt.Sprint(sc.WorkMode))
	data.Set("electricityType", fmt.Sprint(sc.ElectricityType))
	data.Set("oldIndex", fmt.Sprint(sc.OldIndex))
	data.Set("stromEn", fmt.Sprint(modes.stormHedgeEnabled))

	minBatterySOC := f.settings.MinBatterySOC
	if minBatterySOC < 5 {
		minBatterySOC = 5
	}

	soc := sc.ReserveSOC

	slog.DebugContext(ctx, "existing reserve SOC", slog.Float64("reserveSOC", sc.ReserveSOC))

	pc, err := f.getPowerControl(ctx)
	if err != nil {
		return err
	}
	var updatedPC bool
	var updatedModeOrSOC bool
	switch bat {
	case types.BatteryModeChargeAny:
		// if they want to charge the battery then set the SOC to 100 to force it to
		// charge if its not charging already
		// note: since we're not setting emergency backup mode solar will still be
		// used to power the home first then spill over into the battery
		if !sc.CanEditReserveSOC {
			slog.WarnContext(ctx, "cannot edit reserve SOC")
			return errors.New("cannot edit reserve SOC")
		}
		soc = 100
		updatedModeOrSOC = true
		if f.settings.GridChargeBatteries {
			if pc.GridMaxFlag != GridMaxFlagChargeFromGrid {
				pc.GridMaxFlag = GridMaxFlagChargeFromGrid
				updatedPC = true
			}
		} else {
			if pc.GridMaxFlag != GridMaxFlagNoChargeFromGrid {
				pc.GridMaxFlag = GridMaxFlagNoChargeFromGrid
				updatedPC = true
			}
		}
	case types.BatteryModeChargeSolar:
		// we disallow charging from the grid if they only want to charge via solar
		// and otherwise set the SOC to 100
		// note: since we're not setting emergency backup mode solar will still be
		// used to power the home first then spill over into the battery
		if !sc.CanEditReserveSOC {
			slog.WarnContext(ctx, "cannot edit reserve SOC")
			return errors.New("cannot edit reserve SOC")
		}
		soc = 100
		updatedModeOrSOC = true
		if pc.GridMaxFlag != GridMaxFlagNoChargeFromGrid {
			pc.GridMaxFlag = GridMaxFlagNoChargeFromGrid
			updatedPC = true
		}
	case types.BatteryModeLoad:
		// we set the SOC to the minimum battery SOC to ensure we start discharging
		// if we're somehow less than this soc, we'll charge from the solar, unless
		// solar is unavailable then it'll charge from the grid
		// it seems like this accepts an int value
		soc = minBatterySOC
		updatedModeOrSOC = true
		if f.settings.GridChargeBatteries {
			if pc.GridMaxFlag != GridMaxFlagChargeFromGrid {
				pc.GridMaxFlag = GridMaxFlagChargeFromGrid
				updatedPC = true
			}
		} else {
			if pc.GridMaxFlag != GridMaxFlagNoChargeFromGrid {
				pc.GridMaxFlag = GridMaxFlagNoChargeFromGrid
				updatedPC = true
			}
		}
	case types.BatteryModeStandby:
		rd, err := f.getRuntimeData(ctx)
		if err != nil {
			return err
		}
		// we floor the SOC to ensure we don't set it to a value that would cause the
		// battery to charge
		// make sure we don't set it to less than the minimum battery SOC
		if !sc.CanEditReserveSOC {
			slog.WarnContext(ctx, "cannot edit reserve SOC")
			return errors.New("cannot edit reserve SOC")
		}
		soc = math.Max(math.Floor(rd.RuntimeData.SOC), minBatterySOC)
		updatedModeOrSOC = true
		if pc.GridMaxFlag != GridMaxFlagNoChargeFromGrid {
			pc.GridMaxFlag = GridMaxFlagNoChargeFromGrid
			updatedPC = true
		}
	case types.BatteryModeNoChange:
		// Do not change battery settings
	default:
		return fmt.Errorf("unknown battery mode: %v", bat)
	}

	// round to the nearest integer to minimize the chance of the battery charging
	// or discharging when we don't want it to
	data.Set("soc", strconv.Itoa(int(math.Round(soc))))

	switch sol {
	case types.SolarModeAny:
		// if settings allow us to export solar then we start exporting solar
		// this will prefer home, then charge, then export
		if f.settings.GridExportSolar {
			if pc.GridFeedMaxFlag != GridFeedMaxFlagSolarOnly {
				pc.GridFeedMaxFlag = GridFeedMaxFlagSolarOnly
				updatedPC = true
			}
		} else {
			if pc.GridFeedMaxFlag != GridFeedMaxFlagNoExport {
				pc.GridFeedMaxFlag = GridFeedMaxFlagNoExport
				updatedPC = true
			}
		}
	case types.SolarModeNoExport:
		// set the flag to not export solar
		if pc.GridFeedMaxFlag != GridFeedMaxFlagNoExport {
			pc.GridFeedMaxFlag = GridFeedMaxFlagNoExport
			updatedPC = true
		}
	case types.SolarModeNoChange:
		// Do nothing for solar
	default:
		return fmt.Errorf("unknown solar mode: %v", sol)
	}

	if updatedModeOrSOC {
		if f.settings.DryRun {
			if alreadySC {
				slog.DebugContext(
					ctx,
					"dry run: would've updated just soc",
					slog.String("soc", data.Get("soc")),
					slog.String("workMode", data.Get("workMode")),
				)
			} else {
				slog.DebugContext(
					ctx,
					"dry run: would've tou mode",
					slog.String("soc", data.Get("soc")),
					slog.String("workMode", data.Get("workMode")),
				)
			}
		} else {
			if alreadySC {
				slog.DebugContext(
					ctx,
					"updating soc",
					slog.String("soc", data.Get("soc")),
					slog.String("workMode", data.Get("workMode")),
				)
				params := url.Values{}
				params.Set("gatewayId", f.gatewayID)
				params.Set("workMode", strconv.Itoa(sc.WorkMode))
				params.Set("electricityType", strconv.Itoa(sc.ElectricityType))
				params.Set("soc", data.Get("soc"))

				req, err := f.newPostQueryRequest(ctx, "hes-gateway/terminal/tou/updateSocV2", params)
				if err != nil {
					return err
				}
				if err := f.doRequest(req, &struct{}{}); err != nil {
					slog.ErrorContext(ctx, "failed to update soc", slog.Any("error", err))
					return err
				}
			} else {
				slog.DebugContext(
					ctx,
					"updating tou mode",
					slog.String("soc", data.Get("soc")),
					slog.String("workMode", data.Get("workMode")),
				)
				req, err := f.newPostQueryRequest(ctx, "hes-gateway/terminal/tou/updateTouModeV2", data)
				if err != nil {
					return err
				}
				if err := f.doRequest(req, &struct{}{}); err != nil {
					slog.ErrorContext(ctx, "failed to update tou mode", slog.Any("error", err))
					return err
				}
			}
		}
	}

	if updatedPC {
		if f.settings.DryRun {
			slog.DebugContext(
				ctx,
				"dry run: would've set power control",
				slog.Float64("gridMax", pc.GridMax),
				slog.Int("gridMaxFlag", int(pc.GridMaxFlag)),
				slog.Float64("gridFeedMax", pc.GridFeedMax),
				slog.Int("gridFeedMaxFlag", int(pc.GridFeedMaxFlag)),
			)
		} else {
			err := f.setPowerControl(ctx, pc)
			if err != nil {
				slog.ErrorContext(ctx, "failed to set power control", slog.Any("error", err))
				return err
			}
		}
	}

	return nil
}

// GetEnergyHistory retrieves energy history for the specified period.
// It aggregates 5-minute intervals into hourly EnergyStats.
func (f *Franklin) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error) {
	if err := f.login(ctx); err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	di, err := f.getDeviceInfo(ctx)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(di.TimeZone)
	if err != nil {
		slog.WarnContext(ctx, "failed to load location, defaulting to UTC", slog.String("tz", di.TimeZone), slog.Any("error", err))
		loc = time.UTC
	}

	var allStats []types.EnergyStats

	startInLoc := start.In(loc)
	endInLoc := end.In(loc)

	// Iterate through days
	// Start from the beginning of the start day
	current := time.Date(startInLoc.Year(), startInLoc.Month(), startInLoc.Day(), 0, 0, 0, 0, loc)
	for current.Before(endInLoc) || current.Equal(endInLoc) {
		stats, err := f.getEnergyStatsForDay(ctx, current, loc)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get energy stats for day", slog.String("day", current.Format("2006-01-02")), slog.Any("error", err))
			// Continue or return error? Let's return error to be safe.
			return nil, err
		}

		// Filter stats that are within the requested range
		for _, s := range stats {
			hourEnd := s.TSHourStart.Add(time.Hour)
			if !s.TSHourStart.Before(start) && !hourEnd.After(end) {
				allStats = append(allStats, s)
			}
		}

		current = current.AddDate(0, 0, 1)
	}

	return allStats, nil
}

func (f *Franklin) getEnergyStatsForDay(ctx context.Context, day time.Time, loc *time.Location) ([]types.EnergyStats, error) {
	day = day.In(loc)
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)
	params.Set("dayTime", day.Format("2006-01-02"))

	req, err := f.newGetRequest(ctx, "api-energy/power/getFhpPowerByDay", params)
	if err != nil {
		return nil, err
	}

	var res fhpPowerByDayResult
	if err := f.doRequest(req, &res); err != nil {
		return nil, err
	}

	// Aggregate 5-min data into hourly buckets
	hourlyStats := make(map[string]*types.EnergyStats)
	var sortedKeys []string

	expLen := len(res.DeviceTimeArray)
	if len(res.SolarToHomeKWHRates) != expLen {
		slog.WarnContext(ctx, "powerSolarHomeArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.SolarToHomeKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.SolarToGridKWHRates) != expLen {
		slog.WarnContext(ctx, "powerSolarGirdArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.SolarToGridKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.SolarToBatteryKWHRates) != expLen {
		slog.WarnContext(ctx, "powerSolarFhpArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.SolarToBatteryKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.GridToBatteryKWHRates) != expLen {
		slog.WarnContext(ctx, "powerGirdFhpArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.GridToBatteryKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.GridToHomeKWHRates) != expLen {
		slog.WarnContext(ctx, "powerGirdHomeArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.GridToHomeKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.BatteryToGridKWHRates) != expLen {
		slog.WarnContext(ctx, "powerFhpGirdArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.BatteryToGridKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.BatteryToHomeKWHRates) != expLen {
		slog.WarnContext(ctx, "powerFhpHomeArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.BatteryToHomeKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.SOCArray) != expLen {
		slog.WarnContext(ctx, "socArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.SOCArray)))
		return nil, errors.New("unexpected array length in response")
	}

	for i, timeStr := range res.DeviceTimeArray {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", timeStr, loc)
		if err != nil {
			slog.WarnContext(ctx, "failed to parse time", slog.String("time", timeStr), slog.Any("error", err))
			return nil, err
		}
		// figure out the duration of this "bucket"
		var duration time.Duration
		if len(res.DeviceTimeArray) > i+1 {
			nextT, err := time.ParseInLocation("2006-01-02 15:04:05", res.DeviceTimeArray[i+1], loc)
			if err != nil {
				slog.WarnContext(ctx, "failed to parse time", slog.String("time", timeStr), slog.Any("error", err))
				return nil, err
			}
			duration = nextT.Sub(t)
		} else {
			nextDay := day.AddDate(0, 0, 1)
			nextDay = time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), 0, 0, 0, 0, loc)
			// duration until the next day?
			duration = nextDay.Sub(t)
		}

		// Determine hour bucket
		hourKey := t.Format("2006-01-02 15:00:00")
		if _, exists := hourlyStats[hourKey]; !exists {
			hourlyStats[hourKey] = &types.EnergyStats{
				TSHourStart: time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location()),
			}
			sortedKeys = append(sortedKeys, hourKey)
		}
		s := hourlyStats[hourKey]

		// collect all relevant stats for the time
		// and convert them all to KWH from the rate of kwh in that duration
		solarToHome := res.SolarToHomeKWHRates[i] * (duration.Hours())
		solarToGrid := res.SolarToGridKWHRates[i] * (duration.Hours())
		solarToBat := res.SolarToBatteryKWHRates[i] * (duration.Hours())
		gridToBat := res.GridToBatteryKWHRates[i] * (duration.Hours())
		gridToHome := res.GridToHomeKWHRates[i] * (duration.Hours())
		batToGrid := res.BatteryToGridKWHRates[i] * (duration.Hours())
		batToHome := res.BatteryToHomeKWHRates[i] * (duration.Hours())

		s.SolarKWH += (solarToHome + solarToGrid + solarToBat)
		s.BatteryChargedKWH += (solarToBat + gridToBat)
		s.BatteryUsedKWH += (batToHome + batToGrid)
		s.GridExportKWH += (solarToGrid + batToGrid)
		s.GridImportKWH += (gridToHome + gridToBat)
		s.HomeKWH += (solarToHome + gridToHome + batToHome)
		s.BatteryToHomeKWH += batToHome
		s.BatteryToGridKWH += batToGrid
		s.SolarToHomeKWH += solarToHome
		s.SolarToBatteryKWH += solarToBat
		s.SolarToGridKWH += solarToGrid
	}

	// Sort the keys in chronological order
	sort.Strings(sortedKeys)

	var result []types.EnergyStats
	for _, key := range sortedKeys {
		s := hourlyStats[key]
		result = append(result, *s)
	}

	return result, nil
}

// Internal Structs

type deviceCompositeInfoResult struct {
	CurrentWorkMode int         `json:"currentWorkMode"`
	DeviceStatus    int         `json:"deviceStatus"`
	RuntimeData     runtimeData `json:"runtimeData"`
	Valid           bool        `json:"valid"`
	// there's also "currentAlarmVOList": [], "solarHaveVo": {...}
}

type runtimeData struct {
	TOUID     int    `json:"mode"`
	ModeName  string `json:"name"`
	RunStatus int    `json:"run_status"`

	SOC     float64   `json:"soc"`
	EachSOC []float64 `json:"fhpSoc"`

	PowerBattery     float64   `json:"p_fhp"`
	PowerSolar       float64   `json:"p_sun"`
	PowerGrid        float64   `json:"p_uti"`
	PowerLoad        float64   `json:"p_load"`
	PowerGenerator   float64   `json:"p_gen"`
	PowerEachBattery []float64 `json:"fhpPower"`

	TotalBatteryCharge    float64 `json:"kwh_fhp_chg"`
	TotalBatteryDischarge float64 `json:"kwh_fhp_di"`
	TotalGridImport       float64 `json:"kwh_uti_in"`
	TotalGridExport       float64 `json:"kwh_uti_out"`
	TotalSolar            float64 `json:"kwh_sun"`
	TotalGenerator        float64 `json:"kwh_gen"`
	TotalLoad             float64 `json:"kwh_load"`

	GridChargedBattery  float64 `json:"gridChBat"`
	BatteryOutGrid      float64 `json:"batOutGrid"`
	SolarOutGrid        float64 `json:"soOutGrid"`
	SolarChargedBattery float64 `json:"soChBat"`

	// TODO: t_amb? temperature?
	// TODO: kwhSolarLoad, kwhGridLoad, kwhFhpLoad, kwhGenLoad
	// TODO: solarPower (seems to be 10x p_sun?)
}

type deviceInfoV2Result struct {
	GatewayID               string        `json:"gatewayId"`
	DeviceTime              string        `json:"deviceTime"`
	TimeZone                string        `json:"zoneInfo"`
	SystemHardwareVersion   int           `json:"sysHdVersionInt"`
	TotalBatteryCapacityKWH float64       `json:"totalCap"`
	TotalBatteryPowerKW     float64       `json:"fixedPowerTotal"`
	BatteryList             []batteryInfo `json:"batteryList"`

	// TODO: solarFlag, solarTipMsg
	// TODO: activeStatus
	// TODO: sleepStatus, blackSleepFlag
}

type batteryInfo struct {
	Serial     int `json:"id"`
	CapacityWH int `json:"rateBatCap"`
	PowerW     int `json:"ratedPwr"`
}

type gridMaxFlag int

const (
	GridMaxFlagNoChargeFromGrid gridMaxFlag = 1
	GridMaxFlagChargeFromGrid   gridMaxFlag = 2
)

type gridFeedMaxFlag int

const (
	GridFeedMaxFlagNoExport        gridFeedMaxFlag = 3
	GridFeedMaxFlagSolarOnly       gridFeedMaxFlag = 1
	GridFeedMaxFlagBatteryAndSolar gridFeedMaxFlag = 2
)

type getPowerControlSettingResult struct {
	GridFeedMax     float64         `json:"gridFeedMax"`
	GridFeedMaxFlag gridFeedMaxFlag `json:"gridFeedMaxFlag"`
	GridMax         float64         `json:"gridMax"`
	GridMaxFlag     gridMaxFlag     `json:"gridMaxFlag"`

	// TODO: difference between global and non-global?
	// TODO: globalGridDischargeMax, globalGridChargeMax, globalSettingStatus (does this being 1 mean we use global instead?)
	// TODO: peakDemandGridMax
	// TODO: isNem3, isCalifornia
}

type gatewayTouListV2Result struct {
	CurrentID int       `json:"currendId"` // yes, it's misspelled
	List      []touItem `json:"list"`

	// TODO: validate this
	StormHedgeEnabled int `json:"stromEn"`
}

type touItem struct {
	ID                 int     `json:"id"`
	OldIndex           int     `json:"oldIndex"`
	Name               string  `json:"name"`
	ReserveSOC         float64 `json:"soc"`
	MinSOC             float64 `json:"minSoc"`
	MaxSOC             float64 `json:"maxSoc"`
	CanEditReserveSOC  bool    `json:"editSocFlag"`
	WorkMode           int     `json:"workMode"`
	ElectricityType    int     `json:"electricityType"`
	BackupForeverFlag  int     `json:"backupForeverFlag"`
	TimerStartTimeUnix string  `json:"timerStartTimeZero"`

	// TODO: multiSOCFlag
	// TODO: stopMode
	// TODO: gridChargeEn
	// TODO: vppSocVo, todayVppVo
}

type fhpPowerByDayResult struct {
	SOCArray        []float64 `json:"socArray"`
	KwhTotalArray   []float64 `json:"kwhTotalArray"` // Unused for now
	RunStatusArray  []int     `json:"runStatusArray"`
	DeviceTimeArray []string  `json:"deviceTimeArray"`

	SolarToHomeKWHRates    []float64 `json:"powerSolarHomeArray"`
	SolarToGridKWHRates    []float64 `json:"powerSolarGirdArray"` // misspelled
	SolarToBatteryKWHRates []float64 `json:"powerSolarFhpArray"`

	GridToBatteryKWHRates []float64 `json:"powerGirdFhpArray"` // misspelled
	GridToHomeKWHRates    []float64 `json:"powerGirdHomeArray"`

	BatteryToGridKWHRates []float64 `json:"powerFhpGirdArray"` // misspelled
	BatteryToHomeKWHRates []float64 `json:"powerFhpHomeArray"`

	// Generators and V2L ignored for now
}

type homeGateway struct {
	ID       string `json:"id"`
	Status   int    `json:"status"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	ZoneInfo string `json:"zoneInfo"`
}
