package alerts

import (
	"context"
	"fmt"
	"net/smtp"
	"time"
)

// SMTPSender sends alerts via email
type SMTPSender struct {
	host     string
	port     int
	user     string
	password string
	from     string
	to       []string
}

// NewSMTPSender creates a new SMTP sender
func NewSMTPSender(host string, port int, user, password, from string, to []string) *SMTPSender {
	return &SMTPSender{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		from:     from,
		to:       to,
	}
}

// Send sends the alert via email
func (s *SMTPSender) Send(ctx context.Context, payload *AlertPayload) error {
	subject := fmt.Sprintf("[%s] Suspicious trade: $%.2f on %s", payload.Severity, payload.NotionalUSD, payload.MarketTitle)
	body := s.buildEmailBody(payload)

	message := fmt.Sprintf("From: %s\r\n", s.from)
	message += fmt.Sprintf("To: %s\r\n", s.to[0])
	message += fmt.Sprintf("Subject: %s\r\n", subject)
	message += "Content-Type: text/plain; charset=UTF-8\r\n"
	message += "\r\n"
	message += body

	auth := smtp.PlainAuth("", s.user, s.password, s.host)
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	err := smtp.SendMail(addr, auth, s.from, s.to, []byte(message))
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}

func (s *SMTPSender) buildEmailBody(payload *AlertPayload) string {
	body := fmt.Sprintf("INSIDERWATCH ALERT - %s\n", payload.Severity)
	body += fmt.Sprintf("═══════════════════════════════════════\n\n")
	body += fmt.Sprintf("A suspicious trade has been detected:\n\n")
	body += fmt.Sprintf("TRADE DETAILS\n")
	body += fmt.Sprintf("─────────────────────────────────────\n")
	body += fmt.Sprintf("Notional:       $%.2f\n", payload.NotionalUSD)
	body += fmt.Sprintf("Side:           %s %s\n", payload.Side, payload.Outcome)
	body += fmt.Sprintf("Price:          %.2f\n", payload.Price)
	body += fmt.Sprintf("Market:         %s\n", payload.MarketTitle)
	body += fmt.Sprintf("Market URL:     %s\n\n", payload.MarketURL)
	body += fmt.Sprintf("WALLET DETAILS\n")
	body += fmt.Sprintf("─────────────────────────────────────\n")
	body += fmt.Sprintf("Address:        %s\n", payload.WalletAddress)
	body += fmt.Sprintf("Age:            %d days (first seen %s)\n", payload.WalletAgeDays, payload.FirstSeenDate)
	body += fmt.Sprintf("Suspicion Score: %.2f\n\n", payload.SuspicionScore)
	
	// Add score breakdown if available
	if payload.ScoreBreakdown != nil {
		body += s.formatScoreBreakdown(payload.ScoreBreakdown)
	}
	
	body += fmt.Sprintf("TRANSACTION\n")
	body += fmt.Sprintf("─────────────────────────────────────\n")
	body += fmt.Sprintf("Hash:           %s\n", payload.TransactionHash)
	body += fmt.Sprintf("Time:           %s\n\n", payload.Timestamp.Format(time.RFC3339))
	body += fmt.Sprintf("═══════════════════════════════════════\n")
	body += fmt.Sprintf("Environment: %s\n", payload.Environment)
	body += fmt.Sprintf("Generated: %s\n", time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))
	body += fmt.Sprintf("\nNote: This system detects suspicious behavior;\n")
	body += fmt.Sprintf("it does NOT prove insider trading.\n")

	return body
}

func (s *SMTPSender) formatScoreBreakdown(b *ScoreBreakdown) string {
	breakdown := fmt.Sprintf("SCORE CALCULATION\n")
	breakdown += fmt.Sprintf("─────────────────────────────────────\n")
	breakdown += fmt.Sprintf("Base Score:     %.0f\n", b.BaseScore)
	
	if b.TimeToCloseMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Time to Close:  %.2fx (%.1f hours)\n", b.TimeToCloseMultiplier, b.HoursToClose)
	}
	if b.WinRateMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Win Rate:       %.2fx (%.0f%%, %d trades)\n", b.WinRateMultiplier, b.WinRate*100, b.ResolvedTrades)
	}
	if b.FirstTradeLargeMultiplier > 1.0 {
		breakdown += fmt.Sprintf("First Large:    %.1fx\n", b.FirstTradeLargeMultiplier)
	}
	if b.FlashFundingMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Flash Funding:  %.1fx (%.1f minutes)\n", b.FlashFundingMultiplier, b.FundingAgeHours*60)
	}
	if b.LiquidityMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Liquidity:      %.2fx (%.1f%% of pool)\n", b.LiquidityMultiplier, b.LiquidityRatio*100)
	}
	if b.PriceConfidenceMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Extreme Price:  %.1fx\n", b.PriceConfidenceMultiplier)
	}
	if b.ConcentrationMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Concentration:  %.1fx (%.0f%% one-sided)\n", b.ConcentrationMultiplier, b.NetConcentration*100)
	}
	if b.VelocityMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Velocity:       %.1fx (%d trades)\n", b.VelocityMultiplier, b.VelocityCount)
	}
	if b.ClusterMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Cluster:        %.1fx\n", b.ClusterMultiplier)
	}
	if b.CoordinatedMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Coordinated:    %.1fx\n", b.CoordinatedMultiplier)
	}
	if b.FundingAgeMultiplier > 1.0 {
		breakdown += fmt.Sprintf("Fast Funding:   %.2fx (%.1f hours)\n", b.FundingAgeMultiplier, b.FundingAgeHours)
	}
	
	breakdown += fmt.Sprintf("\nFinal Score:    %.0f\n\n", b.FinalScore)
	
	return breakdown
}
