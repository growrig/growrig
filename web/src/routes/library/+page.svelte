<script lang="ts">
	import { onMount } from 'svelte';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		getSpecies,
		getCultivars,
		deleteCultivar,
		cultivarImageURL,
		getRecipes,
		getRecipeTemplates,
		deleteRecipe,
		getMediumRecipes,
		getMediumRecipeTemplates,
		deleteMediumRecipe
	} from '$lib/api';
	import type { Species, Cultivar, FeedingRecipe, MediumRecipe } from '$lib/types';
	import { titleCase } from '$lib/format';
	import CultivarFormModal from '$lib/components/CultivarFormModal.svelte';
	import RecipeFormModal from '$lib/components/RecipeFormModal.svelte';
	import MediumRecipeFormModal from '$lib/components/MediumRecipeFormModal.svelte';
	import Plus from '@lucide/svelte/icons/plus';
	import Dna from '@lucide/svelte/icons/dna';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import FlaskConical from '@lucide/svelte/icons/flask-conical';
	import Leaf from '@lucide/svelte/icons/leaf';
	import Layers from '@lucide/svelte/icons/layers';

	type Tab = 'recipes' | 'mediums' | 'cultivars' | 'species';
	let tab = $state<Tab>('recipes');
	const tabs: { id: Tab; label: string }[] = [
		{ id: 'recipes', label: 'Recipes' },
		{ id: 'mediums', label: 'Mediums' },
		{ id: 'cultivars', label: 'Cultivars' },
		{ id: 'species', label: 'Species' }
	];

	let species = $state<Species[]>([]);
	let cultivars = $state<Cultivar[]>([]);
	let editingCultivar = $state<Cultivar | undefined>(undefined);
	let cultivarModalOpen = $state(false);

	// Feeding recipes: the user's own (shown in the table) plus built-in
	// templates (offered only inside the create form).
	let feedings = $state<FeedingRecipe[]>([]);
	let feedingTemplates = $state<FeedingRecipe[]>([]);
	let editingFeeding = $state<FeedingRecipe | undefined>(undefined);
	let feedingModalOpen = $state(false);

	// Medium recipes: the user's own (shown in the table) plus built-in mixes
	// (offered as templates inside the create form), mirroring feeding recipes.
	let mediums = $state<MediumRecipe[]>([]);
	let mediumTemplates = $state<MediumRecipe[]>([]);
	let editingMedium = $state<MediumRecipe | undefined>(undefined);
	let mediumModalOpen = $state(false);

	const speciesById = $derived(new Map(species.map((s) => [s.id, s])));

	onMount(() => {
		getSpecies().then((s) => (species = s)).catch(() => {});
		refreshCultivars();
		refreshFeedings();
		getRecipeTemplates().then((t) => (feedingTemplates = t)).catch(() => {});
		refreshMediums();
		getMediumRecipeTemplates().then((t) => (mediumTemplates = t)).catch(() => {});
	});

	function refreshCultivars() {
		getCultivars().then((c) => (cultivars = c)).catch(() => {});
	}

	// Enter/Space activates a clickable cultivar card / recipe row.
	function activateOnKey(e: KeyboardEvent, fn: () => void) {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			fn();
		}
	}

	function refreshFeedings() {
		getRecipes().then((f) => (feedings = f)).catch(() => {});
	}

	function newFeeding() {
		editingFeeding = undefined;
		feedingModalOpen = true;
	}
	function editFeeding(f: FeedingRecipe) {
		editingFeeding = f;
		feedingModalOpen = true;
	}
	async function removeFeeding(f: FeedingRecipe) {
		if (!confirm(`Delete feeding recipe “${f.name}”?`)) return;
		try {
			await deleteRecipe(f.id);
			toast.success('Recipe deleted', { description: f.name });
			refreshFeedings();
		} catch (e) {
			toast.error('Could not delete recipe', { description: f.name });
		}
	}

	// Total week count across a recipe's phases, for the card summary.
	function weekCount(f: FeedingRecipe): number {
		return (f.phases ?? []).reduce((n, ph) => n + (ph.weeks?.length ?? 0), 0);
	}

	// Event-triggered phases have no weeks, so a schedule built mostly from
	// amendments would otherwise read as an empty "0 weeks" recipe.
	function oneShotCount(f: FeedingRecipe): number {
		return (f.phases ?? []).filter((ph) => ph.trigger).length;
	}

	function refreshMediums() {
		getMediumRecipes().then((m) => (mediums = m)).catch(() => {});
	}

	function newMedium() {
		editingMedium = undefined;
		mediumModalOpen = true;
	}
	function editMedium(m: MediumRecipe) {
		editingMedium = m;
		mediumModalOpen = true;
	}
	async function removeMedium(m: MediumRecipe) {
		if (!confirm(`Delete medium recipe \u201c${m.name}\u201d?`)) return;
		try {
			await deleteMediumRecipe(m.id);
			toast.success('Medium recipe deleted', { description: m.name });
			refreshMediums();
		} catch (e) {
			toast.error('Could not delete medium recipe', { description: m.name });
		}
	}

	/** "70% coco coir \u00b7 30% perlite" \u2014 the mix at a glance. */
	function mixSummary(m: MediumRecipe): string {
		return (m.components ?? []).map((c) => `${c.percent}% ${c.label.toLowerCase()}`).join(' \u00b7 ');
	}

	function mixTotal(m: MediumRecipe): number {
		return (m.components ?? []).reduce((n, c) => n + c.percent, 0);
	}

	function newCultivar() {
		editingCultivar = undefined;
		cultivarModalOpen = true;
	}
	function editCultivar(c: Cultivar) {
		editingCultivar = c;
		cultivarModalOpen = true;
	}
	async function removeCultivar(c: Cultivar) {
		if (!confirm(`Delete cultivar “${c.name}”?`)) return;
		try {
			await deleteCultivar(c.id);
			toast.success('Cultivar deleted', { description: c.name });
			refreshCultivars();
		} catch (e) {
			toast.error('Could not delete cultivar', { description: c.name });
		}
	}

	// A short, human summary of a cultivar's attributes using its species schema.
	function attrSummary(c: Cultivar): { label: string; value: string }[] {
		const sp = speciesById.get(c.species);
		if (!sp?.cultivarAttributes) return [];
		const out: { label: string; value: string }[] = [];
		for (const attr of sp.cultivarAttributes) {
			const v = c.attributes?.[attr.key];
			if (!v) continue;
			const value = attr.type === 'percent' ? `${v}%` : attr.unit ? `${v} ${attr.unit}` : titleCase(v);
			out.push({ label: attr.label, value });
		}
		return out;
	}

	const cultivarCountFor = (speciesId: string) =>
		cultivars.filter((c) => c.species === speciesId).length;
