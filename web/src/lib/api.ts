// REST client for Grow Core. When the web app is served by Grow Core itself
// (embedded, single binary) the base is same-origin. For local development
// against a separately-running Grow Core, set VITE_GROWCORE_URL.
import type {
	Binding,
	Activity,
	AuthResult,
	AuthStatus,
	BindingKind,
	CameraType,
	CalendarResponse,
	CareActionDef,
	CareEvent,
	CareHistory,
	CatalogProduct,
	Cultivar,
	GrowCareActionConfig,
	LogCareInput,
	DeviceSeries,
	DiscoveredEntity,
	Environment,
	EnvironmentKind,
	EnvAccess,
	EnvPlantsGroup,
	FanType,
	FeedingRecipe,
	FeedingProduct,
	FeedingPhase,
	GeocodeResult,
	Grow,
	GrowDetail,
	InventoryCategory,
	InventoryItem,
	InventoryProduct,
	InventoryStockLine,
	InventoryStatus,
	HAStatus,
	HAUpdateTarget,
	Info,
	Location,
	PlantUnit,
	PlantView,
	Species,
	StagePresets,
	TrackingMode,
	PotUnit,
	User,
	UserRole,
	Weather,
	LightSchedule,
	LightScheduleMode,
	Measurement,
	StageLightDefaults,
	Reading,
	Role,
	SensorSeries,
	Snapshot,
	WeatherHistory,
	IntegrationBundle,
	IntegrationInstance,
	IntegrationBinding,
	CatalogSource,
	CatalogSourcesResponse
} from './types';

export const CORE_URL: string = import.meta.env.VITE_GROWCORE_URL?.replace(/\/$/, '') ?? '';

// --- auth token plumbing ---
// The bearer token is held here so both the REST client and the WebSocket can
// read it. The auth store owns its lifecycle (persisting to localStorage); it
// installs a callback so a 401 can force a re-login without a circular import.
let authToken: string | null = null;
let onUnauthorized: (() => void) | null = null;

export function setAuthToken(token: string | null) {
	authToken = token;
}
export function getAuthToken(): string | null {
	return authToken;
}
export function setUnauthorizedHandler(fn: (() => void) | null) {
	onUnauthorized = fn;
}

export function wsURL(): string {
	const base = CORE_URL || window.location.origin;
	const u = new URL(base);
	u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
	u.pathname = '/api/ws';
	// Browsers can't set headers on a WebSocket handshake, so the token rides in
	// the query string (localhost/same-origin; the server also accepts a bearer
	// header on REST).
	if (authToken) u.searchParams.set('token', authToken);
	return u.toString();
}

/** Authenticated, same-origin snapshot URL for an HA-backed camera binding. */
export function cameraProxyURL(bindingId: string, live = false): string {
	const url = `${CORE_URL}/api/bindings/${encodeURIComponent(bindingId)}/camera${live ? '/live' : ''}`;
	if (!authToken) return url;
	return `${url}?token=${encodeURIComponent(authToken)}`;
}

function authenticatedMediaURL(path: string): string {
	const url = `${CORE_URL}${path}`;
	return authToken ? `${url}?token=${encodeURIComponent(authToken)}` : url;
}

export interface CameraSnapshot { id: string; time: string }
export const getCameraSnapshots = (bindingId: string, limit = 200) =>
	json<CameraSnapshot[]>(`/api/bindings/${encodeURIComponent(bindingId)}/camera/archive?limit=${limit}`);
export const cameraArchiveURL = (bindingId: string, snapshotId: string) =>
	authenticatedMediaURL(`/api/bindings/${encodeURIComponent(bindingId)}/camera/archive/${encodeURIComponent(snapshotId)}`);
export interface CameraStats {
	bitrateBps: number;
	fps: number;
	online: boolean;
	lastFrame?: string;
	status: 'connecting' | 'online' | 'stalled' | 'reconnecting';
	retryCount: number;
	lastError?: string;
	statusSince?: string;
}
export const getCameraStats = (bindingId: string) =>
	json<CameraStats>(`/api/bindings/${encodeURIComponent(bindingId)}/camera/stats`);

