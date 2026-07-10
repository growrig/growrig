// Package sim is a simulator adapter for Grow Core.
//
// It emulates a Grow Controller with a temperature/humidity sensor and PWM fan
// channels, running a small physical model so the whole platform can be
// exercised end-to-end without hardware. It implements control.Adapter and
// stands in for the Home Assistant adapter.
package sim

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// DeviceID is the stable id of the simulated controller that Grow Core
// auto-provisions when the simulator adapter is selected.
const DeviceID = "sim-controller-1"

const (
	ambientTempC   = 22.0 // air pulled in by the exhaust
	ambientHumid   = 48.0
	heatInputC     = 1.6 // °C/min added by lamp + gear at rest
	moistureInput  = 3.0 // %RH/min plant transpiration
	baseCoolRate   = 0.04
	exhaustCooling = 0.9
)

type fan struct {
	speed int // commanded 0-100
	rpm   int // measured
}

// Simulator holds the physical state of one virtual controller. Fan channels
// are created lazily the first time they are commanded, so the simulator
// tracks whatever channels the (database-owned) sim device defines.
type Simulator struct {
	mu       sync.Mutex
	tempC    float64
	humidity float64
	fans     map[string]*fan
	rng      *rand.Rand
}

func New() *Simulator {
	return &Simulator{
		tempC:    26.5,
		humidity: 60,
		fans:     map[string]*fan{},
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *Simulator) Start(context.Context) error { return nil }
func (s *Simulator) Close() error                { return nil }

func (s *Simulator) Health(domain.Device) domain.ControllerHealth { return domain.HealthOnline }

// Tick advances the physical model by dt using the currently commanded speeds.
func (s *Simulator) Tick(dt time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	minutes := dt.Minutes()
	ex := float64(s.dominantSpeed()) / 100.0

	// Newtonian-ish cooling toward ambient, scaled by airflow.
	coolRate := baseCoolRate + exhaustCooling*ex
	dTemp := heatInputC*minutes - coolRate*(s.tempC-ambientTempC)*minutes
	s.tempC += dTemp + s.noise(0.05)
	s.tempC = clampF(s.tempC, ambientTempC-2, 45)

	// Humidity rises from transpiration; airflow flushes it toward ambient.
	dHum := moistureInput*minutes - (0.05+ex)*(s.humidity-ambientHumid)*minutes
	s.humidity += dHum + s.noise(0.2)
	s.humidity = clampF(s.humidity, 20, 95)

	// Tachometers track commanded speed with jitter; fans below a stall
	// threshold report zero RPM.
	for _, f := range s.fans {
		if f.speed < 8 {
			f.rpm = 0
			continue
		}
		f.rpm = clampInt(int(float64(f.speed)*38+s.noise(60)), 0, 4200)
	}
}

func (s *Simulator) Climate(domain.Device) (float64, float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tempC, s.humidity, true
}

func (s *Simulator) SetSpeed(_ domain.Device, ch domain.Channel, speed int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fans[ch.ID]
	if f == nil {
		f = &fan{}
		s.fans[ch.ID] = f
	}
	f.speed = clampInt(speed, 0, 100)
	return nil
}

func (s *Simulator) FanRPM(_ domain.Device, ch domain.Channel) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.fans[ch.ID]; ok {
		return f.rpm, true
	}
	return 0, false
}

// dominantSpeed is the highest commanded fan speed, used as the effective
// airflow that drives cooling. Callers must hold s.mu.
func (s *Simulator) dominantSpeed() int {
	max := 0
	for _, f := range s.fans {
		if f.speed > max {
			max = f.speed
		}
	}
	return max
}

func (s *Simulator) noise(mag float64) float64 { return (s.rng.Float64()*2 - 1) * mag }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
