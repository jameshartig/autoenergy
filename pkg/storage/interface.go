package storage

import (
	"context"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
)

// Provider defines the interface for persisting data and retrieving settings.
type Provider interface {
	// Settings
	GetSettings(ctx context.Context) (types.Settings, error)
	SetSettings(ctx context.Context, settings types.Settings) error

	// Data Persistence
	// UpsertPrice adds or updates a price record.
	UpsertPrice(ctx context.Context, price types.Price) error
	InsertAction(ctx context.Context, action types.Action) error
	UpsertEnergyHistory(ctx context.Context, stats types.EnergyStats) error

	// History
	GetPriceHistory(ctx context.Context, start, end time.Time) ([]types.Price, error)
	GetActionHistory(ctx context.Context, start, end time.Time) ([]types.Action, error)
	GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error)
	GetLatestEnergyHistoryTime(ctx context.Context) (time.Time, error)

	// Lifecycle
	Close() error
}
