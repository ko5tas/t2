package trading212

// Position represents an open position from the portfolio endpoint.
type Position struct {
	Ticker          string  `json:"ticker"`
	Quantity        float64 `json:"quantity"`
	AveragePrice    float64 `json:"averagePrice"`
	CurrentPrice    float64 `json:"currentPrice"`
	Ppl             float64 `json:"ppl"`
	FxPpl           float64 `json:"fxPpl"`
	InitialFillDate string  `json:"initialFillDate"`
	Frontend        string  `json:"frontend"`
	MaxBuy          float64 `json:"maxBuy"`
	MaxSell         float64 `json:"maxSell"`
	PieQuantity     float64 `json:"pieQuantity"`
}

// Instrument represents instrument metadata.
type Instrument struct {
	Ticker             string  `json:"ticker"`
	Name               string  `json:"name"`
	ShortName          string  `json:"shortName"`
	Type               string  `json:"type"`
	CurrencyCode       string  `json:"currencyCode"`
	ISIN               string  `json:"isin"`
	MaxOpenQuantity    float64 `json:"maxOpenQuantity"`
	AddedOn            string  `json:"addedOn"`
	WorkingScheduleID  int     `json:"workingScheduleId"`
}

// Exchange represents an exchange with its working schedules.
type Exchange struct {
	ID               int                `json:"id"`
	Name             string             `json:"name"`
	WorkingSchedules []WorkingSchedule  `json:"workingSchedules"`
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
