// Mirrors the JSON emitted by Grow Core (growcore/internal/domain).

export type Role = 'unassigned' | 'exhaust' | 'intake' | 'circulation';
export type EnvironmentKind = 'tent' | 'room';
export type BindingKind = 'sensor' | 'fan' | 'controller' | 'light' | 'power' | 'camera';
export type Measurement = 'temperature' | 'humidity' | 'co2' | 'power';
export type Health = 'online' | 'stale' | 'offline';
export type Category = 'tent' | 'controller' | 'fan' | 'light' | 'sensor' | 'camera' | 'plug' | 'combo';
export type FanType = 'pc' | 'inline' | 'other';
export type CameraType = 'mjpeg' | 'snapshot' | 'rtsp';

// --- Cultivation layer (grows, plant units, placements) ---

export type GrowStatus = 'active' | 'completed' | 'archived';
export type TrackingMode = 'individual' | 'group';
export type PlantStatus = 'active' | 'harvested' | 'removed' | 'archived';

export interface Grow {
	id: string;
	name: string;
	/** A predefined crop family; drives the stage sequence. */
	species: string;
	/** Current stage name (one of `stages`). */
	stage: string;
	/** Ordered stage sequence, derived from `species`. */
	stages: string[];
	startedAt: string;
	stageStarted: string;
	status: GrowStatus;
	notes: string;
}

export interface PlantUnit {
	id: string;
	growId: string;
	label: string;
	/** Cultivar is per-unit, so one grow can mix cultivars. */
	cultivar: string;
	tracking: TrackingMode;
	quantity: number;
	status: PlantStatus;
	createdAt: string;
}

export interface PlantPlacement {
	id: string;
	plantUnitId: string;
	environmentId: string;
	startedAt: string;
	endedAt?: string; // absent = current
	position?: string;
}

export interface GrowEnvRef {
	id: string;
	name: string;
}

/** Count of active plants of one cultivar within a grow (for card thumbnails). */
export interface GrowCultivarRef {
	cultivar: string;
	count: number;
}

/** Compact live view of an environment's control grow. */
export interface GrowSummary {
	id: string;
	name: string;
	species: string;
	stage: string;
	stageDays: number;
	totalDays: number;
	plantCount: number;
}

/** Dashboard "Active Grows" view: a grow plus derived counts and locations. */
export interface GrowView extends Grow {
	stageDays: number;
	totalDays: number;
	plantCount: number;
	environments: GrowEnvRef[];
	cultivars: GrowCultivarRef[];
}

export interface PlacementView extends PlantPlacement {
	environmentName: string;
}

export type PotUnit = 'L' | 'gal';

/** One pot a plant lived in for a span of time (repot history). */
export interface PlantPot {
	id: string;
	plantUnitId: string;
	size: number;
	unit: PotUnit;
	type?: string;
	startedAt: string;
	endedAt?: string; // absent = current pot
}

export interface PlantDetail extends PlantUnit {
	currentEnvironmentId: string;
	currentEnvironmentName: string;
	placements: PlacementView[];
	currentPot?: PlantPot;
	pots: PlantPot[];
}

export interface PlantView extends PlantUnit {
	growName: string;
	currentEnvironmentId: string;
	currentEnvironmentName: string;
	placements: PlacementView[];
	currentPot?: PlantPot;
	pots: PlantPot[];
}

export interface GrowDetail extends Grow {
	stageDays: number;
	totalDays: number;
	plantCount: number;
	plants: PlantDetail[];
}

/** Current occupants of an environment, grouped by grow. */
export interface EnvPlantsGroup {
	grow: Grow;
	units: PlantUnit[];
}

// --- Care: the grow's manual-action journal (GET/POST /api/grows/{id}/care) ---

export type CareSource = 'manual' | 'automation';

/** What one plant received in a care event. */
export interface CareApplication {
	id: string;
	careEventId: string;
	plantUnitId: string;
	plantLabel: string;
	amountMl?: number;
	note?: string;
}

