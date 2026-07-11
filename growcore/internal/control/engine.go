package control

import (
	"context"
	"fmt"
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

	mu     sync.RWMutex
	latest domain.Snapshot
	onSnap func(domain.Snapshot)
	// History is sampled on a wall-clock cadence (persistEvery) rather than every
	// control tick, so the graphs get a steady point rate independent of how fast
	// the control loop runs.
	persistEvery    time.Duration
	lastPersist     time.Time
	fanCommands     map[string]int
	lightCommands   map[string]bool
	sensorStates    map[string]bool
	emergencyStates map[string]bool
	issueStates     map[string]bool
	lastHealth      domain.ControllerHealth

	// schedMu guards lightOverrides, which a manual switch (HTTP goroutine) may
	// write while the control loop reads it.
	schedMu        sync.Mutex
	lightOverrides map[string]time.Time // envID -> hold the manual light state until this instant
}

func New(st *store.Store, adapter Adapter, onSnapshot func(domain.Snapshot)) *Engine {
	return &Engine{store: st, adapter: adapter, onSnap: onSnapshot, persistEvery: time.Minute,
		fanCommands: map[string]int{}, lightCommands: map[string]bool{}, sensorStates: map[string]bool{},
		emergencyStates: map[string]bool{}, issueStates: map[string]bool{}, lightOverrides: map[string]time.Time{}}
}

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

func (e *Engine) Latest() domain.Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.latest
}

// climate is an environment's aggregated sensor state for one tick.
type climate struct {
	tempC, humidity, co2    float64
	hasTemp, hasHum, hasCO2 bool
	vpd                     float64
}

func (c climate) hasClimate() bool { return c.hasTemp && c.hasHum }

