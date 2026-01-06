package dataapi

// Trade represents a trade from the Data API
type Trade struct {
	ProxyWallet     string  `json:"proxyWallet"`
	Side            string  `json:"side"` // BUY, SELL
	ConditionID     string  `json:"conditionId"`
	Size            float64 `json:"size"`
	Price           float64 `json:"price"`
	Timestamp       int64   `json:"timestamp"` // Unix timestamp in seconds
	Outcome         string  `json:"outcome"`   // YES, NO
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	EventSlug       string  `json:"eventSlug"`
	TransactionHash string  `json:"transactionHash"`
	USDCSize        float64 `json:"usdcSize"` // Preferred notional
}

// ActivityEvent represents an activity event for a wallet
type ActivityEvent struct {
	ID        string `json:"id"`
	EventType string `json:"eventType"`
	User      string `json:"user"`
	Timestamp int64  `json:"timestamp"` // Unix timestamp in seconds
}

// TradesResponse wraps the trades API response
type TradesResponse struct {
	Trades []Trade `json:"data"`
	Count  int     `json:"count"`
}

// ActivityResponse wraps the activity API response
type ActivityResponse struct {
	Activities []ActivityEvent `json:"data"`
	Count      int             `json:"count"`
}

// ErrorResponse represents an API error
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
