<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { live } from '$lib/live.svelte';
	import {
		getGrow,
		getEnvironments,
		getStagePresets,
		changeStage,
		getStageEvents,
		completeGrow,
		deleteGrow,
		createPlant,
		movePlant,
		updatePlant,
		repotPlant,
		harvestPlant,
		removePlant,
		getCultivars,
		getRecipes,
		getCare,
		getCareConfig,
		getGrowPhotos,
		getGrowAnalytics,
		getAlerts,
		getTasks
	} from '$lib/api';
	import type {
		Alert,
		Cultivar,
		CareActionDef,
		CareHistory,
		Environment,
		FeedingRecipe,
		GrowAnalytics,
		GrowDetail,
		GrowPhoto,
		PlantDetail,
		PotUnit,
		StageEvent,
		StagePresets,
		Task,
		TrackingMode
	} from '$lib/types';
	import { defaultPlantLabel, plantDisplayName, plantNumbersById, titleCase } from '$lib/format';
	import GrowFormModal from '$lib/components/GrowFormModal.svelte';
	import StageDatesModal from '$lib/components/StageDatesModal.svelte';
	import LogCareModal from '$lib/components/LogCareModal.svelte';
	import CareSettingsModal from '$lib/components/CareSettingsModal.svelte';
	import { Button, Dialog, Select } from '$lib/components/ui';
	import GrowHeader from '$lib/components/grow/GrowHeader.svelte';
	import OverviewTab from '$lib/components/grow/OverviewTab.svelte';
	import PlantsTab from '$lib/components/grow/PlantsTab.svelte';
	import PlanTab from '$lib/components/grow/PlanTab.svelte';
	import AnalyticsTab from '$lib/components/grow/AnalyticsTab.svelte';
	import HistoryTab from '$lib/components/grow/HistoryTab.svelte';
	import GrowSettingsTab from '$lib/components/grow/SettingsTab.svelte';
	import Sprout from '@lucide/svelte/icons/sprout';
	import ArrowRightLeft from '@lucide/svelte/icons/arrow-right-left';

	const id = $derived(page.params.id);
	const isAdmin = $derived(auth.isAdmin);

	let grow = $state<GrowDetail | null>(null);
	let environments = $state<Environment[]>([]);
	let presets = $state<StagePresets>({});
	let cultivars = $state<Cultivar[]>([]);
	let err = $state('');
	let loading = $state(true);

	// care
	let care = $state<CareHistory | null>(null);
	let careDefs = $state<CareActionDef[]>([]);
	const careActions = $derived(careDefs.filter((d) => d.enabled));
	let recipes = $state<FeedingRecipe[]>([]);
	let careOpen = $state(false);
	let careInitialAction = $state<string | undefined>(undefined);
	let carePreselect = $state<string[]>([]);
	let careSettingsOpen = $state(false);

	// profile data
	let photos = $state<GrowPhoto[]>([]);
	let analytics = $state<GrowAnalytics | null>(null);
	let stageEvents = $state<StageEvent[]>([]);
	let alerts = $state<Alert[]>([]);
	let tasks = $state<Task[]>([]);

	let editing = $state(false);

	const canLogCare = $derived(careActions.length > 0 && (grow?.plantCount ?? 0) > 0);
	const growTasks = $derived(tasks.filter((t) => t.growId === id));
	const growAlerts = $derived(alerts.filter((a) => a.growId === id));

	// --- tabs ---
	type Tab = 'overview' | 'plants' | 'plan' | 'analytics' | 'history' | 'settings';
	const tabs: { id: Tab; label: string }[] = [
		{ id: 'overview', label: 'Overview' },
		{ id: 'plants', label: 'Plants' },
		{ id: 'plan', label: 'Plan' },
		{ id: 'analytics', label: 'Analytics' },
		{ id: 'history', label: 'History' },
		{ id: 'settings', label: 'Settings' }
	];
	const activeTab = $derived.by<Tab>(() => {
		const raw = page.url.searchParams.get('tab');
		// The tab was called "timeline" before it became History; keep old links
		// and bookmarks working.
		if (raw === 'timeline') return 'history';
		const t = raw as Tab | null;
		return t && tabs.some((x) => x.id === t) ? t : 'overview';
	});
	function setTab(t: Tab) {
		const url = new URL(page.url);
		if (t === 'overview') url.searchParams.delete('tab');
		else url.searchParams.set('tab', t);
		goto(url, { replaceState: true, keepFocus: true, noScroll: true });
	}


	async function reloadCare() {
		if (!id) return;
		try {
			care = await getCare(id);
		} catch {
			/* non-critical */
		}
	}
	async function loadCareActions() {
		try {
			if (id) careDefs = (await getCareConfig(id)).actions;
			recipes = await getRecipes();
		} catch {
			/* non-critical */
		}
	}
	function reloadPhotos() {
		if (id) getGrowPhotos(id).then((p) => (photos = p)).catch(() => {});
	}
	function reloadAnalytics() {
		if (id) getGrowAnalytics(id).then((a) => (analytics = a)).catch(() => {});
	}
	function reloadStageEvents() {
		if (id) getStageEvents(id).then((e) => (stageEvents = e)).catch(() => {});
	}
	function reloadAttention() {
		getAlerts().then((a) => (alerts = a)).catch(() => {});
		getTasks('open').then((t) => (tasks = t)).catch(() => {});
	}

	async function reload() {
		if (!id) return;
		try {
			grow = await getGrow(id);
			err = '';
			if (grow.species && careDefs.length === 0) loadCareActions();
		} catch (e) {
			err = errMsg(e, 'Failed to load grow');
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		// Opened from the header quick-add (`?action=logcare`).
		reload().then(() => {
			if (page.url.searchParams.get('action') === 'logcare') openLogCare();
		});
		reloadCare();
		reloadPhotos();
		reloadAnalytics();
		reloadStageEvents();
		reloadAttention();
		getEnvironments().then((e) => (environments = e)).catch(() => {});
		getStagePresets().then((p) => (presets = p)).catch(() => {});
		getCultivars().then((c) => (cultivars = c)).catch(() => {});
	});

	// --- care logging ---
	function openLogCare(actionKey?: string, plantIds: string[] = []) {
		careInitialAction = actionKey;
		carePreselect = plantIds;
		careOpen = true;
	}
	async function onCareLogged() {
		await Promise.all([reload(), reloadCare(), reloadAnalytics()]);
	}
	function onPhotoUploaded() {
		reloadPhotos();
	}

	// --- stage / lifecycle ---
	// A stage change is easy to trigger by accident (a stray click on the stage
	// picker), so route it through a confirmation before committing.
	let pendingStage = $state<string | null>(null);
	let stageConfirmOpen = $state(false);
	let stageBusy = $state(false);
	function advanceStage(stage: string) {
		if (!grow || stage === grow.stage) return;
		pendingStage = stage;
		stageConfirmOpen = true;
	}
	async function confirmStageChange() {
		if (!grow || !pendingStage) return;
		const stage = pendingStage;
		stageBusy = true;
		try {
			await changeStage(grow.id, stage);
			stageConfirmOpen = false;
			toast.success('Stage updated', { description: `${grow.name} · now in ${stage}` });
			await Promise.all([reload(), reloadAnalytics(), reloadStageEvents()]);
		} catch (e) {
			err = errMsg(e, 'Failed');
		} finally {
			stageBusy = false;
		}
	}

	// Reverting to an earlier stage discards the stages entered past it, since
	// stages are strictly directional. Surface which ones in the confirmation.
	const revertedStages = $derived.by(() => {
		if (!grow || !pendingStage) return [] as string[];
		const ti = grow.stages.indexOf(pendingStage);
		const ci = grow.stages.indexOf(grow.stage);
		if (ti < 0 || ti >= ci) return [] as string[];
		return grow.stages.slice(ti + 1, ci + 1);
	});

	// --- stage dates editor ---
	let stageDatesOpen = $state(false);
	function onStageDatesSaved(events: StageEvent[]) {
		stageEvents = events;
		// Editing dates can shift StageStarted, so refresh the grow too.
		Promise.all([reload(), reloadAnalytics()]);
	}
	async function complete() {
		if (!grow || !confirm('Mark this grow as completed?')) return;
		try {
			await completeGrow(grow.id);
			toast.success('Grow completed', { description: grow.name });
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
			toast.error('Could not complete grow', { description: errMsg(e, 'Failed') });
		}
	}
	async function destroy() {
		if (!grow || !confirm('Delete this grow and all its plants? This cannot be undone.')) return;
		const removed = grow.name;
		try {
			await deleteGrow(grow.id);
			toast.success('Grow deleted', { description: removed });
			goto('/grows');
		} catch (e) {
			err = errMsg(e, 'Failed');
			toast.error('Could not delete grow', { description: errMsg(e, 'Failed') });
		}
	}

	const envItems = $derived(environments.map((e) => ({ value: e.id, label: e.name })));
	const speciesCultivars = $derived(grow ? cultivars.filter((c) => c.species === grow!.species) : []);
	const plantNumbers = $derived(plantNumbersById(grow?.plants ?? []));
	function cultivarItems(current: string) {
		const items = [{ value: '', label: '— None —' }];
		const names = new Set<string>();
		for (const c of speciesCultivars) {
			items.push({ value: c.name, label: c.name });
			names.add(c.name);
		}
		if (current && !names.has(current)) items.push({ value: current, label: `${current} (custom)` });
		return items;
	}

	// --- move plant ---
	let moveOpen = $state(false);
	let movingPlant = $state<PlantDetail | null>(null);
	let mpEnv = $state('');
	let mpBusy = $state(false);
	function openMove(plant: PlantDetail) {
		movingPlant = plant;
		mpEnv = plant.currentEnvironmentId;
		moveOpen = true;
	}
	async function saveMove() {
		if (!movingPlant || !mpEnv || mpEnv === movingPlant.currentEnvironmentId) {
			moveOpen = false;
			return;
		}
		mpBusy = true;
		try {
			const dest = environments.find((e) => e.id === mpEnv)?.name;
			await movePlant(movingPlant.id, mpEnv);
			moveOpen = false;
			toast.success('Plant moved', {
				description: dest ? `${plantDisplayName(movingPlant, plantNumbers.get(movingPlant.id))} → ${dest}` : undefined
			});
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
		} finally {
			mpBusy = false;
		}
	}

	// --- edit plant ---
	let editOpen = $state(false);
	let editingPlant = $state<PlantDetail | null>(null);
	let epLabel = $state('');
	let epCultivar = $state('');
	let epTracking = $state<TrackingMode>('individual');
	let epQuantity = $state(1);
	let epPotSize = $state<number | null>(null);
	let epPotUnit = $state<PotUnit>('L');
	let epPotType = $state('');
	let epBusy = $state(false);
	function openEdit(plant: PlantDetail) {
		editingPlant = plant;
		epLabel = plant.label === defaultPlantLabel(plant.tracking) ? '' : plant.label;
		epCultivar = plant.cultivar;
		epTracking = plant.tracking;
		epQuantity = plant.quantity;
		epPotSize = plant.currentPot?.size ?? null;
		epPotUnit = plant.currentPot?.unit ?? 'L';
		epPotType = plant.currentPot?.type ?? '';
		editOpen = true;
	}
	function potChanged(current: PlantDetail['currentPot'], size: number | null, unit: PotUnit, type: string): boolean {
		if (!size || size <= 0) return false;
		if (!current) return true;
		return current.size !== size || current.unit !== unit || (current.type ?? '') !== type;
	}
	async function saveEdit() {
		if (!editingPlant) return;
		epBusy = true;
		try {
			await updatePlant(editingPlant.id, {
				label: epLabel.trim(),
				cultivar: epCultivar.trim(),
				tracking: epTracking,
				quantity: epTracking === 'group' ? epQuantity : 1
			});
			if (potChanged(editingPlant.currentPot, epPotSize, epPotUnit, epPotType)) {
				await repotPlant(editingPlant.id, { size: epPotSize!, unit: epPotUnit, type: epPotType });
			}
			editOpen = false;
			toast.success('Plant updated', { description: plantDisplayName(editingPlant, plantNumbers.get(editingPlant.id)) });
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
		} finally {
			epBusy = false;
		}
	}

	async function harvest(plant: PlantDetail) {
		if (!confirm(`Harvest ${plantDisplayName(plant, plantNumbers.get(plant.id))}?`)) return;
		try {
			await harvestPlant(plant.id);
			toast.success('Plant harvested', { description: plantDisplayName(plant, plantNumbers.get(plant.id)) });
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
			toast.error('Could not harvest plant', { description: errMsg(e, 'Failed') });
		}
	}
	async function discard(plant: PlantDetail) {
		if (!confirm(`Remove ${plantDisplayName(plant, plantNumbers.get(plant.id))}?`)) return;
		try {
			await removePlant(plant.id);
			toast.success('Plant removed', { description: plantDisplayName(plant, plantNumbers.get(plant.id)) });
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
			toast.error('Could not remove plant', { description: errMsg(e, 'Failed') });
		}
	}

	// --- add plant ---
	let addingPlants = $state(false);
	let apTracking = $state<TrackingMode>('individual');
	let apQuantity = $state(1);
	let apLabel = $state('');
	let apCultivar = $state('');
	let apEnv = $state('');
	let apPotSize = $state<number | null>(null);
	let apPotUnit = $state<PotUnit>('L');
	let apPotType = $state('');
	let apBusy = $state(false);
	const trackingItems = [
		{ value: 'individual', label: 'Individual plant' },
		{ value: 'group', label: 'Group (tray / bed / batch)' }
	];
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
		{ value: 'other', label: 'Other' }
	];
	$effect(() => {
		if (addingPlants && !apEnv && environments.length) apEnv = environments[0].id;
	});
	async function addPlant() {
		if (!grow) return;
		apBusy = true;
		try {
			await createPlant(grow.id, {
				tracking: apTracking,
				quantity: apTracking === 'group' ? apQuantity : 1,
				label: apLabel.trim() || undefined,
				cultivar: apCultivar,
				environmentId: apEnv,
				...(apPotSize && apPotSize > 0 ? { potSize: apPotSize, potUnit: apPotUnit, potType: apPotType } : {})
			});
			addingPlants = false;
			toast.success(apTracking === 'group' ? 'Plant group added' : 'Plant added', {
				description: apLabel.trim() || undefined
			});
			apLabel = '';
			apCultivar = '';
			apQuantity = 1;
			apPotSize = null;
			apPotType = '';
			await reload();
		} catch (e) {
			err = errMsg(e, 'Failed');
		} finally {
			apBusy = false;
		}
	}

	const field = 'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';
