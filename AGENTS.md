Build/Test/Lint
- Single test: go test -v ./pkg/<pkg> -run ^TestName$
- Package test: go test -v ./pkg/<pkg>/...
- Storage emulator tests: go test -v ./pkg/storage/... (uses FIRESTORE_EMULATOR_HOST)
- Lint/format: go fmt -w . && go vet ./...

Architecture
- cmd/autoenergy is the main entry point and orchestrator.
- pkg/controller contains decision logic for charging/discharging.
- pkg/utility fetches electricity prices (ComEd).
- pkg/ess controls the Energy Storage System (FranklinWH).
- pkg/storage persists data/config using Google Cloud Firestore.
- pkg/server exposes HTTP API endpoints for update/history.
- pkg/model and pkg/types hold shared data models and types.

Code Style
- Use go fmt formatting and standard Go import grouping.
- Keep package boundaries: controller logic in pkg/controller, external IO in utility/ess/storage/server.
- Prefer context.Context in public APIs and return (value, error).
- Use testify assert/require in tests.
