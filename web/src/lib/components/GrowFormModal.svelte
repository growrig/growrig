<script lang="ts">
	import { onMount } from 'svelte';
	import { errMsg } from '$lib/errors';
	import type {
		Grow,
		Species,
		StagePresets,
		Cultivar,
		Environment,
		TrackingMode,
		PotUnit,
		MediumRecipe
	} from '$lib/types';
	import { createGrow, updateGrow, getSpecies, getCultivars, getEnvironments, createPlant, speciesIconURL, getMediumRecipes, getMediumRecipeTemplates } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import { Button, Dialog, Select, DatePicker } from '$lib/components/ui';
	import { titleCase } from '$lib/format';
	import { stageColorAt } from '$lib/stageColor';
	import Plus from '@lucide/svelte/icons/plus';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import Sprout from '@lucide/svelte/icons/sprout';

	// Crop-neutral fallbacks, mirroring Grow Core's species defaults, used until
	// the species catalog loads or when a species curates no list of its own.
	const FALLBACK_MEDIA = ['soil', 'coco', 'soilless', 'hydroponic', 'aeroponic'];
	const FALLBACK_NUTRIENTS = ['organic', 'mineral', 'living-soil'];
	const POT_TYPES = ['fabric', 'plastic', 'terracotta', 'air-pot', 'other'];

	interface Props {
		open?: boolean;
		/** Provided in edit mode; omit to create. */
		grow?: Grow;
		presets: StagePresets;
		onSaved?: (grow: Grow) => void;
	}
	let { open = $bindable(false), grow, presets, onSaved }: Props = $props();

	// A fresh grow walks through four steps; editing skips the final Plants step,
	// since plant units are managed on the grow's detail page.
	const isEdit = $derived(!!grow);
	const totalSteps = $derived(isEdit ? 3 : 4);
	let step = $state(1);

	let name = $state('');
	let species = $state('');
	let startDate = $state(new Date().toISOString().slice(0, 10));
	let notes = $state('');
	let busy = $state(false);
	let err = $state('');

	// Growing setup.
	let medium = $state('');
	let mediumRecipeId = $state('');
	let mediumDetails = $state('');
	// User recipes and built-in mixes are offered in one list: a built-in is a
	// perfectly good answer to "what is this grow mixed from".
	let mediumRecipes = $state<MediumRecipe[]>([]);
	let nutrientMethod = $state('');
	let potSize = $state<number | null>(null);
	let potUnit = $state('L');
	let potType = $state('');

	// Step 3: plants to seed the grow with (create mode only).
	interface PlantDraft {
		tracking: TrackingMode;
		quantity: number;
		label: string;
		cultivar: string;
		environmentId: string;
		potSize: number | null;
		potUnit: PotUnit;
		potType: string;
	}
	let plants = $state<PlantDraft[]>([]);

	let catalog = $state<Species[]>([]);
	let cultivars = $state<Cultivar[]>([]);
	let environments = $state<Environment[]>([]);
	// Species icons are SVGs served from the catalog; inline their markup so
	// `currentColor` tracks the tile's text colour (an <img> can't do that).
	let iconSvgs = $state<Record<string, string>>({});
	onMount(() => {
		getSpecies()
			.then((s) => {
				catalog = s;
				for (const sp of s) {
					if (!sp.icon) continue;
					fetch(speciesIconURL(sp.id))
						.then((r) => (r.ok ? r.text() : ''))
						.then((txt) => {
							if (txt.trimStart().startsWith('<svg')) iconSvgs = { ...iconSvgs, [sp.id]: txt };
						})
						.catch(() => {});
				}
			})
			.catch(() => {});
		getCultivars().then((c) => (cultivars = c)).catch(() => {});
		getEnvironments().then((e) => (environments = e)).catch(() => {});
		Promise.all([getMediumRecipes(), getMediumRecipeTemplates()])
			.then(([mine, builtin]) => (mediumRecipes = [...mine, ...builtin]))
			.catch(() => {});
	});

	// Medium recipes offered for the grow: crop-tagged ones for other species are
	// dropped, untagged ones always apply.
	const mediumRecipeItems = $derived([
		{ value: '', label: '— No recipe —' },
		...mediumRecipes
			.filter((r) => !r.species || r.species === species)
			.map((r) => ({
				value: r.id,
				label: r.source === 'builtin' ? `${r.name} (built-in)` : r.name
			}))
	]);
	const selectedMediumRecipe = $derived(mediumRecipes.find((r) => r.id === mediumRecipeId));

	/** "70% coco coir · 30% perlite" — the mix at a glance. */
	function mediumSummary(r: MediumRecipe): string {
		return (r.components ?? [])
			.map((c) => `${c.percent}% ${c.label.toLowerCase()}`)
			.join(' · ');
	}

	// Species picker tiles: the rich catalog when loaded, else the preset ids so
	// the picker still works before (or without) the catalog.
	const speciesTiles = $derived(
		catalog.length
			? catalog.map((s) => ({ id: s.id, label: s.label, hasIcon: !!s.icon }))
			: Object.keys(presets).map((k) => ({ id: k, label: titleCase(k), hasIcon: false }))
	);

	// Reseed on open.
	$effect(() => {
		if (!open) return;
		step = 1;
		err = '';
		name = grow?.name ?? '';
		species = grow?.species ?? '';
		startDate = (grow?.startedAt ?? new Date().toISOString()).slice(0, 10);
		notes = grow?.notes ?? '';
		const s = grow?.setup;
		medium = s?.medium ?? '';
		mediumRecipeId = s?.mediumRecipeId ?? '';
		mediumDetails = s?.mediumDetails ?? '';
		nutrientMethod = s?.nutrientMethod ?? '';
		potSize = s?.potSize ?? null;
		potUnit = s?.potUnit || 'L';
		potType = s?.potType ?? '';
		plants = [];
		stageSource = ''; // force the stage selection to rebuild for this grow
	});

	const presetKeys = $derived(Object.keys(presets));
	// Stages are derived from the chosen species, not entered by hand.
	const derivedStages = $derived(presets[species.trim().toLowerCase()] ?? []);

	// The chosen species curates the media / nutrient lists; fall back to the
	// crop-neutral defaults until the catalog loads or when a species curates none.
	const selectedSpecies = $derived(catalog.find((s) => s.id === species.trim().toLowerCase()));

	// Stage sequence: required stages are always in; optional ones (propagation /
	// post-harvest) are grower-chosen via checkboxes. Prefer the catalog's rich
	// stage metadata; fall back to the preset names (all treated as required)
	// until the catalog loads.
	interface StageDef {
		name: string;
		optional: boolean;
		defaultOn: boolean;
		typicalDays?: number;
	}
	const stageDefs = $derived<StageDef[]>(
		selectedSpecies?.stages?.length
			? selectedSpecies.stages.map((s) => ({
					name: s.name,
					optional: !!s.optional,
					defaultOn: !s.optional || s.default !== false,
					typicalDays: s.typicalDays
				}))
			: derivedStages.map((n) => ({ name: n, optional: false, defaultOn: true }))
	);

	let selectedStages = $state<string[]>([]);
	// Signature the current selection was built from; rebuild when the species
	// changes or the catalog's stage metadata first arrives.
	let stageSource = '';
	$effect(() => {
		const sp = species.trim().toLowerCase();
		const defs = stageDefs;
		const sig = `${sp}|${defs.length}`;
		if (sig === stageSource) return;
		stageSource = sig;
		if (!defs.length) {
			selectedStages = [];
			return;
		}
		if (grow && grow.species === sp && grow.stages?.length) {
			// Editing: keep the grow's existing optional-stage choices.
			const set = new Set(grow.stages);
			selectedStages = defs.filter((d) => !d.optional || set.has(d.name)).map((d) => d.name);
		} else {
			selectedStages = defs.filter((d) => d.defaultOn).map((d) => d.name);
		}
	});

	function toggleStage(name: string) {
		selectedStages = selectedStages.includes(name)
			? selectedStages.filter((s) => s !== name)
			: [...selectedStages, name];
	}
	// Canonical, ordered selection sent to the server: required stages plus the
	// chosen optional ones, in the species' order.
	const orderedStages = $derived(
		stageDefs.filter((d) => !d.optional || selectedStages.includes(d.name)).map((d) => d.name)
	);

	const mediaOptions = $derived(selectedSpecies?.media?.options?.length ? selectedSpecies.media!.options : FALLBACK_MEDIA);
	const nutrientOptions = $derived(
		selectedSpecies?.nutrientMethods?.length ? selectedSpecies.nutrientMethods : FALLBACK_NUTRIENTS
	);
	const asItems = (vals: string[]) => vals.map((v) => ({ value: v, label: titleCase(v) }));

	const trackingItems = [
		{ value: 'individual', label: 'Individual plant' },
		{ value: 'group', label: 'Group (tray / bed / batch)' }
	];
	const potUnitItems = [
		{ value: 'L', label: 'liters (L)' },
		{ value: 'gal', label: 'gallons' }
	];
	const potTypeItems = [{ value: '', label: '—' }, ...asItems(POT_TYPES)];
	const envItems = $derived(environments.map((e) => ({ value: e.id, label: e.name })));

	// Cultivars offered in step 3 are scoped to the grow's species.
	const speciesCultivars = $derived(cultivars.filter((c) => c.species === species.trim().toLowerCase()));
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

	function addPlantDraft() {
		plants = [
			...plants,
			{
				tracking: 'individual',
				quantity: 1,
				label: '',
				cultivar: '',
				environmentId: environments[0]?.id ?? '',
				// Inherit the grow's default container as a starting point.
				potSize: potSize && potSize > 0 ? potSize : null,
				potUnit: (potUnit as PotUnit) || 'L',
				potType
			}
		];
	}
	function removePlantDraft(i: number) {
		plants = plants.filter((_, idx) => idx !== i);
	}

	// Step 1 requires a species and a name; step 2 a non-empty stage sequence
	// (guaranteed once a species is chosen). Later steps are optional, so the
	// wizard can be finished from step 3 onward.
	const detailsValid = $derived(!!species.trim() && !!name.trim());
	const stagesValid = $derived(orderedStages.length > 0);
	const canAdvance = $derived(step === 1 ? detailsValid : step === 2 ? stagesValid : true);
	const canFinish = $derived(detailsValid && stagesValid && !busy);

	function next() {
		if (step < totalSteps && canAdvance) step = step + 1;
	}
	function back() {
		if (step > 1) step = step - 1;
	}

	async function save() {
		if (!canFinish) return;
		busy = true;
		err = '';
		try {
			const input = {
				name: name.trim(),
				species: species.trim().toLowerCase(),
				startedAt: startDate,
				notes,
				stages: orderedStages,
				setup: {
					medium,
					mediumRecipeId,
					mediumDetails: mediumDetails.trim(),
					nutrientMethod,
					potSize: potSize && potSize > 0 ? potSize : 0,
					potUnit: potSize && potSize > 0 ? potUnit : '',
					potType: potSize && potSize > 0 ? potType : ''
				}
			};
			const saved = grow ? await updateGrow(grow.id, input) : await createGrow(input);

			// Seed the new grow with any drafted plant units.
			if (!grow) {
				for (const p of plants) {
					await createPlant(saved.id, {
						tracking: p.tracking,
						quantity: p.tracking === 'group' ? Math.max(1, p.quantity) : 1,
						label: p.label.trim() || undefined,
						cultivar: p.cultivar,
						environmentId: p.environmentId || undefined,
						...(p.potSize && p.potSize > 0
							? { potSize: p.potSize, potUnit: p.potUnit, potType: p.potType }
							: {})
					});
				}
			}

			open = false;
			toast.success(isEdit ? 'Grow updated' : 'Grow created', {
				description: isEdit ? saved.name : `“${saved.name}” is ready to track.`
			});
			onSaved?.(saved);
		} catch (e) {
			err = errMsg(e, 'Save failed');
		} finally {
			busy = false;
		}
	}

	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';
	const stepLabels = ['Details', 'Stages', 'Growing setup', 'Plants'];
