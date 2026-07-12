package api

import (
	"net/http"

	"github.com/growrig/growrig-platform/growcore/internal/ha"
)

// The Home Assistant admin tab is the appliance-management surface: it reports
// whether Grow Core is connected to Home Assistant and, when running as a HAOS
// add-on, whether Core/OS/Supervisor/add-ons are up to date — and lets an admin
// trigger updates. All handlers are wired behind requireAdmin.

type homeAssistantStatus struct {
	// Adapter is how Grow Core reaches devices: "simulator" or "homeassistant".
	Adapter string `json:"adapter"`
	// Health is the live connection health to Home Assistant / the simulator.
	Health string `json:"health"`
	// Supervisor carries HAOS appliance status; Available is false when Grow
	// Core is not running as an add-on.
	Supervisor ha.Status `json:"supervisor"`
}

func (s *Server) getHomeAssistant(w http.ResponseWriter, r *http.Request) {
	sup := ha.NewSupervisor()
	status := ha.Status{Available: false}
	if sup.Available() {
		status = sup.FetchStatus()
	}
	writeJSON(w, http.StatusOK, homeAssistantStatus{
		Adapter:    s.adapterType,
		Health:     string(s.adapter.Health()),
		Supervisor: status,
	})
}

func (s *Server) reloadHomeAssistant(w http.ResponseWriter, r *http.Request) {
	sup := ha.NewSupervisor()
	if !sup.Available() {
		writeJSON(w, http.StatusBadRequest, errBody("the Supervisor is only available when running as a Home Assistant add-on"))
		return
	}
	if err := sup.Reload(); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	s.activity("", "", "info", "configuration", "Checked Home Assistant for updates")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updateHomeAssistant(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
		Slug   string `json:"slug"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sup := ha.NewSupervisor()
	if !sup.Available() {
		writeJSON(w, http.StatusBadRequest, errBody("the Supervisor is only available when running as a Home Assistant add-on"))
		return
	}
	if err := sup.Update(body.Target, body.Slug); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	label := body.Target
	if body.Slug != "" {
		label = body.Slug
	}
	s.activity("", "", "info", "configuration", "Started Home Assistant update: "+label)
	w.WriteHeader(http.StatusAccepted)
}
