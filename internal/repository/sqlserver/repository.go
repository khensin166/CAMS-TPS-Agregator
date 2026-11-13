package sqlserver

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"cams-tps-aggregator/internal/repository"
)

// SQLRepository implements repository.Repository using Microsoft SQL Server.
type SQLRepository struct {
	db             *sql.DB
	commandTimeout time.Duration
	logger         *slog.Logger
}

// NewSQLRepository constructs a new SQL Server repository.
func NewSQLRepository(db *sql.DB, timeout time.Duration, log *slog.Logger) *SQLRepository {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &SQLRepository{
		db:             db,
		commandTimeout: timeout,
		logger:         log,
	}
}

// PingContext checks whether the database is responsive.
func (r *SQLRepository) PingContext(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, r.commandTimeout)
	defer cancel()
	return r.db.PingContext(pingCtx)
}

// GetCheckpoint retrieves the last saved checkpoint for a given source and index pattern.
func (r *SQLRepository) GetCheckpoint(ctx context.Context, sourceName, indexPattern string) (*repository.Checkpoint, error) {
	queryCtx, cancel := context.WithTimeout(ctx, r.commandTimeout)
	defer cancel()

	query := `
		SELECT source_name, index_pattern, last_processed_until, last_success_at, updated_at
		FROM dbo.tps_checkpoints
		WHERE source_name = @p1 AND index_pattern = @p2;
	`

	var cp repository.Checkpoint
	err := r.db.QueryRowContext(queryCtx, query, sourceName, indexPattern).Scan(
		&cp.SourceName,
		&cp.IndexPattern,
		&cp.LastProcessedUntil,
		&cp.LastSuccessAt,
		&cp.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No checkpoint yet
		}
		return nil, fmt.Errorf("failed to query checkpoint: %w", err)
	}

	return &cp, nil
}

