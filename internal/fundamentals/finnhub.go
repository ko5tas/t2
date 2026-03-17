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
	defer resp.Body.Close()

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
	return f, nil
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
