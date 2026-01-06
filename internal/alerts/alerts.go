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
	TransactionHash string
	TxHashShort     string // Shortened for display
	Timestamp       time.Time
	Environment     string
}

// Sender defines the interface for alert senders
type Sender interface {
	Send(ctx context.Context, payload *AlertPayload) error
}
