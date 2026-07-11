package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// httpClient is used for the outbound Nominatim / Open-Meteo lookups.
var httpClient = &http.Client{Timeout: 8 * time.Second}

// userAgent identifies Grow Core to Nominatim, whose usage policy requires it.
const userAgent = "GrowRig/1.0 (https://github.com/growrig/growrig-platform)"

// --- Locations CRUD ---

type locationBody struct {
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Address string  `json:"address"`
}

func (s *Server) getLocations(w http.ResponseWriter, r *http.Request) {
	locs, err := s.store.Locations()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if locs == nil {
		locs = []domain.Location{}
	}
	writeJSON(w, http.StatusOK, locs)
}

func (s *Server) createLocation(w http.ResponseWriter, r *http.Request) {
	var b locationBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.saveLocation(w, id(b.Name, "loc"), b)
}

func (s *Server) updateLocation(w http.ResponseWriter, r *http.Request) {
	var b locationBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.saveLocation(w, r.PathValue("id"), b)
}

func (s *Server) saveLocation(w http.ResponseWriter, locID string, b locationBody) {
	if b.Name == "" {
		writeJSON(w, http.StatusBadRequest, errBody("name is required"))
		return
	}
	if b.Lat < -90 || b.Lat > 90 || b.Lon < -180 || b.Lon > 180 {
		writeJSON(w, http.StatusBadRequest, errBody("coordinates out of range"))
		return
	}
	loc := domain.Location{ID: locID, Name: b.Name, Lat: b.Lat, Lon: b.Lon, Address: b.Address}
	if err := s.store.SaveLocation(loc); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, loc)
}

func (s *Server) deleteLocation(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteLocation(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Geocoding proxy (OSM Nominatim) ---

type geocodeResult struct {
	DisplayName string  `json:"displayName"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

func (s *Server) geocode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) < 3 {
		writeJSON(w, http.StatusOK, []geocodeResult{})
		return
	}
	cacheKey := "geo:" + q
	if cached, ok := extCache.get(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cached)
		return
	}
	endpoint := "https://nominatim.openstreetmap.org/search?" + url.Values{
		"q":      {q},
		"format": {"jsonv2"},
		"limit":  {"6"},
	}.Encode()
	body, err := s.fetch(r.Context(), endpoint)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	var raw []struct {
		DisplayName string `json:"display_name"`
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	out := make([]geocodeResult, 0, len(raw))
	for _, x := range raw {
		lat, _ := strconv.ParseFloat(x.Lat, 64)
		lon, _ := strconv.ParseFloat(x.Lon, 64)
		out = append(out, geocodeResult{DisplayName: x.DisplayName, Lat: lat, Lon: lon})
	}
	encoded, _ := json.Marshal(out)
	extCache.set(cacheKey, encoded, time.Hour)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(encoded)
}

// --- Weather proxy (Open-Meteo) ---

type weatherResponse struct {
	Temp     []domain.SeriesPoint `json:"temp"`
	Humidity []domain.SeriesPoint `json:"humidity"`
	Pressure []domain.SeriesPoint `json:"pressure"`
}

func (s *Server) getWeather(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, errLat := strconv.ParseFloat(q.Get("lat"), 64)
	lon, errLon := strconv.ParseFloat(q.Get("lon"), 64)
	if errLat != nil || errLon != nil {
		writeJSON(w, http.StatusBadRequest, errBody("lat and lon are required"))
		return
	}
	// Round the cache key so nearby requests share a lookup.
	cacheKey := fmt.Sprintf("wx:%.3f,%.3f", lat, lon)
	if cached, ok := extCache.get(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cached)
		return
	}
	out, err := s.fetchWeather(r.Context(), lat, lon, 4)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	encoded, _ := json.Marshal(out)
	extCache.set(cacheKey, encoded, 15*time.Minute)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(encoded)
}

// fetchWeather pulls hourly outdoor observations (pastDays back plus a short
// forecast) for a coordinate from Open-Meteo.
func (s *Server) fetchWeather(ctx context.Context, lat, lon float64, pastDays int) (weatherResponse, error) {
	var out weatherResponse
	endpoint := "https://api.open-meteo.com/v1/forecast?" + url.Values{
		"latitude":      {strconv.FormatFloat(lat, 'f', 4, 64)},
		"longitude":     {strconv.FormatFloat(lon, 'f', 4, 64)},
		"hourly":        {"temperature_2m,relative_humidity_2m,surface_pressure"},
		"past_days":     {strconv.Itoa(pastDays)},
		"forecast_days": {"2"},
		"timezone":      {"UTC"},
	}.Encode()
	body, err := s.fetch(ctx, endpoint)
	if err != nil {
		return out, err
	}
	var raw struct {
		Hourly struct {
			Time     []string  `json:"time"`
			Temp     []float64 `json:"temperature_2m"`
			Humidity []float64 `json:"relative_humidity_2m"`
			Pressure []float64 `json:"surface_pressure"`
		} `json:"hourly"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return out, err
	}
	for i, ts := range raw.Hourly.Time {
		t, err := time.Parse("2006-01-02T15:04", ts)
		if err != nil {
			continue
		}
		if i < len(raw.Hourly.Temp) && !math.IsNaN(raw.Hourly.Temp[i]) {
			out.Temp = append(out.Temp, domain.SeriesPoint{Time: t, Value: raw.Hourly.Temp[i]})
		}
		if i < len(raw.Hourly.Humidity) && !math.IsNaN(raw.Hourly.Humidity[i]) {
			out.Humidity = append(out.Humidity, domain.SeriesPoint{Time: t, Value: raw.Hourly.Humidity[i]})
		}
		if i < len(raw.Hourly.Pressure) && !math.IsNaN(raw.Hourly.Pressure[i]) {
			out.Pressure = append(out.Pressure, domain.SeriesPoint{Time: t, Value: raw.Hourly.Pressure[i]})
		}
	}
	return out, nil
}

// resolveEnvLocation returns the location an environment sits at, inheriting
// from its air-source room when the environment itself has none — mirroring the
// web app's resolveLocationId so weather follows the same rule everywhere.
func (s *Server) resolveEnvLocation(envID string) string {
	envs, err := s.store.Environments()
	if err != nil {
		return ""
	}
	var self *domain.Environment
	for i := range envs {
		if envs[i].ID == envID {
			self = &envs[i]
			break
		}
	}
	if self == nil {
		return ""
	}
	if self.LocationID != "" {
		return self.LocationID
	}
	if self.AirSourceID != "" {
		for i := range envs {
			if envs[i].ID == self.AirSourceID && envs[i].LocationID != "" {
				return envs[i].LocationID
			}
		}
	}
	return ""
}

// getWeatherHistory returns the persisted outdoor history for an environment's
// resolved location, for overlaying on the metric-detail modal.
func (s *Server) getWeatherHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hours := 72.0
	if n, e := strconv.ParseFloat(q.Get("hours"), 64); e == nil && n > 0 && n <= 24*90 {
		hours = n
	}
	buckets := 500
	if n, e := strconv.Atoi(q.Get("buckets")); e == nil && n > 0 && n <= 2000 {
		buckets = n
	}
	locID := s.resolveEnvLocation(r.PathValue("id"))
	if locID == "" {
		writeJSON(w, http.StatusOK, domain.WeatherHistory{})
		return
	}
	since := time.Now().Add(-time.Duration(hours * float64(time.Hour)))
	hist, err := s.store.WeatherReadingsSince(locID, since, buckets)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, hist)
}