async function req(path: string, init?: RequestInit): Promise<Response> {
	const headers = new Headers(init?.headers ?? { 'Content-Type': 'application/json' });
	if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
	if (authToken) headers.set('Authorization', `Bearer ${authToken}`);
	const res = await fetch(`${CORE_URL}${path}`, { ...init, headers });
	if (!res.ok) {
		// A 401 means the session is gone/expired; let the auth store react
		// (clear token, route to /login) unless we're already on an auth call.
		if (res.status === 401 && !path.startsWith('/api/auth/')) {
			onUnauthorized?.();
		}
		let msg = `${res.status} ${res.statusText}`;
		try {
			const body = await res.json();
			if (body?.error) msg = body.error;
		} catch {
			/* non-JSON error body */
		}
		throw new Error(msg);
	}
	return res;
}

async function json<T>(path: string, init?: RequestInit): Promise<T> {
	return (await req(path, init)).json() as Promise<T>;
}

// --- info, catalog & discovery ---

export const getInfo = () => json<Info>('/api/info');
export interface Preferences { version: number; timezone: string; locale: string }
export const getPreferences = () => json<Preferences>('/api/preferences');
export const updatePreferences = (prefs: Pick<Preferences, 'timezone' | 'locale'>) =>
	json<Preferences>('/api/preferences', { method: 'PUT', body: JSON.stringify(prefs) });
/** Current live snapshot over REST — used for the initial paint before the
 *  WebSocket feed takes over. */
export const getState = () => json<Snapshot>('/api/state');
export const getDiscovery = () => json<DiscoveredEntity[]>('/api/discovery');
export const getCatalog = () => json<CatalogProduct[]>('/api/catalog');
export const getCatalogSources = () => json<CatalogSourcesResponse>('/api/catalog-sources');
export const createCatalogSource = (repository: string, ref = '') =>
	json<CatalogSource>('/api/catalog-sources', { method: 'POST', body: JSON.stringify({ repository, ref }) });
export const refreshCatalogSource = (id: string) =>
	json<CatalogSource>(`/api/catalog-sources/${encodeURIComponent(id)}/refresh`, { method: 'POST' });
export const deleteCatalogSource = (id: string) =>
	req(`/api/catalog-sources/${encodeURIComponent(id)}`, { method: 'DELETE' });
export const getStagePresets = () => json<StagePresets>('/api/stage-presets');
export const loadDemo = () => req('/api/demo', { method: 'POST' });

// --- environments ---

export const getEnvironments = () => json<Environment[]>('/api/environments');

export interface EnvironmentInput {
	name: string;
	kind: EnvironmentKind;
	airSourceId: string;
	locationId?: string;
	model?: string;
	widthCm?: number;
	depthCm?: number;
	heightCm?: number;
	targetTempC: number;
	targetHumidity: number;
	targetCO2: number;
	emergencyTempC: number;
	leafTempOffsetC: number;
}

export const createEnvironment = (env: EnvironmentInput) =>
	json<Environment>('/api/environments', { method: 'POST', body: JSON.stringify(env) });

export const updateEnvironment = (id: string, env: EnvironmentInput) =>
	json<Environment>(`/api/environments/${encodeURIComponent(id)}`, {
		method: 'PUT',
		body: JSON.stringify(env)
	});

export const deleteEnvironment = (id: string) =>
	req(`/api/environments/${encodeURIComponent(id)}`, { method: 'DELETE' });

export const getEnvironmentYAML = async (id: string) =>
	(await req(`/api/environments/${encodeURIComponent(id)}/config`)).text();

export const updateEnvironmentYAML = (id: string, yaml: string) =>
	req(`/api/environments/${encodeURIComponent(id)}/config`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/yaml' },
		body: yaml
	});

// --- locations, geocoding & weather ---

export const getLocations = () => json<Location[]>('/api/locations');

export interface LocationInput {
	name: string;
	lat: number;
	lon: number;
	address?: string;
}

export const createLocation = (l: LocationInput) =>
	json<Location>('/api/locations', { method: 'POST', body: JSON.stringify(l) });

