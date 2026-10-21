<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { toast } from '$lib/toast.svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { getPlant, getGrow, getEnvironments, movePlant, repotPlant, updatePlant, harvestPlant, removePlant, getCultivars, cultivarImageURL, getCare, getCareConfig, getRecipes } from '$lib/api';
	import type { Environment, PlantView, PlantDetail, Cultivar, TrackingMode, PotUnit, GrowDetail, CareHistory, CareActionDef, FeedingRecipe } from '$lib/types';
	import HistoryPanel from '$lib/components/HistoryPanel.svelte';
	import PlantOverview from '$lib/components/plant/PlantOverview.svelte';
	import LogCareModal from '$lib/components/LogCareModal.svelte';
	import ClipboardList from '@lucide/svelte/icons/clipboard-list';
	import type { PlantPot } from '$lib/types';
	import { titleCase, daysSince, defaultPlantLabel, plantDisplayName, plantNumbersById } from '$lib/format';
	import { Button, Dialog, Select, Breadcrumb } from '$lib/components/ui';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Sprout from '@lucide/svelte/icons/sprout';

	// Human-readable pot summary, e.g. "11 L · Fabric".
	function potLabel(p: PlantPot): string {
		return `${p.size} ${p.unit}${p.type ? ` · ${titleCase(p.type)}` : ''}`;
	}

	const id = $derived(page.params.id);
	const isAdmin = $derived(auth.isAdmin);

	// --- tabs (URL-addressable via ?tab=) ---
	type Tab = 'overview' | 'history' | 'settings';
	const tabs: { id: Tab; label: string }[] = [
		{ id: 'overview', label: 'Overview' },
		{ id: 'history', label: 'History' },
		{ id: 'settings', label: 'Settings' }
	];
	const activeTab = $derived.by<Tab>(() => {
		const t = page.url.searchParams.get('tab') as Tab | null;
		return t && tabs.some((x) => x.id === t) ? t : 'overview';
	});
	function setTab(t: Tab) {
		const url = new URL(page.url);
		if (t === 'overview') url.searchParams.delete('tab');
		else url.searchParams.set('tab', t);
		goto(url, { replaceState: true, keepFocus: true, noScroll: true });
	}

	let plant = $state<PlantView | null>(null);
	let grow = $state<GrowDetail | null>(null);
	let growPlants = $state<PlantDetail[]>([]);
	let care = $state<CareHistory | null>(null);
	let careDefs = $state<CareActionDef[]>([]);
	let recipes = $state<FeedingRecipe[]>([]);
	const careActions = $derived(careDefs.filter((d) => d.enabled));
	let careOpen = $state(false);
	let environments = $state<Environment[]>([]);
	let cultivars = $state<Cultivar[]>([]);
	let err = $state('');
	let loading = $state(true);

	// The plant's cultivar record (for its image), resolved by name.
	const cultivar = $derived(plant ? cultivars.find((c) => c.name === plant!.cultivar) : undefined);
	const plantNumber = $derived(plant ? plantNumbersById(growPlants).get(plant.id) : undefined);

	async function reload() {
		if (!id) return;
		try {
			plant = await getPlant(id);
			try {
				grow = await getGrow(plant.growId);
				growPlants = grow.plants;
			} catch {
				grow = null;
				growPlants = [];
			}
			// Care lives on the grow; the overview narrows it to this plant.
			const growId = plant.growId;
			getCare(growId).then((c) => (care = c)).catch(() => (care = null));
			if (careDefs.length === 0) {
				getCareConfig(growId).then((c) => (careDefs = c.actions)).catch(() => {});
				getRecipes().then((r) => (recipes = r)).catch(() => {});
			}
			err = '';
		} catch (e) {
			err = errMsg(e, 'Failed to load plant');
		} finally {
			loading = false;
		}
	}
	onMount(() => {
		reload();
		getEnvironments().then((e) => (environments = e)).catch(() => {});
		getCultivars().then((c) => (cultivars = c)).catch(() => {});
	});

	const envItems = $derived(environments.map((e) => ({ value: e.id, label: e.name })));


	async function move(envId: string) {
		if (!plant || !envId || envId === plant.currentEnvironmentId) return;
		try {
			const dest = environments.find((e) => e.id === envId)?.name;
			await movePlant(plant.id, envId);
			toast.success('Plant moved', { description: dest });
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
			toast.error('Could not move plant', { description: errMsg(e, 'Failed') });
		}
	}
	let editOpen = $state(false);
	let epLabel = $state('');
	let epCultivar = $state('');
	let epTracking = $state<TrackingMode>('individual');
	let epQuantity = $state(1);
	let epPotSize = $state<number | null>(null);
	let epPotUnit = $state<PotUnit>('L');
	let epPotType = $state('');
	let epBusy = $state(false);
	const trackingItems = [
		{ value: 'individual', label: 'Individual plant' },
		{ value: 'group', label: 'Group (tray / bed / batch)' }
	];
	function openEdit() {
		if (!plant) return;
		epLabel = plant.label === defaultPlantLabel(plant.tracking) ? '' : plant.label;
		epCultivar = plant.cultivar;
		epTracking = plant.tracking;
		epQuantity = plant.quantity;
		epPotSize = plant.currentPot?.size ?? null;
		epPotUnit = plant.currentPot?.unit ?? 'L';
		epPotType = plant.currentPot?.type ?? '';
		editOpen = true;
	}
	function potChanged(
		current: PlantView['currentPot'],
		size: number | null,
		unit: PotUnit,
		type: string
	): boolean {
		if (!size || size <= 0) return false;
		if (!current) return true;
		return current.size !== size || current.unit !== unit || (current.type ?? '') !== type;
	}
	async function saveEdit() {
		if (!plant) return;
		epBusy = true;
		try {
			await updatePlant(plant.id, {
				label: epLabel.trim(),
				cultivar: epCultivar.trim(),
				tracking: epTracking,
				quantity: epTracking === 'group' ? epQuantity : 1
			});
			if (potChanged(plant.currentPot, epPotSize, epPotUnit, epPotType)) {
				await repotPlant(plant.id, {
					size: epPotSize!,
					unit: epPotUnit,
					type: epPotType
				});
			}
			editOpen = false;
			toast.success('Plant updated');
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
		} finally {
			epBusy = false;
		}
	}

	const potUnitItems = [
		{ value: 'L', label: 'liters (L)' },
		{ value: 'gal', label: 'gallons' }
	];
	const potTypeItems = [
		{ value: '', label: '—' },
		{ value: 'fabric', label: 'Fabric' },
		{ value: 'plastic', label: 'Plastic' },
		{ value: 'terracotta', label: 'Terracotta' },
		{ value: 'air-pot', label: 'Air pot' },
		{ value: 'autopot', label: 'AutoPot' },
		{ value: 'other', label: 'Other' }
	];

	// Care needs the grow (the dialog logs against it) and at least one action.
	const canLogCare = $derived(!!grow && careActions.length > 0);
	async function onCareLogged() {
		await reload();
	}

	async function harvest() {
		if (!plant || !confirm(`Harvest ${plantDisplayName(plant, plantNumber)}?`)) return;
		try {
			await harvestPlant(plant.id);
			toast.success('Plant harvested', { description: plantDisplayName(plant, plantNumber) });
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
			toast.error('Could not harvest plant', { description: errMsg(e, 'Failed') });
		}
	}
	async function discard() {
		if (!plant || !confirm('Remove this plant?')) return;
		try {
			await removePlant(plant.id);
			toast.success('Plant removed', { description: plantDisplayName(plant, plantNumber) });
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
			toast.error('Could not remove plant', { description: errMsg(e, 'Failed') });
		}
	}

	const statusTone = (s: string) =>
		s === 'active' ? 'text-leaf' : s === 'harvested' ? 'text-warn' : 'text-rig-500';
	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';