/** One care action performed against a grow's plants at a moment in time. */
export interface CareEvent {
	id: string;
	growId: string;
	type: string;
	occurredAt: string;
	source: CareSource;
	notes?: string;
	recipeId?: string;
	recipeName?: string;
	ph?: number;
	ec?: number;
	runoffMl?: number;
	runoffPh?: number;
	createdAt: string;
	applications: CareApplication[];
}

/** A plant left out of the grow's most recent care action. */
export interface CareSkip {
	plantUnitId: string;
	plantLabel: string;
	lastCareAt?: string;
}

export interface CareSummary {
	lastByType: Record<string, CareEvent>;
	skipped: CareSkip[];
}

export interface CareHistory {
	summary: CareSummary;
	events: CareEvent[];
}

/** One plant's line in a log-care request. */
export interface CareApplicationInput {
	plantUnitId: string;
	amountMl?: number;
	note?: string;
}

/** Body for POST /api/grows/{id}/care. */
export interface LogCareInput {
	type: string;
	occurredAt?: string;
	source?: CareSource;
	notes?: string;
	recipeId?: string;
	ph?: number;
	ec?: number;
	runoffMl?: number;
	runoffPh?: number;
	amountMl?: number;
	plantUnitIds?: string[];
	applications?: CareApplicationInput[];
}

/** Built-in editable stage sequences per crop family (GET /api/stage-presets). */
export type StagePresets = Record<string, string[]>;

// --- Species catalog & cultivars (YAML-defined; GET /api/species) ---

export type AttrType = 'text' | 'number' | 'percent' | 'enum';

/** One species-specific cultivar field declared in a species' YAML. */
export interface SpeciesAttribute {
	key: string;
	label: string;
	type: AttrType;
	options?: string[];
	unit?: string;
}

export interface SpeciesStage {
	name: string;
	lightHours: number;
}

/** A form field a care action may show when logging it. */
export type CareField =
	| 'amount'
	| 'runoff'
	| 'recipe'
	| 'ph'
	| 'ec'
	| 'note'
	| 'photos'
	| 'potSize'
	| 'product'
	| 'trainType';

/** One manual action a grower can log against a grow's plants (species-driven). */
export interface CareAction {
	key: string;
	label: string;
	icon?: string;
	fields: CareField[];
	quick?: boolean;
}

/** A resolved care action for a grow: the effective action (species default
 * overlaid with per-grow config) plus its enabled/custom flags. */
export interface CareActionDef extends CareAction {
	enabled: boolean;
	custom: boolean;
}

/** One action's per-grow customization, sent to PUT /api/grows/{id}/care-config. */
export interface GrowCareActionConfig {
	key: string;
	label?: string;
	enabled: boolean;
	quick: boolean;
	custom?: boolean;
	fields?: CareField[];
}

/** A crop family: its ordered stages, cultivar attribute schema and care actions. */
export interface Species {
	id: string;
	label: string;
	stages: SpeciesStage[];
	cultivarAttributes?: SpeciesAttribute[];
	careActions?: CareAction[];
}

/** A user-defined strain/variety within a species. */
export interface Cultivar {
	id: string;
	species: string;
	name: string;
	creator: string;
	description: string;
	/** Species-specific values keyed by the species' attribute keys. */
	attributes: Record<string, string>;
	/** MIME type of the stored image, or absent when there is none. */
	imageType?: string;
	createdAt: string;
}

// --- Inventory catalog & items (categories YAML-defined; GET /api/inventory) ---

export type InventoryColumnType = 'text' | 'number' | 'enum' | 'date';

/** One category-specific item field declared in a category's YAML. */
export interface InventoryColumn {
	key: string;
	label: string;
	type: InventoryColumnType;
	options?: string[];
	unit?: string;
}

/** A stock category: its display metadata and extra column schema. */
export interface InventoryCategory {
	id: string;
	label: string;
	description?: string;
	icon?: string;
	order: number;
	units?: string[];
	columns?: InventoryColumn[];
}

export type InventoryStatus = 'active' | 'ordered' | 'archived';

/** One pack size of a product, with an optional product code (SKU/barcode). */
export interface InventoryVariant {
	size: string;
	code?: string;
}

