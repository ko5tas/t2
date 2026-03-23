package portfolio

import (
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ko5tas/t2/internal/fundamentals"
	"github.com/ko5tas/t2/internal/trading212"
)

// Service provides portfolio data by combining Trading212 API responses.
type Service struct {
	client  *trading212.Client
	fundsSvc *fundamentals.Service

	mu          sync.RWMutex
	instruments map[string]trading212.Instrument // keyed by ticker
	exchanges   map[int]string                   // workingScheduleId -> exchange name

	returnsMu sync.RWMutex
	returns   map[string]tickerReturns // cached per-ticker returns

	summaryMu sync.RWMutex
	summary   *Summary // cached summary for cheap page polls

	ordersCachePath    string // ~/.cache/t2/orders.json
	dividendsCachePath string // ~/.cache/t2/dividends.json
}

// NewService creates a new portfolio service and loads initial metadata.
// It retries up to 5 times with 30s backoff if rate-limited on startup.
func NewService(client *trading212.Client, fundsSvc *fundamentals.Service) (*Service, error) {
	s := &Service{
		client:   client,
		fundsSvc: fundsSvc,
		returns:  make(map[string]tickerReturns),
	}
	if dir := cacheDir(); dir != "" {
		s.ordersCachePath = filepath.Join(dir, "orders.json")
		s.dividendsCachePath = filepath.Join(dir, "dividends.json")
	}
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		if err = s.refreshMetadata(); err == nil {
			return s, nil
		}
		log.Printf("metadata load failed (attempt %d/5): %v, retrying in 30s...", attempt, err)
		time.Sleep(30 * time.Second)
	}
	return nil, err
}

// StartMetadataRefresh launches a background goroutine that refreshes
// instrument and exchange metadata once every 24 hours.
func (s *Service) StartMetadataRefresh() {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.refreshMetadata(); err != nil {
				log.Printf("metadata refresh failed: %v", err)
			} else {
				log.Println("metadata refreshed successfully")
			}
		}
	}()
}

// StartReturnsRefresh launches a background goroutine that fetches
// order and dividend history immediately, then refreshes periodically.
func (s *Service) StartReturnsRefresh(interval time.Duration) {
	go func() {
		// Initial fetch after a short delay to let metadata settle.
		time.Sleep(5 * time.Second)
		s.refreshReturns()
		s.refreshSummary() // update summary now that returns are available
		s.tryFundamentalsRefresh()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			s.refreshReturns()
			s.refreshSummary()
		}
	}()
}

// StartSummaryRefresh launches a background goroutine that builds
// the cached summary immediately (positions with dashes), then
// refreshes it periodically so the page always has fresh data.
func (s *Service) StartSummaryRefresh(interval time.Duration) {
	go func() {
		// Build initial summary right away (will show dashes for returns).
		s.refreshSummary()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			s.refreshSummary()
		}
	}()
}

// tickerReturns holds computed return data for a single ticker.
type tickerReturns struct {
	totalBuyCost      float64
	totalSellProceeds float64
	totalDividends    float64
	firstBought       string     // earliest BUY fill date (e.g. "2025-01-15")
	buyHistory        []BuyEntry // all BUY fills (date + quantity)
}

// fxRateEntry holds a date and rate pair for FX lookups.
type fxRateEntry struct {
	date string  // "2025-09-17"
	rate float64 // e.g. 1.3662 (GBP/foreign)
}

