package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds the registered Prometheus metric collectors.
type Metrics struct {
	Up                   *prometheus.GaugeVec
	LastSuccessTimestamp *prometheus.GaugeVec
	ProcessingLag        *prometheus.GaugeVec
	CurrentTPS           *prometheus.GaugeVec
	RecoveryMode         *prometheus.GaugeVec
	QueryErrors          *prometheus.CounterVec
	RecordsProcessed     *prometheus.CounterVec
	DatabaseErrors       *prometheus.CounterVec
	ProcessingDuration   *prometheus.HistogramVec
}

// NewMetrics initializes and registers all Prometheus metrics with a custom registry or default.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	factory := promauto.With(reg)

	m := &Metrics{
		Up: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tps_aggregator_up",
				Help: "Indicates whether the worker for a given source is operational (1 for running, 0 for halted).",
			},
			[]string{"source"},
		),
		LastSuccessTimestamp: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tps_aggregator_last_success_timestamp_seconds",
				Help: "Unix epoch timestamp of the last successful aggregation and database commit.",
			},
			[]string{"source"},
		),
		ProcessingLag: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tps_aggregator_processing_lag_seconds",
				Help: "Delay in seconds between current time and the last processed watermark.",
			},
			[]string{"source"},
		),
		CurrentTPS: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tps_aggregator_current_tps",
				Help: "Latest measured transactions per second for the source.",
			},
			[]string{"source"},
		),
		RecoveryMode: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tps_aggregator_recovery_mode",
				Help: "Indicates whether the worker is in catch-up / recovery mode (1 = recovering, 0 = real-time).",
			},
			[]string{"source"},
		),
		QueryErrors: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tps_aggregator_query_errors_total",
				Help: "Total count of Elasticsearch query or connection errors.",
			},
			[]string{"source", "error_type"},
		),
		RecordsProcessed: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tps_aggregator_records_processed_total",
				Help: "Total count of 1-second metric records processed and committed.",
			},
			[]string{"source", "status"},
		),
		DatabaseErrors: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tps_aggregator_database_errors_total",
				Help: "Total count of SQL Server errors encountered.",
			},
			[]string{"source", "error_type"},
		),
		ProcessingDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tps_aggregator_processing_duration_seconds",
				Help:    "Duration in seconds spent in a single polling and aggregation cycle.",
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"source"},
		),
	}

	return m
}
