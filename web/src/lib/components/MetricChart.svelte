<script lang="ts">
	import { line as d3line, curveMonotoneX } from 'd3-shape';
	import { fmtChartHour, fmtChartHourMinute } from '$lib/datetime';

	export interface ChartLine {
		id: string;
		name: string;
		color: string;
		points: { t: number; value: number }[];
		dash?: boolean; // dashed = a comparison line (average / outdoor)
	opacity?: number;
	/** Percentage series use a separate fixed 0–100 right-hand axis. */
	axis?: 'primary' | 'percent';
	unit?: string;
	}

	interface Props {
		lines: ChartLine[];
		unit: string;
		height?: number;
		/** Optional epoch ms; draws a vertical "now" marker when within range. */
		now?: number;
		/** Pin the y-axis floor to 0 (rpm / watts never go negative). */
		zeroBaseline?: boolean;
	}
	let { lines, unit, height = 220, now, zeroBaseline = false }: Props = $props();

	let wrapW = $state(640);
	const pad = { top: 12, right: 40, bottom: 22, left: 44 };
	const plotW = $derived(Math.max(0, wrapW - pad.left - pad.right));
	const plotH = $derived(height - pad.top - pad.bottom);

	const allPoints = $derived(lines.flatMap((l) => l.points));
	const primaryPoints = $derived(lines.filter((l) => l.axis !== 'percent').flatMap((l) => l.points));

	const xDomain = $derived.by<[number, number]>(() => {
		if (allPoints.length === 0) return [0, 1];
		let min = Infinity;
		let max = -Infinity;
		for (const p of allPoints) {
			if (p.t < min) min = p.t;
			if (p.t > max) max = p.t;
		}
		if (min === max) max = min + 1;
		return [min, max];
	});
	const yDomain = $derived.by<[number, number]>(() => {
		let min = Infinity;
		let max = -Infinity;
		for (const p of primaryPoints) {
			if (p.value == null || Number.isNaN(p.value)) continue;
			if (p.value < min) min = p.value;
			if (p.value > max) max = p.value;
		}
		if (!Number.isFinite(min)) return [0, 1];
		if (zeroBaseline) min = 0;
		if (max - min < 1e-6) {
			max += 1;
			if (!zeroBaseline) min -= 1;
		}
		const p = (max - min) * 0.12;
		return [zeroBaseline ? 0 : min - p, max + p];
	});

	const x = $derived((t: number) => {
		const [a, b] = xDomain;
		return pad.left + ((t - a) / (b - a)) * plotW;
	});
	const y = $derived((v: number) => {
		const [a, b] = yDomain;
		return pad.top + plotH - ((v - a) / (b - a)) * plotH;
	});
	const yPercent = $derived((v: number) => pad.top + plotH - (Math.max(0, Math.min(100, v)) / 100) * plotH);

	const paths = $derived(
		lines.map((l) => ({
			id: l.id,
			color: l.color,
			dash: !!l.dash,
			opacity: l.opacity ?? 1,
			axis: l.axis ?? 'primary',
			d:
				d3line<{ t: number; value: number }>()
					.defined((d) => d.value != null && !Number.isNaN(d.value))
					.x((d) => x(d.t))
					.y((d) => l.axis === 'percent' ? yPercent(d.value) : y(d.value))
					.curve(curveMonotoneX)(l.points) ?? ''
		}))
	);

	// y-axis: three evenly spaced ticks.
	const yTicks = $derived.by(() => {
		const [a, b] = yDomain;
		const decs = unit === 'kPa' ? 2 : unit === '°C' ? 1 : 0;
		return [0, 0.5, 1].map((f) => {
			const v = a + (b - a) * f;
			return { y: y(v), label: v.toFixed(decs) };
		});
	});
	// x-axis: up to 5 time ticks across the domain.
	const xTicks = $derived.by(() => {
		const [a, b] = xDomain;
		const n = 4;
		const out: { x: number; label: string }[] = [];
		for (let i = 0; i <= n; i++) {
			const t = a + ((b - a) * i) / n;
			out.push({
				x: x(t),
				label: fmtChartHour(t)
			});
		}
		return out;
	});
	const nowX = $derived(now != null && now >= xDomain[0] && now <= xDomain[1] ? x(now) : null);

	// --- hover ---
	const allTimes = $derived([...new Set(allPoints.map((p) => p.t))].sort((a, b) => a - b));
	// Per-line max gap we'll read a value across; past it the line has no sample
	// at this time, so we render "—" instead of snapping to a distant point.
	const tolerances = $derived.by(() => {
		const out = new Map<string, number>();
		for (const l of lines) {
			const ts = l.points.map((p) => p.t).sort((a, b) => a - b);
			if (ts.length < 2) {
				out.set(l.id, Infinity);
				continue;
			}
			const gaps: number[] = [];
			for (let i = 1; i < ts.length; i++) gaps.push(ts[i] - ts[i - 1]);
			gaps.sort((a, b) => a - b);
			out.set(l.id, gaps[Math.floor(gaps.length / 2)] * 1.5);
		}
		return out;
	});
	let hoverT = $state<number | null>(null);
	function nearest(t: number): number | null {
		if (allTimes.length === 0) return null;
		let best = allTimes[0];
		let bestD = Math.abs(best - t);
		for (const tt of allTimes) {
			const d = Math.abs(tt - t);
			if (d < bestD) {
				bestD = d;
				best = tt;
			}
		}
		return best;
	}
	function valueAt(l: ChartLine, t: number): number | null {
		let best: number | null = null;
		let bestD = Infinity;
		for (const p of l.points) {
			if (p.value == null || Number.isNaN(p.value)) continue;
			const d = Math.abs(p.t - t);
			if (d < bestD) {
				bestD = d;
				best = p.value;
			}
		}
		// No sample near this time → treat as missing.
		if (bestD > (tolerances.get(l.id) ?? Infinity)) return null;
		return best;
	}
	function onMove(e: PointerEvent) {
		// The capture rect starts at x=pad.left, so its bounding-box left already
		// maps to the plot origin — offset within it is straight [0, plotW].
		const rect = (e.currentTarget as SVGRectElement).getBoundingClientRect();
		const px = e.clientX - rect.left;
		const [a, b] = xDomain;
		const t = a + (px / plotW) * (b - a);
		hoverT = nearest(t);
	}
	function onLeave() {
		hoverT = null;
	}
	const decs = $derived(unit === 'kPa' ? 2 : unit === '°C' ? 1 : 0);