// PollWeather periodically records outdoor observations for every sited
// location, so the metric-detail modal can compare indoor readings against
// outdoor history over long windows. Open-Meteo's past-days window backfills
// history on the first poll. Runs until ctx is cancelled.
func (s *Server) PollWeather(ctx context.Context) {
	const interval = 20 * time.Minute
	poll := func() {
		locs, err := s.store.Locations()
		if err != nil {
			log.Printf("weather poll: locations: %v", err)
			return
		}
		for _, loc := range locs {
			wx, err := s.fetchWeather(ctx, loc.Lat, loc.Lon, 7)
			if err != nil {
				log.Printf("weather poll: fetch %s: %v", loc.Name, err)
				continue
			}
			samples := weatherSamples(loc.ID, wx)
			if err := s.store.SaveWeatherReadings(samples); err != nil {
				log.Printf("weather poll: save %s: %v", loc.Name, err)
			}
		}
	}
	poll()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			poll()
		}
	}
}

// weatherSamples aligns the parsed temp/humidity/pressure series (which share
// hourly timestamps) into per-timestamp samples for storage.
func weatherSamples(locationID string, wx weatherResponse) []domain.WeatherSample {
	byTs := map[int64]*domain.WeatherSample{}
	var order []int64
	get := func(t time.Time) *domain.WeatherSample {
		ms := t.UnixMilli()
		s, ok := byTs[ms]
		if !ok {
			s = &domain.WeatherSample{LocationID: locationID, Time: t}
			byTs[ms] = s
			order = append(order, ms)
		}
		return s
	}
	for _, p := range wx.Temp {
		get(p.Time).Temp = p.Value
	}
	for _, p := range wx.Humidity {
		get(p.Time).Humidity = p.Value
	}
	for _, p := range wx.Pressure {
		get(p.Time).Pressure = p.Value
	}
	out := make([]domain.WeatherSample, 0, len(order))
	for _, ms := range order {
		out = append(out, *byTs[ms])
	}
	return out
}

// fetch performs an outbound GET with the required User-Agent, bounded by the
// given context.
func (s *Server) fetch(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	return body, nil
}

// --- tiny TTL cache for outbound lookups ---

type cacheEntry struct {
	data []byte
	exp  time.Time
}

type ttlCache struct {
	mu sync.Mutex
	m  map[string]cacheEntry
}

func (c *ttlCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.exp) {
		return nil, false
	}
	return e.data, true
}

func (c *ttlCache) set(key string, data []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = cacheEntry{data: data, exp: time.Now().Add(ttl)}
}

var extCache = &ttlCache{m: map[string]cacheEntry{}}
