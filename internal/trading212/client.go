package trading212

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Client is an HTTP client for the Trading212 API.
type Client struct {
	baseURL    string
	apiKey     string
	apiSecret  string
	httpClient *http.Client
}

// NewClient creates a new Trading212 API client.
func NewClient(baseURL, apiKey, apiSecret string) *Client {
	return &Client{
		baseURL:   baseURL,
		apiKey:    apiKey,
		apiSecret: apiSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetPositions fetches all open positions via /equity/positions.
// This endpoint returns walletImpact.currentValue which is the position's
// market value already converted to account currency (GBP) by Trading212.
func (c *Client) GetPositions() ([]Position, error) {
	var raw []wirePosition
	if err := c.get("/equity/positions", &raw); err != nil {
		return nil, fmt.Errorf("fetching positions: %w", err)
	}

	positions := make([]Position, len(raw))
	for i, r := range raw {
		positions[i] = Position{
			Ticker:          r.Instrument.Ticker,
			Name:            r.Instrument.Name,
			Quantity:        r.Quantity,
			AveragePrice:    r.AveragePrice,
			CurrentPrice:    r.CurrentPrice,
			CurrentValueGBP: r.WalletImpact.CurrentValue,
		}
	}
	return positions, nil
}

// GetInstruments fetches all instrument metadata.
func (c *Client) GetInstruments() ([]Instrument, error) {
	var instruments []Instrument
	if err := c.get("/equity/metadata/instruments", &instruments); err != nil {
		return nil, fmt.Errorf("fetching instruments: %w", err)
	}
	return instruments, nil
}

// GetExchanges fetches all exchanges with their working schedules.
func (c *Client) GetExchanges() ([]Exchange, error) {
	var exchanges []Exchange
	if err := c.get("/equity/metadata/exchanges", &exchanges); err != nil {
		return nil, fmt.Errorf("fetching exchanges: %w", err)
	}
	return exchanges, nil
}

// GetAccountCash fetches the account cash breakdown.
func (c *Client) GetAccountCash() (*AccountCash, error) {
	var cash AccountCash
	if err := c.get("/equity/account/cash", &cash); err != nil {
		return nil, fmt.Errorf("fetching account cash: %w", err)
	}
	return &cash, nil
}

// GetOrderHistory fetches all historical orders, paginating through all pages.
// Rate limit is 6 req/60s so we sleep 11s between pages and retry on 429.
func (c *Client) GetOrderHistory() ([]OrderHistoryItem, error) {
	var all []OrderHistoryItem
	path := "/equity/history/orders?limit=50"
	for path != "" {
		var page OrderHistoryResponse
		if err := c.getWithRetry(path, &page); err != nil {
			return nil, fmt.Errorf("fetching order history: %w", err)
		}
		all = append(all, page.Items...)
		path = nextPage(page.NextPagePath)
		if path != "" {
			time.Sleep(11 * time.Second)
		}
	}
	return all, nil
}

// GetDividendHistory fetches all historical dividends, paginating through all pages.
func (c *Client) GetDividendHistory() ([]DividendHistoryItem, error) {
	var all []DividendHistoryItem
	path := "/equity/history/dividends?limit=50"
	for path != "" {
		var page DividendHistoryResponse
		if err := c.getWithRetry(path, &page); err != nil {
			return nil, fmt.Errorf("fetching dividend history: %w", err)
		}
		all = append(all, page.Items...)
		path = nextPage(page.NextPagePath)
		if path != "" {
			time.Sleep(11 * time.Second)
		}
	}
	return all, nil
}

// nextPage strips the /api/v0 prefix from a nextPagePath pointer.
func nextPage(p *string) string {
	if p == nil {
		return ""
	}
	path := *p
	const prefix = "/api/v0"
	if len(path) > len(prefix) && path[:len(prefix)] == prefix {
		path = path[len(prefix):]
	}
	return path
}

// getWithRetry calls get, retrying up to 3 times on 429 rate limit errors.
func (c *Client) getWithRetry(path string, result any) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := c.get(path, result)
		if err == nil {
			return nil
		}
		if !isRateLimited(err) {
			return err
		}
		log.Printf("rate limited, waiting 30s before retry (attempt %d/3)", attempt+1)
		time.Sleep(30 * time.Second)
	}
	return c.get(path, result) // final attempt
}

func isRateLimited(err error) bool {
	return err != nil && fmt.Sprintf("%v", err) == "rate limited by Trading212 API (429). Try again later"
}

func (c *Client) get(path string, result any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.SetBasicAuth(c.apiKey, c.apiSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("API authentication failed (401). Check your api_key and api_secret")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited by Trading212 API (429). Try again later")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	return nil
}
