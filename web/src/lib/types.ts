// Mirrors the JSON emitted by Grow Core (growcore/internal/domain).

export type Role = 'unassigned' | 'exhaust' | 'intake' | 'circulation';
export type EnvironmentKind = 'tent' | 'room';
export type BindingKind = 'sensor' | 'fan' | 'controller' | 'light' | 'power' | 'camera' | 'irrigation';
export type Measurement = 'temperature' | 'humidity' | 'co2' | 'power';
export type Health = 'online' | 'stale' | 'offline';
export type Category =
	| 'tent'
	| 'controller'
	| 'fan'
	| 'light'
	| 'sensor'
	| 'camera'
	| 'plug'
	| 'irrigation'
	| 'combo';
export type FanType = 'pc' | 'inline' | 'other';
export type CameraType = 'mjpeg' | 'snapshot' | 'rtsp';
export type IrrigationType = 'autopot' | 'drip' | 'wick' | 'ebb_flow' | 'hand';
export type IrrigationMode = 'passive' | 'controlled';

// --- Cultivation layer (grows, plant units, placements) ---

export type GrowStatus = 'active' | 'completed' | 'archived';
export type TrackingMode = 'individual' | 'group';
export type PlantStatus = 'active' | 'harvested' | 'removed' | 'archived';

export interface Grow {
	id: string;
	name: string;
	/** URL-friendly form of `name`, derived on save. */
	slug: string;
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
	/** How the crop is grown: medium, nutrients and default container. */
	setup: GrowingSetup;
}

/** Per-grow cultivation setup. Medium is species-curated but grower-chosen; the
 *  container seeds each new plant's pot. Irrigation is not here — it lives on the
 *  environment (see `Environment`/bindings), which the grow references. */
export interface GrowingSetup {
	/** soil | coco | soilless | hydroponic | aeroponic */
	medium?: string;
	/**
	 * The MediumRecipe this grow's substrate is mixed from. It is the default
	 * for the grow's plants: a plant with its own `mediumRecipeId` overrides it.
	 */
	mediumRecipeId?: string;
	/** Free text: commercial product or composition. */
	mediumDetails?: string;
	/** organic | mineral | living-soil */
	nutrientMethod?: string;
	/** Default container that seeds new plants' pots. */
	potSize?: number;
	potUnit?: string;
	potType?: string;
}

export interface PlantUnit {
	id: string;
	growId: string;
	label: string;
	/** URL-friendly form of `label`, derived on save. */
	slug: string;
	/** Cultivar is per-unit, so one grow can mix cultivars. */
	cultivar: string;
	/** Overrides the grow's medium recipe; empty means inherit it. */
	mediumRecipeId?: string;
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
	/** Projected total length in days, summed from the species' per-stage typical
	 *  durations. 0 when the species carries no estimate. */
	estimatedDays: number;
	/** Per-stage typical duration in days (stage name → days), for projecting
	 *  per-phase boundaries and predicted stage-switch milestones. */
	stageEstimates?: Record<string, number>;
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
	/** Per-plant amendment doses, keyed by FeedingProduct.key. */
	doses?: Record<string, number>;
	/** `doses` rendered for display, e.g. "2 tabs · 1 g Mycotrex". */
	dosesLabel?: string;
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
	/** Per-plant amendment doses, keyed by FeedingProduct.key. */
	doses?: Record<string, number>;
	amountMl?: number;
	plantUnitIds?: string[];
	applications?: CareApplicationInput[];
}

/** One care action on the calendar (GET /api/calendar): a cross-grow, dated
 *  projection of a care event carrying just what the calendar renders. */
export interface CalendarEvent {
	id: string;
	growId: string;
	growName: string;
	type: string;
	occurredAt: string;
	source: CareSource;
	plantCount: number;
	totalMl?: number;
	recipeName?: string;
	notes?: string;
}

/** Response for GET /api/calendar: care events across all grows in a window. */
export interface CalendarResponse {
	from?: string;
	to?: string;
	events: CalendarEvent[];
}

/** Built-in editable stage sequences per crop family (GET /api/stage-presets). */
export type StagePresets = Record<string, string[]>;

// --- Grow photos, stage history & analytics ---

/** A photo attached to a grow (bytes served separately via growPhotoImageURL). */
export interface GrowPhoto {
	id: string;
	growId: string;
	plantUnitId?: string;
	caption?: string;
	takenAt: string;
	imageType: string;
	createdAt: string;
}

/** One stage transition in a grow's history. */
export interface StageEvent {
	id: string;
	growId: string;
	stage: string;
	enteredAt: string;
}

export interface StageDuration {
	stage: string;
	from: string;
	to?: string;
	days: number;
}
export interface CareWeek {
	weekStart: string;
	count: number;
	feedCount: number;
	totalMl: number;
}
export interface PlacementSpan {
	environmentId: string;
	environmentName: string;
	from: string;
	to?: string;
}

