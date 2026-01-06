-- Increase outcome column size to handle longer outcome names
ALTER TABLE trades_seen MODIFY COLUMN outcome VARCHAR(255) NOT NULL;
ALTER TABLE alerts MODIFY COLUMN outcome VARCHAR(255) NOT NULL;
