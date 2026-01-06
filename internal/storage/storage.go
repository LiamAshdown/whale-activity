package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/liamashdown/insiderwatch/internal/config"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps the GORM database connection
type DB struct {
	conn *gorm.DB
	log  *logrus.Logger
}

// New creates a new database connection with GORM
func New(cfg *config.Config, log *logrus.Logger) (*DB, error) {
	// Configure GORM logger
	gormLogger := logger.New(
		&gormLogAdapter{log: log},
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	conn, err := gorm.Open(mysql.Open(cfg.DatabaseDSN), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := conn.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.DatabaseMaxConns)
	sqlDB.SetMaxIdleConns(cfg.DatabaseMaxConns / 2)
	sqlDB.SetConnMaxIdleTime(cfg.DatabaseMaxIdleTime)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	log.Info("Database connection established")

	return &DB{conn: conn, log: log}, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	sqlDB, err := db.conn.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// AutoMigrate runs GORM auto-migration (for development only)
func (db *DB) AutoMigrate() error {
	return db.conn.AutoMigrate(
		&AppState{},
		&TradeSeen{},
		&Wallet{},
		&Alert{},
		&WalletMarketNet{},
		&MarketMap{},
		&MarketResolution{},
		&WalletStats{},
	)
}

// GetState retrieves a state value by key
func (db *DB) GetState(ctx context.Context, key string) (string, error) {
	var state AppState
	result := db.conn.WithContext(ctx).Where("state_key = ?", key).First(&state)
	if result.Error == gorm.ErrRecordNotFound {
		return "", nil
	}
	if result.Error != nil {
		return "", result.Error
	}
	return state.StateValue, nil
}

// SetState sets a state value
func (db *DB) SetState(ctx context.Context, key, value string) error {
	now := time.Now().Unix()
	state := AppState{
		StateKey:   key,
		StateValue: value,
		UpdatedTS:  now,
	}
	result := db.conn.WithContext(ctx).Save(&state)
	return result.Error
}

// HasTradeSeen checks if a trade has been seen
func (db *DB) HasTradeSeen(ctx context.Context, tradeHash string) (bool, error) {
	var count int64
	result := db.conn.WithContext(ctx).
		Model(&TradeSeen{}).
		Where("trade_hash = ?", tradeHash).
		Count(&count)
	if result.Error != nil {
		return false, result.Error
	}
	return count > 0, nil
}

// InsertTrade inserts a new trade record
func (db *DB) InsertTrade(ctx context.Context, trade *TradeSeen) error {
	result := db.conn.WithContext(ctx).Create(trade)
	return result.Error
}

// GetWallet retrieves a wallet record
func (db *DB) GetWallet(ctx context.Context, address string) (*Wallet, error) {
	var wallet Wallet
	result := db.conn.WithContext(ctx).Where("wallet_address = ?", address).First(&wallet)
	if result.Error == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &wallet, nil
}

// UpsertWallet inserts or updates a wallet record
func (db *DB) UpsertWallet(ctx context.Context, wallet *Wallet) error {
	// Check if exists
	existing, err := db.GetWallet(ctx, wallet.WalletAddress)
	if err != nil {
		return err
	}

	if existing == nil {
		// Insert new
		return db.conn.WithContext(ctx).Create(wallet).Error
	}

	// Update existing
	updates := map[string]interface{}{
		"total_trades":     gorm.Expr("total_trades + ?", wallet.TotalTrades),
		"total_volume_usd": gorm.Expr("total_volume_usd + ?", wallet.TotalVolumeUSD),
		"last_activity_ts": wallet.LastActivityTS,
		"updated_ts":       wallet.UpdatedTS,
	}
	return db.conn.WithContext(ctx).
		Model(&Wallet{}).
		Where("wallet_address = ?", wallet.WalletAddress).
		Updates(updates).Error
}

// InsertAlert inserts a new alert record
func (db *DB) InsertAlert(ctx context.Context, alert *Alert) (int64, error) {
	result := db.conn.WithContext(ctx).Create(alert)
	if result.Error != nil {
		return 0, result.Error
	}
	return alert.ID, nil
}

// GetLastAlertForWallet retrieves the most recent alert for a wallet
func (db *DB) GetLastAlertForWallet(ctx context.Context, wallet string) (*Alert, error) {
	var alert Alert
	result := db.conn.WithContext(ctx).
		Where("wallet_address = ?", wallet).
		Order("created_ts DESC").
		First(&alert)
	if result.Error == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &alert, nil
}

// UpsertNetPosition updates or inserts net position
func (db *DB) UpsertNetPosition(ctx context.Context, pos *WalletMarketNet) error {
	// Check if exists
	var existing WalletMarketNet
	result := db.conn.WithContext(ctx).Where(
		"wallet_address = ? AND condition_id = ? AND window_start_ts = ?",
		pos.WalletAddress, pos.ConditionID, pos.WindowStartTS,
	).First(&existing)

	if result.Error == gorm.ErrRecordNotFound {
		// Insert new
		return db.conn.WithContext(ctx).Create(pos).Error
	}
	if result.Error != nil {
		return result.Error
	}

	// Update existing
	updates := map[string]interface{}{
		"net_notional_usd": gorm.Expr("net_notional_usd + ?", pos.NetNotionalUSD),
		"trade_count":      gorm.Expr("trade_count + ?", pos.TradeCount),
		"updated_ts":       pos.UpdatedTS,
	}
	return db.conn.WithContext(ctx).
		Model(&WalletMarketNet{}).
		Where("wallet_address = ? AND condition_id = ? AND window_start_ts = ?",
			pos.WalletAddress, pos.ConditionID, pos.WindowStartTS).
		Updates(updates).Error
}

// GetNetPosition retrieves net position for a wallet and market
func (db *DB) GetNetPosition(ctx context.Context, wallet, conditionID string, windowStartTS int64) (*WalletMarketNet, error) {
	var pos WalletMarketNet
	result := db.conn.WithContext(ctx).Where(
		"wallet_address = ? AND condition_id = ? AND window_start_ts = ?",
		wallet, conditionID, windowStartTS,
	).First(&pos)
	if result.Error == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &pos, nil
}

// GetMarketMap retrieves a cached market mapping
func (db *DB) GetMarketMap(ctx context.Context, conditionID string) (*MarketMap, error) {
	var market MarketMap
	result := db.conn.WithContext(ctx).Where("condition_id = ?", conditionID).First(&market)
	if result.Error == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &market, nil
}

// UpsertMarketMap inserts or updates a market mapping
func (db *DB) UpsertMarketMap(ctx context.Context, market *MarketMap) error {
	result := db.conn.WithContext(ctx).Save(market)
	return result.Error
}

// GetMarketResolution retrieves a market resolution by condition ID
func (db *DB) GetMarketResolution(ctx context.Context, conditionID string) (*MarketResolution, error) {
	var resolution MarketResolution
	result := db.conn.WithContext(ctx).Where("condition_id = ?", conditionID).First(&resolution)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, result.Error
	}
	return &resolution, nil
}

// UpsertMarketResolution inserts or updates a market resolution
func (db *DB) UpsertMarketResolution(ctx context.Context, resolution *MarketResolution) error {
	result := db.conn.WithContext(ctx).Save(resolution)
	return result.Error
}

// GetWalletStats retrieves wallet statistics
func (db *DB) GetWalletStats(ctx context.Context, walletAddress string) (*WalletStats, error) {
	var stats WalletStats
	result := db.conn.WithContext(ctx).Where("wallet_address = ?", walletAddress).First(&stats)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, result.Error
	}
	return &stats, nil
}

// UpsertWalletStats inserts or updates wallet statistics
func (db *DB) UpsertWalletStats(ctx context.Context, stats *WalletStats) error {
	result := db.conn.WithContext(ctx).Save(stats)
	return result.Error
}

// GetTradesByConditionID retrieves all trades for a specific condition ID
func (db *DB) GetTradesByConditionID(ctx context.Context, conditionID string) ([]TradeSeen, error) {
	var trades []TradeSeen
	result := db.conn.WithContext(ctx).Where("condition_id = ?", conditionID).Find(&trades)
	return trades, result.Error
}

// GetAllConditionIDs retrieves all unique condition IDs from trades
func (db *DB) GetAllConditionIDs(ctx context.Context) ([]string, error) {
	var conditionIDs []string
	result := db.conn.WithContext(ctx).Model(&TradeSeen{}).
		Distinct("condition_id").
		Pluck("condition_id", &conditionIDs)
	return conditionIDs, result.Error
}

// gormLogAdapter adapts logrus to GORM's logger interface
type gormLogAdapter struct {
	log *logrus.Logger
}

func (l *gormLogAdapter) Printf(format string, args ...interface{}) {
	l.log.Debugf(format, args...)
}