</script>

<div class="mb-6">
	<h1 class="text-2xl font-semibold">Library</h1>
	<p class="text-sm text-rig-400">
		Reusable reference data for your grows — feeding recipes, medium mixes, cultivars and species
		definitions.
	</p>
</div>

<!-- Local tabs so unrelated tables/cards don't stack in one long page. -->
<div class="mb-6 flex gap-1 border-b border-rig-800">
	{#each tabs as t (t.id)}
		<button
			onclick={() => (tab = t.id)}
			class="relative -mb-px border-b-2 px-4 py-2 text-sm font-medium transition-colors {tab === t.id
				? 'border-rig-50 text-rig-50'
				: 'border-transparent text-rig-400 hover:text-rig-100'}"
		>
			{t.label}
		</button>
	{/each}
</div>

{#if tab === 'recipes'}
	<!-- Feeding recipes: nutrient schedules (built-in + user). -->
	<section>
		<div class="mb-3 flex items-center justify-between gap-4">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">
				Feeding recipes{feedings.length ? ` · ${feedings.length}` : ''}
			</h2>
			{#if auth.isAdmin && feedings.length}
				<button
					onclick={newFeeding}
					class="inline-flex items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-xs font-medium text-rig-200 transition-colors hover:border-leaf hover:text-white"
				>
					<Plus size={14} /> New recipe
				</button>
			{/if}
		</div>
		{#if feedings.length === 0}
			<div class="rounded-xl border border-dashed border-rig-800 p-10 text-center">
				<div class="mb-3 flex justify-center text-rig-500"><FlaskConical size={40} /></div>
				<h3 class="mb-1 text-lg font-semibold">No feeding recipes yet</h3>
				<p class="mb-5 text-sm text-rig-400">Build nutrient schedules — products dosed per week across each phase of a grow.</p>
				{#if auth.isAdmin}
					<button
						onclick={newFeeding}
						class="rounded-md bg-rig-50 px-5 py-2 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-200"
					>
						Add a recipe
					</button>
				{/if}
			</div>
		{:else}
			<div class="overflow-x-auto rounded-xl border border-rig-800">
				<table class="w-full border-collapse text-sm">
					<thead>
						<tr class="border-b border-rig-800 bg-rig-900/50 text-left text-xs uppercase tracking-wide text-rig-500">
							<th class="px-4 py-2.5 font-medium">Name</th>
							<th class="px-4 py-2.5 font-medium">Brand</th>
							<th class="px-4 py-2.5 font-medium">Species</th>
							<th class="px-4 py-2.5 text-center font-medium">Products</th>
							<th class="px-4 py-2.5 text-center font-medium">Phases</th>
							<th class="px-4 py-2.5 text-center font-medium">Weeks</th>
							{#if auth.isAdmin}<th class="px-4 py-2.5"></th>{/if}
						</tr>
					</thead>
					<tbody>
						{#each feedings as f (f.id)}
							<!-- Row click opens the editor for admins; the Edit button keeps keyboard access. -->
							<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
							<tr
								class="border-b border-rig-800/60 last:border-0 hover:bg-rig-900/30"
								class:cursor-pointer={auth.isAdmin}
								onclick={() => auth.isAdmin && editFeeding(f)}
							>
								<td class="px-4 py-2.5">
									<div class="font-medium text-rig-100">{f.name}</div>
									{#if f.description}<div class="line-clamp-1 max-w-md text-xs text-rig-500">{f.description}</div>{/if}
								</td>
								<td class="px-4 py-2.5 text-rig-300">{f.brand || '—'}</td>
								<td class="px-4 py-2.5">
									<span class="rounded-full bg-rig-800 px-2 py-0.5 text-[11px] capitalize text-rig-300">
										{speciesById.get(f.species)?.label ?? f.species}
									</span>
								</td>
								<td class="px-4 py-2.5 text-center text-rig-300">{f.products?.length ?? 0}</td>
								<td class="px-4 py-2.5 text-center text-rig-300">{f.phases?.length ?? 0}</td>
								<td class="px-4 py-2.5 text-center text-rig-300">
									{weekCount(f)}
									{#if oneShotCount(f)}
										<span class="ml-1 text-[11px] text-rig-500" title="One-shot applications (planting, transplant, top-dress)">
											+{oneShotCount(f)} one-shot
										</span>
									{/if}
								</td>
								{#if auth.isAdmin}
									<td class="px-4 py-2.5">
										<div class="flex justify-end gap-1">
											<button
												onclick={(e) => { e.stopPropagation(); editFeeding(f); }}
												aria-label="Edit recipe"
												class="rounded p-1.5 text-rig-400 hover:text-rig-100"
											>
												<Pencil size={14} />
											</button>
											<button
												onclick={(e) => { e.stopPropagation(); removeFeeding(f); }}
												aria-label="Delete recipe"
												class="rounded p-1.5 text-rig-400 hover:text-danger"
											>
												<Trash2 size={14} />
											</button>
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
{:else if tab === 'mediums'}
	<!-- Medium recipes: what a substrate is mixed from (built-in + user). -->
	<section>
		<div class="mb-3 flex items-center justify-between gap-4">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">
				Medium recipes{mediums.length ? ` \u00b7 ${mediums.length}` : ''}
			</h2>
			{#if auth.isAdmin && mediums.length}
				<button
					onclick={newMedium}
					class="inline-flex items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-xs font-medium text-rig-200 transition-colors hover:border-leaf hover:text-white"
				>
					<Plus size={14} /> New mix
				</button>
			{/if}
		</div>
		{#if mediums.length === 0}
			<div class="rounded-xl border border-dashed border-rig-800 p-10 text-center">
				<div class="mb-3 flex justify-center text-rig-500"><Layers size={40} /></div>
				<h3 class="mb-1 text-lg font-semibold">No medium recipes yet</h3>
				<p class="mb-5 text-sm text-rig-400">
					Write down what your substrate is mixed from \u2014 ingredients by volume, plus anything
					dosed per litre \u2014 then pick it when starting a grow.
				</p>
				{#if auth.isAdmin}
					<button
						onclick={newMedium}
						class="rounded-md bg-rig-50 px-5 py-2 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-200"
					>
						Add a mix
					</button>
				{/if}
			</div>
		{:else}
			<div class="overflow-x-auto rounded-xl border border-rig-800">
				<table class="w-full border-collapse text-sm">
					<thead>
						<tr class="border-b border-rig-800 bg-rig-900/50 text-left text-xs uppercase tracking-wide text-rig-500">
							<th class="px-4 py-2.5 font-medium">Name</th>
							<th class="px-4 py-2.5 font-medium">Mix</th>
							<th class="px-4 py-2.5 font-medium">Produces</th>
							<th class="px-4 py-2.5 text-center font-medium">Amendments</th>
							{#if auth.isAdmin}<th class="px-4 py-2.5"></th>{/if}
						</tr>
					</thead>
					<tbody>
						{#each mediums as m (m.id)}
							<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
							<tr
								class="border-b border-rig-800/60 last:border-0 hover:bg-rig-900/30"
								class:cursor-pointer={auth.isAdmin}
								onclick={() => auth.isAdmin && editMedium(m)}
							>
								<td class="px-4 py-2.5">
									<div class="font-medium text-rig-100">{m.name}</div>
									{#if m.description}
										<div class="line-clamp-1 max-w-md text-xs text-rig-500">{m.description}</div>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-rig-300">
									<span class="line-clamp-1 max-w-md">{mixSummary(m) || '\u2014'}</span>
									{#if Math.abs(mixTotal(m) - 100) > 0.01}
										<span class="text-[11px] text-warn" title="A mix should add up to 100%">
											totals {mixTotal(m)}%
										</span>
									{/if}
								</td>
								<td class="px-4 py-2.5">
									{#if m.base}
										<span class="rounded-full bg-rig-800 px-2 py-0.5 text-[11px] capitalize text-rig-300">
											{m.base}
										</span>
									{:else}<span class="text-rig-600">\u2014</span>{/if}
								</td>
								<td class="px-4 py-2.5 text-center text-rig-300">{m.amendments?.length ?? 0}</td>
								{#if auth.isAdmin}
									<td class="px-4 py-2.5">
										<div class="flex justify-end gap-1">
											<button
												onclick={(e) => { e.stopPropagation(); editMedium(m); }}
												aria-label="Edit medium recipe"
												class="rounded p-1.5 text-rig-400 hover:text-rig-100"
											>
												<Pencil size={14} />
											</button>
											<button
												onclick={(e) => { e.stopPropagation(); removeMedium(m); }}
												aria-label="Delete medium recipe"
												class="rounded p-1.5 text-rig-400 hover:text-danger"
											>
												<Trash2 size={14} />
											</button>
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

{:else if tab === 'cultivars'}
	<section>
		<div class="mb-3 flex items-center justify-between gap-4">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">
				Cultivars{cultivars.length ? ` · ${cultivars.length}` : ''}
			</h2>
			{#if auth.isAdmin && cultivars.length}
				<button
					onclick={newCultivar}
					class="inline-flex items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-xs font-medium text-rig-200 transition-colors hover:border-leaf hover:text-white"
				>
					<Plus size={14} /> New cultivar
				</button>
			{/if}
		</div>
		{#if cultivars.length === 0}
			<div class="rounded-xl border border-dashed border-rig-800 p-10 text-center">
				<div class="mb-3 flex justify-center text-rig-500"><Dna size={40} /></div>
				<h3 class="mb-1 text-lg font-semibold">No cultivars yet</h3>
				<p class="mb-5 text-sm text-rig-400">Build a library of strains and varieties, then bind them to your plants.</p>
				{#if auth.isAdmin}
					<button
						onclick={newCultivar}
						class="rounded-md bg-rig-50 px-5 py-2 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-200"
					>
						Add a cultivar
					</button>
				{/if}
			</div>
		{:else}
			<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
				{#each cultivars as c (c.id)}
					<!-- Whole card opens the editor for admins (buttons below stop propagation). -->
					<div
						class="group relative flex flex-col overflow-hidden rounded-xl border border-rig-800 bg-rig-900/50 transition-colors hover:border-rig-600"
						class:cursor-pointer={auth.isAdmin}
						role="button"
						tabindex={auth.isAdmin ? 0 : -1}
						onclick={() => auth.isAdmin && editCultivar(c)}
						onkeydown={(e) => auth.isAdmin && activateOnKey(e, () => editCultivar(c))}
					>
						<!-- Image occupies the top half, shown in full (no crop). -->
						<div class="flex h-48 items-center justify-center overflow-hidden border-b border-rig-800 bg-rig-950">
							{#if c.imageType}
								<img src={cultivarImageURL(c.id)} alt={c.name} class="max-h-full max-w-full object-contain" />
							{:else}
								<div class="text-rig-700"><Dna size={40} /></div>
							{/if}
						</div>
						<div class="min-w-0 flex-1 p-3">
							<div class="flex items-center justify-between gap-2">
								<h3 class="truncate font-semibold">{c.name}</h3>
								<span class="shrink-0 rounded-full bg-rig-800 px-2 py-0.5 text-[11px] capitalize text-rig-300">
									{speciesById.get(c.species)?.label ?? c.species}
								</span>
							</div>
							{#if c.creator}<p class="truncate text-xs text-rig-500">by {c.creator}</p>{/if}
							{#if attrSummary(c).length}
								<div class="mt-1.5 flex flex-wrap gap-1">
									{#each attrSummary(c) as a (a.label)}
										<span class="rounded bg-rig-800/70 px-1.5 py-0.5 text-[11px] text-rig-300">
											<span class="text-rig-500">{a.label}:</span> {a.value}
										</span>
									{/each}
								</div>
							{/if}
							{#if c.description}<p class="mt-1.5 line-clamp-2 text-xs text-rig-400">{c.description}</p>{/if}
						</div>
						{#if auth.isAdmin}
							<div class="absolute right-2 top-2 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
								<button
									onclick={(e) => { e.stopPropagation(); editCultivar(c); }}
									aria-label="Edit cultivar"
									class="rounded bg-rig-950/80 p-1.5 text-rig-400 hover:text-rig-100"
								>
									<Pencil size={13} />
								</button>
								<button
									onclick={(e) => { e.stopPropagation(); removeCultivar(c); }}
									aria-label="Delete cultivar"
									class="rounded bg-rig-950/80 p-1.5 text-rig-400 hover:text-danger"
								>
									<Trash2 size={13} />
								</button>
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</section>
{:else}
	<!-- Species: read-only reference definitions (stages + cultivar attributes). -->
	<section>
		<div class="mb-3 flex items-center gap-2">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">
				Species{species.length ? ` · ${species.length}` : ''}
			</h2>
		</div>
		{#if species.length === 0}
			<div class="rounded-xl border border-dashed border-rig-800 p-10 text-center text-sm text-rig-500">
				No species definitions available.
			</div>
		{:else}
			<div class="grid gap-4 sm:grid-cols-2">
				{#each species as sp (sp.id)}
					<div class="rounded-xl border border-rig-800 bg-rig-900/50 p-4">
						<div class="mb-3 flex items-center justify-between gap-2">
							<div class="flex items-center gap-2">
								<span class="grid h-8 w-8 place-items-center rounded-md bg-rig-800 text-leaf">
									<Leaf size={16} />
								</span>
								<h3 class="font-semibold">{sp.label}</h3>
							</div>
							<span class="rounded-full bg-rig-800 px-2 py-0.5 text-[11px] text-rig-400">
								{cultivarCountFor(sp.id)} cultivar{cultivarCountFor(sp.id) === 1 ? '' : 's'}
							</span>
						</div>
						{#if sp.stages?.length}
							<div class="mb-2">
								<p class="mb-1 text-[11px] uppercase tracking-wide text-rig-500">Stages</p>
								<div class="flex flex-wrap gap-1.5">
									{#each sp.stages as st (st.name)}
										<span class="rounded bg-rig-800/70 px-2 py-0.5 text-xs text-rig-300">
											{st.name}<span class="ml-1 text-rig-500">{st.lightHours}h</span>
										</span>
									{/each}
								</div>
							</div>
						{/if}
						{#if sp.cultivarAttributes?.length}
							<div>
								<p class="mb-1 text-[11px] uppercase tracking-wide text-rig-500">Cultivar fields</p>
								<div class="flex flex-wrap gap-1.5">
									{#each sp.cultivarAttributes as attr (attr.key)}
										<span class="rounded bg-rig-800/70 px-2 py-0.5 text-xs text-rig-300">{attr.label}</span>
									{/each}
								</div>
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</section>
{/if}

{#if auth.isAdmin}
	<CultivarFormModal
		bind:open={cultivarModalOpen}
		cultivar={editingCultivar}
		{species}
		onSaved={refreshCultivars}
	/>
	<RecipeFormModal
		bind:open={feedingModalOpen}
		recipe={editingFeeding}
		{species}
		templates={feedingTemplates}
		onSaved={refreshFeedings}
	/>
	<MediumRecipeFormModal
		bind:open={mediumModalOpen}
		recipe={editingMedium}
		{species}
		templates={mediumTemplates}
		onSaved={refreshMediums}
	/>
{/if}
