<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import {
		getCatalog,
		getDiscovery,
		getEnvironments,
		getPhases,
		createEnvironment,
		updateEnvironment,
		createBinding,
		setCycle
	} from '$lib/api';
	import type { CatalogProduct, DiscoveredEntity, Environment, Phase } from '$lib/types';
	import { volumeM3 } from '$lib/format';
	import CatalogDevicePicker, { type BindingDraft } from '$lib/components/CatalogDevicePicker.svelte';
	import KindIcon from '$lib/components/KindIcon.svelte';
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import X from '@lucide/svelte/icons/x';
	import { Select } from '$lib/components/ui';

	let catalog = $state<CatalogProduct[]>([]);
	let discovered = $state<DiscoveredEntity[]>([]);
	let environments = $state<Environment[]>([]);
	let phases = $state<Phase[]>([]);
	let error = $state<string | null>(null);
	let saving = $state(false);

	onMount(async () => {
		try {
			[catalog, discovered, environments, phases] = await Promise.all([
				getCatalog(),
				getDiscovery(),
				getEnvironments(),
				getPhases()
			]);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load';
		}
	});

	const steps = ['Box', 'Climate', 'Devices', 'Air source', 'Cycle'];
	let step = $state(0);

	// Draft
	let name = $state('');
	let model = $state('');
	let widthCm = $state(0);
	let depthCm = $state(0);
	let heightCm = $state(0);
	let targetTempC = $state(24);
	let targetHumidity = $state(55);
	let targetCO2 = $state(0);
	let emergencyTempC = $state(35);
	let devices = $state<BindingDraft[]>([]);
	const volume = $derived(volumeM3(widthCm, depthCm, heightCm));
	let airMode = $state<'none' | 'create' | 'link'>('none');
	let roomName = $state('Lung Room');
	let roomId = $state('');
	let strain = $state('');
	let startDate = $state(new Date().toISOString().slice(0, 10));
	let phase = $state<Phase>('vegetative');

	const tents = $derived(catalog.filter((p) => p.category === 'tent'));
	const rooms = $derived(environments.filter((e) => e.kind === 'room'));
	const usedEntities = $derived(new Set(devices.map((d) => d.entity)));

	function addDrafts(drafts: BindingDraft[]) {
		devices = [...devices, ...drafts];
	}
	function removeDevice(i: number) {
		devices = devices.filter((_, j) => j !== i);
	}

	const canNext = $derived(step !== 0 || name.trim().length > 0);

	async function finish() {
		saving = true;
		error = null;
		try {
			// Optionally create the lung room first so we can link it.
			let airSourceId = '';
			if (airMode === 'create' && roomName.trim()) {
				const room = await createEnvironment({
					name: roomName,
					kind: 'room',
					airSourceId: '',
					targetTempC: 22,
					targetHumidity: 50,
					targetCO2: 0,
					emergencyTempC: 35,
					leafTempOffsetC: -2
				});
				airSourceId = room.id;
			} else if (airMode === 'link') {
				airSourceId = roomId;
			}

			const tent = await createEnvironment({
				name,
				kind: 'tent',
				airSourceId,
				model,
				widthCm,
				depthCm,
				heightCm,
				targetTempC,
				targetHumidity,
				targetCO2,
				emergencyTempC,
				leafTempOffsetC: -2
			});

			for (const d of devices) {
				await createBinding({ environmentId: tent.id, ...d });
			}

			if (strain.trim()) {
				await setCycle(tent.id, { strain, startedAt: startDate, phase, notes: '' });
			}

			await goto(`/env/${tent.id}`);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to create grow box';
			saving = false;
		}
	}

	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-rig-500 focus:outline-none';
</script>

<div class="mx-auto max-w-2xl">
	<a href="/" class="mb-4 inline-flex items-center gap-1 text-sm text-rig-400 hover:text-rig-100">
	<ArrowLeft size={15} /> Cancel