export const updateLocation = (id: string, l: LocationInput) =>
	json<Location>(`/api/locations/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(l) });

export const deleteLocation = (id: string) =>
	req(`/api/locations/${encodeURIComponent(id)}`, { method: 'DELETE' });

/** Geocode an address or POI via Grow Core's Nominatim proxy. */
export const geocode = (q: string) => json<GeocodeResult[]>(`/api/geocode?q=${encodeURIComponent(q)}`);

/** Local hourly weather (past + forecast) for coordinates, via Open-Meteo proxy. */
export const weather = (lat: number, lon: number) => json<Weather>(`/api/weather?lat=${lat}&lon=${lon}`);

// --- bindings ---

export const getBindings = () => json<Binding[]>('/api/bindings');

export interface BindingInput {
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

export const createBinding = (b: BindingInput) =>
	json<Binding>('/api/bindings', { method: 'POST', body: JSON.stringify(b) });

export const updateBinding = (id: string, b: BindingInput) =>
	json<Binding>(`/api/bindings/${encodeURIComponent(id)}`, {
		method: 'PUT',
		body: JSON.stringify(b)
	});

export const deleteBinding = (id: string) =>
	req(`/api/bindings/${encodeURIComponent(id)}`, { method: 'DELETE' });

export const setSwitch = (bindingId: string, on: boolean) =>
	req(`/api/bindings/${encodeURIComponent(bindingId)}/switch`, {
		method: 'PUT',
		body: JSON.stringify({ on })
	});

// --- grows & plants ---

export const getGrows = () => json<Grow[]>('/api/grows');
export const getGrow = (id: string) => json<GrowDetail>(`/api/grows/${encodeURIComponent(id)}`);
export const getPlant = (id: string) => json<PlantView>(`/api/plants/${encodeURIComponent(id)}`);

export interface GrowInput {
	name: string;
	/** A predefined crop family; the server derives the stage sequence from it. */
	species: string;
	startedAt: string; // YYYY-MM-DD or RFC3339
	notes: string;
}

export const createGrow = (g: GrowInput) =>
	json<Grow>('/api/grows', { method: 'POST', body: JSON.stringify(g) });

export const updateGrow = (id: string, g: GrowInput) =>
	json<Grow>(`/api/grows/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(g) });

export const deleteGrow = (id: string) =>
	req(`/api/grows/${encodeURIComponent(id)}`, { method: 'DELETE' });

export const changeStage = (id: string, stage: string) =>
	json<Grow>(`/api/grows/${encodeURIComponent(id)}/stage`, { method: 'POST', body: JSON.stringify({ stage }) });

export const completeGrow = (id: string) =>
	json<Grow>(`/api/grows/${encodeURIComponent(id)}/complete`, { method: 'POST' });

/** Create one plant record: an individual plant, or a group (tray/bed/batch)
 * whose `quantity` is how many plants it holds. Each call makes one unit with
 * its own id and history. */
export interface CreatePlantInput {
	tracking: TrackingMode;
	quantity?: number;
	label?: string;
	cultivar?: string;
	environmentId?: string;
	/** Optional starting pot (opens the plant's repot history). */
	potSize?: number;
	potUnit?: PotUnit;
	potType?: string;
}

export const createPlant = (growID: string, b: CreatePlantInput) =>
	json<PlantUnit>(`/api/grows/${encodeURIComponent(growID)}/plants`, {
		method: 'POST',
		body: JSON.stringify(b)
	});

export interface UpdatePlantInput {
	label: string;
	cultivar: string;
	tracking?: TrackingMode;
	quantity?: number;
}

export const updatePlant = (plantID: string, b: UpdatePlantInput) =>
	json<PlantUnit>(`/api/plants/${encodeURIComponent(plantID)}`, {
		method: 'PUT',
		body: JSON.stringify(b)
	});

export const movePlant = (plantID: string, environmentId: string) =>
	json<{ status: string }>(`/api/plants/${encodeURIComponent(plantID)}/move`, {
		method: 'POST',
		body: JSON.stringify({ environmentId })
	});

/** Record a repot: closes the current pot and opens a new one (keeps history). */
export const repotPlant = (plantID: string, pot: { size: number; unit: PotUnit; type?: string }) =>
	json<{ status: string }>(`/api/plants/${encodeURIComponent(plantID)}/repot`, {
		method: 'POST',
		body: JSON.stringify(pot)
	});

export const harvestPlant = (plantID: string) =>
	json<PlantUnit>(`/api/plants/${encodeURIComponent(plantID)}/harvest`, { method: 'POST' });

export const removePlant = (plantID: string) =>
	json<PlantUnit>(`/api/plants/${encodeURIComponent(plantID)}/remove`, { method: 'POST' });

