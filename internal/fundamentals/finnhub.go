package fundamentals

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// fetchFinnhubSector retrieves only the sector classification from Finnhub's profile2 endpoint.
// All financial metrics (P/E, EPS, Revenue, etc.) are sourced from Yahoo for unit consistency.
func fetchFinnhubSector(httpClient *http.Client, apiKey, ticker string) *string {
	url := fmt.Sprintf("https://finnhub.io/api/v1/stock/profile2?symbol=%s&token=%s", ticker, apiKey)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var profile struct {
		FinnhubIndustry string `json:"finnhubIndustry"`
	}
	if json.Unmarshal(body, &profile) != nil || profile.FinnhubIndustry == "" {
		return nil
	}

	sector := mapFinnhubToSector(profile.FinnhubIndustry)
	return &sector
}

// mapFinnhubToSector converts Finnhub's industry classification to Yahoo-style sector names.
var finnhubIndustryToSector = map[string]string{
	// Technology
	"Technology":                "Technology",
	"Semiconductors":            "Technology",
	"Software":                  "Technology",
	"Communication Equipment":   "Technology",
	// Financial Services
	"Banking":                   "Financial Services",
	"Insurance":                 "Financial Services",
	"Financial Services":        "Financial Services",
	"Capital Markets":           "Financial Services",
	// Healthcare
	"Pharmaceuticals":           "Healthcare",
	"Biotechnology":             "Healthcare",
	"Medical Devices":           "Healthcare",
	"Health Care":               "Healthcare",
	// Energy
	"Oil & Gas":                 "Energy",
	"Energy":                    "Energy",
	"Oil/Gas":                   "Energy",
	// Industrials
	"Aerospace & Defense":       "Industrials",
	"Industrial":                "Industrials",
	"Machinery":                 "Industrials",
	"Airlines":                  "Industrials",
	"Defense":                   "Industrials",
	// Consumer Cyclical
	"Retail":                    "Consumer Cyclical",
	"Auto":                      "Consumer Cyclical",
	"Luxury Goods":              "Consumer Cyclical",
	"E-Commerce":                "Consumer Cyclical",
	// Consumer Defensive
	"Consumer Products":         "Consumer Defensive",
	"Tobacco":                   "Consumer Defensive",
	"Food & Beverage":           "Consumer Defensive",
	"Beverages":                 "Consumer Defensive",
	// Communication Services
	"Media":                     "Communication Services",
	"Telecommunications":        "Communication Services",
	"Internet Media & Services": "Communication Services",
	// Basic Materials
	"Mining":                    "Basic Materials",
	"Metals & Mining":           "Basic Materials",
	"Chemicals":                 "Basic Materials",
	"Basic Materials":           "Basic Materials",
	// Utilities
	"Utilities":                 "Utilities",
	"Electric Utilities":        "Utilities",
	// Real Estate
	"REITs":                     "Real Estate",
	"Real Estate":               "Real Estate",
}

func mapFinnhubToSector(industry string) string {
	if sector, ok := finnhubIndustryToSector[industry]; ok {
		return sector
	}
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
