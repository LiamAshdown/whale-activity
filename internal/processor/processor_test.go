package processor

import (
	"testing"

	"github.com/liamashdown/insiderwatch/internal/alerts"
	"github.com/liamashdown/insiderwatch/internal/config"
	"github.com/sirupsen/logrus"
)

func TestCalculateSuspicionScore(t *testing.T) {
	cfg := &config.Config{
		TimeToCloseHoursMax: 48,
	}
	log := logrus.New()
	p := &Processor{cfg: cfg, log: log}

	tests := []struct {
		name          string
		notional      float64
		walletAgeDays int
		hoursToClose  float64
		expectedScore float64
		description   string
	}{
		{
			name:          "basic calculation no multiplier",
			notional:      50000,
			walletAgeDays: 2,
			hoursToClose:  100, // Beyond 48h max
			expectedScore: 25000,
			description:   "50000 / 2 = 25000",
		},
		{
			name:          "zero day wallet protected",
			notional:      50000,
			walletAgeDays: 0,
			hoursToClose:  100,
			expectedScore: 50000,
			description:   "Division by zero protected with max(days, 1)",
		},
		{
			name:          "1 hour before close - maximum multiplier",
			notional:      50000,
			walletAgeDays: 2,
			hoursToClose:  1,
			expectedScore: 122916.67, // 25000 * (1 + (48-1)/48*4) = 25000 * 4.9166...
			description:   "Base 25000 * 4.9166... multiplier",
		},
		{
			name:          "24 hours before close",
			notional:      50000,
			walletAgeDays: 2,
			hoursToClose:  24,
			expectedScore: 75000,
			description:   "Base 25000 * (1 + (48-24)/48*4) = 25000 * 3.0",
		},
		{
			name:          "48 hours before close - minimum multiplier",
			notional:      50000,
			walletAgeDays: 2,
			hoursToClose:  48,
			expectedScore: 25000,
			description:   "Base 25000 * (1 + (48-48)/48*4) = 25000 * 1.0",
		},
		{
			name:          "12 hours before close",
			notional:      50000,
			walletAgeDays: 2,
			hoursToClose:  12,
			expectedScore: 100000,
			description:   "Base 25000 * (1 + (48-12)/48*4) = 25000 * 4.0",
		},
		{
			name:          "negative hours to close",
			notional:      50000,
			walletAgeDays: 2,
			hoursToClose:  -10,
			expectedScore: 25000,
			description:   "Negative hours should not apply multiplier",
		},
		{
			name:          "small trade old wallet",
			notional:      1000,
			walletAgeDays: 30,
			hoursToClose:  100,
			expectedScore: 33.33,
			description:   "1000 / 30 = 33.33",
		},
		{
			name:          "large trade new wallet last minute",
			notional:      100000,
			walletAgeDays: 1,
			hoursToClose:  0.5, // 30 minutes
			expectedScore: 495833.33, // 100000 * (1 + (48-0.5)/48*4) = 100000 * 4.958333
			description:   "Maximum suspicion scenario",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := p.calculateSuspicionScore(tt.notional, tt.walletAgeDays, tt.hoursToClose)
			
			// Allow 0.1% tolerance for floating point comparison
			tolerance := tt.expectedScore * 0.001
			if tolerance < 0.01 {
				tolerance = 0.01
			}
			
			diff := score - tt.expectedScore
			if diff < 0 {
				diff = -diff
			}
			
			if diff > tolerance {
				t.Errorf("%s: got %.2f, want %.2f (diff: %.2f)\nDescription: %s",
					tt.name, score, tt.expectedScore, diff, tt.description)
			}
		})
	}
}

