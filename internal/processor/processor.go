package processor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liamashdown/insiderwatch/internal/alerts"
	"github.com/liamashdown/insiderwatch/internal/config"
	"github.com/liamashdown/insiderwatch/internal/metrics"
	"github.com/liamashdown/insiderwatch/internal/polymarket/dataapi"
	"github.com/liamashdown/insiderwatch/internal/polymarket/gammaapi"
	"github.com/liamashdown/insiderwatch/internal/storage"
	"github.com/sirupsen/logrus"
)

// Processor handles trade processing and detection logic
type Processor struct {
	cfg         *config.Config
	db          *storage.DB
	dataClient  *dataapi.Client
	gammaClient *gammaapi.Client
	alertSender alerts.Sender
	workerPool  chan struct{}
	log         *logrus.Logger
}

// New creates a new processor
func New(
	cfg *config.Config,
	db *storage.DB,
	dataClient *dataapi.Client,
	gammaClient *gammaapi.Client,
	alertSender alerts.Sender,
	log *logrus.Logger,
) *Processor {
	workerPool := make(chan struct{}, cfg.WalletLookupWorkers)
	for i := 0; i < cfg.WalletLookupWorkers; i++ {
		workerPool <- struct{}{}
	}

	return &Processor{
		cfg:         cfg,
		db:          db,
		dataClient:  dataClient,
		gammaClient: gammaClient,
		alertSender: alertSender,
		workerPool:  workerPool,
		log:         log,
	}
}

// ProcessTrades fetches and processes new trades
func (p *Processor) ProcessTrades(ctx context.Context) error {
	// Get checkpoint
	lastProcessedStr, err := p.db.GetState(ctx, "last_processed_ts")
	if err != nil {
		return fmt.Errorf("get last processed ts: %w", err)
	}

	var lastProcessedTS int64
	if lastProcessedStr != "" {
		lastProcessedTS, _ = strconv.ParseInt(lastProcessedStr, 10, 64)
	}

	// Fetch trades with BIG_TRADE_USD filter (sorted by timestamp DESC for recent-first)
	params := dataapi.TradeParams{
		Limit:         10000,
		TakerOnly:     true,
		FilterType:    "CASH",
		FilterAmount:  p.cfg.BigTradeUSD,
		SortBy:        "timestamp",
		SortDirection: "DESC",
	}

	resp, err := p.dataClient.GetTrades(ctx, params)
	if err != nil {
		return fmt.Errorf("fetch trades: %w", err)
	}

	p.log.WithFields(logrus.Fields{
		"count":              len(resp.Trades),
		"last_processed_ts":  lastProcessedTS,
	}).Info("Fetched trades from Data API")

	// Process trades in parallel
	var wg sync.WaitGroup
	for _, trade := range resp.Trades {
		// Skip if already processed
		if trade.Timestamp <= lastProcessedTS {
			continue
		}

		wg.Add(1)
		go func(t dataapi.Trade) {
			defer wg.Done()
			
			// Acquire worker
			<-p.workerPool
			defer func() { p.workerPool <- struct{}{} }()

			if err := p.processTrade(ctx, &t); err != nil {
				p.log.WithError(err).WithField("trade_hash", p.calculateTradeHash(&t)).Error("Failed to process trade")
			}
		}(trade)
	}

	wg.Wait()

	// Update checkpoint
	if len(resp.Trades) > 0 {
		maxTS := int64(0)
		for _, trade := range resp.Trades {
			if trade.Timestamp > maxTS {
				maxTS = trade.Timestamp
			}
		}
		if maxTS > lastProcessedTS {
			if err := p.db.SetState(ctx, "last_processed_ts", strconv.FormatInt(maxTS, 10)); err != nil {
				p.log.WithError(err).Error("Failed to update checkpoint")
			}
		}
	}

	return nil
}

