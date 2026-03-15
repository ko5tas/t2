# T2 Web Dashboard — Increment 1 Design Spec

## Context

T2 is a personal trading dashboard for Trading212 stocks and shares. The user holds positions in a Trading212 Invest account and wants a web-based dashboard to view their portfolio at a glance. All values must be displayed in GBP (£). The application will eventually include a TUI, systemd service, and deb packaging, but this first increment focuses on the web dashboard only.

## Goals

- Display all open positions in a dark, compact table
- Show: row number, ticker + stock name, market value in £, exchange name
- Auto-refresh every 15 minutes with a manual refresh button
- Use Trading212's own data for all calculations (no third-party FX)

## Architecture

```
┌─────────────────────────────────────────────┐
│  t2 binary (single Go binary)               │
│                                             │
│  ┌──────────────┐  ┌─────────────────────┐  │
│  │ Trading212    │  │ HTTP Server         │  │
│  │ API Client    │  │ Go templates + HTMX │  │
│  └──────┬───────┘  └─────────┬───────────┘  │
│         │                    │              │
│  ┌──────┴───────┐  ┌────────┴──────────┐   │
│  │ Portfolio     │  │ Config (YAML)     │   │
│  │ Service       │  │                   │   │
│  └──────────────┘  └───────────────────┘   │
└─────────────────────────────────────────────┘
```

All templates and static assets are embedded in the binary via `embed.FS`.

## Trading212 API

**Base URL**: `https://live.trading212.com/api/v0`
**Auth**: HTTP Basic Auth (`-u API_KEY:API_SECRET`)

### Endpoints Used

| Endpoint | Purpose | Rate Limit |
|---|---|---|
| `GET /equity/portfolio` | All open positions | 1 req / 5s |
| `GET /equity/metadata/instruments` | Instrument name, currency, type, workingScheduleId | 1 req / 50s |
| `GET /equity/metadata/exchanges` | Exchange names, linked to instruments via workingScheduleId | Unknown (treat as 1 req / 50s) |
| `GET /equity/account/cash` | Account totals for cross-check | 1 req / 2s |

### Data Flow

On startup:
1. Fetch `/equity/metadata/exchanges` → build `map[workingScheduleId]exchangeName`
2. Fetch `/equity/metadata/instruments` → build `map[ticker]Instrument` (includes name, currencyCode, workingScheduleId)

On each refresh (every 15 minutes):
1. Fetch `/equity/portfolio` → list of positions
2. For each position, look up instrument metadata (name, exchange) from cached maps
3. Calculate £ market value per position (see below)
4. Fetch `/equity/account/cash` to get account totals (logged as a cross-check against sum of position values; not displayed in the UI)

Metadata (exchanges + instruments) is fetched on startup and cached in memory. A background goroutine refreshes the metadata cache once every 24 hours to pick up any new instruments or exchange changes.

### Response Structures

**Position** (`GET /equity/portfolio`):
```json
{
  "ticker": "AAPL_US_EQ",
  "quantity": 50.0,
  "averagePrice": 145.50,
  "currentPrice": 152.30,
  "ppl": 340.00,
  "fxPpl": 0,
  "initialFillDate": "2025-09-15T14:20:00.000Z",
  "frontend": "API",
  "maxBuy": 1000.0,
  "maxSell": 50.0,
  "pieQuantity": 0
}
```

**Instrument** (`GET /equity/metadata/instruments`):
```json
{
  "ticker": "AAPL_US_EQ",
  "name": "Apple Inc.",
  "shortName": "Apple",
  "type": "STOCK",
  "currencyCode": "USD",
  "isin": "US0378331005",
  "maxOpenQuantity": 10000,
  "addedOn": "2020-01-01T00:00:00.000Z",
  "workingScheduleId": 100
}
```

**Exchange** (`GET /equity/metadata/exchanges`):
```json
{
  "id": 53,
  "name": "NASDAQ",
  "workingSchedules": [
    { "id": 110, "timeEvents": [...] },
    { "id": 71, "timeEvents": [...] }
  ]
}
```

## Market Value Calculation (£)

The API does not provide per-position market value in account currency. We derive it from Trading212's own data:

```
£ market value = (averagePrice × quantity) + ppl + fxPpl
```

- `averagePrice × quantity` = cost basis in instrument currency (e.g. USD)
- `ppl` = profit/loss in account currency (GBP)
- `fxPpl` = FX impact component in account currency (GBP)

**Important caveat**: This formula adds a value in instrument currency to values in GBP, which is mathematically imprecise. However, it is expected to produce a reasonable approximation because Trading212 internally tracks the GBP-equivalent cost basis when computing `ppl`. The formula effectively reconstructs: `GBP cost basis + GBP gain/loss = GBP market value`.

**Validation strategy**: On first run with real data, we will compare the sum of all per-position market values against the `invested + ppl` total from `/equity/account/cash`. If the values diverge significantly, we will revisit this formula and consider alternative approaches (e.g. using a free FX API as a fallback).

**Note on GBX**: Some LSE instruments use GBX (pence). The currencyCode field from instruments metadata identifies these. No special handling needed since T212's ppl is already in account currency.

## Dashboard Columns

| # | Column | Source | Display |
|---|--------|--------|---------|
| 1 | # | Row index | Sequential number |
| 2 | NAME | `shortName` from instruments + `ticker` prefix | **AAPL** Apple Inc (ticker in blue, name in grey) |
| 3 | MARKET VALUE | Calculated (see above) | £4,521.30 (green, formatted with commas) |
| 4 | EXCHANGE | Joined via workingScheduleId | NASDAQ, London Stock Exchange, etc. |

