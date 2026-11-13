package summary

import (
	"context"
	"testing"
	"time"

	"cams-tps-aggregator/internal/repository"
)

type mockSummaryRepo struct {
	savedSummaries []*repository.SummaryRecord
}

func (m *mockSummaryRepo) PingContext(ctx context.Context) error { return nil }
func (m *mockSummaryRepo) GetCheckpoint(ctx context.Context, s, i string) (*repository.Checkpoint, error) {
	return nil, nil
}
func (m *mockSummaryRepo) SaveMetricsAndAdvanceCheckpoint(ctx context.Context, recs []repository.MetricRecord, cp repository.Checkpoint) error {
	return nil
}
func (m *mockSummaryRepo) CalculateAndSaveSummary(ctx context.Context, appName, sourceName, periodType string, start, end time.Time) (*repository.SummaryRecord, error) {
	durationSeconds := end.Sub(start).Seconds()
	rec := &repository.SummaryRecord{
		ApplicationName:   appName,
		SourceName:        sourceName,
		PeriodType:        periodType,
		PeriodStart:       start,
		PeriodEnd:         end,
		MinTPS:            5,
		MaxTPS:            250,
		AvgTPS:            50.0,
		TotalTransactions: int64(50.0 * durationSeconds),
		CalculatedAt:      time.Now().UTC(),
	}
	m.savedSummaries = append(m.savedSummaries, rec)
	return rec, nil
}
func (m *mockSummaryRepo) LogWorkerEvent(ctx context.Context, sourceName, eventType, message string, lagSeconds *int) error {
	return nil
}

func TestCalculator_ComputeAllSummaries(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("failed loading timezone: %v", err)
	}

	repo := &mockSummaryRepo{}
	calc := NewCalculator("CAMS", []string{"onprem", "cloud"}, repo, loc, nil)

	// Ref time: Wednesday, Nov 13, 2026, 14:30:00 Jakarta time
	refTime := time.Date(2026, 11, 13, 14, 30, 0, 0, loc)

	err = calc.ComputeAllSummaries(context.Background(), refTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2 sources * 3 periods (daily, weekly, monthly) = 6 records
	if len(repo.savedSummaries) != 6 {
		t.Fatalf("expected 6 summary records, got: %d", len(repo.savedSummaries))
	}

	// Check daily period for onprem
	daily := repo.savedSummaries[0]
	if daily.PeriodType != "daily" {
		t.Errorf("expected daily period, got %s", daily.PeriodType)
	}

	// Jakarta day starts at 00:00:00 WIB = 17:00:00 UTC previous day (UTC+7)
	expectedDailyStartUTC := time.Date(2026, 11, 12, 17, 0, 0, 0, time.UTC)
	if !daily.PeriodStart.Equal(expectedDailyStartUTC) {
		t.Errorf("expected daily start UTC %v, got %v", expectedDailyStartUTC, daily.PeriodStart)
	}
}

func TestCalculator_StartPeriodicRollup_Cancellation(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Jakarta")
	repo := &mockSummaryRepo{}
	calc := NewCalculator("CAMS", []string{"onprem"}, repo, loc, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should start and gracefully terminate on context deadline
	calc.StartPeriodicRollup(ctx, 30*time.Millisecond)

	if len(repo.savedSummaries) == 0 {
		t.Errorf("expected at least initial summary calculation before timeout")
	}
}