// buildFxRateLookup scans GBP-denominated orders on foreign-currency instruments
// to extract historical FX rates. Returns a map of currency -> sorted rate entries.
func buildFxRateLookup(orders []trading212.OrderHistoryItem) map[string][]fxRateEntry {
	rates := make(map[string][]fxRateEntry)
	seen := make(map[string]bool) // dedupe by currency+date

	for _, item := range orders {
		wi := item.Fill.WalletImpact
		// We want GBP wallet orders with a meaningful fxRate (> 1).
		if wi.Currency != "GBP" || wi.FxRate <= 1 {
			continue
		}
		// The instrument currency tells us what foreign currency this rate converts from.
		instCcy := item.Order.Instrument.Currency
		if instCcy == "" || instCcy == "GBP" || instCcy == "GBX" {
			continue
		}
		date := ""
		if len(item.Fill.FilledAt) >= 10 {
			date = item.Fill.FilledAt[:10]
		}
		if date == "" {
			continue
		}
		key := instCcy + date
		if seen[key] {
			continue
		}
		seen[key] = true
		rates[instCcy] = append(rates[instCcy], fxRateEntry{date: date, rate: wi.FxRate})
	}

	// Sort each currency's entries by date for binary search.
	for ccy := range rates {
		sort.Slice(rates[ccy], func(i, j int) bool {
			return rates[ccy][i].date < rates[ccy][j].date
		})
	}
	return rates
}

// nearestRate finds the closest FX rate by date for a given currency.
func nearestRate(rates map[string][]fxRateEntry, currency, date string) (float64, bool) {
	entries := rates[currency]
	if len(entries) == 0 {
		return 0, false
	}
	// Binary search for the nearest date.
	idx := sort.Search(len(entries), func(i int) bool {
		return entries[i].date >= date
	})
	// Check the entry at idx and idx-1, pick the closest.
	best := -1
	if idx < len(entries) {
		best = idx
	}
	if idx > 0 {
		if best == -1 {
			best = idx - 1
		} else {
			// Both candidates exist; no need for precise distance — just pick nearest.
			// Dates are strings, so simple comparison works for "closeness" heuristic.
			if date < entries[best].date && idx-1 >= 0 {
				best = idx - 1 // prefer the earlier date if we're between two
			}
		}
	}
	if best == -1 {
		return 0, false
	}
	return entries[best].rate, true
}

// toGBP converts an order's netValue to GBP using the FX rate lookup.
func toGBP(item trading212.OrderHistoryItem, rates map[string][]fxRateEntry) float64 {
	wi := item.Fill.WalletImpact
	if wi.Currency == "GBP" || wi.Currency == "" {
		return wi.NetValue
	}
	// Try to find a historical rate for this currency.
	date := ""
	if len(item.Fill.FilledAt) >= 10 {
		date = item.Fill.FilledAt[:10]
	}
	if rate, ok := nearestRate(rates, wi.Currency, date); ok {
		return wi.NetValue / rate
	}
	log.Printf("WARNING: no FX rate found for %s on %s, using netValue as-is (%.2f %s treated as GBP)",
		wi.Currency, date, wi.NetValue, wi.Currency)
	return wi.NetValue
}

// refreshReturns fetches order and dividend history and updates the cache.
func (s *Service) refreshReturns() {
	returns := make(map[string]tickerReturns)

	orders, err := fetchOrdersIncremental(s.client, s.ordersCachePath)
	if err != nil {
		log.Printf("order history fetch failed: %v", err)
	} else {
		fxRates := buildFxRateLookup(orders)
		for _, item := range orders {
			// Skip stock splits — they are zero-sum internal rebookings.
			if item.Fill.Type == "STOCK_SPLIT" {
				continue
			}
			// Skip unfilled orders (safety net).
			if item.Fill.WalletImpact.Currency == "" {
				continue
			}
			netGBP := toGBP(item, fxRates)
			tr := returns[item.Order.Ticker]
			switch item.Order.Side {
			case "BUY":
				tr.totalBuyCost += netGBP
				fillDate := item.Fill.FilledAt
				if len(fillDate) >= 10 {
					fillDate = fillDate[:10]
				}
				if tr.firstBought == "" || fillDate < tr.firstBought {
					tr.firstBought = fillDate
				}
				tr.buyHistory = append(tr.buyHistory, BuyEntry{
					Date:     fillDate,
					Quantity: item.Fill.Quantity,
				})
			case "SELL":
				tr.totalSellProceeds += netGBP
			}
			returns[item.Order.Ticker] = tr
		}
		// Sort each ticker's buy history oldest-first.
		for ticker, tr := range returns {
			sort.Slice(tr.buyHistory, func(i, j int) bool {
				return tr.buyHistory[i].Date < tr.buyHistory[j].Date
			})
			returns[ticker] = tr
		}
		log.Printf("loaded %d historical orders", len(orders))
	}

	time.Sleep(2 * time.Second) // respect rate limits between endpoints

	dividends, err := fetchDividendsIncremental(s.client, s.dividendsCachePath)
	if err != nil {
		log.Printf("dividend history fetch failed: %v", err)
	} else {
		for _, item := range dividends {
			tr := returns[item.Ticker]
			tr.totalDividends += item.Amount
			returns[item.Ticker] = tr
		}
		log.Printf("loaded %d historical dividends", len(dividends))
	}

	s.returnsMu.Lock()
	s.returns = returns
	s.returnsMu.Unlock()
}

