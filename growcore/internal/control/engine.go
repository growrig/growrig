package control

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
	"github.com/growrig/growrig-platform/growcore/internal/store"
)

// Engine ties storage, an adapter and the control law together into a periodic
// reconciliation loop, and publishes a live snapshot each tick.
type Engine struct {
	store   *store.Store
	adapter Adapter

	mu      sync.RWMutex
	latest  domain.Snapshot
	onSnap  func(domain.Snapshot)
	persist int // ticks between history writes
	tick    int
}

func New(st *store.Store, adapter Adapter, onSnapshot func(domain.Snapshot)) *Engine {
	return &Engine{store: st, adapter: adapter, onSnap: onSnapshot, persist: 5}
}

// Run reconciles every interval until ctx is cancelled.
func (e *Engine) Run(ctx context.Context, interval time.Duration) {
	if err := e.step(interval); err != nil {
		log.Printf("control: initial step: %v", err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := e.step(interval); err != nil {
				log.Printf("control: step: %v", err)
			}
		}
	}
}

// Latest returns the most recent snapshot (for the REST bootstrap endpoint).
func (e *Engine) Latest() domain.Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.latest
}

// step runs one reconciliation cycle: advance the adapter, read climate,
// compute desired speeds, command the adapter, persist history and publish.
func (e *Engine) step(dt time.Duration) error {
	e.adapter.Tick(dt)

	envs, err := e.store.Environments()
	if err != nil {
		return err
	}
	devs, err := e.store.Devices()
	if err != nil {
		return err
	}
	envByID := map[string]domain.Environment{}
	for _, en := range envs {
		envByID[en.ID] = en
	}

	now := time.Now()
	// Per-environment climate + exhaust for the history record.
	type sample struct {
		tempC, humidity float64
		exhaust         int
		ok              bool
	}
	samples := map[string]*sample{}

	for di := range devs {
		d := &devs[di]
		env := envByID[d.EnvironmentID]
		d.Health = e.adapter.Health(*d)
		d.LastSeen = now

		tempC, humidity, ok := e.adapter.Climate(*d)
		if ok {
			d.TempC = round1(tempC)
			d.Humidity = round1(humidity)
		}

		s := samples[d.EnvironmentID]
		if s == nil {
			s = &sample{}
			samples[d.EnvironmentID] = s
		}

		for ci := range d.Channels {
			c := &d.Channels[ci]
			if ok {
				speed := ChannelSpeed(c.Role, env, tempC)
				c.DesiredSpeed = speed
				if err := e.adapter.SetSpeed(*d, *c, speed); err != nil {
					log.Printf("control: set %s/%s: %v", d.ID, c.ID, err)
				}
				if c.Role == domain.RoleExhaust || c.Role == domain.RoleIntake {
					if speed > s.exhaust {
						s.exhaust = speed
					}
				}
			}
			if rpm, rok := e.adapter.FanRPM(*d, *c); rok {
				c.RPM = rpm
			}
		}
		if ok && !s.ok {
			s.tempC, s.humidity, s.ok = round1(tempC), round1(humidity), true
		}
	}

	e.tick++
	if e.tick%e.persist == 0 {
		for envID, s := range samples {
			if !s.ok {
				continue
			}
			e.store.InsertReading(domain.Reading{
				EnvironmentID: envID, Time: now,
				TempC: s.tempC, Humidity: s.humidity, ExhaustSpeed: s.exhaust,
			})
		}
	}

	snap := domain.Snapshot{Time: now, Environments: envs, Devices: devs}
	e.mu.Lock()
	e.latest = snap
	e.mu.Unlock()
	if e.onSnap != nil {
		e.onSnap(snap)
	}
	return nil
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