/** A built-in product template (GET /api/inventory/products) that seeds and
 *  binds an item. `id` is fully-qualified as "<category>/<product-id>". */
export interface InventoryProduct {
	id: string;
	category: string;
	name: string;
	description?: string;
	unit?: string;
	/** Pack sizes this product is sold in; offered as a size picker. */
	variants?: InventoryVariant[];
	attributes?: Record<string, string>;
	/** Whether the catalog ships an image for this product. */
	hasImage: boolean;
}

/** One pack size of an owned item with its own on-hand quantity. A simple item
 *  is a single line with a blank size. */
export interface InventoryStockLine {
	size: string;
	quantity: number;
	/** Quantity threshold at/below which this size is low; 0/absent disables. */
	lowStockAt?: number;
}

/** A stock record the grower owns, within an inventory category. */
export interface InventoryItem {
	id: string;
	category: string;
	name: string;
	/** The sizes owned, each with its own quantity. */
	variants: InventoryStockLine[];
	location: string;
	status: InventoryStatus;
	notes: string;
	/** Category-specific values keyed by the category's column keys. */
	attributes: Record<string, string>;
	/** Bound built-in product template ("<category>/<id>"), or absent. */
	productId?: string;
	/** MIME type of a user-uploaded image, or absent when there is none. */
	imageType?: string;
	createdAt: string;
	updatedAt: string;
}

// --- Feeding recipes (nutrient schedules; GET /api/recipes) ---

/** One nutrient line in a schedule. `unit` overrides the recipe default. */
export interface FeedingProduct {
	key: string;
	label: string;
	unit?: string;
}

/** One week of dosing: product key -> amount in that product's unit. */
export interface FeedingWeek {
	doses: Record<string, number>;
}

/** A named span of the schedule; `stage` optionally links it to a species stage. */
export interface FeedingPhase {
	name: string;
	stage?: string;
	weeks: FeedingWeek[];
}

/**
 * A nutrient feeding schedule: products dosed per week across phases. Built-in
 * recipe templates (`source: 'builtin'`) come from species/<id>/feedings.yaml
 * and are read-only; user recipes (`source: 'user'`) are created in-app.
 */
export interface FeedingRecipe {
	id: string;
	species: string;
	name: string;
	brand: string;
	description: string;
	source: 'builtin' | 'user';
	/** Default dose unit, e.g. "ml/L". */
	unit: string;
	products: FeedingProduct[];
	phases: FeedingPhase[];
	createdAt: string;
}

export type LightScheduleMode = 'off' | 'phase' | 'custom';

export interface LightSchedule {
	environmentId: string;
	mode: LightScheduleMode;
	/** Local "HH:MM" the light comes on. */
	lightsOnAt: string;
	/** On-duration used in custom mode. */
	onHours: number;
	/** Per-stage on-hour overrides for phase mode; stages absent use defaults. */
	stageOnHours: Record<string, number>;
}

/** Recommended hours of light per known stage (GET /api/lighting/defaults). */
export type StageLightDefaults = Record<string, number>;

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

/** A concrete product supported by a driver. `specs` is a free-form numeric map
 *  (fans: sizeMm/maxRpm/airflowCfm/…; tents: widthCm/depthCm/heightCm). */
export interface ProductVariant {
	id: string;
	brand?: string;
	vendor?: string;
	group?: string;
	model?: string;
	image?: string;
	images?: { src: string; model?: string }[];
	description?: string;
	specs?: Record<string, number>;
	models?: ProductVariant[];
}

