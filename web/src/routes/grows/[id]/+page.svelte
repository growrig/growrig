<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import {
		getGrow,
		getEnvironments,
		getStagePresets,
		changeStage,
		completeGrow,
		deleteGrow,
		bulkCreatePlants,
		movePlant,
		updatePlant,
		harvestPlant,
		removePlant
	} from '$lib/api';
	import type { Environment, GrowDetail, PlantDetail, StagePresets, TrackingMode } from '$lib/types';
	import { titleCase, daysSince } from '$lib/format';
	import { fmtDate } from '$lib/datetime';
	import GrowFormModal from '$lib/components/GrowFormModal.svelte';
	import { Button, Dialog, Select } from '$lib/components/ui';
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import Sprout from '@lucide/svelte/icons/sprout';
	import Plus from '@lucide/svelte/icons/plus';
	import Pencil from '@lucide/svelte/icons/pencil';

	const id = $derived(page.params.id);
	const isAdmin = $derived(auth.isAdmin);

	let grow = $state<GrowDetail | null>(null);
	let environments = $state<Environment[]>([]);
	let presets = $state<StagePresets>({});
	let err = $state('');
	let loading = $state(true);

	async function reload() {
		if (!id) return;
		try {
			grow = await getGrow(id);
			err = '';
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load grow';
		} finally {
			loading = false;
		}
	}
	onMount(() => {
		reload();
		getEnvironments().then((e) => (environments = e)).catch(() => {});
		getStagePresets().then((p) => (presets = p)).catch(() => {});
	});

	const envItems = $derived(environments.map((e) => ({ value: e.id, label: e.name })));
	const stageItems = $derived((grow?.stages ?? []).map((s) => ({ value: s, label: titleCase(s) })));

	let editing = $state(false);
	let addingPlants = $state(false);

	async function advanceStage(stage: string) {
		if (!grow || stage === grow.stage) return;
		try {
			await changeStage(grow.id, stage);
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed';
		}
	}

	async function move(plant: PlantDetail, envId: string) {
		if (!envId || envId === plant.currentEnvironmentId) return;
		try {
			await movePlant(plant.id, envId);
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed';
		}
	}

	// --- edit a single plant unit (label / cultivar / group size) ---
	let editOpen = $state(false);
	let editingPlant = $state<PlantDetail | null>(null);
	let epLabel = $state('');
	let epCultivar = $state('');
	let epQuantity = $state(1);
	let epBusy = $state(false);
	function openEdit(plant: PlantDetail) {
		editingPlant = plant;
		epLabel = plant.label;
		epCultivar = plant.cultivar;
		epQuantity = plant.quantity;
		editOpen = true;
	}
	async function saveEdit() {
		if (!editingPlant) return;
		epBusy = true;
		try {
			await updatePlant(editingPlant.id, {
				label: epLabel.trim(),
				cultivar: epCultivar.trim(),
				quantity: editingPlant.tracking === 'group' ? epQuantity : undefined
			});
			editOpen = false;
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed';
		} finally {
			epBusy = false;
		}
	}

	async function harvest(plant: PlantDetail) {
		try {
			await harvestPlant(plant.id);
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed';
		}
	}
	async function discard(plant: PlantDetail) {
		if (!confirm(`Remove ${plant.label || 'this plant'}?`)) return;
		try {
			await removePlant(plant.id);
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed';
		}
	}

	async function complete() {
		if (!grow || !confirm('Mark this grow as completed?')) return;
		try {
			await completeGrow(grow.id);
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed';
		}
	}
	async function destroy() {
		if (!grow || !confirm('Delete this grow and all its plants? This cannot be undone.')) return;
		try {
			await deleteGrow(grow.id);
			goto('/grows');
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed';
		}
	}

	// --- add plants form ---
	let apCount = $state(1);
	let apTracking = $state<TrackingMode>('individual');
	let apQuantity = $state(1);
	let apLabel = $state('');
	let apCultivar = $state('');
	let apEnv = $state('');
	let apBusy = $state(false);
	const trackingItems = [
		{ value: 'individual', label: 'Individual plants' },
		{ value: 'group', label: 'Group (tray / bed / batch)' }
	];
	$effect(() => {
		if (addingPlants && !apEnv && environments.length) apEnv = environments[0].id;
	});
	async function addPlants() {
		if (!grow) return;
		apBusy = true;
		try {
			await bulkCreatePlants(grow.id, {
				count: apCount,
				tracking: apTracking,
				quantityPer: apTracking === 'group' ? apQuantity : 1,
				label: apLabel,
				cultivar: apCultivar,
				environmentId: apEnv
			});
			addingPlants = false;
			apCount = 1;
			apLabel = '';
			apCultivar = '';
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed';
		} finally {
			apBusy = false;
		}
	}

	const statusTone = (s: string) =>
		s === 'active' ? 'text-leaf' : s === 'harvested' ? 'text-warn' : 'text-rig-500';
	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-rig-500 focus:outline-none';