export const getEnvironmentPlants = (envID: string) =>
	json<EnvPlantsGroup[]>(`/api/environments/${encodeURIComponent(envID)}/plants`);

// --- care (the grow's manual-action journal) ---

/** Care events across every grow in a [from, to] window (YYYY-MM-DD), for the
 *  calendar. Bounds are optional; the server defaults to a window around now. */
export const getCalendar = (from?: string, to?: string) => {
	const q = new URLSearchParams();
	if (from) q.set('from', from);
	if (to) q.set('to', to);
	const qs = q.toString();
	return json<CalendarResponse>(`/api/calendar${qs ? `?${qs}` : ''}`);
};

/** A grow's care history plus a summary (last action per type, skipped plants). */
export const getCare = (growID: string) =>
	json<CareHistory>(`/api/grows/${encodeURIComponent(growID)}/care`);

/** Log one care action against a grow's plants. */
export const logCare = (growID: string, b: LogCareInput) =>
	json<CareEvent>(`/api/grows/${encodeURIComponent(growID)}/care`, {
		method: 'POST',
		body: JSON.stringify(b)
	});

export const deleteCare = (id: string) =>
	req(`/api/care/${encodeURIComponent(id)}`, { method: 'DELETE' });

/** A grow's effective care actions (species defaults overlaid with per-grow config). */
export const getCareConfig = (growID: string) =>
	json<{ actions: CareActionDef[] }>(`/api/grows/${encodeURIComponent(growID)}/care-config`);

/** Replace a grow's care customization (order, enable/disable, rename, quick, custom). */
export const saveCareConfig = (growID: string, actions: GrowCareActionConfig[]) =>
	json<{ actions: CareActionDef[] }>(`/api/grows/${encodeURIComponent(growID)}/care-config`, {
		method: 'PUT',
		body: JSON.stringify({ actions })
	});

// --- optional-context GrowRig assistant ---
export interface GrowAIStatus { available: boolean; instanceName?: string }
export interface GrowAIMessage { id?: string; role: 'user' | 'assistant'; content: string; createdAt?: string }
export interface AIChat {
	id: string;
	growId: string;
	growName: string;
	environmentId: string;
	environmentName: string;
	title: string;
	instanceName: string;
	archived: boolean;
	messageCount: number;
	preview: string;
	messages?: GrowAIMessage[];
	createdAt: string;
	updatedAt: string;
}
export interface GrowAIReply { chat: AIChat; message: GrowAIMessage; instanceName: string }
export const getAIStatus = () => json<GrowAIStatus>('/api/ai/status');
export const getGrowAIStatus = (growID: string) =>
	json<GrowAIStatus>(`/api/grows/${encodeURIComponent(growID)}/ai`);
export const chatWithGrowAI = (chatID: string, content: string, growID = '', environmentID = '') =>
	json<GrowAIReply>('/api/ai/chat', {
		method: 'POST',
		body: JSON.stringify({ chatId: chatID, content, growId: growID, environmentId: environmentID })
	});
export const getAIChats = (archived?: boolean) =>
	json<AIChat[]>(`/api/ai/chats${archived == null ? '' : `?archived=${archived}`}`);
export const getAIChat = (id: string) =>
	json<AIChat>(`/api/ai/chats/${encodeURIComponent(id)}`);
export const setAIChatArchived = (id: string, archived: boolean) =>
	json<AIChat>(`/api/ai/chats/${encodeURIComponent(id)}`, {
		method: 'PUT',
		body: JSON.stringify({ archived })
	});

// --- species catalog & cultivars ---

export const getSpecies = () => json<Species[]>('/api/species');

/** Cultivars, optionally filtered to a single species. */
export const getCultivars = (species?: string) =>
	json<Cultivar[]>(`/api/cultivars${species ? `?species=${encodeURIComponent(species)}` : ''}`);

export const getCultivar = (id: string) => json<Cultivar>(`/api/cultivars/${encodeURIComponent(id)}`);

export interface CultivarInput {
	species: string;
	name: string;
	creator: string;
	description: string;
	attributes: Record<string, string>;
	/** Optional data URL to set/replace the image; omit to leave unchanged. */
	image?: string;
	/** Set on update to clear an existing image. */
	removeImage?: boolean;
}