export interface CatalogProduct {
	id: string;
	/** Custom catalog source id; absent for GrowRig's built-in catalog. */
	source?: string;
	brand: string;
	vendor?: string;
	model: string;
	image?: string;
	category: Category;
	connection: string;
	description?: string;
	version: string;
	author: string;
	haIntegration?: string;
	documentation?: string;
	provides?: BindingTemplate[];
	maxChannels?: number;
	products?: ProductVariant[];
	fanType?: FanType;
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
	controlGrowId: string;
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
	fanType?: FanType;
	sizeMm?: number;
	maxRpm?: number;
	airflowCfm?: number;
	staticPressureMmH2O?: number;
	startingVoltage?: number;
	ductSizeInches?: number;
	noiseDba?: number;
	wattage?: number;
	primary?: boolean;
	streamUrl?: string;
	cameraType?: CameraType;
	cameraCaptureInterval?: number;
	cameraRetentionDays?: number;
	cameraStorageMb?: number;
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
	entity?: string;
	/** Generic (non-Home-Assistant) camera stream. */
	streamUrl?: string;
	cameraType?: CameraType;
	cameraCaptureInterval?: number;
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
	grow?: GrowSummary;
	schedule?: LightSchedule;
}

export interface Snapshot {
	time: string;
	environments: EnvironmentView[];
	grows: GrowView[];
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
	metric: 'rpm' | 'speed' | 'power';
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
	growId?: string;
	deviceId?: string;
	time: string;
	level: 'info' | 'warning' | 'error';
	type: 'control' | 'warning' | 'notice' | 'configuration' | 'care';
	message: string;
}

export interface Info {
	adapter: string;
}

// --- users & auth ---

export type UserRole = 'admin' | 'user';
export type AccessLevel = 'read' | 'write';

export interface EnvAccess {
	environmentId: string;
	access: AccessLevel;
}

export interface User {
	id: string;
	username: string;
	role: UserRole;
	disabled: boolean;
	created: string;
	/** Per-environment grants; empty/omitted for admins (implicit full access). */
	access: EnvAccess[];
}

export interface AuthStatus {
	needsSetup: boolean;
	signupEnabled: boolean;
}

export interface AuthResult {
	token: string;
	user: User;
}

// --- Home Assistant appliance status (admin control panel) ---

export interface HAComponent {
	version: string;
	versionLatest: string;
	updateAvailable: boolean;
}

export interface HAAddon {
	slug: string;
	name: string;
	version: string;
	versionLatest: string;
	updateAvailable: boolean;
}

export interface HASupervisor {
	/** True only when GrowRig runs as a HAOS add-on (Supervisor reachable). */
	available: boolean;
	core: HAComponent;
	os: HAComponent;
	supervisor: HAComponent;
	addons: HAAddon[];
	error?: string;
}

export interface HAStatus {
	adapter: string; // 'simulator' | 'homeassistant'
	health: string; // 'online' | 'stale' | 'offline'
	supervisor: HASupervisor;
}

export type HAUpdateTarget = 'core' | 'os' | 'supervisor' | 'addon';

// --- External integrations (kept separate from physical device bindings) ---
export interface IntegrationConfigField {
	key: string;
	label: string;
	type: 'text' | 'password' | 'url' | 'number' | 'select';
	required: boolean;
	secret: boolean;
	default?: string;
	placeholder?: string;
	help?: string;
	options?: string[];
}

export interface IntegrationBundle {
	id: string;
	/** Custom catalog source id; absent for GrowRig's built-in catalog. */
	source?: string;
	name: string;
	version: string;
	category: string;
	description: string;
	capabilities: string[];
	config: IntegrationConfigField[];
	icon?: string;
	documentation?: string;
}

// --- Additional catalog repositories (Control panel → Catalog) ---
export interface CatalogSource {
	id: string;
	repo: string;
	ref?: string;
	name: string;
	description?: string;
	maintainer?: string;
	homepage?: string;
	provides: string[];
	addedAt: string;
	fetchedAt: string;
}

export interface CatalogSourcesResponse {
	sources: CatalogSource[];
	mergedKinds: string[];
}

export interface IntegrationInstance {
	id: string;
	bundleId: string;
	name: string;
	config: Record<string, string>;
	secretFields?: string[];
	enabled: boolean;
	status: 'unknown' | 'healthy' | 'error' | 'disabled';
	statusMessage?: string;
	lastCheckedAt?: string;
	createdAt: string;
	updatedAt: string;
}

export interface IntegrationBinding {
	id: string;
	feature: string;
	growId?: string;
	environmentId?: string;
	capability: string;
	instanceId: string;
	createdAt: string;
	updatedAt: string;
}
