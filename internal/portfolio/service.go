package portfolio

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ko5tas/t2/internal/trading212"
)

// Service provides portfolio data by combining Trading212 API responses.
type Service struct {
	client *trading212.Client

	mu          sync.RWMutex
	instruments map[string]trading212.Instrument // keyed by ticker
	exchanges   map[int]string                   // workingScheduleId -> exchange name

	returnsMu sync.RWMutex
	returns   map[string]tickerReturns // cached per-ticker returns

	summaryMu sync.RWMutex
	summary   *Summary // cached summary for cheap page polls
}

// NewService creates a new portfolio service and loads initial metadata.
// It retries up to 5 times with 30s backoff if rate-limited on startup.
func NewService(client *trading212.Client) (*Service, error) {
	s := &Service{
		client:  client,
		returns: make(map[string]tickerReturns),
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
}

// refreshReturns fetches order and dividend history and updates the cache.
func (s *Service) refreshReturns() {
	returns := make(map[string]tickerReturns)

	orders, err := s.client.GetOrderHistory()
	if err != nil {
		log.Printf("order history fetch failed: %v", err)
	} else {
		for _, item := range orders {
			// Skip stock splits — they are zero-sum internal rebookings.
			if item.Fill.Type == "STOCK_SPLIT" {
				continue
			}
			tr := returns[item.Order.Ticker]
			switch item.Order.Side {
			case "BUY":
				tr.totalBuyCost += item.Fill.WalletImpact.NetValue
			case "SELL":
				tr.totalSellProceeds += item.Fill.WalletImpact.NetValue
			}
			returns[item.Order.Ticker] = tr
		}
		log.Printf("loaded %d historical orders", len(orders))
	}

	time.Sleep(2 * time.Second) // respect rate limits between endpoints

	dividends, err := s.client.GetDividendHistory()
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

		pos := Position{
			Ticker:         displayTicker,
			RawTicker:      p.Ticker,
			StockName:      stockName,
			Exchange:       exchange,
			MarketValue:    p.CurrentValueGBP,
			Quantity:       p.Quantity,
			Return:         ret,
			ReturnPct:      retPct,
			Invested:       invested,
			PerformancePct: perfPct,
			Profitable:     invested > 0 && p.CurrentValueGBP > invested+1,
		}
		return &pos
	}
	return nil
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

		result = append(result, Position{
			Ticker:         displayTicker,
			RawTicker:      p.Ticker,
			StockName:      stockName,
			Exchange:       exchange,
			MarketValue:    marketValue,
			Quantity:       p.Quantity,
			Return:         ret,
			ReturnPct:      retPct,
			Invested:       invested,
			PerformancePct: perfPct,
			Profitable:     profitable,
		})
		total += marketValue
		totalReturn += ret
		totalInvested += invested
	}

	go s.logCrossCheck(total)

	totalPerfPct := computePerformance(total, totalReturn, totalInvested)

	summary := &Summary{
		Positions:           result,
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
