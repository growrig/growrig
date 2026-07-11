<script lang="ts">
	import type { Environment, EnvironmentKind, Location } from '$lib/types';
	import { updateEnvironment } from '$lib/api';
	import { formatDimensions, volumeM3 } from '$lib/format';
	import { Button, Dialog, Select, Slider, type SelectItem } from '$lib/components/ui';
	import LocationField from '$lib/components/LocationField.svelte';
	import Pencil from '@lucide/svelte/icons/pencil';

	interface Props {
		env: Environment;
		rooms: Environment[];
		locations: Location[];
		onChanged: () => void;
		flash: (kind: 'ok' | 'err', text: string) => void;
	}
	let { env, rooms, locations, onChanged, flash }: Props = $props();

	let editOpen = $state(false);
	let name = $state('');
	let kind = $state<EnvironmentKind>('tent');
	let model = $state('');
	let airSourceId = $state('');
	let locationId = $state('');
	let widthCm = $state(0);
	let depthCm = $state(0);
	let heightCm = $state(0);
	let temp = $state(24);
	let humidity = $state(55);
	let co2 = $state(0);
	let emergency = $state(35);
	let leafOffset = $state(-2);
	let busy = $state(false);

	$effect(() => {
		temp = env.targetTempC;
		humidity = env.targetHumidity;
		co2 = env.targetCO2;
		emergency = env.emergencyTempC;
		leafOffset = env.leafTempOffsetC ?? -2;
	});

	const otherRooms = $derived(rooms.filter((room) => room.id !== env.id));
	const roomName = $derived(rooms.find((room) => room.id === env.airSourceId)?.name);
	const locationName = $derived(locations.find((l) => l.id === env.locationId)?.name);
	const dimensions = $derived(formatDimensions(env.widthCm, env.depthCm, env.heightCm));
	const volume = $derived(volumeM3(widthCm, depthCm, heightCm));
	const kindItems: SelectItem[] = [{ value: 'tent', label: 'Tent' }, { value: 'room', label: 'Room' }];
	const airItems = $derived<SelectItem[]>([{ value: '__none__', label: 'None' }, ...otherRooms.map((room) => ({ value: room.id, label: room.name }))]);
	const field = 'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2 text-sm focus:border-rig-500 focus:outline-none';

	function openEdit() {
		name = env.name;
		kind = env.kind;
		model = env.model;
		airSourceId = env.airSourceId;
		locationId = env.locationId;
		widthCm = env.widthCm;
		depthCm = env.depthCm;
		heightCm = env.heightCm;
		editOpen = true;
	}

	async function saveDetails() {
		busy = true;
		try {
			await updateEnvironment(env.id, {
				name: name.trim(), kind, model: model.trim(),
				airSourceId: kind === 'tent' ? airSourceId : '',
				locationId,
				widthCm, depthCm, heightCm,
				targetTempC: env.targetTempC, targetHumidity: env.targetHumidity,
				targetCO2: env.targetCO2, emergencyTempC: env.emergencyTempC, leafTempOffsetC: env.leafTempOffsetC ?? -2
			});
			editOpen = false;
			flash('ok', 'Environment details saved');
			onChanged();
		} catch (e) {
			flash('err', e instanceof Error ? e.message : 'Save failed');
		} finally { busy = false; }
	}

	async function saveTargets() {
		busy = true;
		try {
			await updateEnvironment(env.id, {
				name: env.name, kind: env.kind, model: env.model, airSourceId: env.airSourceId,
				locationId: env.locationId,
				widthCm: env.widthCm, depthCm: env.depthCm, heightCm: env.heightCm,
				targetTempC: temp, targetHumidity: humidity, targetCO2: co2, emergencyTempC: emergency, leafTempOffsetC: leafOffset
			});
			flash('ok', 'Targets and safety limits saved');
			onChanged();
		} catch (e) {
			flash('err', e instanceof Error ? e.message : 'Save failed');
		} finally { busy = false; }
	}
</script>

