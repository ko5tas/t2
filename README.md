# t2

A web dashboard for viewing your Trading212 portfolio positions at a glance.

## Features

- Dark compact table showing ticker, stock name, T212 ticker, market value (GBP), and exchange
- Accurate GBP market values using Trading212's own currency conversion
- Sortable columns (click headers to toggle ascending/descending)
- Default sort by market value descending
- Auto-refresh every 15 minutes + manual refresh button
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

## License

See [LICENSE](LICENSE).
