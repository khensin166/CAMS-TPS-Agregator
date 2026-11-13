package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cams-tps-aggregator/internal/config"
	"cams-tps-aggregator/internal/health"
	"cams-tps-aggregator/internal/logger"
	"cams-tps-aggregator/internal/metrics"
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

	// 3. Initialize Telemetry & Metrics
	promMetrics := metrics.NewMetrics(nil)
	promMetrics.Up.WithLabelValues("onprem").Set(1)
	promMetrics.Up.WithLabelValues("cloud").Set(1)

	// 4. Initialize Health Check Router
	healthChecker := health.NewChecker(nil, nil)
	healthChecker.UpdateSourceStatus("onprem", "UP", 0, "")
	healthChecker.UpdateSourceStatus("cloud", "UP", 0, "")

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.App.Port),
		Handler:      healthChecker.Router(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// 5. Start HTTP Server in background goroutine
	go func() {
		log.Info("Health & metrics HTTP server listening", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("HTTP server failure", slog.Any("error", err))
		}
	}()

	// 6. Graceful Shutdown Setup
	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("TPS Monitoring System foundation initialized successfully. Waiting for shutdown signal...")
	<-shutdownCtx.Done()

	log.Info("Shutdown signal received, initiating graceful teardown...")
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctxTimeout); err != nil {
		log.Error("Error during HTTP server shutdown", slog.Any("error", err))
	} else {
		log.Info("HTTP server stopped cleanly")
	}

	log.Info("TPS Monitoring System terminated")
}

