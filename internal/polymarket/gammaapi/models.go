package gammaapi

// Market represents a Gamma API market
type Market struct {
	ID            string  `json:"id"`
	ConditionID   string  `json:"conditionId"`
	Slug          string  `json:"slug"`
	Question      string  `json:"question"`
	EndDate       string  `json:"endDate"`
	Category      string  `json:"category"`
	VolumeNum     float64 `json:"volumeNum"`
	LiquidityNum  float64 `json:"liquidityNum"`
	Active        bool    `json:"active"`
	Closed        bool    `json:"closed"`
	Outcomes      string  `json:"outcomes"`      // e.g., "YES,NO"
	OutcomePrices string  `json:"outcomePrices"` // e.g., "0.02,0.98"
}

// MarketsResponse wraps the markets API response
type MarketsResponse struct {
	Markets []Market `json:"data"`
	Count   int      `json:"count"`
}

// Event represents a Gamma API event
type Event struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Markets     []Market `json:"markets"`
	Category    string   `json:"category"`
	EndDate     string   `json:"endDate"`
	Active      bool     `json:"active"`
	Closed      bool     `json:"closed"`
}
