package fundamentals

import "time"

// Fundamentals holds company financial metrics fetched from external APIs.
type Fundamentals struct {
	Fetched         bool     `json:"fetched"`                    // true once lookup attempted
	PERatio         *float64 `json:"pe_ratio,omitempty"`
	MarketCap       *float64 `json:"market_cap,omitempty"`       // in original units (e.g. USD)
	EPS             *float64 `json:"eps,omitempty"`
	EPSGrowthPct    *float64 `json:"eps_growth_pct,omitempty"`
	Revenue         *float64 `json:"revenue,omitempty"`          // in original units
	ProfitMarginPct *float64 `json:"profit_margin_pct,omitempty"`
}

// PositionInfo identifies a position for the fundamentals service.
type PositionInfo struct {
	DisplayTicker string
	Exchange      string
}

// cacheEntry is the on-disk JSON cache structure.
type cacheEntry struct {
	Data      map[string]Fundamentals `json:"data"`
	FetchedAt time.Time               `json:"fetched_at"`
}
