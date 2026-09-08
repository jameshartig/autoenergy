package types

import "time"

const (
	CurrentEnergyStatsVersion  = 3
	CurrentPriceHistoryVersion = 2
	CurrentWeatherVersion      = 4 // Note: Version 5 was burned in staging/development and should not be reused

	SiteIDNone = "none"
)

// Site represents a household or location that has a battery and solar panels.
type Site struct {
	ID            string                              `json:"id"`
	InviteCode    string                              `json:"inviteCode"`
	Permissions   []SitePermissions                   `json:"permissions"`
	Notifications map[string]UserNotificationSettings `json:"notifications,omitempty"`
}

// SitePermissions represents the permissions for a user on a site.
type SitePermissions struct {
	UserID string `json:"userID"`
}

// UserSite represents a site on a user
type UserSite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// User represents a user of the system.
type User struct {
	ID            string             `json:"id"`
	Email         string             `json:"email"`
	Sites         []UserSite         `json:"sites"`
	Admin         bool               `json:"-"`
	SessionSecret string             `json:"sessionSecret,omitempty"`
	Subscriptions []PushSubscription `json:"subscriptions,omitempty"`
}

// ActionReason represents the type of action taken by the system.
type ActionReason string

const (
	ActionReasonAlwaysChargeBelowThreshold  ActionReason = "alwaysChargeBelowThreshold"
	ActionReasonMissingBattery              ActionReason = "missingBattery"
	ActionReasonDeficitChargeNow            ActionReason = "deficitCharge"
	ActionReasonArbitrageChargeExport       ActionReason = "arbitrageChargeExport"
	ActionReasonDischargeBeforeCapacityNow  ActionReason = "dischargeBeforeCapacity"
	ActionReasonDeficitSaveForPeak          ActionReason = "deficitSaveForPeak"
	ActionReasonDischargeAtPeak             ActionReason = "dischargeAtPeak"
	ActionReasonArbitrageHoldExport         ActionReason = "arbitrageHoldExport"
	ActionReasonSufficientBattery           ActionReason = "sufficientBattery"
	ActionReasonSufficientBatteryTillCharge ActionReason = "sufficientBatteryTillCharge"
	ActionReasonEmergencyMode               ActionReason = "emergencyMode"
	ActionReasonHasAlarms                   ActionReason = "hasAlarms"
	ActionReasonGridUnavailable             ActionReason = "gridUnavailable"
	ActionReasonWaitingToCharge             ActionReason = "waitingToCharge"
	ActionReasonPreventSolarCurtailment     ActionReason = "preventSolarCurtailment"
	ActionReasonHoldSimilarPrice            ActionReason = "holdSimilarPrice"
	ActionReasonBatteryAtReserve            ActionReason = "batteryAtReserve"
	ActionReasonVPPActive                   ActionReason = "vppActive"
	ActionReasonVPPPrep                     ActionReason = "vppPrep"
	ActionReasonEVChargingStandby           ActionReason = "evChargingStandby"

	// Deprecated - we don't use these anymore but we don't delete them so we know they were used
	ActionReasonArbitrageHoldSave   ActionReason = "arbitrageHoldSave"
	ActionReasonArbitrageChargeSave ActionReason = "arbitrageChargeSave"
	ActionReasonArbitrageChargeNow  ActionReason = "arbitrageCharge"
	ActionReasonArbitrageHold       ActionReason = "arbitrageHold"
	ActionReasonDeficitSave         ActionReason = "deficitSave"
)

// ModesOptions contains options for setting the operating modes.
type ModesOptions struct {
	ChargeToSOC int `json:"chargeToSoc,omitempty"`
	MinimumSOC  int `json:"minimumSoc,omitempty"`
}

