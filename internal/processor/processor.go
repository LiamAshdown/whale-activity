package processor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
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
	// Or markets ending more than 2 months from now (too far in future)
	twoMonthsFromNow := time.Now().AddDate(0, 2, 0).Unix()
	if marketInfo != nil && marketInfo.EndDate > 0 && (trade.Timestamp > marketInfo.EndDate || marketInfo.EndDate > twoMonthsFromNow) {
		metrics.TradesProcessed.WithLabelValues("filtered_closed").Inc()
		p.log.WithFields(logrus.Fields{
			"condition_id": trade.ConditionID,
			"title":        marketInfo.Title,
			"trade_time":   trade.Timestamp,
			"end_date":     marketInfo.EndDate,
		}).Debug("Skipping trade for closed or distant market")
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
	var fundingAgeMinutes float64
	if wallet.FundingReceivedTS > 0 && wallet.FirstSeenTS > 0 {
		fundingAgeHours = float64(wallet.FirstSeenTS-wallet.FundingReceivedTS) / 3600.0
		fundingAgeMinutes = float64(wallet.FirstSeenTS-wallet.FundingReceivedTS) / 60.0
	}

	// Check if this is wallet's first trade and it's large
	var firstTradeLargeMultiplier float64 = 1.0
	if wallet.TotalTrades == 1 && notional >= p.cfg.MinTradeUSD {
		firstTradeLargeMultiplier = 2.0
		p.log.WithFields(logrus.Fields{
			"wallet":   wallet.WalletAddress,
			"notional": notional,
		}).Warn("First trade is very large - highly suspicious")
	}

	// Check for flash funding (funded and trading within minutes)
	var flashFundingMultiplier float64 = 1.0
	if fundingAgeMinutes > 0 && fundingAgeMinutes <= 5 {
		flashFundingMultiplier = 3.0
		p.log.WithFields(logrus.Fields{
			"wallet":              wallet.WalletAddress,
			"funding_age_minutes": fundingAgeMinutes,
		}).Warn("Flash funding detected - funded and trading within minutes")
	}

	// Check trade velocity (rapid successive trades)
	var velocityCount int
	var velocityMultiplier float64 = 1.0
	if p.cfg.EnableVelocityDetection {
		var err error
		velocityCount, err = p.checkTradeVelocity(ctx, trade.ProxyWallet, trade.Timestamp)
		if err != nil {
			p.log.WithError(err).Warn("Failed to check trade velocity")
		} else if velocityCount >= p.cfg.VelocityThreshold {
			// Apply velocity multiplier: 3 trades = 1.5x, 5 trades = 2.0x, 10+ = 3.0x
			if velocityCount >= 10 {
				velocityMultiplier = 3.0
			} else if velocityCount >= 5 {
				velocityMultiplier = 2.0
			} else {
				velocityMultiplier = 1.5
			}
			p.log.WithFields(logrus.Fields{
				"wallet":       wallet.WalletAddress,
				"velocity_count": velocityCount,
				"window_minutes": p.cfg.VelocityWindowMinutes,
				"multiplier":     velocityMultiplier,
			}).Warn("High trade velocity detected")
		}
	}

	// Check market liquidity ratio (trade size relative to market)
	var liquidityMultiplier float64 = 1.0
	if marketInfo != nil && marketInfo.LiquidityNum > 0 {
		liquidityRatio := notional / marketInfo.LiquidityNum
		if liquidityRatio > 0.05 { // Trade is 5%+ of market liquidity
			// 5% = 1.2x, 10% = 1.5x, 20% = 2.0x, 50%+ = 3.0x
			if liquidityRatio >= 0.50 {
				liquidityMultiplier = 3.0
			} else if liquidityRatio >= 0.20 {
				liquidityMultiplier = 2.0
			} else if liquidityRatio >= 0.10 {
				liquidityMultiplier = 1.5
			} else {
				liquidityMultiplier = 1.2
			}
			p.log.WithFields(logrus.Fields{
				"wallet":          wallet.WalletAddress,
				"liquidity_ratio": liquidityRatio,
				"multiplier":      liquidityMultiplier,
			}).Warn("Large trade relative to market liquidity")
		}
	}

	// Check for extreme price confidence
	var priceConfidenceMultiplier float64 = 1.0
	if trade.Price >= 0.85 || trade.Price <= 0.15 {
		priceConfidenceMultiplier = 1.5
		p.log.WithFields(logrus.Fields{
			"wallet": wallet.WalletAddress,
			"price":  trade.Price,
			"side":   trade.Side,
		}).Info("Extreme price confidence detected")
	}

	// Check net position concentration (one-sided positioning)
	var concentrationMultiplier float64 = 1.0
	netPosConcentration, err := p.checkNetPositionConcentration(ctx, trade.ProxyWallet, trade.ConditionID, trade.Timestamp, notional, trade.Side)
	if err != nil {
		p.log.WithError(err).Warn("Failed to check net position concentration")
	} else if netPosConcentration > 0.90 { // 90%+ on one side
		concentrationMultiplier = 1.5
		p.log.WithFields(logrus.Fields{
			"wallet":        wallet.WalletAddress,
			"concentration": netPosConcentration,
		}).Warn("High net position concentration detected")
	}

	// Check for coordinated trading patterns
	var isCoordinated bool
	var clusterID string
	var clusterMultiplier float64 = 1.0

	if p.cfg.EnableClusterDetection {
		var err error
		isCoordinated, clusterID, err = p.detectCoordinatedTrade(ctx, trade, trade.ProxyWallet)
		if err != nil {
			p.log.WithError(err).Warn("Failed to detect coordinated trade")
		}

		// Get cluster multiplier
		clusterMultiplier = p.getClusterMultiplier(ctx, trade.ProxyWallet)
	}

	// Check if alert should be triggered
	if walletAgeDays <= p.cfg.NewWalletDaysMax {
		// Apply win rate multiplier to severity determination
		adjustedScore := score
		if winRate >= p.cfg.MinWinRateThreshold {
			// High win rate increases suspicion
			adjustedScore *= (1.0 + winRate)
		}

		// Apply first trade large multiplier
		if firstTradeLargeMultiplier > 1.0 {
			adjustedScore *= firstTradeLargeMultiplier
			p.log.WithFields(logrus.Fields{
				"wallet":                      wallet.WalletAddress,
				"first_trade_large_multiplier": firstTradeLargeMultiplier,
			}).Info("Applied first trade large multiplier")
		}

		// Apply flash funding multiplier
		if flashFundingMultiplier > 1.0 {
			adjustedScore *= flashFundingMultiplier
			p.log.WithFields(logrus.Fields{
				"wallet":                   wallet.WalletAddress,
				"funding_age_minutes":      fundingAgeMinutes,
				"flash_funding_multiplier": flashFundingMultiplier,
			}).Info("Applied flash funding multiplier")
		}

		// Apply liquidity ratio multiplier
		if liquidityMultiplier > 1.0 {
			adjustedScore *= liquidityMultiplier
			p.log.WithFields(logrus.Fields{
				"wallet":               wallet.WalletAddress,
				"liquidity_multiplier": liquidityMultiplier,
			}).Info("Applied liquidity ratio multiplier")
		}

		// Apply extreme price confidence multiplier
		if priceConfidenceMultiplier > 1.0 {
			adjustedScore *= priceConfidenceMultiplier
			p.log.WithFields(logrus.Fields{
				"wallet": wallet.WalletAddress,
				"price":  trade.Price,
			}).Info("Applied extreme price multiplier")
		}

		// Apply net position concentration multiplier
		if concentrationMultiplier > 1.0 {
			adjustedScore *= concentrationMultiplier
			p.log.WithFields(logrus.Fields{
				"wallet":                    wallet.WalletAddress,
				"concentration_multiplier": concentrationMultiplier,
			}).Info("Applied concentration multiplier")
		}

		// Apply velocity multiplier
		if velocityMultiplier > 1.0 {
			adjustedScore *= velocityMultiplier
			p.log.WithFields(logrus.Fields{
				"wallet":              wallet.WalletAddress,
				"velocity_count":      velocityCount,
				"velocity_multiplier": velocityMultiplier,
			}).Info("Applied velocity multiplier")
		}

		// Apply cluster multiplier
		if clusterMultiplier > 1.0 {
			adjustedScore *= clusterMultiplier
			p.log.WithFields(logrus.Fields{
				"wallet":            wallet.WalletAddress,
				"cluster_id":        clusterID,
				"cluster_multiplier": clusterMultiplier,
			}).Info("Applied cluster multiplier")
		}

		// Extra boost if coordinated trade detected
		if isCoordinated {
			adjustedScore *= 2.0
			p.log.WithFields(logrus.Fields{
				"wallet":     wallet.WalletAddress,
				"cluster_id": clusterID,
			}).Warn("Trade is part of coordinated cluster activity")
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
	var fundingSource string
	activity, err := p.dataClient.GetWalletFirstActivity(ctx, address)
	if err != nil {
		p.log.WithError(err).WithField("wallet", address).Warn("Failed to get first activity, using trade timestamp")
		firstSeenTS = tradeTimestamp
		fundingReceivedTS = 0 // Unknown
	} else {
		firstSeenTS = activity.Timestamp
		// First activity is likely funding received
		fundingReceivedTS = activity.Timestamp
		// Extract funding source if available
		fundingSource = activity.GetFromAddress()
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

	// Track funding source if detected
	if fundingSource != "" && p.cfg.EnableClusterDetection {
		if err := p.trackFundingSource(ctx, address, fundingSource, fundingReceivedTS); err != nil {
			p.log.WithError(err).Warn("Failed to track funding source")
		}
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
				Title:        cached.MarketTitle,
				Slug:         cached.MarketSlug,
				URL:          cached.MarketURL,
				Category:     cached.Category,
				EndDate:      cached.EndDate,
				LiquidityNum: cached.LiquidityNum,
				VolumeNum:    cached.VolumeNum,
			}, nil
		}
	}

	// Resolve via Gamma API or trade data
	var marketURL, marketTitle, marketSlug string
	var category string
	var endDate int64
	var liquidityNum, volumeNum float64

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
		liquidityNum = market.LiquidityNum
		volumeNum = market.VolumeNum

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
		Title:        marketTitle,
		Slug:         marketSlug,
		URL:          marketURL,
		Category:     category,
		EndDate:      endDate,
		LiquidityNum: liquidityNum,
		VolumeNum:    volumeNum,
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

// trackFundingSource tracks the funding source for a wallet and updates clusters
func (p *Processor) trackFundingSource(ctx context.Context, walletAddress, fundingSource string, fundingTS int64) error {
	// Store funding source
	source := &storage.WalletFundingSource{
		WalletAddress: walletAddress,
		FundingSource: fundingSource,
		FundingTS:     fundingTS,
	}
	if err := p.db.UpsertWalletFundingSource(ctx, source); err != nil {
		return fmt.Errorf("upsert funding source: %w", err)
	}

	// Update or create cluster
	cluster, err := p.db.GetWalletClusterBySource(ctx, fundingSource)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	if cluster == nil {
		// Create new cluster
		clusterID := fmt.Sprintf("cluster_%x", sha256.Sum256([]byte(fundingSource)))
		cluster = &storage.WalletCluster{
			ClusterID:      clusterID,
			FundingSource:  fundingSource,
			WalletCount:    1,
			FirstSeenTS:    fundingTS,
			LastActivityTS: fundingTS,
		}
	} else {
		// Update existing cluster
		cluster.WalletCount++
		cluster.LastActivityTS = time.Now().Unix()
	}

	if err := p.db.UpsertWalletCluster(ctx, cluster); err != nil {
		return fmt.Errorf("upsert cluster: %w", err)
	}

	// Log if this is a multi-wallet cluster
	if cluster.WalletCount > 1 {
		p.log.WithFields(logrus.Fields{
			"cluster_id":     cluster.ClusterID,
			"funding_source": fundingSource,
			"wallet_count":   cluster.WalletCount,
		}).Info("Detected wallet cluster")
	}

	return nil
}

// detectCoordinatedTrade checks if a trade is part of coordinated activity
func (p *Processor) detectCoordinatedTrade(ctx context.Context, trade *dataapi.Trade, walletAddress string) (bool, string, error) {
	// Get funding source for this wallet
	fundingSource, err := p.db.GetWalletFundingSource(ctx, walletAddress)
	if err != nil {
		return false, "", err
	}
	if fundingSource == nil {
		return false, "", nil // No funding source tracked
	}

	// Get cluster
	cluster, err := p.db.GetWalletClusterBySource(ctx, fundingSource.FundingSource)
	if err != nil {
		return false, "", err
	}
	if cluster == nil || cluster.WalletCount <= 1 {
		return false, "", nil // Not a multi-wallet cluster
	}

	// Get all wallets in this cluster
	clusterWallets, err := p.db.GetWalletsByFundingSource(ctx, fundingSource.FundingSource)
	if err != nil {
		return false, "", err
	}

	// Get recent trades from cluster wallets (configurable lookback period)
	lookbackTS := trade.Timestamp - int64(p.cfg.ClusterLookbackHours*3600)
	var walletAddrs []string
	for _, w := range clusterWallets {
		walletAddrs = append(walletAddrs, w.WalletAddress)
	}

	recentTrades, err := p.db.GetRecentTradesForCluster(ctx, walletAddrs, lookbackTS)
	if err != nil {
		return false, "", err
	}

	// Check for coordinated activity on this market
	var sameMarketTrades []storage.TradeSeen
	for _, t := range recentTrades {
		if t.ConditionID == trade.ConditionID {
			sameMarketTrades = append(sameMarketTrades, t)
		}
	}

	// Flag as coordinated if multiple wallets traded this market within 1 hour
	if len(sameMarketTrades) >= 2 {
		var firstTS, lastTS int64 = trade.Timestamp, trade.Timestamp
		uniqueWallets := make(map[string]bool)
		totalNotional := 0.0

		for _, t := range sameMarketTrades {
			uniqueWallets[t.ProxyWallet] = true
			totalNotional += t.NotionalUSD
			if t.TimestampSec < firstTS {
				firstTS = t.TimestampSec
			}
			if t.TimestampSec > lastTS {
				lastTS = t.TimestampSec
			}
		}

		timeWindowSec := int(lastTS - firstTS)
		if timeWindowSec <= 3600 && len(uniqueWallets) >= 2 {
			// Record coordinated trade
			coordTrade := &storage.CoordinatedTrade{
				ClusterID:        cluster.ClusterID,
				ConditionID:      trade.ConditionID,
				WalletCount:      len(uniqueWallets),
				TotalNotionalUSD: totalNotional,
				TimeWindowSec:    timeWindowSec,
				FirstTradeTS:     firstTS,
				LastTradeTS:      lastTS,
				MarketTitle:      trade.Title,
			}
			if err := p.db.InsertCoordinatedTrade(ctx, coordTrade); err != nil {
				p.log.WithError(err).Warn("Failed to insert coordinated trade")
			}

			p.log.WithFields(logrus.Fields{
				"cluster_id":     cluster.ClusterID,
				"condition_id":   trade.ConditionID,
				"wallet_count":   len(uniqueWallets),
				"time_window":    timeWindowSec,
				"total_notional": totalNotional,
			}).Warn("Detected coordinated trading activity")

			return true, cluster.ClusterID, nil
		}
	}

	return false, "", nil
}

// checkTradeVelocity checks how many trades a wallet made in the recent time window
func (p *Processor) checkTradeVelocity(ctx context.Context, walletAddress string, currentTradeTS int64) (int, error) {
	// Calculate lookback timestamp based on velocity window
	lookbackTS := currentTradeTS - int64(p.cfg.VelocityWindowMinutes*60)

	// Get recent trades for this wallet
	recentTrades, err := p.db.GetRecentTradesForWallet(ctx, walletAddress, lookbackTS)
	if err != nil {
		return 0, fmt.Errorf("get recent trades: %w", err)
	}

	// Count trades in the window (including the current one)
	count := len(recentTrades) + 1

	return count, nil
}

// checkNetPositionConcentration checks if wallet is heavily concentrated on one side of a market
// Returns a ratio from 0.0 to 1.0 indicating concentration (1.0 = 100% on one side)
func (p *Processor) checkNetPositionConcentration(ctx context.Context, walletAddress, conditionID string, currentTS int64, currentNotional float64, currentSide string) (float64, error) {
	// Calculate current window
	windowHrs := int64(p.cfg.NetPositionWindowHrs)
	windowStartTS := (currentTS / (windowHrs * 3600)) * (windowHrs * 3600)

	// Get existing net position
	netPos, err := p.db.GetNetPosition(ctx, walletAddress, conditionID, windowStartTS)
	if err != nil {
		return 0, fmt.Errorf("get net position: %w", err)
	}

	// Calculate projected net position after this trade
	var projectedNet float64
	if netPos != nil {
		projectedNet = netPos.NetNotionalUSD
	}

	// Add current trade to projection
	if currentSide == "BUY" {
		projectedNet += currentNotional
	} else {
		projectedNet -= currentNotional
	}

	// Calculate total volume (absolute values)
	var totalVolume float64
	if netPos != nil {
		// Approximate total volume from net position and trade count
		// This is an estimate since we don't store gross volume
		totalVolume = math.Abs(netPos.NetNotionalUSD) * float64(netPos.TradeCount) / float64(max(netPos.TradeCount-1, 1))
	}
	totalVolume += currentNotional

	if totalVolume == 0 {
		return 0, nil
	}

	// Concentration is |net| / total volume
	concentration := math.Abs(projectedNet) / totalVolume

	return concentration, nil
}

// getClusterMultiplier returns a suspicion score multiplier based on cluster activity
func (p *Processor) getClusterMultiplier(ctx context.Context, walletAddress string) float64 {
	fundingSource, err := p.db.GetWalletFundingSource(ctx, walletAddress)
	if err != nil || fundingSource == nil {
		return 1.0
	}

	cluster, err := p.db.GetWalletClusterBySource(ctx, fundingSource.FundingSource)
	if err != nil || cluster == nil {
		return 1.0
	}

	// Multiplier based on cluster size
	// 2 wallets = 1.5x, 5 wallets = 2.0x, 10+ wallets = 3.0x
	if cluster.WalletCount >= 10 {
		return 3.0
	} else if cluster.WalletCount >= 5 {
		return 2.0
	} else if cluster.WalletCount >= 2 {
		return 1.5
	}

	return 1.0
}

// MarketInfo holds resolved market information
type MarketInfo struct {
	Title        string
	Slug         string
	URL          string
	Category     string
	EndDate      int64   // Unix timestamp
	LiquidityNum float64 // Market liquidity for ratio analysis
	VolumeNum    float64 // Market volume
}
