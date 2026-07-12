package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

type preferences struct {
	Version  int    `json:"version" yaml:"version"`
	Timezone string `json:"timezone" yaml:"timezone"`
	Locale   string `json:"locale" yaml:"locale"`
}

var preferencesMu sync.Mutex

func (s *Server) readPreferences() (preferences, error) {
	preferencesMu.Lock()
	defer preferencesMu.Unlock()
	p := preferences{Version: 1, Timezone: "UTC", Locale: "en-US"}
	raw, err := os.ReadFile(s.preferencesPath)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("parse preferences: %w", err)
	}
	if p.Version == 0 {
		p.Version = 1
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return p, fmt.Errorf("invalid timezone %q", p.Timezone)
	}
	if p.Locale == "" {
		p.Locale = "en-US"
	}
	if !validLocale(p.Locale) {
		return p, fmt.Errorf("invalid locale %q", p.Locale)
	}
	return p, nil
}

func validLocale(s string) bool {
	if len(s) < 2 || len(s) > 35 {
		return false
	}
	for i, r := range s {
		switch {
		case r == '-':
		case unicode.IsLetter(r), unicode.IsDigit(r):
		default:
			return false
		}
		if i == 0 && !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := s.readPreferences()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) putPreferences(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Timezone string `json:"timezone"`
		Locale   string `json:"locale"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := time.LoadLocation(body.Timezone); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("unknown IANA timezone"))
		return
	}
	if !validLocale(body.Locale) {
		writeJSON(w, http.StatusBadRequest, errBody("invalid locale"))
		return
	}
	p := preferences{Version: 1, Timezone: body.Timezone, Locale: body.Locale}
	raw, err := yaml.Marshal(p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	preferencesMu.Lock()
	defer preferencesMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.preferencesPath), 0750); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	tmp := s.preferencesPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0640); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.Rename(tmp, s.preferencesPath); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity("", "", "info", "configuration", fmt.Sprintf("Updated instance preferences (timezone=%s, locale=%s)", body.Timezone, body.Locale))
	writeJSON(w, http.StatusOK, p)
}