func (p *Processor) processTrade(ctx context.Context, trade *dataapi.Trade) error {
	start := time.Now()
	defer func() {
		metrics.RecordTradeProcessing(time.Since(start), "success")
	}()

	// Calculate trade hash for deduplication
	tradeHash := p.calculateTradeHash(trade)

	// Check if already seen
	seen, err := p.db.HasTradeSeen(ctx, tradeHash)
	if err != nil {
		return fmt.Errorf("check trade seen: %w", err)
	}
	if seen {
		metrics.TradesProcessed.WithLabelValues("duplicate").Inc()
		return nil // Already processed
	}

	// Resolve market info FIRST to check if we should process this trade at all
	marketInfo, err := p.resolveMarket(ctx, trade)
	if err != nil {
		p.log.WithError(err).WithField("condition_id", trade.ConditionID).Warn("Failed to resolve market")
	}

	// Skip markets that can't involve insider trading (sports, entertainment, etc.)
	if marketInfo != nil && isNotInsiderCategory(marketInfo.Category) {
		metrics.TradesProcessed.WithLabelValues("filtered_sports").Inc()
		p.log.WithFields(logrus.Fields{
			"category":     marketInfo.Category,
			"condition_id": trade.ConditionID,
			"title":        marketInfo.Title,
		}).Debug("Skipping sports/entertainment market")
		return nil
	}

	// Skip trades for markets that have already ended/resolved
	// And market ending needs to be within 2 months from now
	if marketInfo != nil && marketInfo.EndDate > 0 && (trade.Timestamp > marketInfo.EndDate || marketInfo.EndDate >= time.Now().AddDate(0, 2, 0).Unix()) {
		metrics.TradesProcessed.WithLabelValues("filtered_closed").Inc()
		p.log.WithFields(logrus.Fields{
			"condition_id": trade.ConditionID,
			"title":        marketInfo.Title,
			"trade_time":   trade.Timestamp,
			"end_date":     marketInfo.EndDate,
		}).Debug("Skipping trade for closed market")
		return nil
	}

	// Calculate notional
	notional := p.calculateNotional(trade)

	// Skip if too small (post-API filter)
	if notional < p.cfg.MinTradeUSD {
		metrics.TradesProcessed.WithLabelValues("filtered_size").Inc()
		return nil
	}

	// Get or create wallet record
	wallet, err := p.getOrCreateWallet(ctx, trade.ProxyWallet, trade.Timestamp)
	if err != nil {
		return fmt.Errorf("get wallet: %w", err)
	}

	// Calculate wallet age in days
	walletAgeDays := int((trade.Timestamp - wallet.FirstSeenTS) / 86400)

	// Calculate time to market close (hours)
	var hoursToClose float64
	if marketInfo != nil && marketInfo.EndDate > 0 {
		hoursToClose = float64(marketInfo.EndDate-trade.Timestamp) / 3600.0
	}

	// Calculate suspicion score with time-to-close multiplier
	score := p.calculateSuspicionScore(notional, walletAgeDays, hoursToClose)

	// Store trade
	tradeRecord := &storage.TradeSeen{
		TradeHash:       tradeHash,
		TransactionHash: trade.TransactionHash,
		ConditionID:     trade.ConditionID,
		ProxyWallet:     trade.ProxyWallet,
		TimestampSec:    trade.Timestamp,
		NotionalUSD:     notional,
		Side:            trade.Side,
		Outcome:         trade.Outcome,
		Price:           trade.Price,
	}
	if err := p.db.InsertTrade(ctx, tradeRecord); err != nil {
		return fmt.Errorf("insert trade: %w", err)
	}

	// Update wallet stats
	wallet.TotalTrades++
	wallet.TotalVolumeUSD += notional
	wallet.LastActivityTS = trade.Timestamp
	wallet.UpdatedTS = time.Now().Unix()
	if err := p.db.UpsertWallet(ctx, wallet); err != nil {
		p.log.WithError(err).Error("Failed to update wallet stats")
	}

	// Update net position
	if err := p.updateNetPosition(ctx, trade, notional); err != nil {
		p.log.WithError(err).Error("Failed to update net position")
	}

	// Get wallet win rate for additional scoring context
	walletStats, err := p.db.GetWalletStats(ctx, trade.ProxyWallet)
	if err != nil {
		p.log.WithError(err).Warn("Failed to get wallet stats")
	}
	var winRate float64
	if walletStats != nil && walletStats.TotalResolvedTrades > 0 {
		winRate = walletStats.WinRate
	}

	// Calculate funding age (time between funding and first trade)
	var fundingAgeHours float64
	if wallet.FundingReceivedTS > 0 && wallet.FirstSeenTS > 0 {
		fundingAgeHours = float64(wallet.FirstSeenTS-wallet.FundingReceivedTS) / 3600.0
	}

	// Check if alert should be triggered
	if walletAgeDays <= p.cfg.NewWalletDaysMax {
		// Apply win rate multiplier to severity determination
		adjustedScore := score
		if winRate >= p.cfg.MinWinRateThreshold {
			// High win rate increases suspicion
			adjustedScore *= (1.0 + winRate)
		}

		// Record suspicion score
		metrics.RecordSuspicionScore(adjustedScore)

		// Apply funding age multiplier if wallet traded very soon after funding
		// Suspicious if first trade within 24 hours of receiving funds
		if fundingAgeHours > 0 && fundingAgeHours <= 24 {
			// 1 hour = 2.5x, 12 hours = 1.5x, 24 hours = 1.0x
			fundingMultiplier := 1.0 + (24.0-fundingAgeHours)/24.0*1.5
			adjustedScore *= fundingMultiplier
			p.log.WithFields(logrus.Fields{
				"wallet":             wallet.WalletAddress,
				"funding_age_hours": fundingAgeHours,
				"multiplier":        fundingMultiplier,
			}).Debug("Applied funding age multiplier")
		}

		severity := p.determineSeverity(adjustedScore)
		if severity != alerts.SeverityInfo {
			if err := p.sendAlert(ctx, trade, wallet, marketInfo, notional, walletAgeDays, adjustedScore, severity); err != nil {
				p.log.WithError(err).Error("Failed to send alert")
			}
		}
	}

	return nil
}

