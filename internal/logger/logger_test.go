package logger

import (
	"log/slog"
	"testing"
)

func TestInitLogger(t *testing.T) {
	localLog := InitLogger("local")
	if localLog == nil {
		t.Fatal("expected non-nil logger for local env")
	}
	localLog.Info("test local log", slog.String("key", "val"))

	prodLog := InitLogger("production")
	if prodLog == nil {
		t.Fatal("expected non-nil logger for production env")
	}
	prodLog.Info("test prod log", slog.String("key", "val"))
}

