package store

import (
	"path/filepath"
	"testing"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestDeviceRoundTripWithBindings(t *testing.T) {
	st := open(t)
	if err := st.SaveEnvironment(domain.Environment{ID: "env-a", Name: "A", TargetTempC: 24}); err != nil {
		t.Fatal(err)
	}
	dev := domain.Device{
		ID: "ctrl", Name: "Controller", EnvironmentID: "env-a", Adapter: "homeassistant",
		TempEntity: "sensor.t", HumidityEntity: "sensor.h",
		Channels: []domain.Channel{
			{ID: "fan1", Name: "Exhaust", Role: domain.RoleExhaust, Entity: "fan.exhaust", RPMEntity: "sensor.rpm"},
		},
	}
	if err := st.SaveDevice(dev); err != nil {
		t.Fatal(err)
	}
	got, err := st.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TempEntity != "sensor.t" {
		t.Fatalf("temp entity not persisted: %+v", got)
	}
	if got[0].Channels[0].Entity != "fan.exhaust" || got[0].Channels[0].RPMEntity != "sensor.rpm" {
		t.Fatalf("channel bindings not persisted: %+v", got[0].Channels)
	}
}

func TestDeleteEnvironmentBlockedWhileDeviceReferences(t *testing.T) {
	st := open(t)
	_ = st.SaveEnvironment(domain.Environment{ID: "env-a", Name: "A"})
	_ = st.SaveDevice(domain.Device{ID: "ctrl", Name: "C", EnvironmentID: "env-a", Adapter: "simulator"})

	if err := st.DeleteEnvironment("env-a"); err == nil {
		t.Fatal("expected deletion to be blocked while a device references the environment")
	}
	if err := st.DeleteDevice("ctrl"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteEnvironment("env-a"); err != nil {
		t.Fatalf("delete after removing device: %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	st1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st1.Close()
	st2, err := Open(path) // reopening re-runs migrate; must not error
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st2.Close()
}