// cachedReturns returns the current cached returns map.
func (s *Service) cachedReturns() map[string]tickerReturns {
	s.returnsMu.RLock()
	defer s.returnsMu.RUnlock()
	return s.returns
}

// GetPosition fetches a single position by its raw ticker (e.g. "AAPL_US_EQ").
func (s *Service) GetPosition(rawTicker string) *Position {
	positions, err := s.client.GetPositions()
	if err != nil {
		return nil
	}

	returns := s.cachedReturns()

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range positions {
		if p.Ticker != rawTicker {
			continue
		}
		displayTicker := p.Ticker
		stockName := p.Name
		exchange := "Unknown"
		if inst, ok := s.instruments[p.Ticker]; ok {
			displayTicker = tickerDisplay(inst)
			if stockName == "" {
				stockName = inst.Name
			}
			if exName, ok := s.exchanges[inst.WorkingScheduleID]; ok {
				exchange = exName
			}
		}

		ret, retPct, invested := computeReturn(returns[p.Ticker])
		perfPct := computePerformance(p.CurrentValueGBP, ret, invested)

		currency := ""
		isin := ""
		if inst, ok := s.instruments[p.Ticker]; ok {
			currency = inst.CurrencyCode
			isin = inst.ISIN
		}
		var priceGBP float64
		if p.Quantity > 0 {
			priceGBP = p.CurrentValueGBP / p.Quantity
		}
		tr := returns[p.Ticker]
		var divYield float64
		if p.CurrentValueGBP > 0 && tr.totalDividends > 0 {
			divYield = tr.totalDividends / p.CurrentValueGBP * 100
		}

		pos := Position{
			Ticker:           displayTicker,
			RawTicker:        p.Ticker,
			StockName:        stockName,
			Exchange:         shortenExchange(exchange),
			ExchangeFull:     fullExchangeName(exchange),
			MarketValue:      p.CurrentValueGBP,
			Quantity:         p.Quantity,
			CurrentPrice:     p.CurrentPrice,
			Currency:         currency,
			CurrentPriceGBP:  priceGBP,
			TotalDividends:   tr.totalDividends,
			DividendYieldPct: divYield,
			FirstBought:      tr.firstBought,
			ISIN:             isin,
			Return:           ret,
			ReturnPct:        retPct,
			Invested:         invested,
			PerformancePct:   perfPct,
			Profitable:       invested > 0 && p.CurrentValueGBP > invested+1,
		}
		s.enrichWithFundamentals(&pos)
		s.updateSummaryPosition(&pos)
		return &pos
	}
	return nil
}

