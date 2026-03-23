package portfolio

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ko5tas/t2/internal/trading212"
)

// ordersCacheFile is the on-disk format for cached order history.
type ordersCacheFile struct {
	Items     []trading212.OrderHistoryItem `json:"items"`
	FetchedAt time.Time                     `json:"fetchedAt"`
}

// dividendsCacheFile is the on-disk format for cached dividend history.
type dividendsCacheFile struct {
	Items     []trading212.DividendHistoryItem `json:"items"`
	FetchedAt time.Time                        `json:"fetchedAt"`
}

// orderKey returns a deduplication key for an order fill.
func orderKey(item trading212.OrderHistoryItem) string {
	return fmt.Sprintf("%s|%s|%.6f|%s",
		item.Order.Ticker, item.Fill.FilledAt, item.Fill.Quantity, item.Order.Side)
}

// dividendKey returns a deduplication key for a dividend payout.
func dividendKey(item trading212.DividendHistoryItem) string {
	if item.Reference != "" {
		return item.Reference
	}
	// Fallback for cached items from before Reference was captured.
	return fmt.Sprintf("%s|%.2f|%s", item.Ticker, item.Amount, item.PaidOn)
}

// cacheDir returns the t2 cache directory path (~/.cache/t2).
func cacheDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "t2")
	}
	return ""
}

// loadOrdersCache reads the orders cache from disk.
func loadOrdersCache(path string) []trading212.OrderHistoryItem {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache ordersCacheFile
	if err := json.Unmarshal(raw, &cache); err != nil {
		log.Printf("history-cache: orders parse error: %v", err)
		return nil
	}
	log.Printf("history-cache: loaded %d orders from disk (cached %s ago)",
		len(cache.Items), time.Since(cache.FetchedAt).Round(time.Minute))
	return cache.Items
}

// saveOrdersCache writes the orders cache to disk.
func saveOrdersCache(path string, items []trading212.OrderHistoryItem) {
	if path == "" {
		return
	}
	cache := ordersCacheFile{Items: items, FetchedAt: time.Now()}
	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		log.Printf("history-cache: orders marshal error: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Printf("history-cache: dir error: %v", err)
		return
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		log.Printf("history-cache: orders write error: %v", err)
		return
	}
	log.Printf("history-cache: saved %d orders to disk", len(items))
}

// loadDividendsCache reads the dividends cache from disk.
func loadDividendsCache(path string) []trading212.DividendHistoryItem {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache dividendsCacheFile
	if err := json.Unmarshal(raw, &cache); err != nil {
		log.Printf("history-cache: dividends parse error: %v", err)
		return nil
	}
	log.Printf("history-cache: loaded %d dividends from disk (cached %s ago)",
		len(cache.Items), time.Since(cache.FetchedAt).Round(time.Minute))
	return cache.Items
}

// saveDividendsCache writes the dividends cache to disk.
func saveDividendsCache(path string, items []trading212.DividendHistoryItem) {
	if path == "" {
		return
	}
	cache := dividendsCacheFile{Items: items, FetchedAt: time.Now()}
	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		log.Printf("history-cache: dividends marshal error: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Printf("history-cache: dir error: %v", err)
		return
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		log.Printf("history-cache: dividends write error: %v", err)
		return
	}
	log.Printf("history-cache: saved %d dividends to disk", len(items))
}

// fetchOrdersIncremental fetches order history incrementally, using the cache
// to avoid re-fetching pages that overlap with already-known data.
func fetchOrdersIncremental(client *trading212.Client, cachePath string) ([]trading212.OrderHistoryItem, error) {
	cached := loadOrdersCache(cachePath)

	// Build set of known keys from cache.
	known := make(map[string]bool, len(cached))
	for _, item := range cached {
		known[orderKey(item)] = true
	}

	var newItems []trading212.OrderHistoryItem
	path := ""
	overlapFound := false

	for {
		items, next, err := client.GetOrderHistoryPage(path)
		if err != nil {
			if len(cached) > 0 {
				log.Printf("history-cache: order fetch error, using cached data: %v", err)
				return cached, nil
			}
			return nil, err
		}

		for _, item := range items {
			if known[orderKey(item)] {
				overlapFound = true
				break
			}
			newItems = append(newItems, item)
		}

		if overlapFound || next == "" {
			break
		}

		path = next
		time.Sleep(11 * time.Second)
	}

	if len(newItems) == 0 {
		log.Printf("history-cache: 0 new orders (cache is current)")
		return cached, nil
	}

	// Prepend new items (newest-first) to cached items.
	merged := append(newItems, cached...)
	saveOrdersCache(cachePath, merged)
	log.Printf("history-cache: %d new orders fetched, total %d", len(newItems), len(merged))
	return merged, nil
}

// fetchDividendsIncremental fetches dividend history incrementally, using the cache
// to avoid re-fetching pages that overlap with already-known data.
func fetchDividendsIncremental(client *trading212.Client, cachePath string) ([]trading212.DividendHistoryItem, error) {
	cached := loadDividendsCache(cachePath)

	known := make(map[string]bool, len(cached))
	for _, item := range cached {
		known[dividendKey(item)] = true
	}

	var newItems []trading212.DividendHistoryItem
	path := ""
	overlapFound := false

	for {
		items, next, err := client.GetDividendHistoryPage(path)
		if err != nil {
			if len(cached) > 0 {
				log.Printf("history-cache: dividend fetch error, using cached data: %v", err)
				return cached, nil
			}
			return nil, err
		}

		for _, item := range items {
			if known[dividendKey(item)] {
				overlapFound = true
				break
			}
			newItems = append(newItems, item)
		}

		if overlapFound || next == "" {
			break
		}

		path = next
		time.Sleep(11 * time.Second)
	}

	if len(newItems) == 0 {
		log.Printf("history-cache: 0 new dividends (cache is current)")
		return cached, nil
	}

	merged := append(newItems, cached...)
	saveDividendsCache(cachePath, merged)
	log.Printf("history-cache: %d new dividends fetched, total %d", len(newItems), len(merged))
	return merged, nil
}
