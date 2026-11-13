package aggregator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cams-tps-aggregator/internal/config"
	"cams-tps-aggregator/internal/elasticsearch"
	"cams-tps-aggregator/internal/metrics"
	"cams-tps-aggregator/internal/repository"
	"github.com/prometheus/client_golang/prometheus"
)

// mockRepository implements repository.Repository for unit tests
type mockRepository struct {
	mu           sync.Mutex
	checkpoints  map[string]*repository.Checkpoint
	metricsSaved []repository.MetricRecord
	loggedEvents []repository.WorkerLog
	failSave     bool
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		checkpoints: make(map[string]*repository.Checkpoint),
	}
}

func (m *mockRepository) PingContext(ctx context.Context) error {
	return nil
}

func (m *mockRepository) GetCheckpoint(ctx context.Context, sourceName, indexPattern string) (*repository.Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := sourceName + ":" + indexPattern
	if cp, ok := m.checkpoints[key]; ok {
		return cp, nil
	}
	return nil, nil
}

func (m *mockRepository) SaveMetricsAndAdvanceCheckpoint(ctx context.Context, recs []repository.MetricRecord, cp repository.Checkpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSave {
		return errors.New("simulated database transaction error")
	}
	key := cp.SourceName + ":" + cp.IndexPattern
	m.checkpoints[key] = &cp
	m.metricsSaved = append(m.metricsSaved, recs...)
	return nil
}

func (m *mockRepository) CalculateAndSaveSummary(ctx context.Context, appName, sourceName, periodType string, start, end time.Time) (*repository.SummaryRecord, error) {
	return nil, nil
}

func (m *mockRepository) LogWorkerEvent(ctx context.Context, sourceName, eventType, message string, lagSeconds *int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loggedEvents = append(m.loggedEvents, repository.WorkerLog{
		SourceName: sourceName,
		EventType:  eventType,
		Message:    message,
		LagSeconds: lagSeconds,
		CreatedAt:  time.Now().UTC(),
	})
	return nil
}

// mockHealthReporter tracks status updates per source
type mockHealthReporter struct {
	mu       sync.Mutex
	statuses map[string]string
}

func (h *mockHealthReporter) UpdateSourceStatus(source, status string, lag int64, lastErr string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.statuses == nil {
		h.statuses = make(map[string]string)
	}
	h.statuses[source] = status
}

