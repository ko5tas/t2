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
	CurrentPrice    float64 // price in native currency
	Currency        string  // native currency code (USD, GBX, EUR, GBP)
	CurrentPriceGBP float64 // price per share in £
	TotalDividends   float64 // total dividend income in £
	DividendYieldPct float64 // totalDividends / marketValue * 100
	FirstBought     string  // date of first buy order (e.g. "2025-01-15")
	ISIN            string  // instrument ISIN
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
