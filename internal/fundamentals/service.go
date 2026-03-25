package fundamentals

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// resolveCacheDir returns the t2 cache directory path.
// Prefers ~/.cache/t2 for local users, falls back to /var/cache/t2 for system services.
func resolveCacheDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".cache", "t2")
		if err := os.MkdirAll(dir, 0700); err == nil {
			return dir
		}
	}
	const fallback = "/var/cache/t2"
	if err := os.MkdirAll(fallback, 0700); err == nil {
		return fallback
	}
	return ""
}

// Service manages fetching and caching of company fundamentals.
type Service struct {
	mu         sync.RWMutex
	data       map[string]Fundamentals
	finnhubKey string
	httpClient *http.Client
	yahooAuth  *yahooAuth
	cacheFile  string
}

// NewService creates a fundamentals service and loads any existing disk cache.
func NewService(finnhubKey string) *Service {
	cacheDir := resolveCacheDir()
	cacheFile := ""
	if cacheDir != "" {
		cacheFile = filepath.Join(cacheDir, "fundamentals.json")
	}

	s := &Service{
		data:       make(map[string]Fundamentals),
		finnhubKey: finnhubKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		yahooAuth:  newYahooAuth(),
		cacheFile:  cacheFile,
	}

	if cacheFile != "" {
		s.loadCache()
	}

	return s
}

// Get returns cached fundamentals for a display ticker.
func (s *Service) Get(displayTicker string) Fundamentals {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[displayTicker]
}

