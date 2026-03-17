package fundamentals

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
)

// yahooAuth manages the cookie + crumb session for Yahoo Finance API.
type yahooAuth struct {
	mu     sync.Mutex
	client *http.Client // client with cookie jar
	crumb  string
}

const yahooUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// newYahooAuth creates a new Yahoo auth manager with its own cookie jar.
func newYahooAuth() *yahooAuth {
	jar, _ := cookiejar.New(nil)
	return &yahooAuth{
		client: &http.Client{
			Jar:     jar,
			Timeout: 15e9, // 15 seconds
		},
	}
}

// authenticate obtains a session cookie and crumb token from Yahoo.
func (y *yahooAuth) authenticate() error {
	y.mu.Lock()
	defer y.mu.Unlock()

	// Step 1: Get session cookie by hitting fc.yahoo.com.
	req, err := http.NewRequest("GET", "https://fc.yahoo.com", nil)
	if err != nil {
		return fmt.Errorf("yahoo auth cookie request: %w", err)
	}
	req.Header.Set("User-Agent", yahooUserAgent)
	resp, err := y.client.Do(req)
	if err != nil {
		return fmt.Errorf("yahoo auth cookie: %w", err)
	}
	resp.Body.Close()

	// Step 2: Get crumb token.
	req, err = http.NewRequest("GET", "https://query2.finance.yahoo.com/v1/test/getcrumb", nil)
	if err != nil {
		return fmt.Errorf("yahoo auth crumb request: %w", err)
	}
	req.Header.Set("User-Agent", yahooUserAgent)
	resp, err = y.client.Do(req)
	if err != nil {
		return fmt.Errorf("yahoo auth crumb: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("yahoo auth crumb: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("yahoo auth crumb read: %w", err)
	}

	y.crumb = strings.TrimSpace(string(body))
	if y.crumb == "" {
		return fmt.Errorf("yahoo auth: empty crumb")
	}

	return nil
}

// mapYahooTicker converts a display ticker + exchange to a Yahoo Finance symbol.
func mapYahooTicker(displayTicker, exchange string) string {
	ex := strings.ToLower(exchange)
	switch {
	case strings.Contains(ex, "london"):
		return displayTicker + ".L"
	case strings.Contains(ex, "paris"), strings.Contains(ex, "euronext"):
		return displayTicker + ".PA"
	default:
		return displayTicker
	}
}

// fetchYahoo retrieves fundamental metrics from Yahoo Finance's quoteSummary endpoint.
func fetchYahoo(auth *yahooAuth, yahooTicker string) (*Fundamentals, error) {
	auth.mu.Lock()
	crumb := auth.crumb
	client := auth.client
	auth.mu.Unlock()

	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=defaultKeyStatistics,financialData,summaryDetail,earningsTrend&crumb=%s",
		yahooTicker, crumb,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("yahoo request for %s: %w", yahooTicker, err)
	}
	req.Header.Set("User-Agent", yahooUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yahoo request for %s: %w", yahooTicker, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("yahoo rate limited for %s", yahooTicker)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("yahoo %s: status %d", yahooTicker, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("yahoo read %s: %w", yahooTicker, err)
	}

	var result struct {
		QuoteSummary struct {
			Result []struct {
				DefaultKeyStatistics map[string]interface{} `json:"defaultKeyStatistics"`
				FinancialData        map[string]interface{} `json:"financialData"`
				SummaryDetail        map[string]interface{} `json:"summaryDetail"`
				EarningsTrend        struct {
					Trend []struct {
						Period string                 `json:"period"`
						Growth map[string]interface{} `json:"growth"`
					} `json:"trend"`
				} `json:"earningsTrend"`
			} `json:"result"`
			Error interface{} `json:"error"`
		} `json:"quoteSummary"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("yahoo parse %s: %w", yahooTicker, err)
	}

	if len(result.QuoteSummary.Result) == 0 {
		return nil, fmt.Errorf("yahoo %s: no results", yahooTicker)
	}

	r := result.QuoteSummary.Result[0]
	f := &Fundamentals{Fetched: true}

	// Yahoo wraps numeric values in {"raw": 123.45, "fmt": "123.45"} objects.
	f.PERatio = extractYahooRaw(r.SummaryDetail, "trailingPE")
	if f.PERatio == nil {
		f.PERatio = extractYahooRaw(r.DefaultKeyStatistics, "trailingPE")
	}
	f.MarketCap = extractYahooRaw(r.SummaryDetail, "marketCap")
	if f.MarketCap == nil {
		f.MarketCap = extractYahooRaw(r.FinancialData, "marketCap")
	}
	f.EPS = extractYahooRaw(r.DefaultKeyStatistics, "trailingEps")
	f.EPSGrowthPct = extractYahooRaw(r.FinancialData, "earningsGrowth")
	if f.EPSGrowthPct == nil {
		f.EPSGrowthPct = extractYahooRaw(r.DefaultKeyStatistics, "earningsQuarterlyGrowth")
	}
	// Fallback: use earningsTrend analyst consensus for current year.
	if f.EPSGrowthPct == nil {
		for _, t := range r.EarningsTrend.Trend {
			if t.Period == "0y" {
				f.EPSGrowthPct = extractFloat(t.Growth, "raw")
				break
			}
		}
	}
	// Convert growth from decimal to percentage (Yahoo returns 0.15 for 15%).
	if f.EPSGrowthPct != nil {
		pct := *f.EPSGrowthPct * 100
		f.EPSGrowthPct = &pct
	}
	f.Revenue = extractYahooRaw(r.FinancialData, "totalRevenue")
	f.ProfitMarginPct = extractYahooRaw(r.FinancialData, "profitMargins")
	if f.ProfitMarginPct == nil {
		f.ProfitMarginPct = extractYahooRaw(r.DefaultKeyStatistics, "profitMargins")
	}
	// Convert margin from decimal to percentage.
	if f.ProfitMarginPct != nil {
		pct := *f.ProfitMarginPct * 100
		f.ProfitMarginPct = &pct
	}

	return f, nil
}

// extractYahooRaw extracts the "raw" value from a Yahoo Finance {"raw": N, "fmt": "..."} object.
func extractYahooRaw(m map[string]interface{}, key string) *float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		// Sometimes Yahoo returns a bare number.
		return extractFloat(m, key)
	}
	return extractFloat(obj, "raw")
}
