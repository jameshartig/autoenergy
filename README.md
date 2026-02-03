# AutoEnergy

AutoEnergy is an intelligent home energy management system designed to optimize the usage of Energy Storage Systems (ESS) like FranklinWH based on real-time electricity pricing (e.g., ComEd Hourly Pricing). It automates the charging and discharging of batteries to maximize savings and efficiency.

## Architecture

The project is structured as follows:

- **`cmd/autoenergy`**: The main entry point and orchestrator.
- **`pkg`**: Core backend logic.
    - **`controller`**: Decision-making logic for ESS control.
    - **`ess`**: Interfaces and implementations for ESS (currently supports FranklinWH).
    - **`server`**: HTTP API server for the web dashboard and triggered updates.
    - **`storage`**: Persistence layer (currently supports Google Cloud Firestore).
    - **`utility`**: Electricity pricing fetchers (ComEd & PJM).
- **`web`**: A React + TypeScript + Vite single-page application for the frontend dashboard.
- **`tf`**: Terraform configuration for provisioning infrastructure on Google Cloud.

## Getting Started

### Prerequisites

- Go 1.25+
- Node.js & npm (for web development)
- Google Cloud Project with Firestore enabled
- FranklinWH Account Credentials

### Installation

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/jameshartig/autoenergy.git
    cd autoenergy
    ```

2.  **Build the Web App (Optional if running dev server):**

    ```bash
    cd web
    npm install
    npm run build
    cd ..
    ```

3.  **Build the Go Binary:**

    ```bash
    go build ./cmd/autoenergy
    ```

### Usage

Run the binary with the necessary flags.

```bash
./autoenergy --help
```

### Configuration Flags

The application uses command-line flags for configuration.

#### General / Server
- `--http-listen`: HTTP server listen address (default `:8080`).
- `--dev-proxy`: URL to proxy requests to (e.g., `http://localhost:5173` for local Vite dev server).
- `--update-specific-email`: Email requirement for authenticating calls to `/api/update`.
- `--admin-emails`: Comma-delimited list of email addresses allowed to manage settings.
- `--oidc-audience`: Expected audience for OIDC token validation.

#### Utility (ComEd & PJM)
- `--utility-provider`: Provider to use (default `comed`).
- `--comed-api-url`: URL for the ComEd Hourly Pricing API.
- `--pjm-api-url`: URL for the PJM API (Day-ahead pricing).
- `--pjm-api-key`: API Key for PJM Data Miner 2 (optional, enabled day-ahead lookups).

#### ESS (FranklinWH)
- `--ess-provider`: Provider to use (default `franklin`).
- `--franklin-username`: FranklinWH Email/Username.
- `--franklin-password`: FranklinWH Password.
- `--franklin-md5-password`: MD5 hashed password (alternative to plaintext).
- `--franklin-gateway-id`: FranklinWH Gateway ID (optional, auto-detected if single gateway).
- `--franklin-token`: FranklinWH Access Token (optional override).

#### Storage (Firestore)
- `--storage-provider`: Provider to use (default `firestore`).
- `--firestore-project-id`: Google Cloud Project ID.
- `--firestore-database`: Firestore Database ID (default `(default)`).

## Development

### Running Locally

To run the full stack locally:

1.  **Start Firestore Emulator:**

    ```bash
    gcloud emulators firestore start --host-port=127.0.0.1:8087
    ```

2.  **Start Web Dev Server:**

    ```bash
    cd web
    npm run dev
    ```

3.  **Run Go Backend:**

    ```bash
    export FIRESTORE_EMULATOR_HOST=127.0.0.1:8087
    go run ./cmd/autoenergy \
      --dev-proxy=http://localhost:5173 \
      --franklin-username=YOUR_EMAIL \
      --franklin-password=YOUR_PASSWORD
    ```

### Running Tests

To run all Go tests:

```bash
go test ./...
```

Firestore integration tests will automatically use the emulator if `FIRESTORE_EMULATOR_HOST` is set or default to `127.0.0.1:8087`.

## Deployment

The `tf` directory contains Terraform code to deploy the application to Google Cloud Platform. It sets up:
- **Cloud Run**: Hosts the Go server (which serves the embedded React app).
- **Cloud Scheduler**: Triggers the `/api/update` endpoint periodically.
- **Firestore**: Database for settings, history, and actions.
- **Secret Manager**: Securely stores credentials.

## API Endpoints

- `POST /api/update`: Triggers a logic execution cycle (Fetch Price -> Decide Action -> Control ESS).
- `GET /api/history/prices`: Retrieve historical pricing data.
- `GET /api/history/actions`: Retrieve historical actions taken by the controller.
- `GET /api/settings`: Retrieve current system settings.
- `POST /api/settings`: Update system settings.
- `GET /api/auth/status`: Check current authentication status.
- `GET /healthz`: Health check endpoint.

## Security Note

- **FranklinWH**: The integration relies on a reverse-engineered API. Use responsibly.
- **Credentials**: In production, pass sensitive credentials (passwords, keys) via environment variables or Secret Manager, not hardcoded flags.
