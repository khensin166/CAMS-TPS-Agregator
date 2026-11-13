package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the complete application configuration.
type Config struct {
	App      AppConfig
	Database DatabaseConfig
	OnPrem   WorkerConfig
	Cloud    WorkerConfig
}

// AppConfig holds core application settings.
type AppConfig struct {
	Name        string
	Environment string
	Port        int
	Timezone    string
	Location    *time.Location
}

// DatabaseConfig holds SQL Server connection and pool settings.
type DatabaseConfig struct {
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	CommandTimeout  time.Duration
}

// ConnectionString returns the ADO.NET / MSSQL connection string.
func (d DatabaseConfig) ConnectionString() string {
	return fmt.Sprintf("server=%s;port=%d;database=%s;user id=%s;password=%s;encrypt=disable;connection timeout=%d;",
		d.Host, d.Port, d.Database, d.User, d.Password, int(d.CommandTimeout.Seconds()))
}

// WorkerConfig holds independent configuration for an Elasticsearch poller.
type WorkerConfig struct {
	Enabled         bool
	SourceName      string
	URL             string
	User            string
	Password        string
	IndexPattern    string
	TimestampField  string
	PollInterval    time.Duration
	WatermarkDelay  time.Duration
	OverlapWindow   time.Duration
	RetentionPeriod time.Duration
}

// LoadConfig loads configuration from environment variables with sensible defaults.
func LoadConfig() (*Config, error) {
	tzStr := getEnv("APP_TIMEZONE", "Asia/Jakarta")
	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", tzStr, err)
	}

	cfg := &Config{
		App: AppConfig{
			Name:        getEnv("APP_NAME", "CAMS-TPS-Agregator"),
			Environment: getEnv("APP_ENV", "local"),
			Port:        getEnvInt("APP_PORT", 8080),
			Timezone:    tzStr,
			Location:    loc,
		},
		Database: DatabaseConfig{
			Host:            getEnv("DB_HOST", "127.0.0.1"),
			Port:            getEnvInt("DB_PORT", 1433),
			Database:        getEnv("DB_NAME", "CAMS_Monitoring"),
			User:            getEnv("DB_USER", "sa"),
			Password:        getEnv("DB_PASSWORD", ""),
			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
			CommandTimeout:  getEnvDuration("DB_COMMAND_TIMEOUT", 30*time.Second),
		},
		OnPrem: WorkerConfig{
			Enabled:         getEnvBool("ONPREM_ENABLED", true),
			SourceName:      getEnv("ONPREM_SOURCE_NAME", "onprem"),
			URL:             getEnv("ONPREM_ES_URL", "http://172.18.65.100:9200"),
			User:            getEnv("ONPREM_ES_USER", ""),
			Password:        getEnv("ONPREM_ES_PASSWORD", ""),
			IndexPattern:    getEnv("ONPREM_INDEX_PATTERN", "cams-transactions-*"),
			TimestampField:  getEnv("ONPREM_TIMESTAMP_FIELD", "@timestamp"),
			PollInterval:    getEnvDuration("ONPREM_POLL_INTERVAL", 10*time.Second),
			WatermarkDelay:  getEnvDuration("ONPREM_WATERMARK_DELAY", 60*time.Second),
			OverlapWindow:   getEnvDuration("ONPREM_OVERLAP_WINDOW", 120*time.Second),
			RetentionPeriod: getEnvDuration("ONPREM_RETENTION_PERIOD", 7*24*time.Hour),
		},
		Cloud: WorkerConfig{
			Enabled:         getEnvBool("CLOUD_ENABLED", true),
			SourceName:      getEnv("CLOUD_SOURCE_NAME", "cloud"),
			URL:             getEnv("CLOUD_ES_URL", "http://10.212.224.172:9200"),
			User:            getEnv("CLOUD_ES_USER", ""),
			Password:        getEnv("CLOUD_ES_PASSWORD", ""),
			IndexPattern:    getEnv("CLOUD_INDEX_PATTERN", "cams-transactions-*"),
			TimestampField:  getEnv("CLOUD_TIMESTAMP_FIELD", "@timestamp"),
			PollInterval:    getEnvDuration("CLOUD_POLL_INTERVAL", 10*time.Second),
			WatermarkDelay:  getEnvDuration("CLOUD_WATERMARK_DELAY", 60*time.Second),
			OverlapWindow:   getEnvDuration("CLOUD_OVERLAP_WINDOW", 120*time.Second),
			RetentionPeriod: getEnvDuration("CLOUD_RETENTION_PERIOD", 7*24*time.Hour),
		},
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		if intVal, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		if boolVal, err := strconv.ParseBool(strings.TrimSpace(val)); err == nil {
			return boolVal
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(val)); err == nil {
			return d
		}
	}
	return defaultVal
}
