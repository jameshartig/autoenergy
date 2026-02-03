package utility

import (
	"context"

	"github.com/jameshartig/autoenergy/pkg/types"
)

// Provider defines the interface for fetching energy prices.
type Provider interface {
	// GetCurrentPrice returns the current price of electricity.
	GetCurrentPrice(ctx context.Context) (types.Price, error)

	// LastConfirmedPrice returns the last confirmed price of electricity.
	LastConfirmedPrice(ctx context.Context) (types.Price, error)

	// GetFuturePrices returns a list of future prices.
	GetFuturePrices(ctx context.Context) ([]types.Price, error)
}