// Action represents a control decision made by the system.
type Action struct {
	Timestamp              time.Time        `json:"timestamp"`
	SystemTimestamp        time.Time        `json:"systemTimestamp,omitempty"`
	BatteryMode            BatteryMode      `json:"batteryMode"`
	SolarMode              SolarMode        `json:"solarMode"`
	ChargeToSOC            int              `json:"chargeToSoc,omitempty"`
	Reason                 ActionReason     `json:"reason"`
	Description            string           `json:"description"`
	CurrentPrice           *Price           `json:"currentPrice,omitempty"`
	FuturePrice            *Price           `json:"futurePrice,omitempty"`
	SystemStatus           SystemStatus     `json:"systemStatus"`
	HitDeficitAt           time.Time        `json:"deficitAt"`
	HitBufferedDeficitAt   time.Time        `json:"hitBufferedDeficitAt"`
	HitThresholdDeficitAt  time.Time        `json:"hitThresholdDeficitAt"`
	HitCapacityAt          time.Time        `json:"capacityAt"`
	StrategyBenefitDollars float64          `json:"strategyBenefitDollars,omitempty"`
	DryRun                 bool             `json:"dryRun,omitempty"`
	Fault                  bool             `json:"fault,omitempty"`
	Failed                 bool             `json:"failed,omitempty"`
	Paused                 bool             `json:"paused,omitempty"`
	Error                  string           `json:"error,omitempty"`
	SimulationParams       SimulationParams `json:"simulationParams,omitempty"`

	// Deprecated: use BatteryMode
	TargetBatteryMode BatteryMode `json:"targetBatteryMode,omitempty"`
	// Deprecated: use SolarMode
	TargetSolarMode SolarMode `json:"targetSolarMode,omitempty"`
}

// SimulationParams holds calculated or calibrated solar parameters from
// the weather and history simulation run.
type SimulationParams struct {
	// ClippingCapKWH is the learned solar generation limit in kWh.
	ClippingCapKWH float64 `json:"clippingCapKWH"`
	// PanelAzimuth is the detected/calibrated best panel azimuth direction.
	PanelAzimuth float64 `json:"panelAzimuth"`
	// PanelTilt is the detected/calibrated best panel tilt angle.
	PanelTilt float64 `json:"panelTilt"`
	// AverageSolarEfficiency is the average efficiency calculated across all non-zero generation hours.
	AverageSolarEfficiency float64 `json:"averageSolarEfficiency"`
	// DetectedShift represents any detected structural load shift ("none", "down", "up").
	DetectedShift string `json:"detectedShift,omitempty"`
}

// EnergyStats represents aggregated energy statistics for an hourly period.
type EnergyStats struct {
	TSHourStart  time.Time `json:"tsHourStart"`
	TimeLocation string    `json:"timeLocation,omitempty"`

	// Battery Stats
	MinBatterySOC float64 `json:"minBatterySOC"`
	MaxBatterySOC float64 `json:"maxBatterySOC"`

	// Totals
	BatteryChargedKWH float64 `json:"batteryChargedKWH"`
	BatteryUsedKWH    float64 `json:"batteryUsedKWH"`
	SolarKWH          float64 `json:"solarKWH"`
	HomeKWH           float64 `json:"homeKWH"`
	GridExportKWH     float64 `json:"gridExportKWH"`
	GridImportKWH     float64 `json:"gridImportKWH"`
	VPPExportKWH      float64 `json:"vppExportKWH,omitempty"`

	// Source to destination
	BatteryToHomeKWH  float64 `json:"batteryToHomeKWH"`
	SolarToHomeKWH    float64 `json:"solarToHomeKWH"`
	SolarToBatteryKWH float64 `json:"solarToBatteryKWH"`
	SolarToGridKWH    float64 `json:"solarToGridKWH"`
	BatteryToGridKWH  float64 `json:"batteryToGridKWH"`

	// Miscellaneous
	Alarms []SystemAlarm `json:"alarms,omitempty"`
}

// DailyEnergyStats represents aggregated energy statistics for a day.
type DailyEnergyStats struct {
	TSDayStart   time.Time     `json:"tsDayStart"`
	TimeLocation string        `json:"timeLocation,omitempty"`
	Hourly       []EnergyStats `json:"hourly"`
}

// SystemAlarm represents a single alarm condition.
type SystemAlarm struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
	Code        string    `json:"code"`
}

// Storm represents a storm warning.
type Storm struct {
	Description string    `json:"description"`
	TSStart     time.Time `json:"tsStart"`
	TSEnd       time.Time `json:"tsEnd"`
}

// VPPEvent represents a virtual power plant event.
type VPPEvent struct {
	Description string    `json:"description"`
	TSStart     time.Time `json:"tsStart"`
	TSEnd       time.Time `json:"tsEnd"`
	VPPSoc      float64   `json:"vppSoc"`
	OptOut      bool      `json:"optOut"`
}