export const createCultivar = (c: CultivarInput) =>
	json<Cultivar>('/api/cultivars', { method: 'POST', body: JSON.stringify(c) });

export const updateCultivar = (id: string, c: CultivarInput) =>
	json<Cultivar>(`/api/cultivars/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(c) });

export const deleteCultivar = (id: string) =>
	req(`/api/cultivars/${encodeURIComponent(id)}`, { method: 'DELETE' });

// --- Inventory catalog & items ---

export const getInventoryCategories = () =>
	json<InventoryCategory[]>('/api/inventory/categories');

/** Built-in product templates, optionally filtered to a single category. */
export const getInventoryProducts = (category?: string) =>
	json<InventoryProduct[]>(`/api/inventory/products${category ? `?category=${encodeURIComponent(category)}` : ''}`);

/** Image of a built-in product. `productId` is fully-qualified "<category>/<id>". */
export const inventoryProductImageURL = (productId: string): string =>
	authenticatedMediaURL(`/api/inventory/products/${productId.split('/').map(encodeURIComponent).join('/')}/image`);

/** Image uploaded for a user's inventory item. */
export const inventoryItemImageURL = (id: string): string =>
	authenticatedMediaURL(`/api/inventory/items/${encodeURIComponent(id)}/image`);

/** Inventory items, optionally filtered to a single category. */
export const getInventoryItems = (category?: string) =>
	json<InventoryItem[]>(`/api/inventory/items${category ? `?category=${encodeURIComponent(category)}` : ''}`);

export const getInventoryItem = (id: string) =>
	json<InventoryItem>(`/api/inventory/items/${encodeURIComponent(id)}`);

export interface InventoryItemInput {
	category: string;
	name: string;
	variants: InventoryStockLine[];
	location: string;
	status: InventoryStatus;
	notes: string;
	attributes: Record<string, string>;
	/** Bind to a built-in product template ("<category>/<id>"); '' to unbind. */
	productId?: string;
	/** Optional data URL to set/replace the image; omit to leave unchanged. */
	image?: string;
	/** Set on update to clear an existing image. */
	removeImage?: boolean;
}

export const createInventoryItem = (b: InventoryItemInput) =>
	json<InventoryItem>('/api/inventory/items', { method: 'POST', body: JSON.stringify(b) });

export const updateInventoryItem = (id: string, b: InventoryItemInput) =>
	json<InventoryItem>(`/api/inventory/items/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(b) });

export const deleteInventoryItem = (id: string) =>
	req(`/api/inventory/items/${encodeURIComponent(id)}`, { method: 'DELETE' });

// --- Feeding recipes ---

/** The user's own feeding recipes (stored as YAML on disk). */
export const getRecipes = () => json<FeedingRecipe[]>('/api/recipes');

/** Built-in presets, offered only as templates to seed a new preset. */
export const getRecipeTemplates = (species?: string) =>
	json<FeedingRecipe[]>(`/api/recipe-templates${species ? `?species=${encodeURIComponent(species)}` : ''}`);

export const getRecipe = (id: string) =>
	json<FeedingRecipe>(`/api/recipes/${encodeURIComponent(id)}`);

export interface RecipeInput {
	species: string;
	name: string;
	brand: string;
	description: string;
	unit: string;
	products: FeedingProduct[];
	phases: FeedingPhase[];
}

export const createRecipe = (p: RecipeInput) =>
	json<FeedingRecipe>('/api/recipes', { method: 'POST', body: JSON.stringify(p) });

