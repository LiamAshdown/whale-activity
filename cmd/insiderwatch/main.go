package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/liamashdown/insiderwatch/internal/alerts"
	"github.com/liamashdown/insiderwatch/internal/config"
	"github.com/liamashdown/insiderwatch/internal/metrics"
	"github.com/liamashdown/insiderwatch/internal/polymarket/dataapi"
	"github.com/liamashdown/insiderwatch/internal/polymarket/gammaapi"
	"github.com/liamashdown/insiderwatch/internal/processor"
	"github.com/liamashdown/insiderwatch/internal/storage"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
	log := logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(logrus.InfoLevel)

	log.Info("Starting insiderwatch service...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.WithError(err).Fatal("Failed to load configuration")
	}

	log.WithFields(logrus.Fields{
		"environment":       cfg.Environment,
		"big_trade_usd":     cfg.BigTradeUSD,
		"new_wallet_days":   cfg.NewWalletDaysMax,
		"poll_interval_sec": cfg.PollIntervalSec,
		"alert_mode":        cfg.AlertMode,
	}).Info("Configuration loaded")

	// Initialize database
	db, err := storage.New(cfg, log)
	if err != nil {
		log.WithError(err).Fatal("Failed to connect to database")
	}
	defer db.Close()

	log.Info("Database connected")

	// Run auto-migration
	if err := db.AutoMigrate(); err != nil {
		log.WithError(err).Fatal("Failed to run database migrations")
	}

	log.Info("Database migrations complete")

	// Initialize API clients
	dataClient := dataapi.NewClient(cfg)
	gammaClient := gammaapi.NewClient(cfg)

	log.Info("API clients initialized")

	// Initialize alert sender
	alertSender := createAlertSender(cfg, log)

	log.WithField("alert_mode", cfg.AlertMode).Info("Alert sender initialized")

	// Initialize processor
	proc := processor.New(cfg, db, dataClient, gammaClient, alertSender, log)

	// Start HTTP server (health + metrics)
	go startHTTPServer(cfg.HealthPort, log)

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start polling loop
	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()

	// Start daily win rate recalculation ticker
	winRateTicker := time.NewTicker(24 * time.Hour)
	defer winRateTicker.Stop()

	log.Info("Starting trade processing loop")

	// Process immediately on startup
	if err := proc.ProcessTrades(ctx); err != nil {
		log.WithError(err).Error("Error processing trades")
	}

	// Run win rate calculation on startup (async)
	go func() {
		if err := proc.RecalculateWinRates(ctx); err != nil {
			log.WithError(err).Error("Error calculating win rates on startup")
		}
	}()

	for {
		select {
		case <-ticker.C:
			if err := proc.ProcessTrades(ctx); err != nil {
				log.WithError(err).Error("Error processing trades")
			}
		case <-winRateTicker.C:
			// Run win rate recalculation daily
			go func() {
				if err := proc.RecalculateWinRates(ctx); err != nil {
					log.WithError(err).Error("Error recalculating win rates")
				}
			}()
		case sig := <-sigChan:
			log.WithField("signal", sig).Info("Received shutdown signal")
			cancel()
			log.Info("Graceful shutdown complete")
			return
		case <-ctx.Done():
			log.Info("Context cancelled, shutting down")
			return
		}
	}
}

func createAlertSender(cfg *config.Config, log *logrus.Logger) alerts.Sender {
	// Parse comma-separated alert modes
	modes := strings.Split(cfg.AlertMode, ",")
	
	// Trim whitespace from each mode
	for i, mode := range modes {
		modes[i] = strings.TrimSpace(mode)
	}
	
	// If single mode, return that sender directly
	if len(modes) == 1 {
		switch modes[0] {
		case "log":
			return alerts.NewLogSender(log)

		case "discord":
			// Create senders for all webhook URLs
			if len(cfg.DiscordWebhookURLs) == 0 {
				log.Warn("Discord mode specified but no webhook URLs configured")
				return alerts.NewLogSender(log)
			}
			if len(cfg.DiscordWebhookURLs) == 1 {
				return alerts.NewDiscordSender(cfg.DiscordWebhookURLs[0])
			}
			// Multiple webhooks - use multi sender
			discordSenders := []alerts.Sender{}
			for _, url := range cfg.DiscordWebhookURLs {
				discordSenders = append(discordSenders, alerts.NewDiscordSender(url))
			}
			return alerts.NewMultiSender(discordSenders...)

		case "smtp":
			return alerts.NewSMTPSender(
				cfg.SMTPHost,
				cfg.SMTPPort,
				cfg.SMTPUser,
				cfg.SMTPPassword,
				cfg.SMTPFrom,
				cfg.SMTPTo,
			)

		default:
			log.WithField("alert_mode", modes[0]).Warn("Unknown alert mode, using log")
			return alerts.NewLogSender(log)
		}
	}
	
	// Multiple modes - create multi sender
	senders := []alerts.Sender{}
	
	for _, mode := range modes {
		switch mode {
		case "log":
			senders = append(senders, alerts.NewLogSender(log))
		case "discord":
			if len(cfg.DiscordWebhookURLs) > 0 {
				// Add a sender for each webhook URL
				for _, url := range cfg.DiscordWebhookURLs {
					senders = append(senders, alerts.NewDiscordSender(url))
				}
			} else {
				log.Warn("Discord mode specified but DISCORD_WEBHOOK_URLS not set")
			}
		case "smtp":
			if cfg.SMTPHost != "" {
				senders = append(senders, alerts.NewSMTPSender(
					cfg.SMTPHost,
					cfg.SMTPPort,
					cfg.SMTPUser,
					cfg.SMTPPassword,
					cfg.SMTPFrom,
					cfg.SMTPTo,
				))
			} else {
				log.Warn("SMTP mode specified but SMTP_HOST not set")
			}
		default:
			log.WithField("mode", mode).Warn("Unknown alert mode, skipping")
		}
	}
	
	if len(senders) == 0 {
		log.Warn("No valid alert senders configured, using log")
		return alerts.NewLogSender(log)
	}
	
	return alerts.NewMultiSender(senders...)
}

func startHTTPServer(port int, log *logrus.Logger) {
	mux := http.NewServeMux()

	// Health check endpoints
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		metrics.RecordHealthCheck(true)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy"}`)
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		metrics.RecordHealthCheck(true)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ready"}`)
	})

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	log.WithField("port", port).Info("Starting HTTP server (health + metrics)")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.WithError(err).Error("HTTP server failed")
	}
}