</script>

<a href="/grows" class="mb-4 inline-flex items-center gap-1 text-sm text-rig-400 hover:text-rig-100">
	<ArrowLeft size={15} /> All grows
</a>

{#if loading}
	<p class="text-rig-400">Loading…</p>
{:else if !grow}
	<p class="text-rig-400">Grow not found. <a href="/grows" class="text-leaf hover:underline">Go back</a></p>
{:else}
	<div class="space-y-6">
		{#if err}<p class="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">{err}</p>{/if}

		<div class="flex flex-wrap items-start justify-between gap-3">
			<div>
				<div class="flex items-center gap-3">
					<h1 class="text-2xl font-semibold">{grow.name}</h1>
					<span class="rounded-full bg-rig-800 px-2 py-0.5 text-xs capitalize {statusTone(grow.status)}">{grow.status}</span>
				</div>
				<p class="mt-1 text-sm text-rig-400">
					{titleCase(grow.species) || 'No species set'}
					· started {fmtDate(grow.startedAt)} (day {grow.totalDays})
				</p>
			</div>
			{#if isAdmin}
				<div class="flex flex-wrap items-center gap-2">
					<Button variant="ghost" onclick={() => (editing = true)}>Edit</Button>
					{#if grow.status === 'active'}
						<Button variant="secondary" onclick={complete}>Complete</Button>
					{/if}
					<Button variant="ghost" onclick={destroy}>Delete</Button>
				</div>
			{/if}
		</div>

		<!-- Stage & timeline -->
		<section class="rounded-xl border border-rig-800 bg-rig-900/40 p-4">
			<div class="flex flex-wrap items-center justify-between gap-3">
				<div>
					<div class="text-xs uppercase tracking-wide text-rig-500">Current stage</div>
					<div class="mt-0.5 text-lg font-semibold capitalize">{grow.stage || '—'} <span class="text-sm font-normal text-rig-400">· {grow.stageDays}d</span></div>
				</div>
				{#if isAdmin && grow.status === 'active'}
					<label class="flex items-center gap-2 text-sm">
						<span class="text-rig-400">Advance to</span>
						<Select value={grow.stage} onValueChange={advanceStage} items={stageItems} />
					</label>
				{/if}
			</div>
			<!-- Stage sequence -->
			<div class="mt-4 flex flex-wrap gap-1.5">
				{#each grow.stages as st, i (st)}
					{@const current = st === grow.stage}
					<span
						class="rounded-full px-2.5 py-0.5 text-xs capitalize {current
							? 'bg-leaf/20 text-leaf'
							: 'bg-rig-800 text-rig-400'}"
					>
						{i + 1}. {st}
					</span>
				{/each}
			</div>
		</section>

		<!-- Plants -->
		<section>
			<div class="mb-3 flex items-center justify-between">
				<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Plants · {grow.plantCount} active</h2>
				{#if isAdmin}
					<button
						onclick={() => (addingPlants = true)}
						class="inline-flex items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-sm text-rig-300 transition-colors hover:border-rig-500"
					>
						<Plus size={14} /> Add plants
					</button>
				{/if}
			</div>
			{#if grow.plants.length === 0}
				<div class="rounded-xl border border-dashed border-rig-800 p-6 text-center text-sm text-rig-500">
					No plants yet.{#if isAdmin} Add some to start tracking placements.{/if}
				</div>
			{:else}
				<div class="overflow-x-auto rounded-xl border border-rig-800">
					<table class="w-full min-w-[36rem] text-sm">
						<thead class="border-b border-rig-800 text-left text-xs uppercase tracking-wide text-rig-500">
							<tr>
								<th class="px-4 py-2 font-medium">Plant</th>
								<th class="px-4 py-2 font-medium">Cultivar</th>
								<th class="px-4 py-2 font-medium">Status</th>
								<th class="px-4 py-2 font-medium">Location</th>
								<th class="px-4 py-2 font-medium">Age</th>
								{#if isAdmin}<th class="px-4 py-2 font-medium">Actions</th>{/if}
							</tr>
						</thead>
						<tbody>
							{#each grow.plants as p (p.id)}
								<tr class="border-b border-rig-800/60 last:border-0">
									<td class="px-4 py-2">
										<a href="/plants/{p.id}" class="font-medium hover:text-leaf">{p.label || 'Plant'}</a>
										{#if p.tracking === 'group'}<span class="ml-1 text-xs text-rig-500">×{p.quantity}</span>{/if}
									</td>
									<td class="px-4 py-2 text-rig-300">{p.cultivar || '—'}</td>
									<td class="px-4 py-2 capitalize {statusTone(p.status)}">{p.status}</td>
									<td class="px-4 py-2 text-rig-300">
										{#if isAdmin && p.status === 'active'}
											<Select value={p.currentEnvironmentId} onValueChange={(v) => move(p, v)} items={envItems} class="min-w-[9rem]" />
										{:else}
											{p.currentEnvironmentName || '—'}
										{/if}
									</td>
									<td class="px-4 py-2 tabular-nums text-rig-400">{daysSince(p.createdAt)}d</td>
									{#if isAdmin}
										<td class="px-4 py-2">
											<div class="flex items-center gap-1.5">
												<button onclick={() => openEdit(p)} title="Edit plant" aria-label="Edit plant" class="rounded-md border border-rig-700 p-1.5 text-rig-400 hover:border-rig-500 hover:text-rig-200"><Pencil size={14} /></button>
												{#if p.status === 'active'}
													<button onclick={() => harvest(p)} class="rounded-md border border-rig-700 px-2 py-1 text-xs text-rig-300 hover:border-rig-500">Harvest</button>
													<button onclick={() => discard(p)} class="rounded-md border border-rig-700 px-2 py-1 text-xs text-rig-400 hover:border-danger/60 hover:text-danger">Remove</button>
												{/if}
											</div>
										</td>
									{/if}
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>

		{#if grow.notes}
			<section>
				<h2 class="mb-2 text-sm font-semibold uppercase tracking-wide text-rig-400">Notes</h2>
				<p class="whitespace-pre-wrap rounded-xl border border-rig-800 bg-rig-950/40 p-4 text-sm text-rig-300">{grow.notes}</p>
			</section>
		{/if}
	</div>

	{#if isAdmin}
		<GrowFormModal bind:open={editing} grow={grow} {presets} onSaved={reload} />

		<Dialog bind:open={editOpen} title="Edit plant" description="Update this plant's label and cultivar.">

			<div class="space-y-4">
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block">
						<span class="text-xs text-rig-400">Label</span>
						<input bind:value={epLabel} placeholder="Plant" class="{field} mt-1" />
					</label>
					<label class="block">
						<span class="text-xs text-rig-400">Cultivar</span>
						<input bind:value={epCultivar} placeholder="e.g. Genovese" class="{field} mt-1" />
					</label>
				</div>
				{#if editingPlant?.tracking === 'group'}
					<label class="block">
						<span class="text-xs text-rig-400">Plants in group</span>
						<input type="number" min="1" bind:value={epQuantity} class="{field} mt-1" />
					</label>
				{/if}
				<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
					<Button variant="ghost" onclick={() => (editOpen = false)}>Cancel</Button>
					<Button onclick={saveEdit} disabled={epBusy}>Save</Button>
				</div>
			</div>
		</Dialog>

		<Dialog bind:open={addingPlants} title="Add plants" description="Bulk-create plant units and place them in an environment.">
			<div class="space-y-4">
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block">
						<span class="text-xs text-rig-400">How many units</span>
						<input type="number" min="1" bind:value={apCount} class="{field} mt-1" />
					</label>
					<label class="block">
						<span class="text-xs text-rig-400">Tracking</span>
						<Select value={apTracking} onValueChange={(v) => (apTracking = v as TrackingMode)} items={trackingItems} class="mt-1" />
					</label>
				</div>
				{#if apTracking === 'group'}
					<label class="block">
						<span class="text-xs text-rig-400">Plants per group</span>
						<input type="number" min="1" bind:value={apQuantity} class="{field} mt-1" />
					</label>
				{/if}
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block">
						<span class="text-xs text-rig-400">Label</span>
						<input bind:value={apLabel} placeholder={apTracking === 'group' ? 'Tray' : 'Plant'} class="{field} mt-1" />
					</label>
					<label class="block">
						<span class="text-xs text-rig-400">Cultivar <span class="text-rig-600">(optional)</span></span>
						<input bind:value={apCultivar} placeholder="e.g. Genovese" class="{field} mt-1" />
					</label>
				</div>
				<label class="block">
					<span class="text-xs text-rig-400">Place in</span>
					<Select value={apEnv} onValueChange={(v) => (apEnv = v)} items={envItems} class="mt-1" />
				</label>
				<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
					<Button variant="ghost" onclick={() => (addingPlants = false)}>Cancel</Button>
					<Button onclick={addPlants} disabled={apBusy || apCount < 1}><Sprout size={15} /> Add</Button>
				</div>
			</div>
		</Dialog>
	{/if}
{/if}
