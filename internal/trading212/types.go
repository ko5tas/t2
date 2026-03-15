package trading212

// wirePosition is the JSON shape returned by GET /api/v0/equity/positions.
// This is an undocumented but richer endpoint that includes walletImpact
// with the current value already converted to account currency (GBP).
type wirePosition struct {
	Instrument struct {
		Ticker string `json:"ticker"`
		Name   string `json:"name"`
	} `json:"instrument"`
	Quantity     float64 `json:"quantity"`
	AveragePrice float64 `json:"averagePricePaid"`
	CurrentPrice float64 `json:"currentPrice"`
	WalletImpact struct {
		CurrentValue float64 `json:"currentValue"`
	} `json:"walletImpact"`
}

// Position is the parsed position with the GBP market value from T212.
type Position struct {
	Ticker          string
	Name            string
	Quantity        float64
	AveragePrice    float64
	CurrentPrice    float64
	CurrentValueGBP float64 // from walletImpact.currentValue — already in account currency
}

// Instrument represents instrument metadata.
type Instrument struct {
	Ticker            string  `json:"ticker"`
	Name              string  `json:"name"`
	ShortName         string  `json:"shortName"`
	Type              string  `json:"type"`
	CurrencyCode      string  `json:"currencyCode"`
	ISIN              string  `json:"isin"`
	MaxOpenQuantity   float64 `json:"maxOpenQuantity"`
	AddedOn           string  `json:"addedOn"`
	WorkingScheduleID int     `json:"workingScheduleId"`
}

// Exchange represents an exchange with its working schedules.
type Exchange struct {
	ID               int               `json:"id"`
	Name             string            `json:"name"`
	WorkingSchedules []WorkingSchedule `json:"workingSchedules"`
}

// WorkingSchedule represents a trading schedule.
type WorkingSchedule struct {
	ID         int         `json:"id"`
	TimeEvents []TimeEvent `json:"timeEvents"`
}

// TimeEvent represents an open/close event.
type TimeEvent struct {
	Date string `json:"date"`
	Type string `json:"type"`
}

// OrderHistoryResponse is the paginated response from /equity/history/orders.
type OrderHistoryResponse struct {
	Items        []OrderHistoryItem `json:"items"`
	NextPagePath *string            `json:"nextPagePath"`
}

// OrderHistoryItem is a single filled order from history.
type OrderHistoryItem struct {
	Order struct {
		Ticker string `json:"ticker"`
		Side   string `json:"side"` // "BUY" or "SELL"
	} `json:"order"`
	Fill struct {
		Quantity     float64 `json:"quantity"`
		Type         string  `json:"type"` // e.g. "MARKET", "STOCK_SPLIT"
		WalletImpact struct {
			NetValue float64 `json:"netValue"` // in account currency (GBP)
		} `json:"walletImpact"`
	} `json:"fill"`
}

// DividendHistoryResponse is the paginated response from /equity/history/dividends.
type DividendHistoryResponse struct {
	Items        []DividendHistoryItem `json:"items"`
	NextPagePath *string               `json:"nextPagePath"`
}

// DividendHistoryItem is a single dividend payout from history.
type DividendHistoryItem struct {
	Ticker string  `json:"ticker"`
	Amount float64 `json:"amount"` // in account currency (GBP)
}

// AccountCash represents the account cash breakdown.
type AccountCash struct {
	Blocked  float64 `json:"blocked"`
	Free     float64 `json:"free"`
	Invested float64 `json:"invested"`
	PieCash  float64 `json:"pieCash"`
	Ppl      float64 `json:"ppl"`
	Result   float64 `json:"result"`
	Total    float64 `json:"total"`
}
