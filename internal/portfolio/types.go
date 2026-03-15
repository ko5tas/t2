package portfolio

import "time"

// Position is the domain type displayed in the dashboard.
type Position struct {
	Ticker      string  // display ticker (e.g. "AAPL")
	StockName   string  // full name (e.g. "Apple Inc.")
	Exchange    string
	MarketValue float64 // in account currency (£)
}

// Summary holds all positions plus aggregates.
type Summary struct {
	Positions        []Position
	TotalMarketValue float64
	LastUpdated      time.Time
	Error            string // non-empty if last fetch failed
}
