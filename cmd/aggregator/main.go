package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cams-tps-aggregator/internal/aggregator"
	"cams-tps-aggregator/internal/config"
	"cams-tps-aggregator/internal/elasticsearch"
	"cams-tps-aggregator/internal/health"
	"cams-tps-aggregator/internal/logger"
	"cams-tps-aggregator/internal/metrics"
	"cams-tps-aggregator/internal/repository"
	"cams-tps-aggregator/internal/repository/sqlserver"
	"cams-tps-aggregator/internal/summary"
)

func main() {
	// 1. Load Configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal configuration error: %v\n", err)
		os.Exit(1)
	}

	// 2. Initialize Structured Logging
	log := logger.InitLogger(cfg.App.Environment)
	log.Info("Starting TPS Monitoring System",
		slog.String("app", cfg.App.Name),
		slog.String("env", cfg.App.Environment),
		slog.Int("port", cfg.App.Port),
		slog.String("timezone", cfg.App.Timezone),
	)

	// Context for graceful application shutdown
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 3. Connect to Microsoft SQL Server
	var db *sql.DB
	var repo repository.Repository

	dbConn, err := sqlserver.Connect(rootCtx, cfg.Database, log)
	if err != nil {
		if cfg.App.Environment == "production" {
			log.Error("Fatal database connection failure in production", slog.Any("error", err))
			os.Exit(1)
		}
		log.Warn("Could not connect to SQL Server in local development environment. Operating in mock offline mode",
			slog.Any("error", err),
			slog.String("hint", "Start local SQL Server using 'docker compose -f deployments/docker-compose.local.yml up -d'"),
		)
		// Local development fallback mock
		repo = newLocalDevMockRepo(log)
	} else {
		db = dbConn
		defer db.Close()

		// Run Database Migrations
		if err := sqlserver.RunMigrations(rootCtx, db, "./migrations", log); err != nil {
			log.Error("Failed to apply database migrations", slog.Any("error", err))
			if cfg.App.Environment == "production" {
				os.Exit(1)
			}
		}

		repo = sqlserver.NewSQLRepository(db, cfg.Database.CommandTimeout, log)
	}

	// 4. Initialize Telemetry & Metrics
	promMetrics := metrics.NewMetrics(nil)

	// 5. Initialize Health Checker
	healthChecker := health.NewChecker(repo, nil)

	// 6. Initialize Elasticsearch Clients
	clientOnPrem := elasticsearch.NewHTTPClient(
		cfg.OnPrem.URL,
		cfg.OnPrem.User,
		cfg.OnPrem.Password,
		15*time.Second,
	)

	clientCloud := elasticsearch.NewHTTPClient(
		cfg.Cloud.URL,
		cfg.Cloud.User,
		cfg.Cloud.Password,
		15*time.Second,
	)

	// 7. Initialize Dual Independent Workers
	workerOnPrem := aggregator.NewWorker(
		cfg.App.Name,
		cfg.OnPrem,
		clientOnPrem,
		repo,
		promMetrics,
		healthChecker,
		log,
	)

	workerCloud := aggregator.NewWorker(
		cfg.App.Name,
		cfg.Cloud,
		clientCloud,
		repo,
		promMetrics,
		healthChecker,
		log,
	)

	// 8. Initialize Summary Calculator
	summaryCalc := summary.NewCalculator(
		cfg.App.Name,
		[]string{cfg.OnPrem.SourceName, cfg.Cloud.SourceName},
		repo,
		cfg.App.Location,
		log,
	)

	// 9. Start Workers and Rollup Engines in Goroutines
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()

	go workerOnPrem.Start(workerCtx)
	go workerCloud.Start(workerCtx)
	go summaryCalc.StartPeriodicRollup(workerCtx, 1*time.Hour)

	// 10. Start HTTP Server for /health, /ready, /metrics
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.App.Port),
		Handler:      healthChecker.Router(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("Health & metrics HTTP server listening", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("HTTP server failure", slog.Any("error", err))
		}
	}()

	log.Info("TPS Monitoring System operational. Press Ctrl+C to terminate.")
	<-rootCtx.Done()

	log.Info("Shutdown signal received, initiating graceful teardown...")
	cancelWorkers() // Stop worker loops

	// Stop HTTP server
	shutdownTimeoutCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownTimeoutCtx); err != nil {
		log.Error("Error during HTTP server shutdown", slog.Any("error", err))
	} else {
		log.Info("HTTP server stopped cleanly")
	}

	log.Info("TPS Monitoring System terminated safely")
}

// localDevMockRepo satisfies repository.Repository for local offline runs without SQL Server
type localDevMockRepo struct {
	log *slog.Logger
}

func newLocalDevMockRepo(log *slog.Logger) *localDevMockRepo {
	return &localDevMockRepo{log: log}
}

func (m *localDevMockRepo) PingContext(ctx context.Context) error {
	return nil
}

func (m *localDevMockRepo) GetCheckpoint(ctx context.Context, sourceName, indexPattern string) (*repository.Checkpoint, error) {
	return nil, nil
}

func (m *localDevMockRepo) SaveMetricsAndAdvanceCheckpoint(ctx context.Context, recs []repository.MetricRecord, cp repository.Checkpoint) error {
	m.log.Debug("Mock repo saved metrics batch",
		slog.String("source", cp.SourceName),
		slog.Int("count", len(recs)),
		slog.Time("watermark", cp.LastProcessedUntil),
	)
	return nil
}

func (m *localDevMockRepo) CalculateAndSaveSummary(ctx context.Context, appName, sourceName, periodType string, start, end time.Time) (*repository.SummaryRecord, error) {
	return &repository.SummaryRecord{
		ApplicationName: appName,
		SourceName:      sourceName,
		PeriodType:      periodType,
		PeriodStart:     start,
		PeriodEnd:       end,
		MinTPS:          0,
		MaxTPS:          0,
		AvgTPS:          0,
		CalculatedAt:    time.Now().UTC(),
	}, nil
}

func (m *localDevMockRepo) LogWorkerEvent(ctx context.Context, sourceName, eventType, message string, lagSeconds *int) error {
	return nil
}