</script>

{#if loading}
	<p class="text-rig-400">Loading…</p>
{:else if !grow}
	<p class="text-rig-400">Grow not found. <a href="/grows" class="text-leaf hover:underline">Go back</a></p>
{:else}
	<div class="space-y-6">
		{#if err}<p class="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">{err}</p>{/if}

		<GrowHeader
			{grow}
			{isAdmin}
			{canLogCare}
			dueCount={growTasks.length + growAlerts.length}
			onLogCare={() => openLogCare()}
			{onPhotoUploaded}
		/>

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
			<OverviewTab
				{grow}
				{isAdmin}
				{photos}
				{care}
				{careActions}
				{analytics}
				alerts={growAlerts}
				tasks={growTasks}
				onMoveStage={advanceStage}
				{onPhotoUploaded}
			/>
		{:else if activeTab === 'plants'}
			<PlantsTab
				{grow}
				{isAdmin}
				{cultivars}
				{canLogCare}
				onAddPlant={() => (addingPlants = true)}
				onEdit={openEdit}
				onMove={openMove}
				onHarvest={harvest}
				onDiscard={discard}
				onLogCare={(pid) => openLogCare(undefined, [pid])}
			/>
		{:else if activeTab === 'plan'}
			<PlanTab {grow} {isAdmin} {analytics} {careDefs} onAdvance={advanceStage} onEditStages={() => (stageDatesOpen = true)} onCareSettings={() => (careSettingsOpen = true)} />
		{:else if activeTab === 'analytics'}
			<AnalyticsTab {grow} {analytics} {photos} />
		{:else if activeTab === 'history'}
			<HistoryTab {grow} />
		{:else if activeTab === 'settings'}
			<GrowSettingsTab
				{grow}
				{isAdmin}
				onEdit={() => (editing = true)}
				onCareSettings={() => (careSettingsOpen = true)}
				onComplete={complete}
				onDelete={destroy}
			/>
		{/if}
	</div>

	{#if isAdmin}
		<GrowFormModal bind:open={editing} {grow} {presets} onSaved={reload} />
		<LogCareModal
			bind:open={careOpen}
			{grow}
			actions={careActions}
			{recipes}
			preselectedPlantIds={carePreselect}
			initialActionKey={careInitialAction}
			onLogged={onCareLogged}
		/>
		<CareSettingsModal bind:open={careSettingsOpen} growId={grow.id} actions={careDefs} onSaved={(a) => (careDefs = a)} />

		<StageDatesModal bind:open={stageDatesOpen} {grow} events={stageEvents} onSaved={onStageDatesSaved} />

		<Dialog
			bind:open={stageConfirmOpen}
			title={revertedStages.length ? 'Revert stage?' : 'Change stage?'}
			description={revertedStages.length
				? 'Stages are directional, so going back discards the stages entered past this one.'
				: 'This records the stage transition and reshapes the timeline. You can correct the date later.'}
		>
			<div class="space-y-4">
				<p class="text-sm text-rig-200">
					Move <span class="font-medium">{grow.name}</span> from
					<span class="font-medium text-rig-100">{titleCase(grow.stage)}</span> to
					<span class="font-medium text-leaf">{pendingStage ? titleCase(pendingStage) : ''}</span>?
				</p>
				{#if revertedStages.length}
					<div class="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
						This deletes the recorded {revertedStages.length === 1 ? 'stage' : 'stages'}
						<span class="font-medium">{revertedStages.map(titleCase).join(', ')}</span>
						and {revertedStages.length === 1 ? 'its date' : 'their dates'}. This can't be undone.
					</div>
				{/if}
				<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
					<Button variant="ghost" onclick={() => (stageConfirmOpen = false)} disabled={stageBusy}>Cancel</Button>
					<Button onclick={confirmStageChange} disabled={stageBusy}>
						{revertedStages.length ? 'Revert' : 'Move'} to {pendingStage ? titleCase(pendingStage) : ''}
					</Button>
				</div>
			</div>
		</Dialog>

		<Dialog bind:open={moveOpen} title="Change location" description="Move this plant to another environment. Its placement history is kept.">
			<div class="space-y-4">
				<label class="block">
					<span class="text-xs text-rig-400">Environment</span>
					<Select value={mpEnv} onValueChange={(v) => (mpEnv = v)} items={envItems} class="mt-1" />
				</label>
				<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
					<Button variant="ghost" onclick={() => (moveOpen = false)}>Cancel</Button>
					<Button onclick={saveMove} disabled={mpBusy || !mpEnv || mpEnv === movingPlant?.currentEnvironmentId}><ArrowRightLeft size={15} /> Move</Button>
				</div>
			</div>
		</Dialog>

		<Dialog bind:open={editOpen} title="Edit plant" description="Change this plant's type, label, cultivar and pot. Each plant keeps its own id and history.">
			<div class="space-y-4">
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block"><span class="text-xs text-rig-400">Type</span><Select value={epTracking} onValueChange={(v) => (epTracking = v as TrackingMode)} items={trackingItems} class="mt-1" /></label>
					{#if epTracking === 'group'}<label class="block"><span class="text-xs text-rig-400">Plants in group</span><input type="number" min="1" bind:value={epQuantity} class="{field} mt-1" /></label>{/if}
				</div>
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block"><span class="text-xs text-rig-400">Label <span class="text-rig-600">(optional)</span></span><input bind:value={epLabel} placeholder="Plant" class="{field} mt-1" /></label>
					<label class="block"><span class="text-xs text-rig-400">Cultivar <span class="text-rig-600">(optional)</span></span><Select value={epCultivar} onValueChange={(v) => (epCultivar = v)} items={cultivarItems(epCultivar)} class="mt-1" /></label>
				</div>
				<div class="grid gap-3 sm:grid-cols-3">
					<label class="block"><span class="text-xs text-rig-400">Pot size <span class="text-rig-600">(optional)</span></span><input type="number" min="0" step="any" bind:value={epPotSize} placeholder="e.g. 11" class="{field} mt-1" /></label>
					<label class="block"><span class="text-xs text-rig-400">Unit</span><Select value={epPotUnit} onValueChange={(v) => (epPotUnit = v as PotUnit)} items={potUnitItems} class="mt-1" /></label>
					<label class="block"><span class="text-xs text-rig-400">Pot type</span><Select value={epPotType} onValueChange={(v) => (epPotType = v)} items={potTypeItems} class="mt-1" /></label>
				</div>
				<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
					<Button variant="ghost" onclick={() => (editOpen = false)}>Cancel</Button>
					<Button onclick={saveEdit} disabled={epBusy || (epTracking === 'group' && epQuantity < 1)}>Save</Button>
				</div>
			</div>
		</Dialog>

		<Dialog bind:open={addingPlants} title="Add a plant" description="Add one plant — an individual, or a group (tray / bed / batch). Each gets its own id and history.">
			<div class="space-y-4">
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block"><span class="text-xs text-rig-400">Type</span><Select value={apTracking} onValueChange={(v) => (apTracking = v as TrackingMode)} items={trackingItems} class="mt-1" /></label>
					{#if apTracking === 'group'}<label class="block"><span class="text-xs text-rig-400">Plants in group</span><input type="number" min="1" bind:value={apQuantity} class="{field} mt-1" /></label>{/if}
				</div>
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block"><span class="text-xs text-rig-400">Label <span class="text-rig-600">(optional)</span></span><input bind:value={apLabel} placeholder={apTracking === 'group' ? 'Group' : 'Plant'} class="{field} mt-1" /></label>
					<label class="block"><span class="text-xs text-rig-400">Cultivar <span class="text-rig-600">(optional)</span></span><Select value={apCultivar} onValueChange={(v) => (apCultivar = v)} items={cultivarItems(apCultivar)} class="mt-1" /></label>
				</div>
				<label class="block"><span class="text-xs text-rig-400">Place in</span><Select value={apEnv} onValueChange={(v) => (apEnv = v)} items={envItems} class="mt-1" /></label>
				<div class="grid gap-3 sm:grid-cols-3">
					<label class="block"><span class="text-xs text-rig-400">Pot size <span class="text-rig-600">(optional)</span></span><input type="number" min="0" step="any" bind:value={apPotSize} placeholder="e.g. 11" class="{field} mt-1" /></label>
					<label class="block"><span class="text-xs text-rig-400">Unit</span><Select value={apPotUnit} onValueChange={(v) => (apPotUnit = v as PotUnit)} items={potUnitItems} class="mt-1" /></label>
					<label class="block"><span class="text-xs text-rig-400">Pot type</span><Select value={apPotType} onValueChange={(v) => (apPotType = v)} items={potTypeItems} class="mt-1" /></label>
				</div>
				<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
					<Button variant="ghost" onclick={() => (addingPlants = false)}>Cancel</Button>
					<Button onclick={addPlant} disabled={apBusy || (apTracking === 'group' && apQuantity < 1)}><Sprout size={15} /> Add plant</Button>
				</div>
			</div>
		</Dialog>
	{/if}
{/if}
