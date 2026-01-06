# InsiderWatch

**Production-ready Go service for monitoring Polymarket trades for suspicious activity.**

InsiderWatch monitors Polymarket's public APIs for large bets made by newly-seen wallets, which may indicate potential insider trading. The system detects patterns, tracks wallet history, and sends alerts via Discord, email, or logs.

> **⚠️ Important Disclaimer**  
> This system detects suspicious behavior patterns; it does **NOT** prove insider trading. Alerts should be investigated manually and treated as potential signals only.

---

## Features

- ✅ Real-time trade monitoring via Polymarket Data API
- ✅ Market resolution via Gamma API
- ✅ Wallet age tracking and first activity lookup
- ✅ Suspicion scoring based on trade size and wallet age
- ✅ Net position tracking per wallet per market
- ✅ Alert cooldown to prevent spam
- ✅ Pluggable alert system (Discord, SMTP, log, multi)
- ✅ Token bucket rate limiting for API calls
- ✅ MySQL persistence with proper indexing
- ✅ Docker Compose deployment
- ✅ Health checks and graceful shutdown

---

## Quick Start

### Prerequisites

- Docker & Docker Compose
- (Optional) Discord webhook URL for alerts
- (Optional) SMTP server for email alerts

### 1. Clone and Configure

```bash
git clone <repository>
cd insiderwatch
```

Edit `docker-compose.yml` to configure your environment variables (see Configuration section below).

### 2. Run with Docker Compose

```bash
docker compose up --build
```

