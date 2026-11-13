package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// AggregationBucket represents a 1-second histogram bucket from Elasticsearch.
type AggregationBucket struct {
	KeyAsString string `json:"key_as_string"`
	Key         int64  `json:"key"` // Epoch millis
	DocCount    int64  `json:"doc_count"`
	Success     struct {
		DocCount int64 `json:"doc_count"`
	} `json:"success"`
	Failed struct {
		DocCount int64 `json:"doc_count"`
	} `json:"failed"`
}

// AggregationResponse represents the parsed response from an ES date_histogram aggregation.
type AggregationResponse struct {
	Took     int  `json:"took"`
	TimedOut bool `json:"timed_out"`
	Hits     struct {
		Total struct {
			Value int64 `json:"value"`
		} `json:"total"`
	} `json:"hits"`
	Aggregations struct {
		BySecond struct {
			Buckets []AggregationBucket `json:"buckets"`
		} `json:"by_second"`
	} `json:"aggregations"`
}

// Client defines the contract for querying Elasticsearch.
type Client interface {
	Query1sHistogram(ctx context.Context, indexPattern string, startTime, endTime time.Time, timestampField string) (*AggregationResponse, error)
	Ping(ctx context.Context) error
}

// HTTPClient is a production-ready HTTP-based Elasticsearch client compatible with ES 7.x, 8.x, and OpenSearch.
type HTTPClient struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// NewHTTPClient creates a new HTTPClient with direct internal transport (bypassing corporate proxies).
func NewHTTPClient(baseURL, username, password string, timeout time.Duration) *HTTPClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	transport := &http.Transport{
		Proxy: nil, // Critical: direct connection to internal network, bypassing ambient HTTP_PROXY
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}

	return &HTTPClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
	}
}

// Ping checks if the Elasticsearch cluster is reachable.
func (c *HTTPClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/", nil)
	if err != nil {
		return fmt.Errorf("failed to create ping request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("elasticsearch ping failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("elasticsearch ping returned status: %d", resp.StatusCode)
	}
	return nil
}

// Query1sHistogram executes a bounded date_histogram aggregation at 1-second fixed interval.
func (c *HTTPClient) Query1sHistogram(ctx context.Context, indexPattern string, startTime, endTime time.Time, timestampField string) (*AggregationResponse, error) {
	if timestampField == "" {
		timestampField = "@timestamp"
	}

	startISO := startTime.UTC().Format(time.RFC3339)
	endISO := endTime.UTC().Format(time.RFC3339)

	// Valid success conditions verified against CAMS Elasticsearch schema:
	// 1. json_resp.ResponseCode == "00" (On-Premise CAMS v2)
	// 2. json_resp.responseCode == "00" (Cloud CAMS v3)
	// 3. json_resp.status == "200" (Cloud HTTP status in json_resp)
	// 4. status == "SUCCESS" or "00"
	// 5. response_code == "00"
	// 6. http_status in [200, 300)
	successConditions := []map[string]interface{}{
		{"term": map[string]interface{}{"json_resp.ResponseCode.keyword": "00"}},
		{"term": map[string]interface{}{"json_resp.ResponseCode": "00"}},
		{"term": map[string]interface{}{"json_resp.responseCode.keyword": "00"}},
		{"term": map[string]interface{}{"json_resp.responseCode": "00"}},
		{"term": map[string]interface{}{"json_resp.status.keyword": "200"}},
		{"term": map[string]interface{}{"json_resp.status": "200"}},
		{"term": map[string]interface{}{"status": "SUCCESS"}},
		{"term": map[string]interface{}{"status": "00"}},
		{"term": map[string]interface{}{"response_code": "00"}},
		{"range": map[string]interface{}{"http_status": map[string]interface{}{"gte": 200, "lt": 300}}},
	}

	query := map[string]interface{}{
		"size": 0,
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"filter": []map[string]interface{}{
					{
						"range": map[string]interface{}{
							timestampField: map[string]interface{}{
								"gte": startISO,
								"lt":  endISO,
							},
						},
					},
				},
			},
		},
		"aggs": map[string]interface{}{
			"by_second": map[string]interface{}{
				"date_histogram": map[string]interface{}{
					"field":          timestampField,
					"fixed_interval": "1s",
					"min_doc_count":  0,
					"extended_bounds": map[string]interface{}{
						"min": startISO,
						"max": endISO,
					},
				},
				"aggs": map[string]interface{}{
					"success": map[string]interface{}{
						"filter": map[string]interface{}{
							"bool": map[string]interface{}{
								"should": successConditions,
							},
						},
					},
					"failed": map[string]interface{}{
						"filter": map[string]interface{}{
							"bool": map[string]interface{}{
								"must_not": successConditions,
							},
						},
					},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal aggregation query: %w", err)
	}

	url := fmt.Sprintf("%s/%s/_search", c.baseURL, indexPattern)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("elasticsearch error status %d: %s", resp.StatusCode, string(respBody))
	}

	var aggResp AggregationResponse
	if err := json.NewDecoder(resp.Body).Decode(&aggResp); err != nil {
		return nil, fmt.Errorf("failed to decode aggregation response: %w", err)
	}

	return &aggResp, nil
}

func (c *HTTPClient) setAuth(req *http.Request) {
	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
}
