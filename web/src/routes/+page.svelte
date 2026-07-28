<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { live } from '$lib/live.svelte';
	import { auth } from '$lib/auth.svelte';
	import { climateTone, titleCase, toneClass, valueNow, vpdZone } from '$lib/format';
	import { cameraProxyURL, createEnvironment, getCultivars, getInfo, getLocations, loadDemo, weather } from '$lib/api';
	import { onMount } from 'svelte';
	import type { Cultivar, EnvironmentView, GrowView, Location, Weather } from '$lib/types';
	import { resolveLocationId } from '$lib/location';
	import { addEnv } from '$lib/addEnv.svelte';
	import { attention } from '$lib/attention.svelte';
	import { Dialog, Select } from '$lib/components/ui';
	import NewLocationForm from '$lib/components/NewLocationForm.svelte';
	import EnvironmentDetailsDialog from '$lib/components/EnvironmentDetailsDialog.svelte';
	import CameraPreview from '$lib/components/CameraPreview.svelte';
	import GrowCard from '$lib/components/GrowCard.svelte';
	import AttentionPanel from '$lib/components/AttentionPanel.svelte';
	import Sprout from '@lucide/svelte/icons/sprout';
	import MapPin from '@lucide/svelte/icons/map-pin';
	import Thermometer from '@lucide/svelte/icons/thermometer';
	import Droplets from '@lucide/svelte/icons/droplets';
	import Gauge from '@lucide/svelte/icons/gauge';
	import ChevronRight from '@lucide/svelte/icons/chevron-right';
	import Plus from '@lucide/svelte/icons/plus';
	import Pencil from '@lucide/svelte/icons/pencil';

	const snap = $derived(live.snapshot);

	const healthDot = (h: string) =>
		h === 'online' ? 'bg-leaf' : h === 'stale' ? 'bg-warn' : 'bg-danger';

	// 0,0 is the sentinel for a location saved without coordinates.
	const hasCoords = (loc: Location) => !(loc.lat === 0 && loc.lon === 0);

	let locations = $state<Location[]>([]);
	let weatherByLoc = $state<Record<string, Weather>>({});
	let cultivars = $state<Cultivar[]>([]);

	// "New Lung Room" modal — a room is just a name plus an optional parent site,
	// prefilled with the location the user launched it from.
	let addingRoom = $state(false);
	let roomName = $state('Lung Room');
	let roomLocationId = $state('');
	let savingRoom = $state(false);
	let roomError = $state('');

	function openAddRoom(locId: string) {
		roomName = 'Lung Room';
		roomLocationId = locId;
		roomError = '';
		addingRoom = true;
	}

	async function saveRoom() {
		if (!roomName.trim()) return;
		savingRoom = true;
		roomError = '';
		try {
			await createEnvironment({
				name: roomName.trim(),
				kind: 'room',
				airSourceId: '',
				locationId: roomLocationId,
				targetTempC: 22,
				targetHumidity: 50,
				targetCO2: 0,
				emergencyTempC: 35,
				leafTempOffsetC: -2
			});
			// The live feed pushes the new room on its next reconciliation tick.
			addingRoom = false;
		} catch (e) {
			roomError = errMsg(e, 'Failed to create room');
		} finally {
			savingRoom = false;
		}
	}

	const locationItems = $derived([
		{ value: '__none__', label: 'No location' },
		...locations.map((l) => ({ value: l.id, label: l.name }))
	]);

	// "Edit environment" modal — reused from the settings page, opened per grow box.
	let editEnv = $state<EnvironmentView | null>(null);
	let envEditOpen = $state(false);
	const roomEnvs = $derived((snap?.environments ?? []).filter((e) => e.kind === 'room'));

	function openEditEnv(env: EnvironmentView) {
		editEnv = env;
		envEditOpen = true;
	}

	// Refresh the location list so a location added from the edit dialog appears.
	async function refreshLocations() {
		try {
			locations = await getLocations();
		} catch {
			/* ignore */
		}
	}

	// When set, the location dialog is in edit mode for this site.
	let editingLocation = $state<Location | null>(null);
	let editOpen = $state(false);

	function openEditLocation(loc: Location) {
		editingLocation = loc;
		editOpen = true;
	}

	// Refresh the location list after one is created or edited, and refresh
	// weather so the header strip reflects any coordinate change.
	async function onLocationSaved() {
		editOpen = false;
		editingLocation = null;
		try {
			locations = await getLocations();
		} catch {
			/* ignore */
		}
		for (const loc of locations) {
			if (!hasCoords(loc)) continue;
			weather(loc.lat, loc.lon)
				.then((w) => (weatherByLoc = { ...weatherByLoc, [loc.id]: w }))
				.catch(() => {});
		}
	}

	// Effective location per environment, inheriting a tent's air-source room's
	// location when it has none of its own.
	const locOf = (e: EnvironmentView) => resolveLocationId(e, snap?.environments ?? []);

	// Build the Location → Room → Grow box hierarchy. A tent's air source is its
	// room; a room's location is the site. Tents whose air source isn't a room in
	// their location fall back to a "loose" list rendered at the location level.
	type RoomNode = { room: EnvironmentView; boxes: EnvironmentView[] };
	type LocNode = {
		key: string;
		name: string;
		located: boolean;
		loc?: Location;
		rooms: RoomNode[];
		looseBoxes: EnvironmentView[];
	};

	const groups = $derived.by<LocNode[]>(() => {
		const envs = snap?.environments ?? [];
		const byId = new Map(envs.map((e) => [e.id, e]));
		const rooms = envs.filter((e) => e.kind === 'room');
		const tents = envs.filter((e) => e.kind === 'tent');
		const roomOf = (t: EnvironmentView) => {
			const src = t.airSourceId ? byId.get(t.airSourceId) : undefined;
			return src && src.kind === 'room' ? src : undefined;
		};

		const out: LocNode[] = [];
		const placed = new Set<string>();

		for (const loc of [...locations].sort((a, b) => a.name.localeCompare(b.name))) {
			const roomNodes: RoomNode[] = rooms
				.filter((r) => locOf(r) === loc.id)
				.map((room) => ({ room, boxes: tents.filter((t) => roomOf(t)?.id === room.id) }));
			const looseBoxes = tents.filter(
				(t) => locOf(t) === loc.id && !roomNodes.some((n) => n.boxes.includes(t))
			);
			if (!roomNodes.length && !looseBoxes.length) continue;
			for (const n of roomNodes) {
				placed.add(n.room.id);
				n.boxes.forEach((b) => placed.add(b.id));
			}
			looseBoxes.forEach((b) => placed.add(b.id));
			out.push({ key: loc.id, name: loc.name, located: true, loc, rooms: roomNodes, looseBoxes });
		}

		const orphans = envs.filter((e) => !placed.has(e.id));
		if (orphans.length) {
			const roomNodes: RoomNode[] = orphans
				.filter((e) => e.kind === 'room')
				.map((room) => ({
					room,
					boxes: orphans.filter((t) => t.kind === 'tent' && roomOf(t)?.id === room.id)
				}));
			const looseBoxes = orphans.filter(
				(e) => e.kind === 'tent' && !roomNodes.some((n) => n.boxes.includes(e))
			);
			out.push({ key: '__none__', name: 'No location', located: false, rooms: roomNodes, looseBoxes });
		}
		return out;
	});

	const hasEnvs = $derived((snap?.environments ?? []).length > 0);
	const activeGrows = $derived((snap?.grows ?? []).filter((g) => g.status === 'active'));

	let isSimulator = $state(false);
	let loadingDemo = $state(false);
	onMount(async () => {
		attention.load();
		try {
			isSimulator = (await getInfo()).adapter === 'simulator';
		} catch {
			/* ignore */
		}
		try {
			locations = await getLocations();
		} catch {
			/* ignore */
		}
		getCultivars().then((c) => (cultivars = c)).catch(() => {});
		// One weather fetch per sited location (with coordinates) for the header strip.
		for (const loc of locations) {
			if (!hasCoords(loc)) continue;
			weather(loc.lat, loc.lon)
				.then((w) => (weatherByLoc = { ...weatherByLoc, [loc.id]: w }))
				.catch(() => {});
		}
	});
	async function seedDemo() {
		loadingDemo = true;
		try {
			await loadDemo();
		} catch {
			/* ignore; live feed will refresh */
		} finally {
			loadingDemo = false;
		}
	}