/** Interpreted grow data (GET /api/grows/{id}/analytics). */
export interface GrowAnalytics {
	stageDurations: StageDuration[];
	careByWeek: CareWeek[];
	careFrequency: Record<string, number>;
	placements: PlacementSpan[];
	pctInTarget?: number;
	sampleCount: number;
}

/** Body for POST /api/grows/{id}/photos. */
export interface UploadPhotoInput {
	image: string; // data:<mime>;base64,<payload>
	caption?: string;
	plantUnitId?: string;
	takenAt?: string;
}

// --- Alerts, tasks & attention ---

export type AlertSeverity = 'info' | 'warning' | 'critical';
export type AlertStatus = 'open' | 'acknowledged' | 'resolved';

/** A system-detected condition ("something is wrong"), opened/resolved by the
 *  control engine and deduplicated by `key`. */
export interface Alert {
	id: string;
	key: string;
	environmentId?: string;
	growId?: string;
	deviceId?: string;
	severity: AlertSeverity;
	kind: string;
	title: string;
	message?: string;
	status: AlertStatus;
	firstSeenAt: string;
	lastSeenAt: string;
	acknowledgedAt?: string;
	resolvedAt?: string;
}

export type TaskStatus = 'open' | 'completed' | 'skipped';
export type TaskSource = 'manual' | 'recipe' | 'automation';

/** Something the grower should do ("something should be done"). Completing a
 *  task records a care event and links it via `completedCareEventId`. */
export interface Task {
	id: string;
	growId?: string;
	environmentId?: string;
	plantUnitId?: string;
	actionType: string;
	title: string;
	dueAt?: string;
	status: TaskStatus;
	source: TaskSource;
	completedCareEventId?: string;
	createdAt: string;
	completedAt?: string;
}

export interface LowStockItem {
	id: string;
	name: string;
	category: string;
	quantity: number;
}

export interface UnhealthyIntegration {
	id: string;
	name: string;
	status: string;
	message?: string;
}

/** Live projection of everything needing attention (GET /api/attention). Not
 *  persisted — recomputed per request from alerts, tasks, inventory, integrations. */
export interface Attention {
	alerts: Alert[];
	tasks: Task[];
	lowStock: LowStockItem[];
	integrations: UnhealthyIntegration[];
}

/** Body for POST /api/tasks. */
export interface CreateTaskInput {
	growId?: string;
	environmentId?: string;
	plantUnitId?: string;
	actionType?: string;
	title: string;
	dueAt?: string;
	source?: TaskSource;
}

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
	typicalDays?: number;
	/** Whether a grower may include or omit this stage per grow (propagation /
	 *  post-harvest phases). Non-optional stages are always in the sequence. */
	optional?: boolean;
	/** For an optional stage, whether it is pre-selected on a new grow.
	 *  Undefined means on; ignored for non-optional stages. */
	default?: boolean;
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
	| 'trainType'
	| 'doses';

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
	/** Curated growing-media options for this crop (grower picks one per grow). */
	media?: { default?: string; options: string[] };
	/** Curated nutrient-method options for this crop. */
	nutrientMethods?: string[];
	/** Served URL of the species' catalog icon (SVG), absent when it ships none. */
	icon?: string;
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
/**
 * What a dose is measured against. Absent means per litre of nutrient solution
 * — the common case. Dry amendments (tablets, mycorrhiza powder) are dosed per
 * plant, per pot, or per litre of substrate instead.
 */
export type DoseBasis = 'plant' | 'pot' | 'substrate';

export interface FeedingProduct {
	key: string;
	label: string;
	unit?: string;
	basis?: DoseBasis;
}

/** One week of dosing: product key -> amount in that product's unit. */
export interface FeedingWeek {
	doses: Record<string, number>;
}

/** The event a one-shot phase is applied at, instead of running week by week. */
export type PhaseTrigger = 'planting' | 'transplant' | 'topdress';

/**
 * A named span of the schedule; `stage` optionally links it to a species stage.
 *
 * A phase is either weekly (`weeks`) or event-triggered (`trigger` + `doses`),
 * never both. A triggered phase is a single application — a soil amendment
 * buried at planting, say — with no position on the week timeline.
 */
