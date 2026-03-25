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
	_ = resp.Body.Close()

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
	defer func() { _ = resp.Body.Close() }()

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

// mapYahooTicker converts a display ticker + abbreviated exchange to a Yahoo Finance symbol.
func mapYahooTicker(displayTicker, exchange string) string {
	switch exchange {
	case "LSE", "LAIM", "LSE*":
		return displayTicker + ".L"
	case "EPA":
		return displayTicker + ".PA"
	case "AMS":
		return displayTicker + ".AS"
	case "ELI":
		return displayTicker + ".LS"
	case "EBR":
		return displayTicker + ".BR"
	case "XETR", "GETTEX":
		return displayTicker + ".DE"
	case "BIT":
		return displayTicker + ".MI"
	case "SIX":
		return displayTicker + ".SW"
	case "BME":
		return displayTicker + ".MC"
	case "VIE":
		return displayTicker + ".VI"
	case "TSX":
		return displayTicker + ".TO"
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
		"https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=defaultKeyStatistics,financialData,summaryDetail,earningsTrend,assetProfile&crumb=%s",
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
	defer func() { _ = resp.Body.Close() }()

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
				AssetProfile         map[string]interface{} `json:"assetProfile"`
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
	// summaryDetail.marketCap is in the trading currency.
	f.MarketCap = extractYahooRaw(r.SummaryDetail, "marketCap")
	if v, ok := r.SummaryDetail["currency"].(string); ok {
		f.TradingCurrency = v
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
	// Always extract revenue; currency conversion is handled by RefreshAll.
	f.Revenue = extractYahooRaw(r.FinancialData, "totalRevenue")
	if v, ok := r.FinancialData["financialCurrency"].(string); ok {
		f.FinancialCurrency = v
	}
	f.ProfitMarginPct = extractYahooRaw(r.FinancialData, "profitMargins")
	if f.ProfitMarginPct == nil {
		f.ProfitMarginPct = extractYahooRaw(r.DefaultKeyStatistics, "profitMargins")
	}
	// Convert margin from decimal to percentage.
	if f.ProfitMarginPct != nil {
		pct := *f.ProfitMarginPct * 100
		f.ProfitMarginPct = &pct
	}

	// Extract sector from assetProfile module.
	if r.AssetProfile != nil {
		if s, ok := r.AssetProfile["sector"].(string); ok && s != "" {
			f.Sector = &s
		}
	}

	return f, nil
}

// fetchYahooFXRate fetches the exchange rate for a currency pair from Yahoo (e.g. "TWD" → TWDUSD=X).
// Returns the rate to multiply by to convert from the given currency to USD.
// Callers should normalize GBp to GBP before calling.
func fetchYahooFXRate(auth *yahooAuth, currency string) (float64, error) {
	ticker := currency + "USD=X"

	auth.mu.Lock()
	crumb := auth.crumb
	client := auth.client
	auth.mu.Unlock()

	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=price&crumb=%s",
		ticker, crumb,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("yahoo FX request for %s: %w", ticker, err)
	}
	req.Header.Set("User-Agent", yahooUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("yahoo FX fetch %s: %w", ticker, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("yahoo FX %s: status %d", ticker, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("yahoo FX read %s: %w", ticker, err)
	}

	var result struct {
		QuoteSummary struct {
			Result []struct {
				Price map[string]interface{} `json:"price"`
			} `json:"result"`
		} `json:"quoteSummary"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("yahoo FX parse %s: %w", ticker, err)
	}

	if len(result.QuoteSummary.Result) == 0 {
		return 0, fmt.Errorf("yahoo FX %s: no results", ticker)
	}

	rate := extractYahooRaw(result.QuoteSummary.Result[0].Price, "regularMarketPrice")
	if rate == nil || *rate == 0 {
		return 0, fmt.Errorf("yahoo FX %s: no rate", ticker)
	}


	return *rate, nil
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