// step runs one reconciliation cycle.
func (e *Engine) step(dt time.Duration) error {
	e.adapter.Tick(dt)

	envs, err := e.store.Environments()
	if err != nil {
		return err
	}
	bindings, err := e.store.Bindings()
	if err != nil {
		return err
	}
	byEnv := map[string][]domain.Binding{}
	powerEntityByDevice := map[string]string{}
	// Power-meter entity per device: a plug's actual wattage reading, distinct
	// from the switch entity, used to report real light draw instead of rated.
	powerMeterByDevice := map[string]string{}
	controllerChannels := map[string]domain.Binding{}
	for _, b := range bindings {
		byEnv[b.EnvironmentID] = append(byEnv[b.EnvironmentID], b)
		if b.Kind == domain.KindPower {
			powerEntityByDevice[b.DeviceID] = b.Entity
		}
		if b.Kind == domain.KindSensor && b.Measurement == domain.MeasurePower {
			powerMeterByDevice[b.DeviceID] = b.Entity
		}
		if b.Kind == domain.KindController {
			controllerChannels[b.ID] = b
		}
	}
	cycles, err := e.store.Cycles()
	if err != nil {
		return err
	}
	cycleByEnv := map[string]domain.Cycle{}
	for _, c := range cycles {
		cycleByEnv[c.EnvironmentID] = c
	}
	schedules, err := e.store.LightSchedules()
	if err != nil {
		return err
	}
	scheduleByEnv := map[string]domain.LightSchedule{}
	for _, sc := range schedules {
		scheduleByEnv[sc.EnvironmentID] = sc
	}

	// Pass 1: aggregate sensor climate per environment (needed before tents can
	// reference their lung room's readings).
	climates := map[string]climate{}
	sensorsByEnv := map[string][]domain.SensorReading{}
	for _, env := range envs {
		c, sensors := e.readSensors(byEnv[env.ID])
		if c.hasTemp && c.hasHum {
			c.vpd = round2(domain.LeafVPD(c.tempC, c.humidity, env.LeafTempOffsetC))
		}
		climates[env.ID] = c
		sensorsByEnv[env.ID] = sensors
	}

	// Pass 2: drive controls and assemble the live views.
	now := time.Now()
	// Persist history at most once per persistEvery window (zero lastPersist on
	// the first step forces an immediate sample).
	persistTick := now.Sub(e.lastPersist) >= e.persistEvery
	health := e.adapter.Health()
	views := make([]domain.EnvironmentView, 0, len(envs))
	for _, env := range envs {
		c := climates[env.ID]
		if e.lastHealth != health {
			level, eventType := "info", "notice"
			if health != domain.HealthOnline {
				level, eventType = "warning", "warning"
			}
			e.activity(env.ID, "", level, eventType, "Home Assistant connection is "+string(health))
		}
		for _, sensor := range sensorsByEnv[env.ID] {
			previous, seen := e.sensorStates[sensor.ID]
			if (!seen && !sensor.OK) || (seen && previous != sensor.OK) {
				if sensor.OK {
					e.activity(env.ID, sensor.ID, "info", "notice", sensor.Name+" is reporting again")
				} else {
					e.activity(env.ID, sensor.ID, "warning", "warning", sensor.Name+" is unavailable")
				}
			}
			e.sensorStates[sensor.ID] = sensor.OK
		}
		emergency := c.hasTemp && env.EmergencyTempC > 0 && c.tempC >= env.EmergencyTempC
		if previous := e.emergencyStates[env.ID]; emergency != previous {
			if emergency {
				e.activity(env.ID, "", "warning", "warning", "Emergency temperature reached")
			} else if previous {
				e.activity(env.ID, "", "info", "notice", "Temperature returned below the emergency limit")
			}
			e.emergencyStates[env.ID] = emergency
		}
		hasFans := false
		for _, binding := range byEnv[env.ID] {
			if binding.Kind == domain.KindFan {
				hasFans = true
				break
			}
		}
		e.issue(env.ID+":fan-climate", hasFans && !c.hasTemp, env.ID, "", "Fan control is paused because no temperature reading is available", "Temperature reading restored; fan control resumed")
		view := domain.EnvironmentView{
			Environment: env,
			Health:      health,
			HasClimate:  c.hasClimate(),
			HasTemp:     c.hasTemp,
			HasHum:      c.hasHum,
			TempC:       round1(c.tempC),
			Humidity:    round1(c.humidity),
			CO2:         round0(c.co2),
			HasCO2:      c.hasCO2,
			VPD:         c.vpd,
			Sensors:     sensorsByEnv[env.ID],
		}

		exhaust := 0
		var devSamples []domain.DeviceReading
		for _, b := range byEnv[env.ID] {
			switch b.Kind {
			case domain.KindFan:
				channel := controllerChannels[b.ControllerChannelID]
				if channel.Entity == "" {
					channel = b
				}
				displayName := b.DeviceName
				if displayName == "" {
					displayName = b.Name
				}
				cs := domain.ControlState{ID: b.ID, Name: displayName, Kind: domain.KindFan, Role: b.Role, Entity: channel.Entity, MaxRPM: b.MaxRPM}
				if c.hasTemp {
					speed := ChannelSpeed(b.Role, env, c.tempC)
					cs.DesiredSpeed = speed
					err := e.adapter.SetFan(channel.Entity, speed)
					if err != nil {
						log.Printf("control: set fan %s: %v", channel.Entity, err)
					}
					if previous, seen := e.fanCommands[channel.Entity]; !seen || previous != speed {
						if err != nil {
							e.activity(env.ID, b.DeviceID, "error", "control", "Failed to set "+b.Name+" speed")
						} else {
							e.activity(env.ID, b.DeviceID, "info", "control", fmt.Sprintf("Set %s to %d%% via %s", b.Name, speed, channel.Name))
						}
						e.fanCommands[channel.Entity] = speed
					}
					if (b.Role == domain.RoleExhaust || b.Role == domain.RoleIntake) && speed > exhaust {
						exhaust = speed
					}
				}
				if rpm, ok := e.adapter.Value(channel.RPMEntity); ok {
					cs.RPM = int(rpm)
				}
				if persistTick {
					devSamples = append(devSamples, domain.DeviceReading{
						BindingID: b.ID, EnvironmentID: env.ID, Time: now, Metric: "rpm", Value: float64(cs.RPM),
					})
					devSamples = append(devSamples, domain.DeviceReading{
						BindingID: b.ID, EnvironmentID: env.ID, Time: now, Metric: "speed", Value: float64(cs.DesiredSpeed),
					})
				}
				view.Controls = append(view.Controls, cs)
			case domain.KindLight:
				entity := powerEntityByDevice[b.PowerControllerID]
				e.issue(env.ID+":light:"+b.ID, entity == "", env.ID, b.DeviceID, b.Name+" has no power controller assigned", b.Name+" power controller is connected")
				displayName := b.DeviceName
				if displayName == "" {
					displayName = b.Name
				}
				cs := domain.ControlState{
					ID: b.ID, Name: displayName, Kind: domain.KindLight, Entity: entity,
					Wattage: b.Wattage, Primary: b.Primary,
				}
				on, known := e.adapter.SwitchState(entity)
				// The photoperiod schedule drives the box's primary light only.
				sched := scheduleByEnv[env.ID]
				if b.Primary && entity != "" && sched.Mode != domain.LightScheduleOff && sched.Mode != "" {
					phase := schedulePhase(cycleByEnv, env.ID)
					if desired, ok := sched.DesiredOn(phase, now); ok && !e.overrideActive(env.ID, now) {
						changed := e.driveLight(env, b, entity, desired, on, known)
						if changed || !known {
							on, known = desired, true
						}
					}
				}
				if known {
					cs.On = on
				}
				// Prefer the plug's measured wattage; fall back to rated power while
				// the light is on when no meter is bound.
				power, measured := 0.0, false
				if meter := powerMeterByDevice[b.PowerControllerID]; meter != "" {
					if v, ok := e.adapter.Value(meter); ok {
						power, measured = v, true
					}
				}
				if !measured && cs.On {
					power = b.Wattage
				}
				cs.Power = power
				if persistTick && entity != "" {
					devSamples = append(devSamples, domain.DeviceReading{
						BindingID: b.ID, EnvironmentID: env.ID, Time: now, Metric: "power", Value: power,
					})
				}
				view.Controls = append(view.Controls, cs)
			case domain.KindCamera:
				view.Cameras = append(view.Cameras, domain.CameraRef{ID: b.ID, Name: b.Name, Entity: b.Entity})
			}
		}

		if c, ok := cycleByEnv[env.ID]; ok {
			cycle := c
			view.Cycle = &cycle
		}

		if sc, ok := scheduleByEnv[env.ID]; ok {
			sched := sc
			view.Schedule = &sched
		}

		if env.Kind == domain.KindTent && env.AirSourceID != "" {
			if src := findEnv(envs, env.AirSourceID); src != nil {
				sc := climates[src.ID]
				view.AirSource = &domain.AirSourceView{
					ID: src.ID, Name: src.Name,
					TempC: round1(sc.tempC), Humidity: round1(sc.humidity),
					VPD: sc.vpd, OK: sc.hasClimate(),
				}
			}
		}

		views = append(views, view)

		if persistTick && len(devSamples) > 0 {
			if err := e.store.InsertDeviceReadings(devSamples); err != nil {
				log.Printf("control: insert device readings: %v", err)
			}
		}
		if persistTick {
			var sensSamples []domain.SensorSample
			for _, sr := range view.Sensors {
				if !sr.OK || sr.Measurement == "" {
					continue
				}
				sensSamples = append(sensSamples, domain.SensorSample{
					BindingID: sr.ID, EnvironmentID: env.ID, Time: now,
					Measurement: sr.Measurement, Value: sr.Value,
				})
			}
			if len(sensSamples) > 0 {
				if err := e.store.InsertSensorReadings(sensSamples); err != nil {
					log.Printf("control: insert sensor readings: %v", err)
				}
			}
		}
		if persistTick && c.hasClimate() {
			e.store.InsertReading(domain.Reading{
				EnvironmentID: env.ID, Time: now,
				TempC: round1(c.tempC), Humidity: round1(c.humidity),
				CO2: round0(c.co2), VPD: c.vpd, ExhaustSpeed: exhaust,
			})
		}
	}
	e.lastHealth = health
	if persistTick {
		e.lastPersist = now
	}

	snap := domain.Snapshot{Time: now, Environments: views}
	e.mu.Lock()
	e.latest = snap
	e.mu.Unlock()
	if e.onSnap != nil {
		e.onSnap(snap)
	}
	return nil
}

