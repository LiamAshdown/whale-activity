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
			"value":  fmt.Sprintf("%.2f", payload.SuspicionScore),
			"inline": true,
		},
		{
			"name":   "Tx",
			"value":  fmt.Sprintf("`%s`", payload.TxHashShort),
			"inline": true,
		},
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