func TestDetermineSeverity(t *testing.T) {
	cfg := &config.Config{
		SuspicionScoreAlert: 25000,
		SuspicionScoreWarn:  10000,
	}
	log := logrus.New()
	p := &Processor{cfg: cfg, log: log}

	tests := []struct {
		name             string
		score            float64
		expectedSeverity alerts.Severity
	}{
		{"critical threshold exact", 25000, alerts.SeverityAlert},
		{"critical above threshold", 30000, alerts.SeverityAlert},
		{"high threshold exact", 10000, alerts.SeverityWarn},
		{"high above threshold", 15000, alerts.SeverityWarn},
		{"info below thresholds", 5000, alerts.SeverityInfo},
		{"info very low", 100, alerts.SeverityInfo},
		{"info zero", 0, alerts.SeverityInfo},
		{"just below critical", 24999, alerts.SeverityWarn},
		{"just below high", 9999, alerts.SeverityInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := p.determineSeverity(tt.score)
			if severity != tt.expectedSeverity {
				t.Errorf("score %.0f: got %s, want %s",
					tt.score, severity, tt.expectedSeverity)
			}
		})
	}
}

func TestDetermineWinner(t *testing.T) {
	cfg := &config.Config{}
	log := logrus.New()
	p := &Processor{cfg: cfg, log: log}

	tests := []struct {
		name           string
		outcomes       string
		outcomePrices  string
		expectedWinner string
		description    string
	}{
		{
			name:           "clear YES winner",
			outcomes:       "YES,NO",
			outcomePrices:  "0.98,0.02",
			expectedWinner: "YES",
			description:    "YES at 98% is clear winner",
		},
		{
			name:           "clear NO winner",
			outcomes:       "YES,NO",
			outcomePrices:  "0.02,0.98",
			expectedWinner: "NO",
			description:    "NO at 98% is clear winner",
		},
		{
			name:           "exact 95% threshold",
			outcomes:       "YES,NO",
			outcomePrices:  "0.95,0.05",
			expectedWinner: "YES",
			description:    "95% exactly meets threshold",
		},
		{
			name:           "just below threshold",
			outcomes:       "YES,NO",
			outcomePrices:  "0.94,0.06",
			expectedWinner: "",
			description:    "94% does not meet 95% threshold",
		},
		{
			name:           "multi-outcome with winner",
			outcomes:       "Donald Trump,Kamala Harris,Other",
			outcomePrices:  "0.96,0.03,0.01",
			expectedWinner: "Donald Trump",
			description:    "First outcome wins in 3-way market",
		},
		{
			name:           "multi-outcome no clear winner",
			outcomes:       "A,B,C",
			outcomePrices:  "0.50,0.30,0.20",
			expectedWinner: "",
			description:    "No outcome reaches 95%",
		},
		{
			name:           "empty outcomes",
			outcomes:       "",
			outcomePrices:  "0.98,0.02",
			expectedWinner: "",
			description:    "Empty outcomes string",
		},
		{
			name:           "empty prices",
			outcomes:       "YES,NO",
			outcomePrices:  "",
			expectedWinner: "",
			description:    "Empty prices string",
		},
		{
			name:           "mismatched lengths",
			outcomes:       "YES,NO,MAYBE",
			outcomePrices:  "0.50,0.50",
			expectedWinner: "",
			description:    "3 outcomes but only 2 prices",
		},
		{
			name:           "invalid price format",
			outcomes:       "YES,NO",
			outcomePrices:  "invalid,0.98",
			expectedWinner: "NO",
			description:    "Skips invalid price, finds valid winner",
		},
		{
			name:           "whitespace handling",
			outcomes:       " YES , NO ",
			outcomePrices:  " 0.98 , 0.02 ",
			expectedWinner: "YES",
			description:    "Trims whitespace from outcomes",
		},
		{
			name:           "100% certainty",
			outcomes:       "YES,NO",
			outcomePrices:  "1.0,0.0",
			expectedWinner: "YES",
			description:    "100% price is valid winner",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			winner := p.determineWinner(tt.outcomes, tt.outcomePrices)
			if winner != tt.expectedWinner {
				t.Errorf("got '%s', want '%s'\nDescription: %s",
					winner, tt.expectedWinner, tt.description)
			}
		})
	}
}