</script>

{#if loading}
	<p class="text-rig-400">Loading…</p>
{:else if !plant}
	<Breadcrumb items={[{ label: 'All grows', href: '/grows' }]} />
	<p class="mt-4 text-rig-400">Plant not found.</p>
{:else}
	<div class="space-y-6">
		{#if err}<p class="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">{err}</p>{/if}

		<!-- Header -->
		<header class="space-y-1.5">
			<Breadcrumb items={[{ label: 'All grows', href: '/grows' }, { label: plant.growName || 'Grow', href: `/grows/${plant.growId}` }]} />
			<div class="flex items-center gap-4">
				<div class="h-14 w-14 shrink-0 overflow-hidden rounded-full border border-rig-700 bg-rig-950">
					{#if cultivar?.imageType}
						<img src={cultivarImageURL(cultivar.id)} alt={plant.cultivar} class="h-full w-full object-cover" />
					{:else}
						<div class="flex h-full w-full items-center justify-center text-rig-600"><Sprout size={24} /></div>
					{/if}
				</div>
				<div class="min-w-0">
					<div class="flex items-center gap-3">
						<h1 class="text-2xl font-semibold">
							{plantDisplayName(plant, plantNumber)}{#if plant.tracking === 'group' && plant.quantity > 1}<span class="text-rig-500">&nbsp;×{plant.quantity}</span>{/if}
						</h1>
						<span class="rounded-full bg-rig-800 px-2 py-0.5 text-xs capitalize {statusTone(plant.status)}">{plant.status}</span>
					</div>
					<p class="mt-1 text-sm text-rig-400">
						{plant.tracking === 'group' && plant.quantity > 1 ? `Group of ${plant.quantity}` : 'Individually tracked'}
						{#if plant.cultivar} · {plant.cultivar}{/if}
						· {daysSince(plant.createdAt)}d old
					</p>
				</div>
				{#if isAdmin && plant.status === 'active' && canLogCare}
					<!-- The one way to record anything that happened to this plant —
					     watering, feeding, transplanting — same dialog as the grow. -->
					<Button class="ml-auto shrink-0" onclick={() => (careOpen = true)}><ClipboardList size={15} /> Log care</Button>
				{/if}
			</div>
		</header>

		<!-- Tabs -->
		<div class="flex gap-1 overflow-x-auto overflow-y-hidden border-b border-rig-800">
			{#each tabs as t (t.id)}
				<button
					onclick={() => setTab(t.id)}
					class="-mb-px shrink-0 border-b-2 px-4 py-2 text-sm font-medium transition-colors {activeTab === t.id ? 'border-rig-50 text-rig-50' : 'border-transparent text-rig-400 hover:text-rig-100'}"
				>
					{t.label}
				</button>
			{/each}
		</div>

		{#if activeTab === 'overview'}
			<PlantOverview {plant} {grow} {care} onShowHistory={() => setTab('history')} />
		{:else if activeTab === 'history'}
			<HistoryPanel
				plantId={plant.id}
				photoGrowId={plant.growId}
				canEdit={isAdmin}
				empty="Nothing recorded for this plant yet."
			/>
		{:else if activeTab === 'settings'}
			{#if !isAdmin}
				<p class="text-rig-400">You don't have permission to change this plant.</p>
			{:else}
				<div class="space-y-8">
					<!-- Plant details -->
					<section class="space-y-3">
						<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Plant details</h2>
						<div class="flex items-center justify-between gap-4 rounded-xl border border-rig-800 bg-rig-900/40 p-4">
							<div>
								<div class="text-sm font-medium">{plantDisplayName(plant, plantNumber)}</div>
								<div class="text-xs text-rig-500">
									{plant.tracking === 'group' && plant.quantity > 1 ? `Group of ${plant.quantity}` : 'Individually tracked'}{#if plant.cultivar} · {plant.cultivar}{/if}
								</div>
							</div>
							<Button variant="secondary" onclick={openEdit}><Pencil size={15} /> Edit plant</Button>
						</div>
					</section>

					{#if plant.status === 'active'}
						<!-- Location & pot -->
						<section class="space-y-3">
							<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Location &amp; pot</h2>
							<div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-rig-800 bg-rig-900/40 p-4">
								<div>
									<div class="text-sm font-medium">Move to another environment</div>
									<div class="text-xs text-rig-500">Its placement history is kept.</div>
								</div>
								<Select value={plant.currentEnvironmentId} onValueChange={move} items={envItems} />
							</div>
							<div class="flex items-center justify-between gap-3 rounded-xl border border-rig-800 bg-rig-900/40 p-4">
								<div>
									<div class="text-sm font-medium">Pot</div>
									<div class="text-xs text-rig-500">
										Current: {plant.currentPot ? potLabel(plant.currentPot) : 'none'}. Moving it to a new pot is a
										<button type="button" onclick={() => (careOpen = true)} class="underline hover:text-rig-300">transplant</button>, logged as care so it lands in the history.
									</div>
								</div>
								{#if canLogCare}
									<Button variant="secondary" onclick={() => (careOpen = true)}><ClipboardList size={15} /> Log care</Button>
								{/if}
							</div>
						</section>

						<!-- Danger zone -->
						<section class="space-y-3">
							<h2 class="text-sm font-semibold uppercase tracking-wide text-danger/80">Danger zone</h2>
							<div class="flex items-center justify-between gap-3 rounded-xl border border-rig-800 bg-rig-900/40 p-4">
								<div>
									<div class="text-sm font-medium">Harvest this plant</div>
									<div class="text-xs text-rig-500">Marks it harvested; it stays in the grow's history.</div>
								</div>
								<Button variant="secondary" onclick={harvest}>Harvest</Button>
							</div>
							<div class="flex items-center justify-between gap-3 rounded-xl border border-danger/30 bg-danger/5 p-4">
								<div>
									<div class="text-sm font-medium">Remove this plant</div>
									<div class="text-xs text-rig-500">Removes the plant from the grow.</div>
								</div>
								<button onclick={discard} class="rounded-md bg-danger/90 px-4 py-1.5 text-sm font-medium text-white transition-colors hover:bg-danger">Remove</button>
							</div>
						</section>
					{/if}
				</div>
			{/if}
		{/if}
	</div>

	{#if isAdmin}
		{#if grow}
			<LogCareModal
				bind:open={careOpen}
				grow={{ ...grow, plants: growPlants }}
				actions={careActions}
				{recipes}
				preselectedPlantIds={[plant.id]}
				onLogged={onCareLogged}
			/>
		{/if}

		<Dialog bind:open={editOpen} title="Edit plant" description="Change this plant's type, label, cultivar and pot. Each plant keeps its own id and history.">
			<div class="space-y-4">
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block">
						<span class="text-xs text-rig-400">Type</span>
						<Select value={epTracking} onValueChange={(v) => (epTracking = v as TrackingMode)} items={trackingItems} class="mt-1" />
					</label>
					{#if epTracking === 'group'}
						<label class="block">
							<span class="text-xs text-rig-400">Plants in group</span>
							<input type="number" min="1" bind:value={epQuantity} class="{field} mt-1" />
						</label>
					{/if}
				</div>
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block">
						<span class="text-xs text-rig-400">Label <span class="text-rig-600">(optional)</span></span>
						<input bind:value={epLabel} placeholder="Plant" class="{field} mt-1" />
					</label>
					<label class="block">
						<span class="text-xs text-rig-400">Cultivar <span class="text-rig-600">(optional)</span></span>
						<input bind:value={epCultivar} placeholder="e.g. Genovese" class="{field} mt-1" />
					</label>
				</div>
				<div class="grid gap-3 sm:grid-cols-3">
					<label class="block">
						<span class="text-xs text-rig-400">Pot size <span class="text-rig-600">(optional)</span></span>
						<input type="number" min="0" step="any" bind:value={epPotSize} placeholder="e.g. 11" class="{field} mt-1" />
					</label>
					<label class="block">
						<span class="text-xs text-rig-400">Unit</span>
						<Select value={epPotUnit} onValueChange={(v) => (epPotUnit = v as PotUnit)} items={potUnitItems} class="mt-1" />
					</label>
					<label class="block">
						<span class="text-xs text-rig-400">Pot type</span>
						<Select value={epPotType} onValueChange={(v) => (epPotType = v)} items={potTypeItems} class="mt-1" />
					</label>
				</div>
				<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
					<Button variant="ghost" onclick={() => (editOpen = false)}>Cancel</Button>
					<Button onclick={saveEdit} disabled={epBusy || (epTracking === 'group' && epQuantity < 1)}>Save</Button>
				</div>
			</div>
		</Dialog>

	{/if}
{/if}
