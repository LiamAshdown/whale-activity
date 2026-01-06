package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DiscordSender sends alerts to Discord via webhook
type DiscordSender struct {
	webhookURL string
	httpClient *http.Client
}

// NewDiscordSender creates a new Discord sender
func NewDiscordSender(webhookURL string) *DiscordSender {
	return &DiscordSender{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send sends the alert to Discord
func (s *DiscordSender) Send(ctx context.Context, payload *AlertPayload) error {
	embed := s.buildEmbed(payload)
	
	webhookPayload := map[string]interface{}{
		"embeds": []interface{}{embed},
	}

	body, err := json.Marshal(webhookPayload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	return nil
}

func (s *DiscordSender) buildEmbed(payload *AlertPayload) map[string]interface{} {
	// Determine title and color
	var title string
	var color int
	switch payload.Severity {
	case SeverityAlert:
		title = "🚨 New wallet big bet (ALERT)"
		color = 0xFF0000 // Red
	case SeverityWarn:
		title = "⚠️ Suspicious big bet (WARN)"
		color = 0xFFA500 // Orange
	default:
		title = "ℹ️ Big trade detected"
		color = 0x0099FF // Blue
	}

	// Build description
	description := fmt.Sprintf("**$%.2f** on **%s** @ **%.2f**\nWallet age **%dd** (first seen %s)",
		payload.NotionalUSD,
		payload.Outcome,
		payload.Price,
		payload.WalletAgeDays,
		payload.FirstSeenDate,
	)

	// Build fields
	fields := []map[string]interface{}{
		{
			"name":   "Wallet",
			"value":  fmt.Sprintf("`%s`", payload.WalletShort),
			"inline": true,
		},
		{
			"name":   "Market",
			"value":  truncate(payload.MarketTitle, 100),
			"inline": true,
		},
		{
			"name":   "Side",
			"value":  fmt.Sprintf("%s %s", payload.Side, payload.Outcome),
			"inline": true,
		},
		{
			"name":   "Bet Total",
			"value":  fmt.Sprintf("$%.2f", payload.NotionalUSD),
			"inline": true,
		},
		{
			"name":   "Bet Price",
			"value":  fmt.Sprintf("%.2f", payload.Price),
			"inline": true,
		},
		{
			"name":   "Wallet Age",
			"value":  fmt.Sprintf("%d days", payload.WalletAgeDays),
			"inline": true,
		},
		{
			"name":   "Suspicion Score",
			"value":  fmt.Sprintf("**%.0f/100**", payload.NormalizedScore),
			"inline": true,
		},
		{
			"name":   "Tx",
			"value":  fmt.Sprintf("`%s`", payload.TxHashShort),
			"inline": true,
		},
	}

	// Add score breakdown if available
	if payload.ScoreBreakdown != nil {
		breakdownText := s.formatScoreBreakdown(payload.ScoreBreakdown)
		fields = append(fields, map[string]interface{}{
			"name":   "📊 Score Calculation",
			"value":  breakdownText,
			"inline": false,
		})
	}

	// Footer
	footer := map[string]interface{}{
		"text": fmt.Sprintf("Whale Activity • %s • %s", payload.Environment, payload.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC")),
	}

	embed := map[string]interface{}{
		"title":       title,
		"url":         payload.MarketURL,
		"description": description,
		"color":       color,
		"fields":      fields,
		"footer":      footer,
		"timestamp":   payload.Timestamp.Format(time.RFC3339),
	}

	return embed
}

func (s *DiscordSender) formatScoreBreakdown(b *ScoreBreakdown) string {
	var parts []string
	
	parts = append(parts, fmt.Sprintf("Base Score: %.0f", b.BaseScore))	
	if b.TimeToCloseMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("⏰ Market closes soon (%.1fh) - timing matters: **%.2fx**", b.HoursToClose, b.TimeToCloseMultiplier))
	}
	if b.WinRateMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("🎯 Proven track record (%.0f%% wins, %d trades): **%.2fx**", b.WinRate*100, b.ResolvedTrades, b.WinRateMultiplier))
	}
	if b.FirstTradeLargeMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("🆕 First trade is a big one - unusual confidence: **%.1fx**", b.FirstTradeLargeMultiplier))
	}
	if b.FlashFundingMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("⚡ Wallet funded & traded immediately (%.1fm ago): **%.1fx**", b.FundingAgeHours*60, b.FlashFundingMultiplier))
	}
	if b.LiquidityMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("💧 Large bet vs available liquidity (%.1f%%): **%.2fx**", b.LiquidityRatio*100, b.LiquidityMultiplier))
	}
	if b.PriceConfidenceMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("💪 Betting on extreme odds - high conviction: **%.1fx**", b.PriceConfidenceMultiplier))
	}
	if b.ConcentrationMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("📈 Heavily one-sided betting (%.0f%% concentration): **%.1fx**", b.NetConcentration*100, b.ConcentrationMultiplier))
	}
	if b.VelocityMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("🚀 Rapid-fire trading (%d trades in short time): **%.1fx**", b.VelocityCount, b.VelocityMultiplier))
	}
	if b.ClusterMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("👥 Part of connected wallet group: **%.1fx**", b.ClusterMultiplier))
	}
	if b.CoordinatedMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("🤝 Coordinated activity with other wallets: **%.1fx**", b.CoordinatedMultiplier))
	}
	if b.FundingAgeMultiplier > 1.0 {
		parts = append(parts, fmt.Sprintf("⏱️ Very new wallet (funded %.1fh ago): **%.2fx**", b.FundingAgeHours, b.FundingAgeMultiplier))
	}
	
	if len(parts) > 1 {
		parts = append(parts, fmt.Sprintf("\n🎯 Final Suspicion Score: **%.0f/100** (raw: %.0f)", b.NormalizedScore, b.FinalScore))
	}
	
	return truncate(joinParts(parts), 1000)
}

func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n"
		}
		result += p
	}
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
