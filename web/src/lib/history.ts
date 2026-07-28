// Rendering for a collection's History — turning the structured entries the
// server sends into the lines a person reads.
//
// The merge, the ordering, the paging and the noise filtering all happen in the
// database now (see GET /api/history): a client cannot page a merged stream
// correctly without holding every source in full, because the next-oldest entry
// could come from any of them.
//
// What stays here is wording. The API deliberately carries no rendered English,
// so clauses, pluralisation and link placement are decided in the UI — which is
// also what keeps the targets inside a sentence linkable, the thing
// Activity.Message can never do.
import type { Component } from 'svelte';
import type { CareEvent, HistoryEntry, HistoryKind, HistoryRef } from '$lib/types';
import { careVisual, fmtVolume } from '$lib/care';
import { titleCase } from '$lib/format';
import Camera from '@lucide/svelte/icons/camera';
import Milestone from '@lucide/svelte/icons/milestone';
import Cpu from '@lucide/svelte/icons/cpu';
import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
import MapPin from '@lucide/svelte/icons/map-pin';
import Shovel from '@lucide/svelte/icons/shovel';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type AnyIcon = Component<any>;

/** A clickable target inside a history line, e.g. the plant that was watered. */
export interface HistoryLink {
	label: string;
	href: string;
}

/**
 * One piece of a history line: plain text, or a link to the thing it names.
 * Lines are built as segments rather than a formatted string precisely so the
 * targets stay clickable — a pre-rendered sentence can't be linked afterwards.
 */
export type HistorySegment = string | HistoryLink;

export const isLink = (s: HistorySegment): s is HistoryLink => typeof s !== 'string';

/** An entry prepared for display. */
export interface HistoryItem {
	id: string;
	/** Epoch ms, for sorting and day grouping. */
	t: number;
	kind: HistoryKind;
	icon: AnyIcon;
	/** The headline, e.g. ["Watered ", {Cheese #1}, " and ", {Cheese #2}]. */
	title: HistorySegment[];
	/** Secondary summary line: volumes, pH, recipe. */
	detail?: string;
	/** A note the grower wrote; shown inline, not just in the detail modal. */
	note?: string;
	/** Photo to show inline as a thumbnail. */
	photoId?: string;
	automation?: boolean;
	/** The source entry, so the detail modal needs no refetch. */
	entry: HistoryEntry;
}

/** Past-tense verbs for the built-in care actions, falling back to the label. */
const VERBS: Record<string, string> = {
	water: 'Watered',
	feed: 'Fed',
	inspect: 'Inspected',
	train: 'Trained',
	trim: 'Trimmed',
	prune: 'Pruned',
	transplant: 'Transplanted',
	amend: 'Amended',
	treat: 'Treated',
	flush: 'Flushed',
	harvest: 'Harvested'
};

function careVerb(type: string): string {
	return VERBS[type] ?? titleCase(careVisual(type).label);
}

const hrefFor = (r: HistoryRef): string =>
	r.kind === 'plant'
		? `/plants/${r.id}`
		: r.kind === 'grow'
			? `/grows/${r.id}`
			: r.kind === 'environment'
				? `/env/${r.id}`
				: '';

/**
 * Name the referenced entities as links. Beyond `max` the tail collapses to
 * "+N more" — a 30-plant watering shouldn't produce a paragraph.
 */
function refSegments(refs: HistoryRef[], max = 3): HistorySegment[] {
	if (refs.length === 0) return [];
	const out: HistorySegment[] = [];
	const shown = refs.slice(0, max);
	shown.forEach((r, i) => {
		if (i > 0) out.push(i === shown.length - 1 && refs.length <= max ? ' and ' : ', ');
		out.push({ label: r.label || r.id, href: hrefFor(r) });
	});
	if (refs.length > max) out.push(` and ${refs.length - max} more`);
	return out;
}