func (e *Engine) activity(envID, deviceID, level, eventType, message string) {
	if err := e.store.AddActivity(domain.Activity{EnvironmentID: envID, DeviceID: deviceID, Level: level, Type: eventType, Message: message}); err != nil {
		log.Printf("activity: %v", err)
	}
}

func (e *Engine) issue(key string, active bool, envID, deviceID, warning, resolved string) {
	previous, seen := e.issueStates[key]
	if active && (!seen || !previous) {
		e.activity(envID, deviceID, "warning", "warning", warning)
	}
	if !active && seen && previous {
		e.activity(envID, deviceID, "info", "notice", resolved)
	}
	e.issueStates[key] = active
}

// driveLight reconciles a scheduled light to the desired on/off state. It
// commands the switch when the desired value changes, when the actual state is
// known to differ, or on the first tick, and logs each commanded transition.
// It reports whether a command was issued this tick.
func (e *Engine) driveLight(env domain.Environment, b domain.Binding, entity string, desired, actual, known bool) bool {
	last, seen := e.lightCommands[entity]
	if seen && last == desired && !(known && actual != desired) {
		return false
	}
	err := e.adapter.SetSwitch(entity, desired)
	if err != nil {
		log.Printf("control: set light %s: %v", entity, err)
	}
	if !seen || last != desired {
		state := "off"
		if desired {
			state = "on"
		}
		if err != nil {
			e.activity(env.ID, b.DeviceID, "error", "control", "Failed to switch "+b.Name+" "+state)
		} else {
			e.activity(env.ID, b.DeviceID, "info", "control", "Switched "+b.Name+" "+state+" on schedule")
		}
	}
	e.lightCommands[entity] = desired
	return err == nil
}

