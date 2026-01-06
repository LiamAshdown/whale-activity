package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/liamashdown/insiderwatch/internal/secrets"
)

// AuthMode represents the authentication mode for Data API
type AuthMode string

const (
	AuthModeNone   AuthMode = "none"
	AuthModeBearer AuthMode = "bearer"
	AuthModeAPIKey AuthMode = "api_key"
)

// Config holds all application configuration
type Config struct {
	// Environment
	Environment string

	// Database
	DatabaseDSN         string
	DatabaseMaxConns    int
	DatabaseMaxIdleTime time.Duration

	// Data API
	DataAPIBaseURL      string
	DataAPIAuthMode     AuthMode
	DataAPIBearerToken  string
	DataAPIAPIKey       string
	DataAPIExtraHeaders map[string]string

	// Gamma API
	GammaAPIBaseURL string

	// Detection thresholds
	BigTradeUSD          float64 // Minimum to fetch from API
	MinTradeUSD          float64 // Minimum to process and alert
	NewWalletDaysMax     int
	SuspicionScoreWarn   float64
	SuspicionScoreAlert  float64
	NetPositionWindowHrs int
	AlertCooldownMins    int
	TimeToCloseHoursMax  int     // Hours before market close to flag trades
	MinWinRateThreshold  float64 // Win rate threshold (0.0-1.0) to flag wallets

	// Rate limits (requests per second)
	DataAPITradesRPS   float64
	DataAPIActivityRPS float64
	GammaAPIMarketsRPS float64

	// Worker pool
	WalletLookupWorkers int

	// Polling
	PollIntervalSec int

	// Alerts
	AlertMode     string // log, discord, smtp, multi
	DiscordWebURL string
	SMTPHost      string
	SMTPPort      int
	SMTPUser      string
	SMTPPassword  string
	SMTPFrom      string
	SMTPTo        []string

	// Metrics/Health
	MetricsPort int
	HealthPort  int
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Environment:          getEnv("ENVIRONMENT", "production"),
		DatabaseDSN:          getEnv("DATABASE_DSN", "insiderwatch:insiderwatch@tcp(mysql:3306)/insiderwatch?parseTime=true"),
		DatabaseMaxConns:     getEnvInt("DATABASE_MAX_CONNS", 25),
		DatabaseMaxIdleTime:  time.Duration(getEnvInt("DATABASE_MAX_IDLE_TIME_MINS", 5)) * time.Minute,
		DataAPIBaseURL:       getEnv("DATA_API_BASE_URL", "https://data-api.polymarket.com"),
		DataAPIAuthMode:      AuthMode(getEnv("DATA_API_AUTH_MODE", "none")),
		DataAPIBearerToken:   secrets.GetOptionalSecret("DATA_API_BEARER_TOKEN", ""),
		DataAPIAPIKey:        secrets.GetOptionalSecret("DATA_API_API_KEY", ""),
		GammaAPIBaseURL:      getEnv("GAMMA_API_BASE_URL", "https://gamma-api.polymarket.com"),
		BigTradeUSD:          getEnvFloat("BIG_TRADE_USD", 10000.0),
		MinTradeUSD:          getEnvFloat("MIN_TRADE_USD", 5000.0),
		NewWalletDaysMax:     getEnvInt("NEW_WALLET_DAYS_MAX", 7),
		SuspicionScoreWarn:   getEnvFloat("SUSPICION_SCORE_WARN", 5000.0),
		SuspicionScoreAlert:  getEnvFloat("SUSPICION_SCORE_ALERT", 10000.0),
		NetPositionWindowHrs: getEnvInt("NET_POSITION_WINDOW_HRS", 24),
		AlertCooldownMins:    getEnvInt("ALERT_COOLDOWN_MINS", 60),
		TimeToCloseHoursMax:  getEnvInt("TIME_TO_CLOSE_HOURS_MAX", 48),
		MinWinRateThreshold:  getEnvFloat("MIN_WIN_RATE_THRESHOLD", 0.75),
		DataAPITradesRPS:     getEnvFloat("DATA_API_TRADES_RPS", 2.0),
		DataAPIActivityRPS:   getEnvFloat("DATA_API_ACTIVITY_RPS", 1.0),
		GammaAPIMarketsRPS:   getEnvFloat("GAMMA_API_MARKETS_RPS", 5.0),
		WalletLookupWorkers:  getEnvInt("WALLET_LOOKUP_WORKERS", 5),
		PollIntervalSec:      getEnvInt("POLL_INTERVAL_SEC", 30),
		AlertMode:            getEnv("ALERT_MODE", "log"),
		DiscordWebURL:        secrets.GetOptionalSecret("DISCORD_WEBHOOK_URL", ""),
		SMTPHost:             getEnv("SMTP_HOST", ""),
		SMTPPort:             getEnvInt("SMTP_PORT", 587),
		SMTPUser:             getEnv("SMTP_USER", ""),
		SMTPPassword:         secrets.GetOptionalSecret("SMTP_PASSWORD", ""),
		SMTPFrom:             getEnv("SMTP_FROM", "insiderwatch@example.com"),
		MetricsPort:          getEnvInt("METRICS_PORT", 9090),
		HealthPort:           getEnvInt("HEALTH_PORT", 8080),
	}

	// Parse SMTP_TO (comma-separated)
	smtpTo := getEnv("SMTP_TO", "")
	if smtpTo != "" {
		cfg.SMTPTo = parseCSV(smtpTo)
	}

	// Parse extra headers JSON
	extraHeadersJSON := getEnv("DATA_API_EXTRA_HEADERS", "{}")
	if err := json.Unmarshal([]byte(extraHeadersJSON), &cfg.DataAPIExtraHeaders); err != nil {
		return nil, fmt.Errorf("invalid DATA_API_EXTRA_HEADERS JSON: %w", err)
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks configuration for errors
func (c *Config) Validate() error {
	if c.DatabaseDSN == "" {
		return fmt.Errorf("DATABASE_DSN is required")
	}

	// Validate auth mode
	switch c.DataAPIAuthMode {
	case AuthModeNone:
		// No validation needed
	case AuthModeBearer:
		if c.DataAPIBearerToken == "" {
			return fmt.Errorf("DATA_API_BEARER_TOKEN is required when AUTH_MODE is bearer")
		}
	case AuthModeAPIKey:
		if c.DataAPIAPIKey == "" {
			return fmt.Errorf("DATA_API_API_KEY is required when AUTH_MODE is api_key")
		}
	default:
		return fmt.Errorf("invalid DATA_API_AUTH_MODE: %s (must be none, bearer, or api_key)", c.DataAPIAuthMode)
	}

	// Validate alert mode (comma-separated list)
	modes := strings.Split(c.AlertMode, ",")
	hasDiscord := false
	hasSMTP := false
	
	for _, mode := range modes {
		mode = strings.TrimSpace(mode)
		switch mode {
		case "log", "discord", "smtp":
			if mode == "discord" {
				hasDiscord = true
			}
			if mode == "smtp" {
				hasSMTP = true
			}
		default:
			return fmt.Errorf("invalid ALERT_MODE value: %s (valid values: log, discord, smtp)", mode)
		}
	}

	if hasDiscord && c.DiscordWebURL == "" {
		return fmt.Errorf("DISCORD_WEBHOOK_URL is required when discord is in ALERT_MODE")
	}

	if hasSMTP && c.SMTPHost == "" {
		return fmt.Errorf("SMTP_HOST is required when smtp is in ALERT_MODE")
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			return floatVal
		}
	}
	return defaultValue
}

func parseCSV(s string) []string {
	var result []string
	for _, item := range splitCSV(s) {
		if trimmed := trim(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitCSV(s string) []string {
	var result []string
	var current string
	for _, char := range s {
		if char == ',' {
			result = append(result, current)
			current = ""
		} else {
			current += string(char)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}