<section class="space-y-3">
	<div class="flex items-center justify-between">
		<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Environment</h2>
		<Button variant="secondary" size="sm" onclick={openEdit}><Pencil size={14} /> Edit</Button>
	</div>
	<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-5">
		<div class="grid gap-x-8 gap-y-4 sm:grid-cols-2 lg:grid-cols-4">
			<div><div class="text-xs text-rig-500">Name</div><div class="mt-1 text-sm font-medium">{env.name}</div></div>
			<div><div class="text-xs text-rig-500">Type</div><div class="mt-1 text-sm capitalize">{env.kind}</div></div>
			<div><div class="text-xs text-rig-500">{env.kind === 'tent' ? 'Tent model' : 'Model'}</div><div class="mt-1 text-sm">{env.model || '—'}</div></div>
			<div><div class="text-xs text-rig-500">Location</div><div class="mt-1 text-sm">{locationName || 'None'}</div></div>
			<div><div class="text-xs text-rig-500">ID</div><div class="mt-1 truncate font-mono text-xs text-rig-300">{env.id}</div></div>
			{#if env.kind === 'tent'}
				<div><div class="text-xs text-rig-500">Dimensions</div><div class="mt-1 text-sm">{dimensions || '—'}</div></div>
				<div><div class="text-xs text-rig-500">Volume</div><div class="mt-1 text-sm">{volumeM3(env.widthCm, env.depthCm, env.heightCm) ? `${volumeM3(env.widthCm, env.depthCm, env.heightCm).toFixed(2)} m³` : '—'}</div></div>
				<div><div class="text-xs text-rig-500">Air source</div><div class="mt-1 text-sm">{roomName || 'None'}</div></div>
			{/if}
		</div>
	</div>
</section>

{#if env.kind === 'tent'}
	<section class="space-y-3">
		<div><h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Targets & Safety</h2><p class="mt-1 text-xs text-rig-500">Climate targets and emergency protection limits.</p></div>
		<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-5">
			<div class="grid gap-6 sm:grid-cols-2 lg:grid-cols-5">
				<div><span class="text-sm text-rig-400">Temperature — {temp}°C</span><Slider min={15} max={35} step={0.5} bind:value={temp} class="mt-3" /></div>
				<div><span class="text-sm text-rig-400">Humidity — {humidity}%</span><Slider min={20} max={90} step={1} bind:value={humidity} class="mt-3" /></div>
				<div><span class="text-sm text-rig-400">CO₂ — {co2 ? `${co2} ppm` : 'off'}</span><Slider min={0} max={1500} step={50} bind:value={co2} class="mt-3" /></div>
				<div><span class="text-sm text-rig-400">Emergency temperature — {emergency}°C</span><Slider min={28} max={45} step={0.5} bind:value={emergency} tone="warn" class="mt-3" /></div>
				<div><span class="text-sm text-rig-400">Leaf temperature offset — {leafOffset > 0 ? '+' : ''}{leafOffset}°C</span><Slider min={-5} max={5} step={0.5} bind:value={leafOffset} class="mt-3" /><p class="mt-2 text-xs text-rig-500">Estimated leaf temp relative to air; −2°C is a common starting point.</p></div>
			</div>
			<div class="mt-5"><Button onclick={saveTargets} disabled={busy}>Save targets</Button></div>
		</div>
	</section>
{/if}

<Dialog bind:open={editOpen} title="Edit environment" description="Update static environment details. Climate targets are managed separately.">
	<div class="space-y-4">
		<label class="block"><span class="text-sm text-rig-400">Name</span><input bind:value={name} class="{field} mt-1" /></label>
		<label class="block"><span class="text-sm text-rig-400">Type</span><Select items={kindItems} value={kind} onValueChange={(value) => (kind = value as EnvironmentKind)} class="mt-1" /></label>
		<label class="block"><span class="text-sm text-rig-400">{kind === 'tent' ? 'Tent model' : 'Model'}</span><input bind:value={model} class="{field} mt-1" /></label>
		<div><span class="text-sm text-rig-400">Location</span><div class="mt-1"><LocationField bind:value={locationId} {locations} onCreated={onChanged} /></div><p class="mt-1 text-xs text-rig-500">Used for local weather and dashboard grouping.</p></div>
		{#if kind === 'tent'}
			<div><span class="text-sm text-rig-400">Dimensions (cm)</span><div class="mt-1 grid grid-cols-[1fr_auto_1fr_auto_1fr] items-center gap-2"><input type="number" min="0" bind:value={widthCm} placeholder="W" class={field} /><span class="text-rig-600">×</span><input type="number" min="0" bind:value={depthCm} placeholder="D" class={field} /><span class="text-rig-600">×</span><input type="number" min="0" bind:value={heightCm} placeholder="H" class={field} /></div><div class="mt-1 text-xs text-rig-500">Volume: {volume ? `${volume.toFixed(2)} m³` : '—'}</div></div>
			<label class="block"><span class="text-sm text-rig-400">Air source</span><Select items={airItems} value={airSourceId || '__none__'} onValueChange={(value) => (airSourceId = value === '__none__' ? '' : value)} class="mt-1" /></label>
		{/if}
		<div class="flex justify-end gap-2 pt-2"><Button variant="ghost" onclick={() => (editOpen = false)}>Cancel</Button><Button onclick={saveDetails} disabled={busy || !name.trim()}>{busy ? 'Saving…' : 'Save'}</Button></div>
	</div>
</Dialog>
