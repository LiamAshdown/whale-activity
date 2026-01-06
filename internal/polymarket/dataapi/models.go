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
	ProxyWallet          string  `json:"proxyWallet"`
	Timestamp            int64   `json:"timestamp"` // Unix timestamp in seconds
	ConditionID          string  `json:"conditionId"`
	Type                 string  `json:"type"` // TRADE, TRANSFER, etc.
	Size                 float64 `json:"size"`
	USDCSize             float64 `json:"usdcSize"`
	TransactionHash      string  `json:"transactionHash"`
	Price                float64 `json:"price"`
	Asset                string  `json:"asset"`
	Side                 string  `json:"side"` // BUY, SELL
	OutcomeIndex         int     `json:"outcomeIndex"`
	Title                string  `json:"title"`
	Slug                 string  `json:"slug"`
	Icon                 string  `json:"icon"`
	EventSlug            string  `json:"eventSlug"`
	Outcome              string  `json:"outcome"`
	Name                 string  `json:"name"`
	Pseudonym            string  `json:"pseudonym"`
	Bio                  string  `json:"bio"`
	ProfileImage         string  `json:"profileImage"`
	ProfileImageOptimized string `json:"profileImageOptimized"`
}

// GetFromAddress extracts the 'from' address from activity details (for funding events)
// Note: This may need to be updated based on actual funding event structure
func (a *ActivityEvent) GetFromAddress() string {
	// For TRANSFER type events, the from address might be in a different field
	// This is a placeholder - update based on actual API response
	return ""
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

// TradeParams holds parameters for fetching trades
type TradeParams struct {
	Limit          int
	Offset         int
	TakerOnly      bool
	FilterType     string
	FilterAmount   float64
	Market         string
	EventID        string
	User           string
	Side           string
	SortBy         string // e.g., "timestamp"
	SortDirection  string // "ASC" or "DESC"
}
