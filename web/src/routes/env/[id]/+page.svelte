<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { live } from '$lib/live.svelte';
	import { auth } from '$lib/auth.svelte';
	import { history, historyRange, deviceHistory, setSwitch, getGrows, getLightingDefaults, getLocations, weather } from '$lib/api';
	import type { CameraRef, DeviceSeries, Grow, Location, StageLightDefaults, Reading, Weather } from '$lib/types';
	import { resolveLocationId } from '$lib/location';
	import TimelineChart from '$lib/components/TimelineChart.svelte';
	import { climateTone, toneClass, vpdZone, volumeM3, formatDimensions } from '$lib/format';
	import StatTile from '$lib/components/StatTile.svelte';
	import VpdGauge from '$lib/components/VpdGauge.svelte';
	import MetricModal, { type MetricDescriptor } from '$lib/components/MetricModal.svelte';
	import ControlGrowCard from '$lib/components/ControlGrowCard.svelte';
	import EnvironmentOccupancy from '$lib/components/EnvironmentOccupancy.svelte';
	import SensorsDialog from '$lib/components/SensorsDialog.svelte';
	import ActivityLog from '$lib/components/ActivityLog.svelte';
	import { Switch } from '$lib/components/ui';
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import Settings from '@lucide/svelte/icons/settings';
	import Lightbulb from '@lucide/svelte/icons/lightbulb';
	import LightbulbOff from '@lucide/svelte/icons/lightbulb-off';
	import Camera from '@lucide/svelte/icons/camera';
	import { cameraProxyURL } from '$lib/api';
	import CameraPreview from '$lib/components/CameraPreview.svelte';
	import CameraDetailModal from '$lib/components/CameraDetailModal.svelte';
	import CameraStreamStats from '$lib/components/CameraStreamStats.svelte';
	import Star from '@lucide/svelte/icons/star';
	import Zap from '@lucide/svelte/icons/zap';

	const id = $derived(page.params.id);
	const env = $derived(live.snapshot?.environments?.find((e) => e.id === id));
	// Write access = operate the grow (toggle devices, edit cycle/schedule).
	// Adding/removing devices and editing settings is admin-only.
	const canWrite = $derived(!!id && auth.canWrite(id));
	const isAdmin = $derived(auth.isAdmin);

	const fans = $derived(env?.controls?.filter((c) => c.kind === 'fan') ?? []);
	const fanSections = $derived([
		{ label: 'Ventilation', fans: fans.filter((fan) => fan.role === 'exhaust' || fan.role === 'intake') },
		{ label: 'Circulation', fans: fans.filter((fan) => fan.role === 'circulation') },
		{ label: 'Other fans', fans: fans.filter((fan) => !fan.role || fan.role === 'unassigned') }
	].filter((section) => section.fans.length > 0));
	const rpmProgress = (rpm: number, maxRpm?: number) => Math.min(100, Math.max(0, (rpm / (maxRpm || 2500)) * 100));
	const lights = $derived(env?.controls?.filter((c) => c.kind === 'light') ?? []);
	// Primary grow light first, then the rest.
	const primaryLight = $derived(lights.find((l) => l.primary) ?? lights[0]);
	const orderedLights = $derived(
		primaryLight ? [primaryLight, ...lights.filter((l) => l.id !== primaryLight.id)] : []
	);

	const dims = $derived(env ? formatDimensions(env.widthCm, env.depthCm, env.heightCm) : '');
	const vol = $derived(env ? volumeM3(env.widthCm, env.depthCm, env.heightCm) : 0);

	let readings = $state<Reading[]>([]);
	let rangeReadings = $state<Reading[]>([]);
	let deviceSeries = $state<DeviceSeries[]>([]);
	let grows = $state<Grow[]>([]);
	let lightingDefaults = $state<StageLightDefaults>({});
	let locations = $state<Location[]>([]);
	let weatherData = $state<Weather | undefined>();

	// The env's location coordinates as a stable string key, so weather is
	// fetched once per location rather than on every live snapshot. A tent
	// without its own location inherits its air-source room's.
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
		weather(lat, lon)
			.then((w) => (weatherData = w))
			.catch(() => {});
	});
	async function refreshHistory() {
		if (!id) return;
		try {
			[readings, rangeReadings, deviceSeries] = await Promise.all([
				history(id, 120),
				historyRange(id, 72, 500),
				deviceHistory(id, 72, 500)
			]);
		} catch {
			/* keep last */
		}
	}
	function refreshGrows() {
		getGrows().then((g) => (grows = g)).catch(() => {});
	}
	onMount(() => {
		refreshHistory();
		refreshGrows();
		getLightingDefaults().then((d) => (lightingDefaults = d)).catch(() => {});
		getLocations().then((l) => (locations = l)).catch(() => {});
		const t = setInterval(refreshHistory, 5000);
		return () => clearInterval(t);
	});

	async function toggleLight(bindingId: string, on: boolean) {
		try {
			await setSwitch(bindingId, on);
		} catch {
			/* ignore; state reconciles via live feed */
		}
	}

