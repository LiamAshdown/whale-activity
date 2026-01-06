# InsiderWatch Setup

## Quick Start

### Development (Local Testing)

```bash
# Start with development settings (MailHog included)
docker compose up -d

# View logs
docker compose logs -f insiderwatch

# Access services
# - Health: http://localhost:8080/health
# - Metrics: http://localhost:8080/metrics
# - MailHog: http://localhost:8025
```

**Development defaults:**
- Lower thresholds ($1,000 trades)
- 30-day wallet age window
- 5-minute alert cooldown
- Uses MailHog for SMTP testing

### Production Deployment

```bash
# Set required environment variables
export ALERT_MODE=discord
export DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/YOUR_WEBHOOK_HERE"

# Optional: Set other production configs
export MYSQL_ROOT_PASSWORD="secure_password"
export MYSQL_PASSWORD="secure_password"

# Start with production settings
docker compose -f docker-compose.prod.yml up -d

# View logs
docker compose -f docker-compose.prod.yml logs -f insiderwatch

# Access monitoring
# - Health: http://localhost:8080/health
# - Metrics: http://localhost:8080/metrics
```

**Production defaults:**
- Higher thresholds ($5,000 trades)
- 7-day wallet age window
- 60-minute alert cooldown
- Requires Discord/SMTP configuration

## Configuration Methods

### Option 1: Environment Variables (Recommended for Production)

```bash
# Set before running docker compose
export ALERT_MODE=discord
export DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/..."
export SMTP_HOST="smtp.gmail.com"
export SMTP_PASSWORD="your-app-password"

docker compose -f docker-compose.prod.yml up -d
```

### Option 2: .env File

Create a `.env` file in the project root:
```bash
ALERT_MODE=discord
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
SMTP_HOST=smtp.gmail.com
SMTP_PASSWORD=your-app-password
```

Then run:
```bash
docker compose -f docker-compose.prod.yml up -d
```

### Option 3: Docker Secrets (Most Secure)

```bash
# Create secret files
echo "https://discord.com/api/webhooks/..." > discord_webhook.txt
echo "your-smtp-password" > smtp_password.txt

# Add to docker-compose.prod.yml:
secrets:
  discord_webhook:
    file: ./discord_webhook.txt
  smtp_password:
    file: ./smtp_password.txt

services:
  insiderwatch:
    secrets:
      - discord_webhook
      - smtp_password
    environment:
      DISCORD_WEBHOOK_URL_FILE: /run/secrets/discord_webhook
      SMTP_PASSWORD_FILE: /run/secrets/smtp_password
```

## Comparison: Dev vs Production

| Setting | Development | Production |
|---------|-------------|------------|
| **Environment** | `docker-compose.yml` | `docker-compose.prod.yml` |
| **Min Trade Size** | $1,000 | $5,000 |
| **Wallet Age Window** | 30 days | 7 days |
| **Alert Cooldown** | 5 minutes | 60 minutes |
| **Score Thresholds** | 1,000 / 5,000 | 10,000 / 25,000 |
| **Alert Method** | Log (+ MailHog) | Discord/SMTP/Multi |
| **Worker Pool** | 3 workers | 5 workers |
| **DB Max Connections** | 10 | 25 |

## Setting Up Discord Alerts

### 1. Create a Discord Webhook

1. **Open your Discord server** and go to Server Settings
2. Navigate to **Integrations** → **Webhooks**
3. Click **New Webhook** or **Create Webhook**
4. Configure the webhook:
   - Name: `InsiderWatch` (or any name you prefer)
   - Channel: Select the channel where alerts should be posted
   - (Optional) Upload a custom avatar
5. Click **Copy Webhook URL**

Your webhook URL will look like:
```
https://discord.com/api/webhooks/1234567890/AbCdEfGhIjKlMnOpQrStUvWxYz
```

### 2. Add to Environment File

**Option A: Direct in .env file (Development)**
```bash
# Open your .env file
nano .env

# Set alert mode and webhook
ALERT_MODE=discord
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/YOUR_WEBHOOK_URL_HERE
```

**Option B: Using Docker secrets (Production)**
```bash
# Create a secret file
echo "https://discord.com/api/webhooks/YOUR_WEBHOOK_URL_HERE" > discord_webhook.txt

# Update .env to use file-based secret
ALERT_MODE=discord
DISCORD_WEBHOOK_URL_FILE=/run/secrets/discord_webhook
```

Then add to your `docker-compose.yml`:
```yaml
secrets:
  discord_webhook:
    file: ./discord_webhook.txt

services:
  insiderwatch:
    secrets:
      - discord_webhook
```

### 3. Test the Setup

```bash
# Restart the service
docker compose down
docker compose up -d

# Watch logs for alerts
docker compose logs -f insiderwatch
```

### 4. Using Multiple Alert Methods

To send alerts to multiple destinations, use comma-separated values:
```bash
# Send to logs and Discord
ALERT_MODE=log,discord
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/YOUR_WEBHOOK_URL_HERE

# Send to all three: logs, Discord, and SMTP
ALERT_MODE=log,discord,smtp
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/YOUR_WEBHOOK_URL_HERE
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASSWORD=your-app-password
SMTP_FROM=insiderwatch@example.com
SMTP_TO=alerts@example.com
```

### Alert Format

Alerts can be sent to multiple destinations by using comma-separated values in `ALERT_MODE`, for example: `log,discord` or `log,discord,smtp`.

Discord alerts include:
- 🚨 **Severity badge** (Critical/High/Medium/Low)
- **Wallet address** (with age in days)
- **Market title** and Polymarket link
- **Trade details**: Side (BUY/SELL), Outcome, Notional USD, Price
- **Suspicion score** (calculated)
- **Transaction hash** (for verification)
- **Timestamp** of the trade

## Setting Up SMTP Email Alerts

## Using Docker Secrets (Production)

For production deployments, use Docker secrets instead of environment variables:

1. **Create secret files:**
   ```bash
   echo "your_webhook_url" > discord_webhook.txt
   echo "your_smtp_password" > smtp_password.txt
   ```

2. **Update docker-compose.yml:**
   ```yaml
   secrets:
     discord_webhook:
       file: ./discord_webhook.txt
     smtp_password:
       file: ./smtp_password.txt
   
   services:
     insiderwatch:
       secrets:
         - discord_webhook
         - smtp_password
   ```

3. **Use secret file paths in .env:**
   ```bash
   DISCORD_WEBHOOK_URL_FILE=/run/secrets/discord_webhook
   SMTP_PASSWORD_FILE=/run/secrets/smtp_password
   ```

## Configuration Reference

See `.env.prod` for full configuration options with documentation.

## Monitoring

Prometheus metrics available at `http://localhost:8080/metrics`:
- `insiderwatch_trades_processed_total` - Trade processing stats
- `insiderwatch_alerts_triggered_total` - Alert counts by severity
- `insiderwatch_suspicion_scores` - Score distribution
- `insiderwatch_api_requests_total` - API success/error rates
- `insiderwatch_database_queries_total` - DB operation stats

## Troubleshooting

**Build fails:**
```bash
docker compose down
docker compose build --no-cache
docker compose up
```

**Database issues:**
```bash
docker compose down -v  # WARNING: Deletes all data
docker compose up
```

**View all logs:**
```bash
docker compose logs
```
