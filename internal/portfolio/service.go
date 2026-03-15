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
}

// NewService creates a new portfolio service and loads initial metadata.
func NewService(client *trading212.Client) (*Service, error) {
	s := &Service{
		client: client,
	}
	if err := s.refreshMetadata(); err != nil {
		return nil, err
	}
	return s, nil
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

// GetSummary fetches current positions and returns a portfolio summary.
func (s *Service) GetSummary() *Summary {
	positions, err := s.client.GetPositions()
	if err != nil {
		return &Summary{
			LastUpdated: time.Now(),
			Error:       err.Error(),
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Position
	var total float64

	for _, p := range positions {
		// Calculate £ market value.
		marketValue := (p.AveragePrice * p.Quantity) + p.Ppl + p.FxPpl

		// Look up instrument metadata.
		displayTicker := p.Ticker
		stockName := ""
		exchange := "Unknown"
		if inst, ok := s.instruments[p.Ticker]; ok {
			displayTicker = tickerDisplay(inst)
			stockName = inst.Name
			if exName, ok := s.exchanges[inst.WorkingScheduleID]; ok {
				exchange = exName
			}
		}

		result = append(result, Position{
			Ticker:      displayTicker,
			StockName:   stockName,
			Exchange:    exchange,
			MarketValue: marketValue,
		})
		total += marketValue
	}

	// Log cross-check against account cash.
	go s.logCrossCheck(total)

	return &Summary{
		Positions:        result,
		TotalMarketValue: total,
		LastUpdated:      time.Now(),
	}
}

func (s *Service) refreshMetadata() error {
	exchanges, err := s.client.GetExchanges()
	if err != nil {
		return err
	}

	// Build schedule ID -> exchange name map.
	exchangeMap := make(map[int]string)
	for _, ex := range exchanges {
		for _, ws := range ex.WorkingSchedules {
			exchangeMap[ws.ID] = ex.Name
		}
	}

	// Wait before fetching instruments to respect rate limits.
	time.Sleep(2 * time.Second)

	instruments, err := s.client.GetInstruments()
	if err != nil {
		return err
	}

	// Build ticker -> instrument map.
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

// tickerDisplay returns the display ticker (without the suffix like _US_EQ).
func tickerDisplay(inst trading212.Instrument) string {
	if inst.ShortName != "" {
		return inst.ShortName
	}
	// Fallback: strip common suffixes.
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
