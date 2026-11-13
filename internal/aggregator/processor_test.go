package aggregator

import (
	"testing"
	"time"

	"cams-tps-aggregator/internal/elasticsearch"
)

func TestProcessAggregationResponse(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	queryStart := now.Add(-5 * time.Second)
	queryEnd := now

	resp := &elasticsearch.AggregationResponse{}
	resp.Aggregations.BySecond.Buckets = []elasticsearch.AggregationBucket{
		{
			KeyAsString: queryStart.Add(1 * time.Second).Format(time.RFC3339),
			Key:         queryStart.Add(1 * time.Second).UnixMilli(),
			DocCount:    100,
			Success: struct {
				DocCount int64 `json:"doc_count"`
			}{DocCount: 95},
			Failed: struct {
				DocCount int64 `json:"doc_count"`
			}{DocCount: 5},
		},
		{ // Verified zero-transaction second
			KeyAsString: queryStart.Add(2 * time.Second).Format(time.RFC3339),
			Key:         queryStart.Add(2 * time.Second).UnixMilli(),
			DocCount:    0,
			Success: struct {
				DocCount int64 `json:"doc_count"`
			}{DocCount: 0},
			Failed: struct {
				DocCount int64 `json:"doc_count"`
			}{DocCount: 0},
		},
	}

	records, err := ProcessAggregationResponse("CAMS-TPS-Agregator", "onprem", resp, queryStart, queryEnd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// First bucket: 100 total, 95 success, 5 failed
	r1 := records[0]
	if r1.TotalTransactions != 100 || r1.SuccessTransactions != 95 || r1.FailedTransactions != 5 {
		t.Errorf("r1 metrics mismatch: %+v", r1)
	}
	if !r1.IsComplete {
		t.Errorf("expected is_complete true for r1")
	}

	// Second bucket: Verified zero TPS
	r2 := records[1]
	if r2.TotalTransactions != 0 || r2.SuccessTransactions != 0 || r2.FailedTransactions != 0 {
		t.Errorf("r2 metrics mismatch: %+v", r2)
	}
	if !r2.IsComplete {
		t.Errorf("expected is_complete true for verified zero-tps bucket")
	}
}

