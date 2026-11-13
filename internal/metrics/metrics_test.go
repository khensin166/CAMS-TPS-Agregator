package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetrics_Initialization(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.Up.WithLabelValues("onprem").Set(1)
	m.CurrentTPS.WithLabelValues("onprem").Set(450)
	m.RecordsProcessed.WithLabelValues("onprem", "SUCCESS").Inc()
	m.QueryErrors.WithLabelValues("onprem", "TIMEOUT").Inc()
	m.DatabaseErrors.WithLabelValues("onprem", "CONN_FAIL").Inc()

	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather prometheus metrics: %v", err)
	}

	if len(metricFamilies) == 0 {
		t.Fatalf("expected registered metric families, got 0")
	}
}
