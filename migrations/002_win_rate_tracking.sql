-- Migration: 002_win_rate_tracking
-- Description: Add tables for tracking market resolutions and wallet win rates

-- Market resolutions (track which outcome won)
CREATE TABLE IF NOT EXISTS market_resolutions (
    condition_id VARCHAR(128) PRIMARY KEY,
    winning_outcome VARCHAR(10) NOT NULL, -- YES, NO
    resolved_ts BIGINT NOT NULL,
    market_title VARCHAR(512),
    INDEX idx_resolved (resolved_ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Wallet stats with win rate tracking
CREATE TABLE IF NOT EXISTS wallet_stats (
    wallet_address VARCHAR(128) PRIMARY KEY,
    total_resolved_trades INT NOT NULL DEFAULT 0,
    winning_trades INT NOT NULL DEFAULT 0,
    losing_trades INT NOT NULL DEFAULT 0,
    win_rate DECIMAL(5, 4) NOT NULL DEFAULT 0.0000, -- 0.0000 to 1.0000
    total_profit_usd DECIMAL(20, 6) NOT NULL DEFAULT 0,
    last_calculated_ts BIGINT NOT NULL,
    INDEX idx_win_rate (win_rate),
    INDEX idx_calculated (last_calculated_ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
