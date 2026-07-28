// Visual mapping for care action types, shared by the calendar (and available to
// any other cross-grow view). Icons mirror the species care-action icons; the
// label is a sensible default for the built-in actions, falling back to a
// title-cased key for custom actions.
import type { LucideIcon } from '@lucide/svelte';
import Droplet from '@lucide/svelte/icons/droplet';
import FlaskConical from '@lucide/svelte/icons/flask-conical';
import Search from '@lucide/svelte/icons/search';
import Spline from '@lucide/svelte/icons/spline';
import Scissors from '@lucide/svelte/icons/scissors';
import Shovel from '@lucide/svelte/icons/shovel';
import SprayCan from '@lucide/svelte/icons/spray-can';
import Waves from '@lucide/svelte/icons/waves';
import Sprout from '@lucide/svelte/icons/sprout';
import Layers from '@lucide/svelte/icons/layers';
import ListPlus from '@lucide/svelte/icons/list-plus';

interface CareVisual {
	icon: LucideIcon;
	label: string;
}

const VISUALS: Record<string, CareVisual> = {
	water: { icon: Droplet, label: 'Water' },
	feed: { icon: FlaskConical, label: 'Feed' },
	inspect: { icon: Search, label: 'Inspect' },
	train: { icon: Spline, label: 'Train' },
	trim: { icon: Scissors, label: 'Trim' },
	prune: { icon: Scissors, label: 'Prune' },
	transplant: { icon: Shovel, label: 'Transplant' },
	amend: { icon: Layers, label: 'Amend' },
	treat: { icon: SprayCan, label: 'Treat' },
	flush: { icon: Waves, label: 'Flush' },
	harvest: { icon: Sprout, label: 'Harvest' }
};

const titleCase = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

/** Icon + label for a care action type key. */
export function careVisual(type: string): CareVisual {
	return VISUALS[type] ?? { icon: ListPlus, label: titleCase(type) };
}

/** A compact "just now / 5h ago / 3d ago" from an ISO timestamp (past only). */
export function ago(iso: string): string {
	const ms = Date.now() - new Date(iso).getTime();
	if (ms < 60_000) return 'just now';
	const min = Math.floor(ms / 60_000);
	if (min < 60) return `${min}m ago`;
	const h = Math.floor(min / 60);
	if (h < 24) return `${h}h ago`;
	return `${Math.floor(h / 24)}d ago`;
}

/** Millilitres as a compact "900 ml" / "3.2 L" string. */
export function fmtVolume(ml: number): string {
	return ml >= 1000 ? `${(ml / 1000).toFixed(1)} L` : `${Math.round(ml)} ml`;
}

// A small, colour-blind-friendly palette for distinguishing grows on the
// calendar. Grows are assigned a colour by their position in the active list so
// the same grow keeps its colour across day cells and the legend.
export const GROW_COLORS = [
	{ dot: 'bg-emerald-400', text: 'text-emerald-300', border: 'border-l-emerald-400' },
	{ dot: 'bg-sky-400', text: 'text-sky-300', border: 'border-l-sky-400' },
	{ dot: 'bg-violet-400', text: 'text-violet-300', border: 'border-l-violet-400' },
	{ dot: 'bg-amber-400', text: 'text-amber-300', border: 'border-l-amber-400' },
	{ dot: 'bg-rose-400', text: 'text-rose-300', border: 'border-l-rose-400' },
	{ dot: 'bg-teal-400', text: 'text-teal-300', border: 'border-l-teal-400' },
	{ dot: 'bg-fuchsia-400', text: 'text-fuchsia-300', border: 'border-l-fuchsia-400' },
	{ dot: 'bg-lime-400', text: 'text-lime-300', border: 'border-l-lime-400' }
] as const;

export type GrowColor = (typeof GROW_COLORS)[number];