Total row at bottom: sum of all market values.

Positions are displayed in the order returned by the Trading212 API (no explicit sorting in increment 1).

## Exchange Resolution

Instruments link to exchanges through `workingScheduleId`:

1. Exchanges endpoint returns each exchange with an array of `workingSchedules`, each having an `id`
2. We build: `map[scheduleId] → exchangeName`
3. Each instrument has a `workingScheduleId` field
4. Lookup: `instrument.workingScheduleId → exchangeName`

If a schedule ID appears under multiple exchanges (unlikely but possible), first match wins. In practice, the API data from Trading212 shows no overlapping schedule IDs across exchanges.

Known exchanges from the API:
- London Stock Exchange (14 schedule IDs)
- London Stock Exchange AIM (4 schedule IDs)
- NYSE (3 schedule IDs)
- NASDAQ (3 schedule IDs)
- Euronext Paris, Amsterdam, Brussels, Lisbon
- Deutsche Börse Xetra, Gettex
- Borsa Italiana, Bolsa de Madrid
- SIX Swiss Exchange, Wiener Börse
- Toronto Stock Exchange
- OTC Markets
- London Stock Exchange NON-ISA

## Project Layout

```
t2/
├── cmd/t2/main.go                 # Entry point
├── internal/
│   ├── config/config.go           # YAML config loading
│   ├── trading212/
│   │   ├── client.go              # HTTP client, auth
│   │   └── types.go               # API response DTOs
│   ├── portfolio/
│   │   ├── types.go               # Domain types (Position, Summary)
│   │   └── service.go             # Merges API data, calculates £ values
│   └── web/
│       ├── handler.go             # HTTP handlers
│       └── templates.go           # embed.FS, template rendering
├── web/
│   ├── static/
│   │   └── htmx.min.js           # HTMX library (embedded)
│   └── templates/
│       ├── index.html             # Full page (dark theme, HTMX)
│       └── positions.html         # Partial (table rows for HTMX swap)
├── go.mod
├── go.sum
└── config.example.yaml
```

## Configuration

File: `config.yaml` (searched in order: `$T2_CONFIG`, `~/.config/t2/config.yaml`, `/etc/t2/config.yaml`)

```yaml
api_key: "your-api-key"
api_secret: "your-api-secret"
refresh_interval: "15m"
listen: ":8080"
base_url: "https://live.trading212.com/api/v0"
```

Go type:
```go
type Config struct {
    APIKey          string        `yaml:"api_key"`
    APISecret       string        `yaml:"api_secret"`
    RefreshInterval time.Duration `yaml:"refresh_interval"`
    Listen          string        `yaml:"listen"`
    BaseURL         string        `yaml:"base_url"`
}
```

Defaults: `refresh_interval: 15m`, `listen: :8080`, `base_url: https://live.trading212.com/api/v0`

Validation: `api_key` and `api_secret` must be non-empty. Fatal on invalid config.

## Web UI

### Routes

| Method | Path | Handler | Description |
|---|---|---|---|
| GET | `/` | `handleIndex` | Full page render |
| GET | `/positions` | `handlePositions` | HTMX partial: table body only |
| GET | `/static/*` | `http.FileServer` | Embedded static files (htmx.min.js) |

### HTMX Refresh

The `#positions` div uses `hx-get="/positions" hx-trigger="load, every 900s"` (900s = 15 minutes).

A manual refresh button: `<button hx-get="/positions" hx-target="#positions" hx-swap="innerHTML">Refresh</button>`

HTMX is embedded in the binary (downloaded at build time and included via `embed.FS`) to keep the dashboard fully self-contained with no external runtime dependencies.

### Visual Design

- Dark theme (background: #1a1a2e, text: #e0e0e0)
- Ticker in blue (#90caf9)
- Stock name in grey (#888)
- Market value in green (#81c784), right-aligned
- Table rows with subtle borders (#222)
- Total row with blue highlight (#64b5f6)
- Inline `<style>` — no external CSS files
- Title: "T2 - Trading212 Dashboard"

## Error Handling

| Scenario | Behaviour |
|---|---|
| Invalid/missing config | `log.Fatal` on startup with clear message |
| API auth failed (401) | Error banner in positions partial |
| API unreachable / timeout | Error banner; HTTP client uses 10s timeout |
| Rate limited (429) | Error banner with message to wait |
| Empty portfolio | Table with "No open positions" message row |
| Instruments/exchanges fetch fails on startup | `log.Fatal` — metadata is required |
| Instrument not found for a position | Show raw ticker, "Unknown" for exchange |

The web handler always returns valid HTML (including error states) so HTMX swaps work cleanly. Never return HTTP 500 to the browser.

## Dependencies

```
go.mod:
  module github.com/ko5tas/t2
  go 1.23

  require gopkg.in/yaml.v3 v3.0.1
```

Only external dependency is `yaml.v3`. Everything else is stdlib:
- `net/http` — routing and serving
- `html/template` — templates
- `encoding/json` — API response parsing
- `embed` — embedding templates
- `os`, `time`, `fmt`, `log`, `path/filepath` — utilities

## Verification

1. **Unit test**: Mock Trading212 API responses, verify market value calculation
2. **Integration test**: Use demo API credentials to fetch real data
3. **Manual test**: Run binary locally, open browser, verify table renders correctly
4. **Cross-check**: Compare sum of displayed market values against `/account/cash` total
5. **Error test**: Start with invalid API key, verify error banner appears
