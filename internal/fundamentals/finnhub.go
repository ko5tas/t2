package fundamentals

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// fetchFinnhub retrieves fundamental metrics from the Finnhub API for a US stock.
func fetchFinnhub(httpClient *http.Client, apiKey, ticker string) (*Fundamentals, error) {
	url := fmt.Sprintf("https://finnhub.io/api/v1/stock/metric?symbol=%s&metric=all&token=%s", ticker, apiKey)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("finnhub request for %s: %w", ticker, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("finnhub rate limited for %s", ticker)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("finnhub %s: status %d", ticker, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("finnhub read %s: %w", ticker, err)
	}

	var result struct {
		Metric map[string]interface{} `json:"metric"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("finnhub parse %s: %w", ticker, err)
	}

	if result.Metric == nil {
		return nil, fmt.Errorf("finnhub %s: no metric data", ticker)
	}

	f := &Fundamentals{Fetched: true}
	f.PERatio = extractFloat(result.Metric, "peBasicExclExtraTTM")
	f.MarketCap = extractFloat(result.Metric, "marketCapitalization")
	f.EPS = extractFloat(result.Metric, "epsBasicExclExtraItemsTTM")
	f.EPSGrowthPct = extractFloat(result.Metric, "epsGrowthTTMYoy")
	f.Revenue = extractFloat(result.Metric, "revenuePerShareTTM")
	f.ProfitMarginPct = extractFloat(result.Metric, "netProfitMarginTTM")

	// Finnhub returns marketCapitalization in millions — keep as-is.

	// Fetch sector from profile2 endpoint (finnhubIndustry is not in metrics).
	profileURL := fmt.Sprintf("https://finnhub.io/api/v1/stock/profile2?symbol=%s&token=%s", ticker, apiKey)
	profileResp, err := httpClient.Get(profileURL)
	if err == nil {
		defer func() { _ = profileResp.Body.Close() }()
		if profileResp.StatusCode == 200 {
			profileBody, _ := io.ReadAll(profileResp.Body)
			var profile struct {
				FinnhubIndustry string `json:"finnhubIndustry"`
			}
			if json.Unmarshal(profileBody, &profile) == nil && profile.FinnhubIndustry != "" {
				sector := mapFinnhubToSector(profile.FinnhubIndustry)
				f.Sector = &sector
			}
		}
	}

	return f, nil
}

// mapFinnhubToSector converts Finnhub's industry classification to Yahoo-style sector names.
var finnhubIndustryToSector = map[string]string{
	// Technology
	"Technology":                  "Technology",
	"Semiconductors":              "Technology",
	"Software":                    "Technology",
	"Communication Equipment":     "Technology",
	// Financial Services
	"Banking":                     "Financial Services",
	"Insurance":                   "Financial Services",
	"Financial Services":          "Financial Services",
	"Capital Markets":             "Financial Services",
	// Healthcare
	"Pharmaceuticals":             "Healthcare",
	"Biotechnology":               "Healthcare",
	"Medical Devices":             "Healthcare",
	"Health Care":                 "Healthcare",
	// Energy
	"Oil & Gas":                   "Energy",
	"Energy":                      "Energy",
	"Oil/Gas":                     "Energy",
	// Industrials
	"Aerospace & Defense":         "Industrials",
	"Industrial":                  "Industrials",
	"Machinery":                   "Industrials",
	"Airlines":                    "Industrials",
	"Defense":                     "Industrials",
	// Consumer Cyclical
	"Retail":                      "Consumer Cyclical",
	"Auto":                        "Consumer Cyclical",
	"Luxury Goods":                "Consumer Cyclical",
	"E-Commerce":                  "Consumer Cyclical",
	// Consumer Defensive
	"Consumer Products":           "Consumer Defensive",
	"Tobacco":                     "Consumer Defensive",
	"Food & Beverage":             "Consumer Defensive",
	"Beverages":                   "Consumer Defensive",
	// Communication Services
	"Media":                       "Communication Services",
	"Telecommunications":          "Communication Services",
	"Internet Media & Services":   "Communication Services",
	// Basic Materials
	"Mining":                      "Basic Materials",
	"Metals & Mining":             "Basic Materials",
	"Chemicals":                   "Basic Materials",
	"Basic Materials":             "Basic Materials",
	// Utilities
	"Utilities":                   "Utilities",
	"Electric Utilities":          "Utilities",
	// Real Estate
	"REITs":                       "Real Estate",
	"Real Estate":                 "Real Estate",
}

func mapFinnhubToSector(industry string) string {
	if sector, ok := finnhubIndustryToSector[industry]; ok {
		return sector
	}
	// Fallback: use the industry name as-is.
	return industry
}

// extractFloat pulls a float64 from a map, returning nil if absent or not a number.
func extractFloat(m map[string]interface{}, key string) *float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case float64:
		return &n
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return nil
		}
		return &f
	}
	return nil
}