func (p *Processor) getOrCreateWallet(ctx context.Context, address string, tradeTimestamp int64) (*storage.Wallet, error) {
	wallet, err := p.db.GetWallet(ctx, address)
	if err != nil {
		return nil, err
	}

	if wallet != nil {
		return wallet, nil
	}

	// New wallet - get first activity
	var firstSeenTS, fundingReceivedTS int64
	activity, err := p.dataClient.GetWalletFirstActivity(ctx, address)
	if err != nil {
		p.log.WithError(err).WithField("wallet", address).Warn("Failed to get first activity, using trade timestamp")
		firstSeenTS = tradeTimestamp
		fundingReceivedTS = 0 // Unknown
	} else {
		firstSeenTS = activity.Timestamp
		// First activity is likely funding received
		fundingReceivedTS = activity.Timestamp
	}

	wallet = &storage.Wallet{
		WalletAddress:     address,
		FirstSeenTS:       firstSeenTS,
		FundingReceivedTS: fundingReceivedTS,
		TotalTrades:       0,
		TotalVolumeUSD:    0,
		LastActivityTS:    tradeTimestamp,
		UpdatedTS:         time.Now().Unix(),
	}

	return wallet, nil
}

func (p *Processor) resolveMarket(ctx context.Context, trade *dataapi.Trade) (*MarketInfo, error) {
	// Check cache first
	cached, err := p.db.GetMarketMap(ctx, trade.ConditionID)
	if err != nil {
		return nil, err
	}

	if cached != nil {
		// Check TTL (24 hours)
		if time.Now().Unix()-cached.UpdatedTS < 86400 {
			return &MarketInfo{
				Title:    cached.MarketTitle,
				Slug:     cached.MarketSlug,
				URL:      cached.MarketURL,
				Category: cached.Category,
				EndDate:  cached.EndDate,
			}, nil
		}
	}

	// Resolve via Gamma API or trade data
	var marketURL, marketTitle, marketSlug string
	var category string
	var endDate int64

	// Always try to get market info from Gamma API for category data
	market, err := p.gammaClient.GetMarketByConditionID(ctx, trade.ConditionID)
	if err != nil {
		// Fallback to trade data if Gamma API fails
		if trade.Slug != "" {
			marketSlug = trade.Slug
			marketTitle = trade.Title
			marketURL = fmt.Sprintf("https://polymarket.com/market/%s", trade.Slug)
			// No category available - cannot filter sports
		} else {
			marketURL = fmt.Sprintf("https://polymarket.com/search?q=%s", trade.ConditionID)
			marketTitle = trade.Title
			marketSlug = ""
		}
	} else {
		marketSlug = market.Slug
		marketTitle = market.Question
		marketURL = fmt.Sprintf("https://polymarket.com/market/%s", market.Slug)
		category = market.Category

		// Parse EndDate if present
		if market.EndDate != "" {
			endTime, err := time.Parse(time.RFC3339, market.EndDate)
			if err == nil {
				endDate = endTime.Unix()
			}
		}

		// Cache it
		mapRecord := &storage.MarketMap{
			ConditionID:  trade.ConditionID,
			MarketSlug:   market.Slug,
			MarketTitle:  market.Question,
			MarketURL:    marketURL,
			Category:     market.Category,
			EndDate:      endDate,
			VolumeNum:    market.VolumeNum,
			LiquidityNum: market.LiquidityNum,
			IsActive:     market.Active,
			UpdatedTS:    time.Now().Unix(),
		}
		if err := p.db.UpsertMarketMap(ctx, mapRecord); err != nil {
			p.log.WithError(err).Error("Failed to cache market map")
		}
	}

	return &MarketInfo{
		Title:    marketTitle,
		Slug:     marketSlug,
		URL:      marketURL,
		Category: category,
		EndDate:  endDate,
	}, nil
}