</a>
	<h1 class="mb-1 text-2xl font-semibold">New Grow Box</h1>

	<!-- stepper -->
	<div class="mb-6 flex gap-2">
		{#each steps as label, i (label)}
			<div class="flex-1">
				<div class="h-1 rounded-full {i <= step ? 'bg-rig-500' : 'bg-rig-800'}"></div>
				<div class="mt-1 text-[11px] {i === step ? 'text-rig-100' : 'text-rig-500'}">{label}</div>
			</div>
		{/each}
	</div>

	{#if error}
		<div class="mb-4 rounded-lg bg-danger/15 px-4 py-2 text-sm text-danger">{error}</div>
	{/if}

	<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-5">
		{#if step === 0}
			<div class="space-y-4">
				<label class="block">
					<span class="text-sm text-rig-400">Name</span>
					<input bind:value={name} placeholder="Main Grow Tent" class="{field} mt-1" />
				</label>
				<label class="block">
					<span class="text-sm text-rig-400">Tent model</span>
					<Select bind:value={model} placeholder="Optional" items={tents.map((t) => ({ value: `${t.brand} ${t.model}`, label: `${t.brand} ${t.model}` }))} class="mt-1" />
				</label>
				<label class="block">
					<span class="text-sm text-rig-400">Dimensions (cm){#if volume} — {volume.toFixed(2)} m³{/if}</span>
					<div class="mt-1 flex items-center gap-2">
						<input type="number" min="0" step="1" bind:value={widthCm} placeholder="W" class="{field} w-full" />
						<span class="text-rig-600">×</span>
						<input type="number" min="0" step="1" bind:value={depthCm} placeholder="D" class="{field} w-full" />
						<span class="text-rig-600">×</span>
						<input type="number" min="0" step="1" bind:value={heightCm} placeholder="H" class="{field} w-full" />
					</div>
				</label>
			</div>
		{:else if step === 1}
			<div class="grid gap-4 sm:grid-cols-2">
				<label class="block">
					<span class="text-sm text-rig-400">Target temp — {targetTempC}°C</span>
					<input type="range" min="15" max="35" step="0.5" bind:value={targetTempC} class="mt-2 w-full accent-rig-500" />
				</label>
				<label class="block">
					<span class="text-sm text-rig-400">Target humidity — {targetHumidity}%</span>
					<input type="range" min="20" max="90" step="1" bind:value={targetHumidity} class="mt-2 w-full accent-rig-500" />
				</label>
				<label class="block">
					<span class="text-sm text-rig-400">Target CO₂ — {targetCO2 || 'off'}{targetCO2 ? ' ppm' : ''}</span>
					<input type="range" min="0" max="1500" step="50" bind:value={targetCO2} class="mt-2 w-full accent-rig-500" />
				</label>
				<label class="block">
					<span class="text-sm text-rig-400">Emergency temp — {emergencyTempC}°C</span>
					<input type="range" min="28" max="45" step="0.5" bind:value={emergencyTempC} class="mt-2 w-full accent-warn" />
				</label>
			</div>
		{:else if step === 2}
			<div class="space-y-4">
				<p class="text-sm text-rig-400">Add fans, lights, and sensors. Pick a product, then choose the matching Home Assistant entity.</p>
				<CatalogDevicePicker {catalog} {discovered} {usedEntities} onAdd={addDrafts} />
				{#if devices.length}
					<div class="space-y-1.5">
						<div class="text-xs font-medium uppercase tracking-wide text-rig-500">Added</div>
						{#each devices as d, i (i)}
							<div class="flex items-center gap-2 rounded-md bg-rig-950/40 px-3 py-1.5 text-sm">
								<KindIcon kind={d.kind} size={16} class="shrink-0 text-rig-400" />
								<span class="flex-1">{d.name} <span class="text-xs text-rig-500">{d.entity}</span></span>
								<button onclick={() => removeDevice(i)} class="text-rig-500 hover:text-danger" aria-label="Remove"><X size={15} /></button>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{:else if step === 3}
			<div class="space-y-4">
				<p class="text-sm text-rig-400">A lung room is the space that supplies your tent's intake air. Optional.</p>
				<div class="flex gap-2">
					{#each [{ k: 'none', l: 'None' }, { k: 'create', l: 'Create room' }, { k: 'link', l: 'Link existing' }] as o (o.k)}
						<button
							onclick={() => (airMode = o.k as typeof airMode)}
							class="rounded-md px-3 py-1.5 text-sm transition-colors {airMode === o.k ? 'bg-rig-700 text-rig-50' : 'bg-rig-950/40 text-rig-400 hover:bg-rig-800'}"
						>
							{o.l}
						</button>
					{/each}
				</div>
				{#if airMode === 'create'}
					<label class="block">
						<span class="text-sm text-rig-400">Room name</span>
						<input bind:value={roomName} class="{field} mt-1" />
						<span class="mt-1 block text-xs text-rig-500">You can add the room's sensors afterwards from Add → Device.</span>
					</label>
				{:else if airMode === 'link'}
					{#if rooms.length}
						<Select bind:value={roomId} placeholder="Select a room…" items={rooms.map((room) => ({ value: room.id, label: room.name }))} />
					{:else}
						<p class="text-sm text-rig-500">No rooms exist yet.</p>
					{/if}
				{/if}
			</div>
		{:else if step === 4}
			<div class="space-y-4">
				<p class="text-sm text-rig-400">Start a grow cycle now, or leave the strain blank to skip.</p>
				<label class="block">
					<span class="text-sm text-rig-400">Strain</span>
					<input bind:value={strain} placeholder="e.g. Blue Dream" class="{field} mt-1" />
				</label>
				<div class="grid gap-4 sm:grid-cols-2">
					<label class="block">
						<span class="text-sm text-rig-400">Start date</span>
						<input type="date" bind:value={startDate} class="{field} mt-1" />
					</label>
					<label class="block">
						<span class="text-sm text-rig-400">Phase</span>
						<Select value={phase} onValueChange={(value) => (phase = value as Phase)} items={phases.map((p) => ({ value: p, label: p[0].toUpperCase() + p.slice(1) }))} class="mt-1" />
					</label>
				</div>
			</div>
		{/if}
	</div>

	<div class="mt-4 flex justify-between">
		<button
			onclick={() => (step = Math.max(0, step - 1))}
			disabled={step === 0}
			class="rounded-md border border-rig-700 px-4 py-1.5 text-sm text-rig-300 transition-colors hover:border-rig-500 disabled:opacity-40"
		>
			Back
		</button>
		{#if step < steps.length - 1}
			<button
				onclick={() => (step += 1)}
				disabled={!canNext}
				class="rounded-md bg-rig-500 px-5 py-1.5 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-400 disabled:opacity-40"
			>
				Next
			</button>
		{:else}
			<button
				onclick={finish}
				disabled={saving || !name.trim()}
				class="rounded-md bg-rig-500 px-5 py-1.5 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-400 disabled:opacity-40"
			>
				{saving ? 'Creating…' : 'Create grow box'}
			</button>
		{/if}
	</div>
</div>
