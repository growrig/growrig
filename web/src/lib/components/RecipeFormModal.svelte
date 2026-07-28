<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { toast } from '$lib/toast.svelte';
	import type {
		DoseBasis,
		FeedingRecipe,
		FeedingProduct,
		FeedingPhase,
		PhaseTrigger,
		Species
	} from '$lib/types';
	import { createRecipe, updateRecipe, type RecipeInput } from '$lib/api';
	import { Button, Dialog, Select } from '$lib/components/ui';
	import { titleCase } from '$lib/format';
	import Plus from '@lucide/svelte/icons/plus';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import ChevronUp from '@lucide/svelte/icons/chevron-up';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';

	interface Props {
		open?: boolean;
		/** Provided to edit an existing user recipe; omit to create. */
		recipe?: FeedingRecipe;
		species: Species[];
		/** Built-in recipes offered as a starting point in create mode. */
		templates?: FeedingRecipe[];
		/** Preselect a species (create mode). */
		defaultSpecies?: string;
		onSaved?: (p: FeedingRecipe) => void;
	}
	let {
		open = $bindable(false),
		recipe,
		species,
		templates = [],
		defaultSpecies,
		onSaved
	}: Props = $props();

	// Local editable shapes: dose values may be blank while typing.
	type Week = { doses: Record<string, number | null> };
	type Phase = {
		name: string;
		stage: string;
		/** '' = weekly phase; otherwise a one-shot applied at that event. */
		trigger: PhaseTrigger | '';
		doses: Record<string, number | null>;
		weeks: Week[];
	};

	const basisItems: { value: DoseBasis | ''; label: string }[] = [
		{ value: '', label: 'per L solution' },
		{ value: 'plant', label: 'per plant' },
		{ value: 'pot', label: 'per pot' },
		{ value: 'substrate', label: 'per L substrate' }
	];
	const triggerItems: { value: PhaseTrigger | ''; label: string }[] = [
		{ value: '', label: 'Weekly' },
		{ value: 'planting', label: 'Once at planting' },
		{ value: 'transplant', label: 'Once at transplant' },
		{ value: 'topdress', label: 'Once when top-dressing' }
	];

	let speciesId = $state('');
	let name = $state('');
	let brand = $state('');
	let description = $state('');
	let unit = $state('ml/L');
	let products = $state<FeedingProduct[]>([]);
	let phases = $state<Phase[]>([]);
	let templateId = $state('');
	let busy = $state(false);
	let err = $state('');

	const isEdit = $derived(!!recipe);
	const selected = $derived(species.find((s) => s.id === speciesId));
	const stages = $derived(selected?.stages ?? []);
	const canSave = $derived(!!name.trim() && !!speciesId);
	// Templates for the picker: narrow to the chosen species once one is set.
	const templateOptions = $derived(
		speciesId ? templates.filter((t) => t.species === speciesId) : templates
	);

	const title = $derived(isEdit ? 'Edit feeding recipe' : 'New feeding recipe');

	/** Server phase → local editable phase, shared by reseed and template fill. */
	function toPhase(ph: FeedingPhase): Phase {
		return {
			name: ph.name,
			stage: ph.stage ?? '',
			trigger: ph.trigger ?? '',
			doses: { ...(ph.doses ?? {}) },
			weeks: (ph.weeks ?? []).map((wk) => ({ doses: { ...wk.doses } }))
		};
	}

	// Reseed on open.
	$effect(() => {
		if (!open) return;
		speciesId = recipe?.species ?? defaultSpecies ?? '';
		name = recipe?.name ?? '';
		brand = recipe?.brand ?? '';
		description = recipe?.description ?? '';
		unit = recipe?.unit || 'ml/L';
		products = (recipe?.products ?? []).map((p) => ({ ...p }));
		phases = (recipe?.phases ?? []).map(toPhase);
		templateId = '';
		err = '';
	});

	// Prefill everything but the name from the chosen built-in template.
	function applyTemplate() {
		const t = templates.find((x) => x.id === templateId);
		if (!t) return;
		speciesId = t.species;
		brand = t.brand;
		description = t.description;
		unit = t.unit || 'ml/L';
		products = (t.products ?? []).map((p) => ({ ...p }));
		phases = (t.phases ?? []).map(toPhase);
	}

	function nextProductKey(): string {
		const used = new Set(products.map((p) => p.key));
		let n = 1;
		while (used.has(`p${n}`)) n++;
		return `p${n}`;
	}
	function addProduct() {
		products = [...products, { key: nextProductKey(), label: '', unit: '' }];
	}
	function removeProduct(i: number) {
		const key = products[i].key;
		products = products.filter((_, k) => k !== i);
		for (const ph of phases) {
			delete ph.doses[key];
			for (const wk of ph.weeks) delete wk.doses[key];
		}
	}

	function addPhase() {
		phases = [
			...phases,
			{ name: `Phase ${phases.length + 1}`, stage: '', trigger: '', doses: {}, weeks: [{ doses: {} }] }
		];
	}
	function removePhase(i: number) {
		phases = phases.filter((_, k) => k !== i);
	}
	function movePhase(i: number, dir: -1 | 1) {
		const j = i + dir;
		if (j < 0 || j >= phases.length) return;
		const next = [...phases];
		[next[i], next[j]] = [next[j], next[i]];
		phases = next;
	}
	function addWeek(pi: number) {
		phases[pi].weeks = [...phases[pi].weeks, { doses: {} }];
	}
	function removeWeek(pi: number, wi: number) {
		phases[pi].weeks = phases[pi].weeks.filter((_, k) => k !== wi);
	}

	function unitFor(p: FeedingProduct): string {
		return p.unit?.trim() || unit.trim();
	}

	/** Drop blank/zero entries and coerce the strings the inputs hand back. */
	function cleanDoses(in_: Record<string, number | null>): Record<string, number> {
		const out: Record<string, number> = {};
		for (const [k, v] of Object.entries(in_)) {
			const n = typeof v === 'number' ? v : parseFloat(String(v));
			if (Number.isFinite(n) && n > 0) out[k] = n;
		}
		return out;
	}

	async function save() {
		if (!canSave) return;
		busy = true;
		err = '';
		try {
			const input: RecipeInput = {
				species: speciesId,
				name: name.trim(),
				brand: brand.trim(),
				description: description.trim(),
				unit: unit.trim(),
				products: products
					.filter((p) => p.label.trim())
					.map((p) => ({
						key: p.key,
						label: p.label.trim(),
						...(p.unit?.trim() ? { unit: p.unit.trim() } : {}),
						...(p.basis ? { basis: p.basis } : {})
					})),
				phases: phases
					.filter((ph) => ph.name.trim())
					.map((ph) => ({
						name: ph.name.trim(),
						...(ph.stage ? { stage: ph.stage } : {}),
						// One-shot phases carry a single dose map and no weeks; the
						// server drops whichever half doesn't match the trigger anyway.
						...(ph.trigger
							? { trigger: ph.trigger, doses: cleanDoses(ph.doses), weeks: [] }
							: { weeks: ph.weeks.map((wk) => ({ doses: cleanDoses(wk.doses) })) })
					}))
			};
			const saved = isEdit ? await updateRecipe(recipe!.id, input) : await createRecipe(input);
			open = false;
			toast.success(isEdit ? 'Recipe updated' : 'Recipe created', { description: saved.name });
			onSaved?.(saved);
		} catch (e) {
			err = errMsg(e, 'Save failed');
		} finally {
			busy = false;
		}
	}

	// Full-width top fields keep w-full; row inputs must NOT (w-full fights flex
	// sizing and collapses the name inputs), so they use `cell` + explicit widths.
	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';
	const cell =
		'rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';
	const iconBtn = 'rounded p-1.5 text-rig-400 transition-colors hover:text-rig-100 disabled:opacity-30';
