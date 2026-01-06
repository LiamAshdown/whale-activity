-- Migration: 001_initial_schema
-- Description: Initial database schema for insiderwatch

-- Application state table for checkpointing
CREATE TABLE IF NOT EXISTS app_state (
    state_key VARCHAR(64) PRIMARY KEY,
    state_value TEXT NOT NULL,
    updated_ts BIGINT NOT NULL,
    INDEX idx_updated_ts (updated_ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Trades seen (for deduplication)
CREATE TABLE IF NOT EXISTS trades_seen (
    trade_hash VARCHAR(128) PRIMARY KEY,
    transaction_hash VARCHAR(128),
    condition_id VARCHAR(128) NOT NULL,
    proxy_wallet VARCHAR(128) NOT NULL,
    timestamp_sec BIGINT NOT NULL,
    notional_usd DECIMAL(20, 6) NOT NULL,
    side VARCHAR(10) NOT NULL,
    outcome VARCHAR(10) NOT NULL,
    price DECIMAL(10, 6) NOT NULL,
    created_ts BIGINT NOT NULL,
    INDEX idx_timestamp (timestamp_sec),
    INDEX idx_wallet (proxy_wallet),
    INDEX idx_condition (condition_id),
    INDEX idx_tx_hash (transaction_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Wallet first seen tracking
CREATE TABLE IF NOT EXISTS wallets (
    wallet_address VARCHAR(128) PRIMARY KEY,
    first_seen_ts BIGINT NOT NULL,
    total_trades INT NOT NULL DEFAULT 1,
    total_volume_usd DECIMAL(20, 6) NOT NULL DEFAULT 0,
    last_activity_ts BIGINT NOT NULL,
    updated_ts BIGINT NOT NULL,
    INDEX idx_first_seen (first_seen_ts),
    INDEX idx_last_activity (last_activity_ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Alerts generated
CREATE TABLE IF NOT EXISTS alerts (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    alert_type VARCHAR(32) NOT NULL, -- WARN, ALERT
    wallet_address VARCHAR(128) NOT NULL,
    condition_id VARCHAR(128) NOT NULL,
    market_title VARCHAR(512),
    market_slug VARCHAR(255),
    market_url VARCHAR(512),
    side VARCHAR(10) NOT NULL,
    outcome VARCHAR(10) NOT NULL,
    notional_usd DECIMAL(20, 6) NOT NULL,
    price DECIMAL(10, 6) NOT NULL,
    wallet_age_days INT NOT NULL,
    suspicion_score DECIMAL(20, 6) NOT NULL,
    transaction_hash VARCHAR(128),
    trade_timestamp_sec BIGINT NOT NULL,
    created_ts BIGINT NOT NULL,
    INDEX idx_wallet (wallet_address),
    INDEX idx_condition (condition_id),
    INDEX idx_created (created_ts),
    INDEX idx_type (alert_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Net position tracking per wallet per market
CREATE TABLE IF NOT EXISTS wallet_market_net (
    wallet_address VARCHAR(128) NOT NULL,
    condition_id VARCHAR(128) NOT NULL,
    window_start_ts BIGINT NOT NULL,
    net_notional_usd DECIMAL(20, 6) NOT NULL,
    trade_count INT NOT NULL DEFAULT 0,
    updated_ts BIGINT NOT NULL,
    PRIMARY KEY (wallet_address, condition_id, window_start_ts),
    INDEX idx_window (window_start_ts),
    INDEX idx_net_notional (net_notional_usd)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Market mapping cache (Gamma API resolution)
CREATE TABLE IF NOT EXISTS market_map (
    condition_id VARCHAR(128) PRIMARY KEY,
    market_slug VARCHAR(255),
    market_title VARCHAR(512),
    market_url VARCHAR(512),
    category VARCHAR(128),
    end_date BIGINT,
    volume_num DECIMAL(20, 6),
    liquidity_num DECIMAL(20, 6),
    is_active BOOLEAN DEFAULT true,
    updated_ts BIGINT NOT NULL,
    INDEX idx_slug (market_slug),
    INDEX idx_updated (updated_ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Initialize checkpoint state
INSERT INTO app_state (state_key, state_value, updated_ts) 
VALUES ('last_processed_ts', '0', UNIX_TIMESTAMP())
ON DUPLICATE KEY UPDATE state_key=state_key;

INSERT INTO app_state (state_key, state_value, updated_ts) 
VALUES ('last_trade_key', '', UNIX_TIMESTAMP())
ON DUPLICATE KEY UPDATE state_key=state_key;
