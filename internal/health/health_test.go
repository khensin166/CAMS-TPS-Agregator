package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockPinger struct {
	err error
}

func (m *mockPinger) PingContext(ctx context.Context) error {
	return m.err
}

func TestChecker_Health(t *testing.T) {
	checker := NewChecker(nil, nil)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	checker.HealthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var res map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if res["status"] != "UP" {
		t.Errorf("expected status UP, got %s", res["status"])
	}
}

func TestChecker_Ready_Healthy(t *testing.T) {
	checker := NewChecker(&mockPinger{err: nil}, nil)
	checker.UpdateSourceStatus("onprem", "UP", 5, "")
	checker.UpdateSourceStatus("cloud", "UP", 2, "")

	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()

	checker.ReadyHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestChecker_Ready_DBDown(t *testing.T) {
	checker := NewChecker(&mockPinger{err: errors.New("connection refused")}, nil)
	checker.UpdateSourceStatus("onprem", "UP", 5, "")

	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()

	checker.ReadyHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}
}