// calculateSuspicionScore calculates a suspicion score based on trade size, wallet age, and time to close
func (p *Processor) calculateSuspicionScore(notional float64, walletAgeDays int, hoursToClose float64) float64 {
	// Base score: notional / wallet age
	baseScore := notional / float64(max(walletAgeDays, 1))

	// Apply time-to-close multiplier if trade is close to market resolution
	if hoursToClose > 0 && hoursToClose <= float64(p.cfg.TimeToCloseHoursMax) {
		// Exponential multiplier: closer to close = higher multiplier
		// e.g., 48 hours = 1.5x, 24 hours = 2x, 12 hours = 3x, 1 hour = 5x
		multiplier := 1.0 + (float64(p.cfg.TimeToCloseHoursMax)-hoursToClose)/float64(p.cfg.TimeToCloseHoursMax)*4.0
		baseScore *= multiplier
	}

	return baseScore
}

// isNotInsiderCategory checks if a market category cannot involve insider trading
// (sports, entertainment, etc.)
func isNotInsiderCategory(category string) bool {
	if category == "" {
		return false
	}
	excludedCategories := []string{
		"sports",
		"nfl",
		"nba",
		"mlb",
		"nhl",
		"soccer",
		"football",
		"basketball",
		"baseball",
		"hockey",
		"mma",
		"ufc",
		"boxing",
		"tennis",
		"golf",
		"racing",
		"f1",
		"nascar",
	}
	categoryLower := strings.ToLower(category)
	for _, excluded := range excludedCategories {
		if strings.Contains(categoryLower, excluded) {
			return true
		}
	}
	return false
}

func (p *Processor) updateNetPosition(ctx context.Context, trade *dataapi.Trade, notional float64) error {
	// Calculate window start (rolling window in hours)
	windowHrs := int64(p.cfg.NetPositionWindowHrs)
	windowStartTS := (trade.Timestamp / (windowHrs * 3600)) * (windowHrs * 3600)

	// Net notional is positive for buys, negative for sells
	netNotional := notional
	if trade.Side == "SELL" {
		netNotional = -notional
	}

	pos := &storage.WalletMarketNet{
		WalletAddress:  trade.ProxyWallet,
		ConditionID:    trade.ConditionID,
		WindowStartTS:  windowStartTS,
		NetNotionalUSD: netNotional,
		TradeCount:     1,
		UpdatedTS:      time.Now().Unix(),
	}

	return p.db.UpsertNetPosition(ctx, pos)
}