// RefreshAll fetches fundamentals for all positions, routing to Finnhub or Yahoo by exchange.
func (s *Service) RefreshAll(positions []PositionInfo) {
	// Authenticate with Yahoo before fetching.
	if err := s.yahooAuth.authenticate(); err != nil {
		log.Printf("fundamentals: yahoo auth failed: %v", err)
	} else {
		log.Println("fundamentals: yahoo auth successful")
	}

	// Start from existing cache so failed fetches don't lose previous data.
	s.mu.RLock()
	data := make(map[string]Fundamentals, len(s.data))
	for k, v := range s.data {
		data[k] = v
	}
	s.mu.RUnlock()

	for _, p := range positions {
		// Yahoo is the primary source for all metrics (consistent units).
		yahooTicker := mapYahooTicker(p.DisplayTicker, p.Exchange)
		f, err := fetchYahoo(s.yahooAuth, yahooTicker)
		if err != nil {
			log.Printf("fundamentals: yahoo fetch failed for %s: %v", p.DisplayTicker, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// If Yahoo didn't return a sector and we have Finnhub, try Finnhub profile2.
		if f.Sector == nil && s.finnhubKey != "" && s.isUSExchange(p.Exchange) {
			if sector := fetchFinnhubSector(s.httpClient, s.finnhubKey, p.DisplayTicker); sector != nil {
				f.Sector = sector
			}
			time.Sleep(1100 * time.Millisecond) // respect Finnhub rate limit
		}

		data[p.DisplayTicker] = *f
		log.Printf("fundamentals: loaded %s via yahoo", p.DisplayTicker)
		time.Sleep(500 * time.Millisecond)
	}

	// Convert non-USD financials to USD using live FX rates from Yahoo.
	// Revenue uses FinancialCurrency (reporting currency, e.g. TWD for TSMC).
	// Market cap and EPS use TradingCurrency (e.g. GBp for LSE, USD for NYSE).
	currencies := make(map[string]bool)
	for _, f := range data {
		if f.FinancialCurrency != "" && f.FinancialCurrency != "USD" {
			// Normalize GBp to GBP — we fetch one rate and derive pence.
			ccy := f.FinancialCurrency
			if ccy == "GBp" {
				ccy = "GBP"
			}
			currencies[ccy] = true
		}
		if f.TradingCurrency != "" && f.TradingCurrency != "USD" {
			ccy := f.TradingCurrency
			if ccy == "GBp" {
				ccy = "GBP"
			}
			currencies[ccy] = true
		}
	}
	fxRates := make(map[string]float64)
	for ccy := range currencies {
		rate, err := fetchYahooFXRate(s.yahooAuth, ccy)
		if err != nil {
			log.Printf("fundamentals: FX rate fetch failed for %s: %v", ccy, err)
			continue
		}
		fxRates[ccy] = rate
		log.Printf("fundamentals: FX rate %s→USD = %.6f", ccy, rate)
		time.Sleep(500 * time.Millisecond)
	}
	for ticker, f := range data {
		changed := false
		// Convert revenue (uses financial/reporting currency).
		// Revenue is always in base currency (GBP not GBp).
		if f.Revenue != nil && f.FinancialCurrency != "" && f.FinancialCurrency != "USD" {
			revCcy := f.FinancialCurrency
			if revCcy == "GBp" {
				revCcy = "GBP"
			}
			if rate, ok := fxRates[revCcy]; ok {
				converted := *f.Revenue * rate
				f.Revenue = &converted
				changed = true
			} else {
				f.Revenue = nil
				f.FXError = true
				changed = true
			}
		}
		// Convert market cap and EPS for non-USD trading currencies.
		// Market cap is in the base currency (GBP for LSE, EUR for EPA).
		// EPS is in pence (GBp) for LSE, base currency for others.
		if f.TradingCurrency != "" && f.TradingCurrency != "USD" {
			baseCcy := f.TradingCurrency
			if baseCcy == "GBp" {
				baseCcy = "GBP"
			}
			if rate, ok := fxRates[baseCcy]; ok {
				if f.MarketCap != nil {
					converted := *f.MarketCap * rate
					f.MarketCap = &converted
					changed = true
				}
				if f.EPS != nil {
					epsRate := rate
					if f.TradingCurrency == "GBp" {
						epsRate = rate / 100 // pence → USD
					}
					converted := *f.EPS * epsRate
					f.EPS = &converted
					changed = true
				}
			} else {
				f.MarketCap = nil
				f.EPS = nil
				f.FXError = true
				changed = true
			}
		}
		if changed {
			data[ticker] = f
		}
	}

	s.mu.Lock()
	s.data = data
	s.mu.Unlock()

	s.saveCache()
	log.Printf("fundamentals: refreshed %d/%d positions", len(data), len(positions))
}

func (s *Service) isUSExchange(exchange string) bool {
	return exchange == "NYSE" || exchange == "NASDAQ" || exchange == "OTC"
}

func (s *Service) loadCache() {
	raw, err := os.ReadFile(s.cacheFile)
	if err != nil {
		return // no cache file yet
	}

	var entry cacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		log.Printf("fundamentals: cache parse error: %v", err)
		return
	}

	if time.Since(entry.FetchedAt) > 24*time.Hour {
		log.Println("fundamentals: disk cache expired (>24h), will refresh from APIs")
		return
	}

	// Mark all cached entries as fetched (for caches saved before the Fetched field existed).
	for k, v := range entry.Data {
		v.Fetched = true
		entry.Data[k] = v
	}

	s.mu.Lock()
	s.data = entry.Data
	s.mu.Unlock()
	log.Printf("fundamentals: loaded %d entries from disk cache (fetched %s ago)", len(entry.Data), time.Since(entry.FetchedAt).Round(time.Minute))
}

func (s *Service) saveCache() {
	if s.cacheFile == "" {
		return
	}

	s.mu.RLock()
	entry := cacheEntry{
		Data:      s.data,
		FetchedAt: time.Now(),
	}
	s.mu.RUnlock()

	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		log.Printf("fundamentals: cache marshal error: %v", err)
		return
	}

	if err := os.MkdirAll(filepath.Dir(s.cacheFile), 0700); err != nil {
		log.Printf("fundamentals: cache dir error: %v", err)
		return
	}

	if err := os.WriteFile(s.cacheFile, raw, 0600); err != nil {
		log.Printf("fundamentals: cache write error: %v", err)
		return
	}
}

// NeedsRefresh returns true if the cache is empty or any requested ticker is missing.
func (s *Service) NeedsRefresh(positions []PositionInfo) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.data) == 0 {
		return true
	}
	for _, p := range positions {
		f, ok := s.data[p.DisplayTicker]
		if !ok {
			return true
		}
		// Force refresh if sector data is missing (added in later version).
		if f.Fetched && f.Sector == nil {
			return true
		}
	}
	return false
}
