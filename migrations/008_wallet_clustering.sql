-- Wallet funding sources - track where wallets receive their initial funding
CREATE TABLE IF NOT EXISTS wallet_funding_sources (
    wallet_address VARCHAR(255) NOT NULL,
    funding_source VARCHAR(255) NOT NULL,
    funding_ts BIGINT NOT NULL,
    amount_usd DECIMAL(20,2) DEFAULT 0,
    tx_hash VARCHAR(255),
    created_ts BIGINT NOT NULL,
    PRIMARY KEY (wallet_address),
    INDEX idx_funding_source (funding_source),
    INDEX idx_funding_ts (funding_ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Wallet clusters - groups of wallets funded from same source
CREATE TABLE IF NOT EXISTS wallet_clusters (
    cluster_id VARCHAR(64) NOT NULL,
    funding_source VARCHAR(255) NOT NULL,
    wallet_count INT NOT NULL DEFAULT 1,
    total_volume_usd DECIMAL(20,2) DEFAULT 0,
    first_seen_ts BIGINT NOT NULL,
    last_activity_ts BIGINT NOT NULL,
    suspicion_score DECIMAL(10,2) DEFAULT 0,
    is_flagged BOOLEAN DEFAULT FALSE,
    updated_ts BIGINT NOT NULL,
    PRIMARY KEY (cluster_id),
    UNIQUE KEY idx_funding_source (funding_source),
    INDEX idx_suspicion_score (suspicion_score DESC),
    INDEX idx_last_activity (last_activity_ts DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Coordinated trades - same market, similar time, across cluster wallets
CREATE TABLE IF NOT EXISTS coordinated_trades (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cluster_id VARCHAR(64) NOT NULL,
    condition_id VARCHAR(255) NOT NULL,
    wallet_count INT NOT NULL,
    total_notional_usd DECIMAL(20,2) NOT NULL,
    time_window_sec INT NOT NULL,
    first_trade_ts BIGINT NOT NULL,
    last_trade_ts BIGINT NOT NULL,
    market_title TEXT,
    created_ts BIGINT NOT NULL,
    INDEX idx_cluster_id (cluster_id),
    INDEX idx_condition_id (condition_id),
    INDEX idx_first_trade_ts (first_trade_ts DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
