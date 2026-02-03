package types

// Settings represents the configuration stored in the database.
// These are dynamic settings that can be changed without redeploying.
type Settings struct {
	DryRun bool `json:"dryRun"`
	// Pause updates
	Pause bool `json:"pause"`

	// Price Settings
	// Always charge when the price is under this amount (in $/kWh)
	AlwaysChargeUnderDollarsPerKWH float64 `json:"alwaysChargeUnderDollarsPerKWH"`
	// Additional fees to add to the price when charging (in $/kWh)
	AdditionalFeesDollarsPerKWH float64 `json:"additionalFeesDollarsPerKWH"`
	// TODO: add a setting for solar credit value (in $/kWh)
	MinArbitrageDifferenceDollarsPerKWH float64 `json:"minArbitrageDifferenceDollarsPerKWH"`

	// The minimum battery SOC should be charged to at all times.
	MinBatterySOC float64 `json:"minBatterySOC"`

	// Grid Settings
	// Maximum Grid Use (in kW) (not supported yet)
	// MaxGridUseKW float64 `json:"maxGridUseKW"`
	// Can charge batteries from grid
	GridChargeBatteries bool `json:"gridChargeBatteries"`
	// Maximum Grid Export (in kW)
	//MaxGridExportKW float64 `json:"maxGridExportKW"`
	// Can export solar to grid
	GridExportSolar bool `json:"gridExportSolar"`
	// Can export batteries to grid (not supported yet)
	//GridExportBatteries bool `json:"gridExportBatteries"`
}
