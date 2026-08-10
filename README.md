<div align="center">
  <img src="frontend/public/logo.png" alt="CPA-Helper Logo" width="104" height="104" />
  <h1>CPA-Helper</h1>
  <p><strong>A local self-hosted multi-user admin panel for CLIProxyAPI</strong></p>
  <p>Usage analytics · Request tracing · User balances · API key management · Model pricing maintenance · Codex auth file inspection</p>
  <p>
    <strong>English</strong>
    <span> · </span>
    <a href="README.zh-CN.md">中文</a>
  </p>
  <p>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go 1.25+" /></a>
    <a href="https://vuejs.org/"><img src="https://img.shields.io/badge/Vue-3.5+-42b883?logo=vuedotjs&logoColor=white" alt="Vue 3.5+" /></a>
    <a href="https://vitejs.dev/"><img src="https://img.shields.io/badge/Vite-5.4+-646cff?logo=vite&logoColor=white" alt="Vite 5.4+" /></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="MIT License" /></a>
    <a href="https://linux.do"><img src="https://shorturl.at/ggSqS" alt="LINUX DO" /></a>
  </p>
</div>

---

CPA-Helper is a local self-hosted multi-user administration panel for CLIProxyAPI / CPA users. It centralizes usage analytics, request records, user accounts, API keys, model pricing, available models and Codex auth file inspection.

It separates API keys and usage data by user: each user can create and manage their own keys and inspect their own requests, tokens and cost statistics, while administrators can create or disable regular accounts and review global plus per-user usage. It is built with Go, SQLite, Vue 3 and Vite, with runtime data stored in the root-level `data/` directory by default.

For clarity, model requests initiated by an Agent are still sent directly from that Agent to CPA. CPA-Helper does not proxy or relay those requests; it only calls CPA management-style interfaces such as the usage queue, API key creation and deletion, and credential management for usage views, key management and credential maintenance.

## Table of Contents

