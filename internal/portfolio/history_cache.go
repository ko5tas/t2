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

// cacheDir returns the t2 cache directory path.
// Order: $CACHE_DIRECTORY (set by systemd CacheDirectory=), $HOME/.cache/t2,
// then /var/cache/t2 as a last resort.
func cacheDir() string {
	// systemd sets CACHE_DIRECTORY when the unit has CacheDirectory=t2.
	// This is the right answer for service installations.
	if dir := os.Getenv("CACHE_DIRECTORY"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err == nil {
			return dir
		}
		log.Printf("history-cache: CACHE_DIRECTORY=%q is not writable", dir)
	}
	// Honor $HOME but skip the well-known /nonexistent placeholder used for
	// system users — MkdirAll would silently succeed if a stale directory
	// existed and silently fail otherwise.
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/nonexistent" {
		dir := filepath.Join(home, ".cache", "t2")
		if err := os.MkdirAll(dir, 0700); err == nil {
			return dir
		}
	}
	const fallback = "/var/cache/t2"
	if err := os.MkdirAll(fallback, 0700); err == nil {
		return fallback
	}
	log.Printf("history-cache: WARNING — no writable cache directory found; cache disabled")
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

// loadOrdersCacheWithFallback reads the primary cache and falls back to the
// backup_dir copy if the primary is missing. Used at startup so a fresh
// install (or wiped cache directory) can rehydrate from the user's cloud
// backup without re-fetching everything from Trading212.
func loadOrdersCacheWithFallback(path, backupDir string) []trading212.OrderHistoryItem {
	if items := loadOrdersCache(path); items != nil {
		return items
	}
	if backupDir == "" {
		return nil
	}
	bp := filepath.Join(backupDir, "orders.json")
	items := loadOrdersCache(bp)
	if items != nil {
		log.Printf("history-cache: rehydrated %d orders from backup_dir (%s)", len(items), bp)
		// Seed the primary cache so subsequent loads don't re-touch the backup.
		saveOrdersCache(path, items)
	}
	return items
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
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		log.Printf("history-cache: dir error: %v", err)
		return
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
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

// loadDividendsCacheWithFallback mirrors loadOrdersCacheWithFallback.
func loadDividendsCacheWithFallback(path, backupDir string) []trading212.DividendHistoryItem {
	if items := loadDividendsCache(path); items != nil {
		return items
	}
	if backupDir == "" {
		return nil
	}
	bp := filepath.Join(backupDir, "dividends.json")
	items := loadDividendsCache(bp)
	if items != nil {
		log.Printf("history-cache: rehydrated %d dividends from backup_dir (%s)", len(items), bp)
		saveDividendsCache(path, items)
	}
	return items
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
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		log.Printf("history-cache: dir error: %v", err)
		return
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		log.Printf("history-cache: dividends write error: %v", err)
		return
	}
	log.Printf("history-cache: saved %d dividends to disk", len(items))
}

// mirrorOrders writes the same orders cache to a backup location (e.g. a
// Dropbox- or rclone-synced directory). Errors are logged but not returned —
// the primary cache write is what matters for runtime correctness.
func mirrorOrders(backupDir string, items []trading212.OrderHistoryItem) {
	if backupDir == "" {
		return
	}
	saveOrdersCache(filepath.Join(backupDir, "orders.json"), items)
}

// mirrorDividends mirrors the dividends cache to a backup location.
func mirrorDividends(backupDir string, items []trading212.DividendHistoryItem) {
	if backupDir == "" {
		return
	}
	saveDividendsCache(filepath.Join(backupDir, "dividends.json"), items)
}

// fetchOrdersIncremental fetches order history incrementally, using the cache
// to avoid re-fetching pages that overlap with already-known data.
// If backupDir is non-empty, the cache is mirrored there after each successful
// save and used as a fallback rehydration source if the primary cache is missing.
func fetchOrdersIncremental(client *trading212.Client, cachePath, backupDir string) ([]trading212.OrderHistoryItem, error) {
	cached := loadOrdersCacheWithFallback(cachePath, backupDir)

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
	mirrorOrders(backupDir, merged)
	log.Printf("history-cache: %d new orders fetched, total %d", len(newItems), len(merged))
	return merged, nil
}

// fetchDividendsIncremental fetches dividend history incrementally, using the cache
// to avoid re-fetching pages that overlap with already-known data.
// If backupDir is non-empty, the cache is mirrored there after each successful
// save and used as a fallback rehydration source if the primary cache is missing.
func fetchDividendsIncremental(client *trading212.Client, cachePath, backupDir string) ([]trading212.DividendHistoryItem, error) {
	cached := loadDividendsCacheWithFallback(cachePath, backupDir)

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
	mirrorDividends(backupDir, merged)
	log.Printf("history-cache: %d new dividends fetched, total %d", len(newItems), len(merged))
	return merged, nil
}
