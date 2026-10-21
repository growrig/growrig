// Shared mapping from a device Binding + the live environment snapshot to a
// human label and current-status line. Used by the Equipment tab (device
// boxes) and the equipment detail modal so both stay in sync.
import type { Binding, EnvironmentView } from '$lib/types';
import { measurementUnit } from '$lib/format';

export interface DeviceGroup {
	id: string;
	name: string;
	bindings: Binding[];
}

/** Group an environment's bindings into physical devices, preserving order. */
export function groupDevices(bindings: Binding[]): DeviceGroup[] {
	const grouped = new Map<string, DeviceGroup>();
	for (const b of bindings) {
		const device = grouped.get(b.deviceId) ?? { id: b.deviceId, name: b.deviceName, bindings: [] };
		device.bindings.push(b);
		grouped.set(b.deviceId, device);
	}
	return [...grouped.values()];
}

/** Short capability label for one binding. */
export function bindingMeta(b: Binding): string {
	if (b.kind === 'sensor') return b.measurement ?? 'sensor';
	if (b.kind === 'fan') return b.role ?? 'fan';
	if (b.kind === 'light') return b.wattage ? `${b.wattage} W` : 'light';
	if (b.kind === 'power') return 'switch';
	if (b.kind === 'controller') return b.name;
	if (b.kind === 'irrigation') return `${b.irrigationType ?? 'irrigation'} · ${b.irrigationMode ?? 'passive'}`;
	return b.kind;
}

/** Latest value/state + reachability for one binding, from the live snapshot. */
export function bindingStatus(
	b: Binding,
	env: EnvironmentView | undefined
): { value: string; online: boolean | null } {
	const online = env ? (env.health === 'online' ? true : false) : null;

	if (b.kind === 'sensor') {
		const s = env?.sensors?.find((x) => x.id === b.id);
		if (!s) return { value: '—', online: null };
		const unit = b.measurement ? measurementUnit[b.measurement] : '';
		return { value: s.ok ? `${s.value}${unit}` : '—', online: s.ok };
	}
	if (b.kind === 'fan') {
		const c = env?.controls?.find((x) => x.id === b.id);
		if (!c) return { value: '—', online: env ? env.kind === 'tent' : null };
		return { value: `${c.desiredSpeed}%${c.rpm ? ` · ${c.rpm} rpm` : ''}`, online };
	}
	if (b.kind === 'light') {
		if (!b.powerControllerId) return { value: 'Unassigned', online: null };
		const c = env?.controls?.find((x) => x.id === b.id);
		return { value: c ? (c.on ? 'On' : 'Off') : '—', online };
	}
	if (b.kind === 'power') {
		return { value: '', online };
	}
	if (b.kind === 'controller') return { value: b.rpmEntity ? 'RPM connected' : 'No RPM', online };
	if (b.kind === 'irrigation') {
		const parts = [];
		if (b.reservoirL) parts.push(`${b.reservoirL} L`);
		if (b.valveCount) parts.push(`${b.valveCount} valve${b.valveCount === 1 ? '' : 's'}`);
		// Passive setups have no live telemetry, so no online state.
		return { value: parts.join(' · ') || 'Passive', online: b.irrigationMode === 'controlled' ? online : null };
	}
	return { value: '', online }; // camera
}