This will:
- Start MySQL 8.0
- Run database migrations
- Start the insiderwatch service
- Start MailHog for SMTP testing (web UI at http://localhost:8025)

### 3. Monitor Logs

```bash
docker compose logs -f insiderwatch
```

### 4. Stop

```bash
docker compose down
```

To also remove volumes:

```bash
docker compose down -v
```

---

## Configuration

All configuration is done via environment variables in `docker-compose.yml`:

### Database

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_DSN` | `insiderwatch:insiderwatch@tcp(mysql:3306)/insiderwatch?parseTime=true` | MySQL connection string |
| `DATABASE_MAX_CONNS` | `25` | Max database connections |
| `DATABASE_MAX_IDLE_TIME_MINS` | `5` | Max idle time for connections |

### Data API (Polymarket)

| Variable | Default | Description |
|----------|---------|-------------|
| `DATA_API_BASE_URL` | `https://data-api.polymarket.com` | Data API base URL |
| `DATA_API_AUTH_MODE` | `none` | Auth mode: `none`, `bearer`, `api_key` |
| `DATA_API_BEARER_TOKEN` | - | Bearer token (if using `bearer` mode) |
| `DATA_API_API_KEY` | - | API key (if using `api_key` mode) |
| `DATA_API_EXTRA_HEADERS` | `{}` | JSON map of extra headers (e.g., Cloudflare Access) |

### Gamma API (Markets)

| Variable | Default | Description |
|----------|---------|-------------|
| `GAMMA_API_BASE_URL` | `https://gamma-api.polymarket.com` | Gamma API base URL |

**Note:** Gamma API is public and requires no authentication.

### Detection Thresholds

| Variable | Default | Description |
|----------|---------|-------------|
| `BIG_TRADE_USD` | `10000.0` | Minimum trade size in USD to consider |
| `NEW_WALLET_DAYS_MAX` | `7` | Max wallet age in days to trigger alerts |
| `SUSPICION_SCORE_WARN` | `5000.0` | Score threshold for WARN alerts |
| `SUSPICION_SCORE_ALERT` | `10000.0` | Score threshold for ALERT alerts |
| `NET_POSITION_WINDOW_HRS` | `24` | Rolling window for net position tracking |
| `ALERT_COOLDOWN_MINS` | `60` | Cooldown between alerts for same wallet |

**Suspicion Score Formula:**
```
score = notional_usd / max(wallet_age_days, 1)
```

### Rate Limiting

| Variable | Default | Description |
|----------|---------|-------------|
| `DATA_API_TRADES_RPS` | `2.0` | Requests per second for trades endpoint |
| `DATA_API_ACTIVITY_RPS` | `1.0` | Requests per second for activity endpoint |
| `GAMMA_API_MARKETS_RPS` | `5.0` | Requests per second for markets endpoint |

### Worker Pool

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLET_LOOKUP_WORKERS` | `5` | Parallel workers for wallet lookups |

### Polling

| Variable | Default | Description |
|----------|---------|-------------|
| `POLL_INTERVAL_SEC` | `30` | Seconds between trade polls |

### Alerts

| Variable | Default | Description |
|----------|---------|-------------|
| `ALERT_MODE` | `log` | Alert mode: `log`, `discord`, `smtp`, `multi` |

#### Discord Alerts

| Variable | Default | Description |
|----------|---------|-------------|
| `DISCORD_WEBHOOK_URL` | - | Discord webhook URL (required for `discord` or `multi` mode) |

#### SMTP Alerts

| Variable | Default | Description |
|----------|---------|-------------|
| `SMTP_HOST` | `mailhog` | SMTP server hostname |
| `SMTP_PORT` | `1025` | SMTP server port |
| `SMTP_USER` | - | SMTP username |
| `SMTP_PASSWORD` | - | SMTP password |
| `SMTP_FROM` | `insiderwatch@example.com` | From email address |
| `SMTP_TO` | `alerts@example.com` | Comma-separated recipient emails |

---

## Alert Examples

### Discord Alert (ALERT Severity)

```
🚨 New wallet big bet (ALERT)

**$12,340** on **YES** @ **0.62**
Wallet age **2d** (first seen 2026-01-03)

Wallet: `0x1234...abcd`
Market: Will Trump win 2024 election?
Side: BUY YES
Notional: $12,340.00
Price: 0.62
Age: 2 days
Score: 6170.00
Tx: `0xabcd...1234`

insiderwatch • production • 2026-01-05 14:32:10 UTC
```

### Email Alert

```
From: insiderwatch@example.com
To: alerts@example.com
Subject: [ALERT] Suspicious trade: $12340.00 on Will Trump win 2024 election?

INSIDERWATCH ALERT - ALERT
═══════════════════════════════════════

A suspicious trade has been detected:

TRADE DETAILS
─────────────────────────────────────
Notional:       $12340.00
Side:           BUY YES
Price:          0.62
Market:         Will Trump win 2024 election?
Market URL:     https://polymarket.com/market/trump-2024

WALLET DETAILS
─────────────────────────────────────
Address:        0x1234567890abcdef1234567890abcdef12345678
Age:            2 days (first seen 2026-01-03)
Suspicion Score: 6170.00

TRANSACTION
─────────────────────────────────────
Hash:           0xabcdef1234567890abcdef1234567890abcdef12
Time:           2026-01-05T14:32:10Z

═══════════════════════════════════════
Environment: production
Generated: 2026-01-05 14:32:10 UTC

Note: This system detects suspicious behavior;
it does NOT prove insider trading.
```

---

## Architecture

### Project Structure

```
insiderwatch/
├── cmd/
│   └── insiderwatch/
│       └── main.go              # Application entry point
├── internal/
│   ├── config/                  # Configuration management
│   ├── polymarket/
│   │   ├── dataapi/             # Data API client
│   │   └── gammaapi/            # Gamma API client
│   ├── processor/               # Core detection logic
│   ├── storage/                 # MySQL repository layer
│   ├── alerts/                  # Alert senders (Discord, SMTP, log)
│   ├── ratelimit/               # Token bucket rate limiter
│   └── metrics/                 # (Future: Prometheus metrics)
├── migrations/
│   └── 001_initial_schema.sql   # Database schema
├── Dockerfile
├── docker-compose.yml
└── README.md
```

### Data Flow

1. **Poll**: Service polls `/trades` endpoint with `BIG_TRADE_USD` filter
2. **Deduplicate**: Check if trade already processed (via transaction hash)
3. **Wallet Lookup**: Query `/activity` for wallet's first activity timestamp
4. **Calculate**: Compute wallet age and suspicion score
5. **Resolve Market**: Use Gamma API to resolve market details (cached in MySQL)
6. **Track Position**: Update rolling net position for wallet + market
7. **Alert**: If score exceeds threshold and within cooldown, send alert

### MySQL Schema

- `app_state`: Checkpointing (last processed timestamp)
- `trades_seen`: Deduplication via transaction hash
- `wallets`: Wallet first seen timestamp and stats
- `alerts`: Alert history
- `wallet_market_net`: Net position tracking per wallet per market
- `market_map`: Cached market resolution from Gamma API

---

## API Sources of Truth

### Data API
- **Base URL**: https://data-api.polymarket.com
- **Endpoints Used**:
  - `GET /trades` (with `filterType=CASH`, `filterAmount=BIG_TRADE_USD`)
  - `GET /activity` (to determine wallet first activity)

### Gamma API
- **Base URL**: https://gamma-api.polymarket.com
- **Endpoints Used**:
  - `GET /markets?condition_ids[]=<conditionId>`
  - `GET /markets/slug/{slug}`
  - `GET /markets/{id}`

---

## False Positives & Limitations

### Common False Positives

1. **Legitimate new traders**: Some users genuinely start with large trades
2. **Wallet rotation**: Experienced traders may use fresh wallets for privacy
3. **Arbitrage bots**: New wallets deployed for automated trading strategies
4. **Market makers**: New LP wallets providing liquidity

### Limitations

- **No proof of insider knowledge**: System only detects patterns
- **Market timing**: Cannot detect if trade was made before or after news
- **Cross-chain analysis**: Does not track wallets across multiple chains
- **Data API delays**: Trades may appear with delay
- **No social graph**: Cannot detect coordination between wallets

### Recommended Investigation

When an alert fires, investigate:
- Check if wallet has other blockchain activity
- Review timing relative to market events
- Look for patterns across multiple markets
- Verify market resolution timeline
- Check if similar patterns exist for other wallets

---

## Health Checks

The service exposes two health endpoints:

- `GET /health` - Basic health check (returns 200 OK)
- `GET /ready` - Readiness check (returns 200 READY)

Default port: `8080`

---

## Development

### Local Development (without Docker)

1. Start MySQL locally:
```bash
mysql -u root -p
CREATE DATABASE insiderwatch;
CREATE USER 'insiderwatch'@'localhost' IDENTIFIED BY 'insiderwatch';
GRANT ALL PRIVILEGES ON insiderwatch.* TO 'insiderwatch'@'localhost';
```

2. Run migrations:
```bash
mysql -u insiderwatch -pinsiderwatch insiderwatch < migrations/001_initial_schema.sql
```

3. Set environment variables:
```bash
export DATABASE_DSN="insiderwatch:insiderwatch@tcp(localhost:3306)/insiderwatch?parseTime=true"
export ALERT_MODE=log
# ... other vars
```

4. Run:
```bash
go run cmd/insiderwatch/main.go
```

### Running Tests

```bash
go test ./...
```

---

## Production Deployment

### Considerations

1. **API Keys**: Configure `DATA_API_AUTH_MODE` if Polymarket requires authentication
2. **Rate Limits**: Adjust `*_RPS` values based on your API quota
3. **Database**: Use managed MySQL with backups
4. **Monitoring**: Add Prometheus metrics (future enhancement)
5. **Logging**: Send logs to centralized logging system
6. **Secrets**: Use secrets management (AWS Secrets Manager, Vault, etc.)
7. **Discord Webhooks**: Rotate webhook URLs periodically
8. **Alert Tuning**: Adjust thresholds based on observed false positive rate

### Environment Variables for Production

```yaml
ENVIRONMENT: production
DATA_API_AUTH_MODE: bearer
DATA_API_BEARER_TOKEN: ${SECRET_TOKEN}
ALERT_MODE: multi
DISCORD_WEBHOOK_URL: ${DISCORD_WEBHOOK}
SMTP_HOST: smtp.sendgrid.net
SMTP_PORT: 587
SMTP_USER: apikey
SMTP_PASSWORD: ${SENDGRID_API_KEY}
SMTP_TO: security-team@company.com
```

---

## Troubleshooting

### No trades being detected

- Check `BIG_TRADE_USD` threshold (default: $10,000)
- Verify Data API is accessible
- Check rate limiting logs
- Verify `DATA_API_AUTH_MODE` is correct

### 401 Unauthorized from Data API

- Set `DATA_API_AUTH_MODE=bearer` or `api_key`
- Provide valid `DATA_API_BEARER_TOKEN` or `DATA_API_API_KEY`
- Check `DATA_API_EXTRA_HEADERS` for Cloudflare Access tokens

### Market resolution failing

- Gamma API is public and should not require auth
- Check `GAMMA_API_BASE_URL` is correct
- Verify rate limiting is not too aggressive

### Alerts not sending

- Check `ALERT_MODE` is set correctly
- For Discord: verify `DISCORD_WEBHOOK_URL` is valid
- For SMTP: check `SMTP_HOST`, `SMTP_PORT`, credentials
- Check logs for error messages

### Database connection issues

- Verify `DATABASE_DSN` is correct
- Ensure MySQL is running and migrations are applied
- Check network connectivity (in Docker, use service name `mysql`)

---

## License

MIT License

---

## Contributing

Contributions are welcome! Please open an issue or pull request.

---

## Support

For issues, questions, or feature requests, please open a GitHub issue.

---

**Built with ❤️ for Polymarket traders**