func TestIsNotInsiderCategory(t *testing.T) {
	tests := []struct {
		name     string
		category string
		expected bool
	}{
		{"empty category", "", false},
		{"sports exact", "sports", true},
		{"sports uppercase", "SPORTS", true},
		{"sports in phrase", "Professional Sports", true},
		{"nfl", "NFL", true},
		{"nba", "NBA", true},
		{"mlb", "MLB", true},
		{"soccer lowercase", "soccer", true},
		{"football", "football", true},
		{"basketball", "basketball", true},
		{"ufc", "UFC", true},
		{"politics", "politics", false},
		{"crypto", "crypto", false},
		{"business", "business", false},
		{"elections", "elections", false},
		{"science", "science", false},
		{"sport singular not matched", "sport", false}, // Only "sports" plural matches
		{"contains nba", "NBA Playoffs", true},
		{"contains politics no match", "US Politics", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNotInsiderCategory(tt.category)
			if result != tt.expected {
				t.Errorf("category '%s': got %v, want %v", tt.category, result, tt.expected)
			}
		})
	}
}

func TestCalculateFundingAgeMultiplier(t *testing.T) {
	// This tests the funding age logic that appears in processTrade
	// Testing the multiplier calculation: 1.0 + (24-hours)/24*1.5
	
	tests := []struct {
		name               string
		fundingAgeHours    float64
		expectedMultiplier float64
		description        string
	}{
		{
			name:               "1 hour - maximum multiplier",
			fundingAgeHours:    1,
			expectedMultiplier: 2.4375,
			description:        "1 + (24-1)/24*1.5 = 1 + 1.4375 = 2.4375",
		},
		{
			name:               "12 hours - medium multiplier",
			fundingAgeHours:    12,
			expectedMultiplier: 1.75,
			description:        "1 + (24-12)/24*1.5 = 1 + 0.75 = 1.75",
		},
		{
			name:               "24 hours - no multiplier",
			fundingAgeHours:    24,
			expectedMultiplier: 1.0,
			description:        "1 + (24-24)/24*1.5 = 1 + 0 = 1.0",
		},
		{
			name:               "0.5 hours (30 min) - nearly maximum",
			fundingAgeHours:    0.5,
			expectedMultiplier: 2.46875,
			description:        "1 + (24-0.5)/24*1.5 = 1 + 1.46875 = 2.46875",
		},
		{
			name:               "6 hours",
			fundingAgeHours:    6,
			expectedMultiplier: 2.125,
			description:        "1 + (24-6)/24*1.5 = 1 + 1.125 = 2.125",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the logic from processTrade
			multiplier := 1.0 + (24.0-tt.fundingAgeHours)/24.0*1.5
			
			tolerance := 0.0001
			diff := multiplier - tt.expectedMultiplier
			if diff < 0 {
				diff = -diff
			}
			
			if diff > tolerance {
				t.Errorf("funding age %.1f hours: got %.5f, want %.5f\nDescription: %s",
					tt.fundingAgeHours, multiplier, tt.expectedMultiplier, tt.description)
			}
		})
	}
}

