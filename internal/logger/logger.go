package logger

import (
	"log/slog"
	"os"
	"strings"
)

// InitLogger initializes and sets the default slog logger based on environment.
func InitLogger(env string) *slog.Logger {
	var handler slog.Handler
	level := slog.LevelInfo

	opts := &slog.HandlerOptions{
		Level: level,
	}

	if strings.ToLower(env) == "production" || strings.ToLower(env) == "prod" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	l := slog.New(handler)
	slog.SetDefault(l)
	return l
}
