<script lang="ts">
	// Minimal dependency-free line chart with an optional target reference line.
	interface Props {
		values: number[];
		target?: number;
		color?: string;
		height?: number;
		unit?: string;
	}
	let { values, target, color = 'var(--color-leaf)', height = 56, unit = '' }: Props = $props();

	const width = 240;

	const bounds = $derived.by(() => {
		const pool = target != null ? [...values, target] : values;
		if (pool.length === 0) return { min: 0, max: 1 };
		let min = Math.min(...pool);
		let max = Math.max(...pool);
		if (max - min < 1) {
			min -= 0.5;
			max += 0.5;
		}
		const pad = (max - min) * 0.1;
		return { min: min - pad, max: max + pad };
	});

	function y(v: number): number {
		const { min, max } = bounds;
		return height - ((v - min) / (max - min)) * height;
	}

	const path = $derived.by(() => {
		if (values.length === 0) return '';
		const step = values.length > 1 ? width / (values.length - 1) : 0;
		return values.map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)},${y(v).toFixed(1)}`).join(' ');
	});

	const last = $derived(values.at(-1));
</script>

<div class="relative">
	<svg viewBox="0 0 {width} {height}" class="w-full" preserveAspectRatio="none" style="height:{height}px">
		{#if target != null}
			<line
				x1="0"
				x2={width}
				y1={y(target)}
				y2={y(target)}
				stroke="var(--color-rig-400)"
				stroke-width="1"
				stroke-dasharray="3 3"
				opacity="0.6"
			/>
		{/if}
		{#if path}
			<path d={path} fill="none" stroke={color} stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
		{/if}
	</svg>
	{#if last != null}
		<span class="absolute right-0 top-0 text-xs text-rig-400">now {last.toFixed(1)}{unit}</span>
	{/if}
</div>