func TestWorker_SuccessfulPollingCycle(t *testing.T) {
	mockES := elasticsearch.NewMockServer(false, "", "")
	defer mockES.Close()

	now := time.Now().UTC().Truncate(time.Second)
	// Seed 2 seconds of transactions
	mockES.SeedBucket(now.Add(-65*time.Second), 200, 190, 10)
	mockES.SeedBucket(now.Add(-64*time.Second), 350, 340, 10)

	cfg := config.WorkerConfig{
		Enabled:        true,
		SourceName:     "onprem",
		URL:            mockES.URL(),
		IndexPattern:   "cams-transactions-*",
		PollInterval:   50 * time.Millisecond,
		WatermarkDelay: 60 * time.Second,
		OverlapWindow:  120 * time.Second,
		TimestampField: "@timestamp",
	}

	repo := newMockRepository()
	// Set initial checkpoint to 70s ago
	repo.checkpoints["onprem:cams-transactions-*"] = &repository.Checkpoint{
		SourceName:         "onprem",
		IndexPattern:       "cams-transactions-*",
		LastProcessedUntil: now.Add(-70 * time.Second),
		LastSuccessAt:      now.Add(-70 * time.Second),
	}

	esClient := elasticsearch.NewHTTPClient(mockES.URL(), "", "", 2*time.Second)
	promMetrics := metrics.NewMetrics(prometheus.NewRegistry())
	health := &mockHealthReporter{}

	worker := NewWorker("CAMS-TPS-Agregator", cfg, esClient, repo, promMetrics, health, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	worker.Start(ctx)

	// Verify checkpoint advanced
	cp, err := repo.GetCheckpoint(context.Background(), "onprem", "cams-transactions-*")
	if err != nil || cp == nil {
		t.Fatalf("expected checkpoint to be recorded, got: %v", err)
	}

	if !cp.LastProcessedUntil.After(now.Add(-70 * time.Second)) {
		t.Errorf("expected checkpoint to advance, but got: %v", cp.LastProcessedUntil)
	}
}

func TestWorker_DatabaseFailureRetainsCheckpoint(t *testing.T) {
	mockES := elasticsearch.NewMockServer(false, "", "")
	defer mockES.Close()

	now := time.Now().UTC().Truncate(time.Second)
	mockES.SeedBucket(now.Add(-65*time.Second), 100, 95, 5)

	initialWatermark := now.Add(-80 * time.Second)
	repo := newMockRepository()
	repo.failSave = true // Inject DB failure
	repo.checkpoints["onprem:cams-*"] = &repository.Checkpoint{
		SourceName:         "onprem",
		IndexPattern:       "cams-*",
		LastProcessedUntil: initialWatermark,
	}

	cfg := config.WorkerConfig{
		Enabled:        true,
		SourceName:     "onprem",
		IndexPattern:   "cams-*",
		PollInterval:   50 * time.Millisecond,
		WatermarkDelay: 60 * time.Second,
		OverlapWindow:  120 * time.Second,
	}

	esClient := elasticsearch.NewHTTPClient(mockES.URL(), "", "", 2*time.Second)
	promMetrics := metrics.NewMetrics(prometheus.NewRegistry())
	health := &mockHealthReporter{}

	worker := NewWorker("CAMS", cfg, esClient, repo, promMetrics, health, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	worker.Start(ctx)

	// Verify that checkpoint was NOT advanced
	cp, err := repo.GetCheckpoint(context.Background(), "onprem", "cams-*")
	if err != nil || cp == nil {
		t.Fatalf("expected checkpoint to exist")
	}

	if !cp.LastProcessedUntil.Equal(initialWatermark) {
		t.Errorf("expected checkpoint to remain unchanged at %v, got %v", initialWatermark, cp.LastProcessedUntil)
	}

	// Verify that worker is in DEGRADED status
	health.mu.Lock()
	status := health.statuses["onprem"]
	health.mu.Unlock()
	if status != "DEGRADED" {
		t.Errorf("expected worker status DEGRADED on DB failure, got %s", status)
	}
}

func TestWorker_DataLossRiskDetection(t *testing.T) {
	mockES := elasticsearch.NewMockServer(false, "", "")
	defer mockES.Close()

	now := time.Now().UTC().Truncate(time.Second)
	// Watermark is 10 days ago, while retention is 7 days
	oldWatermark := now.Add(-10 * 24 * time.Hour)

	repo := newMockRepository()
	repo.checkpoints["onprem:cams-*"] = &repository.Checkpoint{
		SourceName:         "onprem",
		IndexPattern:       "cams-*",
		LastProcessedUntil: oldWatermark,
	}

	cfg := config.WorkerConfig{
		Enabled:         true,
		SourceName:      "onprem",
		IndexPattern:    "cams-*",
		PollInterval:    50 * time.Millisecond,
		WatermarkDelay:  60 * time.Second,
		OverlapWindow:   120 * time.Second,
		RetentionPeriod: 7 * 24 * time.Hour,
	}

	esClient := elasticsearch.NewHTTPClient(mockES.URL(), "", "", 2*time.Second)
	reg := prometheus.NewRegistry()
	promMetrics := metrics.NewMetrics(reg)
	health := &mockHealthReporter{}

	worker := NewWorker("CAMS", cfg, esClient, repo, promMetrics, health, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	worker.Start(ctx)

	// Verify that DataLossRisk log was created
	repo.mu.Lock()
	hasDataLossLog := false
	for _, l := range repo.loggedEvents {
		if l.EventType == "DATA_LOSS_RISK" {
			hasDataLossLog = true
			break
		}
	}
	repo.mu.Unlock()

	if !hasDataLossLog {
		t.Errorf("expected DATA_LOSS_RISK log event to be recorded when lag > retention")
	}
}

func TestDualWorkers_IndependentFailure(t *testing.T) {
	// Worker A: Healthy mock ES
	mockA := elasticsearch.NewMockServer(false, "", "")
	defer mockA.Close()

	// Worker B: Failing mock ES
	mockB := elasticsearch.NewMockServer(false, "", "")
	defer mockB.Close()
	mockB.SetFailures(100) // Fail all requests

	now := time.Now().UTC().Truncate(time.Second)
	mockA.SeedBucket(now.Add(-65*time.Second), 500, 490, 10)

	repo := newMockRepository()
	promMetrics := metrics.NewMetrics(prometheus.NewRegistry())
	health := &mockHealthReporter{}

	cfgA := config.WorkerConfig{
		Enabled:        true,
		SourceName:     "onprem",
		IndexPattern:   "cams-*",
		PollInterval:   50 * time.Millisecond,
		WatermarkDelay: 60 * time.Second,
		OverlapWindow:  120 * time.Second,
	}
	cfgB := config.WorkerConfig{
		Enabled:        true,
		SourceName:     "cloud",
		IndexPattern:   "cams-*",
		PollInterval:   50 * time.Millisecond,
		WatermarkDelay: 60 * time.Second,
		OverlapWindow:  120 * time.Second,
	}

	clientA := elasticsearch.NewHTTPClient(mockA.URL(), "", "", 2*time.Second)
	clientB := elasticsearch.NewHTTPClient(mockB.URL(), "", "", 2*time.Second)

	workerA := NewWorker("CAMS", cfgA, clientA, repo, promMetrics, health, nil)
	workerB := NewWorker("CAMS", cfgB, clientB, repo, promMetrics, health, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		workerA.Start(ctx)
	}()

	go func() {
		defer wg.Done()
		workerB.Start(ctx)
	}()

	wg.Wait()

	// Health status check: Worker A must be UP, Worker B must be DEGRADED
	health.mu.Lock()
	statusA := health.statuses["onprem"]
	statusB := health.statuses["cloud"]
	health.mu.Unlock()

	if statusA != "UP" {
		t.Errorf("expected workerA status UP, got %s", statusA)
	}
	if statusB != "DEGRADED" {
		t.Errorf("expected workerB status DEGRADED, got %s", statusB)
	}
}
