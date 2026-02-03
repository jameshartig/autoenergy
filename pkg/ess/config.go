package ess

import (
	"fmt"

	"github.com/levenlabs/go-lflag"
)

// Configured sets up the ESS system based on flags.
func Configured() System {
	provider := lflag.String("ess-provider", "franklin", "Energy Storage System provider to use (available: franklin)")

	var s struct{ System }

	// Configure implementations
	franklin := configuredFranklin()

	lflag.Do(func() {
		switch *provider {
		case "franklin":
			if err := franklin.Validate(); err != nil {
				panic(fmt.Sprintf("franklin validation failed: %v", err))
			}
			s.System = franklin
		default:
			panic(fmt.Sprintf("unknown ess provider: %s", *provider))
		}
	})

	return &s
}
