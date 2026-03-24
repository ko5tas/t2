# t2

A web dashboard for viewing your Trading212 portfolio positions at a glance.

## Features

- Dark compact table with ticker, stock name, ISIN, market value (GBP), exchange, and more
- Accurate GBP market values using Trading212's own currency conversion
- Recovery tracking: recovered amount (sells + dividends), dividend yield %, performance %
- Financial fundamentals: P/E, EPS, EPS Growth, Market Cap, Revenue, Profit Margin
  - Fetched from Yahoo Finance (EU stocks, ISIN-based ticker lookup) with optional Finnhub for US stocks
  - Daily refresh with disk cache
  - Hover tooltips on column headers explaining each metric
  - ETFs detected via Trading212 instrument type → show "N/A" instead of dashes
  - P/E fallback: calculated from price/EPS when API doesn't return P/E directly
- Native currency prices with GBP conversion for foreign stocks
- Profitable position highlighting (green blink + favicon alert)
- Near-breakeven blinking (0–4% performance in orange)
- Buy history tooltips: hover over stock name to see purchase dates and quantities
- Historical FX rate conversion for foreign-currency orders
- Incremental history caching: orders and dividends cached to disk
  - Local: `~/.cache/t2/` — systemd fallback: `/var/cache/t2/` (owner-only permissions)
  - Cold start: fetches all pages from API, saves to disk
  - Warm start: fetches only new data by finding overlap with cache (typically 1 API call)
- Sortable columns (click headers to toggle ascending/descending)
- Default sort by market value descending
- Auto-refresh every 15 minutes + manual refresh button per row
- Exchange abbreviations with hover tooltips (LSE, NASDAQ, NYSE, XETR, EPA, etc.)
- Click-to-pin row highlighting that persists across auto-refreshes
- Double-click to select individual text spans (ticker, name, ISIN, raw ticker)
- Zebra striping and hover highlighting for row readability
- History tab for closed/sold positions with performance tracking and totals
- Exchange resolution via Trading212 metadata API
- Single binary with embedded HTMX (no CDN dependency)

## Quick Start

1. Copy and edit the config file:
   ```bash
   cp config.example.yaml config.yaml
   # Edit config.yaml with your Trading212 API credentials
   ```

2. Build and run:
   ```bash
   go build -o t2 ./cmd/t2
   ./t2
   ```

3. Open http://localhost:8080

## Installation (Debian/DietPi)

### Add the apt repository

```bash
# Import GPG key
curl -fsSL https://ko5tas.github.io/t2/t2-repo.gpg | sudo gpg --dearmor -o /usr/share/keyrings/t2-repo.gpg

# Add repository
echo "deb [signed-by=/usr/share/keyrings/t2-repo.gpg] https://ko5tas.github.io/t2 stable main" | sudo tee /etc/apt/sources.list.d/t2.list

# Install
sudo apt update && sudo apt install t2
```

### Configure

Edit `/etc/t2/config.yaml` with your Trading212 API credentials, then restart:

```bash
sudo systemctl restart t2
```

### Upgrade

```bash
sudo apt update && sudo apt upgrade
```

The service restarts automatically after upgrade.

### Service management

```bash
sudo systemctl status t2    # Check status
sudo systemctl restart t2   # Restart
sudo systemctl stop t2      # Stop
sudo journalctl -u t2 -f    # View logs
```

## Configuration

The config file is searched in order:
1. `$T2_CONFIG` environment variable
2. `~/.config/t2/config.yaml`
3. `/etc/t2/config.yaml`

| Setting | Default | Description |
|---------|---------|-------------|
| `api_key` | (required) | Trading212 API key |
| `api_secret` | (required) | Trading212 API secret |
| `base_url` | `https://live.trading212.com/api/v0` | API base URL |
| `refresh_interval` | `15m` | How often to refresh positions |
| `listen` | `:8080` | HTTP server listen address |
| `finnhub_api_key` | (optional) | [Finnhub](https://finnhub.io) API key for US stock fundamentals (free tier) |

## Architecture

### Data Flow

```mermaid
graph TB
    subgraph External APIs
        T212[Trading212 API]
        YF[Yahoo Finance]
        FH[Finnhub]
    end

    subgraph "Background Goroutines"
        SR["SummaryRefresh<br/>(every 15m)"]
        RR["ReturnsRefresh<br/>(every 15m)"]
        FR["FundamentalsRefresh<br/>(every 24h)"]
    end

    subgraph "Disk Cache (/var/cache/t2/)"
        DC_O["orders.json"]
        DC_D["dividends.json"]
        DC_F["fundamentals.json"]
    end

    subgraph "In-Memory Cache"
        PC[Positions + Prices]
        RC[Order History + Returns]
        FC["Fundamentals<br/>(P/E, EPS, etc.)"]
    end

    subgraph "Go HTTP Server :8080"
        H[Handler]
    end

    subgraph Browser
        HX["HTMX auto-poll<br/>(every 30s)"]
    end

    SR -->|GetPositions| T212
    SR -->|builds| PC
    RR -->|"incremental fetch<br/>(overlap detection)"| T212
    RR <-->|"load/save"| DC_O
    RR <-->|"load/save"| DC_D
    RR -->|builds| RC
    FR -->|US stocks| FH
    FR -->|"EU stocks<br/>(ISIN→ticker)"| YF
    FR <-->|"load/save"| DC_F
    FR -->|builds| FC

    PC --> H
    RC --> H
    FC --> H

    HX -->|"GET /positions<br/>GET /history"| H
    H -->|HTML| HX
```

### Refresh Timeline

```mermaid
sequenceDiagram
    participant Disk as Disk Cache
    participant T212 as Trading212 API
    participant FH as Finnhub / Yahoo
    participant BG as Background Goroutines
    participant Cache as In-Memory Cache
    participant HTMX as Browser (HTMX)

    Note over BG: Startup
    BG->>Cache: refreshSummary() — initial render with dashes
    BG->>Disk: load orders.json + dividends.json
    Disk-->>BG: cached history (if exists)
    BG->>T212: fetch page 1 (50 most recent)
    T212-->>BG: page 1 items
    Note over BG: overlap found?<br/>Yes → merge new items, stop<br/>No → fetch next page
    BG->>Disk: save merged history
    BG->>Cache: rebuild summary with returns

    BG->>Disk: load fundamentals.json
    Disk-->>BG: cached fundamentals
    BG->>FH: fetchFundamentals (after 15s)
    FH-->>BG: P/E, EPS, etc.
    BG->>Disk: save fundamentals.json
    BG->>Cache: rebuild summary with fundamentals

    loop Every 30 seconds
        HTMX->>Cache: GET /positions (read from cache)
        Cache-->>HTMX: HTML response
    end

    loop Every 15 minutes
        BG->>T212: GetPositions (full fetch)
        T212-->>BG: current positions
        BG->>Disk: load cached orders/dividends
        BG->>T212: fetch page 1 (incremental)
        Note over BG: typically 0 new items
        BG->>Cache: rebuild summary
    end

    loop Every 24 hours
        BG->>FH: fetch fundamentals
        FH-->>BG: P/E, EPS, etc.
        BG->>Disk: save fundamentals.json
        BG->>Cache: rebuild summary
    end
```

## License

See [LICENSE](LICENSE).
