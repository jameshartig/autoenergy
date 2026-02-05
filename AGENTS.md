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
- Log with slog and use slog.String, slog.Int, slog.Float64, slog.Bool, slog.Any, etc for key value pairs
- Log field names with headless camelcase
- Struct fields should have json Go tags with their name headless camelcase
- Tests should live in a file with the same name as the file they test, with suffix _test
- Tests should be named Test<FunctionName> and all related tests should live as subtests in that function with t.Run()
