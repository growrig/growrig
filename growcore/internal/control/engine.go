package control

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/store"
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

	// startedAt marks when the control loop began. For settleWindow after that,
	// engine-generated activities are suppressed (see activity): the state maps
	// above still update, so the engine re-establishes its baseline silently
	// instead of logging a restart as a burst of "unavailable → reporting again"
	// churn. Genuine lifecycle events ("Grow Core started/stopped") are logged
	// separately by main and bypass this window.
	startedAt time.Time

	// schedMu guards the schedule timezone and manual light overrides, which
	// HTTP goroutines may update while the control loop reads them.
	schedMu          sync.Mutex
	lightOverrides   map[string]time.Time // envID -> hold the manual light state until this instant
	scheduleLocation *time.Location
}

// SetScheduleLocation applies the instance timezone to photoperiods and manual
// override boundaries, independently of the host's timezone.
func (e *Engine) SetScheduleLocation(loc *time.Location) {
	e.schedMu.Lock()
	e.scheduleLocation = loc
	e.schedMu.Unlock()
}

func (e *Engine) scheduleTime(now time.Time) time.Time {
	e.schedMu.Lock()
	loc := e.scheduleLocation
	e.schedMu.Unlock()
	if loc == nil {
		loc = time.UTC
	}
	return now.In(loc)
}

func New(st *store.Store, adapter Adapter, onSnapshot func(domain.Snapshot)) *Engine {
	return &Engine{store: st, adapter: adapter, onSnap: onSnapshot, persistEvery: time.Minute,
		fanCommands: map[string]int{}, lightCommands: map[string]bool{}, sensorStates: map[string]bool{},
		emergencyStates: map[string]bool{}, issueStates: map[string]bool{}, lightOverrides: map[string]time.Time{}}
}

// settleWindow is the grace period after start during which transient
// state-change activities are seeded silently rather than logged, so a normal
// restart or quit doesn't spew availability churn into the activity log. It
// must comfortably outlast adapter/Home Assistant reconnection after a restart.
const settleWindow = 30 * time.Second

// settling reports whether the engine is still within its post-start settle
// window. During it, activity() drops engine-generated events.
func (e *Engine) settling() bool {
	return time.Since(e.startedAt) < settleWindow
}