</script>

<Dialog
	bind:open
	{title}
	size="3xl"
	description="A nutrient schedule: products dosed per week across the phases of a grow."
>
	<div class="space-y-5">
		{#if err}<p class="text-xs text-danger">{err}</p>{/if}

		<!-- Core fields -->
		<div class="grid gap-3 sm:grid-cols-2">
			<label class="block">
				<span class="text-xs text-rig-400">Name</span>
				<input bind:value={name} placeholder="e.g. My veg + bloom" class="{field} mt-1" />
			</label>
			<label class="block">
				<span class="text-xs text-rig-400">Species</span>
				<Select
					class="mt-1"
					bind:value={speciesId}
					placeholder="Select a species…"
					items={species.map((sp) => ({ value: sp.id, label: sp.label }))}
				/>
			</label>
		</div>

		<!-- Start from a built-in schedule (create mode only). -->
		{#if !isEdit && templateOptions.length}
			<label class="block rounded-lg border border-rig-800 bg-rig-900/40 p-3">
				<span class="text-xs font-semibold uppercase tracking-wide text-rig-400">Start from a built-in template</span>
				<Select
					class="mt-1.5"
					bind:value={templateId}
					placeholder="Start blank"
					onValueChange={applyTemplate}
					items={[
						{ value: '', label: 'Start blank' },
						...templateOptions.map((t) => ({
							value: t.id,
							label: `${t.name}${t.brand ? ` — ${t.brand}` : ''}`
						}))
					]}
				/>
				<span class="mt-1 block text-[11px] text-rig-500">Fills the products, phases and weeks below — you can edit everything after.</span>
			</label>
		{/if}

		<div class="grid gap-3 sm:grid-cols-2">
			<label class="block">
				<span class="text-xs text-rig-400">Brand <span class="text-rig-600">(optional)</span></span>
				<input bind:value={brand} placeholder="e.g. BioBizz" class="{field} mt-1" />
			</label>
			<label class="block">
				<span class="text-xs text-rig-400">Default unit</span>
				<input bind:value={unit} placeholder="ml/L" class="{field} mt-1" />
			</label>
		</div>
		<label class="block">
			<span class="text-xs text-rig-400">Description <span class="text-rig-600">(optional)</span></span>
			<textarea bind:value={description} rows="2" class="{field} mt-1"></textarea>
		</label>

		<!-- Products -->
		<section class="space-y-2">
			<div class="flex items-center justify-between">
				<h3 class="text-xs font-semibold uppercase tracking-wide text-rig-400">Products</h3>
				<button
					type="button"
					onclick={addProduct}
					class="inline-flex items-center gap-1 rounded-md border border-rig-700 px-2 py-1 text-xs text-rig-200 hover:border-leaf hover:text-white"
				>
					<Plus size={13} /> Add product
				</button>
			</div>
			{#if products.length === 0}
				<p class="text-xs text-rig-500">Add the nutrient lines this schedule doses (e.g. Bio·Grow, Bio·Bloom).</p>
			{:else}
				<div class="space-y-1.5">
					<div class="flex items-center gap-2 px-0.5 text-[11px] text-rig-500">
						<span class="flex-1">Product name</span>
						<span class="w-20 shrink-0">Unit</span>
						<span class="w-36 shrink-0">Dosed</span>
						<span class="w-7 shrink-0"></span>
					</div>
					{#each products as p, i (p.key)}
						<div class="flex items-center gap-2">
							<input bind:value={p.label} placeholder="e.g. Bio·Grow" class="{cell} min-w-0 flex-1" />
							<input
								bind:value={p.unit}
								placeholder={unit || 'unit'}
								class="{cell} w-20 shrink-0 text-center"
								title="Unit override (blank uses the default)"
							/>
							<Select
								class="w-36 shrink-0"
								bind:value={p.basis}
								placeholder="per L solution"
								items={basisItems}
							/>
							<button type="button" onclick={() => removeProduct(i)} aria-label="Remove product" class={iconBtn}>
								<Trash2 size={14} />
							</button>
						</div>
					{/each}
				</div>
			{/if}
		</section>

		<!-- Phases -->
		<section class="space-y-3">
			<div class="flex items-center justify-between">
				<h3 class="text-xs font-semibold uppercase tracking-wide text-rig-400">Phases &amp; weeks</h3>
				<button
					type="button"
					onclick={addPhase}
					class="inline-flex items-center gap-1 rounded-md border border-rig-700 px-2 py-1 text-xs text-rig-200 hover:border-leaf hover:text-white"
				>
					<Plus size={13} /> Add phase
				</button>
			</div>
			{#if phases.length === 0}
				<p class="text-xs text-rig-500">Add phases (e.g. Vegetative, Flowering) and the weeks within each.</p>
			{/if}

			{#each phases as phase, pi (pi)}
				<div class="rounded-lg border border-rig-800 bg-rig-900/40 p-3">
					<div class="mb-2 flex items-center gap-2">
						<input bind:value={phase.name} placeholder="Phase name" class="{cell} min-w-0 flex-1" />
						<Select
							class="w-44 shrink-0"
							bind:value={phase.trigger}
							placeholder="Weekly"
							items={triggerItems}
						/>
						<Select
							class="w-40 shrink-0"
							bind:value={phase.stage}
							placeholder="No stage link"
							items={[
								{ value: '', label: 'No stage link' },
								...stages.map((st) => ({ value: st.name, label: titleCase(st.name) }))
							]}
						/>
						<button type="button" onclick={() => movePhase(pi, -1)} disabled={pi === 0} aria-label="Move up" class={iconBtn}>
							<ChevronUp size={14} />
						</button>
						<button
							type="button"
							onclick={() => movePhase(pi, 1)}
							disabled={pi === phases.length - 1}
							aria-label="Move down"
							class={iconBtn}
						>
							<ChevronDown size={14} />
						</button>
						<button type="button" onclick={() => removePhase(pi)} aria-label="Remove phase" class={iconBtn}>
							<Trash2 size={14} />
						</button>
					</div>

					{#if products.length === 0}
						<p class="text-xs text-rig-500">Add products above to dose them here.</p>
					{:else if phase.trigger}
						<!-- One-shot: a single dose per product, no week timeline. -->
						<p class="mb-2 text-xs text-rig-500">
							Applied once {triggerItems
								.find((t) => t.value === phase.trigger)
								?.label.toLowerCase()
								.replace(/^once /, '')} — leave a product blank if it isn't part of this
							application.
						</p>
						<div class="space-y-1">
							{#each products as p (p.key)}
								<div class="flex items-center gap-2">
									<span class="min-w-0 flex-1 truncate text-sm text-rig-200">{p.label || '—'}</span>
									<input
										type="number"
										inputmode="decimal"
										step="any"
										min="0"
										bind:value={phase.doses[p.key]}
										class="w-16 rounded border border-rig-700 bg-rig-950 px-1.5 py-1 text-center text-xs focus:border-leaf focus:outline-none"
									/>
									<span class="w-28 shrink-0 text-[11px] text-rig-500">
										{unitFor(p)}{p.basis
											? ` ${basisItems.find((b) => b.value === p.basis)?.label}`
											: ''}
									</span>
								</div>
							{/each}
						</div>
					{:else}
						<div class="overflow-x-auto">
							<table class="w-full border-collapse text-sm">
								<thead>
									<tr class="text-left text-xs text-rig-500">
										<th class="sticky left-0 z-10 bg-rig-900/40 py-1 pr-3 font-medium">Product</th>
										{#each phase.weeks as _wk, wi (wi)}
											<th class="px-1 py-1 text-center font-medium">
												<div class="flex items-center justify-center gap-0.5">
													Wk {wi + 1}
													<button
														type="button"
														onclick={() => removeWeek(pi, wi)}
														aria-label="Remove week"
														class="text-rig-600 hover:text-danger"
													>
														<Trash2 size={11} />
													</button>
												</div>
											</th>
										{/each}
										<th class="px-1 py-1">
											<button
												type="button"
												onclick={() => addWeek(pi)}
												aria-label="Add week"
												class="inline-flex items-center gap-0.5 rounded border border-rig-700 px-1.5 py-0.5 text-[11px] text-rig-300 hover:border-leaf hover:text-white"
											>
												<Plus size={11} /> Week
											</button>
										</th>
									</tr>
								</thead>
								<tbody>
									{#each products as p (p.key)}
										<tr class="border-t border-rig-800/60">
											<td class="sticky left-0 z-10 whitespace-nowrap bg-rig-900/40 py-1 pr-3">
												<span class="text-rig-200">{p.label || '—'}</span>
												<span class="ml-1 text-[10px] text-rig-500">
													{unitFor(p)}{p.basis
														? ` ${basisItems.find((b) => b.value === p.basis)?.label}`
														: ''}
												</span>
											</td>
											{#each phase.weeks as _wk, wi (wi)}
												<td class="px-1 py-1 text-center">
													<input
														type="number"
														inputmode="decimal"
														step="any"
														min="0"
														bind:value={phase.weeks[wi].doses[p.key]}
														class="w-14 rounded border border-rig-700 bg-rig-950 px-1.5 py-1 text-center text-xs focus:border-leaf focus:outline-none"
													/>
												</td>
											{/each}
											<td></td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
						{#if phase.weeks.length === 0}
							<p class="mt-1 text-xs text-rig-500">No weeks yet — add one to start dosing.</p>
						{/if}
					{/if}
				</div>
			{/each}
		</section>

		<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
			<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
			<Button onclick={save} disabled={busy || !canSave}>{isEdit ? 'Save' : 'Create'}</Button>
		</div>
	</div>
</Dialog>
