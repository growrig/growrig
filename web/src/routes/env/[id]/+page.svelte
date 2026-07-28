<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { live } from '$lib/live.svelte';
	import { auth } from '$lib/auth.svelte';
	import {
		history,
		historyRange,
		deviceHistory,
		getGrows,
		getLightingDefaults,
		getLocations,
		getAlerts,
		getActivity,
		weather
	} from '$lib/api';
	import type {
		Activity,
		Alert,
		DeviceSeries,
		Grow,
		Location,
		Reading,
		StageLightDefaults,
		Weather
	} from '$lib/types';
	import { resolveLocationId } from '$lib/location';
	import type { MetricDescriptor } from '$lib/components/MetricModal.svelte';
	import type { TargetBand, Annotation } from '$lib/components/timeline/TimelineBody.svelte';
	import MetricModal from '$lib/components/MetricModal.svelte';
	import EnvHeader from '$lib/components/env/EnvHeader.svelte';
	import OverviewTab from '$lib/components/env/OverviewTab.svelte';
	import ClimateTab from '$lib/components/env/ClimateTab.svelte';
	import ControlTab from '$lib/components/env/ControlTab.svelte';
	import EquipmentTab from '$lib/components/env/EquipmentTab.svelte';
	import HistoryTab from '$lib/components/env/HistoryTab.svelte';
	import SettingsTab from '$lib/components/env/SettingsTab.svelte';

	const id = $derived(page.params.id);
	const env = $derived(live.snapshot?.environments?.find((e) => e.id === id));
	const canWrite = $derived(!!id && auth.canWrite(id));
	const isAdmin = $derived(auth.isAdmin);

	// --- tabs (URL-addressable via ?tab=) ---
	type Tab = 'overview' | 'climate' | 'control' | 'equipment' | 'history' | 'settings';
	const tabs: { id: Tab; label: string }[] = [
		{ id: 'overview', label: 'Overview' },
		{ id: 'climate', label: 'Climate' },
		{ id: 'control', label: 'Control' },
		{ id: 'equipment', label: 'Equipment' },
		{ id: 'history', label: 'History' },
		{ id: 'settings', label: 'Settings' }
	];
	const activeTab = $derived.by<Tab>(() => {
		let t = page.url.searchParams.get('tab');
		if (t === 'camera') t = 'equipment'; // cameras live under Equipment now
		if (t === 'activity') t = 'history'; // the Activity tab became History
		return t && tabs.some((x) => x.id === t) ? (t as Tab) : 'overview';
	});
	function setTab(t: Tab) {
		const url = new URL(page.url);
		if (t === 'overview') url.searchParams.delete('tab');
		else url.searchParams.set('tab', t);
		goto(url, { replaceState: true, keepFocus: true, noScroll: true });
	}

	// --- data ---
	let readings = $state<Reading[]>([]);
	let rangeReadings = $state<Reading[]>([]);
	let deviceSeries = $state<DeviceSeries[]>([]);
	let grows = $state<Grow[]>([]);
	let lightingDefaults = $state<StageLightDefaults>({});
	let locations = $state<Location[]>([]);
	let weatherData = $state<Weather | undefined>();
	let alerts = $state<Alert[]>([]);
	let activity = $state<Activity[]>([]);
	let timelineHours = $state(72);

	// Env-scoped open alerts, for the header "all good" state and the Overview row.
	const envAlerts = $derived(alerts.filter((a) => a.environmentId === id));

	// Target "ok bands" for the timeline's Climate mode, from the env's ranges.
	const targetBands = $derived.by<TargetBand[]>(() => {
		if (!env) return [];
		const out: TargetBand[] = [];
		if (env.targetTempMinC && env.targetTempMaxC)
			out.push({ key: 'tempC', min: env.targetTempMinC, max: env.targetTempMaxC, color: '#f97316' });
		if (env.targetHumidityMin && env.targetHumidityMax)
			out.push({ key: 'humidity', min: env.targetHumidityMin, max: env.targetHumidityMax, color: '#38bdf8' });
		if (env.targetVpdMin && env.targetVpdMax)
			out.push({ key: 'vpd', min: env.targetVpdMin, max: env.targetVpdMax, color: '#4ade80' });
		return out;
	});

	// Activity log entries become timeline event markers.
	const annotations = $derived.by<Annotation[]>(() =>
		activity.map((a) => ({
			t: new Date(a.time).getTime(),
			label: a.message,
			color:
				a.level === 'error'
					? 'var(--color-danger)'
					: a.level === 'warning'
						? 'var(--color-warn)'
						: a.type === 'care'
							? 'var(--color-leaf)'
							: 'var(--color-rig-400)'
		}))
	);

	const weatherKey = $derived.by(() => {
		const locId = resolveLocationId(env, live.snapshot?.environments ?? []);
		const loc = locations.find((l) => l.id === locId);
		return loc ? `${loc.lat},${loc.lon}` : '';
	});
	$effect(() => {
		const key = weatherKey;
		if (!key) {
			weatherData = undefined;
			return;
		}
		const [lat, lon] = key.split(',').map(Number);
		weather(lat, lon).then((w) => (weatherData = w)).catch(() => {});
	});

	async function refreshHistory() {
		if (!id) return;
		try {
			[readings, rangeReadings, deviceSeries] = await Promise.all([
				history(id, 120),
				historyRange(id, timelineHours, 500),
				deviceHistory(id, timelineHours, 500)
			]);
		} catch {
			/* keep last */
		}
	}
	function refreshAlerts() {
		getAlerts().then((a) => (alerts = a)).catch(() => {});
	}
	function refreshActivity() {
		if (!id) return;
		getActivity({ environmentId: id, limit: 60 }).then((p) => (activity = p.items)).catch(() => {});
	}
	function onRangeChange(h: number) {
		timelineHours = h;
		refreshHistory();
	}

	onMount(() => {
		refreshHistory();
		refreshAlerts();
		refreshActivity();
		getGrows().then((g) => (grows = g)).catch(() => {});
		getLightingDefaults().then((d) => (lightingDefaults = d)).catch(() => {});
		getLocations().then((l) => (locations = l)).catch(() => {});
		const t = setInterval(() => {
			refreshHistory();
			refreshActivity();
		}, 5000);
		const a = setInterval(refreshAlerts, 30000);
		return () => {
			clearInterval(t);
			clearInterval(a);
		};
	});

	// --- metric detail modal (owned here; tabs open it via onMetric) ---
	let metric = $state<{ descriptor: MetricDescriptor; title: string; unit: string } | null>(null);
	let metricOpen = $state(false);
	function openMetric(descriptor: MetricDescriptor, title: string, unit: string) {
		metric = { descriptor, title, unit };
		metricOpen = true;
	}