// SystemStatus represents the current system status.
type SystemStatus struct {
	Timestamp               time.Time     `json:"timestamp"`
	TimeLocation            string        `json:"timeLocation,omitempty"`
	BatterySOC              float64       `json:"batterySOC"`            // 0-100
	BatteryKW               float64       `json:"batteryKW"`             // Positive for discharge, negative for charge
	BatteryCapacityKWH      float64       `json:"batteryCapacityKWH"`    // Total capacity of the battery (kWh)
	MaxBatteryChargeKW      float64       `json:"maxBatteryChargeKW"`    // Maximum charge rate of the battery (kW)
	MaxBatteryDischargeKW   float64       `json:"maxBatteryDischargeKW"` // Maximum discharge rate of the battery (kW)
	SolarKW                 float64       `json:"solarKW"`               // Solar generation (kW)
	GridKW                  float64       `json:"gridKW"`                // Grid import/export (kW, + import, - export)
	HomeKW                  float64       `json:"homeKW"`                // Home consumption (kW)
	ElevatedMinBatterySOC   bool          `json:"elevatedMinBatterySOC"` // True if the minimum SOC is elevated to force standby
	BatteryAboveMinSOC      bool          `json:"batteryAboveMinSOC"`    // True if the battery SOC is above the minimum SOC
	EmergencyMode           bool          `json:"emergencyMode,omitempty"`
	GridUnavailable         bool          `json:"gridUnavailable,omitempty"`         // True if the grid is unavailable
	BatteryChargingDisabled bool          `json:"batteryChargingDisabled,omitempty"` // True if battery charging is disabled due to alarms
	Alarms                  []SystemAlarm `json:"alarms,omitempty"`
	Storms                  []Storm       `json:"storms,omitempty"`
	VPPActive               bool          `json:"vppActive,omitempty"` // True if VPP is currently controlling
	VPPKW                   float64       `json:"vppKW,omitempty"`     // Power going to/from the VPP
	VPPSOC                  float64       `json:"vppSOC,omitempty"`    // VPP target SOC
	VPPEvents               []VPPEvent    `json:"vppEvents,omitempty"`
}

// BatteryMode represents the mode of the battery.
type BatteryMode int

const (
	BatteryModeNoChange  BatteryMode = 0
	BatteryModeStandby   BatteryMode = 1
	BatteryModeChargeAny BatteryMode = 2
	BatteryModeLoad      BatteryMode = -1
)

// SolarMode represents the mode of the solar panels.
type SolarMode int

const (
	SolarModeNoChange SolarMode = 0
	SolarModeNoExport SolarMode = 1
	SolarModeAny      SolarMode = 2
	// TODO: we could add SolarModeExportOnly SolarMode = 2 but right now no ESS system supports this mode
)

// Feedback represents feedback submitted by a user.
type Feedback struct {
	ID        string            `json:"id"`
	Sentiment string            `json:"sentiment"`
	Comment   string            `json:"comment"`
	SiteID    string            `json:"siteID"`
	UserID    string            `json:"userID"`
	Extra     map[string]string `json:"extra"`
	Timestamp time.Time         `json:"timestamp"`
}

