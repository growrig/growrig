<script lang="ts">
	interface Props {
		tempC: number | null;
		humidity: number | null;
		vpd: number | null;
		leafTempOffsetC: number;
	}

	let { tempC, humidity, vpd, leafTempOffsetC }: Props = $props();

	const width = 760;
	const height = 760;
	const plot = { left: 66, top: 38, right: 24, bottom: 52 };
	const plotWidth = width - plot.left - plot.right;
	const plotHeight = height - plot.top - plot.bottom;
	const humidityMin = 0;
	const humidityMax = 100;
	const tempMin = 0;
	const tempMax = 50;
	const humiditySteps = Array.from({ length: 100 }, (_, i) => humidityMax - i);
	const tempSteps = Array.from({ length: 100 }, (_, i) => tempMin + i * 0.5);
	const humidityTicks = [100, 90, 80, 70, 60, 50, 40, 30, 20, 10, 0];
	const tempTicks = [0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50];

	const x = (rh: number) => plot.left + ((humidityMax - rh) / (humidityMax - humidityMin)) * plotWidth;
	const y = (temp: number) => plot.top + ((temp - tempMin) / (tempMax - tempMin)) * plotHeight;
	const svp = (temp: number) => 0.61078 * Math.exp((17.27 * temp) / (temp + 237.3));
	const leafVpd = (temp: number, rh: number) => Math.max(0, svp(temp + leafTempOffsetC) - svp(temp) * (rh / 100));
	const color = (value: number) => {
		if (value < 0.4) return '#2477a5';
		if (value < 0.8) return '#28aa9d';
		if (value < 1.2) return '#9bc85a';
		if (value < 1.6) return '#edc727';
		return '#d64336';
	};
	let canvas = $state<HTMLCanvasElement>();

	$effect(() => {
		if (!canvas) return;
		const ratio = window.devicePixelRatio || 1;
		canvas.width = Math.round(plotWidth * ratio);
		canvas.height = Math.round(plotHeight * ratio);
		const ctx = canvas.getContext('2d');
		if (!ctx) return;
		ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
		const cellWidth = plotWidth / humiditySteps.length;
		const cellHeight = plotHeight / tempSteps.length;
		for (let row = 0; row < tempSteps.length; row++) {
			const temp = tempSteps[row];
			for (let column = 0; column < humiditySteps.length; column++) {
				const rh = humiditySteps[column];
				ctx.fillStyle = color(leafVpd(temp + 0.25, rh - 0.5));
				ctx.fillRect(column * cellWidth, row * cellHeight, cellWidth + 0.5, cellHeight + 0.5);
			}
		}
	});

	const valid = $derived(tempC != null && humidity != null && vpd != null);
	const currentX = $derived(x(Math.max(humidityMin, Math.min(humidityMax, humidity ?? humidityMax))));
	const currentY = $derived(y(Math.max(tempMin, Math.min(tempMax, tempC ?? tempMin))));
</script>