const roleLabel: Record<string, string> = {
		exhaust: 'Exhaust',
		intake: 'Intake',
		circulation: 'Circulation',
		unassigned: 'Unassigned'
	};

	// --- metric detail modal ---
	// The modal owns its own timeframe + data fetching; the page just names which
	// metric to open and hands it the live current values.
	let metric = $state<{ descriptor: MetricDescriptor; title: string; unit: string } | null>(null);
	let metricOpen = $state(false);
	let cameraOpen = $state(false);
	let detailCamera = $state<CameraRef | null>(null);
	function openMetric(descriptor: MetricDescriptor, title: string, unit: string) {
		metric = { descriptor, title, unit };
		metricOpen = true;
	}
	function openCamera(camera: CameraRef) {
		detailCamera = camera;
		cameraOpen = true;
	}
</script>

<a href="/" class="mb-4 inline-flex items-center gap-1 text-sm text-rig-400 hover:text-rig-100">
	<ArrowLeft size={15} /> All environments
</a>

{#if !live.snapshot}
	<p class="text-rig-400">Connecting to Grow Core…</p>
{:else if !env}
	<p class="text-rig-400">Environment not found. <a href="/" class="text-leaf hover:underline">Go back</a></p>
{:else}
	<div class="space-y-6">
			<div class="flex items-start justify-between">
			<div>
				<div class="flex flex-wrap items-center gap-2">
					<h1 class="text-2xl font-semibold">{env.name}</h1>
					<span class="rounded-full bg-rig-800 px-2 py-0.5 text-[10px] uppercase tracking-wide text-rig-400">{env.kind}</span>
				</div>
				{#if env.model}<p class="mt-1 text-sm text-rig-400">{env.model}</p>{/if}
					{#if env.airSource || dims || vol}
						<p class="text-sm text-rig-400">
							{#if env.airSource}<span>in</span>{' '}<a href="/env/{env.airSource.id}" class="text-rig-300 underline decoration-rig-600 underline-offset-2 transition-colors hover:text-leaf hover:decoration-leaf">{env.airSource.name}</a>{/if}{#if dims}<span>{env.airSource ? ', ' : ''}{dims}</span>{#if vol}{' '}<span>({vol.toFixed(2)} m³)</span>{/if}{:else if vol}<span>{env.airSource ? ', ' : ''}{vol.toFixed(2)} m³</span>{/if}
						</p>
					{/if}
				</div>
			<div class="flex items-center gap-3">
				{#if env.kind === 'tent'}
					<span class="hidden text-sm text-rig-400 sm:inline">target {env.targetTempC}°C · {env.targetHumidity}% RH{#if env.targetCO2 > 0} · {env.targetCO2} ppm{/if}</span>
				{/if}
				<SensorsDialog sensors={env.sensors ?? []} />
				{#if isAdmin}
					<a href="/env/{id}/settings" class="inline-flex items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-sm text-rig-300 transition-colors hover:border-rig-500 hover:text-rig-100">
						<Settings size={15} /> Settings & Devices
					</a>
				{/if}
			</div>
		</div>

		<div class:grid={env.cameras?.length} class="gap-6 lg:grid-cols-2">
			<div class="space-y-6">
				<!-- Current occupants grouped by grow (named here, so the control-grow
				     card below need not repeat it). -->
				<EnvironmentOccupancy environmentId={env.id} />

				{#if env.kind === 'tent'}
					<ControlGrowCard
						environmentId={env.id}
						grow={env.grow}
						schedule={env.schedule}
						hasPrimaryLight={!!primaryLight}
						canEdit={canWrite}
						{grows}
						defaults={lightingDefaults}
					/>
				{/if}
			</div>

			<!-- Cameras sit beside grow controls and occupants on wide screens. -->
			{#if env.cameras?.length}
				<section>
					<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">Cameras</h2>
					<div class="space-y-3">
						{#each env.cameras as cam (cam.id)}
							<div role="button" tabindex="0" onclick={() => openCamera(cam)} onkeydown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); openCamera(cam); } }} class="group cursor-pointer rounded-lg border border-rig-800 bg-rig-950/40 p-3 transition-colors hover:border-rig-600 hover:bg-rig-900/50 focus-visible:border-rig-500 focus-visible:outline-none">
								<CameraPreview url={cam.cameraType === 'rtsp' || cam.entity || !cam.streamUrl ? cameraProxyURL(cam.id) : cam.streamUrl} liveUrl={cam.cameraType === 'rtsp' || (!cam.streamUrl && !cam.entity) ? cameraProxyURL(cam.id, true) : ''} type={cam.cameraType === 'rtsp' ? 'snapshot' : cam.streamUrl ? cam.cameraType : 'snapshot'} refreshSeconds={cam.cameraType === 'rtsp' ? cam.cameraCaptureInterval ?? 60 : 2} emptyLabel="Connecting to camera…" errorLabel="Connecting to camera…" />
								<div class="mt-2 flex items-center gap-2 text-sm">
									<Camera size={16} class="text-rig-400" />
									<span class="transition-colors group-hover:text-leaf">{cam.name}</span>
									<span class="ml-auto flex items-center gap-2 text-xs text-rig-500">{#if cam.cameraType === 'rtsp'}<CameraStreamStats cameraId={cam.id} showProtocol />{:else}<span>{cam.entity ? 'Home Assistant' : cam.cameraType || 'Connecting…'}</span>{/if}</span>
								</div>
							</div>
						{/each}
					</div>
				</section>
			{/if}
		</div>

		<!-- Timeline -->
		<TimelineChart
			readings={rangeReadings}
			{deviceSeries}
			controls={env.controls ?? []}
			weather={weatherData}
			schedule={env.schedule}
			stage={env.grow?.stage ?? ''}
			defaults={lightingDefaults}
		/>

		<!-- Climate tiles (value + trend; click for per-sensor history) -->
		<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
			<StatTile
				label="Temperature"
				value={env.hasTemp ? env.tempC.toFixed(1) : '—'}
				unit="°C"
				tone={env.hasTemp ? climateTone(env.tempC, env.targetTempC, env.emergencyTempC) : 'muted'}
				spark={env.hasTemp ? readings.map((r) => r.tempC) : undefined}
				sparkColor="#f97316"
				sparkTarget={env.targetTempC}
				onclick={() => openMetric({ kind: 'sensor', measurement: 'temperature' }, 'Temperature', '°C')}
			/>
			<StatTile
				label="Humidity"
				value={env.hasHum ? env.humidity.toFixed(0) : '—'}
				unit="%"
				spark={env.hasHum ? readings.map((r) => r.humidity) : undefined}
				sparkColor="#38bdf8"
				sparkTarget={env.targetHumidity}
				onclick={() => openMetric({ kind: 'sensor', measurement: 'humidity' }, 'Humidity', '%')}
			/>
			{#if env.hasCO2}
				<StatTile
					label="CO₂"
					value={env.co2.toFixed(0)}
					unit="ppm"
					spark={readings.map((r) => r.co2)}
					sparkColor="#a78bfa"
					onclick={() => openMetric({ kind: 'sensor', measurement: 'co2' }, 'CO₂', 'ppm')}
				/>
			{/if}
			<VpdGauge vpd={env.vpd} ok={env.hasClimate} onclick={() => openMetric({ kind: 'vpd' }, 'VPD', 'kPa')} />
		</div>

		<!-- Light -->
		{#if env.kind === 'tent' || lights.length}
			<section>
				<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">Light</h2>
				{#if lights.length === 0}
					<div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed border-warn/40 bg-warn/5 p-4">
						<div class="flex items-center gap-3">
							<LightbulbOff size={22} class="text-warn" />
							<div>
								<div class="text-sm font-medium">No grow light assigned</div>
								<div class="text-xs text-rig-400">A grow box needs a light to run its photoperiod. Assign one to get started.</div>
							</div>
						</div>
						{#if isAdmin}
							<a
								href="/env/{id}/settings#devices"
								class="inline-flex items-center gap-1.5 rounded-md bg-rig-500 px-4 py-1.5 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-400"
							>
								<Lightbulb size={15} /> Assign a light
							</a>
						{/if}
					</div>
				{:else}
					<div class="grid gap-3 sm:grid-cols-2">
						{#each orderedLights as light (light.id)}
							{@const power = light.power ?? (light.on ? light.wattage ?? 0 : 0)}
							<div
								role="button" tabindex="0"
								onclick={() => openMetric({ kind: 'device', bindingId: light.id, metric: 'power' }, `${light.name} · power`, 'W')}
								onkeydown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); openMetric({ kind: 'device', bindingId: light.id, metric: 'power' }, `${light.name} · power`, 'W'); } }}
								class="group cursor-pointer rounded-lg border border-rig-800 bg-rig-950/40 p-3 text-left transition-colors hover:border-rig-600 focus-visible:border-rig-500 focus-visible:outline-none"
							>
								<div class="flex items-center justify-between">
									<div class="flex items-center gap-2" role="presentation" onclick={(event) => event.stopPropagation()} onkeydown={(event) => event.stopPropagation()}>
										{#if light.on}
											<Lightbulb size={18} class="text-leaf" />
										{:else}
											<LightbulbOff size={18} class="text-rig-500" />
										{/if}
										<div class="flex items-center gap-2 text-sm font-medium">
											{light.name}
											{#if light.primary}
												<span class="inline-flex items-center gap-1 rounded-full bg-rig-800 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-rig-300">
													<Star size={10} fill="currentColor" class="text-warn" /> Primary
												</span>
											{/if}
										</div>
									</div>
									<div class="flex items-center gap-2">
										<span class="text-xs font-medium tabular-nums {light.on ? 'text-leaf' : 'text-rig-400'}">
											{light.on ? 'On' : 'Off'}
										</span>
										{#if canWrite}
											<Switch checked={light.on} onCheckedChange={(v) => toggleLight(light.id, v)} />
										{/if}
									</div>
								</div>
								{#if light.wattage}
									<div class="mb-1 mt-3 flex items-center justify-between text-xs text-rig-400">
										<span class="flex items-center gap-1"><Zap size={12} /> power</span>
										<span class="tabular-nums">{Math.round(power)} / {light.wattage} W</span>
									</div>
									<div class="h-2 overflow-hidden rounded-full bg-rig-800">
										<div class="h-full rounded-full bg-leaf transition-all duration-500" style="width:{light.wattage ? (power / light.wattage) * 100 : 0}%"></div>
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			</section>
		{/if}

		<!-- Fans grouped by airflow purpose. -->
		{#each fanSections as fanSection (fanSection.label)}
			<section>
				<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">{fanSection.label}</h2>
				<div class="grid gap-3 sm:grid-cols-2">
					{#each fanSection.fans as fan (fan.id)}
						<div
							role="button" tabindex="0"
							onclick={() => openMetric({ kind: 'device', bindingId: fan.id, metric: 'rpm' }, `${fan.name} · speed`, 'rpm')}
							onkeydown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); openMetric({ kind: 'device', bindingId: fan.id, metric: 'rpm' }, `${fan.name} · speed`, 'rpm'); } }}
							class="group cursor-pointer rounded-lg border border-rig-800 bg-rig-950/40 p-3 text-left transition-colors hover:border-rig-600 focus-visible:border-rig-500 focus-visible:outline-none"
						>
							<div class="mb-2 flex items-center justify-between text-sm">
								<span class="font-medium">{fan.name}</span>
								<div class="flex items-center gap-2">
									<span class="text-rig-400">{roleLabel[fan.role ?? 'unassigned']}</span>
								</div>
							</div>
							<div class="mb-1 flex items-center justify-between text-xs text-rig-400">
								<span>speed</span>
								<span class="tabular-nums">{fan.desiredSpeed}% · {fan.rpm} rpm</span>
							</div>
							<div class="relative h-2 overflow-hidden rounded-full bg-rig-800" title="Speed setting {fan.desiredSpeed}% · RPM feedback {fan.rpm}">
								<div class="absolute inset-y-0 left-0 rounded-full bg-sky-500/45 transition-all duration-500" style="width:{fan.desiredSpeed}%"></div>
								<div class="absolute inset-y-0 left-0 rounded-full bg-leaf transition-all duration-500" style="width:{rpmProgress(fan.rpm, fan.maxRpm)}%"></div>
							</div>
						</div>
					{/each}
				</div>
			</section>
		{/each}

		<!-- Air source (lung room) -->
		{#if env.airSource}
			<section>
				<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">Air source</h2>
				<a
					href="/env/{env.airSource.id}"
					class="flex items-center justify-between rounded-lg border border-rig-800 bg-rig-950/40 p-4 transition-colors hover:border-rig-600"
				>
					<span class="font-medium">{env.airSource.name}</span>
					{#if env.airSource.ok}
						<span class="text-sm tabular-nums text-rig-300">
							{env.airSource.tempC.toFixed(1)}°C · {env.airSource.humidity.toFixed(0)}% ·
							<span class={toneClass[vpdZone(env.airSource.vpd).tone]}>{env.airSource.vpd.toFixed(2)} kPa</span>
						</span>
					{:else}
						<span class="text-sm text-rig-500">no data</span>
					{/if}
				</a>
			</section>
		{/if}

		<section>
			<ActivityLog environmentId={env.id} limit={20} title="Activity Log" />
		</section>
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
	{#if detailCamera}
		<CameraDetailModal bind:open={cameraOpen} camera={detailCamera} />
	{/if}
{/if}