</script>

<!-- Compact climate readout for a room header (temp · RH, plus VPD). -->
{#snippet climate(env: EnvironmentView)}
	{#if env.hasTemp || env.hasHum || env.hasCO2}
		<div class="flex items-center gap-3 tabular-nums">
			<span class="text-sm text-rig-300">
				{#if env.hasTemp}<span class={climateTone(env.tempC, env.targetTempC, env.emergencyTempC)}
						>{env.tempC.toFixed(1)}°</span
					>{/if}{#if env.hasHum}<span class="text-rig-500"
						>{#if env.hasTemp} · {/if}{env.humidity.toFixed(0)}%</span
					>{/if}
			</span>
			{#if env.hasClimate}
				<span class="text-sm font-medium {toneClass[vpdZone(env.vpd).tone]}">
					{env.vpd.toFixed(2)}<span class="ml-0.5 text-[10px] text-rig-500">VPD</span>
				</span>
			{/if}
		</div>
	{/if}
{/snippet}

<!-- Grow box (tent) card — the leaf of the hierarchy. -->
{#snippet box(env: EnvironmentView)}
	{@const camera = env.cameras?.[0]}
	<!-- The whole card links to the env; a full-bleed overlay anchor keeps that
	     tap target while letting the edit button live outside the anchor. -->
	<div
		class="group/box relative rounded-xl border border-rig-800 bg-rig-900/50 p-4 transition-colors hover:border-rig-600"
	>
		<a href="/env/{env.id}" class="absolute inset-0 rounded-xl" aria-label="Open {env.name}"></a>
		<div class="pointer-events-none relative grid gap-4 sm:grid-cols-[minmax(0,2fr)_minmax(12rem,1fr)]">
			<div>
			<div class="mb-3 flex items-center justify-between gap-2">
				<div class="flex items-center gap-2">
					<span class="h-2 w-2 rounded-full {healthDot(env.health)}"></span>
					<h3 class="font-semibold">{env.name}</h3>
				</div>
				<div class="flex items-center gap-2">
					{#if auth.isAdmin}
						<button
							onclick={() => openEditEnv(env)}
							title="Edit details"
							aria-label="Edit details"
							class="pointer-events-auto grid h-6 w-6 place-items-center rounded-md border border-rig-700 text-rig-400 opacity-0 transition-opacity focus-visible:opacity-100 group-hover/box:opacity-100 hover:border-leaf hover:text-rig-100"
						>
							<Pencil size={13} />
						</button>
					{/if}
					<span
						class="rounded-full bg-rig-800 px-2 py-0.5 text-[10px] uppercase tracking-wide text-rig-400"
					>
						{env.kind}
					</span>
				</div>
			</div>
			{#if env.hasTemp || env.hasHum || env.hasCO2}
			<div class="flex items-end justify-between">
				<div>
					<div
						class="text-3xl font-semibold tabular-nums {env.hasTemp
							? climateTone(env.tempC, env.targetTempC, env.emergencyTempC)
							: 'text-rig-500'}"
					>
						{env.hasTemp ? `${env.tempC.toFixed(1)}°C` : '—'}
					</div>
					<div class="text-sm text-rig-400">
						{#if env.hasHum}{env.humidity.toFixed(0)}% RH{/if}{#if env.hasCO2}{#if env.hasHum} ·
							{/if}{env.co2.toFixed(0)} ppm{/if}
					</div>
				</div>
				{#if env.hasClimate}
					<div class="text-right">
						<div class="text-lg font-semibold tabular-nums {toneClass[vpdZone(env.vpd).tone]}">
							{env.vpd.toFixed(2)}
						</div>
						<div class="text-xs text-rig-500">VPD kPa</div>
					</div>
				{/if}
			</div>
			{:else}
				<p class="text-sm text-rig-500">no climate sensors yet</p>
			{/if}
			</div>
			{#if camera}
				<div class="overflow-hidden rounded-lg border border-rig-800 bg-rig-950/60">
					<CameraPreview url={camera.entity || camera.cameraType === 'rtsp' || !camera.streamUrl ? cameraProxyURL(camera.id) : camera.streamUrl} type={camera.cameraType === 'rtsp' ? 'snapshot' : camera.cameraType} refreshSeconds={camera.cameraType === 'rtsp' ? camera.cameraCaptureInterval ?? 60 : 2} class="border-0" emptyLabel="Camera connecting…" errorLabel="Camera connecting…" />
				</div>
			{/if}
		</div>
	</div>
{/snippet}

<!-- A room and the grow boxes it feeds air to. -->
{#snippet roomBlock(node: RoomNode)}
	<div class="rounded-2xl border border-rig-800 bg-rig-900/20 p-4 sm:p-5">
		<div class="group/roomhead flex items-center justify-between gap-3">
			<div class="flex items-center gap-2">
				<span class="h-2 w-2 rounded-full {healthDot(node.room.health)}"></span>
				<a href="/env/{node.room.id}" class="font-semibold hover:text-rig-100">{node.room.name}</a>
				<span
					class="rounded-full bg-rig-800 px-2 py-0.5 text-[10px] uppercase tracking-wide text-rig-400"
				>
					{node.room.kind}
				</span>
				{#if auth.isAdmin}
					<span class="ml-1 flex items-center gap-1.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/roomhead:opacity-100">
						<button
							onclick={() => openEditEnv(node.room)}
							title="Edit room"
							aria-label="Edit room"
							class="grid h-6 w-6 place-items-center rounded-md border border-rig-700 text-rig-400 transition-colors hover:border-leaf hover:text-rig-100"
						>
							<Pencil size={13} />
						</button>
						<button
							onclick={() => addEnv.start({ kind: 'tent', airSourceId: node.room.id, locationId: locOf(node.room) })}
							title="Add new tent"
							aria-label="Add new tent"
							class="grid h-6 w-6 place-items-center rounded-md border border-rig-700 text-rig-400 transition-colors hover:border-leaf hover:text-rig-100"
						>
							<Plus size={14} />
						</button>
					</span>
				{/if}
			</div>
			<a href="/env/{node.room.id}" class="group flex items-center gap-2 text-rig-500">
				{@render climate(node.room)}
				<ChevronRight size={16} class="transition-transform group-hover:translate-x-0.5" />
			</a>
		</div>

		<div class="mt-4 border-t border-rig-800/70 pt-4">
			{#if node.boxes.length}
				<div class="grid gap-3 sm:grid-cols-2">
					{#each node.boxes as env (env.id)}{@render box(env)}{/each}
				</div>
			{:else}
				<p class="text-sm text-rig-500">No grow boxes in this room yet.</p>
			{/if}
		</div>
	</div>
{/snippet}

<!-- Active grow card for the dashboard's Active Grows section. -->
{#snippet growCard(g: GrowView)}
	<GrowCard grow={g} {cultivars} />
{/snippet}

<!-- Outdoor weather strip for a located site. -->
{#snippet weatherStrip(w: Weather)}
	{@const t = valueNow(w.temp)}
	{@const h = valueNow(w.humidity)}
	{@const p = valueNow(w.pressure)}
	<div class="flex items-center gap-3 text-sm text-rig-400">
		<span class="text-[11px] uppercase tracking-wide text-rig-600">Outside</span>
		{#if t !== undefined}
			<span class="flex items-center gap-1 tabular-nums"><Thermometer size={14} />{t.toFixed(1)}°C</span>
		{/if}
		{#if h !== undefined}
			<span class="flex items-center gap-1 tabular-nums"><Droplets size={14} />{h.toFixed(0)}%</span>
		{/if}
		{#if p !== undefined}
			<span class="hidden items-center gap-1 tabular-nums sm:flex"
				><Gauge size={14} />{p.toFixed(0)} hPa</span
			>
		{/if}
	</div>
{/snippet}

{#if !snap}
	<div class="grid place-items-center py-24 text-rig-400"><p>Connecting to Grow Core…</p></div>
{:else if !hasEnvs}
	<div class="rounded-xl border border-dashed border-rig-800 p-10 text-center">
		<div class="mb-3 flex justify-center text-rig-500"><Sprout size={40} /></div>
		<h2 class="mb-1 text-lg font-semibold">Welcome to GrowRig</h2>
		<p class="mb-5 text-sm text-rig-400">Set up your first grow box to get started.</p>
		<div class="flex flex-wrap justify-center gap-3">
			<button
				onclick={() => addEnv.start({ kind: 'tent' })}
				class="rounded-md bg-rig-50 px-5 py-2 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-200"
			>
				Set up a Grow Box
			</button>
			{#if isSimulator}
				<button
					onclick={seedDemo}
					disabled={loadingDemo}
					class="rounded-md border border-rig-700 px-5 py-2 text-sm text-rig-200 transition-colors hover:border-leaf disabled:opacity-50"
				>
					{loadingDemo ? 'Loading…' : 'Load demo tent'}
				</button>
			{/if}
		</div>
	</div>
{:else}
	<div class="space-y-10">
		<AttentionPanel />

		{#each groups as group (group.key)}
			<section>
				<div class="group/loc mb-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
					<h1
						class="flex items-center gap-2 text-sm font-semibold uppercase tracking-wide {group.located
							? 'text-rig-400'
							: 'text-rig-500'}"
					>
						<span class="flex items-center gap-1.5"><MapPin size={14} />{group.name}</span>
						{#if auth.isAdmin}
							<span
								class="flex items-center gap-1.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/loc:opacity-100"
							>
								{#if group.loc}
									<button
										onclick={() => group.loc && openEditLocation(group.loc)}
										title="Edit location"
										aria-label="Edit location"
										class="grid h-6 w-6 place-items-center rounded-md border border-rig-700 text-rig-400 transition-colors hover:border-leaf hover:text-rig-100"
									>
										<Pencil size={13} />
									</button>
								{/if}
								<button
									onclick={() => openAddRoom(group.loc?.id ?? '')}
									class="inline-flex items-center gap-1 rounded-md border border-rig-700 px-2 py-0.5 text-[11px] font-medium normal-case tracking-normal text-rig-300 transition-colors hover:border-leaf hover:text-rig-100"
								>
									<Plus size={12} /> Add lung room
								</button>
							</span>
						{/if}
					</h1>
					{#if group.loc && weatherByLoc[group.loc.id]}
						{@render weatherStrip(weatherByLoc[group.loc.id])}
					{/if}
				</div>

				<div class="space-y-4">
					{#each group.rooms as node (node.room.id)}{@render roomBlock(node)}{/each}
					{#if group.looseBoxes.length}
						<div class="grid gap-3 sm:grid-cols-2">
							{#each group.looseBoxes as env (env.id)}{@render box(env)}{/each}
						</div>
					{/if}
				</div>
			</section>
		{/each}

		<!-- Active Grows — the cultivation layer, below the physical locations. -->
		<section>
			<div class="mb-4 flex items-center justify-between gap-4">
				<h1 class="flex items-center gap-1.5 text-sm font-semibold uppercase tracking-wide text-rig-400">
					<Sprout size={14} /> Active Grows
				</h1>
				<a href="/grows" class="text-xs text-rig-500 hover:underline">Manage grows</a>
			</div>
			{#if activeGrows.length}
				<div class="grid gap-3 sm:grid-cols-2">
					{#each activeGrows as g (g.id)}{@render growCard(g)}{/each}
				</div>
			{:else}
				<div class="rounded-xl border border-dashed border-rig-800 p-6 text-center text-sm text-rig-500">
					No active grows. <a href="/grows" class="text-leaf hover:underline">Start a grow</a> to track plants across your environments.
				</div>
			{/if}
		</section>

	</div>
{/if}

{#if auth.isAdmin}
	<Dialog bind:open={editOpen} title="Edit location" size="lg">
		{#if editingLocation}
			{#key editingLocation.id}
				<NewLocationForm location={editingLocation} onSaved={onLocationSaved} />
			{/key}
		{/if}
	</Dialog>

	{#if editEnv}
		<EnvironmentDetailsDialog
			env={editEnv}
			rooms={roomEnvs}
			{locations}
			bind:open={envEditOpen}
			onLocationCreated={refreshLocations}
		/>
	{/if}

	<Dialog bind:open={addingRoom} title="New Lung Room" size="lg">
		<div class="space-y-4">
			<label class="block">
				<span class="text-sm text-rig-400">Room name</span>
				<input
					bind:value={roomName}
					class="mt-1 w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2 text-sm focus:border-leaf focus:outline-none"
				/>
			</label>
			<label class="block">
				<span class="text-sm text-rig-400">Location <span class="text-rig-600">(optional)</span></span>
				<Select
					items={locationItems}
					value={roomLocationId || '__none__'}
					onValueChange={(v) => (roomLocationId = v === '__none__' ? '' : v)}
					class="mt-1"
				/>
			</label>
			{#if roomError}
				<p class="text-sm text-danger">{roomError}</p>
			{/if}
			<div class="flex justify-end">
				<button
					onclick={saveRoom}
					disabled={savingRoom || !roomName.trim()}
					class="rounded-md bg-rig-50 px-5 py-1.5 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-200 disabled:opacity-40"
				>
					{savingRoom ? 'Creating…' : 'Create room'}
				</button>
			</div>
		</div>
	</Dialog>
{/if}
