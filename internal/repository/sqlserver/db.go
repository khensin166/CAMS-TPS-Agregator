package sqlserver

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cams-tps-aggregator/internal/config"
	_ "github.com/microsoft/go-mssqldb"
)

// Connect establishes a connection pool to Microsoft SQL Server.
func Connect(ctx context.Context, cfg config.DatabaseConfig, log *slog.Logger) (*sql.DB, error) {
	if log == nil {
		log = slog.Default()
	}

	connStr := cfg.ConnectionString()
	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlserver connection: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.CommandTimeout)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping sqlserver at %s:%d/%s: %w", cfg.Host, cfg.Port, cfg.Database, err)
	}

	log.Info("Successfully connected to SQL Server",
		slog.String("host", cfg.Host),
		slog.Int("port", cfg.Port),
		slog.String("database", cfg.Database),
		slog.Int("max_open_conns", cfg.MaxOpenConns),
		slog.Int("max_idle_conns", cfg.MaxIdleConns),
	)

	return db, nil
}

// RunMigrations discovers and executes all .sql migration files in order.
func RunMigrations(ctx context.Context, db *sql.DB, migrationsDir string, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}

	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory %q: %w", migrationsDir, err)
	}

	var sqlFiles []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".sql") {
			sqlFiles = append(sqlFiles, filepath.Join(migrationsDir, f.Name()))
		}
	}
	sort.Strings(sqlFiles)

	for _, path := range sqlFiles {
		log.Info("Executing migration", slog.String("file", filepath.Base(path)))
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration file %q: %w", path, err)
		}

		// Split on GO statements if present, otherwise execute whole batch
		statements := strings.Split(string(content), "\nGO\n")
		for _, stmt := range statements {
			trimmed := strings.TrimSpace(stmt)
			if trimmed == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, trimmed); err != nil {
				return fmt.Errorf("failed executing migration statement in %q: %w", filepath.Base(path), err)
			}
		}
	}

	log.Info("All database schema migrations applied successfully", slog.Int("count", len(sqlFiles)))
	return nil
}
