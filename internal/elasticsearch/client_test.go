package elasticsearch

import (
	"context"
	"testing"
	"time"
)

func TestHTTPClient_Ping_Success(t *testing.T) {
	mock := NewMockServer(false, "", "")
	defer mock.Close()

	client := NewHTTPClient(mock.URL(), "", "", 2*time.Second)
	ctx := context.Background()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("expected ping to succeed, got: %v", err)
	}
}

func TestHTTPClient_Auth(t *testing.T) {
	mock := NewMockServer(true, "elastic", "secret123")
	defer mock.Close()

	// Unauthorized client
	unauthClient := NewHTTPClient(mock.URL(), "wrong", "wrong", 2*time.Second)
	if err := unauthClient.Ping(context.Background()); err == nil {
		t.Fatalf("expected unauthorized client to fail ping")
	}

	// Authorized client
	authClient := NewHTTPClient(mock.URL(), "elastic", "secret123", 2*time.Second)
	if err := authClient.Ping(context.Background()); err != nil {
		t.Fatalf("expected authorized client to succeed, got: %v", err)
	}
}

func TestHTTPClient_Query1sHistogram(t *testing.T) {
	mock := NewMockServer(false, "", "")
	defer mock.Close()

	now := time.Now().UTC().Truncate(time.Second)
	mock.SeedBucket(now, 150, 145, 5)

	client := NewHTTPClient(mock.URL(), "", "", 2*time.Second)
	ctx := context.Background()

	resp, err := client.Query1sHistogram(ctx, "cams-transactions-*", now.Add(-10*time.Second), now, "@timestamp")
	if err != nil {
		t.Fatalf("failed to query aggregation: %v", err)
	}

	if len(resp.Aggregations.BySecond.Buckets) == 0 {
		t.Fatalf("expected at least 1 bucket")
	}

	bucket := resp.Aggregations.BySecond.Buckets[0]
	if bucket.DocCount != 150 {
		t.Errorf("expected doc_count 150, got %d", bucket.DocCount)
	}
	if bucket.Success.DocCount != 145 {
		t.Errorf("expected success doc_count 145, got %d", bucket.Success.DocCount)
	}
	if bucket.Failed.DocCount != 5 {
		t.Errorf("expected failed doc_count 5, got %d", bucket.Failed.DocCount)
	}
}

