package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

func (s *Server) getEnvironmentConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := s.store.EnvironmentYAML(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) putEnvironmentConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SaveEnvironmentYAML(r.PathValue("id"), raw); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	s.activity(r.PathValue("id"), "", "info", "configuration", "Updated environment YAML configuration")
	w.WriteHeader(http.StatusNoContent)
}

// --- Environments ---

type environmentBody struct {
	Name            string                 `json:"name"`
	Kind            domain.EnvironmentKind `json:"kind"`
	AirSourceID     string                 `json:"airSourceId"`
	LocationID      string                 `json:"locationId"`
	Model           string                 `json:"model"`
	WidthCm         float64                `json:"widthCm"`
	DepthCm         float64                `json:"depthCm"`
	HeightCm        float64                `json:"heightCm"`
	TargetTempC     float64                `json:"targetTempC"`
	TargetHumidity  float64                `json:"targetHumidity"`
	TargetCO2       float64                `json:"targetCO2"`
	EmergencyTempC  float64                `json:"emergencyTempC"`
	LeafTempOffsetC *float64               `json:"leafTempOffsetC"`
}

func (s *Server) createEnvironment(w http.ResponseWriter, r *http.Request) {
	var b environmentBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	env, err := s.buildEnvironment(id(b.Name, "env"), b)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	if err := s.store.SaveEnvironment(env); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity(env.ID, "", "info", "configuration", "Created environment "+env.Name)
	writeJSON(w, http.StatusCreated, env)
}

func (s *Server) updateEnvironment(w http.ResponseWriter, r *http.Request) {
	var b environmentBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	env, err := s.buildEnvironment(r.PathValue("id"), b)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	if err := s.store.SaveEnvironment(env); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity(env.ID, "", "info", "configuration", "Updated environment settings")
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) deleteEnvironment(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("id")
	if err := s.store.DeleteEnvironment(envID); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	s.activity(envID, "", "info", "configuration", "Deleted environment")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) buildEnvironment(envID string, b environmentBody) (domain.Environment, error) {
	if strings.TrimSpace(b.Name) == "" {
		return domain.Environment{}, fmt.Errorf("name is required")
	}
	kind := b.Kind
	if kind == "" {
		kind = domain.KindTent
	}
	if kind != domain.KindTent && kind != domain.KindRoom {
		return domain.Environment{}, fmt.Errorf("unknown kind %q", kind)
	}
	env := domain.Environment{
		ID: envID, Name: b.Name, Kind: kind, AirSourceID: b.AirSourceID,
		LocationID:      b.LocationID,
		Model:           b.Model,
		WidthCm:         b.WidthCm,
		DepthCm:         b.DepthCm,
		HeightCm:        b.HeightCm,
		TargetTempC:     orDefault(b.TargetTempC, 24),
		TargetHumidity:  orDefault(b.TargetHumidity, 55),
		TargetCO2:       b.TargetCO2,
		EmergencyTempC:  orDefault(b.EmergencyTempC, 35),
		LeafTempOffsetC: -2,
	}
	if b.LeafTempOffsetC != nil {
		env.LeafTempOffsetC = *b.LeafTempOffsetC
	}
	if env.LeafTempOffsetC < -10 || env.LeafTempOffsetC > 10 {
		return domain.Environment{}, fmt.Errorf("leafTempOffsetC must be between -10 and 10")
	}
	if kind == domain.KindRoom {
		env.AirSourceID = "" // rooms don't have an air source
	}
	if env.AirSourceID != "" && env.AirSourceID == envID {
		return domain.Environment{}, fmt.Errorf("an environment cannot be its own air source")
	}
	return env, nil
}

// --- Bindings ---

type bindingBody struct {
	DeviceID            string             `json:"deviceId"`
	DeviceName          string             `json:"deviceName"`
	PowerControllerID   string             `json:"powerControllerId"`
	ControllerChannelID string             `json:"controllerChannelId"`
	EnvironmentID       string             `json:"environmentId"`
	Kind                domain.BindingKind `json:"kind"`
	Name                string             `json:"name"`
	Entity              string             `json:"entity"`
	Measurement         domain.Measurement `json:"measurement"`
	Role                domain.Role        `json:"role"`
	RPMEntity           string             `json:"rpmEntity"`
	SizeMM              int                `json:"sizeMm"`
	MaxRPM              int                `json:"maxRpm"`
	AirflowCFM          float64            `json:"airflowCfm"`
	StaticPressureMMH2O float64            `json:"staticPressureMmH2O"`
	StartingVoltage     float64            `json:"startingVoltage"`
	Wattage             float64            `json:"wattage"`
	Primary             bool               `json:"primary"`
}

