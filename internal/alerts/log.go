package alerts

import (
	"context"
	"fmt"

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
	fields := logrus.Fields{
		"severity":         payload.Severity,
		"wallet":           payload.WalletShort,
		"market":           payload.MarketTitle,
		"notional_usd":     payload.NotionalUSD,
		"wallet_age_days":  payload.WalletAgeDays,
		"normalized_score": payload.NormalizedScore,
		"raw_score":        payload.SuspicionScore,
		"tx_hash":          payload.TxHashShort,
	}
	
	if payload.ScoreBreakdown != nil {
		fields["score_breakdown"] = s.formatScoreBreakdown(payload.ScoreBreakdown)
	}
	
	s.log.WithFields(fields).Info("Alert generated")
	return nil
}

func (s *LogSender) formatScoreBreakdown(b *ScoreBreakdown) string {
	breakdown := fmt.Sprintf("base=%.0f", b.BaseScore)
	
	if b.TimeToCloseMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", time_to_close=%.2fx(%.1fh)", b.TimeToCloseMultiplier, b.HoursToClose)
	}
	if b.WinRateMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", win_rate=%.2fx(%.0f%%, %dt)", b.WinRateMultiplier, b.WinRate*100, b.ResolvedTrades)
	}
	if b.FirstTradeLargeMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", first_large=%.1fx", b.FirstTradeLargeMultiplier)
	}
	if b.FlashFundingMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", flash_fund=%.1fx(%.1fm)", b.FlashFundingMultiplier, b.FundingAgeHours*60)
	}
	if b.LiquidityMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", liquidity=%.2fx(%.1f%%)", b.LiquidityMultiplier, b.LiquidityRatio*100)
	}
	if b.PriceConfidenceMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", extreme_price=%.1fx", b.PriceConfidenceMultiplier)
	}
	if b.ConcentrationMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", concentration=%.1fx(%.0f%%)", b.ConcentrationMultiplier, b.NetConcentration*100)
	}
	if b.VelocityMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", velocity=%.1fx(%dt)", b.VelocityMultiplier, b.VelocityCount)
	}
	if b.ClusterMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", cluster=%.1fx", b.ClusterMultiplier)
	}
	if b.CoordinatedMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", coordinated=%.1fx", b.CoordinatedMultiplier)
	}
	if b.FundingAgeMultiplier > 1.0 {
		breakdown += fmt.Sprintf(", fast_fund=%.2fx(%.1fh)", b.FundingAgeMultiplier, b.FundingAgeHours)
	}
	
	breakdown += fmt.Sprintf(" => final=%.0f", b.FinalScore)
	
	return breakdown
}
