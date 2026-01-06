package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Trade processing metrics
	TradesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "insiderwatch_trades_processed_total",
			Help: "Total number of trades processed",
		},
		[]string{"status"}, // success, duplicate, filtered
	)

	TradeProcessingDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "insiderwatch_trade_processing_duration_seconds",
			Help:    "Duration of trade processing",
			Buckets: prometheus.DefBuckets,
		},
	)

	// Alert metrics
	AlertsTriggered = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "insiderwatch_alerts_triggered_total",
			Help: "Total number of alerts triggered",
		},
		[]string{"severity"}, // critical, high, medium, low
	)

	AlertsSent = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "insiderwatch_alerts_sent_total",
			Help: "Total number of alerts sent",
		},
		[]string{"status", "type"}, // success/error, discord/smtp/log
	)

	AlertsSuppressed = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "insiderwatch_alerts_suppressed_total",
			Help: "Total number of alerts suppressed due to cooldown",
		},
	)

	// API metrics
	APIRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "insiderwatch_api_requests_total",
			Help: "Total number of API requests",
		},
		[]string{"api", "endpoint", "status"}, // data/gamma, /trades, success/error
	)

	APIRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "insiderwatch_api_request_duration_seconds",
			Help:    "Duration of API requests",
			Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"api", "endpoint"},
	)

	// Database metrics
	DatabaseQueries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "insiderwatch_database_queries_total",
			Help: "Total number of database queries",
		},
		[]string{"operation", "status"}, // get/insert/update, success/error
	)

	DatabaseQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "insiderwatch_database_query_duration_seconds",
			Help:    "Duration of database queries",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
		[]string{"operation"},
	)

	// Win rate calculation metrics
	WinRateCalculations = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "insiderwatch_win_rate_calculations_total",
			Help: "Total number of win rate calculation runs",
		},
	)

	MarketsResolved = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "insiderwatch_markets_resolved_total",
			Help: "Total number of markets resolved",
		},
	)

	WinRateCalculationDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "insiderwatch_win_rate_calculation_duration_seconds",
			Help:    "Duration of win rate calculation",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		},
	)

	// Suspicion score metrics
	SuspicionScores = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "insiderwatch_suspicion_scores",
			Help:    "Distribution of calculated suspicion scores",
			Buckets: []float64{100, 500, 1000, 5000, 10000, 25000, 50000, 100000, 500000, 1000000},
		},
	)

	// System health
	HealthChecks = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "insiderwatch_health_checks_total",
			Help: "Total number of health check requests",
		},
		[]string{"status"}, // healthy/unhealthy
	)
)

// RecordTradeProcessing records trade processing metrics
func RecordTradeProcessing(duration time.Duration, status string) {
	TradesProcessed.WithLabelValues(status).Inc()
	TradeProcessingDuration.Observe(duration.Seconds())
}

// RecordAlert records alert metrics
func RecordAlert(severity, sendStatus, alertType string, suppressed bool) {
	if suppressed {
		AlertsSuppressed.Inc()
		return
	}
	
	AlertsTriggered.WithLabelValues(severity).Inc()
	AlertsSent.WithLabelValues(sendStatus, alertType).Inc()
}

// RecordAPIRequest records API request metrics
func RecordAPIRequest(api, endpoint string, duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	APIRequests.WithLabelValues(api, endpoint, status).Inc()
	APIRequestDuration.WithLabelValues(api, endpoint).Observe(duration.Seconds())
}

// RecordDatabaseQuery records database query metrics
func RecordDatabaseQuery(operation string, duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	DatabaseQueries.WithLabelValues(operation, status).Inc()
	DatabaseQueryDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// RecordWinRateCalculation records win rate calculation metrics
func RecordWinRateCalculation(duration time.Duration, marketsResolved int) {
	WinRateCalculations.Inc()
	MarketsResolved.Add(float64(marketsResolved))
	WinRateCalculationDuration.Observe(duration.Seconds())
}

// RecordSuspicionScore records suspicion score for histogram
func RecordSuspicionScore(score float64) {
	SuspicionScores.Observe(score)
}

// RecordHealthCheck records health check status
func RecordHealthCheck(healthy bool) {
	status := "healthy"
	if !healthy {
		status = "unhealthy"
	}
	HealthChecks.WithLabelValues(status).Inc()
}
