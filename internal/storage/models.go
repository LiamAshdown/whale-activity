package storage

import (
	"time"

	"gorm.io/gorm"
)

// AppState stores application state for checkpointing
type AppState struct {
	StateKey   string `gorm:"primaryKey;size:64"`
	StateValue string `gorm:"type:text;not null"`
	UpdatedTS  int64  `gorm:"not null;index"`
}

func (AppState) TableName() string {
	return "app_state"
}

// TradeSeen tracks processed trades for deduplication
type TradeSeen struct {
	TradeHash       string  `gorm:"primaryKey;size:128"`
	TransactionHash string  `gorm:"size:128;index"`
	ConditionID     string  `gorm:"size:128;not null;index"`
	ProxyWallet     string  `gorm:"size:128;not null;index"`
	TimestampSec    int64   `gorm:"not null;index"`
	NotionalUSD     float64 `gorm:"type:decimal(20,6);not null"`
	Side            string  `gorm:"size:10;not null"`
	Outcome         string  `gorm:"size:255;not null"`
	Price           float64 `gorm:"type:decimal(10,6);not null"`
	CreatedTS       int64   `gorm:"not null"`
}

func (TradeSeen) TableName() string {
	return "trades_seen"
}

// Wallet tracks wallet first seen and activity
type Wallet struct {
	WalletAddress    string  `gorm:"primaryKey;size:128"`
	FirstSeenTS      int64   `gorm:"not null;index"`
	FundingReceivedTS int64  `gorm:"default:0;index"` // When wallet first received funds (if detectable)
	TotalTrades      int     `gorm:"not null;default:1"`
	TotalVolumeUSD   float64 `gorm:"type:decimal(20,6);not null;default:0"`
	LastActivityTS   int64   `gorm:"not null;index"`
	UpdatedTS        int64   `gorm:"not null"`
}

func (Wallet) TableName() string {
	return "wallets"
}

// Alert stores generated alerts
type Alert struct {
	ID                int64   `gorm:"primaryKey;autoIncrement"`
	AlertType         string  `gorm:"size:32;not null;index"`
	WalletAddress     string  `gorm:"size:128;not null;index"`
	ConditionID       string  `gorm:"size:128;not null;index"`
	MarketTitle       string  `gorm:"size:512"`
	MarketSlug        string  `gorm:"size:255"`
	MarketURL         string  `gorm:"size:512"`
	Side              string  `gorm:"size:10;not null"`
	Outcome           string  `gorm:"size:255;not null"`
	NotionalUSD       float64 `gorm:"type:decimal(20,6);not null"`
	Price             float64 `gorm:"type:decimal(10,6);not null"`
	WalletAgeDays     int     `gorm:"not null"`
	SuspicionScore    float64 `gorm:"type:decimal(20,6);not null"`
	TransactionHash   string  `gorm:"size:128"`
	TradeTimestampSec int64   `gorm:"not null"`
	CreatedTS         int64   `gorm:"not null;index"`
}

func (Alert) TableName() string {
	return "alerts"
}

// WalletMarketNet tracks net position per wallet per market
type WalletMarketNet struct {
	WalletAddress  string  `gorm:"primaryKey;size:128"`
	ConditionID    string  `gorm:"primaryKey;size:128"`
	WindowStartTS  int64   `gorm:"primaryKey;not null;index"`
	NetNotionalUSD float64 `gorm:"type:decimal(20,6);not null;index"`
	TradeCount     int     `gorm:"not null;default:0"`
	UpdatedTS      int64   `gorm:"not null"`
}

func (WalletMarketNet) TableName() string {
	return "wallet_market_net"
}

// MarketMap caches market resolution from Gamma API
type MarketMap struct {
	ConditionID  string  `gorm:"primaryKey;size:128"`
	MarketSlug   string  `gorm:"size:255;index"`
	MarketTitle  string  `gorm:"size:512"`
	MarketURL    string  `gorm:"size:512"`
	Category     string  `gorm:"size:128"`
	EndDate      int64   `gorm:"default:0"`
	VolumeNum    float64 `gorm:"type:decimal(20,6)"`
	LiquidityNum float64 `gorm:"type:decimal(20,6)"`
	IsActive     bool    `gorm:"default:true"`
	UpdatedTS    int64   `gorm:"not null;index"`
}

func (MarketMap) TableName() string {
	return "market_map"
}

// MarketResolution tracks which outcome won for resolved markets
type MarketResolution struct {
	ConditionID     string `gorm:"primaryKey;size:128"`
	WinningOutcome  string `gorm:"size:255;not null"`
	ResolvedTS      int64  `gorm:"not null;index"`
	MarketTitle     string `gorm:"size:512"`
}

func (MarketResolution) TableName() string {
	return "market_resolutions"
}

// WalletStats tracks win rate and performance for wallets
type WalletStats struct {
	WalletAddress      string  `gorm:"primaryKey;size:128"`
	TotalResolvedTrades int    `gorm:"not null;default:0"`
	WinningTrades      int     `gorm:"not null;default:0"`
	LosingTrades       int     `gorm:"not null;default:0"`
	WinRate            float64 `gorm:"type:decimal(5,4);not null;default:0.0000;index"`
	TotalProfitUSD     float64 `gorm:"type:decimal(20,6);not null;default:0"`
	LastCalculatedTS   int64   `gorm:"not null;index"`
}

func (WalletStats) TableName() string {
	return "wallet_stats"
}

// BeforeCreate hook for timestamps
func (a *AppState) BeforeCreate(tx *gorm.DB) error {
	if a.UpdatedTS == 0 {
		a.UpdatedTS = time.Now().Unix()
	}
	return nil
}

func (t *TradeSeen) BeforeCreate(tx *gorm.DB) error {
	if t.CreatedTS == 0 {
		t.CreatedTS = time.Now().Unix()
	}
	return nil
}

func (w *Wallet) BeforeCreate(tx *gorm.DB) error {
	if w.UpdatedTS == 0 {
		w.UpdatedTS = time.Now().Unix()
	}
	return nil
}

func (a *Alert) BeforeCreate(tx *gorm.DB) error {
	if a.CreatedTS == 0 {
		a.CreatedTS = time.Now().Unix()
	}
	return nil
}

func (w *WalletMarketNet) BeforeCreate(tx *gorm.DB) error {
	if w.UpdatedTS == 0 {
		w.UpdatedTS = time.Now().Unix()
	}
	return nil
}

func (m *MarketMap) BeforeCreate(tx *gorm.DB) error {
	if m.UpdatedTS == 0 {
		m.UpdatedTS = time.Now().Unix()
	}
	return nil
}

func (w *WalletStats) BeforeCreate(tx *gorm.DB) error {
	if w.LastCalculatedTS == 0 {
		w.LastCalculatedTS = time.Now().Unix()
	}
	return nil
}
