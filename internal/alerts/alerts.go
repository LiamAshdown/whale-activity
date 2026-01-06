package alerts

import (
	"context"
	"time"
)

// Severity represents alert severity
type Severity string

const (
	SeverityInfo  Severity = "INFO"
	SeverityWarn  Severity = "WARN"
	SeverityAlert Severity = "ALERT"
)

// ScoreBreakdown contains the calculation details for the suspicion score
type ScoreBreakdown struct {
	BaseScore                  float64
	TimeToCloseMultiplier      float64
	WinRateMultiplier          float64
	FirstTradeLargeMultiplier  float64
	FlashFundingMultiplier     float64
	LiquidityMultiplier        float64
	PriceConfidenceMultiplier  float64
	ConcentrationMultiplier    float64
	VelocityMultiplier         float64
	ClusterMultiplier          float64
	CoordinatedMultiplier      float64
	FundingAgeMultiplier       float64
	FinalScore                 float64
	
	// Context for understanding the score
	WinRate                    float64
	ResolvedTrades             int
	FundingAgeHours            float64
	HoursToClose               float64
	LiquidityRatio             float64
	NetConcentration           float64
	VelocityCount              int
	ClusterID                  string
	IsCoordinated              bool
}

// AlertPayload contains all information for an alert
type AlertPayload struct {
	Severity        Severity
	WalletAddress   string
	WalletShort     string // Shortened for display
	MarketTitle     string
	MarketURL       string
	Side            string
	Outcome         string
	NotionalUSD     float64
	Price           float64
	WalletAgeDays   int
	FirstSeenDate   string
	SuspicionScore  float64
	ScoreBreakdown  *ScoreBreakdown // Calculation details
	TransactionHash string
	TxHashShort     string // Shortened for display
	Timestamp       time.Time
	Environment     string
}

// Sender defines the interface for alert senders
type Sender interface {
	Send(ctx context.Context, payload *AlertPayload) error
}
