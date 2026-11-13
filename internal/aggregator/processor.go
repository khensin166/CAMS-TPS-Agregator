package aggregator

import (
	"fmt"
	"time"

	"cams-tps-aggregator/internal/elasticsearch"
	"cams-tps-aggregator/internal/repository"
)

// ProcessAggregationResponse converts an Elasticsearch aggregation response into a slice of MetricRecords.
func ProcessAggregationResponse(
	appName string,
	sourceName string,
	aggResp *elasticsearch.AggregationResponse,
	queryStart time.Time,
	queryEnd time.Time,
) ([]repository.MetricRecord, error) {
	if aggResp == nil {
		return nil, fmt.Errorf("nil aggregation response received for source %s", sourceName)
	}

	buckets := aggResp.Aggregations.BySecond.Buckets
	records := make([]repository.MetricRecord, 0, len(buckets))
	now := time.Now().UTC()

	for _, b := range buckets {
		metricTime := time.UnixMilli(b.Key).UTC().Truncate(time.Second)

		// Filter out any bucket falling outside the requested bounds
		if metricTime.Before(queryStart.Truncate(time.Second)) || !metricTime.Before(queryEnd.Truncate(time.Second)) {
			continue
		}

		total := int(b.DocCount)
		success := int(b.Success.DocCount)
		failed := int(b.Failed.DocCount)

		// Sanity check: Ensure success + failed does not exceed total
		if success+failed < total {
			// Unclassified transactions count towards failed/unconfirmed
			failed = total - success
		} else if success > total {
			success = total
			failed = 0
		}

		record := repository.MetricRecord{
			ApplicationName:     appName,
			SourceName:          sourceName,
			MetricTime:          metricTime,
			TotalTransactions:   total,
			SuccessTransactions: success,
			FailedTransactions:  failed,
			IsComplete:          true, // Verified aggregation response from ES
			CollectedAt:         now,
		}

		records = append(records, record)
	}

	return records, nil
}