export const updateRecipe = (id: string, p: RecipeInput) =>
	json<FeedingRecipe>(`/api/recipes/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(p) });

export const deleteRecipe = (id: string) =>
	req(`/api/recipes/${encodeURIComponent(id)}`, { method: 'DELETE' });

/** Authenticated same-origin URL for a cultivar's stored image. */
export const cultivarImageURL = (id: string): string =>
	authenticatedMediaURL(`/api/cultivars/${encodeURIComponent(id)}/image`);

export const setControlGrow = (envID: string, growId: string) =>
	json<Environment>(`/api/environments/${encodeURIComponent(envID)}/control-grow`, {
		method: 'PUT',
		body: JSON.stringify({ growId })
	});

// --- light schedule (photoperiod automation) ---

export interface ScheduleInput {
	mode: LightScheduleMode;
	lightsOnAt: string;
	onHours: number;
	stageOnHours: Record<string, number>;
}

export const getSchedule = (envID: string) =>
	json<LightSchedule>(`/api/environments/${encodeURIComponent(envID)}/schedule`);

export const setSchedule = (envID: string, s: ScheduleInput) =>
	json<LightSchedule>(`/api/environments/${encodeURIComponent(envID)}/schedule`, {
		method: 'PUT',
		body: JSON.stringify(s)
	});

export const getLightingDefaults = () => json<StageLightDefaults>('/api/lighting/defaults');

// --- history ---

export const history = (envID: string, limit = 120) =>
	json<Reading[]>(`/api/environments/${encodeURIComponent(envID)}/history?limit=${limit}`);

/** Downsampled readings over the last `hours`, averaged into ~`buckets` points. */
export const historyRange = (envID: string, hours = 72, buckets = 500) =>
	json<Reading[]>(
		`/api/environments/${encodeURIComponent(envID)}/history?hours=${hours}&buckets=${buckets}`
	);

/** Downsampled per-device series (fan rpm, light power) over the last `hours`. */
export const deviceHistory = (envID: string, hours = 72, buckets = 500) =>
	json<DeviceSeries[]>(
		`/api/environments/${encodeURIComponent(envID)}/device-history?hours=${hours}&buckets=${buckets}`
	);

/** Downsampled per-sensor series (each bound sensor's own readings) over `hours`. */
export const sensorHistory = (envID: string, hours = 72, buckets = 500) =>
	json<SensorSeries[]>(
		`/api/environments/${encodeURIComponent(envID)}/sensor-history?hours=${hours}&buckets=${buckets}`
	);

/** Persisted outdoor history for the env's resolved location, over `hours`. */
export const weatherHistory = (envID: string, hours = 72, buckets = 500) =>
	json<WeatherHistory>(
		`/api/environments/${encodeURIComponent(envID)}/weather-history?hours=${hours}&buckets=${buckets}`
	);

export interface ActivityPage {
	items: Activity[];
	total: number;
}

/** Clear the entire activity log (admin). */
export const clearActivity = () => req('/api/activity', { method: 'DELETE' });

/** Trigger a graceful restart of Grow Core (admin). Under a process supervisor
 * the service comes back up on its own. */
export const restartCore = () => req('/api/admin/restart', { method: 'POST' });

export interface DatabaseTable { name: string; rows: number; sizeBytes: number }
export const getDatabaseTables = () => json<DatabaseTable[]>('/api/admin/database/tables');

export const getActivity = async (
	opts: { environmentId?: string; growId?: string; levels?: string[]; types?: string[]; limit?: number; offset?: number } = {}
): Promise<ActivityPage> => {
	const params = new URLSearchParams({ limit: String(opts.limit ?? 100) });
	if (opts.environmentId) params.set('environmentId', opts.environmentId);
	if (opts.growId) params.set('growId', opts.growId);
	if (opts.levels?.length) params.set('levels', opts.levels.join(','));
	if (opts.types?.length) params.set('types', opts.types.join(','));
	if (opts.offset) params.set('offset', String(opts.offset));
	// Tolerate the pre-pagination response (a bare array) so the UI doesn't break
	// during a rolling deploy where the backend hasn't been restarted yet.
	const res = await json<ActivityPage | Activity[]>(`/api/activity?${params}`);
	return Array.isArray(res) ? { items: res, total: res.length } : res;
};

// --- auth ---

export const getAuthStatus = () => json<AuthStatus>('/api/auth/status');

export const login = (username: string, password: string) =>
	json<AuthResult>('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) });

export const bootstrap = (username: string, password: string) =>
	json<AuthResult>('/api/auth/bootstrap', { method: 'POST', body: JSON.stringify({ username, password }) });

export const register = (username: string, password: string) =>
	json<AuthResult>('/api/auth/register', { method: 'POST', body: JSON.stringify({ username, password }) });

export const logout = () => req('/api/auth/logout', { method: 'POST' });

export const getMe = () => json<User>('/api/auth/me');

// --- user management (admin) ---

export interface UserInput {
	username: string;
	password: string;
	role: UserRole;
	access: EnvAccess[];
}

export interface UserUpdate {
	role?: UserRole;
	disabled?: boolean;
	password?: string;
	access?: EnvAccess[];
}

export const getUsers = () => json<User[]>('/api/users');

export const createUser = (u: UserInput) =>
	json<User>('/api/users', { method: 'POST', body: JSON.stringify(u) });

export const updateUser = (id: string, u: UserUpdate) =>
	json<User>(`/api/users/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(u) });

