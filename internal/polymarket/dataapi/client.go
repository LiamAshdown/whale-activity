package dataapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/liamashdown/insiderwatch/internal/config"
	"github.com/liamashdown/insiderwatch/internal/ratelimit"
)

// Client handles communication with the Polymarket Data API
type Client struct {
	baseURL      string
	httpClient   *http.Client
	authMode     config.AuthMode
	bearerToken  string
	apiKey       string
	extraHeaders map[string]string
	tradesLimiter   *ratelimit.Limiter
	activityLimiter *ratelimit.Limiter
}

// NewClient creates a new Data API client
func NewClient(cfg *config.Config) *Client {
	return &Client{
		baseURL:      cfg.DataAPIBaseURL,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		authMode:     cfg.DataAPIAuthMode,
		bearerToken:  cfg.DataAPIBearerToken,
		apiKey:       cfg.DataAPIAPIKey,
		extraHeaders: cfg.DataAPIExtraHeaders,
		tradesLimiter:   ratelimit.New(cfg.DataAPITradesRPS),
		activityLimiter: ratelimit.New(cfg.DataAPIActivityRPS),
	}
}

// GetTrades fetches trades from the Data API with BIG_TRADE_USD filter
func (c *Client) GetTrades(ctx context.Context, params TradeParams) (*TradesResponse, error) {
	// Rate limit
	if err := c.tradesLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	u, err := url.Parse(c.baseURL + "/trades")
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		q.Set("offset", strconv.Itoa(params.Offset))
	}
	if params.TakerOnly {
		q.Set("takerOnly", "true")
	}
	if params.FilterType != "" {
		q.Set("filterType", params.FilterType)
	}
	if params.FilterAmount > 0 {
		q.Set("filterAmount", strconv.FormatFloat(params.FilterAmount, 'f', 2, 64))
	}
	if params.Market != "" {
		q.Set("market", params.Market)
	}
	if params.EventID != "" {
		q.Set("eventId", params.EventID)
	}
	if params.User != "" {
		q.Set("user", params.User)
	}
	if params.Side != "" {
		q.Set("side", params.Side)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("401 Unauthorized (auth_mode=%s) - check credentials", c.authMode)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// Try to decode as array first (actual API response)
	var trades []Trade
	if err := json.NewDecoder(resp.Body).Decode(&trades); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &TradesResponse{Trades: trades, Count: len(trades)}, nil
}

// GetWalletFirstActivity fetches the earliest activity for a wallet
func (c *Client) GetWalletFirstActivity(ctx context.Context, wallet string) (*ActivityEvent, error) {
	// Rate limit
	if err := c.activityLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	u, err := url.Parse(c.baseURL + "/activity")
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	q.Set("user", wallet)
	q.Set("sortBy", "timestamp")
	q.Set("sortDirection", "ASC")
	q.Set("limit", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("401 Unauthorized (auth_mode=%s) - check credentials", c.authMode)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// Decode as array directly
	var activities []ActivityEvent
	if err := json.NewDecoder(resp.Body).Decode(&activities); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(activities) == 0 {
		return nil, fmt.Errorf("no activity found for wallet %s", wallet)
	}

	return &activities[0], nil
}

func (c *Client) setAuthHeaders(req *http.Request) {
	switch c.authMode {
	case config.AuthModeBearer:
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	case config.AuthModeAPIKey:
		req.Header.Set("X-API-KEY", c.apiKey)
	case config.AuthModeNone:
		// No auth headers
	}

	// Add extra headers
	for k, v := range c.extraHeaders {
		req.Header.Set(k, v)
	}
}

// TradeParams holds parameters for the GetTrades call
type TradeParams struct {
	Limit        int
	Offset       int
	TakerOnly    bool
	FilterType   string  // CASH
	FilterAmount float64 // BIG_TRADE_USD
	Market       string
	EventID      string
	User         string
	Side         string // BUY, SELL
}