func TestWinRateMultiplier(t *testing.T) {
	// Tests the win rate multiplier logic: adjustedScore *= (1.0 + winRate)
	cfg := &config.Config{
		MinWinRateThreshold: 0.75,
	}

	tests := []struct {
		name               string
		baseScore          float64
		winRate            float64
		shouldApply        bool
		expectedScore      float64
		description        string
	}{
		{
			name:          "75% win rate - threshold exact",
			baseScore:     10000,
			winRate:       0.75,
			shouldApply:   true,
			expectedScore: 17500,
			description:   "10000 * (1 + 0.75) = 17500",
		},
		{
			name:          "80% win rate",
			baseScore:     10000,
			winRate:       0.80,
			shouldApply:   true,
			expectedScore: 18000,
			description:   "10000 * (1 + 0.80) = 18000",
		},
		{
			name:          "90% win rate - highly suspicious",
			baseScore:     10000,
			winRate:       0.90,
			shouldApply:   true,
			expectedScore: 19000,
			description:   "10000 * (1 + 0.90) = 19000",
		},
		{
			name:          "74% win rate - below threshold",
			baseScore:     10000,
			winRate:       0.74,
			shouldApply:   false,
			expectedScore: 10000,
			description:   "Below 75% threshold, no multiplier applied",
		},
		{
			name:          "50% win rate - no multiplier",
			baseScore:     10000,
			winRate:       0.50,
			shouldApply:   false,
			expectedScore: 10000,
			description:   "Average win rate, no multiplier",
		},
		{
			name:          "100% win rate - maximum",
			baseScore:     10000,
			winRate:       1.0,
			shouldApply:   true,
			expectedScore: 20000,
			description:   "10000 * (1 + 1.0) = 20000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adjustedScore := tt.baseScore
			if tt.winRate >= cfg.MinWinRateThreshold {
				adjustedScore *= (1.0 + tt.winRate)
			}

			tolerance := 0.01
			diff := adjustedScore - tt.expectedScore
			if diff < 0 {
				diff = -diff
			}

			if diff > tolerance {
				t.Errorf("got %.2f, want %.2f\nDescription: %s",
					adjustedScore, tt.expectedScore, tt.description)
			}
		})
	}
}

func TestCombinedMultipliers(t *testing.T) {
	// Test realistic scenarios with all multipliers combined
	tests := []struct {
		name               string
		notional           float64
		walletAgeDays      int
		hoursToClose       float64
		fundingAgeHours    float64
		winRate            float64
		minWinRateThreshold float64
		expectedScore      float64
		description        string
	}{
		{
			name:               "worst case insider: new wallet, last minute, quick funding, high win rate",
			notional:           50000,
			walletAgeDays:      1,
			hoursToClose:       1,
			fundingAgeHours:    1,
			winRate:            0.85,
			minWinRateThreshold: 0.75,
			expectedScore:      1108554.69, // 50000 * 4.9166 * 2.4375 * 1.85
			description:        "Maximum suspicion with all factors",
		},
		{
			name:               "moderate case: older wallet, some time left",
			notional:           25000,
			walletAgeDays:      3,
			hoursToClose:       24,
			fundingAgeHours:    12,
			winRate:            0.60, // Below threshold
			minWinRateThreshold: 0.75,
			expectedScore:      43750, // (25000/3) * 3.0 * 1.75 * 1.0 = 43750
			description:        "Moderate suspicion, no win rate boost",
		},
		{
			name:               "low suspicion: old wallet, far from close",
			notional:           10000,
			walletAgeDays:      30,
			hoursToClose:       100, // No multiplier
			fundingAgeHours:    48,  // No multiplier
			winRate:            0.50,
			minWinRateThreshold: 0.75,
			expectedScore:      333.33, // 10000/30 = 333.33
			description:        "No multipliers applied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate base score with time multiplier
			baseScore := tt.notional / float64(max(tt.walletAgeDays, 1))
			
			// Time to close multiplier
			if tt.hoursToClose > 0 && tt.hoursToClose <= 48 {
				multiplier := 1.0 + (48-tt.hoursToClose)/48*4.0
				baseScore *= multiplier
			}

			// Funding age multiplier
			if tt.fundingAgeHours > 0 && tt.fundingAgeHours <= 24 {
				fundingMultiplier := 1.0 + (24.0-tt.fundingAgeHours)/24.0*1.5
				baseScore *= fundingMultiplier
			}

			// Win rate multiplier
			if tt.winRate >= tt.minWinRateThreshold {
				baseScore *= (1.0 + tt.winRate)
			}

			tolerance := tt.expectedScore * 0.01 // 1% tolerance
			diff := baseScore - tt.expectedScore
			if diff < 0 {
				diff = -diff
			}

			if diff > tolerance {
				t.Errorf("got %.2f, want %.2f (diff: %.2f)\nDescription: %s",
					baseScore, tt.expectedScore, diff, tt.description)
			}
		})
	}
}
