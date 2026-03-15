# Design Decisions

## Dashboard Columns

The positions table displays columns in this order:

| # | Column | Description |
|---|--------|-------------|
| 1 | **#** | Row number |
| 2 | **Name** | Ticker (short name or cleaned), stock name, raw T212 ticker |
| 3 | **Market Value** | Current position value in GBP (with per-row refresh button) |
| 4 | **Exchange** | Exchange name from T212 instrument metadata |
| 5 | **Recovered (Sells+Dividends)** | Total sell proceeds + dividends received for the ticker |
| 6 | **Invested** | Total buy cost for the ticker (displayed as "£100.00 bought") |
| 7 | **Recovered %** | (Recovered / Invested) * 100 |
| 8 | **Performance %** | (Market Value + Recovered - Invested) / Invested * 100 |
| 9 | **Qty** | Number of shares held |

**Default sort**: Market Value descending.

## Performance % Color Tiers

| Range | Color | CSS Class | Hex |
|-------|-------|-----------|-----|
| < 0% | Red | `perf-negative` | `#ef5350` |
| 0% – 9.99% | Orange | `perf-warning` | `#ff9800` |
| 10% – 24.99% | White | (default) | `#ffffff` |
| 25% – 49.99% | Yellow | `perf-good` | `#fdd835` |
| 50% – 99.99% | Green | `perf-positive` | `#4caf50` |
| >= 100% | Teal | `perf-legendary` | `#5bc0be` |

## Data Loading & Caching

### Startup sequence
1. Load instrument + exchange metadata (retries up to 5 times with 30s backoff if rate-limited)
2. Build initial summary from live positions API (shows dashes for return columns)
3. Background goroutine fetches order + dividend history (~3-5 min due to pagination and rate limits)
4. Summary is rebuilt immediately after returns data loads

### Caching strategy
- **Summary cache**: `GetSummary()` reads from a cached `*Summary` (mutex-protected). No API calls on page poll.
- **Returns cache**: Order/dividend history stored in `map[string]tickerReturns`, refreshed every 15 minutes.
- **HTMX page poll**: Every 30 seconds (cheap cache read). API fetches happen on the 15-minute interval.
- **Metadata refresh**: Instruments and exchanges refreshed every 24 hours.

### Dash placeholders
When returns data hasn't loaded yet (Invested = 0), the Recovered, Invested, Recovered %, and Performance % columns show "—" instead of misleading £0.00 / 0.00% values.

## Stock Split Handling

Orders with `fill.type == "STOCK_SPLIT"` are skipped in return calculations. Stock splits create phantom BUY/SELL entries that are zero-sum internal rebookings and would inflate the invested amount.

## Rate Limit Resilience

- **Startup**: Metadata fetch retries up to 5 times with 30s backoff. Prevents systemd crash-loop on DietPi when rate-limited.
- **Order/dividend history**: Paginated fetch with 3 retries per page, 30s backoff between retries.
- **Between endpoints**: 2-second sleep between different API calls to respect rate limits.

## Text Alignment

All table columns are left-aligned for consistent layout.

## Release Pipeline

- Triggered by semver tags (e.g., `0.2.5`) — no "v" prefix.
- All jobs run sequentially on tag push: create-release -> build -> publish-repo.
- Builds .deb packages for arm64 and armhf (DietPi targets).
- Publishes to APT repository on gh-pages branch (GPG-signed).
- Note: `GITHUB_TOKEN`-created releases don't trigger `release: published` events, so all jobs chain via `needs:` instead.

## Git Workflow

- Feature branches from main, PR to main.
- No git worktrees — work directly in main repository.
- Descriptive branch names (e.g., `feature/add-sorting`, `fix/text-selection`).
- Branch protection enabled on main.
- Clean up stale local and remote branches after merging.

## UI Interactions

- **Column sorting**: Click header to toggle ascending/descending.
- **Double-click on ticker/name**: Selects full span text for easy copy.
- **Per-row refresh**: Button next to market value fetches fresh data for that position via HTMX.
