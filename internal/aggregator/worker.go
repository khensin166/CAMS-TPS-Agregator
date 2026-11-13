package aggregator

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"cams-tps-aggregator/internal/config"
	"cams-tps-aggregator/internal/elasticsearch"
	"cams-tps-aggregator/internal/metrics"
	"cams-tps-aggregator/internal/repository"
)

// HealthReporter defines an interface for reporting worker status.
type HealthReporter interface {
	UpdateSourceStatus(source, status string, lag int64, lastErr string)
}

// Worker executes independent polling and metric aggregation for a single source.
type Worker struct {
	appName           string
	cfg               config.WorkerConfig
	esClient          elasticsearch.Client
	repo              repository.Repository
	metrics           *metrics.Metrics
	health            HealthReporter
	logger            *slog.Logger
	currentWatermark  time.Time
	consecutiveErrors int
	maxBackoff        time.Duration
}

// NewWorker constructs a new independent worker instance.
func NewWorker(
	appName string,
	cfg config.WorkerConfig,
	esClient elasticsearch.Client,
	repo repository.Repository,
	promMetrics *metrics.Metrics,
	health HealthReporter,
	log *slog.Logger,
) *Worker {
	if log == nil {
		log = slog.Default()
	}

	return &Worker{
		appName:     appName,
		cfg:         cfg,
		esClient:    esClient,
		repo:        repo,
		metrics:     promMetrics,
		health:      health,
		logger:      log.With(slog.String("worker", cfg.SourceName)),
		maxBackoff:  60 * time.Second,
	}
}

// Start begins the worker's polling loop until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	if !w.cfg.Enabled {
		w.logger.Info("Worker is disabled by configuration, skipping start")
		if w.health != nil {
			w.health.UpdateSourceStatus(w.cfg.SourceName, "DISABLED", 0, "")
		}
		if w.metrics != nil {
			w.metrics.Up.WithLabelValues(w.cfg.SourceName).Set(0)
		}
		return
	}

	w.logger.Info("Starting worker poller",
		slog.String("source", w.cfg.SourceName),
		slog.String("index_pattern", w.cfg.IndexPattern),
		slog.Duration("poll_interval", w.cfg.PollInterval),
		slog.Duration("watermark_delay", w.cfg.WatermarkDelay),
		slog.Duration("overlap_window", w.cfg.OverlapWindow),
	)

	if w.metrics != nil {
		w.metrics.Up.WithLabelValues(w.cfg.SourceName).Set(1)
	}

	// 1. Initialize Checkpoint
	w.initCheckpoint(ctx)

	// 2. Start Poller Loop
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	// Run initial cycle immediately
	w.pollOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Worker stopped cleanly due to context cancellation")
			if w.metrics != nil {
				w.metrics.Up.WithLabelValues(w.cfg.SourceName).Set(0)
			}
			return
		case <-ticker.C:
			w.pollOnce(ctx)
		}
	}
}

// initCheckpoint restores or initializes the processing watermark.
func (w *Worker) initCheckpoint(ctx context.Context) {
	now := time.Now().UTC()
	defaultWatermark := now.Add(-w.cfg.WatermarkDelay).Add(-5 * time.Minute).Truncate(time.Second)

	cp, err := w.repo.GetCheckpoint(ctx, w.cfg.SourceName, w.cfg.IndexPattern)
	if err != nil || cp == nil {
		w.logger.Warn("No existing checkpoint found or query failed, initializing new watermark",
			slog.Time("initial_watermark", defaultWatermark),
			slog.Any("error", err),
		)
		w.currentWatermark = defaultWatermark
	} else {
		w.logger.Info("Restored existing checkpoint",
			slog.Time("last_processed_until", cp.LastProcessedUntil),
			slog.Time("last_success_at", cp.LastSuccessAt),
		)
		w.currentWatermark = cp.LastProcessedUntil
	}
}