<div class="overflow-hidden rounded-xl border border-rig-800 bg-rig-950/40">
	<div class="relative">
		<canvas
			bind:this={canvas}
			class="pointer-events-none absolute"
			style={`left:${(plot.left / width) * 100}%;top:${(plot.top / height) * 100}%;width:${(plotWidth / width) * 100}%;height:${(plotHeight / height) * 100}%;border-radius:10px`}
		></canvas>
	<svg viewBox="0 0 {width} {height}" class="relative block h-auto w-full" role="img" aria-label="VPD chart by air temperature and relative humidity">
		<defs>
			<clipPath id="vpd-plot-clip"><rect x={plot.left} y={plot.top} width={plotWidth} height={plotHeight} rx="10" /></clipPath>
			<filter id="vpd-shadow" x="-30%" y="-30%" width="160%" height="160%">
				<feDropShadow dx="0" dy="2" stdDeviation="3" flood-opacity="0.45" />
			</filter>
		</defs>

		<g clip-path="url(#vpd-plot-clip)">
			{#each humidityTicks as rh}
				<line x1={x(rh)} x2={x(rh)} y1={plot.top} y2={plot.top + plotHeight} stroke="white" stroke-opacity="0.12" />
			{/each}
			{#each tempTicks as temp}
				<line x1={plot.left} x2={plot.left + plotWidth} y1={y(temp)} y2={y(temp)} stroke="white" stroke-opacity="0.12" />
			{/each}

			{#if valid}
				<line x1={currentX} x2={currentX} y1={plot.top} y2={plot.top + plotHeight} stroke="white" stroke-width="1.5" stroke-dasharray="7 7" opacity="0.9" />
				<line x1={plot.left} x2={plot.left + plotWidth} y1={currentY} y2={currentY} stroke="white" stroke-width="1.5" stroke-dasharray="7 7" opacity="0.9" />
			{/if}
		</g>

		<rect x={plot.left} y={plot.top} width={plotWidth} height={plotHeight} rx="10" fill="none" stroke="#475569" stroke-width="1.5" />

		{#each humidityTicks as rh}
			<text x={x(rh)} y="25" fill="#cbd5e1" font-size="13" text-anchor="middle">{rh}%</text>
		{/each}
		{#each tempTicks as temp}
			<text x={plot.left - 11} y={y(temp) + 4} fill="#cbd5e1" font-size="13" text-anchor="end">{temp}°</text>
		{/each}
		<text x={plot.left + plotWidth / 2} y={height - 12} fill="#94a3b8" font-size="13" text-anchor="middle">Air relative humidity</text>
		<text transform="translate(18 {plot.top + plotHeight / 2}) rotate(-90)" fill="#94a3b8" font-size="13" text-anchor="middle">Air temperature (°C)</text>

		{#if valid}
			<g filter="url(#vpd-shadow)">
				<circle cx={currentX} cy={currentY} r="8" fill="#0f172a" stroke="white" stroke-width="3" />
				<g transform="translate({Math.min(currentX + 14, width - 190)} {Math.max(currentY - (leafTempOffsetC === 0 ? 62 : 80), plot.top + 8)})">
					<rect width="166" height={leafTempOffsetC === 0 ? 52 : 70} rx="7" fill="#f8fafc" fill-opacity="0.94" />
					<text x="11" y="21" fill="#334155" font-size="13" font-weight="600">{leafTempOffsetC === 0 ? 'Air' : 'Leaf'} VPD {vpd?.toFixed(2)} kPa</text>
					<text x="11" y="40" fill="#64748b" font-size="12">{tempC?.toFixed(1)}°C · {humidity?.toFixed(0)}% RH</text>
					{#if leafTempOffsetC !== 0}<text x="11" y="58" fill="#64748b" font-size="12">Leaf {(tempC! + leafTempOffsetC).toFixed(1)}°C ({leafTempOffsetC > 0 ? '+' : ''}{leafTempOffsetC}°C)</text>{/if}
				</g>
			</g>
		{/if}
	</svg>
	</div>

	<div class="grid grid-cols-2 gap-x-4 gap-y-2 border-t border-rig-800 px-4 py-3 text-xs sm:grid-cols-5">
		<div class="flex items-center gap-2"><span class="h-2.5 w-2.5 rounded-sm bg-[#2477a5]"></span><span class="text-rig-300">&lt;0.4 Too humid</span></div>
		<div class="flex items-center gap-2"><span class="h-2.5 w-2.5 rounded-sm bg-[#28aa9d]"></span><span class="text-rig-300">0.4–0.8 Propagation</span></div>
		<div class="flex items-center gap-2"><span class="h-2.5 w-2.5 rounded-sm bg-[#9bc85a]"></span><span class="text-rig-300">0.8–1.2 Vegetative</span></div>
		<div class="flex items-center gap-2"><span class="h-2.5 w-2.5 rounded-sm bg-[#edc727]"></span><span class="text-rig-300">1.2–1.6 Flowering</span></div>
		<div class="flex items-center gap-2"><span class="h-2.5 w-2.5 rounded-sm bg-[#d64336]"></span><span class="text-rig-300">&gt;1.6 Too dry</span></div>
	</div>
</div>