// overrideActive reports whether a manual light override is holding for env.
// Expired overrides are cleared.
func (e *Engine) overrideActive(envID string, now time.Time) bool {
	e.schedMu.Lock()
	defer e.schedMu.Unlock()
	until, ok := e.lightOverrides[envID]
	if !ok {
		return false
	}
	if now.Before(until) {
		return true
	}
	delete(e.lightOverrides, envID)
	return false
}

// NoteManualLightSwitch records that the primary light of an environment was
// switched by hand, so the schedule holds that state until its next scheduled
// transition (a "hold until next period"). When the hold ends the control loop
// reconciles the light back to the schedule.
func (e *Engine) NoteManualLightSwitch(envID string) {
	sched, _, err := e.store.LightSchedule(envID)
	if err != nil || sched.Mode == domain.LightScheduleOff {
		return
	}
	cycles, _ := e.store.Cycles()
	phase := domain.PhaseVegetative
	for _, c := range cycles {
		if c.EnvironmentID == envID {
			phase = c.Phase
			break
		}
	}
	now := time.Now()
	until := sched.NextTransition(phase, now)
	if until.IsZero() {
		// Always-on / always-off schedule has no boundary; hold for a day.
		until = now.Add(24 * time.Hour)
	}
	e.schedMu.Lock()
	e.lightOverrides[envID] = until
	e.schedMu.Unlock()
}

// schedulePhase resolves the phase used by a light schedule: the environment's
// cycle phase, or vegetative when no cycle is active.
func schedulePhase(cycles map[string]domain.Cycle, envID string) domain.Phase {
	if c, ok := cycles[envID]; ok && c.Phase != "" {
		return c.Phase
	}
	return domain.PhaseVegetative
}

// readSensors reads every sensor binding and aggregates temperature, humidity
// and CO2 (averaging when several sensors of the same kind are present).
func (e *Engine) readSensors(bindings []domain.Binding) (climate, []domain.SensorReading) {
	var c climate
	var tempSum, humSum, co2Sum float64
	var tempN, humN, co2N int
	var readings []domain.SensorReading

	for _, b := range bindings {
		if b.Kind != domain.KindSensor {
			continue
		}
		v, ok := e.adapter.Value(b.Entity)
		readings = append(readings, domain.SensorReading{
			ID: b.ID, Name: b.Name, Measurement: b.Measurement, Entity: b.Entity,
			Value: round1(v), OK: ok,
		})
		if !ok {
			continue
		}
		switch b.Measurement {
		case domain.MeasureTemperature:
			tempSum += v
			tempN++
		case domain.MeasureHumidity:
			humSum += v
			humN++
		case domain.MeasureCO2:
			co2Sum += v
			co2N++
		}
	}
	if tempN > 0 {
		c.tempC, c.hasTemp = tempSum/float64(tempN), true
	}
	if humN > 0 {
		c.humidity, c.hasHum = humSum/float64(humN), true
	}
	if co2N > 0 {
		c.co2, c.hasCO2 = co2Sum/float64(co2N), true
	}
	return c, readings
}

func findEnv(envs []domain.Environment, id string) *domain.Environment {
	for i := range envs {
		if envs[i].ID == id {
			return &envs[i]
		}
	}
	return nil
}

func round0(v float64) float64 { return float64(int(v + 0.5)) }
func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }
