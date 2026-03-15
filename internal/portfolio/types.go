package portfolio

import "time"

// Position is the domain type displayed in the dashboard.
type Position struct {
	Ticker      string  // display ticker (e.g. "AAPL")
	RawTicker   string  // T212 ticker (e.g. "AAPL_US_EQ")
	StockName   string  // full name (e.g. "Apple Inc.")
	Exchange    string
	MarketValue float64 // in account currency (£)
	Quantity       float64 // number of shares held
	Return         float64 // realised return in £ (sells + dividends)
	ReturnPct      float64 // return as percentage of total buy cost
	Invested       float64 // total buy cost in £
	PerformancePct float64 // (marketValue + sells + dividends - invested) / invested * 100
	Profitable     bool    // true when MarketValue > Invested + £1 (minimum sell threshold)
}

// Summary holds all positions plus aggregates.
type Summary struct {
	Positions        []Position
	TotalMarketValue float64
	TotalReturn      float64
	TotalInvested       float64
	TotalPerformancePct float64
	LastUpdated         time.Time
	AnyProfitable       bool
	Error            string // non-empty if last fetch failed
}
