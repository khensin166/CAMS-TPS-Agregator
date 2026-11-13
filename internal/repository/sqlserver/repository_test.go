package sqlserver

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"cams-tps-aggregator/internal/repository"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestSQLRepository_GetCheckpoint_Found(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewSQLRepository(db, 2*time.Second, nil)
	now := time.Now().UTC()

	rows := sqlmock.NewRows([]string{"source_name", "index_pattern", "last_processed_until", "last_success_at", "updated_at"}).
		AddRow("onprem", "cams-*", now, now, now)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT source_name, index_pattern, last_processed_until, last_success_at, updated_at FROM dbo.tps_checkpoints")).
		WithArgs("onprem", "cams-*").
		WillReturnRows(rows)

	cp, err := repo.GetCheckpoint(context.Background(), "onprem", "cams-*")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cp == nil || cp.SourceName != "onprem" {
		t.Errorf("expected checkpoint onprem, got: %+v", cp)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestSQLRepository_GetCheckpoint_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewSQLRepository(db, 2*time.Second, nil)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT source_name, index_pattern, last_processed_until, last_success_at, updated_at FROM dbo.tps_checkpoints")).
		WithArgs("onprem", "cams-*").
		WillReturnError(sql.ErrNoRows)

	cp, err := repo.GetCheckpoint(context.Background(), "onprem", "cams-*")
	if err != nil {
		t.Fatalf("expected nil error on ErrNoRows, got: %v", err)
	}
	if cp != nil {
		t.Errorf("expected nil checkpoint, got: %v", cp)
	}
}

func TestSQLRepository_SaveMetricsAndAdvanceCheckpoint_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewSQLRepository(db, 2*time.Second, nil)
	now := time.Now().UTC()

	metrics := []repository.MetricRecord{
		{
			ApplicationName:     "CAMS",
			SourceName:          "onprem",
			MetricTime:          now,
			TotalTransactions:   100,
			SuccessTransactions: 98,
			FailedTransactions:  2,
			IsComplete:          true,
			CollectedAt:         now,
		},
	}

	cp := repository.Checkpoint{
		SourceName:         "onprem",
		IndexPattern:       "cams-*",
		LastProcessedUntil: now,
		LastSuccessAt:      now,
		UpdatedAt:          now,
	}

	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta("MERGE dbo.tps_metrics WITH (HOLDLOCK)"))
	mock.ExpectExec(regexp.QuoteMeta("MERGE dbo.tps_metrics WITH (HOLDLOCK)")).
		WithArgs("CAMS", "onprem", now, 100, 98, 2, true, now).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec(regexp.QuoteMeta("MERGE dbo.tps_checkpoints WITH (HOLDLOCK)")).
		WithArgs("onprem", "cams-*", now, now, now).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	err = repo.SaveMetricsAndAdvanceCheckpoint(context.Background(), metrics, cp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestSQLRepository_SaveMetricsAndAdvanceCheckpoint_RollbackOnError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewSQLRepository(db, 2*time.Second, nil)
	now := time.Now().UTC()

	metrics := []repository.MetricRecord{
		{
			ApplicationName:     "CAMS",
			SourceName:          "onprem",
			MetricTime:          now,
			TotalTransactions:   100,
			SuccessTransactions: 98,
			FailedTransactions:  2,
			IsComplete:          true,
			CollectedAt:         now,
		},
	}

	cp := repository.Checkpoint{
		SourceName:         "onprem",
		IndexPattern:       "cams-*",
		LastProcessedUntil: now,
	}

	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta("MERGE dbo.tps_metrics WITH (HOLDLOCK)"))
	mock.ExpectExec(regexp.QuoteMeta("MERGE dbo.tps_metrics WITH (HOLDLOCK)")).
		WithArgs("CAMS", "onprem", now, 100, 98, 2, true, now).
		WillReturnError(errors.New("deadlock victim"))
	mock.ExpectRollback()

	err = repo.SaveMetricsAndAdvanceCheckpoint(context.Background(), metrics, cp)
	if err == nil {
		t.Fatalf("expected error on failed metric exec")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestSQLRepository_CalculateAndSaveSummary(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewSQLRepository(db, 2*time.Second, nil)
	now := time.Now().UTC()
	start := now.Add(-1 * time.Hour)
	end := now

	queryRows := sqlmock.NewRows([]string{"min_tps", "max_tps", "total_trx"}).
		AddRow(10, 500, int64(360000))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ISNULL(MIN(total_transactions), 0) AS min_tps, ISNULL(MAX(total_transactions), 0) AS max_tps, ISNULL(SUM(CAST(total_transactions AS BIGINT)), 0) AS total_trx FROM dbo.tps_metrics")).
		WithArgs("CAMS", "onprem", start, end).
		WillReturnRows(queryRows)

	mock.ExpectExec(regexp.QuoteMeta("MERGE dbo.tps_summary WITH (HOLDLOCK)")).
		WithArgs("CAMS", "onprem", "daily", start, end, 10, 500, float64(100.0), int64(360000), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	summary, err := repo.CalculateAndSaveSummary(context.Background(), "CAMS", "onprem", "daily", start, end)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.MinTPS != 10 || summary.MaxTPS != 500 || summary.TotalTransactions != 360000 {
		t.Errorf("summary metrics mismatch: %+v", summary)
	}
	if summary.AvgTPS != 100.0 {
		t.Errorf("expected avg_tps 100.0 (360000/3600s), got: %f", summary.AvgTPS)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
