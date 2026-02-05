package types

import "time"

// Price represents the cost of electricity in a time interval.
type Price struct {
	TSStart       time.Time `json:"tsStart"`
	TSEnd         time.Time `json:"tsEnd"`
	DollarsPerKWH float64   `json:"dollarsPerKWH"`
}

// ActionType represents the type of action taken by the system.
type ActionType string

const (
	ActionTypeCharge    ActionType = "charge"
	ActionTypeDischarge ActionType = "discharge"
	ActionTypeIdle      ActionType = "idle"
)

// Action represents a control decision made by the system.
type Action struct {
	Timestamp    time.Time    `json:"timestamp"`
	BatteryMode  BatteryMode  `json:"batteryMode"`
	SolarMode    SolarMode    `json:"solarMode"`
	Description  string       `json:"description"`
	CurrentPrice Price        `json:"currentPrice"`
	SystemStatus SystemStatus `json:"systemStatus"`
	DryRun       bool         `json:"dryRun,omitempty"`
}

// EnergyStats represents aggregated energy statistics for an hourly period.
type EnergyStats struct {
	TSHourStart time.Time `json:"tsHourStart"`

	// Totals
	BatteryChargedKWH float64 `json:"batteryChargedKWH"`
	BatteryUsedKWH    float64 `json:"batteryUsedKWH"`
	SolarKWH          float64 `json:"solarKWH"`
	HomeKWH           float64 `json:"homeKWH"`
	GridExportKWH     float64 `json:"gridExportKWH"`
	GridImportKWH     float64 `json:"gridImportKWH"`

	// Source to destination
	BatteryToHomeKWH  float64 `json:"batteryToHomeKWH"`
	SolarToHomeKWH    float64 `json:"solarToHomeKWH"`
	SolarToBatteryKWH float64 `json:"solarToBatteryKWH"`
	SolarToGridKWH    float64 `json:"solarToGridKWH"`
	BatteryToGridKWH  float64 `json:"batteryToGridKWH"`
}

// SystemStatus represents the current system status.
type SystemStatus struct {
	Timestamp             time.Time `json:"timestamp"`
	BatterySOC            float64   `json:"batterySOC"`            // 0-100
	EachBatterySOC        []float64 `json:"eachBatterySOC"`        // 0-100
	BatteryKW             float64   `json:"batteryKW"`             // Positive for discharge, negative for charge
	EachBatteryKW         []float64 `json:"eachBatteryKW"`         // Positive for discharge, negative for charge
	BatteryCapacityKWH    float64   `json:"batteryCapacityKWH"`    // Total capacity of the battery (kWh)
	MaxBatteryChargeKW    float64   `json:"maxBatteryChargeKW"`    // Maximum charge rate of the battery (kW)
	MaxBatteryDischargeKW float64   `json:"maxBatteryDischargeKW"` // Maximum discharge rate of the battery (kW)
	SolarKW               float64   `json:"solarKW"`               // Solar generation (kW)
	GridKW                float64   `json:"gridKW"`                // Grid import/export (kW, + import, - export)
	HomeKW                float64   `json:"homeKW"`                // Home consumption (kW)
	CanExportSolar        bool      `json:"canExportSolar"`        // True if solar exporting is enabled
	CanExportBattery      bool      `json:"canExportBattery"`      // True if battery exporting is enabled
	CanImportBattery      bool      `json:"canImportBattery"`      // True if battery importing is enabled
	ElevatedMinBatterySOC bool      `json:"elevatedMinBatterySOC"` // True if the minimum SOC is elevated to force standby
	EmergencyMode         bool      `json:"emergencyMode"`
}

// 0: standby, 1: charge, -1: discharge
type BatteryMode int

const (
	BatteryModeNoChange    BatteryMode = 0
	BatteryModeStandby     BatteryMode = 1
	BatteryModeChargeAny   BatteryMode = 2
	BatteryModeChargeSolar BatteryMode = 3
	BatteryModeLoad        BatteryMode = -1
)

type SolarMode int

const (
	SolarModeNoChange SolarMode = 0
	SolarModeNoExport SolarMode = 1
	SolarModeAny      SolarMode = 2
	// TODO: SolarModeExportOnly SolarMode = 2
)

type PowerControlConfig struct {
	GridChargeEnabled bool    `json:"gridChargeEnabled"`
	GridExportEnabled bool    `json:"gridExportEnabled"`
	GridExportMax     float64 `json:"gridExportMax"`
}