/** Volumes, pH/EC, recipe and amendment doses — whatever the event carries. */
function careDetail(e: CareEvent, plantId?: string): string {
	const bits: string[] = [];
	// On a plant's page "200 ml" should mean what *that* plant got, not the
	// session total.
	const mine = plantId ? (e.applications ?? []).find((a) => a.plantUnitId === plantId) : undefined;
	const ml = mine
		? (mine.amountMl ?? 0)
		: (e.applications ?? []).reduce((n, a) => n + (a.amountMl ?? 0), 0);
	if (ml) bits.push(fmtVolume(ml));
	if (e.recipeName) bits.push(e.recipeName);
	if (e.dosesLabel) bits.push(`${e.dosesLabel} per plant`);
	if (e.ph) bits.push(`pH ${e.ph}`);
	if (e.ec) bits.push(`EC ${e.ec}`);
	if (!mine && e.runoffMl) bits.push(`${fmtVolume(e.runoffMl)} runoff`);
	return bits.join(' · ');
}

/**
 * Prepare one entry for display.
 *
 * `plantId`, when the history belongs to a plant, keeps lines from repeating
 * the page's own subject: "Watered Cheese #1" on Cheese #1's page says nothing.
 */
export function toItem(e: HistoryEntry, plantId?: string): HistoryItem {
	const base = { id: e.id, t: new Date(e.at).getTime(), kind: e.kind, automation: e.automation, entry: e };
	const refs = e.refs ?? [];

	switch (e.kind) {
		case 'care': {
			const care = e.care;
			if (!care) break;
			const title: HistorySegment[] = [careVerb(care.type)];
			if (!plantId && refs.length) {
				title.push(' ', ...refSegments(refs.filter((r) => r.kind === 'plant')));
			}
			const mine = plantId
				? (care.applications ?? []).find((a) => a.plantUnitId === plantId)
				: undefined;
			return {
				...base,
				icon: careVisual(care.type).icon,
				title,
				detail: careDetail(care, plantId) || undefined,
				note: mine?.note || care.notes || undefined
			};
		}
		case 'photo':
			return {
				...base,
				icon: Camera,
				title: ['Photo added'],
				note: e.photo?.caption || undefined,
				photoId: e.photo?.id
			};
		case 'milestone':
			return { ...base, icon: Milestone, title: [`Entered ${titleCase(e.stage ?? '')} stage`] };
		case 'placement': {
			const env = refs.find((r) => r.kind === 'environment');
			const plant = refs.find((r) => r.kind === 'plant');
			// Read from the plant's side or the environment's, depending on which
			// one the reader is looking at.
			const title: HistorySegment[] = plant
				? [{ label: plant.label || plant.id, href: hrefFor(plant) }, ' moved in']
				: env
					? ['Moved into ', { label: env.label || env.id, href: hrefFor(env) }]
					: ['Moved'];
			return { ...base, icon: MapPin, title, detail: e.placement?.position || undefined };
		}
		case 'pot': {
			const p = e.pot;
			return {
				...base,
				icon: Shovel,
				title: [p ? `Potted into ${p.size} ${p.unit}${p.type ? ` ${p.type}` : ''} pot` : 'Repotted']
			};
		}
		case 'system': {
			const a = e.activity;
			const title: HistorySegment[] = [a?.message ?? ''];
			// Activity messages are pre-rendered strings with no structured
			// references, so they can't be linked mid-sentence the way care can.
			// The entities the row does carry are offered as trailing links.
			for (const r of refs) {
				title.push(' · ', { label: r.label || r.kind, href: hrefFor(r) });
			}
			return {
				...base,
				icon: a?.level === 'warning' || a?.level === 'error' ? TriangleAlert : Cpu,
				title
			};
		}
	}
	return { ...base, icon: Cpu, title: [titleCase(e.kind)] };
}

/** Prepare a page of entries for display. */
export const toItems = (entries: HistoryEntry[], plantId?: string): HistoryItem[] =>
	entries.map((e) => toItem(e, plantId));

/** Group into [dayKey, items] pairs for day headers, preserving order. */
export function groupByDay(items: HistoryItem[]): [string, HistoryItem[]][] {
	const map = new Map<string, HistoryItem[]>();
	for (const it of items) {
		const key = new Date(it.t).toDateString();
		const bucket = map.get(key);
		if (bucket) bucket.push(it);
		else map.set(key, [it]);
	}
	return [...map.entries()];
}