func (p *Processor) sendAlert(
	ctx context.Context,
	trade *dataapi.Trade,
	wallet *storage.Wallet,
	marketInfo *MarketInfo,
	notional float64,
	walletAgeDays int,
	score float64,
	severity alerts.Severity,
) error {
	// Check cooldown
	lastAlert, err := p.db.GetLastAlertForWallet(ctx, wallet.WalletAddress)
	if err != nil {
		p.log.WithError(err).Warn("Failed to get last alert")
	}
	if lastAlert != nil {
		cooldownSec := int64(p.cfg.AlertCooldownMins * 60)
		if time.Now().Unix()-lastAlert.CreatedTS < cooldownSec {
			p.log.WithField("wallet", wallet.WalletAddress).Info("Alert suppressed (cooldown)")
			metrics.AlertsSuppressed.Inc()
			return nil
		}
	}

	// Store alert
	alertRecord := &storage.Alert{
		AlertType:         string(severity),
		WalletAddress:     wallet.WalletAddress,
		ConditionID:       trade.ConditionID,
		MarketTitle:       marketInfo.Title,
		MarketSlug:        marketInfo.Slug,
		MarketURL:         marketInfo.URL,
		Side:              trade.Side,
		Outcome:           trade.Outcome,
		NotionalUSD:       notional,
		Price:             trade.Price,
		WalletAgeDays:     walletAgeDays,
		SuspicionScore:    score,
		TransactionHash:   trade.TransactionHash,
		TradeTimestampSec: trade.Timestamp,
	}
	if _, err := p.db.InsertAlert(ctx, alertRecord); err != nil {
		return fmt.Errorf("insert alert: %w", err)
	}

	// Send alert
	metrics.AlertsTriggered.WithLabelValues(string(severity)).Inc()

	payload := &alerts.AlertPayload{
		Severity:        severity,
		WalletAddress:   wallet.WalletAddress,
		WalletShort:     shortenAddress(wallet.WalletAddress),
		MarketTitle:     marketInfo.Title,
		MarketURL:       marketInfo.URL,
		Side:            trade.Side,
		Outcome:         trade.Outcome,
		NotionalUSD:     notional,
		Price:           trade.Price,
		WalletAgeDays:   walletAgeDays,
		FirstSeenDate:   time.Unix(wallet.FirstSeenTS, 0).Format("2006-01-02"),
		SuspicionScore:  score,
		TransactionHash: trade.TransactionHash,
		TxHashShort:     shortenHash(trade.TransactionHash),
		Timestamp:       time.Unix(trade.Timestamp, 0),
		Environment:     p.cfg.Environment,
	}

	return p.alertSender.Send(ctx, payload)
}

func (p *Processor) determineSeverity(score float64) alerts.Severity {
	if score >= p.cfg.SuspicionScoreAlert {
		return alerts.SeverityAlert
	}
	if score >= p.cfg.SuspicionScoreWarn {
		return alerts.SeverityWarn
	}
	return alerts.SeverityInfo
}

func (p *Processor) calculateTradeHash(trade *dataapi.Trade) string {
	// Prefer transaction hash
	if trade.TransactionHash != "" {
		return trade.TransactionHash
	}

	// Fallback to derived hash
	data := fmt.Sprintf("%s:%s:%d:%.6f:%.6f",
		trade.ProxyWallet,
		trade.ConditionID,
		trade.Timestamp,
		trade.Size,
		trade.Price,
	)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)
}

func (p *Processor) calculateNotional(trade *dataapi.Trade) float64 {
	// Prefer usdcSize
	if trade.USDCSize > 0 {
		return trade.USDCSize
	}

	// Fallback to size * price
	return trade.Size * trade.Price
}

func parseFloat(s string) float64 {
	val, _ := strconv.ParseFloat(s, 64)
	return val
}

func shortenAddress(addr string) string {
	if len(addr) <= 10 {
		return addr
	}
	return addr[:6] + "..." + addr[len(addr)-4:]
}

