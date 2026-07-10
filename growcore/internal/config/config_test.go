package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "growcore.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadExpandsEnvAndParsesDuration(t *testing.T) {
	t.Setenv("TEST_HA_TOKEN", "secret-token")
	p := writeTemp(t, `
server:
  addr: ":9000"
control:
  interval: 3s
adapter:
  type: homeassistant
homeassistant:
  url: http://homeassistant.local:8123
  token: ${TEST_HA_TOKEN}
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HomeAssistant.Token != "secret-token" {
		t.Errorf("token not expanded: %q", cfg.HomeAssistant.Token)
	}
	if cfg.Control.Interval.Std().String() != "3s" {
		t.Errorf("interval = %s, want 3s", cfg.Control.Interval.Std())
	}
	if cfg.Server.Addr != ":9000" {
		t.Errorf("addr = %s", cfg.Server.Addr)
	}
}

func TestPartialConfigKeepsDefaults(t *testing.T) {
	p := writeTemp(t, "adapter:\n  type: simulator\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("addr default lost: %q", cfg.Server.Addr)
	}
	if cfg.Storage.Path != "growcore.db" {
		t.Errorf("storage default lost: %q", cfg.Storage.Path)
	}
}

func TestValidateRejectsHAWithoutToken(t *testing.T) {
	p := writeTemp(t, `
adapter:
  type: homeassistant
homeassistant:
  url: http://homeassistant.local:8123
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestDefaultIsSimulatorAndValid(t *testing.T) {
	cfg := Default()
	if cfg.Adapter.Type != AdapterSimulator {
		t.Errorf("default adapter = %s", cfg.Adapter.Type)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config invalid: %v", err)
	}
}
