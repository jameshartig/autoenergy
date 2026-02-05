package utility

import (
	"context"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
)

// Provider defines the interface for fetching energy prices.
type Provider interface {
	// GetCurrentPrice returns the current price of electricity.
	GetCurrentPrice(ctx context.Context) (types.Price, error)

	// GetFuturePrices returns a list of future prices.
	GetFuturePrices(ctx context.Context) ([]types.Price, error)

	// GetConfirmedPrices returns confirmed prices for a specific time range.
	// This should be used for syncing historical data.
	GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error)
}
