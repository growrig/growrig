// REST client for Grow Core. When the web app is served by Grow Core itself
// (embedded, single binary) the base is same-origin. For local development
// against a separately-running Grow Core, set VITE_GROWCORE_URL.
import type { Device, Environment, Info, Reading, Role } from './types';

export const CORE_URL: string = import.meta.env.VITE_GROWCORE_URL?.replace(/\/$/, '') ?? '';

export function wsURL(): string {
	const base = CORE_URL || window.location.origin;
	const u = new URL(base);
	u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
	u.pathname = '/api/ws';
	return u.toString();
}

async function req(path: string, init?: RequestInit): Promise<Response> {
	const res = await fetch(`${CORE_URL}${path}`, {
		headers: { 'Content-Type': 'application/json' },
		...init
	});
	if (!res.ok) {
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

// --- info ---

export const getInfo = () => json<Info>('/api/info');

// --- environments ---

export const getEnvironments = () => json<Environment[]>('/api/environments');

export interface EnvironmentInput {
	name: string;
	targetTempC: number;
	targetHumidity: number;
	emergencyTempC: number;
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

export async function setTargets(
	envID: string,
	targetTempC: number,
	targetHumidity: number
): Promise<void> {
	await req(`/api/environments/${encodeURIComponent(envID)}/targets`, {
		method: 'PUT',
		body: JSON.stringify({ targetTempC, targetHumidity })
	});
}

// --- devices ---

export const getDevices = () => json<Device[]>('/api/devices');

export interface ChannelInput {
	id?: string;
	name: string;
	role: Role;
	entity: string;
	rpmEntity: string;
}

export interface DeviceInput {
	name: string;
	environmentId: string;
	tempEntity: string;
	humidityEntity: string;
	channels: ChannelInput[];
}

export const createDevice = (dev: DeviceInput) =>
	json<Device>('/api/devices', { method: 'POST', body: JSON.stringify(dev) });

export const updateDevice = (id: string, dev: DeviceInput) =>
	json<Device>(`/api/devices/${encodeURIComponent(id)}`, {
		method: 'PUT',
		body: JSON.stringify(dev)
	});

export const deleteDevice = (id: string) =>
	req(`/api/devices/${encodeURIComponent(id)}`, { method: 'DELETE' });

export async function setChannelRole(
	deviceID: string,
	channelID: string,
	role: Role
): Promise<void> {
	await req(
		`/api/devices/${encodeURIComponent(deviceID)}/channels/${encodeURIComponent(channelID)}/role`,
		{ method: 'PUT', body: JSON.stringify({ role }) }
	);
}

// --- history ---

export async function history(envID: string, limit = 120): Promise<Reading[]> {
	return json<Reading[]>(`/api/environments/${encodeURIComponent(envID)}/history?limit=${limit}`);
}