</script>

<Dialog
	bind:open
	size="xl"
	title={grow ? 'Edit grow' : 'New grow'}
	description="A crop-neutral cultivation run with a configurable stage sequence."
>
	<div class="space-y-4">
		<!-- Step indicator -->
		<ol class="flex items-center gap-2 text-xs">
			{#each Array(totalSteps) as _, i (i)}
				{@const n = i + 1}
				<li class="flex items-center gap-2">
					<span
						class="inline-flex h-6 w-6 items-center justify-center rounded-full border text-[11px] font-medium
							{step === n
							? 'border-leaf bg-leaf text-rig-950'
							: step > n
								? 'border-leaf/60 text-leaf'
								: 'border-rig-700 text-rig-500'}"
					>
						{n}
					</span>
					<span class={step === n ? 'text-rig-100' : 'text-rig-500'}>{stepLabels[i]}</span>
					{#if n < totalSteps}<span class="mx-1 h-px w-6 bg-rig-700"></span>{/if}
				</li>
			{/each}
		</ol>

		{#if err}<p class="text-xs text-danger">{err}</p>{/if}

		<!-- Step 1: species, name, start date -->
		{#if step === 1}
			<div class="block">
				<span class="text-xs text-rig-400">Species</span>
				<div class="mt-1.5 grid grid-cols-2 gap-3 sm:grid-cols-3">
					{#each speciesTiles as t (t.id)}
						{@const sel = species.trim().toLowerCase() === t.id}
						<button
							type="button"
							onclick={() => (species = t.id)}
							class="flex flex-col items-center justify-center gap-2 rounded-xl border p-4 transition-colors
								{sel
								? 'border-leaf bg-leaf/10 text-leaf'
								: 'border-rig-800 bg-rig-900/30 text-rig-300 hover:border-rig-600 hover:text-rig-100'}"
						>
							<span class="grid h-10 w-10 place-items-center [&_svg]:h-9 [&_svg]:w-9">
								{#if t.hasIcon && iconSvgs[t.id]}
									<!-- eslint-disable-next-line svelte/no-at-html-tags -->
									{@html iconSvgs[t.id]}
								{:else}
									<Sprout size={30} />
								{/if}
							</span>
							<span class="text-sm font-medium {sel ? 'text-leaf' : 'text-rig-200'}">{t.label}</span>
						</button>
					{/each}
				</div>
			</div>
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="block">
					<span class="text-xs text-rig-400">Name</span>
					<input bind:value={name} placeholder="e.g. Summer basil" class="{field} mt-1" />
				</label>
				<label class="block">
					<span class="text-xs text-rig-400">Start date</span>
					<DatePicker class="mt-1" bind:value={startDate} />
				</label>
			</div>

		<!-- Step 2: stage sequence -->
		{:else if step === 2}
			<div class="block">
				<span class="text-xs text-rig-400">Stages</span>
				{#if stageDefs.length}
					<div class="mt-1.5 overflow-hidden rounded-lg border border-rig-800">
						{#each stageDefs as st, i (st.name)}
							{@const on = !st.optional || selectedStages.includes(st.name)}
							<label
								class="flex items-center gap-3 border-b border-rig-800/60 px-3 py-2 last:border-0 {st.optional ? 'cursor-pointer hover:bg-rig-800/40' : ''} {on ? '' : 'opacity-50'}"
							>
								<input
									type="checkbox"
									checked={on}
									disabled={!st.optional}
									onchange={() => toggleStage(st.name)}
									class="h-4 w-4 shrink-0 accent-leaf disabled:opacity-60"
								/>
								<span class="h-2.5 w-2.5 shrink-0 rounded-full" style="background:{stageColorAt(i)}"></span>
								<span class="flex-1 text-sm capitalize {on ? 'text-rig-200' : 'text-rig-400'}">{st.name}</span>
								{#if st.typicalDays}<span class="text-xs tabular-nums text-rig-500">~{st.typicalDays}d</span>{/if}
								{#if !st.optional}<span class="text-[10px] uppercase tracking-wide text-rig-600">required</span>{/if}
							</label>
						{/each}
					</div>
					<span class="mt-1.5 block text-xs text-rig-500">Required stages are always included; toggle the optional propagation and post-harvest phases. The first selected stage is the starting stage.</span>
				{:else}
					<p class="mt-1 text-xs text-rig-500">Pick a species to set the stage sequence.</p>
				{/if}
			</div>

		<!-- Step 3: growing setup -->
		{:else if step === 3}
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="block">
					<span class="text-xs text-rig-400">Growing medium</span>
					<Select class="mt-1" bind:value={medium} placeholder="Select a medium…" items={asItems(mediaOptions)} />
				</label>
				<label class="block">
					<span class="text-xs text-rig-400">Nutrient method <span class="text-rig-600">(optional)</span></span>
					<Select class="mt-1" bind:value={nutrientMethod} placeholder="—" items={asItems(nutrientOptions)} />
				</label>
			</div>
			<label class="block">
				<span class="text-xs text-rig-400">Medium recipe <span class="text-rig-600">(optional)</span></span>
				<Select
					class="mt-1"
					bind:value={mediumRecipeId}
					placeholder="— No recipe —"
					items={mediumRecipeItems}
				/>
				<span class="mt-1 block text-[11px] text-rig-500">
					{#if selectedMediumRecipe}
						{mediumSummary(selectedMediumRecipe)} · the default for this grow's plants, which
						each plant can override.
					{:else}
						What the substrate is mixed from. Manage recipes in the Library.
					{/if}
				</span>
			</label>
			<label class="block">
				<span class="text-xs text-rig-400">Medium details <span class="text-rig-600">(optional)</span></span>
				<input bind:value={mediumDetails} placeholder="e.g. BioBizz Light-Mix + worm castings + perlite" class="{field} mt-1" />
			</label>
			<div class="grid gap-3 sm:grid-cols-3">
				<label class="block">
					<span class="text-xs text-rig-400">Default pot size <span class="text-rig-600">(optional)</span></span>
					<input type="number" min="0" step="any" bind:value={potSize} placeholder="e.g. 20" class="{field} mt-1" />
				</label>
				<label class="block">
					<span class="text-xs text-rig-400">Unit</span>
					<Select class="mt-1" bind:value={potUnit} items={potUnitItems} />
				</label>
				<label class="block">
					<span class="text-xs text-rig-400">Pot type</span>
					<Select class="mt-1" bind:value={potType} placeholder="—" items={asItems(POT_TYPES)} />
				</label>
			</div>
			<p class="text-xs text-rig-500">The default container seeds each new plant's pot. Irrigation is configured on the environment.</p>
			<label class="block">
				<span class="text-xs text-rig-400">Notes</span>
				<textarea bind:value={notes} rows="2" class="{field} mt-1"></textarea>
			</label>

		<!-- Step 4: plants (create mode only) -->
		{:else}
			<div class="space-y-3">
				<p class="text-xs text-rig-500">
					Add the plants this grow starts with — an individual, or a group (tray / bed / batch). Each gets its own
					id and history. You can skip this and add plants later.
				</p>
				{#if plants.length === 0}
					<p class="rounded-md border border-dashed border-rig-700 px-3 py-6 text-center text-xs text-rig-500">
						No plants yet.
					</p>
				{/if}
				{#each plants as p, i (i)}
					<div class="space-y-3 rounded-lg border border-rig-800 bg-rig-950/40 p-3">
						<div class="flex items-center justify-between">
							<span class="text-xs font-medium text-rig-300">Plant {i + 1}</span>
							<button
								type="button"
								class="rounded p-1 text-rig-500 hover:text-danger"
								aria-label="Remove plant"
								onclick={() => removePlantDraft(i)}
							>
								<Trash2 size={15} />
							</button>
						</div>
						<div class="grid gap-3 sm:grid-cols-2">
							<label class="block"><span class="text-xs text-rig-400">Type</span>
								<Select value={p.tracking} onValueChange={(v) => (p.tracking = v as TrackingMode)} items={trackingItems} class="mt-1" /></label>
							{#if p.tracking === 'group'}
								<label class="block"><span class="text-xs text-rig-400">Plants in group</span>
									<input type="number" min="1" bind:value={p.quantity} class="{field} mt-1" /></label>
							{/if}
						</div>
						<div class="grid gap-3 sm:grid-cols-2">
							<label class="block"><span class="text-xs text-rig-400">Label <span class="text-rig-600">(optional)</span></span>
								<input bind:value={p.label} placeholder={p.tracking === 'group' ? 'Group' : 'Plant'} class="{field} mt-1" /></label>
							<label class="block"><span class="text-xs text-rig-400">Cultivar <span class="text-rig-600">(optional)</span></span>
								<Select value={p.cultivar} onValueChange={(v) => (p.cultivar = v)} items={cultivarItems(p.cultivar)} class="mt-1" /></label>
						</div>
						<label class="block"><span class="text-xs text-rig-400">Place in</span>
							<Select value={p.environmentId} onValueChange={(v) => (p.environmentId = v)} placeholder="Select an environment…" items={envItems} class="mt-1" /></label>
						<div class="grid gap-3 sm:grid-cols-3">
							<label class="block"><span class="text-xs text-rig-400">Pot size <span class="text-rig-600">(optional)</span></span>
								<input type="number" min="0" step="any" bind:value={p.potSize} placeholder="e.g. 11" class="{field} mt-1" /></label>
							<label class="block"><span class="text-xs text-rig-400">Unit</span>
								<Select value={p.potUnit} onValueChange={(v) => (p.potUnit = v as PotUnit)} items={potUnitItems} class="mt-1" /></label>
							<label class="block"><span class="text-xs text-rig-400">Pot type</span>
								<Select value={p.potType} onValueChange={(v) => (p.potType = v)} items={potTypeItems} class="mt-1" /></label>
						</div>
					</div>
				{/each}
				<Button variant="ghost" onclick={addPlantDraft}><Plus size={15} /> Add plant</Button>
			</div>
		{/if}

		<!-- Footer navigation -->
		<div class="flex items-center justify-between gap-2 border-t border-rig-800 pt-4">
			<div>
				{#if step > 1}
					<Button variant="ghost" onclick={back}>Back</Button>
				{/if}
			</div>
			<div class="flex gap-2">
				<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
				{#if step < totalSteps}
					<Button onclick={next} disabled={!canAdvance}>Next</Button>
				{:else}
					<Button onclick={save} disabled={!canFinish}>{grow ? 'Save' : 'Create grow'}</Button>
				{/if}
			</div>
		</div>
	</div>
</Dialog>