export interface FeedingPhase {
	name: string;
	stage?: string;
	trigger?: PhaseTrigger;
	/** One-shot doses for a triggered phase, keyed like a week's doses. */
	doses?: Record<string, number>;
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

/**
 * A growing-medium composition: what a substrate is mixed from.
 *
 * `components` are bulk ingredients as a share of total volume and should total
 * 100; `amendments` are dosed against the finished mix (5 g per litre) and sit
 * outside that total. Built-in recipes (`source: 'builtin'`) are read-only;
 * user recipes (`source: 'user'`) are created in-app.
 */
export interface MediumRecipe {
	id: string;
	name: string;
	description?: string;
	source: 'builtin' | 'user';
	/** Optional crop tag; recipes are global, so empty means "any crop". */
	species?: string;
	/** Coarse medium category this mix produces (soil, coco, …). */
	base?: string;
	components: MediumComponent[];
	amendments?: MediumAmendment[];
	createdAt: string;
}

/**
 * One bulk ingredient as a share of total mix volume.
 *
 * Normally a catalog product (`productId`, e.g. "consumables/biobizz-light-mix")
 * — the product carries the brand, image and description, which is why a recipe
 * has no brand of its own. Loose materials with no catalog entry (perlite, a
 * local compost) are recorded by `label` alone. `label` is always set: for a
 * product it snapshots the name so the recipe still reads if the catalog moves.
 */
export interface MediumComponent {
	productId?: string;
	label: string;
	percent: number;
	note?: string;
}

/** An ingredient dosed per litre of finished mix rather than taking a share. */
export interface MediumAmendment {
	productId?: string;
	label: string;
	amount: number;
	/** Unit of `amount` per litre of mix — "g" means g/L. */
	unit?: string;
	note?: string;
}

// --- History: a collection's merged, chronological story (GET /api/history) ---

export type HistoryKind = 'care' | 'photo' | 'milestone' | 'placement' | 'pot' | 'system';

/** A thing an entry points at, resolved to a label so it can render as a link. */
export interface HistoryRef {
	kind: 'plant' | 'grow' | 'environment' | 'device';
	id: string;
	label: string;
}

/**
 * One thing that happened, at one moment.
 *
 * Entries carry structured records, never rendered sentences — the wording is
 * built client-side (see history.ts), which is what keeps the targets inside a
 * line linkable. Exactly one payload is set, matching `kind`.
 */
export interface HistoryEntry {
	id: string;
	kind: HistoryKind;
	at: string;
	/** Routine control-loop chatter; hidden until asked for. */
	automation?: boolean;
	refs?: HistoryRef[];
	care?: CareEvent;
	photo?: GrowPhoto;
	stage?: string;
	placement?: PlantPlacement;
	pot?: PlantPot;
	activity?: Activity;
}

/** One page of history plus the total available in that scope. */
export interface HistoryPage {
	items: HistoryEntry[];
	total: number;
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
	irrigationType?: IrrigationType;
	irrigationMode?: IrrigationMode;
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
	/** Optional display target ranges (0 = unset); do not affect control. */
	targetTempMinC: number;
	targetTempMaxC: number;
	targetHumidityMin: number;
	targetHumidityMax: number;
	targetVpdMin: number;
	targetVpdMax: number;
	targetCo2Min: number;
	targetCo2Max: number;
	emergencyTempC: number;
	leafTempOffsetC: number;
	/** Per-category automation policy. On a live EnvironmentView this is resolved
	 *  (defaults filled); on a raw Environment it may be partially unset. */
	control: ControlConfig;
}

export type ControlMode = 'auto' | 'manual';

export interface ControlConfig {
	lighting: { mode: ControlMode };
	airExchange: { mode: ControlMode; exhaust: number; circulation: number };
	irrigation: { mode: ControlMode };
}

export interface Binding {
	id: string;
	deviceId: string;
	deviceName: string;
	/** Catalog product this device was installed from ("<category>/<id>"). */
	productId?: string;
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
	irrigationType?: IrrigationType;
	irrigationMode?: IrrigationMode;
	reservoirL?: number;
	valveCount?: number;
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

export interface IrrigationRef {
	id: string;
	name: string;
	type: IrrigationType;
	mode: IrrigationMode;
	reservoirL?: number;
	valveCount?: number;
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
	irrigation: IrrigationRef[];
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

// --- Tailscale remote access (admin) ---

export type TailscaleState = 'stopped' | 'starting' | 'needs-login' | 'running' | 'error';

export interface TailscaleStatus {
	/** Whether remote access is compiled/wired into this build. */
	available: boolean;
	enabled: boolean;
	state: TailscaleState;
	hostname: string;
	/** Login URL to authorize the node into the user's tailnet (while needs-login). */
	authUrl?: string;
	/** Remote HTTPS URL once running. */
	url?: string;
	dnsName?: string;
	controlUrl?: string;
	/** Node-key expiry (ISO); the node becomes unreachable after this. */
	keyExpiry?: string;
	keyExpired: boolean;
	error?: string;
}

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

// --- Additional catalog packages (Control panel → Catalogs) ---
export interface CatalogSource {
	id: string;
	repository: string;
	provider: 'github' | 'gitlab' | 'bitbucket' | 'codeberg' | 'gitea' | 'forgejo' | 'archive';
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