</script>

{#if !live.snapshot}
	<p class="text-rig-400">Connecting to Grow Core…</p>
{:else if !env}
	<p class="text-rig-400">Environment not found. <a href="/" class="text-leaf hover:underline">Go back</a></p>
{:else}
	<div class="space-y-6">
		<EnvHeader {env} {locations} alertCount={envAlerts.length} />

		<!-- tabs -->
		<div class="flex gap-1 overflow-x-auto overflow-y-hidden border-b border-rig-800">
			{#each tabs as t (t.id)}
				<button
					onclick={() => setTab(t.id)}
					class="-mb-px shrink-0 border-b-2 px-4 py-2 text-sm font-medium transition-colors {activeTab === t.id
						? 'border-rig-50 text-rig-50'
						: 'border-transparent text-rig-400 hover:text-rig-100'}"
				>
					{t.label}
				</button>
			{/each}
		</div>

		{#if activeTab === 'overview'}
			<OverviewTab
				{env}
				{readings}
				{rangeReadings}
				{deviceSeries}
				{weatherData}
				{grows}
				defaults={lightingDefaults}
				{locations}
				alerts={envAlerts}
				{timelineHours}
				{targetBands}
				{annotations}
				{onRangeChange}
				onMetric={openMetric}
			/>
		{:else if activeTab === 'climate'}
			<ClimateTab
				{env}
				{readings}
				{rangeReadings}
				{timelineHours}
				{onRangeChange}
				onMetric={openMetric}
			/>
		{:else if activeTab === 'control'}
			<ControlTab {env} {canWrite} {grows} defaults={lightingDefaults} />
		{:else if activeTab === 'equipment'}
			<EquipmentTab {env} {canWrite} {isAdmin} />
		{:else if activeTab === 'history'}
			<HistoryTab environmentId={env.id} />
		{:else if activeTab === 'settings'}
			<SettingsTab id={env.id} />
		{/if}
	</div>

	{#if metric}
		<MetricModal
			bind:open={metricOpen}
			envId={env.id}
			title={metric.title}
			unit={metric.unit}
			descriptor={metric.descriptor}
			sensors={env.sensors ?? []}
			controls={env.controls ?? []}
			vpdCurrent={env.hasClimate ? env.vpd : null}
			vpdTempC={env.hasClimate ? env.tempC : null}
			vpdHumidity={env.hasClimate ? env.humidity : null}
			vpdLeafTempOffsetC={env.leafTempOffsetC ?? -2}
		/>
	{/if}
{/if}
