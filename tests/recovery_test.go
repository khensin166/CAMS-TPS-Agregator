package tests

import (
	"context"
	"testing"
	"time"

	"cams-tps-aggregator/internal/aggregator"
	"cams-tps-aggregator/internal/config"
	"cams-tps-aggregator/internal/elasticsearch"
	"cams-tps-aggregator/internal/metrics"
	"cams-tps-aggregator/internal/repository"
	"github.com/prometheus/client_golang/prometheus"
)

// mockTestRepo provides an in-memory repository for recovery and overlap testing
type mockTestRepo struct {
	checkpoints  map[string]*repository.Checkpoint
	metricsStore map[string]repository.MetricRecord
}

func newMockTestRepo() *mockTestRepo {
	return &mockTestRepo{
		checkpoints:  make(map[string]*repository.Checkpoint),
		metricsStore: make(map[string]repository.MetricRecord),
	}
}

func (m *mockTestRepo) PingContext(ctx context.Context) error { return nil }

func (m *mockTestRepo) GetCheckpoint(ctx context.Context, s, i string) (*repository.Checkpoint, error) {
	key := s + ":" + i
	if cp, ok := m.checkpoints[key]; ok {
		return cp, nil
	}
	return nil, nil
}

func (m *mockTestRepo) SaveMetricsAndAdvanceCheckpoint(ctx context.Context, recs []repository.MetricRecord, cp repository.Checkpoint) error {
	// Idempotent upsert simulation
	for _, r := range recs {
		key := r.ApplicationName + ":" + r.SourceName + ":" + r.MetricTime.Format(time.RFC3339)
		m.metricsStore[key] = r
	}
	key := cp.SourceName + ":" + cp.IndexPattern
	m.checkpoints[key] = &cp
	return nil
}

func (m *mockTestRepo) CalculateAndSaveSummary(ctx context.Context, appName, sourceName, periodType string, start, end time.Time) (*repository.SummaryRecord, error) {
	return nil, nil
}

func (m *mockTestRepo) LogWorkerEvent(ctx context.Context, sourceName, eventType, message string, lagSeconds *int) error {
	return nil
}

// TestRecovery_WorkerRestartWithLateArrivingEvents tests checkpoint recovery and reprocessing within the overlap window
func TestRecovery_WorkerRestartWithLateArrivingEvents(t *testing.T) {
	mockES := elasticsearch.NewMockServer(false, "", "")
	defer mockES.Close()

	now := time.Now().UTC().Truncate(time.Second)
	tMinus90 := now.Add(-90 * time.Second)
	tMinus80 := now.Add(-80 * time.Second)
	tMinus65 := now.Add(-65 * time.Second)

	// Seed original data
	mockES.SeedBucket(tMinus90, 50, 48, 2)
	mockES.SeedBucket(tMinus80, 75, 70, 5)

	repo := newMockTestRepo()
	// Pre-seed checkpoint at -85s
	repo.checkpoints["onprem:cams-*"] = &repository.Checkpoint{
		SourceName:         "onprem",
		IndexPattern:       "cams-*",
		LastProcessedUntil: now.Add(-85 * time.Second),
		LastSuccessAt:      now.Add(-85 * time.Second),
	}

	cfg := config.WorkerConfig{
		Enabled:        true,
		SourceName:     "onprem",
		URL:            mockES.URL(),
		IndexPattern:   "cams-*",
		PollInterval:   50 * time.Millisecond,
		WatermarkDelay: 70 * time.Second, // targetEnd = now - 70s (which is > -85s)
		OverlapWindow:  120 * time.Second,
		TimestampField: "@timestamp",
	}

	esClient := elasticsearch.NewHTTPClient(mockES.URL(), "", "", 2*time.Second)
	promMetrics := metrics.NewMetrics(prometheus.NewRegistry())

	// Step 1: Run worker for 1st cycle (advances checkpoint to now - 70s)
	worker1 := aggregator.NewWorker("CAMS", cfg, esClient, repo, promMetrics, nil, nil)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	worker1.Start(ctx1)
	cancel1()

	cp1, _ := repo.GetCheckpoint(context.Background(), "onprem", "cams-*")
	if cp1 == nil {
		t.Fatalf("expected checkpoint after cycle 1")
	}

	// Step 2: Simulate late arriving events in the past (tMinus80 increased from 75 to 90) and new events at tMinus65
	mockES.SeedBucket(tMinus80, 90, 85, 5) // late arriving events within overlap window
	mockES.SeedBucket(tMinus65, 120, 115, 5)

	// Step 3: Change watermark delay to 60s so targetEnd advances to now - 60s (> now - 70s)
	cfg.WatermarkDelay = 60 * time.Second

	// Step 4: Simulate application restart (New Worker instance restoring from checkpoint at -70s)
	worker2 := aggregator.NewWorker("CAMS", cfg, esClient, repo, promMetrics, nil, nil)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	worker2.Start(ctx2)
	cancel2()

	// Verify that late arriving events at tMinus80 were updated idempotently
	keyTMinus80 := "CAMS:onprem:" + tMinus80.Format(time.RFC3339)
	record, exists := repo.metricsStore[keyTMinus80]
	if !exists {
		t.Fatalf("expected record at %s to exist", keyTMinus80)
	}

	if record.TotalTransactions != 90 {
		t.Errorf("expected updated total_transactions = 90 due to overlap reprocessing, got %d", record.TotalTransactions)
	}

	// Verify new event at tMinus65 was captured
	keyTMinus65 := "CAMS:onprem:" + tMinus65.Format(time.RFC3339)
	rec65, exists65 := repo.metricsStore[keyTMinus65]
	if !exists65 {
		t.Fatalf("expected record at %s to exist", keyTMinus65)
	}
	if rec65.TotalTransactions != 120 {
		t.Errorf("expected total_transactions = 120, got %d", rec65.TotalTransactions)
	}
}

// TestEmptyAggregationBuckets verifies that intervals with zero transactions are stored as verified zero TPS
func TestEmptyAggregationBuckets(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	queryStart := now.Add(-10 * time.Second)
	queryEnd := now

	emptyResp := &elasticsearch.AggregationResponse{}
	emptyResp.Aggregations.BySecond.Buckets = []elasticsearch.AggregationBucket{
		{
			KeyAsString: queryStart.Add(1 * time.Second).Format(time.RFC3339),
			Key:         queryStart.Add(1 * time.Second).UnixMilli(),
			DocCount:    0,
		},
	}

	records, err := aggregator.ProcessAggregationResponse("CAMS", "cloud", emptyResp, queryStart, queryEnd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	if records[0].TotalTransactions != 0 {
		t.Errorf("expected total 0, got %d", records[0].TotalTransactions)
	}
	if !records[0].IsComplete {
		t.Errorf("expected is_complete true for empty bucket")
	}
}
