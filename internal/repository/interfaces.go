package repository

import (
	"context"
	"time"
)

// MetricRecord represents a single second's transaction metric.
type MetricRecord struct {
	ApplicationName     string
	SourceName          string
	MetricTime          time.Time // UTC DATETIME2(0)
	TotalTransactions   int
	SuccessTransactions int
	FailedTransactions  int
	IsComplete          bool
	CollectedAt         time.Time
}

// Checkpoint represents the current polling progress watermark for an index pattern.
type Checkpoint struct {
	SourceName         string
	IndexPattern       string
	LastProcessedUntil time.Time // UTC DATETIME2(3)
	LastSuccessAt      time.Time
	UpdatedAt          time.Time
}

// SummaryRecord represents calendar-based aggregated performance metrics.
type SummaryRecord struct {
	ApplicationName   string
	SourceName        string
	PeriodType        string    // "daily", "weekly", "monthly"
	PeriodStart       time.Time // UTC
	PeriodEnd         time.Time // UTC
	MinTPS            int
	MaxTPS            int
	AvgTPS            float64
	TotalTransactions int64
	CalculatedAt      time.Time
}

// WorkerLog represents an audit / operational log entry.
type WorkerLog struct {
	LogID      int64
	SourceName string
	EventType  string // "POLL_SUCCESS", "POLL_ERROR", "DB_ERROR", "RECOVERY"
	Message    string
	LagSeconds *int
	CreatedAt  time.Time
}

// Repository defines the contract for persisting TPS data into SQL Server.
type Repository interface {
	PingContext(ctx context.Context) error
	GetCheckpoint(ctx context.Context, sourceName, indexPattern string) (*Checkpoint, error)
	SaveMetricsAndAdvanceCheckpoint(ctx context.Context, metrics []MetricRecord, cp Checkpoint) error
	CalculateAndSaveSummary(ctx context.Context, appName, sourceName, periodType string, start, end time.Time) (*SummaryRecord, error)
	LogWorkerEvent(ctx context.Context, sourceName, eventType, message string, lagSeconds *int) error
}
