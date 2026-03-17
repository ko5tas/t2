# t2

A web dashboard for viewing your Trading212 portfolio positions at a glance.

## Features

- Dark compact table with ticker, stock name, ISIN, market value (GBP), exchange, and more
- Accurate GBP market values using Trading212's own currency conversion
- Recovery tracking: recovered amount (sells + dividends), dividend yield %, performance %
- Financial fundamentals: P/E, EPS, EPS Growth, Market Cap, Revenue, Profit Margin
  - Fetched from Yahoo Finance (all stocks) with optional Finnhub support for US stocks
  - Daily refresh with disk cache at `~/.cache/t2/fundamentals.json`
  - Hover tooltips on column headers explaining each metric
- Native currency prices with GBP conversion for foreign stocks
- Profitable position highlighting (green blink + favicon alert)
- Historical FX rate conversion for foreign-currency orders
- Sortable columns (click headers to toggle ascending/descending)
- Default sort by market value descending
- Auto-refresh every 15 minutes + manual refresh button per row
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
| `finnhub_api_key` | (optional) | Finnhub API key for US stock fundamentals |

## License

See [LICENSE](LICENSE).