// updateSummaryPosition patches the cached summary with a freshly fetched position
// so that the next HTMX poll does not overwrite it with stale data.
func (s *Service) updateSummaryPosition(pos *Position) {
	s.summaryMu.Lock()
	defer s.summaryMu.Unlock()

	if s.summary == nil {
		return
	}

	found := false
	for i := range s.summary.Positions {
		if s.summary.Positions[i].RawTicker == pos.RawTicker {
			s.summary.Positions[i] = *pos
			found = true
			break
		}
	}
	if !found {
		return
	}

	var total, totalReturn, totalInvested float64
	anyProfitable := false
	for _, p := range s.summary.Positions {
		total += p.MarketValue
		totalReturn += p.Return
		totalInvested += p.Invested
		if p.Profitable {
			anyProfitable = true
		}
	}

	s.summary.TotalMarketValue = total
	s.summary.TotalReturn = totalReturn
	s.summary.TotalInvested = totalInvested
	s.summary.TotalPerformancePct = computePerformance(total, totalReturn, totalInvested)
	s.summary.AnyProfitable = anyProfitable
}

// GetSummary returns the cached portfolio summary.
// Returns a placeholder if no data has been fetched yet.
func (s *Service) GetSummary() *Summary {
	s.summaryMu.RLock()
	defer s.summaryMu.RUnlock()
	if s.summary != nil {
		return s.summary
	}
	return &Summary{LastUpdated: time.Now()}
}

// refreshSummary fetches current positions from the API and rebuilds the cached summary.
func (s *Service) refreshSummary() {
	positions, err := s.client.GetPositions()
	if err != nil {
		log.Printf("summary refresh failed: %v", err)
		return // keep serving stale cache rather than overwriting with error
	}

	returns := s.cachedReturns()

	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Position
	var total float64
	var totalReturn float64
	var totalInvested float64
	var anyProfitable bool

	for _, p := range positions {
		marketValue := p.CurrentValueGBP

		displayTicker := p.Ticker
		stockName := p.Name
		exchange := "Unknown"
		if inst, ok := s.instruments[p.Ticker]; ok {
			displayTicker = tickerDisplay(inst)
			if stockName == "" {
				stockName = inst.Name
			}
			if exName, ok := s.exchanges[inst.WorkingScheduleID]; ok {
				exchange = exName
			}
		}

		ret, retPct, invested := computeReturn(returns[p.Ticker])
		perfPct := computePerformance(marketValue, ret, invested)
		profitable := invested > 0 && marketValue > invested+1

		if profitable {
			anyProfitable = true
		}

		currency := ""
		isin := ""
		if inst, ok := s.instruments[p.Ticker]; ok {
			currency = inst.CurrencyCode
			isin = inst.ISIN
		}
		var priceGBP float64
		if p.Quantity > 0 {
			priceGBP = marketValue / p.Quantity
		}

		var divYield float64
		tr := returns[p.Ticker]
		if marketValue > 0 && tr.totalDividends > 0 {
			divYield = tr.totalDividends / marketValue * 100
		}

		pos := Position{
			Ticker:           displayTicker,
			RawTicker:        p.Ticker,
			StockName:        stockName,
			Exchange:         shortenExchange(exchange),
			ExchangeFull:     fullExchangeName(exchange),
			MarketValue:      marketValue,
			Quantity:         p.Quantity,
			CurrentPrice:     p.CurrentPrice,
			Currency:         currency,
			CurrentPriceGBP:  priceGBP,
			TotalDividends:   tr.totalDividends,
			DividendYieldPct: divYield,
			FirstBought:      tr.firstBought,
			ISIN:             isin,
			BuyHistory:       tr.buyHistory,
			Return:           ret,
			ReturnPct:        retPct,
			Invested:         invested,
			PerformancePct:   perfPct,
			Profitable:       profitable,
		}
		s.enrichWithFundamentals(&pos)
		result = append(result, pos)
		total += marketValue
		totalReturn += ret
		totalInvested += invested
	}

	go s.logCrossCheck(total)

	// Build closed positions: tickers in returns but not in open positions.
	openTickers := make(map[string]bool, len(positions))
	for _, p := range positions {
		openTickers[p.Ticker] = true
	}
	var closed []Position
	for ticker, tr := range returns {
		if openTickers[ticker] {
			continue
		}
		// Must have had at least one buy to be meaningful.
		if tr.totalBuyCost == 0 {
			continue
		}
		ret, retPct, invested := computeReturn(tr)
		perfPct := computePerformance(0, ret, invested) // no market value for closed

		displayTicker := ticker
		stockName := ""
		exchange := "Unknown"
		isin := ""
		if inst, ok := s.instruments[ticker]; ok {
			displayTicker = tickerDisplay(inst)
			stockName = inst.Name
			isin = inst.ISIN
			if exName, ok := s.exchanges[inst.WorkingScheduleID]; ok {
				exchange = exName
			}
		}

		var divYield float64
		if invested > 0 && tr.totalDividends > 0 {
			divYield = tr.totalDividends / invested * 100
		}

		pos := Position{
			Ticker:           displayTicker,
			RawTicker:        ticker,
			StockName:        stockName,
			Exchange:         shortenExchange(exchange),
			ExchangeFull:     fullExchangeName(exchange),
			Return:           ret,
			ReturnPct:        retPct,
			Invested:         invested,
			PerformancePct:   perfPct,
			TotalDividends:   tr.totalDividends,
			DividendYieldPct: divYield,
			FirstBought:      tr.firstBought,
			ISIN:             isin,
			BuyHistory:       tr.buyHistory,
		}
		s.enrichWithFundamentals(&pos)
		closed = append(closed, pos)
	}

	totalPerfPct := computePerformance(total, totalReturn, totalInvested)

	summary := &Summary{
		Positions:           result,
		ClosedPositions:     closed,
		TotalMarketValue:    total,
		TotalReturn:         totalReturn,
		TotalInvested:       totalInvested,
		TotalPerformancePct: totalPerfPct,
		AnyProfitable:       anyProfitable,
		LastUpdated:         time.Now(),
	}

	s.summaryMu.Lock()
	s.summary = summary
	s.summaryMu.Unlock()
}

