<script lang="ts">
	import { getContext } from 'svelte';
	import { line as d3line, curveMonotoneX } from 'd3-shape';
	import type { Interval } from '$lib/photoperiod';
	import { fmtTimelineDay, fmtTimelineHour } from '$lib/datetime';

	export interface SeriesDef {
		key: string;
		label: string;
		unit: string;
		color: string;
		points: { t: number; value: number }[];
		opacity?: number; // line opacity (weather overlays render fainter)
		dash?: boolean; // dashed line
		dashFrom?: number; // epoch ms: solid up to here, dashed after (forecast)
		scaleGroup?: string; // series sharing a group share one y-scale (e.g. in/out temp)
		zeroBaseline?: boolean; // pin the scale's floor to 0 (rpm, watts, ppm never go negative)
	}
	export interface HoverInfo {
		x: number; // px within the padded plot
		time: number;
		values: { color: string; label: string; unit: string; value: number }[];
		lit: boolean;
	}

	interface Props {
		series: SeriesDef[]; // enabled series only
		intervals: Interval[]; // light-ON windows
		showLight: boolean;
		now: number;
		onHover?: (info: HoverInfo | null) => void;
	}
	let { series, intervals, showLight, now, onHover }: Props = $props();

	// LayerCake context (Svelte stores) — gives us the time x-scale and pixel size.
	const { xScale, width, height } = getContext<any>('LayerCake');

	// One linear y-scale per scale-group: series sharing a group (e.g. indoor and
	// outdoor temperature) span a common domain so they read on the same scale.
	// Groups flagged zeroBaseline pin their floor to 0 (rpm/watts/ppm can't go
	// negative, so a low reading sits at the bottom rather than mid-chart).
	const scales = $derived.by(() => {
		const groups = new Map<string, { min: number; max: number; zero: boolean }>();
		for (const s of series) {
			const g = s.scaleGroup ?? s.key;
			let acc = groups.get(g);
			if (!acc) {
				acc = { min: Infinity, max: -Infinity, zero: false };
				groups.set(g, acc);
			}
			if (s.zeroBaseline) acc.zero = true;
			for (const p of s.points) {
				if (p.value == null || Number.isNaN(p.value)) continue;
				if (p.value < acc.min) acc.min = p.value;
				if (p.value > acc.max) acc.max = p.value;
			}
		}
		const h = $height;
		const fnByGroup = new Map<string, (v: number) => number>();
		for (const [g, acc] of groups) {
			let { min, max } = acc;
			if (!Number.isFinite(min)) {
				min = 0;
				max = 1;
			}
			if (acc.zero) min = 0;
			if (max - min < 1e-6) {
				max += 1;
				if (!acc.zero) min -= 1;
			}
			const span = max - min;
			max += span * 0.12;
			if (!acc.zero) min -= span * 0.12;
			fnByGroup.set(g, (v: number) => h - ((v - min) / (max - min)) * h);
		}
		const byKey = new Map<string, (v: number) => number>();
		for (const s of series) byKey.set(s.key, fnByGroup.get(s.scaleGroup ?? s.key)!);
		return byKey;
	});

	// Per-series max time gap we'll still read a value across when hovering; past
	// it the series has no sample here, so the tooltip shows "—" rather than a
	// misleadingly stale nearest value.
	const tolerances = $derived.by(() => {
		const out = new Map<string, number>();
		for (const s of series) {
			const ts = s.points.map((p) => p.t).sort((a, b) => a - b);
			if (ts.length < 2) {
				out.set(s.key, Infinity);
				continue;
			}
			const gaps: number[] = [];
			for (let i = 1; i < ts.length; i++) gaps.push(ts[i] - ts[i - 1]);
			gaps.sort((a, b) => a - b);
			out.set(s.key, gaps[Math.floor(gaps.length / 2)] * 1.5);
		}
		return out;
	});

	type DrawPath = { key: string; d: string; dash: boolean; color: string; opacity: number };

	// Linearly interpolate a point at `boundary` between two straddling samples,
	// so a series split there joins cleanly across the seam.
	function interpAt(a: { t: number; value: number }, b: { t: number; value: number }, boundary: number) {
		const f = (boundary - a.t) / (b.t - a.t);
		return { t: boundary, value: a.value + (b.value - a.value) * f };
	}

	const paths = $derived.by<DrawPath[]>(() => {
		const out: DrawPath[] = [];
		for (const s of series) {
			const y = scales.get(s.key)!;
			const gen = d3line<{ t: number; value: number }>()
				.defined((d) => d.value != null && !Number.isNaN(d.value))
				.x((d) => $xScale(d.t))
				.y((d) => y(d.value))
				.curve(curveMonotoneX);
			const opacity = s.opacity ?? 1;
			if (s.dashFrom != null) {
				// Solid up to the boundary (observed), dashed after (forecast).
				const b = s.dashFrom;
				const solid = s.points.filter((p) => p.t <= b);
				const dashed = s.points.filter((p) => p.t > b);
				const last = solid.at(-1);
				const first = dashed[0];
				if (last?.value != null && first?.value != null && !Number.isNaN(last.value) && !Number.isNaN(first.value)) {
					const mid = interpAt(last, first, b);
					solid.push(mid);
					dashed.unshift(mid);
				}
				out.push({ key: s.key + ':solid', d: gen(solid) ?? '', dash: false, color: s.color, opacity });
				out.push({ key: s.key + ':dash', d: gen(dashed) ?? '', dash: true, color: s.color, opacity });
			} else {
				out.push({ key: s.key, d: gen(s.points) ?? '', dash: !!s.dash, color: s.color, opacity });
			}
		}
		return out;
	});

	// Light bands, each split at `now` so past reads solid and future reads faint.
	const bands = $derived.by(() => {
		if (!showLight) return [] as { x: number; w: number; future: boolean }[];
		const segs: { x: number; w: number; future: boolean }[] = [];
		for (const iv of intervals) {
			const parts =
				iv.start < now && iv.end > now
					? [
							{ s: iv.start, e: now, future: false },
							{ s: now, e: iv.end, future: true }
						]
					: [{ s: iv.start, e: iv.end, future: iv.start >= now }];
			for (const part of parts) {
				const x = $xScale(part.s);
				const w = $xScale(part.e) - x;
				if (w > 0.3) segs.push({ x, w, future: part.future });
			}
		}
		return segs;
	});

	// X ticks every 12h, aligned to local midnight.
	const ticks = $derived.by(() => {
		const [start, end] = $xScale.domain() as [number, number];
		const d = new Date(start);
		d.setHours(0, 0, 0, 0);
		const out: { x: number; label: string; major: boolean }[] = [];
		for (let t = d.getTime(); t <= end; t += 12 * 3_600_000) {
			if (t < start) continue;
			const dt = new Date(t);
			const midnight = dt.getHours() === 0;
			out.push({
				x: $xScale(t),
				major: midnight,
				label: midnight ? fmtTimelineDay(t) : fmtTimelineHour(t)
			});
		}
		return out;
	});

	const nowX = $derived($xScale(now));

	// --- hover ---
	let hoverX = $state<number | null>(null);
	// Merged, sorted set of all times across enabled series (for snapping).
	const allTimes = $derived.by(() => {
		const set = new Set<number>();
		for (const s of series) for (const p of s.points) set.add(p.t);
		return [...set].sort((a, b) => a - b);
	});
	function nearestTime(time: number): number | null {
		if (allTimes.length === 0) return null;
		let best = allTimes[0];
		let bestD = Math.abs(best - time);
		for (const t of allTimes) {
			const dd = Math.abs(t - time);
			if (dd < bestD) {
				bestD = dd;
				best = t;
			}
		}
		return best;
	}
	function valueAt(s: SeriesDef, time: number): number | null {
		let best: number | null = null;
		let bestD = Infinity;
		for (const p of s.points) {
			const dd = Math.abs(p.t - time);
			if (dd < bestD) {
				bestD = dd;
				best = p.value;
			}
		}
		// No sample near this time → treat as missing.
		if (bestD > (tolerances.get(s.key) ?? Infinity)) return null;
		if (best != null && Number.isNaN(best)) return null;
		return best;
	}
	function litAt(time: number): boolean {
		return intervals.some((iv) => time >= iv.start && time < iv.end);
	}
	function onMove(e: PointerEvent) {
		const rect = (e.currentTarget as SVGRectElement).getBoundingClientRect();
		const t = $xScale.invert(e.clientX - rect.left);
		const snapped = nearestTime(t);
		if (snapped == null) return;
		const px = $xScale(snapped);
		hoverX = px;
		onHover?.({
			x: px,
			time: snapped,
			lit: litAt(snapped),
			values: series.map((s) => ({
				color: s.color,
				label: s.label,
				unit: s.unit,
				value: valueAt(s, snapped) as number
			}))
		});
	}
	function onLeave() {
		hoverX = null;
		onHover?.(null);
	}
	const hoverTime = $derived(hoverX == null ? null : nearestTime($xScale.invert(hoverX)));