// SaveMetricsAndAdvanceCheckpoint atomically writes 1s metrics and updates the checkpoint watermark in a single SQL transaction.
func (r *SQLRepository) SaveMetricsAndAdvanceCheckpoint(ctx context.Context, metrics []repository.MetricRecord, cp repository.Checkpoint) error {
	txCtx, cancel := context.WithTimeout(ctx, r.commandTimeout)
	defer cancel()

	tx, err := r.db.BeginTx(txCtx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback() // Safe if already committed
	}()

	// 1. Upsert Metrics if any
	if len(metrics) > 0 {
		metricUpsertQuery := `
			MERGE dbo.tps_metrics WITH (HOLDLOCK) AS target
			USING (VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8)) AS source 
				(application_name, source_name, metric_time, total_transactions, success_transactions, failed_transactions, is_complete, collected_at)
			ON (target.application_name = source.application_name 
				AND target.source_name = source.source_name 
				AND target.metric_time = source.metric_time)
			WHEN MATCHED THEN
				UPDATE SET 
					target.total_transactions = source.total_transactions,
					target.success_transactions = source.success_transactions,
					target.failed_transactions = source.failed_transactions,
					target.is_complete = source.is_complete,
					target.collected_at = source.collected_at
			WHEN NOT MATCHED THEN
				INSERT (application_name, source_name, metric_time, total_transactions, success_transactions, failed_transactions, is_complete, collected_at)
				VALUES (source.application_name, source.source_name, source.metric_time, source.total_transactions, source.success_transactions, source.failed_transactions, source.is_complete, source.collected_at);
		`

		stmt, err := tx.PrepareContext(txCtx, metricUpsertQuery)
		if err != nil {
			return fmt.Errorf("failed to prepare metric upsert statement: %w", err)
		}
		defer stmt.Close()

		for _, m := range metrics {
			_, err := stmt.ExecContext(txCtx,
				m.ApplicationName,
				m.SourceName,
				m.MetricTime.UTC(),
				m.TotalTransactions,
				m.SuccessTransactions,
				m.FailedTransactions,
				m.IsComplete,
				m.CollectedAt.UTC(),
			)
			if err != nil {
				return fmt.Errorf("failed to upsert metric for %s at %s: %w", m.SourceName, m.MetricTime.Format(time.RFC3339), err)
			}
		}
	}

	// 2. Advance Checkpoint in the same transaction
	cpUpsertQuery := `
		MERGE dbo.tps_checkpoints WITH (HOLDLOCK) AS target
		USING (VALUES (@p1, @p2, @p3, @p4, @p5)) AS source 
			(source_name, index_pattern, last_processed_until, last_success_at, updated_at)
		ON (target.source_name = source.source_name AND target.index_pattern = source.index_pattern)
		WHEN MATCHED THEN
			UPDATE SET 
				target.last_processed_until = source.last_processed_until,
				target.last_success_at = source.last_success_at,
				target.updated_at = source.updated_at
		WHEN NOT MATCHED THEN
			INSERT (source_name, index_pattern, last_processed_until, last_success_at, updated_at)
			VALUES (source.source_name, source.index_pattern, source.last_processed_until, source.last_success_at, source.updated_at);
	`

	_, err = tx.ExecContext(txCtx, cpUpsertQuery,
		cp.SourceName,
		cp.IndexPattern,
		cp.LastProcessedUntil.UTC(),
		cp.LastSuccessAt.UTC(),
		cp.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("failed to advance checkpoint for %s: %w", cp.SourceName, err)
	}

	// 3. Commit atomically
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// CalculateAndSaveSummary aggregates calendar-based TPS summaries and writes them to dbo.tps_summary.
func (r *SQLRepository) CalculateAndSaveSummary(ctx context.Context, appName, sourceName, periodType string, start, end time.Time) (*repository.SummaryRecord, error) {
	calcCtx, cancel := context.WithTimeout(ctx, r.commandTimeout)
	defer cancel()

	durationSeconds := end.Sub(start).Seconds()
	if durationSeconds <= 0 {
		return nil, fmt.Errorf("invalid period range: end time %v must be after start time %v", end, start)
	}

	query := `
		SELECT 
			ISNULL(MIN(total_transactions), 0) AS min_tps,
			ISNULL(MAX(total_transactions), 0) AS max_tps,
			ISNULL(SUM(CAST(total_transactions AS BIGINT)), 0) AS total_trx
		FROM dbo.tps_metrics
		WHERE application_name = @p1 
			AND source_name = @p2 
			AND metric_time >= @p3 
			AND metric_time < @p4;
	`

	var minTPS, maxTPS int
	var totalTrx int64

	err := r.db.QueryRowContext(calcCtx, query, appName, sourceName, start.UTC(), end.UTC()).Scan(
		&minTPS,
		&maxTPS,
		&totalTrx,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate summary for %s/%s: %w", appName, sourceName, err)
	}

	avgTPS := float64(totalTrx) / durationSeconds
	now := time.Now().UTC()

	record := &repository.SummaryRecord{
		ApplicationName:   appName,
		SourceName:        sourceName,
		PeriodType:        periodType,
		PeriodStart:       start.UTC(),
		PeriodEnd:         end.UTC(),
		MinTPS:            minTPS,
		MaxTPS:            maxTPS,
		AvgTPS:            avgTPS,
		TotalTransactions: totalTrx,
		CalculatedAt:      now,
	}

	upsertSummaryQuery := `
		MERGE dbo.tps_summary WITH (HOLDLOCK) AS target
		USING (VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9, @p10)) AS source 
			(application_name, source_name, period_type, period_start, period_end, min_tps, max_tps, avg_tps, total_transactions, calculated_at)
		ON (target.application_name = source.application_name 
			AND target.source_name = source.source_name 
			AND target.period_type = source.period_type 
			AND target.period_start = source.period_start)
		WHEN MATCHED THEN
			UPDATE SET 
				target.period_end = source.period_end,
				target.min_tps = source.min_tps,
				target.max_tps = source.max_tps,
				target.avg_tps = source.avg_tps,
				target.total_transactions = source.total_transactions,
				target.calculated_at = source.calculated_at
		WHEN NOT MATCHED THEN
			INSERT (application_name, source_name, period_type, period_start, period_end, min_tps, max_tps, avg_tps, total_transactions, calculated_at)
			VALUES (source.application_name, source.source_name, source.period_type, source.period_start, source.period_end, source.min_tps, source.max_tps, source.avg_tps, source.total_transactions, source.calculated_at);
	`

	_, err = r.db.ExecContext(calcCtx, upsertSummaryQuery,
		record.ApplicationName,
		record.SourceName,
		record.PeriodType,
		record.PeriodStart,
		record.PeriodEnd,
		record.MinTPS,
		record.MaxTPS,
		record.AvgTPS,
		record.TotalTransactions,
		record.CalculatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to persist summary: %w", err)
	}

	return record, nil
}

// LogWorkerEvent logs an operational event into dbo.tps_worker_logs.
func (r *SQLRepository) LogWorkerEvent(ctx context.Context, sourceName, eventType, message string, lagSeconds *int) error {
	logCtx, cancel := context.WithTimeout(ctx, r.commandTimeout)
	defer cancel()

	query := `
		INSERT INTO dbo.tps_worker_logs (source_name, event_type, message, lag_seconds, created_at)
		VALUES (@p1, @p2, @p3, @p4, SYSUTCDATETIME());
	`

	_, err := r.db.ExecContext(logCtx, query, sourceName, eventType, message, lagSeconds)
	if err != nil {
		return fmt.Errorf("failed to log worker event: %w", err)
	}

	return nil
}