func computeReturn(tr tickerReturns) (ret, retPct, invested float64) {
	ret = tr.totalSellProceeds + tr.totalDividends
	invested = tr.totalBuyCost
	if tr.totalBuyCost > 0 {
		retPct = (ret / tr.totalBuyCost) * 100
	}
	return
}

func computePerformance(marketValue, recovered, invested float64) float64 {
	if invested > 0 {
		return (marketValue + recovered - invested) / invested * 100
	}
	return 0
}

func (s *Service) refreshMetadata() error {
	exchanges, err := s.client.GetExchanges()
	if err != nil {
		return err
	}

	exchangeMap := make(map[int]string)
	for _, ex := range exchanges {
		for _, ws := range ex.WorkingSchedules {
			exchangeMap[ws.ID] = ex.Name
		}
	}

	time.Sleep(2 * time.Second)

	instruments, err := s.client.GetInstruments()
	if err != nil {
		return err
	}

	instrumentMap := make(map[string]trading212.Instrument, len(instruments))
	for _, inst := range instruments {
		instrumentMap[inst.Ticker] = inst
	}

	s.mu.Lock()
	s.instruments = instrumentMap
	s.exchanges = exchangeMap
	s.mu.Unlock()

	log.Printf("loaded %d instruments and %d exchange schedule mappings", len(instrumentMap), len(exchangeMap))
	return nil
}

func tickerDisplay(inst trading212.Instrument) string {
	if inst.ShortName != "" {
		return inst.ShortName
	}
	ticker := inst.Ticker
	for _, suffix := range []string{"_US_EQ", "_L_EQ", "_PA_EQ", "_CA_EQ"} {
		ticker = strings.TrimSuffix(ticker, suffix)
	}
	return ticker
}

