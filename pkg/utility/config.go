package utility

import (
	"fmt"

	"github.com/levenlabs/go-lflag"
)

// Configured sets up the utility provider based on flags.
func Configured() Provider {
	provider := lflag.String("utility-provider", "comed", "Utility provider to use (available: comed)")

	var p struct{ Provider }

	// Configure implementations
	comed := configuredComEd()

	lflag.Do(func() {
		switch *provider {
		case "comed":
			if err := comed.Validate(); err != nil {
				panic(fmt.Sprintf("comed validation failed: %v", err))
			}
			p.Provider = comed
		default:
			panic(fmt.Sprintf("unknown utility provider: %s", *provider))
		}
	})

	return &p
}