export const deleteUser = (id: string) =>
	req(`/api/users/${encodeURIComponent(id)}`, { method: 'DELETE' });

export const getSignupSetting = () => json<{ enabled: boolean }>('/api/settings/signup');

export const setSignupSetting = (enabled: boolean) =>
	json<{ enabled: boolean }>('/api/settings/signup', { method: 'PUT', body: JSON.stringify({ enabled }) });

// --- passkeys (WebAuthn) ---
// Ceremony options are opaque WebAuthn JSON (a `publicKey` object plus a
// server-issued `handle` echoed back on finish). The credential responses are
// serialized by lib/webauthn.ts.

export interface Passkey {
	id: string;
	name: string;
	created: string;
}

interface CeremonyOptions {
	publicKey: Record<string, unknown>;
	handle: string;
}

export const passkeyRegisterBegin = () =>
	json<CeremonyOptions>('/api/auth/passkey/register/begin', { method: 'POST' });

export const passkeyRegisterFinish = (handle: string, name: string, credential: unknown) =>
	json<Passkey>(
		`/api/auth/passkey/register/finish?handle=${encodeURIComponent(handle)}&name=${encodeURIComponent(name)}`,
		{ method: 'POST', body: JSON.stringify(credential) }
	);

export const passkeyLoginBegin = () =>
	json<CeremonyOptions>('/api/auth/passkey/login/begin', { method: 'POST' });

export const passkeyLoginFinish = (handle: string, credential: unknown) =>
	json<AuthResult>(`/api/auth/passkey/login/finish?handle=${encodeURIComponent(handle)}`, {
		method: 'POST',
		body: JSON.stringify(credential)
	});

export const getPasskeys = () => json<Passkey[]>('/api/auth/passkeys');

export const deletePasskey = (id: string) =>
	req(`/api/auth/passkeys/${encodeURIComponent(id)}`, { method: 'DELETE' });

// --- Home Assistant control panel (admin) ---

export const getHomeAssistant = () => json<HAStatus>('/api/admin/homeassistant');

export const reloadHomeAssistant = () =>
	req('/api/admin/homeassistant/reload', { method: 'POST' });

export const updateHomeAssistant = (target: HAUpdateTarget, slug?: string) =>
	req('/api/admin/homeassistant/update', {
		method: 'POST',
		body: JSON.stringify({ target, slug: slug ?? '' })
	});

// --- integrations ---
export const getIntegrationBundles = () => json<IntegrationBundle[]>('/api/integration-bundles');
export const getIntegrationInstances = () => json<IntegrationInstance[]>('/api/integration-instances');
export interface IntegrationInstanceInput { bundleId?: string; name?: string; config?: Record<string, string>; enabled?: boolean }
export const createIntegrationInstance = (input: IntegrationInstanceInput) =>
	json<IntegrationInstance>('/api/integration-instances', { method: 'POST', body: JSON.stringify(input) });
export const updateIntegrationInstance = (id: string, input: IntegrationInstanceInput) =>
	json<IntegrationInstance>(`/api/integration-instances/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(input) });
export const deleteIntegrationInstance = (id: string) =>
	req(`/api/integration-instances/${encodeURIComponent(id)}`, { method: 'DELETE' });
export const testIntegrationInstance = (id: string) =>
	json<IntegrationInstance>(`/api/integration-instances/${encodeURIComponent(id)}/test`, { method: 'POST' });
export const getIntegrationBindings = () => json<IntegrationBinding[]>('/api/integration-bindings');
export const saveIntegrationBinding = (input: Pick<IntegrationBinding, 'feature' | 'capability' | 'instanceId'> & { growId?: string; environmentId?: string }) =>
	json<IntegrationBinding>('/api/integration-bindings', { method: 'POST', body: JSON.stringify(input) });
export const deleteIntegrationBinding = (id: string) =>
	req(`/api/integration-bindings/${encodeURIComponent(id)}`, { method: 'DELETE' });
