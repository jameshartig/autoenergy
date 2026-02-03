package ess

import (
	"context"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
)

// System defines the interface for interacting with an Energy Storage System (like FranklinWH).
type System interface {
	// GetStatus returns the current status of the system.
	GetStatus(ctx context.Context) (types.SystemStatus, error)

	// SetModes sets the operating modes of the system.
	SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error

	// ApplySettings updates the system using the provided global settings.
	ApplySettings(ctx context.Context, settings types.Settings) error

	// GetEnergyHistory returns the energy history for the specified period.
	GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error)
}