</script>

<!-- light bands -->
{#each bands as b (b.x + '-' + b.future)}
	<rect x={b.x} y="0" width={b.w} height={$height} fill="var(--color-warn)" opacity={b.future ? 0.06 : 0.13} />
	{#if b.future}
		<rect x={b.x} y="0" width={b.w} height={$height} fill="url(#lc-hatch)" opacity="0.5" />
	{/if}
{/each}

<!-- x grid + axis -->
{#each ticks as t (t.x)}
	<line x1={t.x} x2={t.x} y1="0" y2={$height} stroke="var(--color-rig-800)" stroke-width="1" />
	<text x={t.x + 3} y={$height + 14} fill="var(--color-rig-500)" font-size="10" font-weight={t.major ? 600 : 400}>{t.label}</text>
{/each}

<!-- now line -->
<line x1={nowX} x2={nowX} y1="0" y2={$height} stroke="var(--color-leaf)" stroke-width="1" stroke-dasharray="3 3" opacity="0.8" />
<text x={nowX - 3} y="10" text-anchor="end" fill="var(--color-leaf)" font-size="10" font-weight="600">now</text>

<!-- series lines (clipped to the plot so weather overlays can't overflow) -->
<clipPath id="lc-plot-clip">
	<rect x="0" y="0" width={Math.max(0, $width)} height={$height} />
</clipPath>
<g clip-path="url(#lc-plot-clip)">
	{#each paths as p (p.key)}
		<path
			d={p.d}
			fill="none"
			stroke={p.color}
			stroke-width={p.dash ? 1.5 : 1.75}
			stroke-opacity={p.opacity}
			stroke-dasharray={p.dash ? '4 3' : undefined}
			stroke-linejoin="round"
			stroke-linecap="round"
		/>
	{/each}
</g>

<!-- hover crosshair + dots -->
{#if hoverX != null && hoverTime != null}
	<line x1={hoverX} x2={hoverX} y1="0" y2={$height} stroke="var(--color-rig-300)" stroke-width="1" opacity="0.5" />
	{#each series as s (s.key)}
		{@const v = valueAt(s, hoverTime)}
		{#if v != null}
			<circle cx={hoverX} cy={scales.get(s.key)!(v)} r="3.5" fill={s.color} stroke="var(--color-rig-950)" stroke-width="1.5" />
		{/if}
	{/each}
{/if}

<!-- pointer capture -->
<rect x="0" y="0" width={Math.max(0, $width)} height={$height} fill="transparent" onpointermove={onMove} onpointerleave={onLeave} role="presentation" />