func (s *Server) createBinding(w http.ResponseWriter, r *http.Request) {
	var b bindingBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	binding, err := s.buildBinding(id(b.Name, string(b.Kind)), b)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	if err := s.store.SaveBinding(binding); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if binding.Kind == domain.KindLight {
		if binding.Primary {
			if err := s.store.SetPrimaryLight(binding.EnvironmentID, binding.ID); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			binding.Primary = true
		} else if err := s.store.EnsurePrimaryLight(binding.EnvironmentID); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.activity(binding.EnvironmentID, binding.DeviceID, "info", "configuration", "Added "+binding.DeviceName+" capability "+binding.Name)
	writeJSON(w, http.StatusCreated, binding)
}

func (s *Server) updateBinding(w http.ResponseWriter, r *http.Request) {
	var b bindingBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	binding, err := s.buildBinding(r.PathValue("id"), b)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	if err := s.store.SaveBinding(binding); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if binding.Kind == domain.KindLight {
		if binding.Primary {
			if err := s.store.SetPrimaryLight(binding.EnvironmentID, binding.ID); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		} else if err := s.store.EnsurePrimaryLight(binding.EnvironmentID); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.activity(binding.EnvironmentID, binding.DeviceID, "info", "configuration", "Updated "+binding.DeviceName+" capability "+binding.Name)
	writeJSON(w, http.StatusOK, binding)
}

func (s *Server) deleteBinding(w http.ResponseWriter, r *http.Request) {
	bindingID := r.PathValue("id")

	// Remember the environment of a light so we can promote a new primary
	// after removal.
	var lightEnv string
	var removed domain.Binding
	if all, err := s.store.Bindings(); err == nil {
		for _, b := range all {
			if b.ID == bindingID {
				removed = b
				if b.Kind == domain.KindLight {
					lightEnv = b.EnvironmentID
				}
				break
			}
		}
	}

	if err := s.store.DeleteBinding(bindingID); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if lightEnv != "" {
		if err := s.store.EnsurePrimaryLight(lightEnv); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if removed.ID != "" {
		s.activity(removed.EnvironmentID, removed.DeviceID, "info", "configuration", "Removed "+removed.DeviceName+" capability "+removed.Name)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) buildBinding(bindingID string, b bindingBody) (domain.Binding, error) {
	if strings.TrimSpace(b.DeviceID) == "" || strings.TrimSpace(b.DeviceName) == "" {
		return domain.Binding{}, fmt.Errorf("deviceId and deviceName are required")
	}
	if strings.TrimSpace(b.Name) == "" {
		return domain.Binding{}, fmt.Errorf("name is required")
	}
	if strings.TrimSpace(b.Entity) == "" && b.Kind != domain.KindLight && b.Kind != domain.KindFan {
		return domain.Binding{}, fmt.Errorf("entity is required")
	}
	envs, err := s.store.Environments()
	if err != nil {
		return domain.Binding{}, err
	}
	if !containsEnv(envs, b.EnvironmentID) {
		return domain.Binding{}, fmt.Errorf("unknown environment %q", b.EnvironmentID)
	}
	if b.Kind == domain.KindLight && b.PowerControllerID != "" {
		bindings, err := s.store.Bindings()
		if err != nil {
			return domain.Binding{}, err
		}
		validController := false
		for _, candidate := range bindings {
			if candidate.EnvironmentID == b.EnvironmentID && candidate.DeviceID == b.PowerControllerID && candidate.Kind == domain.KindPower {
				validController = true
				break
			}
		}
		if !validController {
			return domain.Binding{}, fmt.Errorf("unknown power controller %q", b.PowerControllerID)
		}
	}

	binding := domain.Binding{
		ID: bindingID, DeviceID: b.DeviceID, DeviceName: b.DeviceName,
		PowerControllerID: b.PowerControllerID, ControllerChannelID: b.ControllerChannelID, EnvironmentID: b.EnvironmentID, Kind: b.Kind, Name: b.Name, Entity: b.Entity,
	}
	switch b.Kind {
	case domain.KindSensor:
		switch b.Measurement {
		case domain.MeasureTemperature, domain.MeasureHumidity, domain.MeasureCO2, domain.MeasurePower:
			binding.Measurement = b.Measurement
		default:
			return domain.Binding{}, fmt.Errorf("sensor needs a measurement (temperature, humidity, co2 or power)")
		}
	case domain.KindFan:
		binding.Entity = ""
		if b.ControllerChannelID == "" {
			return domain.Binding{}, fmt.Errorf("fan needs a controller channel")
		}
		binding.ControllerChannelID = b.ControllerChannelID
		role := b.Role
		if role == "" {
			role = domain.RoleUnassigned
		}
		if !validRole(role) {
			return domain.Binding{}, fmt.Errorf("unknown role %q", role)
		}
		binding.Role = role
		if b.SizeMM < 0 || b.SizeMM > 1000 || b.MaxRPM < 0 || b.MaxRPM > 100000 || b.AirflowCFM < 0 || b.StaticPressureMMH2O < 0 || b.StartingVoltage < 0 || b.StartingVoltage > 48 {
			return domain.Binding{}, fmt.Errorf("fan specifications must be positive and within supported ranges")
		}
		binding.SizeMM, binding.MaxRPM, binding.AirflowCFM = b.SizeMM, b.MaxRPM, b.AirflowCFM
		binding.StaticPressureMMH2O, binding.StartingVoltage = b.StaticPressureMMH2O, b.StartingVoltage
	case domain.KindController:
		role := b.Role
		if role == "" {
			role = domain.RoleUnassigned
		}
		if !validRole(role) {
			return domain.Binding{}, fmt.Errorf("unknown role %q", role)
		}
		binding.Role = role
		binding.RPMEntity = b.RPMEntity
	case domain.KindLight:
		binding.Entity = "" // fixtures never bind directly to Home Assistant
		if b.Wattage < 0 || b.Wattage > 100000 {
			return domain.Binding{}, fmt.Errorf("wattage must be between 0 and 100000")
		}
		binding.Wattage = b.Wattage
		binding.Primary = b.Primary
	case domain.KindPower:
		// A switchable power controller capability.
	case domain.KindCamera:
		// no extra fields
	default:
		return domain.Binding{}, fmt.Errorf("unknown binding kind %q", b.Kind)
	}
	return binding, nil
}

// --- helpers ---

func containsEnv(envs []domain.Environment, id string) bool {
	for _, e := range envs {
		if e.ID == id {
			return true
		}
	}
	return false
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }

func orDefault(v, def float64) float64 {
	if v == 0 {
		return def
	}
	return v
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// id builds a readable, unique id from a name plus a short random suffix.
func id(name, prefix string) string {
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = prefix
	}
	return slug + "-" + randHex(3)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
