<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { toast } from '$lib/toast.svelte';
	import { onMount } from 'svelte';
	import type { InventoryProduct, MediumRecipe, Species } from '$lib/types';
	import {
		createMediumRecipe,
		updateMediumRecipe,
		getInventoryProducts,
		type MediumRecipeInput
	} from '$lib/api';
	import { Button, Dialog, Select } from '$lib/components/ui';
	import Plus from '@lucide/svelte/icons/plus';
	import Trash2 from '@lucide/svelte/icons/trash-2';

	interface Props {
		open?: boolean;
		/** Provided to edit an existing user recipe; omit to create. */
		recipe?: MediumRecipe;
		species: Species[];
		/** Built-in recipes offered as a starting point in create mode. */
		templates?: MediumRecipe[];
		onSaved?: (r: MediumRecipe) => void;
	}
	let {
		open = $bindable(false),
		recipe,
		species,
		templates = [],
		onSaved
	}: Props = $props();

	// Local editable shapes: amounts may be blank while typing. `productId` is ''
	// for a custom ingredient the user types themselves.
	type Component = { productId: string; label: string; percent: number | null };
	type Amendment = { productId: string; label: string; amount: number | null; unit: string };

	let name = $state('');
	let description = $state('');
	let speciesId = $state('');
	let base = $state('');
	let components = $state<Component[]>([]);
	let amendments = $state<Amendment[]>([]);
	let templateId = $state('');
	let busy = $state(false);
	let err = $state('');

	// Ingredients are catalog products wherever possible — that is where a
	// brand, image and description live, which is why the recipe carries no
	// brand of its own. Anything the catalog doesn't know (perlite, a local
	// compost) is still typed in by hand.
	let products = $state<InventoryProduct[]>([]);
	onMount(() => {
		getInventoryProducts('consumables')
			.then((p) => (products = p))
			.catch(() => {});
	});
	const CUSTOM = { value: '', label: 'Custom — type a name' };
	// Growing media for the bulk ingredients; everything else can be an
	// amendment, so that picker offers the whole consumables catalog.
	const mediaItems = $derived([
		CUSTOM,
		...products
			.filter((p) => p.attributes?.kind === 'media')
			.map((p) => ({ value: p.id, label: p.name }))
	]);
	const amendmentItems = $derived([CUSTOM, ...products.map((p) => ({ value: p.id, label: p.name }))]);

	/** Picking a product snapshots its name; clearing it frees the text field. */
	function onComponentProduct(i: number) {
		const p = products.find((x) => x.id === components[i].productId);
		components[i].label = p ? p.name : '';
	}
	function onAmendmentProduct(i: number) {
		const p = products.find((x) => x.id === amendments[i].productId);
		amendments[i].label = p ? p.name : '';
	}

	const isEdit = $derived(!!recipe);
	const title = $derived(isEdit ? 'Edit medium recipe' : 'New medium recipe');
	const canSave = $derived(
		!!name.trim() && components.some((c) => c.label.trim() && (c.percent ?? 0) > 0)
	);

	// The mix has to describe a whole substrate, so surface the running total
	// rather than silently rescaling what the user typed.
	const total = $derived(
		components.reduce((n, c) => n + (typeof c.percent === 'number' ? c.percent : 0), 0)
	);
	const totalOk = $derived(Math.abs(total - 100) < 0.01);

	const baseItems = [
		{ value: '', label: '— None —' },
		{ value: 'soil', label: 'Soil' },
		{ value: 'coco', label: 'Coco' },
		{ value: 'soilless', label: 'Soilless' },
		{ value: 'hydroponic', label: 'Hydroponic' },
		{ value: 'aeroponic', label: 'Aeroponic' }
	];

	function seed(r: MediumRecipe | undefined) {
		name = r?.name ?? '';
		description = r?.description ?? '';
		speciesId = r?.species ?? '';
		base = r?.base ?? '';
		const comps = (r?.components ?? []).map((c) => ({
			productId: c.productId ?? '',
			label: c.label,
			percent: c.percent
		}));
		// A blank form opens with two empty rows: a mix needs at least two things
		// in it, and an empty list gives the user nothing to type into.
		components = comps.length
			? comps
			: [
					{ productId: '', label: '', percent: null },
					{ productId: '', label: '', percent: null }
				];
		amendments = (r?.amendments ?? []).map((a) => ({
			productId: a.productId ?? '',
			label: a.label,
			amount: a.amount,
			unit: a.unit ?? ''
		}));
	}

	// Reseed on open. This must never *read* the state it seeds — doing so makes
	// the effect depend on its own writes and Svelte aborts the update loop.
	$effect(() => {
		if (!open) return;
		seed(recipe);
		templateId = '';
		err = '';
	});

	// Prefill everything but the name from the chosen built-in mix.
	function applyTemplate() {
		const t = templates.find((x) => x.id === templateId);
		if (!t) return;
		const keep = name;
		seed(t);
		name = keep;
	}

	function addComponent() {
		components = [...components, { productId: '', label: '', percent: null }];
	}
	function removeComponent(i: number) {
		components = components.filter((_, k) => k !== i);
	}
	/** Give the last component whatever is missing from 100%. */
	function balance() {
		if (components.length === 0) return;
		const head = components.slice(0, -1);
		const used = head.reduce((n, c) => n + (typeof c.percent === 'number' ? c.percent : 0), 0);
		const rest = Math.round((100 - used) * 100) / 100;
		if (rest <= 0) return;
		components[components.length - 1].percent = rest;
	}

	function addAmendment() {
		amendments = [...amendments, { productId: '', label: '', amount: null, unit: 'g' }];
	}
	function removeAmendment(i: number) {
		amendments = amendments.filter((_, k) => k !== i);
	}

	async function save() {
		if (!canSave) return;
		busy = true;
		err = '';
		try {
			const input: MediumRecipeInput = {
				name: name.trim(),
				description: description.trim(),
				species: speciesId,
				base,
				components: components
					.filter((c) => c.label.trim() && (c.percent ?? 0) > 0)
					.map((c) => ({
						...(c.productId ? { productId: c.productId } : {}),
						label: c.label.trim(),
						percent: Number(c.percent)
					})),
				amendments: amendments
					.filter((a) => a.label.trim() && (a.amount ?? 0) > 0)
					.map((a) => ({
						...(a.productId ? { productId: a.productId } : {}),
						label: a.label.trim(),
						amount: Number(a.amount),
						...(a.unit.trim() ? { unit: a.unit.trim() } : {})
					}))
			};
			const saved = isEdit ? await updateMediumRecipe(recipe!.id, input) : await createMediumRecipe(input);
			open = false;
			toast.success(isEdit ? 'Medium recipe updated' : 'Medium recipe created', {
				description: saved.name
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
	const cell =
		'rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';
	const iconBtn = 'rounded p-1.5 text-rig-400 transition-colors hover:text-rig-100 disabled:opacity-30';
</script>

<Dialog
	bind:open
	{title}
	size="2xl"
	description="A substrate composition: bulk ingredients by volume, plus anything dosed per litre of mix."
>
	<div class="space-y-5">
		{#if err}<p class="text-xs text-danger">{err}</p>{/if}

		<div class="grid gap-3 sm:grid-cols-2">
			<label class="block">
				<span class="text-xs text-rig-400">Name</span>
				<input bind:value={name} placeholder="e.g. My coco mix" class="{field} mt-1" />
			</label>
			<label class="block">
				<span class="text-xs text-rig-400">Produces medium <span class="text-rig-600">(optional)</span></span>
				<Select class="mt-1" bind:value={base} placeholder="— None —" items={baseItems} />
			</label>
		</div>

		<!-- Start from a built-in mix (create mode only). -->
		{#if !isEdit && templates.length}
			<label class="block rounded-lg border border-rig-800 bg-rig-900/40 p-3">
				<span class="text-xs font-semibold uppercase tracking-wide text-rig-400">
					Start from a built-in mix
				</span>
				<Select
					class="mt-1.5"
					bind:value={templateId}
					placeholder="Start blank"
					onValueChange={applyTemplate}
					items={[
						{ value: '', label: 'Start blank' },
						...templates.map((t) => ({ value: t.id, label: t.name }))
					]}
				/>
				<span class="mt-1 block text-[11px] text-rig-500">
					Fills the ingredients below — you can edit everything after.
				</span>
			</label>
		{/if}

		<div class="grid gap-3 sm:grid-cols-2">
			<label class="block">
				<span class="text-xs text-rig-400">Crop tag <span class="text-rig-600">(optional)</span></span>
				<Select
					class="mt-1"
					bind:value={speciesId}
					placeholder="Any crop"
					items={[
						{ value: '', label: 'Any crop' },
						...species.map((sp) => ({ value: sp.id, label: sp.label }))
					]}
				/>
			</label>
		</div>
		<label class="block">
			<span class="text-xs text-rig-400">Description <span class="text-rig-600">(optional)</span></span>
			<textarea bind:value={description} rows="2" class="{field} mt-1"></textarea>
		</label>

		<!-- Bulk ingredients -->
		<section class="space-y-2">
			<div class="flex items-center justify-between">
				<h3 class="text-xs font-semibold uppercase tracking-wide text-rig-400">Ingredients (by volume)</h3>
				<button
					type="button"
					onclick={addComponent}
					class="inline-flex items-center gap-1 rounded-md border border-rig-700 px-2 py-1 text-xs text-rig-200 hover:border-leaf hover:text-white"
				>
					<Plus size={13} /> Add ingredient
				</button>
			</div>
			<div class="space-y-1.5">
				{#each components as c, i (i)}
					<div class="flex items-center gap-2">
						<Select
							class="min-w-0 flex-1"
							bind:value={c.productId}
							placeholder="Custom — type a name"
							onValueChange={() => onComponentProduct(i)}
							items={mediaItems}
						/>
						{#if !c.productId}
							<!-- Only a custom ingredient needs typing; a product already
							     carries its own name. -->
							<input bind:value={c.label} placeholder="e.g. Perlite" class="{cell} min-w-0 flex-1" />
						{/if}
						<input
							type="number"
							inputmode="decimal"
							step="any"
							min="0"
							max="100"
							bind:value={c.percent}
							class="{cell} w-20 shrink-0 text-center"
						/>
						<span class="w-4 shrink-0 text-xs text-rig-500">%</span>
						<button type="button" onclick={() => removeComponent(i)} aria-label="Remove ingredient" class={iconBtn}>
							<Trash2 size={14} />
						</button>
					</div>
				{/each}
			</div>
			<div class="flex items-center justify-between gap-3 border-t border-rig-800/60 pt-2">
				<span class="text-xs {totalOk ? 'text-rig-500' : 'text-warn'}">
					Total {total.toFixed(total % 1 ? 1 : 0)}%{totalOk ? '' : ' — a mix should add up to 100%'}
				</span>
				{#if !totalOk && components.length > 0}
					<button
						type="button"
						onclick={balance}
						class="rounded-md border border-rig-700 px-2 py-1 text-[11px] text-rig-300 hover:border-leaf hover:text-white"
					>
						Balance last to {(100 - total + (components.at(-1)?.percent ?? 0)).toFixed(0)}%
					</button>
				{/if}
			</div>
		</section>

		<!-- Per-litre amendments -->
		<section class="space-y-2">
			<div class="flex items-center justify-between">
				<h3 class="text-xs font-semibold uppercase tracking-wide text-rig-400">
					Amendments <span class="normal-case text-rig-600">(per litre of mix)</span>
				</h3>
				<button
					type="button"
					onclick={addAmendment}
					class="inline-flex items-center gap-1 rounded-md border border-rig-700 px-2 py-1 text-xs text-rig-200 hover:border-leaf hover:text-white"
				>
					<Plus size={13} /> Add amendment
				</button>
			</div>
			{#if amendments.length === 0}
				<p class="text-xs text-rig-500">
					Anything dosed against the finished volume rather than taking a share of it — dolomite
					lime, mycorrhiza, slow-release granules.
				</p>
			{:else}
				<div class="space-y-1.5">
					{#each amendments as a, i (i)}
						<div class="flex items-center gap-2">
							<Select
								class="min-w-0 flex-1"
								bind:value={a.productId}
								placeholder="Custom — type a name"
								onValueChange={() => onAmendmentProduct(i)}
								items={amendmentItems}
							/>
							{#if !a.productId}
								<input bind:value={a.label} placeholder="e.g. Dolomite lime" class="{cell} min-w-0 flex-1" />
							{/if}
							<input
								type="number"
								inputmode="decimal"
								step="any"
								min="0"
								bind:value={a.amount}
								class="{cell} w-20 shrink-0 text-center"
							/>
							<input
								bind:value={a.unit}
								placeholder="g"
								class="{cell} w-16 shrink-0 text-center"
								title="Unit per litre of mix"
							/>
							<span class="w-6 shrink-0 text-xs text-rig-500">/L</span>
							<button type="button" onclick={() => removeAmendment(i)} aria-label="Remove amendment" class={iconBtn}>
								<Trash2 size={14} />
							</button>
						</div>
					{/each}
				</div>
			{/if}
		</section>

		<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
			<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
			<Button onclick={save} disabled={busy || !canSave}>{isEdit ? 'Save' : 'Create'}</Button>
		</div>
	</div>
</Dialog>
