package storage

import (
	"context"
	"fmt"

	"github.com/levenlabs/go-lflag"
)

// Configured sets up the Storage provider based on flags.
func Configured() Provider {
	provider := lflag.String("storage-provider", "firestore", "Storage provider to use (available: firestore)")

	var p struct{ Provider }

	fs := configuredFirestore()

	lflag.Do(func() {
		switch *provider {
		case "firestore":
			if err := fs.Validate(); err != nil {
				panic(fmt.Sprintf("firestore validation failed: %v", err))
			}
			p.Provider = fs
			if err := fs.Init(context.Background()); err != nil {
				panic(fmt.Sprintf("firestore init failed: %v", err))
			}
		default:
			panic(fmt.Sprintf("unknown storage provider: %s", *provider))
		}
	})

	return &p
}
