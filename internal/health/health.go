package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Pinger defines an interface for checking connectivity.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// SourceStatus represents the operational status of a single worker source.
type SourceStatus struct {
	Status     string    `json:"status"` // "UP", "DEGRADED", "HALTED"
	LagSeconds int64     `json:"lag_seconds"`
	LastError  string    `json:"last_error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Checker manages application liveness and readiness statuses.
type Checker struct {
	mu           sync.RWMutex
	dbPinger     Pinger
	sourceStates map[string]*SourceStatus
	reg          *prometheus.Registry
}

// NewChecker creates a new health and readiness Checker.
func NewChecker(db Pinger, reg *prometheus.Registry) *Checker {
	return &Checker{
		dbPinger:     db,
		sourceStates: make(map[string]*SourceStatus),
		reg:          reg,
	}
}

// UpdateSourceStatus updates the status for an individual worker.
func (c *Checker) UpdateSourceStatus(source, status string, lag int64, lastErr string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sourceStates[source] = &SourceStatus{
		Status:     status,
		LagSeconds: lag,
		LastError:  lastErr,
		UpdatedAt:  time.Now().UTC(),
	}
}

// HealthHandler returns HTTP 200 OK as long as the process is alive.
func (c *Checker) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "UP",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// ReadyHandler evaluates system readiness: DB connectivity and worker states.
func (c *Checker) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	c.mu.RLock()
	defer c.mu.RUnlock()

	dbStatus := "UP"
	if c.dbPinger != nil {
		if err := c.dbPinger.PingContext(ctx); err != nil {
			dbStatus = "DOWN"
		}
	}

	overallReady := dbStatus == "UP"
	// Check if at least one worker is alive or not halted
	allHalted := len(c.sourceStates) > 0
	for _, s := range c.sourceStates {
		if s.Status == "UP" || s.Status == "DEGRADED" {
			allHalted = false
			break
		}
	}

	if allHalted {
		overallReady = false
	}

	resp := map[string]interface{}{
		"status":   "UP",
		"database": dbStatus,
		"sources":  c.sourceStates,
		"time":     time.Now().UTC().Format(time.RFC3339),
	}

	if !overallReady {
		resp["status"] = "DOWN"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// Router returns an http.Handler with /health, /ready, and /metrics registered.
func (c *Checker) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", c.HealthHandler)
	mux.HandleFunc("/ready", c.ReadyHandler)

	if c.reg != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(c.reg, promhttp.HandlerOpts{}))
	} else {
		mux.Handle("/metrics", promhttp.Handler())
	}

	return mux
}