</script>

<div class="relative" bind:clientWidth={wrapW}>
	<svg viewBox="0 0 {wrapW} {height}" width={wrapW} {height} class="block">
		<!-- y grid + labels -->
		{#each yTicks as t (t.label)}
			<line x1={pad.left} x2={wrapW - pad.right} y1={t.y} y2={t.y} stroke="var(--color-rig-800)" stroke-width="1" />
			<text x={pad.left - 6} y={t.y + 3} text-anchor="end" fill="var(--color-rig-500)" font-size="10">{t.label}</text>
		{/each}
		{#if lines.some((line) => line.axis === 'percent')}
			{#each [0, 50, 100] as value}
				<text x={wrapW - pad.right + 4} y={yPercent(value) + 3} text-anchor="start" fill="var(--color-sky-400)" font-size="10">{value}%</text>
			{/each}
		{/if}
		<!-- x labels -->
		{#each xTicks as t (t.x)}
			<text x={t.x} y={height - 6} text-anchor="middle" fill="var(--color-rig-500)" font-size="10">{t.label}</text>
		{/each}
		<!-- now -->
		{#if nowX != null}
			<line x1={nowX} x2={nowX} y1={pad.top} y2={pad.top + plotH} stroke="var(--color-leaf)" stroke-width="1" stroke-dasharray="3 3" opacity="0.7" />
		{/if}
		<!-- lines -->
		{#each paths as p (p.id)}
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
		<!-- hover -->
		{#if hoverT != null}
			<line x1={x(hoverT)} x2={x(hoverT)} y1={pad.top} y2={pad.top + plotH} stroke="var(--color-rig-300)" stroke-width="1" opacity="0.5" />
			{#each lines as l (l.id)}
				{@const v = valueAt(l, hoverT)}
				{#if v != null}
				<circle cx={x(hoverT)} cy={l.axis === 'percent' ? yPercent(v) : y(v)} r="3.5" fill={l.color} stroke="var(--color-rig-950)" stroke-width="1.5" />
				{/if}
			{/each}
		{/if}
		<!-- capture -->
		<rect x={pad.left} y={pad.top} width={plotW} height={plotH} fill="transparent" onpointermove={onMove} onpointerleave={onLeave} role="presentation" />
	</svg>

	{#if hoverT != null}
		<div class="pointer-events-none absolute left-1/2 top-0 -translate-x-1/2 rounded-md border border-rig-700 bg-rig-900/95 px-3 py-1.5 text-xs shadow-lg backdrop-blur">
			<div class="mb-1 text-rig-400">{fmtChartHourMinute(hoverT)}</div>
			{#each lines as l (l.id)}
				{@const v = valueAt(l, hoverT)}
				<div class="flex items-center justify-between gap-3">
					<span class="inline-flex items-center gap-1.5"><span class="inline-block h-2 w-2 rounded-full" style="background:{l.color}"></span>{l.name}</span>
					<span class="tabular-nums text-rig-100">{v == null ? '—' : `${v.toFixed(l.axis === 'percent' ? 0 : decs)} ${l.unit ?? unit}`}</span>
				</div>
			{/each}
		</div>
	{/if}
</div>