// SiteLocation represents the geographical location of a site.
type SiteLocation struct {
	PostalCode   string  `json:"postalCode"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	City         string  `json:"city"`
	CountryCode  string  `json:"countryCode"`
	TimeZone     string  `json:"timeZone"`
	Elevation    float64 `json:"elevation"`
	SolarAzimuth float64 `json:"solarAzimuth"`
	SolarTilt    float64 `json:"solarTilt"`
}

// HourlyWeather represents the solar forecast data for a specific hour.
type HourlyWeather struct {
	TSHourStart       time.Time `json:"tsHourStart"`
	GHI               float64   `json:"ghi,omitempty"`               // Shortwave radiation in W/m²
	GTI               float64   `json:"gti,omitempty"`               // Global tilted irradiance in W/m²
	DHI               float64   `json:"dhi,omitempty"`               // Diffuse radiation in W/m²
	DNI               float64   `json:"dni,omitempty"`               // Direct normal irradiance in W/m²
	TemperatureC      float64   `json:"temperatureC,omitempty"`      // Temperature in °C
	SnowfallCM        float64   `json:"snowfallCM,omitempty"`        // Snowfall in cm
	SnowDepthCM       float64   `json:"snowDepthCM,omitempty"`       // Snow depth in cm
	CloudCoverPercent float64   `json:"cloudCoverPercent,omitempty"` // Cloud cover
}

// Weather represents the daily weather forecast data.
type Weather struct {
	TSDayStart    time.Time       `json:"tsDayStart"`
	TimeLocation  string          `json:"timeLocation"`
	Latitude      float64         `json:"latitude"`
	Longitude     float64         `json:"longitude"`
	TSSunrise     time.Time       `json:"tsSunrise"`
	TSSunset      time.Time       `json:"tsSunset"`
	TSUpdated     time.Time       `json:"tsUpdated"`
	ForecastHours []HourlyWeather `json:"forecastHours"`
}

// HistorySummary represents the monthly history data stored in the history_summary Firestore collection.
type HistorySummary struct {
	TSMonthStart time.Time          `json:"tsMonthStart"`
	Energy       []DailyEnergyStats `json:"energy"`
	Weather      []Weather          `json:"weather"`
}

// EVSession represents a detected EV charging session.
type EVSession struct {
	TSStartHour time.Time `json:"tsStartHour"`
	TSEndHour   time.Time `json:"tsEndHour"`
	DurationHr  int       `json:"durationHr"`
	PeakKW      float64   `json:"peakKW"`
	AvgKW       float64   `json:"avgKW"`
	TotalKWH    float64   `json:"totalKWH"`
	NetStepKW   float64   `json:"netStepKW"`
}

// EVDetectionResult contains the output of EV charging estimation across history.
type EVDetectionResult struct {
	Detected           bool         `json:"detected"`
	RecommendedPeriod  TimePeriod   `json:"recommendedPeriod"`
	AllDetectedPeriods []TimePeriod `json:"allDetectedPeriods,omitempty"`
	EstimatedRateKW    float64      `json:"estimatedRateKW"`
	SessionsCount      int          `json:"sessionsCount"`
	Sessions           []EVSession  `json:"sessions,omitempty"`
	Message            string       `json:"message,omitempty"`
}

// PushSubscriptionKeys holds the client P256DH and Auth secrets from PushSubscription.toJSON().
type PushSubscriptionKeys struct {
	P256DH string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// PushSubscription represents a single subscribed browser/device target.
type PushSubscription struct {
	ID        string               `json:"id"`
	Endpoint  string               `json:"endpoint"`
	Keys      PushSubscriptionKeys `json:"keys"`
	UserAgent string               `json:"userAgent,omitempty"`
	TSCreated time.Time            `json:"tsCreated"`
}

// Notification types sent by the push notification system.
const (
	NotificationTypeMorningSummary       = "morning_summary"
	NotificationTypeEveningSummary       = "evening_summary"
	NotificationTypeGridOutage           = "grid_outage"
	NotificationTypeGridRestored         = "grid_restored"
	NotificationTypePriceSpike           = "price_spike"
	NotificationTypeSolarUnderproduction = "solar_underproduction"
	NotificationTypeVPPDispatch          = "vpp_dispatch"
)

// UserNotificationSettings holds notification preferences for a specific user on a specific site.
type UserNotificationSettings struct {
	MorningSummaryEnabled bool   `json:"morningSummaryEnabled"`
	MorningSummaryHour    int    `json:"morningSummaryHour"`
	MorningSummaryFlavor  string `json:"morningSummaryFlavor"`

	EveningSummaryEnabled bool   `json:"eveningSummaryEnabled"`
	EveningSummaryHour    int    `json:"eveningSummaryHour"`
	EveningSummaryFlavor  string `json:"eveningSummaryFlavor"`

	GridOutageAlert           bool   `json:"gridOutageAlert"`
	PriceSpikeAlert           string `json:"priceSpikeAlert,omitempty"`           // "", "low", "medium", "high"
	SolarUnderproductionAlert string `json:"solarUnderproductionAlert,omitempty"` // "", "low", "medium", "high"
	VPPDispatchAlert          bool   `json:"vppDispatchAlert"`
}

// NotificationLog records a sent push notification for debugging and click analysis.
type NotificationLog struct {
	ID         string    `json:"id"`
	TSCreated  time.Time `json:"tsCreated"`
	UserID     string    `json:"userID"`
	Endpoint   string    `json:"endpoint"`
	Type       string    `json:"type"`
	Flavor     string    `json:"flavor"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Success    bool      `json:"success"`
	StatusCode int       `json:"statusCode"`
	Error      string    `json:"error,omitempty"`
	Clicked    bool      `json:"clicked,omitempty"`
	TSClicked  time.Time `json:"tsClicked,omitempty"`
}

// MonthlyNotificationLogs groups notifications by month in UTC.
type MonthlyNotificationLogs struct {
	TSMonthStart time.Time         `json:"tsMonthStart"`
	Logs         []NotificationLog `json:"logs"`
}
