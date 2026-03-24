package fundamentals

// sectorMedianPE maps Yahoo Finance sector names to trailing P/E ratios.
// Source: S&P 500 sector P/E ratios from worldperatio.com (March 2026).
// Yahoo uses "Consumer Cyclical" / "Consumer Defensive" instead of S&P's
// "Consumer Discretionary" / "Consumer Staples".
var sectorMedianPE = map[string]float64{
	"Technology":             33.90,
	"Real Estate":            31.78,
	"Industrials":            30.79,
	"Consumer Cyclical":      30.74, // S&P: Consumer Discretionary
	"Basic Materials":        27.64, // S&P: Materials
	"Consumer Defensive":     27.37, // S&P: Consumer Staples
	"Healthcare":             26.90,
	"Utilities":              22.72,
	"Energy":                 22.60,
	"Communication Services": 17.51,
	"Financial Services":     17.23, // S&P: Financials
}

// SectorMedianPE returns the trailing P/E ratio for the given sector,
// or nil if the sector is unknown.
func SectorMedianPE(sector string) *float64 {
	if pe, ok := sectorMedianPE[sector]; ok {
		return &pe
	}
	return nil
}
