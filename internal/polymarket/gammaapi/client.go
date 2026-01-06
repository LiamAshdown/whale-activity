package gammaapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/liamashdown/insiderwatch/internal/config"
	"github.com/liamashdown/insiderwatch/internal/ratelimit"
)

// Client handles communication with the Polymarket Gamma API
type Client struct {
	baseURL    string
	httpClient *http.Client
	limiter    *ratelimit.Limiter
}

// NewClient creates a new Gamma API client
func NewClient(cfg *config.Config) *Client {
	return &Client{
		baseURL:    cfg.GammaAPIBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		limiter:    ratelimit.New(cfg.GammaAPIMarketsRPS),
	}
}

// GetMarketByConditionID fetches market details by condition ID
func (c *Client) GetMarketByConditionID(ctx context.Context, conditionID string) (*Market, error) {
	// Rate limit
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	u, err := url.Parse(c.baseURL + "/markets")
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	q.Set("condition_ids", conditionID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Gamma API is public - no auth headers needed per spec
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// Response can be either array or single market
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Try array first
	var markets []Market
	if err := json.Unmarshal(body, &markets); err == nil {
		if len(markets) > 0 {
			return &markets[0], nil
		}
		return nil, fmt.Errorf("no market found for condition_id %s", conditionID)
	}

	// Try single market
	var market Market
	if err := json.Unmarshal(body, &market); err == nil {
		return &market, nil
	}

	return nil, fmt.Errorf("failed to decode market response")
}

// GetMarketBySlug fetches market details by slug
func (c *Client) GetMarketBySlug(ctx context.Context, slug string) (*Market, error) {
	// Rate limit
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	u := c.baseURL + "/markets/slug/" + url.PathEscape(slug)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var market Market
	if err := json.NewDecoder(resp.Body).Decode(&market); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &market, nil
}

// GetMarketByID fetches market details by ID
func (c *Client) GetMarketByID(ctx context.Context, id string) (*Market, error) {
	// Rate limit
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	u := c.baseURL + "/markets/" + url.PathEscape(id)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var market Market
	if err := json.NewDecoder(resp.Body).Decode(&market); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &market, nil
}