// pollOnce executes a single polling, querying, and checkpoint advancement step.
func (w *Worker) pollOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	startTime := time.Now()
	now := time.Now().UTC()
	targetEnd := now.Add(-w.cfg.WatermarkDelay).Truncate(time.Second)

	// Guard: Do not query if targetEnd has not progressed past current watermark
	if !targetEnd.After(w.currentWatermark) {
		w.logger.Debug("Target end time has not progressed beyond watermark, waiting for next tick",
			slog.Time("current_watermark", w.currentWatermark),
			slog.Time("target_end", targetEnd),
		)
		return
	}

	queryStart := w.currentWatermark.Add(-w.cfg.OverlapWindow).Truncate(time.Second)

	// Check if we are recovering (lag > 5 minutes)
	lagSeconds := int64(now.Sub(w.currentWatermark).Seconds())
	isRecovering := lagSeconds > 300
	if w.metrics != nil {
		val := 0.0
		if isRecovering {
			val = 1.0
		}
		w.metrics.RecoveryMode.WithLabelValues(w.cfg.SourceName).Set(val)
	}

	w.logger.Debug("Executing aggregation query",
		slog.Time("query_start", queryStart),
		slog.Time("query_end", targetEnd),
		slog.Int64("lag_seconds", lagSeconds),
	)

	// 1. Query Elasticsearch
	aggResp, err := w.esClient.Query1sHistogram(ctx, w.cfg.IndexPattern, queryStart, targetEnd, w.cfg.TimestampField)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		w.handleQueryError(ctx, err, lagSeconds)
		return
	}

	// 2. Parse Aggregation Buckets
	records, err := ProcessAggregationResponse(w.appName, w.cfg.SourceName, aggResp, queryStart, targetEnd)
	if err != nil {
		w.handleProcessingError(err, lagSeconds)
		return
	}

	// 3. Atomically Save Metrics and Advance Checkpoint
	newCheckpoint := repository.Checkpoint{
		SourceName:         w.cfg.SourceName,
		IndexPattern:       w.cfg.IndexPattern,
		LastProcessedUntil: targetEnd,
		LastSuccessAt:      now,
		UpdatedAt:          now,
	}

	if err := w.repo.SaveMetricsAndAdvanceCheckpoint(ctx, records, newCheckpoint); err != nil {
		if ctx.Err() != nil {
			return
		}
		w.handleDatabaseError(ctx, err, lagSeconds)
		return
	}

	// 4. Successful Commit: Advance local state and emit metrics
	w.currentWatermark = targetEnd
	w.consecutiveErrors = 0
	duration := time.Since(startTime).Seconds()

	if w.health != nil {
		w.health.UpdateSourceStatus(w.cfg.SourceName, "UP", lagSeconds, "")
	}

	if w.metrics != nil {
		w.metrics.ProcessingDuration.WithLabelValues(w.cfg.SourceName).Observe(duration)
		w.metrics.ProcessingLag.WithLabelValues(w.cfg.SourceName).Set(float64(lagSeconds))
		w.metrics.LastSuccessTimestamp.WithLabelValues(w.cfg.SourceName).Set(float64(now.Unix()))
		w.metrics.RecordsProcessed.WithLabelValues(w.cfg.SourceName, "SUCCESS").Add(float64(len(records)))

		if len(records) > 0 {
			latestRecord := records[len(records)-1]
			w.metrics.CurrentTPS.WithLabelValues(w.cfg.SourceName).Set(float64(latestRecord.TotalTransactions))
		}
	}

	w.logger.Info("Successfully committed metrics batch",
		slog.Int("records_count", len(records)),
		slog.Time("advanced_watermark", targetEnd),
		slog.Float64("duration_sec", duration),
		slog.Int64("lag_sec", lagSeconds),
	)
}

func (w *Worker) handleQueryError(ctx context.Context, err error, lag int64) {
	w.consecutiveErrors++
	w.logger.Error("Elasticsearch query failed, retaining checkpoint for retry",
		slog.Any("error", err),
		slog.Int("consecutive_errors", w.consecutiveErrors),
		slog.Int64("lag_seconds", lag),
	)

	if w.metrics != nil {
		w.metrics.QueryErrors.WithLabelValues(w.cfg.SourceName, "QUERY_FAILED").Inc()
	}
	if w.health != nil {
		w.health.UpdateSourceStatus(w.cfg.SourceName, "DEGRADED", lag, fmt.Sprintf("ES Query Error: %v", err))
	}

	w.applyBackoff(ctx)
}

func (w *Worker) handleProcessingError(err error, lag int64) {
	w.consecutiveErrors++
	w.logger.Error("Failed to parse aggregation response, retaining checkpoint",
		slog.Any("error", err),
		slog.Int64("lag_seconds", lag),
	)

	if w.metrics != nil {
		w.metrics.QueryErrors.WithLabelValues(w.cfg.SourceName, "PARSE_FAILED").Inc()
	}
	if w.health != nil {
		w.health.UpdateSourceStatus(w.cfg.SourceName, "DEGRADED", lag, fmt.Sprintf("Parse Error: %v", err))
	}
}

func (w *Worker) handleDatabaseError(ctx context.Context, err error, lag int64) {
	w.consecutiveErrors++
	w.logger.Error("Database commit failed, retaining checkpoint for retry",
		slog.Any("error", err),
		slog.Int("consecutive_errors", w.consecutiveErrors),
		slog.Int64("lag_seconds", lag),
	)

	if w.metrics != nil {
		w.metrics.DatabaseErrors.WithLabelValues(w.cfg.SourceName, "TRANSACTION_FAILED").Inc()
	}
	if w.health != nil {
		w.health.UpdateSourceStatus(w.cfg.SourceName, "DEGRADED", lag, fmt.Sprintf("DB Commit Error: %v", err))
	}

	w.applyBackoff(ctx)
}

func (w *Worker) applyBackoff(ctx context.Context) {
	backoffFactor := math.Pow(2, float64(w.consecutiveErrors))
	backoff := time.Duration(float64(w.cfg.PollInterval) * backoffFactor)
	if backoff > w.maxBackoff {
		backoff = w.maxBackoff
	}
	w.logger.Warn("Applying exponential backoff before next retry", slog.Duration("backoff", backoff))
	select {
	case <-ctx.Done():
	case <-time.After(backoff):
	}
}