- [Features](#features)
- [Screenshots](#screenshots)
- [Tech Stack](#tech-stack)
- [Project Structure](#project-structure)
- [Requirements](#requirements)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Development and Checks](#development-and-checks)
- [Contributing](#contributing)
- [Acknowledgements](#acknowledgements)
- [License](#license)

## Features

- **Usage analytics and cost estimation**: Track requests, tokens, success rate, latency, model distribution and estimated cost from global, per-user and current-account views.
- **Request record tracing**: Admins can filter global request events by time, user, API key description, provider, model, endpoint and failure state; regular users inspect only their own account records.
- **User and permission management**: Provide administrator and regular-user views; admins can create or disable regular accounts and manage nicknames, login accounts, passwords and roles.
- **User balances and automatic key pause**: Users are unlimited by default; admins can configure monthly balance and lifetime balance, usage is priced in USD with current model prices, monthly balance is consumed first, and exhausted users only have their CPA API keys paused.
- **API key lifecycle management**: Each user can generate or supply a custom API key, then independently edit, copy and delete their own keys. Administrators can also create keys for regular users. Newly created keys are synchronized to CPA, with usage counted per user and per-key request guidance plus live request testing.
- **Model pricing maintenance**: Maintain token-model input, output and cache prices in USD per million tokens; models whose name contains `image` are charged by a fixed USD price per successful request, with CPA model comparison for quickly filling LiteLLM / manual prices.
- **Card shop index**: Admins can browse real-time public card-shop and product snapshots, search by product title, use popular tags, sort results and favorite shops for faster lookup. This is only a public information index and does not participate in transactions.
- **Available model aggregation**: Query available models through the current account's bound CPA API keys and enrich them with local pricing data.
- **CLIProxyAPI / CPAMC integration**: Configure the service URL, management key, usage queue and local collector options to persist remote usage events into SQLite.
- **Codex auth file inspection**: Support Cron scheduling, quota thresholds, check-only mode, conditional scanning, concurrent workers, priority rules, account enable/disable and deletion.
- **Local-first data storage**: Use SQLite and the `data/` directory by default, with `CPA_HELPER_DATA_DIR` available for overriding the runtime data path.
- **Modern admin interface**: Built with Vue 3, Naive UI, ECharts and lucide icons, with light, dark and system theme modes.

## Screenshots

### Admin

**Usage dashboard**

Admins can inspect global request volume, tokens, cost, trends and distributions by time range, user, model and endpoint.

![Usage dashboard](pictures/历史用量.png)

**Request records**

Admins can filter global request events, while regular users can inspect records scoped to their own account, with drawer-based detail views.

![Request records](pictures/请求明细.png)

**User management**

Admins can create or disable regular accounts, manage nicknames, roles and enabled status, and review per-user daily usage, monthly balance and lifetime balance.

![User management](pictures/用户管理.png)

**Model pricing**

Compare CPA's currently available models with the local price catalog, quickly price missing models, and recalculate historical request costs using the latest configured prices.

![Model pricing](pictures/模型价格.png)

**Card shop index**

Browse public card-shop and product snapshots, search by product title, use popular tags, sort results and favorite shops for follow-up lookup.

![Card shop index](pictures/卡网收录.png)

**System settings**

Configure the CLIProxyAPI / CPAMC endpoint, model request URL, management key, local collector and polling options.

![System settings](pictures/系统设置.png)

### Account Inspection

**Inspection settings**

Configure Codex auth file inspection with Cron schedules, quota thresholds, conditional scanning, timeouts, retries, worker count and priority rules.

![Inspection settings](pictures/巡检设置.png)

**Account status**

Review auth file health, quota windows, account types, priorities and the latest inspection actions.

![Account status](pictures/账号状态.png)

### Account Views

**My usage**

Each user can review their own requests, tokens, costs, trends, model usage and current balance state.

![My usage](pictures/我的账户.png)

**My records**

Each user can inspect request events and details scoped to their own account, separated from other users.

![My records](pictures/我的明细.png)

**API keys**

Each account can create and manage API keys using either automatic generation or a custom value. Administrators can also create keys for regular users. The page includes daily request, token, cost and balance summaries, plus request examples and live request testing for each key.

![API keys](pictures/API密钥.png)

**Available models**

Query available models through bound CPA API keys and display availability plus local pricing information.

![Available models](pictures/可用模型.png)

**Account settings**

View the current signed-in account and update the password.

![Account settings](pictures/账户设置.png)

## Tech Stack

- **Backend**: Go standard-library HTTP server, SQLite, robfig/cron and modernc.org/sqlite.
- **Frontend**: Vue 3, Vite, TypeScript, Vue Router, Naive UI, ECharts and lucide-vue-next.
- **Runtime data**: Stored in root-level `data/` by default; the SQLite database is `data/db/cpa_helper.sqlite3`.
- **API shape**: The backend exposes `/api/*`; the Vite development server proxies API calls to `http://127.0.0.1:18317`.

## Project Structure

```text
CPA-Helper/
├── backend/                 # Go backend project
│   ├── cmd/cpa-helper/      # Application entrypoint
│   ├── internal/app/        # App composition, routes, business logic and database access
│   ├── internal/httpserver/ # HTTP server lifecycle and graceful shutdown
│   ├── internal/platform/   # External-system infrastructure adapters
│   ├── internal/security/   # Password, session and API-key security helpers
│   ├── migrations/          # Embedded goose SQLite migrations
│   ├── go.mod
│   └── go.sum
├── frontend/                # Vue + Vite frontend project
│   ├── src/                 # App, feature modules, shared utilities and styles
│   ├── public/              # Static assets
│   └── package.json         # Frontend dependencies and scripts
├── pictures/                # README screenshots
├── docs/                    # Reference documentation
├── data/                    # Runtime data, ignored by Git by default
├── VERSION                  # Shared app version for Docker tags and frontend display
├── README.md
├── README.zh-CN.md
└── LICENSE
```

## Requirements

- Go 1.25 or newer.
- Node.js 20 or newer.
- npm.
- An accessible CLIProxyAPI / CPA service. The default URL is `http://127.0.0.1:8317`.

## Quick Start

### 1. Docker Compose deployment (recommended)

Create `docker-compose.yml` in the deployment directory:

```yaml
services:
  cpa-helper:
    image: walkingd/cpa-helper:latest
    container_name: cpa-helper
    restart: always
    # 如需改为bridge,需将容器内部端口 18317 映射至主机
    # 程序默认访问地址为 `http://127.0.0.1:18317`
    network_mode: host
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - ./data:/app/data
```

Then pull the image and start the service:

```powershell
docker compose pull
docker compose up -d
```

Open:

```text
http://127.0.0.1:18317
```

On first visit, the application guides you through creating the first administrator account.

### 2. Clone the repository

```powershell
git clone <your-repo-url>
cd CPA-Helper
```

### 3. Start the backend

Run all backend commands from `backend/`:

```powershell
cd backend
go mod download
go run ./cmd/cpa-helper
```

The binary also exposes explicit operational subcommands:

```powershell
go run ./cmd/cpa-helper migrate  # run database migrations and exit
go run ./cmd/cpa-helper serve    # start only after read-only startup checks pass
go run ./cmd/cpa-helper doctor   # run read-only startup checks and exit
```

Running without a subcommand is the user-facing startup path: it runs migrations,
checks readiness, and then starts the service.

For a local binary build, write the output to the ignored `backend/bin/` directory:

```powershell
go build -o bin/cpa-helper.exe ./cmd/cpa-helper
```

Health check:

```powershell
curl http://127.0.0.1:18317/api/health
```

Expected response:

```json
{"status":"ok"}
```

### 4. Start the frontend development server

Open a new terminal and run from `frontend/`:

```powershell
cd frontend
npm install
npm run dev
```

If a normal backend is already using `18317`, start the test backend on another
port and point the Vite proxy to it:

```powershell
$env:CPA_HELPER_PROXY_TARGET="http://127.0.0.1:18318"
npm run dev -- --host 127.0.0.1 --port 5174 --strictPort
```

Open:

```text
http://127.0.0.1:5173
```

On first visit, the application guides you through creating the first administrator account.

### 5. Single-service preview or deployment

To let the Go backend serve the frontend static files, build the frontend first:

```powershell
cd frontend
npm install
npm run build
```

Then start the backend:

```powershell
cd ../backend
go run ./cmd/cpa-helper
```

Open:

```text
http://127.0.0.1:18317
```

When `frontend/dist` exists, the backend serves the built single-page application.

## Configuration

### CLIProxyAPI / CPAMC

Use the System Settings page to configure:

- **CLIProxyAPI / CPAMC URL**: defaults to `http://127.0.0.1:8317`.
- **Model request URL**: used only by the API Keys page request-test dialog to generate the Base URL, endpoint URL and examples; it does not affect collection, the management key or backend synchronization.
- **Management key**: used to access the CLIProxyAPI Management API.
- **Enable local collector**: when enabled, CPA-Helper reads events from the usage queue and writes them to the local database.
- **Batch size, polling interval and retry interval**: control local collector throughput and failure retry behavior.

### User Balances

- Existing users and newly created users are unlimited by default.
- Admins can disable the unlimited toggle in User Management and then set monthly balance plus lifetime balance; both numeric fields default to `0` and cannot be left blank.
- Initial balance setup does not retroactively charge historical usage; only newly collected usage after setup participates in balance deduction.
- Balance consumption uses the USD amount estimated from current model prices. The fixed order is monthly balance first, then lifetime balance for any overflow.
- When both balances are unavailable or exhausted, CPA-Helper removes that user's locally bound API keys from CPA, but it does not disable the login account. The user can still sign in and view the reason.
- Monthly balance resets by the `Asia/Shanghai` calendar month. Entering a new month, adding balance or switching the user back to unlimited automatically restores keys that were paused only because of balance exhaustion.
- Unpriced usage does not consume balance, but CPA-Helper records it as an unpriced balance event and surfaces the warning to admins and the affected user.

### Model Pricing and Request Testing

- The Model Pricing page shows CPA's currently available models alongside the local price catalog so missing prices are easy to find.
- Token models store input, output, cache read and cache write prices as USD per million tokens. Models whose name contains `image` use a fixed USD price per successful request; if that per-request price is missing, the model is treated as unpriced.
- Usage-history pages recalculate displayed costs with the current price catalog; already written balance charge records keep the amount calculated at the time and are not retroactively repriced.
- LiteLLM sync can quickly fill the local price catalog while preserving manual prices. If GitHub is not reachable, configure the LiteLLM proxy from the Model Pricing page.
- Each API key row includes Request Test, which shows the Base URL, endpoint URL, auth header and curl example, and can send one real test request.
- Request testing supports Chat Completions, Responses and Claude Messages formats. The examples and test payload switch with the selected format.

### Data Directory

Default runtime data directory:

```text
data/
```

Default SQLite database:

```text
data/db/cpa_helper.sqlite3
```

Override the runtime data directory with:

```powershell
$env:CPA_HELPER_DATA_DIR="<your-data-dir>"
```

Then start the backend service.

### Account Inspection

The Inspection Settings page manages Codex auth files:

- Cron expressions define the automatic inspection schedule.
- Quota thresholds decide when account priority should be degraded or restored.
- Check-only mode records planned actions without disabling accounts or changing priorities.
- Conditional scanning compares locally recorded accounts with the current CPA account list: accounts missing locally are queried once for quota and recorded, while accounts no longer present in CPA are removed locally.
- Priority rules define default scheduling weights by account type.
- The Account Status page shows health, quota, latest inspection, enabled state and manual priority.

## Development and Checks

### Isolated Local Testing

If a normal service is already running locally, do not reuse its ports or real
data directory for tests. Start the backend with a temporary port and data
directory:

```powershell
cd backend
$env:CPA_HELPER_ADDR=":18318"
$env:CPA_HELPER_DATA_DIR="$env:TEMP\cpa-helper-test-data"
go run ./cmd/cpa-helper
```

Run the frontend test server on a separate port and point the Vite proxy at the
test backend:

```powershell
cd frontend
$env:CPA_HELPER_PROXY_TARGET="http://127.0.0.1:18318"
npm run dev -- --host 127.0.0.1 --port 5174 --strictPort
```

Automated validation should not use a real CPA URL or real management key by
default. For account inspection, enable/disable, priority changes, or deletion
flows, use a fake CLIProxyAPI / CPAMC test double and prefer check-only mode;
connect to real CPA only after the risk is explicitly accepted.

### Version Management

The project version is stored in the root `VERSION` file, for example `0.1.0`.
GitHub Actions reads it to push `walkingd/cpa-helper:v0.1.0` and
`walkingd/cpa-helper:latest`; the frontend build reads the same file and
displays it as `v0.1.0`.

Backend:

```powershell
cd backend
go fmt ./...
go test ./...
```

Frontend:

```powershell
cd frontend
npm run lint
npm run build
```

Database schema:

```powershell
cd backend
go test ./...
```

The Go backend executable provides `migrate`, `serve`, and `doctor` subcommands. The default no-subcommand startup path runs embedded goose SQLite migrations before serving; `serve` performs read-only startup checks and fails if the database is missing or behind. Migration files live in `backend/migrations/`, and Alembic is no longer required.
For Docker upgrades, keep mounting the existing `data/db/cpa_helper.sqlite3`; migration logic is packaged into the Go binary and does not read the old source tree.

## Contributing

Issues and pull requests are welcome. Before submitting changes, please check:

- Backend passes `go fmt ./...` and `go test ./...`.
- Frontend passes `npm run lint` and `npm run build`.
- Relational schema changes add or update goose migrations under `backend/migrations/`.
- Local runtime data, virtual environments, build outputs and secrets are not committed.

## Acknowledgements

Thanks to the [Linux.do](https://linux.do/) site and community for support and inspiration around the project.

## License

This project is open-sourced under the [MIT License](LICENSE).