// PositionTickers returns display ticker + exchange pairs for all known instruments
// that have open positions. Used by the fundamentals service to know which tickers to fetch.
func (s *Service) PositionTickers() []fundamentals.PositionInfo {
	s.summaryMu.RLock()
	defer s.summaryMu.RUnlock()
	if s.summary == nil {
		return nil
	}
	all := append(s.summary.Positions, s.summary.ClosedPositions...)
	infos := make([]fundamentals.PositionInfo, len(all))
	for i, p := range all {
		infos[i] = fundamentals.PositionInfo{
			DisplayTicker: p.Ticker,
			Exchange:      p.Exchange,
		}
	}
	return infos
}

// tryFundamentalsRefresh checks if any tickers are missing from the fundamentals
// cache and fetches them if needed. Called after returns refresh completes.
func (s *Service) tryFundamentalsRefresh() {
	if s.fundsSvc == nil {
		return
	}
	tickers := s.PositionTickers()
	log.Printf("fundamentals: %d tickers to check", len(tickers))
	if len(tickers) > 0 && s.fundsSvc.NeedsRefresh(tickers) {
		s.fundsSvc.RefreshAll(tickers)
		s.refreshSummary()
	}
}

// StartFundamentalsRefresh launches a background goroutine that refreshes
// company fundamentals once every 24 hours.
func (s *Service) StartFundamentalsRefresh() {
	if s.fundsSvc == nil {
		return
	}
	go func() {
		// Initial check after a short delay (covers startup without order history).
		time.Sleep(15 * time.Second)
		s.tryFundamentalsRefresh()

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			tickers := s.PositionTickers()
			if len(tickers) > 0 {
				s.fundsSvc.RefreshAll(tickers)
				s.refreshSummary()
			}
		}
	}()
}

// enrichWithFundamentals populates the 6 fundamental fields on a Position.
func (s *Service) enrichWithFundamentals(pos *Position) {
	if s.fundsSvc == nil {
		return
	}
	f := s.fundsSvc.Get(pos.Ticker)
	pos.FundsFetched = f.Fetched
	pos.PERatio = f.PERatio
	pos.MarketCapM = f.MarketCap
	pos.EPS = f.EPS
	pos.EPSGrowthPct = f.EPSGrowthPct
	pos.RevenueM = f.Revenue
	pos.ProfitMarginPct = f.ProfitMarginPct
}

// shortenExchange abbreviates common exchange names for compact display.
var exchangeAbbreviations = map[string]string{
	"London Stock Exchange":         "LSE",
	"London Stock Exchange AIM":     "LAIM",
	"London Stock Exchange NON-ISA": "LSE*",
	"Deutsche Börse Xetra":          "XETR",
	"Euronext Paris":                "EPA",
	"Euronext Amsterdam":            "AMS",
	"Euronext Lisbon":               "ELI",
	"Euronext Brussels":             "EBR",
	"Borsa Italiana":                "BIT",
	"SIX Swiss Exchange":            "SIX",
	"Bolsa de Madrid":               "BME",
	"Wiener Börse":                  "VIE",
	"Toronto Stock Exchange":        "TSX",
	"OTC Markets":                   "OTC",
	"Gettex":                        "GETTEX",
}

// exchangeFullNames expands terse Trading212 names into proper full names for tooltips.
var exchangeFullNames = map[string]string{
	"NASDAQ": "National Association of Securities Dealers Automated Quotations",
	"NYSE":   "New York Stock Exchange",
}

func shortenExchange(name string) string {
	if short, ok := exchangeAbbreviations[name]; ok {
		return short
	}
	return name
}

func fullExchangeName(name string) string {
	if full, ok := exchangeFullNames[name]; ok {
		return full
	}
	return name
}

func (s *Service) logCrossCheck(calculatedTotal float64) {
	cash, err := s.client.GetAccountCash()
	if err != nil {
		log.Printf("cross-check: could not fetch account cash: %v", err)
		return
	}
	accountTotal := cash.Invested + cash.Ppl
	diff := calculatedTotal - accountTotal
	log.Printf("cross-check: calculated=%.2f, account(invested+ppl)=%.2f, diff=%.2f", calculatedTotal, accountTotal, diff)
}
