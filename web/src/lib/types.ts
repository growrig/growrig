// Mirrors the JSON emitted by Grow Core (growcore/internal/domain).

export type Role = 'unassigned' | 'exhaust' | 'intake' | 'circulation';

export type Health = 'online' | 'stale' | 'offline';

export interface Environment {
	id: string;
	name: string;
	targetTempC: number;
	targetHumidity: number;
	emergencyTempC: number;
}

export interface Channel {
	id: string;
	name: string;
	role: Role;
	entity: string;
	rpmEntity: string;
	desiredSpeed: number;
	rpm: number;
}

export interface Device {
	id: string;
	name: string;
	environmentId: string;
	adapter: string;
	tempEntity: string;
	humidityEntity: string;
	health: Health;
	channels: Channel[];
	tempC: number;
	humidity: number;
	lastSeen: string;
}

export interface Info {
	adapter: string;
}

export interface Snapshot {
	time: string;
	environments: Environment[];
	devices: Device[];
}

export interface Reading {
	environmentId: string;
	time: string;
	tempC: number;
	humidity: number;
	exhaustSpeed: number;
}
