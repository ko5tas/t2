package trading212

import (
	"encoding/json"
	"fmt"
	"io"
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

// GetPositions fetches all open positions.
func (c *Client) GetPositions() ([]Position, error) {
	var positions []Position
	if err := c.get("/equity/portfolio", &positions); err != nil {
		return nil, fmt.Errorf("fetching positions: %w", err)
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
