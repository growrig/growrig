// Mirrors the JSON emitted by Grow Core (growcore/internal/domain).

export type Role = 'unassigned' | 'exhaust' | 'intake' | 'circulation';
export type EnvironmentKind = 'tent' | 'room';
export type BindingKind = 'sensor' | 'fan' | 'controller' | 'light' | 'power' | 'camera';
export type Measurement = 'temperature' | 'humidity' | 'co2' | 'power';
export type Health = 'online' | 'stale' | 'offline';
export type Phase = 'seedling' | 'vegetative' | 'flowering' | 'flush' | 'drying' | 'cure';
export type Category = 'tent' | 'controller' | 'fan' | 'light' | 'sensor' | 'camera' | 'plug' | 'combo';

export interface Cycle {
	environmentId: string;
	strain: string;
	startedAt: string;
	phase: Phase;
	phaseStarted: string;
	notes: string;
}

export type LightScheduleMode = 'off' | 'phase' | 'custom';

export interface LightSchedule {
	environmentId: string;
	mode: LightScheduleMode;
	/** Local "HH:MM" the light comes on. */
	lightsOnAt: string;
	/** On-duration used in custom mode. */
	onHours: number;
	/** Per-phase on-hour overrides for phase mode; phases absent use defaults. */
	phaseOnHours: Partial<Record<Phase, number>>;
}

/** Recommended hours of light per phase (from GET /api/lighting/defaults). */
export type PhotoperiodDefaults = Partial<Record<Phase, number>>;

export interface BindingTemplate {
	label: string;
	kind: BindingKind;
	measurement?: Measurement;
	role?: Role;
	entityDomain: string;
	deviceClass?: string;
	wattage?: number;
	rpmEntityDomain?: string;
}

export interface FanPreset {
	id: string;
	label: string;
	sizeMm?: number;
	maxRpm?: number;
	airflowCfm?: number;
	staticPressureMmH2O?: number;
	startingVoltage?: number;
}

export interface CatalogProduct {
	id: string;
	brand: string;
	model: string;
	category: Category;
	connection: string;
	description?: string;
	version: string;
	author: string;
	haIntegration?: string;
	documentation?: string;
	provides?: BindingTemplate[];
	maxChannels?: number;
	fanPresets?: FanPreset[];
}

export interface Location {
	id: string;
	name: string;
	lat: number;
	lon: number;
	address: string;
}

export interface GeocodeResult {
	displayName: string;
	lat: number;
	lon: number;
}

export interface Weather {
	temp: SeriesPoint[];
	humidity: SeriesPoint[];
	pressure: SeriesPoint[];
}

export interface Environment {
	id: string;
	name: string;
	kind: EnvironmentKind;
	airSourceId: string;
	locationId: string;
	model: string;
	widthCm: number;
	depthCm: number;
	heightCm: number;
	targetTempC: number;
	targetHumidity: number;
	targetCO2: number;
	emergencyTempC: number;
	leafTempOffsetC: number;
}

export interface Binding {
	id: string;
	deviceId: string;
	deviceName: string;
	powerControllerId?: string;
	controllerChannelId?: string;
	environmentId: string;
	kind: BindingKind;
	name: string;
	entity: string;
	measurement?: Measurement;
	role?: Role;
	rpmEntity?: string;
	sizeMm?: number;
	maxRpm?: number;
	airflowCfm?: number;
	staticPressureMmH2O?: number;
	startingVoltage?: number;
	wattage?: number;
	primary?: boolean;
}

export interface DiscoveredEntity {
	entity: string;
	name: string;
	kind: BindingKind;
	measurement?: Measurement;
	haDeviceId?: string;
	deviceName?: string;
	integration?: string;
	entityCategory?: string;
	manufacturer?: string;
	model?: string;
	unit?: string;
}

export interface SensorReading {
	id: string;
	name: string;
	measurement: Measurement;
	entity: string;
	value: number;
	ok: boolean;
}

export interface ControlState {
	id: string;
	name: string;
	kind: BindingKind;
	role?: Role;
	entity: string;
	desiredSpeed: number;
	rpm: number;
	maxRpm?: number;
	on: boolean;
	wattage?: number;
	power?: number; // lights: actual measured watts (from plug meter), else rated while on
	primary?: boolean;
}

export interface CameraRef {
	id: string;
	name: string;
	entity: string;
}

export interface AirSourceView {
	id: string;
	name: string;
	tempC: number;
	humidity: number;
	vpd: number;
	ok: boolean;
}

export interface EnvironmentView extends Environment {
	health: Health;
	hasClimate: boolean;
	hasTemp: boolean;
	hasHum: boolean;
	tempC: number;
	humidity: number;
	co2: number;
	hasCO2: boolean;
	vpd: number;
	sensors: SensorReading[];
	controls: ControlState[];
	cameras: CameraRef[];
	airSource?: AirSourceView;
	cycle?: Cycle;
	schedule?: LightSchedule;
}

export interface Snapshot {
	time: string;
	environments: EnvironmentView[];
}

export interface Reading {
	environmentId: string;
	time: string;
	tempC: number;
	humidity: number;
	co2: number;
	vpd: number;
	exhaustSpeed: number;
}

export interface SeriesPoint {
	time: string;
	value: number;
}

export interface DeviceSeries {
	bindingId: string;
	metric: 'rpm' | 'power';
	points: SeriesPoint[];
}

export interface SensorSeries {
	bindingId: string;
	name: string;
	entity: string;
	measurement: Measurement;
	points: SeriesPoint[];
}

export interface WeatherHistory {
	temp: SeriesPoint[];
	humidity: SeriesPoint[];
	pressure: SeriesPoint[];
}

export interface Activity {
	id: string;
	environmentId?: string;
	deviceId?: string;
	time: string;
	level: 'info' | 'warning' | 'error';
	type: 'control' | 'warning' | 'notice' | 'configuration';
	message: string;
}

export interface Info {
	adapter: string;
}
