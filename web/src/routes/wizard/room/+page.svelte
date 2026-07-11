<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { getCatalog, getDiscovery, createEnvironment, createBinding } from '$lib/api';
	import type { CatalogProduct, DiscoveredEntity } from '$lib/types';
	import CatalogDevicePicker, { type BindingDraft } from '$lib/components/CatalogDevicePicker.svelte';
	import KindIcon from '$lib/components/KindIcon.svelte';
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import X from '@lucide/svelte/icons/x';

	let catalog = $state<CatalogProduct[]>([]);
	let discovered = $state<DiscoveredEntity[]>([]);
	let error = $state<string | null>(null);
	let saving = $state(false);

	let name = $state('Lung Room');
	let devices = $state<BindingDraft[]>([]);
	const usedEntities = $derived(new Set(devices.map((d) => d.entity)));

	onMount(async () => {
		try {
			[catalog, discovered] = await Promise.all([getCatalog(), getDiscovery()]);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load';
		}
	});

	async function finish() {
		saving = true;
		error = null;
		try {
			const room = await createEnvironment({
				name,
				kind: 'room',
				airSourceId: '',
				targetTempC: 22,
				targetHumidity: 50,
				targetCO2: 0,
				emergencyTempC: 35,
				leafTempOffsetC: -2
			});
			for (const d of devices) await createBinding({ environmentId: room.id, ...d });
			await goto(`/env/${room.id}`);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to create room';
			saving = false;
		}
	}

	const field = 'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-rig-500 focus:outline-none';
</script>

<div class="mx-auto max-w-2xl">
	<a href="/" class="mb-4 inline-flex items-center gap-1 text-sm text-rig-400 hover:text-rig-100">
	<ArrowLeft size={15} /> Cancel
</a>
	<h1 class="mb-4 text-2xl font-semibold">New Lung Room</h1>

	{#if error}
		<div class="mb-4 rounded-lg bg-danger/15 px-4 py-2 text-sm text-danger">{error}</div>
	{/if}

	<div class="space-y-4 rounded-xl border border-rig-800 bg-rig-900/40 p-5">
		<label class="block">
			<span class="text-sm text-rig-400">Room name</span>
			<input bind:value={name} class="{field} mt-1" />
		</label>
		<div>
			<p class="mb-2 text-sm text-rig-400">Add its sensors (temperature, humidity, CO₂).</p>
			<CatalogDevicePicker {catalog} {discovered} {usedEntities} onAdd={(d) => (devices = [...devices, ...d])} />
		</div>
		{#if devices.length}
			<div class="space-y-1.5">
				{#each devices as d, i (i)}
					<div class="flex items-center gap-2 rounded-md bg-rig-950/40 px-3 py-1.5 text-sm">
						<KindIcon kind={d.kind} size={16} class="shrink-0 text-rig-400" />
						<span class="flex-1">{d.name} <span class="text-xs text-rig-500">{d.entity}</span></span>
						<button onclick={() => (devices = devices.filter((_, j) => j !== i))} class="text-rig-500 hover:text-danger" aria-label="Remove"><X size={15} /></button>
					</div>
				{/each}
			</div>
		{/if}
	</div>

	<div class="mt-4 flex justify-end">
		<button
			onclick={finish}
			disabled={saving || !name.trim()}
			class="rounded-md bg-rig-500 px-5 py-1.5 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-400 disabled:opacity-40"
		>
			{saving ? 'Creating…' : 'Create room'}
		</button>
	</div>
</div>
