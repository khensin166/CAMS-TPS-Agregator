package summary

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cams-tps-aggregator/internal/repository"
)

// Calculator computes calendar-based daily, weekly, and monthly TPS summaries.
type Calculator struct {
	appName  string
	sources  []string
	repo     repository.Repository
	location *time.Location
	logger   *slog.Logger
}

// NewCalculator creates a new summary Calculator.
func NewCalculator(appName string, sources []string, repo repository.Repository, loc *time.Location, log *slog.Logger) *Calculator {
	if loc == nil {
		loc, _ = time.LoadLocation("Asia/Jakarta")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Calculator{
		appName:  appName,
		sources:  sources,
		repo:     repo,
		location: loc,
		logger:   log.With(slog.String("component", "summary_calculator")),
	}
}

// ComputeAllSummaries recalculates daily, weekly, and monthly summaries up to the specified reference time.
func (c *Calculator) ComputeAllSummaries(ctx context.Context, refTime time.Time) error {
	refLocal := refTime.In(c.location)

	// 1. Daily Period: Current day in Asia/Jakarta
	dayStartLocal := time.Date(refLocal.Year(), refLocal.Month(), refLocal.Day(), 0, 0, 0, 0, c.location)
	dayEndLocal := dayStartLocal.AddDate(0, 0, 1)

	// 2. Weekly Period: Monday 00:00:00 to next Monday
	weekdayOffset := int(refLocal.Weekday())
	if weekdayOffset == 0 {
		weekdayOffset = 7 // Sunday is treated as day 7
	}
	weekStartLocal := time.Date(refLocal.Year(), refLocal.Month(), refLocal.Day()-(weekdayOffset-1), 0, 0, 0, 0, c.location)
	weekEndLocal := weekStartLocal.AddDate(0, 0, 7)

	// 3. Monthly Period: 1st of current month to 1st of next month
	monthStartLocal := time.Date(refLocal.Year(), refLocal.Month(), 1, 0, 0, 0, 0, c.location)
	monthEndLocal := monthStartLocal.AddDate(0, 1, 0)

	periods := []struct {
		periodType string
		startUTC   time.Time
		endUTC     time.Time
	}{
		{"daily", dayStartLocal.UTC(), dayEndLocal.UTC()},
		{"weekly", weekStartLocal.UTC(), weekEndLocal.UTC()},
		{"monthly", monthStartLocal.UTC(), monthEndLocal.UTC()},
	}

	for _, source := range c.sources {
		for _, p := range periods {
			c.logger.Info("Computing summary rollup",
				slog.String("source", source),
				slog.String("period", p.periodType),
				slog.Time("start_utc", p.startUTC),
				slog.Time("end_utc", p.endUTC),
			)

			summary, err := c.repo.CalculateAndSaveSummary(ctx, c.appName, source, p.periodType, p.startUTC, p.endUTC)
			if err != nil {
				c.logger.Error("Failed computing summary",
					slog.String("source", source),
					slog.String("period", p.periodType),
					slog.Any("error", err),
				)
				return fmt.Errorf("failed calculating %s summary for %s: %w", p.periodType, source, err)
			}

			c.logger.Info("Successfully persisted summary",
				slog.String("source", source),
				slog.String("period", p.periodType),
				slog.Int("min_tps", summary.MinTPS),
				slog.Int("max_tps", summary.MaxTPS),
				slog.Float64("avg_tps", summary.AvgTPS),
				slog.Int64("total_trx", summary.TotalTransactions),
			)
		}
	}

	return nil
}

// StartPeriodicRollup starts a background goroutine calculating summaries on a periodic interval.
func (c *Calculator) StartPeriodicRollup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.logger.Info("Starting periodic summary calculator", slog.Duration("interval", interval))

	// Run initial calculation
	if err := c.ComputeAllSummaries(ctx, time.Now()); err != nil {
		c.logger.Warn("Initial summary rollup had error", slog.Any("error", err))
	}

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Summary calculator stopped cleanly")
			return
		case t := <-ticker.C:
			if err := c.ComputeAllSummaries(ctx, t); err != nil {
				c.logger.Error("Periodic summary calculation error", slog.Any("error", err))
			}
		}
	}
}

