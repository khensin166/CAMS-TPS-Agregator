package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear any overrides
	os.Clearenv()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected no error loading defaults, got: %v", err)
	}

	if cfg.App.Name != "CAMS-TPS-Agregator" {
		t.Errorf("expected App.Name to be CAMS-TPS-Agregator, got: %s", cfg.App.Name)
	}

	if cfg.Database.Database != "CAMS_Monitoring" {
		t.Errorf("expected Database to be CAMS_Monitoring, got: %s", cfg.Database.Database)
	}

	if cfg.OnPrem.PollInterval != 10*time.Second {
		t.Errorf("expected OnPrem.PollInterval to be 10s, got: %v", cfg.OnPrem.PollInterval)
	}

	if cfg.Cloud.WatermarkDelay != 60*time.Second {
		t.Errorf("expected Cloud.WatermarkDelay to be 60s, got: %v", cfg.Cloud.WatermarkDelay)
	}

	if cfg.App.Location == nil || cfg.App.Location.String() != "Asia/Jakarta" {
		t.Errorf("expected timezone Asia/Jakarta, got: %v", cfg.App.Location)
	}
}

func TestLoadConfig_Overrides(t *testing.T) {
	os.Setenv("APP_NAME", "Custom-TPS")
	os.Setenv("DB_NAME", "CustomDB")
	os.Setenv("ONPREM_POLL_INTERVAL", "5s")
	os.Setenv("ONPREM_ENABLED", "false")
	defer os.Clearenv()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.App.Name != "Custom-TPS" {
		t.Errorf("expected Custom-TPS, got %s", cfg.App.Name)
	}
	if cfg.Database.Database != "CustomDB" {
		t.Errorf("expected CustomDB, got %s", cfg.Database.Database)
	}
	if cfg.OnPrem.PollInterval != 5*time.Second {
		t.Errorf("expected 5s, got %v", cfg.OnPrem.PollInterval)
	}
	if cfg.OnPrem.Enabled {
		t.Errorf("expected OnPrem.Enabled to be false")
	}
}

