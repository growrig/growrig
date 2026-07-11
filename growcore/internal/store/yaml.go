package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
	"gopkg.in/yaml.v3"
)

func (s *Store) EnvironmentYAML(envID string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.configDir, envID, "environment.yaml"))
}

func (s *Store) SaveEnvironmentYAML(envID string, raw []byte) error {
	var doc environmentDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	if doc.ID != envID {
		return fmt.Errorf("environment id must remain %q", envID)
	}
	if doc.Name == "" || doc.Kind == "" {
		return fmt.Errorf("environment name and type are required")
	}
	for _, dev := range doc.Devices {
		if dev.ID == "" || dev.Name == "" {
			return fmt.Errorf("every device needs an id and name")
		}
		if len(dev.Capabilities) == 0 {
			return fmt.Errorf("device %q needs at least one capability", dev.Name)
		}
		for _, cap := range dev.Capabilities {
			if cap.ID == "" || cap.Kind == "" || cap.Name == "" {
				return fmt.Errorf("every capability on device %q needs an id, kind and name", dev.Name)
			}
		}
	}
	dir := filepath.Join(s.configDir, envID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".environment.yaml.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, "environment.yaml")); err != nil {
		return err
	}
	return s.syncYAMLConfig()
}

// environmentDocument is the portable, user-editable representation of all
// static configuration owned by one environment.
type environmentDocument struct {
	Version            int `yaml:"version"`
	domain.Environment `yaml:",inline"`
	Devices            []deviceDocument `yaml:"devices,omitempty"`
}

type deviceDocument struct {
	ID                  string               `yaml:"id"`
	Name                string               `yaml:"name"`
	PowerControllerID   string               `yaml:"powerControllerId,omitempty"`
	ControllerChannelID string               `yaml:"controllerChannelId,omitempty"`
	Capabilities        []capabilityDocument `yaml:"capabilities"`
}

type capabilityDocument struct {
	ID                  string             `yaml:"id"`
	Kind                domain.BindingKind `yaml:"kind"`
	Name                string             `yaml:"name"`
	Entity              string             `yaml:"entity,omitempty"`
	Measurement         domain.Measurement `yaml:"measurement,omitempty"`
	Role                domain.Role        `yaml:"role,omitempty"`
	RPMEntity           string             `yaml:"rpmEntity,omitempty"`
	SizeMM              int                `yaml:"sizeMm,omitempty"`
	MaxRPM              int                `yaml:"maxRpm,omitempty"`
	AirflowCFM          float64            `yaml:"airflowCfm,omitempty"`
	StaticPressureMMH2O float64            `yaml:"staticPressureMmH2O,omitempty"`
	StartingVoltage     float64            `yaml:"startingVoltage,omitempty"`
	Wattage             float64            `yaml:"wattage,omitempty"`
	Primary             bool               `yaml:"primary,omitempty"`
}

func (s *Store) syncYAMLConfig() error {
	paths, err := filepath.Glob(filepath.Join(s.configDir, "*", "environment.yaml"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		if err := os.MkdirAll(s.configDir, 0o755); err != nil {
			return err
		}
		envs, err := s.Environments()
		if err != nil {
			return err
		}
		for _, env := range envs {
			if err := s.writeEnvironmentConfig(env.ID); err != nil {
				return err
			}
		}
		return nil
	}

	docs := make([]environmentDocument, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc environmentDocument
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if doc.ID == "" || doc.Name == "" {
			return fmt.Errorf("%s: id and name are required", path)
		}
		docs = append(docs, doc)
	}

	s.syncing = true
	defer func() { s.syncing = false }()
	if _, err := s.db.Exec(`DELETE FROM bindings; DELETE FROM environments`); err != nil {
		return err
	}
	for _, doc := range docs {
		if err := s.SaveEnvironment(doc.Environment); err != nil {
			return err
		}
		for _, dev := range doc.Devices {
			for _, cap := range dev.Capabilities {
				b := domain.Binding{
					ID: cap.ID, DeviceID: dev.ID, DeviceName: dev.Name,
					PowerControllerID: dev.PowerControllerID, ControllerChannelID: dev.ControllerChannelID,
					EnvironmentID: doc.ID, Kind: cap.Kind, Name: cap.Name, Entity: cap.Entity,
					Measurement: cap.Measurement, Role: cap.Role, RPMEntity: cap.RPMEntity, SizeMM: cap.SizeMM, MaxRPM: cap.MaxRPM, AirflowCFM: cap.AirflowCFM, StaticPressureMMH2O: cap.StaticPressureMMH2O, StartingVoltage: cap.StartingVoltage,
					Wattage: cap.Wattage, Primary: cap.Primary,
				}
				if err := s.SaveBinding(b); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Store) writeEnvironmentConfig(envID string) error {
	if s.syncing || envID == "" {
		return nil
	}
	envs, err := s.Environments()
	if err != nil {
		return err
	}
	var env *domain.Environment
	for i := range envs {
		if envs[i].ID == envID {
			env = &envs[i]
			break
		}
	}
	if env == nil {
		return nil
	}
	bindings, err := s.Bindings()
	if err != nil {
		return err
	}
	byDevice := map[string]*deviceDocument{}
	for _, b := range bindings {
		if b.EnvironmentID != envID {
			continue
		}
		dev := byDevice[b.DeviceID]
		if dev == nil {
			dev = &deviceDocument{ID: b.DeviceID, Name: b.DeviceName}
			byDevice[b.DeviceID] = dev
		}
		if b.PowerControllerID != "" {
			dev.PowerControllerID = b.PowerControllerID
		}
		if b.ControllerChannelID != "" {
			dev.ControllerChannelID = b.ControllerChannelID
		}
		dev.Capabilities = append(dev.Capabilities, capabilityDocument{
			ID: b.ID, Kind: b.Kind, Name: b.Name, Entity: b.Entity,
			Measurement: b.Measurement, Role: b.Role, RPMEntity: b.RPMEntity, SizeMM: b.SizeMM, MaxRPM: b.MaxRPM, AirflowCFM: b.AirflowCFM, StaticPressureMMH2O: b.StaticPressureMMH2O, StartingVoltage: b.StartingVoltage,
			Wattage: b.Wattage, Primary: b.Primary,
		})
	}
	devices := make([]deviceDocument, 0, len(byDevice))
	for _, dev := range byDevice {
		sort.Slice(dev.Capabilities, func(i, j int) bool { return dev.Capabilities[i].ID < dev.Capabilities[j].ID })
		devices = append(devices, *dev)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	raw, err := yaml.Marshal(environmentDocument{Version: 1, Environment: *env, Devices: devices})
	if err != nil {
		return err
	}
	dir := filepath.Join(s.configDir, envID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".environment.yaml.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "environment.yaml"))
}

func (s *Store) removeEnvironmentConfig(envID string) error {
	err := os.RemoveAll(filepath.Join(s.configDir, envID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
