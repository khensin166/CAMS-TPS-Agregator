package elasticsearch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// MockServer simulates an Elasticsearch cluster for local testing.
type MockServer struct {
	server       *httptest.Server
	mu           sync.Mutex
	buckets      map[int64]AggregationBucket
	failNextN    int
	requireAuth  bool
	authUsername string
	authPassword string
}

// NewMockServer creates and starts a test HTTP server mimicking Elasticsearch.
func NewMockServer(requireAuth bool, user, pass string) *MockServer {
	ms := &MockServer{
		buckets:      make(map[int64]AggregationBucket),
		requireAuth:  requireAuth,
		authUsername: user,
		authPassword: pass,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ms.handleRoot)
	mux.HandleFunc("/_cluster/health", ms.handleHealth)
	ms.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ms.requireAuth {
			u, p, ok := r.BasicAuth()
			if !ok || u != ms.authUsername || p != ms.authPassword {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}

		ms.mu.Lock()
		if ms.failNextN > 0 {
			ms.failNextN--
			ms.mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": "simulated elasticsearch cluster error"}`))
			return
		}
		ms.mu.Unlock()

		if r.URL.Path == "/" {
			ms.handleRoot(w, r)
			return
		}
		if r.URL.Path == "/_cluster/health" {
			ms.handleHealth(w, r)
			return
		}
		if len(r.URL.Path) > 8 && r.URL.Path[len(r.URL.Path)-8:] == "/_search" {
			ms.handleSearch(w, r)
			return
		}

		http.NotFound(w, r)
	}))

	return ms
}

// URL returns the mock server's HTTP base URL.
func (ms *MockServer) URL() string {
	return ms.server.URL
}

// Close stops the mock HTTP server.
func (ms *MockServer) Close() {
	ms.server.Close()
}

// SetFailures causes the next n requests to fail with HTTP 500.
func (ms *MockServer) SetFailures(n int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.failNextN = n
}

// SeedBucket adds or updates a 1-second bucket in the mock store.
func (ms *MockServer) SeedBucket(t time.Time, total, success, failed int64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	epochMillis := t.UTC().Truncate(time.Second).UnixMilli()
	b := AggregationBucket{
		KeyAsString: t.UTC().Truncate(time.Second).Format(time.RFC3339),
		Key:         epochMillis,
		DocCount:    total,
	}
	b.Success.DocCount = success
	b.Failed.DocCount = failed
	ms.buckets[epochMillis] = b
}

func (ms *MockServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{
		"name": "mock-es-node-01",
		"cluster_name": "mock-cluster",
		"version": {"number": "8.12.0", "build_flavor": "default"},
		"tagline": "You Know, for Search"
	}`))
}

func (ms *MockServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"cluster_name": "mock-cluster", "status": "green", "number_of_nodes": 1}`))
}

func (ms *MockServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)

	// Build buckets based on seeded data
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var bucketList []AggregationBucket
	var totalHits int64
	for _, b := range ms.buckets {
		bucketList = append(bucketList, b)
		totalHits += b.DocCount
	}

	resp := AggregationResponse{
		Took:     2,
		TimedOut: false,
	}
	resp.Hits.Total.Value = totalHits
	resp.Aggregations.BySecond.Buckets = bucketList

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