func (e *Engine) Run(ctx context.Context, interval time.Duration) {
	e.startedAt = time.Now()
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
	grows, err := e.store.Grows()
	if err != nil {
		return err
	}
	units, err := e.store.PlantUnitsAll()
	if err != nil {
		return err
	}
	placements, err := e.store.CurrentPlacements()
	if err != nil {
		return err
	}
	growByID := map[string]domain.Grow{}
	for _, g := range grows {
		growByID[g.ID] = g
	}
	envName := map[string]string{}
	for _, env := range envs {
		envName[env.ID] = env.Name
	}
	// Per-grow aggregates: active plant count and the distinct environments each
	// grow currently occupies (via its units' open placements).
	unitByID := map[string]domain.PlantUnit{}
	plantCountByGrow := map[string]int{}
	// Per-grow breakdown of active plants by cultivar, in first-seen order, for
	// the plant thumbnails on the grow card.
	cultByGrow := map[string][]domain.GrowCultivarRef{}
	cultIndex := map[string]map[string]int{}
	for _, u := range units {
		unitByID[u.ID] = u
		if u.Status != domain.PlantActive {
			continue
		}
		plantCountByGrow[u.GrowID] += u.Quantity
		if cultIndex[u.GrowID] == nil {
			cultIndex[u.GrowID] = map[string]int{}
		}
		if idx, ok := cultIndex[u.GrowID][u.Cultivar]; ok {
			cultByGrow[u.GrowID][idx].Count += u.Quantity
		} else {
			cultIndex[u.GrowID][u.Cultivar] = len(cultByGrow[u.GrowID])
			cultByGrow[u.GrowID] = append(cultByGrow[u.GrowID], domain.GrowCultivarRef{Cultivar: u.Cultivar, Count: u.Quantity})
		}
	}
	growEnvs := map[string][]domain.GrowEnvRef{}
	growEnvSeen := map[string]map[string]bool{}
	for _, p := range placements {
		u, ok := unitByID[p.PlantUnitID]
		if !ok {
			continue
		}
		if growEnvSeen[u.GrowID] == nil {
			growEnvSeen[u.GrowID] = map[string]bool{}
		}
		if growEnvSeen[u.GrowID][p.EnvironmentID] {
			continue
		}
		growEnvSeen[u.GrowID][p.EnvironmentID] = true
		growEnvs[u.GrowID] = append(growEnvs[u.GrowID], domain.GrowEnvRef{ID: p.EnvironmentID, Name: envName[p.EnvironmentID]})
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
	now := e.scheduleTime(time.Now())
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
				connected := channel.Entity != ""
				e.issue(env.ID+":fan:"+b.ID, !connected, env.ID, b.DeviceID, displayName+" has no controller channel assigned", displayName+" controller channel is connected")
				if c.hasTemp && connected {
					speed := DesiredChannelSpeed(b.Role, env, c.tempC)
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
					stage := controlStage(growByID, unitByID, placements, env)
					if desired, ok := sched.DesiredOn(stage, now); ok && !e.overrideActive(env.ID, now) {
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
				view.Cameras = append(view.Cameras, domain.CameraRef{ID: b.ID, Name: b.Name, Entity: b.Entity, StreamURL: b.StreamURL, CameraType: b.CameraType, CameraCaptureInterval: b.CameraCaptureInterval})
			case domain.KindIrrigation:
				// Passive irrigation has no live telemetry; surface its static
				// description so Equipment and Operating now can render it.
				view.Irrigation = append(view.Irrigation, domain.IrrigationRef{ID: b.ID, Name: b.Name, Type: b.IrrigationType, Mode: b.IrrigationMode, ReservoirL: b.ReservoirL, ValveCount: b.ValveCount})
			}
		}

		if g, ok := domain.ResolveControlGrow(env, growByID, unitByID, placements); ok {
			view.Grow = &domain.GrowSummary{
				ID: g.ID, Name: g.Name, Species: g.Species,
				Stage:      g.Stage,
				StageDays:  domain.DaysSince(g.StageStarted, now),
				TotalDays:  domain.DaysSince(g.StartedAt, now),
				PlantCount: plantCountByGrow[g.ID],
			}
		}

		sched, hasSched := scheduleByEnv[env.ID]
		if hasSched {
			s := sched
			view.Schedule = &s
		}
		// Expose the resolved per-category control policy so clients render the
		// effective auto/manual modes (lighting derives from the schedule).
		view.Control = env.Control.Resolve(hasSched && sched.Mode != domain.LightScheduleOff && sched.Mode != "")

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

	growViews := make([]domain.GrowView, 0, len(grows))
	for _, g := range grows {
		envs := growEnvs[g.ID]
		if envs == nil {
			envs = []domain.GrowEnvRef{}
		}
		cults := cultByGrow[g.ID]
		if cults == nil {
			cults = []domain.GrowCultivarRef{}
		}
		growViews = append(growViews, domain.GrowView{
			Grow:         g,
			StageDays:    domain.DaysSince(g.StageStarted, now),
			TotalDays:    domain.DaysSince(g.StartedAt, now),
			PlantCount:   plantCountByGrow[g.ID],
			Environments: envs,
			Cultivars:    cults,
		})
	}
	snap := domain.Snapshot{Time: now, Environments: views, Grows: growViews}
	e.mu.Lock()
	e.latest = snap
	e.mu.Unlock()
	if e.onSnap != nil {
		e.onSnap(snap)
	}
	return nil
}

func (e *Engine) activity(envID, deviceID, level, eventType, message string) {
	// Suppress transient churn while settling after start (see startedAt). The
	// callers still update their state maps, so only genuine post-settle changes
	// are logged.
	if e.settling() {
		return
	}
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
	// Mirror the in-memory issue state into the persistent alerts table so the
	// "needs attention" projection survives restarts. activity_log above keeps
	// the historical record; alerts hold the current open/resolved condition.
	if e.settling() {
		return
	}
	if active {
		if err := e.store.OpenAlert(domain.Alert{
			Key: key, EnvironmentID: envID, DeviceID: deviceID,
			Severity: domain.AlertWarning, Kind: alertKind(key),
			Title: warning, Message: warning,
		}); err != nil {
			log.Printf("alert open: %v", err)
		}
	} else {
		if err := e.store.ResolveAlert(key); err != nil {
			log.Printf("alert resolve: %v", err)
		}
	}
}

// alertKind derives a coarse alert kind from an issue key of the form
// "<envID>:<kind>[:<detail>]" — e.g. "env-1:fan:b-2" → "fan".
func alertKind(key string) string {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) >= 2 {
		return parts[1]
	}
	return "system"
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
	stage := e.currentControlStage(envID)
	now := e.scheduleTime(time.Now())
	until := sched.NextTransition(stage, now)
	if until.IsZero() {
		// Always-on / always-off schedule has no boundary; hold for a day.
		until = now.Add(24 * time.Hour)
	}
	e.schedMu.Lock()
	e.lightOverrides[envID] = until
	e.schedMu.Unlock()
}

// currentControlStage loads what controlStage needs from the store, for
// callers outside the control loop. Any load failure yields "".
func (e *Engine) currentControlStage(envID string) string {
	envs, err := e.store.Environments()
	if err != nil {
		return ""
	}
	env := findEnv(envs, envID)
	if env == nil {
		return ""
	}
	grows, err := e.store.Grows()
	if err != nil {
		return ""
	}
	units, err := e.store.PlantUnitsAll()
	if err != nil {
		return ""
	}
	placements, err := e.store.CurrentPlacements()
	if err != nil {
		return ""
	}
	growByID := map[string]domain.Grow{}
	for _, g := range grows {
		growByID[g.ID] = g
	}
	unitByID := map[string]domain.PlantUnit{}
	for _, u := range units {
		unitByID[u.ID] = u
	}
	return controlStage(growByID, unitByID, placements, *env)
}

// controlStage resolves the stage that drives an environment's light schedule:
// the current stage of its control grow (nominated, or the only grow growing
// there), or "" when there is none (in which case the schedule falls back to
// default photoperiod hours).
func controlStage(grows map[string]domain.Grow, units map[string]domain.PlantUnit, placements []domain.PlantPlacement, env domain.Environment) string {
	if g, ok := domain.ResolveControlGrow(env, grows, units, placements); ok {
		return g.Stage
	}
	return ""
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