func shortenHash(hash string) string {
	if len(hash) <= 16 {
		return hash
	}
	return hash[:8] + "..." + hash[len(hash)-8:]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RecalculateWinRates checks for resolved markets and updates wallet win rates
func (p *Processor) RecalculateWinRates(ctx context.Context) error {
	start := time.Now()
	p.log.Info("Starting win rate recalculation")

	// Get all unique condition IDs from trades
	conditionIDs, err := p.db.GetAllConditionIDs(ctx)
	if err != nil {
		return fmt.Errorf("get condition IDs: %w", err)
	}

	p.log.WithField("markets", len(conditionIDs)).Info("Checking markets for resolution")

	resolvedCount := 0
	for _, conditionID := range conditionIDs {
		// Check if already resolved
		existing, err := p.db.GetMarketResolution(ctx, conditionID)
		if err != nil {
			p.log.WithError(err).WithField("condition_id", conditionID).Warn("Failed to check resolution")
			continue
		}
		if existing != nil {
			continue // Already resolved
		}

		// Try to resolve via Gamma API
		market, err := p.gammaClient.GetMarketByConditionID(ctx, conditionID)
		if err != nil {
			p.log.WithError(err).WithField("condition_id", conditionID).Debug("Failed to fetch market")
			continue
		}

		// Check if market is closed
		if !market.Closed {
			continue
		}

		// Determine winning outcome from prices
		winningOutcome := p.determineWinner(market.Outcomes, market.OutcomePrices)
		if winningOutcome == "" {
			p.log.WithFields(logrus.Fields{
				"condition_id": conditionID,
				"market":       market.Question,
				"outcomes":     market.Outcomes,
				"prices":       market.OutcomePrices,
			}).Debug("Could not determine winner")
			continue
		}

		// Store resolution
		resolution := &storage.MarketResolution{
			ConditionID:    conditionID,
			WinningOutcome: winningOutcome,
			ResolvedTS:     time.Now().Unix(),
			MarketTitle:    market.Question,
		}
		if err := p.db.UpsertMarketResolution(ctx, resolution); err != nil {
			p.log.WithError(err).Error("Failed to store resolution")
			continue
		}

		// Update wallet stats
		if err := p.updateWalletStatsForResolution(ctx, conditionID, winningOutcome); err != nil {
			p.log.WithError(err).Error("Failed to update wallet stats")
			continue
		}

		resolvedCount++
		p.log.WithFields(logrus.Fields{
			"condition_id":    conditionID,
			"market":          market.Question,
			"winning_outcome": winningOutcome,
		}).Info("Resolved market and updated wallet stats")
	}

	p.log.WithField("resolved_count", resolvedCount).Info("Win rate recalculation complete")
	metrics.RecordWinRateCalculation(time.Since(start), resolvedCount)
	return nil
}

// determineWinner parses outcome prices to find the winning outcome
func (p *Processor) determineWinner(outcomes, outcomePrices string) string {
	if outcomes == "" || outcomePrices == "" {
		return ""
	}

	outcomeList := strings.Split(outcomes, ",")
	priceList := strings.Split(outcomePrices, ",")

	if len(outcomeList) != len(priceList) {
		return ""
	}

	// Find outcome with price >= 0.95 (95% probability = winner)
	for i, priceStr := range priceList {
		price, err := strconv.ParseFloat(strings.TrimSpace(priceStr), 64)
		if err != nil {
			continue
		}
		if price >= 0.95 {
			return strings.TrimSpace(outcomeList[i])
		}
	}

	return "" // No clear winner
}

// updateWalletStatsForResolution updates wallet win rates after a market resolves
func (p *Processor) updateWalletStatsForResolution(ctx context.Context, conditionID string, winningOutcome string) error {
	// Get all trades for this market
	trades, err := p.db.GetTradesByConditionID(ctx, conditionID)
	if err != nil {
		return fmt.Errorf("get trades: %w", err)
	}

	// Group trades by wallet and determine if they won or lost
	walletOutcomes := make(map[string]bool) // true = won, false = lost

	for _, trade := range trades {
		if trade.Side == "BUY" {
			// Bought the outcome - win if it matches winning outcome
			walletOutcomes[trade.ProxyWallet] = (trade.Outcome == winningOutcome)
		} else {
			// Sold the outcome - win if it DOESN'T match winning outcome
			walletOutcomes[trade.ProxyWallet] = (trade.Outcome != winningOutcome)
		}
	}

	// Update stats for each wallet
	for walletAddr, won := range walletOutcomes {
		stats, err := p.db.GetWalletStats(ctx, walletAddr)
		if err != nil {
			p.log.WithError(err).WithField("wallet", walletAddr).Warn("Failed to get wallet stats")
			continue
		}

		if stats == nil {
			stats = &storage.WalletStats{
				WalletAddress: walletAddr,
			}
		}

		stats.TotalResolvedTrades++
		if won {
			stats.WinningTrades++
		} else {
			stats.LosingTrades++
		}

		// Recalculate win rate
		if stats.TotalResolvedTrades > 0 {
			stats.WinRate = float64(stats.WinningTrades) / float64(stats.TotalResolvedTrades)
		}

		stats.LastCalculatedTS = time.Now().Unix()

		if err := p.db.UpsertWalletStats(ctx, stats); err != nil {
			p.log.WithError(err).WithField("wallet", walletAddr).Error("Failed to update wallet stats")
		}
	}

	return nil
}

// MarketInfo holds resolved market information
type MarketInfo struct {
	Title    string
	Slug     string
	URL      string
	Category string
	EndDate  int64 // Unix timestamp
}
