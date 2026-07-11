<script lang="ts">
	import { Dialog } from '$lib/components/ui';
	import MetricChart, { type ChartLine } from '$lib/components/MetricChart.svelte';
	import VpdChart from '$lib/components/VpdChart.svelte';
	import { deviceHistory, historyRange, sensorHistory, weatherHistory } from '$lib/api';
	import type {
		ControlState,
		DeviceSeries,
		Measurement,
		Reading,
		SensorReading,
		SensorSeries,
		WeatherHistory
	} from '$lib/types';
	import CloudSun from '@lucide/svelte/icons/cloud-sun';

	export type MetricDescriptor =
		| { kind: 'sensor'; measurement: Measurement }
		| { kind: 'vpd' }
		| { kind: 'device'; bindingId: string; metric: 'rpm' | 'power' };

	export interface MetricListItem {
		id: string;
		name: string;
		entity: string;
		color: string;
		dash?: boolean;
		current: number | null;
		ok: boolean;
		sub?: string;
	}

	interface Props {
		open?: boolean;
		envId: string;
		title: string;
		unit: string;
		descriptor: MetricDescriptor;
		/** Live sensors of this measurement (labels + current readings). */
		sensors?: SensorReading[];
		/** Live controls (device current value). */
		controls?: ControlState[];
		vpdCurrent?: number | null;
		vpdTempC?: number | null;
		vpdHumidity?: number | null;
		vpdLeafTempOffsetC?: number;
	}
	let { open = $bindable(false), envId, title, unit, descriptor, sensors = [], controls = [], vpdCurrent = null, vpdTempC = null, vpdHumidity = null, vpdLeafTempOffsetC = -2 }: Props =
		$props();

	const timeframes = [
		{ label: '15m', hours: 0.25 },
		{ label: '1h', hours: 1 },
		{ label: '3h', hours: 3 },
		{ label: '12h', hours: 12 },
		{ label: '24h', hours: 24 },
		{ label: '3d', hours: 72 },
		{ label: '7d', hours: 168 },
		{ label: '30d', hours: 720 }
	];
	let hours = $state(72);
	let showForecast = $state(true);
	let vpdPanel = $state<'guide' | 'history'>('guide');
	let wasOpen = false;
	$effect(() => {
		if (open && !wasOpen) {
			showForecast = true;
			if (descriptor.kind === 'vpd') vpdPanel = 'guide';
		}
		wasOpen = open;
	});

	const sourcePalette = ['#f97316', '#38bdf8', '#a78bfa', '#4ade80', '#f472b6', '#facc15', '#2dd4bf'];
	const avgColor = '#e5e7eb'; // near-white, dashed — the controlled average
	const outdoorColor = '#94a3b8'; // slate, dashed — distinct from every sensor hue

	let aggData = $state<Reading[]>([]);
	let sensorData = $state<SensorSeries[]>([]);
	let deviceData = $state<DeviceSeries[]>([]);
	let weatherData = $state<WeatherHistory>({ temp: [], humidity: [], pressure: [] });
	let loading = $state(false);

	const toMs = (pts: { time: string; value: number }[]) =>
		pts.map((p) => ({ t: new Date(p.time).getTime(), value: p.value }));

	// Fetch the selected window whenever the modal is open (and on timeframe /
	// target change). A cancel flag drops stale responses if inputs change fast.
	$effect(() => {
		if (!open) return;
		const h = hours;
		const eid = envId;
		const d = descriptor;
		let cancelled = false;
		loading = true;
		(async () => {
			try {
				if (d.kind === 'device') {
					const ds = await deviceHistory(eid, h, 500);
					if (!cancelled) deviceData = ds;
				} else if (d.kind === 'vpd') {
					const agg = await historyRange(eid, h, 500);
					if (!cancelled) aggData = agg;
				} else {
					const [ss, agg, wx] = await Promise.all([
						sensorHistory(eid, h, 500),
						historyRange(eid, h, 500),
						weatherHistory(eid, h, 500)
					]);
					if (!cancelled) {
						sensorData = ss;
						aggData = agg;
						weatherData = wx;
					}
				}
			} catch {
				/* keep last */
			} finally {
				if (!cancelled) loading = false;
			}
		})();
		return () => {
			cancelled = true;
		};
	});

	const aggField = (m: Measurement): keyof Reading =>
		m === 'temperature' ? 'tempC' : m === 'humidity' ? 'humidity' : 'co2';
	const outdoorPts = (m: Measurement) => {
		const points = m === 'temperature' ? weatherData.temp : m === 'humidity' ? weatherData.humidity : [];
		const futureLimit = Date.now() + (showForecast ? hours / 6 : 0) * 60 * 60 * 1000;
		return points.filter((point) => new Date(point.time).getTime() <= futureLimit);
	};
	const supportsForecast = $derived(descriptor.kind === 'sensor' && (descriptor.measurement === 'temperature' || descriptor.measurement === 'humidity'));

	// Chart lines + the source list share the same set, so build both together.
	const built = $derived.by<{ lines: ChartLine[]; items: MetricListItem[] }>(() => {
		const lines: ChartLine[] = [];
		const items: MetricListItem[] = [];

		if (descriptor.kind === 'sensor') {
			const m = descriptor.measurement;
			const mine = sensors.filter((s) => s.measurement === m);
			const histById = new Map(sensorData.filter((s) => s.measurement === m).map((s) => [s.bindingId, s.points]));
			mine.forEach((s, i) => {
				const color = sourcePalette[i % sourcePalette.length];
				const points = toMs(histById.get(s.id) ?? []);
				lines.push({ id: s.id, name: s.name, color, points });
				items.push({ id: s.id, name: s.name, entity: s.entity, color, current: s.ok ? s.value : null, ok: s.ok });
			});
			// Average of the sensors (the value the engine controls on) — only
			// worth showing when more than one sensor feeds the measurement.
			if (mine.length > 1) {
				const f = aggField(m);
				const points = aggData.map((r) => ({ t: new Date(r.time).getTime(), value: r[f] as number }));
				lines.push({ id: 'avg', name: 'Average', color: avgColor, points, dash: true });
				items.push({
					id: 'avg',
					name: 'Average',
					entity: '',
					color: avgColor,
					dash: true,
					current: points.at(-1)?.value ?? null,
					ok: points.length > 0,
					sub: `mean of ${mine.length} sensors`
				});
			}
			// Outdoor comparison (temperature / humidity have an outdoor analogue).
			const wx = toMs(outdoorPts(m));
			if (wx.length) {
				lines.push({ id: 'outdoor', name: 'Outdoor', color: outdoorColor, points: wx, dash: true, opacity: 0.7 });
				items.push({
					id: 'outdoor',
					name: 'Outdoor',
					entity: '',
					color: outdoorColor,
					dash: true,
					current: wx.at(-1)?.value ?? null,
					ok: wx.length > 0,
					sub: 'local weather'
				});
			}
		} else if (descriptor.kind === 'vpd') {
			const points = aggData.map((r) => ({ t: new Date(r.time).getTime(), value: r.vpd }));
			lines.push({ id: 'vpd', name: 'VPD', color: '#4ade80', points });
			items.push({ id: 'vpd', name: `${vpdLeafTempOffsetC === 0 ? 'Air' : 'Leaf'} VPD (derived)`, entity: '', color: '#4ade80', current: vpdCurrent, ok: vpdCurrent != null });
		} else {
			const ds = deviceData.find((d) => d.bindingId === descriptor.bindingId);
			const ctrl = controls.find((c) => c.id === descriptor.bindingId);
			const color = descriptor.metric === 'rpm' ? '#22d3ee' : '#fbbf24';
			const current =
				descriptor.metric === 'rpm'
					? (ctrl?.rpm ?? null)
					: ctrl
						? (ctrl.power ?? (ctrl.on ? ctrl.wattage ?? 0 : 0))
						: null;
			lines.push({ id: descriptor.bindingId, name: ctrl?.name ?? descriptor.bindingId, color, points: toMs(ds?.points ?? []) });
			items.push({
				id: descriptor.bindingId,
				name: ctrl?.name ?? descriptor.bindingId,
				entity: ctrl?.entity ?? '',
				color,
				current,
				ok: !!ctrl,
				sub: descriptor.metric === 'rpm' ? ctrl?.role : ctrl?.wattage ? `${ctrl.wattage} W max` : undefined
			});
		}
		return { lines, items };
	});

	const note = $derived(descriptor.kind === 'vpd' ? (vpdLeafTempOffsetC === 0 ? 'Air VPD derived from air temperature & humidity — no leaf-temperature correction.' : `Leaf VPD derived using a ${vpdLeafTempOffsetC > 0 ? '+' : ''}${vpdLeafTempOffsetC}°C leaf-temperature offset.`) : undefined);
	const hasHistory = $derived(built.lines.some((l) => l.points.length > 1));
	const decs = $derived(unit === 'kPa' ? 2 : unit === '°C' ? 1 : 0);
	const fmt = (v: number | null) => (v == null ? '—' : `${v.toFixed(decs)}${unit ? ' ' + unit : ''}`);
