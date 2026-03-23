# Fix: Fundamentals data disappearing after individual refresh

## Context
When individually refreshing a stock, the fundamentals columns (P/E, EPS, etc.) appear briefly then disappear after a few seconds. This is the same class of bug as commit d7a1c8d ("Fix individual refresh being overwritten by stale cached summary") but affects fundamentals specifically.

## Root Cause
Two issues work together:

1. **`refreshSummary()` race condition**: Multiple goroutines call `refreshSummary()` (StartSummaryRefresh, StartReturnsRefresh, StartFundamentalsRefresh). When `refreshSummary()` runs, it replaces the entire `s.summary` (line 496-498), potentially overwriting an individual refresh's `updateSummaryPosition()` update if they overlap.

2. **`RefreshAll()` wholesale cache replacement** (lines 62-98 in `fundamentals/service.go`): Creates an empty map, only adds successful fetches, then replaces the entire `s.data`. Any stock that fails to fetch is silently dropped from the cache.

3. **Startup timing**: At t=0 and t=5s, `refreshSummary()` runs before fundamentals are loaded (15s delay). If the HTMX 30s poll fires during this window, it renders positions without fundamentals.

## Fix

### 1. Merge fundamentals in `RefreshAll()` instead of replacing
**File**: `internal/fundamentals/service.go`, `RefreshAll()` method

Instead of:
```go
data := make(map[string]Fundamentals)
// ...fetch loop...
s.data = data
```

Do:
```go
// Start from existing cache so failed fetches don't lose data
s.mu.RLock()
data := make(map[string]Fundamentals, len(s.data))
for k, v := range s.data {
    data[k] = v
}
s.mu.RUnlock()

// ...fetch loop (overwrites only successful fetches)...

s.mu.Lock()
s.data = data
s.mu.Unlock()
```

This preserves previously cached fundamentals for stocks that fail on the current refresh cycle. Also prevents unnecessary API usage from re-fetching data that was lost.

### 2. Ensure `refreshSummary()` preserves individual refresh updates
**File**: `internal/portfolio/service.go`

No code change needed here — the current `updateSummaryPosition()` correctly writes back to the cached summary. The race window is small (15-min interval) and the fix in step 1 ensures fundamentals data is always available in the cache when `enrichWithFundamentals()` reads it.

### 3. Delay initial HTMX poll until fundamentals are ready (optional)
Not needed if disk cache is valid. The disk cache loads synchronously at startup.

## Files to modify
- `internal/fundamentals/service.go` — `RefreshAll()` method (lines 54-102)

## Verification
1. Build and run locally
2. Wait for fundamentals to load (check server logs)
3. Individually refresh a stock → verify fundamentals appear
4. Wait 30+ seconds for HTMX poll → verify fundamentals persist
5. Check server logs for any fundamentals fetch failures → verify those stocks retain cached values
