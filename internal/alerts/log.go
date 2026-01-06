package alerts

import (
	"context"

	"github.com/sirupsen/logrus"
)

// LogSender sends alerts to the logger
type LogSender struct {
	log *logrus.Logger
}

// NewLogSender creates a new log sender
func NewLogSender(log *logrus.Logger) *LogSender {
	return &LogSender{log: log}
}

// Send logs the alert
func (s *LogSender) Send(ctx context.Context, payload *AlertPayload) error {
	s.log.WithFields(logrus.Fields{
		"severity":        payload.Severity,
		"wallet":          payload.WalletShort,
		"market":          payload.MarketTitle,
		"notional_usd":    payload.NotionalUSD,
		"wallet_age_days": payload.WalletAgeDays,
		"suspicion_score": payload.SuspicionScore,
		"tx_hash":         payload.TxHashShort,
	}).Info("Alert generated")
	return nil
}
