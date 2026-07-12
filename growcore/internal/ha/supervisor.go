package ha

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Supervisor talks to the Home Assistant Supervisor API, available when Grow
// Core runs as a Home Assistant OS add-on. The Supervisor injects a
// SUPERVISOR_TOKEN and is reachable at http://supervisor. This is how GrowRig
// reports whether Home Assistant Core, the OS, the Supervisor and add-ons are
// up to date, and can trigger their updates — so a user managing the whole
// appliance never has to leave GrowRig.
//
// The base URL is overridable via GROWCORE_SUPERVISOR_URL so the integration
// can be exercised against a mock during development.
type Supervisor struct {
	base   string
	token  string
	client *http.Client
}

func NewSupervisor() *Supervisor {
	base := os.Getenv("GROWCORE_SUPERVISOR_URL")
	if base == "" {
		base = "http://supervisor"
	}
	return &Supervisor{
		base:   strings.TrimRight(base, "/"),
		token:  os.Getenv("SUPERVISOR_TOKEN"),
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

// Available reports whether a Supervisor token is present, i.e. Grow Core is
// running as a HAOS add-on and can surface appliance-level status.
func (s *Supervisor) Available() bool { return s.token != "" }

// Component is the version/update state of one updatable piece (Core, OS,
// Supervisor, or an add-on).
type Component struct {
	Version         string `json:"version"`
	VersionLatest   string `json:"versionLatest"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// Addon is an installed add-on's update state.
type Addon struct {
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	VersionLatest   string `json:"versionLatest"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// Status is the appliance-level snapshot shown on the Home Assistant admin tab.
type Status struct {
	Available  bool      `json:"available"`
	Core       Component `json:"core"`
	OS         Component `json:"os"`
	Supervisor Component `json:"supervisor"`
	Addons     []Addon   `json:"addons"`
	// Error is set when the Supervisor is present but a query failed, so the UI
	// can distinguish "not an add-on" from "add-on, but Supervisor unreachable".
	Error string `json:"error,omitempty"`
}

// wire mirrors the Supervisor's snake_case component payload.
type wireComponent struct {
	Version         string `json:"version"`
	VersionLatest   string `json:"version_latest"`
	UpdateAvailable bool   `json:"update_available"`
}

func (c wireComponent) toComponent() Component {
	return Component{Version: c.Version, VersionLatest: c.VersionLatest, UpdateAvailable: c.UpdateAvailable}
}

// get fetches a Supervisor endpoint and unwraps its {"result","data"} envelope
// into out.
func (s *Supervisor) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, s.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("supervisor %s: HTTP %d", path, resp.StatusCode)
	}
	var env struct {
		Result string          `json:"result"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return err
	}
	return json.Unmarshal(env.Data, out)
}

// post issues a Supervisor action (e.g. an update or reload).
func (s *Supervisor) post(path string) error {
	req, err := http.NewRequest(http.MethodPost, s.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supervisor %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// FetchStatus gathers Core, OS, Supervisor and add-on update state. A partial
// failure is reported via Status.Error rather than failing the whole call.
func (s *Supervisor) FetchStatus() Status {
	status := Status{Available: true}
	var core, osInfo, sup wireComponent
	if err := s.get("/core/info", &core); err != nil {
		status.Error = err.Error()
	} else {
		status.Core = core.toComponent()
	}
	if err := s.get("/os/info", &osInfo); err == nil {
		status.OS = osInfo.toComponent()
	}
	if err := s.get("/supervisor/info", &sup); err == nil {
		status.Supervisor = sup.toComponent()
	}
	var addonsData struct {
		Addons []struct {
			Slug            string `json:"slug"`
			Name            string `json:"name"`
			Version         string `json:"version"`
			VersionLatest   string `json:"version_latest"`
			UpdateAvailable bool   `json:"update_available"`
		} `json:"addons"`
	}
	if err := s.get("/addons", &addonsData); err == nil {
		for _, a := range addonsData.Addons {
			status.Addons = append(status.Addons, Addon{
				Slug: a.Slug, Name: a.Name, Version: a.Version,
				VersionLatest: a.VersionLatest, UpdateAvailable: a.UpdateAvailable,
			})
		}
		// Surface add-ons with pending updates first, then alphabetically.
		sort.Slice(status.Addons, func(i, j int) bool {
			if status.Addons[i].UpdateAvailable != status.Addons[j].UpdateAvailable {
				return status.Addons[i].UpdateAvailable
			}
			return status.Addons[i].Name < status.Addons[j].Name
		})
	}
	return status
}

// Reload asks the Supervisor to refresh its view of available versions (a
// "check for updates").
func (s *Supervisor) Reload() error { return s.post("/store/reload") }

// Update triggers an update of a target: "core", "os", "supervisor", or
// "addon" (with slug). The Supervisor performs it asynchronously.
func (s *Supervisor) Update(target, slug string) error {
	switch target {
	case "core":
		return s.post("/core/update")
	case "os":
		return s.post("/os/update")
	case "supervisor":
		return s.post("/supervisor/update")
	case "addon":
		if slug == "" {
			return fmt.Errorf("addon slug is required")
		}
		return s.post("/addons/" + slug + "/update")
	default:
		return fmt.Errorf("unknown update target %q", target)
	}
}
