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
		var f *Fundamentals
		var err error
		var source string

		if s.isUSExchange(p.Exchange) && s.finnhubKey != "" {
			source = "finnhub"
			f, err = fetchFinnhub(s.httpClient, s.finnhubKey, p.DisplayTicker)
			if err != nil {
				log.Printf("fundamentals: %s fetch failed for %s: %v, falling back to yahoo", source, p.DisplayTicker, err)
				source = "yahoo"
				yahooTicker := mapYahooTicker(p.DisplayTicker, p.Exchange)
				f, err = fetchYahoo(s.yahooAuth, yahooTicker)
			}
			time.Sleep(1100 * time.Millisecond) // respect Finnhub rate limit
		} else {
			source = "yahoo"
			yahooTicker := mapYahooTicker(p.DisplayTicker, p.Exchange)
			f, err = fetchYahoo(s.yahooAuth, yahooTicker)
			time.Sleep(500 * time.Millisecond) // be respectful to Yahoo
		}

		if err != nil {
			log.Printf("fundamentals: %s fetch failed for %s: %v", source, p.DisplayTicker, err)
			continue
		}
		if f != nil {
			data[p.DisplayTicker] = *f
			log.Printf("fundamentals: loaded %s via %s", p.DisplayTicker, source)
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
		if _, ok := s.data[p.DisplayTicker]; !ok {
			return true
		}
	}
	return false
}