</script>

<Dialog bind:open {title} description={note} size="2xl">
	<div class="space-y-4">
		{#if descriptor.kind === 'vpd'}
			<div class="flex w-fit rounded-lg border border-rig-800 bg-rig-950/60 p-1" aria-label="VPD view">
				<button type="button" onclick={() => (vpdPanel = 'guide')} class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors {vpdPanel === 'guide' ? 'bg-rig-700 text-rig-50 shadow-sm' : 'text-rig-400 hover:text-rig-100'}">Guide</button>
				<button type="button" onclick={() => (vpdPanel = 'history')} class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors {vpdPanel === 'history' ? 'bg-rig-700 text-rig-50 shadow-sm' : 'text-rig-400 hover:text-rig-100'}">History</button>
			</div>
		{/if}

		{#if descriptor.kind === 'vpd' && vpdPanel === 'guide'}
			<VpdChart tempC={vpdTempC} humidity={vpdHumidity} vpd={vpdCurrent} leafTempOffsetC={vpdLeafTempOffsetC} />
		{:else}
		<!-- timeframe selector -->
		<div class="flex items-center justify-end gap-1">
			{#each timeframes as tf (tf.hours)}
				<button
					type="button"
					onclick={() => (hours = tf.hours)}
					class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors {hours === tf.hours
						? 'bg-rig-700 text-rig-50'
						: 'text-rig-400 hover:bg-rig-800 hover:text-rig-100'}"
				>
					{tf.label}
				</button>
			{/each}
			{#if supportsForecast}
				<span class="mx-1 h-4 w-px bg-rig-800"></span>
				<button
					type="button"
					onclick={() => (showForecast = !showForecast)}
					class="rounded-md p-1.5 transition-colors {showForecast ? 'bg-rig-700 text-leaf' : 'text-rig-500 hover:bg-rig-800 hover:text-rig-100'}"
					aria-label={showForecast ? 'Hide outdoor forecast' : 'Show outdoor forecast'}
					title={showForecast ? 'Outdoor forecast on · future uses ⅙ of the selected window' : 'Outdoor forecast off'}
				>
					<CloudSun size={15} />
				</button>
			{/if}
		</div>

		{#if hasHistory}
			<MetricChart lines={built.lines} {unit} now={Date.now()} height={240} zeroBaseline={descriptor.kind === 'device'} />
		{:else}
			<div class="flex h-40 items-center justify-center rounded-lg border border-dashed border-rig-800 text-center text-sm text-rig-500">
				{loading ? 'Loading…' : 'Not enough history for this window yet — collecting readings…'}
			</div>
		{/if}
		{/if}

		<ul class="space-y-1.5">
			{#each built.items as s (s.id)}
				<li class="flex items-center justify-between gap-3 rounded-lg border border-rig-800 bg-rig-950/40 px-3 py-2">
					<div class="flex min-w-0 items-center gap-2.5">
						<span
							class="inline-block h-2.5 w-2.5 shrink-0 rounded-full {s.dash ? 'ring-1 ring-inset' : ''}"
							style={s.dash ? `border:1px dashed ${s.color};background:transparent` : `background:${s.color}`}
						></span>
						<div class="min-w-0">
							<div class="truncate text-sm text-rig-100">{s.name}{#if s.sub}<span class="text-rig-500"> · {s.sub}</span>{/if}</div>
						</div>
					</div>
					<div class="flex items-center gap-2 whitespace-nowrap">
						<span class="text-sm font-semibold tabular-nums {s.ok ? 'text-rig-100' : 'text-rig-600'}">{s.ok ? fmt(s.current) : '—'}</span>
					</div>
				</li>
			{/each}
			{#if built.items.length === 0}
				<li class="rounded-lg border border-dashed border-rig-800 px-3 py-4 text-center text-sm text-rig-500">No sources bound.</li>
			{/if}
		</ul>
	</div>
</Dialog>
