// REST client for Grow Core. When the web app is served by Grow Core itself
// (embedded, single binary) the base is same-origin. For local development
// against a separately-running Grow Core, set VITE_GROWCORE_URL.
import type {
	Binding,
	Activity,
	AuthResult,
	AuthStatus,
	BindingKind,
	CatalogProduct,
	Cycle,
	DeviceSeries,
	DiscoveredEntity,
	Environment,
	EnvironmentKind,
	EnvAccess,
	FanType,
	GeocodeResult,
	HAStatus,
	HAUpdateTarget,
	Info,
	Location,
	User,
	UserRole,
	Weather,
	LightSchedule,
	LightScheduleMode,
	Measurement,
	Phase,
	PhotoperiodDefaults,
	Reading,
	Role,
	SensorSeries,
	Snapshot,
	WeatherHistory
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
/** Current live snapshot over REST — used for the initial paint before the
 *  WebSocket feed takes over. */
export const getState = () => json<Snapshot>('/api/state');
export const getDiscovery = () => json<DiscoveredEntity[]>('/api/discovery');
export const getCatalog = () => json<CatalogProduct[]>('/api/catalog');
export const getPhases = () => json<Phase[]>('/api/phases');
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

// --- cycles ---

export interface CycleInput {
	strain: string;
	startedAt: string; // YYYY-MM-DD or RFC3339
	phase: Phase;
	notes: string;
}

export const setCycle = (envID: string, c: CycleInput) =>
	json<Cycle>(`/api/environments/${encodeURIComponent(envID)}/cycle`, {
		method: 'PUT',
		body: JSON.stringify(c)
	});

export const clearCycle = (envID: string) =>
	req(`/api/environments/${encodeURIComponent(envID)}/cycle`, { method: 'DELETE' });

// --- light schedule (photoperiod automation) ---

export interface ScheduleInput {
	mode: LightScheduleMode;
	lightsOnAt: string;
	onHours: number;
	phaseOnHours: Partial<Record<Phase, number>>;
}

export const getSchedule = (envID: string) =>
	json<LightSchedule>(`/api/environments/${encodeURIComponent(envID)}/schedule`);

export const setSchedule = (envID: string, s: ScheduleInput) =>
	json<LightSchedule>(`/api/environments/${encodeURIComponent(envID)}/schedule`, {
		method: 'PUT',
		body: JSON.stringify(s)
	});

export const getLightingDefaults = () => json<PhotoperiodDefaults>('/api/lighting/defaults');

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

export const getActivity = (environmentId?: string, limit = 100) => {
	const params = new URLSearchParams({ limit: String(limit) });
	if (environmentId) params.set('environmentId', environmentId);
	return json<Activity[]>(`/api/activity?${params}`);
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
