package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// --- Environment management ---

type environmentBody struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	TargetTempC    float64 `json:"targetTempC"`
	TargetHumidity float64 `json:"targetHumidity"`
	EmergencyTempC float64 `json:"emergencyTempC"`
}

func (s *Server) createEnvironment(w http.ResponseWriter, r *http.Request) {
	var b environmentBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(b.Name) == "" {
		writeJSON(w, http.StatusBadRequest, errBody("name is required"))
		return
	}
	env := domain.Environment{
		ID:             id(b.ID, b.Name, "env"),
		Name:           b.Name,
		TargetTempC:    orDefault(b.TargetTempC, 24),
		TargetHumidity: orDefault(b.TargetHumidity, 55),
		EmergencyTempC: orDefault(b.EmergencyTempC, 35),
	}
	if err := s.store.SaveEnvironment(env); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, env)
}

func (s *Server) updateEnvironment(w http.ResponseWriter, r *http.Request) {
	var b environmentBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	env := domain.Environment{
		ID:             r.PathValue("id"),
		Name:           b.Name,
		TargetTempC:    b.TargetTempC,
		TargetHumidity: b.TargetHumidity,
		EmergencyTempC: b.EmergencyTempC,
	}
	if err := s.store.SaveEnvironment(env); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) deleteEnvironment(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteEnvironment(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Device management ---

type channelBody struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Role      domain.Role `json:"role"`
	Entity    string      `json:"entity"`
	RPMEntity string      `json:"rpmEntity"`
}

type deviceBody struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	EnvironmentID  string        `json:"environmentId"`
	Adapter        string        `json:"adapter"`
	TempEntity     string        `json:"tempEntity"`
	HumidityEntity string        `json:"humidityEntity"`
	Channels       []channelBody `json:"channels"`
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var b deviceBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	dev, err := s.buildDevice(id(b.ID, b.Name, "dev"), b)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	if err := s.store.SaveDevice(dev); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, dev)
}

func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request) {
	var b deviceBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	dev, err := s.buildDevice(r.PathValue("id"), b)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	if err := s.store.SaveDevice(dev); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, dev)
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteDevice(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// buildDevice validates the request and assembles a domain device, generating
// channel ids and validating environment + role references.
func (s *Server) buildDevice(deviceID string, b deviceBody) (domain.Device, error) {
	if strings.TrimSpace(b.Name) == "" {
		return domain.Device{}, fmt.Errorf("name is required")
	}
	envs, err := s.store.Environments()
	if err != nil {
		return domain.Device{}, err
	}
	known := false
	for _, e := range envs {
		if e.ID == b.EnvironmentID {
			known = true
			break
		}
	}
	if !known {
		return domain.Device{}, fmt.Errorf("unknown environment %q", b.EnvironmentID)
	}

	adapter := b.Adapter
	if adapter == "" {
		adapter = s.adapter
	}
	dev := domain.Device{
		ID: deviceID, Name: b.Name, EnvironmentID: b.EnvironmentID, Adapter: adapter,
		TempEntity: b.TempEntity, HumidityEntity: b.HumidityEntity,
	}
	seen := map[string]bool{}
	for _, c := range b.Channels {
		role := c.Role
		if role == "" {
			role = domain.RoleUnassigned
		}
		if !validRole(role) {
			return domain.Device{}, fmt.Errorf("unknown role %q", role)
		}
		cid := id(c.ID, c.Name, "ch")
		for seen[cid] { // guarantee uniqueness within the device
			cid = id("", c.Name, "ch")
		}
		seen[cid] = true
		dev.Channels = append(dev.Channels, domain.Channel{
			ID: cid, Name: c.Name, Role: role, Entity: c.Entity, RPMEntity: c.RPMEntity,
		})
	}
	return dev, nil
}

// --- helpers ---

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

// id returns explicit if non-empty, otherwise a slug of name plus a short
// random suffix, prefixed to keep ids readable and unique.
func id(explicit, name, prefix string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
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
