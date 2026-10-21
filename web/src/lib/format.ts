import type { Measurement, SeriesPoint, TrackingMode } from './types';

export type Tone = 'good' | 'warn' | 'danger' | 'muted';

export const measurementUnit: Record<Measurement, string> = {
	temperature: '°C',
	humidity: '%',
	co2: 'ppm',
	power: ' W'
};

export const measurementLabel: Record<Measurement, string> = {
	temperature: 'Temperature',
	humidity: 'Humidity',
	co2: 'CO₂',
	power: 'Power'
};

export function defaultPlantLabel(tracking: TrackingMode = 'individual'): string {
	return tracking === 'group' ? 'Group' : 'Plant';
}

/** Grouping key for per-grow plant numbering (cultivar, else custom label, else default). */
export function plantNumberingKey(p: {
	cultivar?: string;
	label?: string;
	tracking?: TrackingMode;
}): string {
	if (p.cultivar?.trim()) return p.cultivar.trim();
	const label = p.label?.trim();
	if (label && label !== defaultPlantLabel(p.tracking)) return label;
	return defaultPlantLabel(p.tracking);
}

/** Assigns #1, #2, … within each numbering group, ordered by creation time. */
export function plantNumbersById<
	T extends { id: string; createdAt: string; cultivar?: string; label?: string; tracking?: TrackingMode }
>(plants: T[]): Map<string, number> {
	const sorted = [...plants].sort(
		(a, b) => a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id)
	);
	const counts = new Map<string, number>();
	const out = new Map<string, number>();
	for (const p of sorted) {
		const key = plantNumberingKey(p);
		const n = (counts.get(key) ?? 0) + 1;
		counts.set(key, n);
		out.set(p.id, n);
	}
	return out;
}

/** Name shown in lists and headings: cultivar, then custom label, then the default. */
export function plantDisplayName(
	p: {
		cultivar?: string;
		label?: string;
		tracking?: TrackingMode;
	},
	number?: number
): string {
	let base: string;
	if (p.cultivar?.trim()) base = p.cultivar.trim();
	else if (p.label?.trim()) base = p.label.trim();
	else base = defaultPlantLabel(p.tracking);
	return number != null ? `${base} #${number}` : base;
}

export function formatValue(measurement: Measurement, value: number): string {
	if (measurement === 'co2') return Math.round(value).toString();
	if (measurement === 'humidity') return value.toFixed(0);
	if (measurement === 'power') return Math.round(value).toString();
	return value.toFixed(1);
}

// VPD growth zones (kPa), a widely used rule of thumb.
export function vpdZone(vpd: number): { label: string; tone: Tone } {
	if (vpd < 0.4) return { label: 'Too humid', tone: 'danger' };
	if (vpd < 0.8) return { label: 'Propagation', tone: 'warn' };
	if (vpd < 1.2) return { label: 'Vegetative', tone: 'good' };
	if (vpd < 1.6) return { label: 'Flowering', tone: 'good' };
	return { label: 'Too dry', tone: 'danger' };
}

export const toneClass: Record<Tone, string> = {
	good: 'text-leaf',
	warn: 'text-warn',
	danger: 'text-danger',
	muted: 'text-rig-400'
};

// Tent air volume in m³ from centimetre dimensions; 0 if any dimension is unset.
export function volumeM3(widthCm: number, depthCm: number, heightCm: number): number {
	if (widthCm <= 0 || depthCm <= 0 || heightCm <= 0) return 0;
	return (widthCm * depthCm * heightCm) / 1_000_000;
}

// "120 × 120 × 200 cm" from centimetre dimensions, or '' if any is unset.
export function formatDimensions(widthCm: number, depthCm: number, heightCm: number): string {
	if (widthCm <= 0 || depthCm <= 0 || heightCm <= 0) return '';
	return `${widthCm} × ${depthCm} × ${heightCm} cm`;
}

// Value of the hourly series point nearest to now (the Open-Meteo weather
// feed spans past readings plus forecast); undefined when the series is empty.
export function valueNow(points: SeriesPoint[] | undefined): number | undefined {
	if (!points?.length) return undefined;
	const now = Date.now();
	let best = points[0];
	let bestDelta = Infinity;
	for (const p of points) {
		const delta = Math.abs(new Date(p.time).getTime() - now);
		if (delta < bestDelta) {
			bestDelta = delta;
			best = p;
		}
	}
	return best.value;
}

export function climateTone(tempC: number, target: number, emergency: number): Tone {
	if (emergency > 0 && tempC >= emergency) return 'danger';
	if (tempC - target >= 2) return 'warn';
	return 'good';
}

// Whole days between an ISO timestamp and now, floored at 0.
export function daysSince(iso?: string): number {
	if (!iso) return 0;
	return Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 86_400_000));
}

// A short relative duration for a millisecond span, e.g. "2h 5m" or "now".
export function relTime(ms: number): string {
	const min = Math.max(0, Math.round(ms / 60_000));
	const h = Math.floor(min / 60);
	const m = min % 60;
	if (min < 1) return 'now';
	if (h === 0) return `${m}m`;
	if (m === 0) return `${h}h`;
	return `${h}h ${m}m`;
}

// Title-case a stage/species token for display.
export function titleCase(s: string): string {
	return s ? s[0].toUpperCase() + s.slice(1) : s;
}

/** WebSocket round-trip latency; sub-1 ms values show one decimal place. */
export function fmtLatencyMs(ms: number): string {
	return ms < 1 ? ms.toFixed(1) : String(Math.round(ms));
}

/** Disk usage in binary units, e.g. "869 MB" or "1.4 GB". */
export function fmtBytes(bytes: number): string {
	if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`;
	if (bytes >= 1 << 20) return `${Math.round(bytes / (1 << 20))} MB`;
	if (bytes >= 1 << 10) return `${Math.round(bytes / (1 << 10))} KB`;
	return `${bytes} B`;
}
