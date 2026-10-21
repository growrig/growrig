<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { toast } from '$lib/toast.svelte';
	import type { CareAction, CareField, FeedingRecipe, GrowDetail, LogCareInput, PlantDetail, PotUnit } from '$lib/types';
	import { logCare } from '$lib/api';
	import { Button, Dialog, Select } from '$lib/components/ui';
	import { plantDisplayName, plantNumbersById } from '$lib/format';
	import { fmtDateTime } from '$lib/datetime';
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';

	interface Props {
		open?: boolean;
		grow: GrowDetail;
		/** Care actions available for the grow (from its species). */
		actions: CareAction[];
		/** Nutrient recipes offered when feeding. */
		recipes?: FeedingRecipe[];
		/** Preselect these plants when opening (empty = all active plants). */
		preselectedPlantIds?: string[];
		/** Jump straight to details for this action, all plants selected. */
		initialActionKey?: string;
		onLogged?: () => void;
	}
	let {
		open = $bindable(false),
		grow,
		actions,
		recipes = [],
		preselectedPlantIds = [],
		initialActionKey,
		onLogged
	}: Props = $props();

	type Step = 'action' | 'plants' | 'details';
	let step = $state<Step>('action');
	let action = $state<CareAction | null>(null);
	let selected = $state<Set<string>>(new Set());
	let busy = $state(false);
	let err = $state('');

	// Detail fields (only those the chosen action declares are shown/sent).
	let amountMl = $state<number | null>(null);
	let perPlant = $state(false);
	let perAmount = $state<Record<string, number | null>>({});
	let recipeId = $state('');
	let ph = $state<number | null>(null);
	let ec = $state<number | null>(null);
	let runoffMl = $state<number | null>(null);
	let runoffPh = $state<number | null>(null);
	let note = $state('');
	let trainType = $state('');
	let product = $state('');
	// When it happened. Defaults to now; editable so a grower can write up
	// yesterday's watering without it landing on today.
	let occurredAt = $state('');
	let potSize = $state<number | null>(null);
	let potUnitValue = $state<PotUnit>('L');
	let potType = $state('');
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
	// Amendment doses: product key -> amount per plant, in that product's unit.
	let doses = $state<Record<string, number | null>>({});

	const activePlants = $derived(grow.plants.filter((p) => p.status === 'active'));
	const plantNumbers = $derived(plantNumbersById(grow.plants));
	const quickActions = $derived(actions.filter((a) => a.quick));
	const moreActions = $derived(actions.filter((a) => !a.quick));
	const has = (f: CareField) => !!action?.fields.includes(f);
	const name = (p: PlantDetail) => plantDisplayName(p, plantNumbers.get(p.id));

	// (Re)initialise each time the dialog opens.
	$effect(() => {
		if (!open) return;
		resetDetails();
		const preset =
			preselectedPlantIds.length > 0
				? preselectedPlantIds
				: activePlants.map((p) => p.id);
		selected = new Set(preset);
		if (initialActionKey) {
			const a = actions.find((x) => x.key === initialActionKey) ?? null;
			action = a;
			// Water/Feed all: skip to details against every active plant.
			selected = new Set(activePlants.map((p) => p.id));
			step = a ? 'details' : 'action';
		} else {
			action = null;
			step = 'action';
		}
	});

	/** `now` as the value a datetime-local input expects (local, minute precision). */
	function localNow(): string {
		const d = new Date();
		d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
		return d.toISOString().slice(0, 16);
	}

	function resetDetails() {
		occurredAt = localNow();
		amountMl = null;
		perPlant = false;
		perAmount = {};
		recipeId = '';
		ph = ec = runoffMl = runoffPh = null;
		note = '';
		trainType = '';
		product = '';
		potSize = null;
		potUnitValue = 'L';
		potType = '';
		doses = {};
		amendPhase = '';
		err = '';
	}

	function chooseAction(a: CareAction) {
		action = a;
		step = 'plants';
	}
	function toggle(id: string) {
		const next = new Set(selected);
		next.has(id) ? next.delete(id) : next.add(id);
		selected = next;
	}
	function selectAll() {
		selected = new Set(activePlants.map((p) => p.id));
	}
	function selectNone() {
		selected = new Set();
	}

	const selectedPlants = $derived(activePlants.filter((p) => selected.has(p.id)));
	// Anything more than a minute in the past is a deliberate backdate.
	const backdated = $derived(
		!!occurredAt && Date.now() - new Date(occurredAt).getTime() > 90_000
	);
	const canSubmit = $derived(!!action && selectedPlants.length > 0 && !busy);

	// Structured fields without their own column are folded into the note so
	// nothing is lost until they get first-class handling.
	function composedNote(): string {
		const lines: string[] = [];
		if (has('trainType') && trainType.trim()) lines.push(`Training: ${trainType.trim()}`);
		if (has('product') && product.trim()) lines.push(`Product: ${product.trim()}`);
		if (note.trim()) lines.push(note.trim());
		return lines.join('\n');
	}

	async function submit() {
		if (!action || selectedPlants.length === 0) return;
		busy = true;
		err = '';
		try {
			const body: LogCareInput = { type: action.key, notes: composedNote() };
			// Only send a time when it was actually moved off "now", so a
			// same-moment log keeps the server's clock.
			if (occurredAt && occurredAt !== localNow()) {
				const at = new Date(occurredAt);
				if (!isNaN(at.getTime())) body.occurredAt = at.toISOString();
			}
			if (has('recipe') && recipeId) body.recipeId = recipeId;
			if (has('doses')) {
				const out: Record<string, number> = {};
				for (const [k, v] of Object.entries(doses)) {
					const n = typeof v === 'number' ? v : parseFloat(String(v));
					if (Number.isFinite(n) && n > 0) out[k] = n;
				}
				if (Object.keys(out).length) body.doses = out;
			}
			if (has('potSize') && potSize && potSize > 0) {
				body.potSize = potSize;
				body.potUnit = potUnitValue;
				body.potType = potType;
			}
			if (has('ph') && ph != null) body.ph = ph;
			if (has('ec') && ec != null) body.ec = ec;
			if (has('runoff')) {
				if (runoffMl != null) body.runoffMl = runoffMl;
				if (runoffPh != null) body.runoffPh = runoffPh;
			}
			if (has('amount') && perPlant) {
				body.applications = selectedPlants.map((p) => ({
					plantUnitId: p.id,
					amountMl: perAmount[p.id] ?? 0
				}));
			} else {
				body.plantUnitIds = selectedPlants.map((p) => p.id);
				if (has('amount') && amountMl != null) body.amountMl = amountMl;
			}
			await logCare(grow.id, body);
			open = false;
			const potNote = has('potSize') && potSize && potSize > 0 ? ` · ${potSize} ${potUnitValue} pot` : '';
			toast.success(`${action.label} logged`, {
				description: `${selectedPlants.length} ${selectedPlants.length === 1 ? 'plant' : 'plants'} · ${grow.name}${potNote}`
			});
			onLogged?.();
		} catch (e) {
			err = errMsg(e, 'Failed to log care');
		} finally {
			busy = false;
		}
	}

	const recipeItems = $derived([
		{ value: '', label: '— No recipe —' },
		...recipes.map((r) => ({ value: r.id, label: r.brand ? `${r.name} · ${r.brand}` : r.name }))
	]);

	// --- Amendments (the `doses` field) ---

	const selectedRecipe = $derived(recipes.find((r) => r.id === recipeId));
	/** The recipe's one-shot applications, offered as dose prefills. */
	const oneShotPhases = $derived((selectedRecipe?.phases ?? []).filter((ph) => ph.trigger));
	let amendPhase = $state('');
	const amendPhaseItems = $derived([
		{ value: '', label: '— Custom amounts —' },
		...oneShotPhases.map((ph) => ({ value: ph.name, label: ph.name }))
	]);

	/**
	 * Products worth showing a dose input for: everything the chosen phase doses,
	 * plus any product measured per plant/pot/substrate, since those are the ones
	 * that can't be expressed as a watering amount.
	 */
	const amendProducts = $derived.by(() => {
		const r = selectedRecipe;
		if (!r) return [];
		const phase = oneShotPhases.find((ph) => ph.name === amendPhase);
		const inPhase = new Set(Object.keys(phase?.doses ?? {}));
		return (r.products ?? []).filter((p) => inPhase.has(p.key) || !!p.basis);
	});

	const basisLabel: Record<string, string> = {
		plant: 'per plant',
		pot: 'per pot',
		substrate: 'per L substrate'
	};
	function doseUnit(key: string): string {
		const p = selectedRecipe?.products?.find((x) => x.key === key);
		const unit = p?.unit?.trim() || selectedRecipe?.unit?.trim() || '';
		return p?.basis ? `${unit} ${basisLabel[p.basis] ?? ''}`.trim() : unit;
	}

	/** Prefill the dose inputs from the chosen one-shot phase (blank = clear). */
	function applyAmendPhase() {
		const phase = oneShotPhases.find((ph) => ph.name === amendPhase);
		doses = { ...(phase?.doses ?? {}) };
	}

	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';
	const dialogTitle = $derived(
		step === 'action' ? 'Log care' : action ? `${action.label}` : 'Log care'
	);
</script>

<Dialog bind:open title={dialogTitle} description="Record a manual action against this grow's plants." size="xl">
	<div class="space-y-4">
		{#if err}<p class="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-xs text-danger">{err}</p>{/if}

		<!-- Step indicator -->
		{#if !initialActionKey}
			<div class="flex items-center gap-1.5 text-xs text-rig-500">
				<span class={step === 'action' ? 'text-rig-200' : ''}>1 · Action</span>
				<span>›</span>
				<span class={step === 'plants' ? 'text-rig-200' : ''}>2 · Plants</span>
				<span>›</span>
				<span class={step === 'details' ? 'text-rig-200' : ''}>3 · Details</span>
			</div>
		{/if}

		{#if step === 'action'}
			<div class="grid grid-cols-2 gap-2 sm:grid-cols-3">
				{#each [...quickActions, ...moreActions] as a (a.key)}
					<button
						onclick={() => chooseAction(a)}
						class="flex flex-col items-start gap-1 rounded-lg border border-rig-700 bg-rig-950/40 px-3 py-2.5 text-left transition-colors hover:border-leaf/60 hover:bg-rig-900"
					>
						<span class="text-sm font-medium text-rig-100">{a.label}</span>
						{#if a.quick}<span class="text-[10px] uppercase tracking-wide text-leaf/70">Quick</span>{/if}
					</button>
				{/each}
			</div>
		{:else if step === 'plants'}
			<div class="flex items-center justify-between">
				<span class="text-sm text-rig-300">{selected.size} of {activePlants.length} selected</span>
				<div class="flex gap-2 text-xs">
					<button onclick={selectAll} class="text-rig-400 hover:text-rig-100">All</button>
					<button onclick={selectNone} class="text-rig-400 hover:text-rig-100">None</button>
				</div>
			</div>
			<div class="max-h-64 space-y-1 overflow-y-auto rounded-lg border border-rig-800 p-1.5">
				{#each activePlants as p (p.id)}
					<label class="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 hover:bg-rig-800/50">
						<input type="checkbox" checked={selected.has(p.id)} onchange={() => toggle(p.id)} class="accent-leaf" />
						<span class="text-sm text-rig-200">{name(p)}</span>
						{#if p.tracking === 'group' && p.quantity > 1}<span class="text-xs text-rig-500">×{p.quantity}</span>{/if}
						{#if p.currentEnvironmentName}<span class="ml-auto text-xs text-rig-500">{p.currentEnvironmentName}</span>{/if}
					</label>
				{:else}
					<p class="px-2 py-3 text-center text-sm text-rig-500">No active plants in this grow.</p>
				{/each}
			</div>
		{:else if step === 'details'}
			<div class="space-y-3">
				{#if has('recipe')}
					<label class="block">
						<span class="text-xs text-rig-400">Nutrient recipe</span>
						<Select class="mt-1" bind:value={recipeId} items={recipeItems} />
					</label>
				{/if}

				{#if has('doses')}
					<div class="rounded-lg border border-rig-800 p-3">
						<span class="text-xs font-medium text-rig-300">Amendment</span>
						{#if !selectedRecipe}
							<p class="mt-1 text-xs text-rig-500">
								Pick a nutrient recipe above to dose its amendments.
							</p>
						{:else}
							{#if oneShotPhases.length}
								<Select
									class="mt-1.5"
									bind:value={amendPhase}
									placeholder="— Custom amounts —"
									onValueChange={applyAmendPhase}
									items={amendPhaseItems}
								/>
							{/if}
							{#if amendProducts.length === 0}
								<p class="mt-1.5 text-xs text-rig-500">
									This recipe has no amendments — its products are all dosed per litre of solution.
								</p>
							{:else}
								<div class="mt-2 space-y-1.5">
									{#each amendProducts as p (p.key)}
										<div class="flex items-center gap-2">
											<span class="min-w-0 flex-1 truncate text-sm text-rig-200">{p.label}</span>
											<input
												type="number"
												inputmode="decimal"
												step="any"
												min="0"
												bind:value={doses[p.key]}
												class="w-20 rounded-md border border-rig-700 bg-rig-950 px-2 py-1 text-center text-sm focus:border-leaf focus:outline-none"
											/>
											<span class="w-28 shrink-0 text-[11px] text-rig-500">{doseUnit(p.key)}</span>
										</div>
									{/each}
								</div>
								<p class="mt-2 text-[11px] text-rig-500">
									Amounts are what each selected plant receives.
								</p>
							{/if}
						{/if}
					</div>
				{/if}

				{#if has('amount')}
					<div class="rounded-lg border border-rig-800 p-3">
						<div class="flex items-center justify-between">
							<span class="text-xs font-medium text-rig-300">Amount per plant (ml)</span>
							<label class="flex items-center gap-1.5 text-xs text-rig-400">
								<input type="checkbox" bind:checked={perPlant} class="accent-leaf" /> Per-plant
							</label>
						</div>
						{#if perPlant}
							<div class="mt-2 space-y-1.5">
								{#each selectedPlants as p (p.id)}
									<label class="flex items-center gap-2 text-sm">
										<span class="w-40 shrink-0 truncate text-rig-300">{name(p)}</span>
										<input type="number" min="0" step="any" bind:value={perAmount[p.id]} placeholder="ml" class={field} />
									</label>
								{/each}
							</div>
						{:else}
							<input type="number" min="0" step="any" bind:value={amountMl} placeholder="e.g. 900" class="{field} mt-2" />
						{/if}
					</div>
				{/if}

				{#if has('ph') || has('ec')}
					<div class="grid gap-3 sm:grid-cols-2">
						{#if has('ph')}
							<label class="block"><span class="text-xs text-rig-400">pH</span>
								<input type="number" step="any" bind:value={ph} placeholder="e.g. 6.2" class="{field} mt-1" /></label>
						{/if}
						{#if has('ec')}
							<label class="block"><span class="text-xs text-rig-400">EC</span>
								<input type="number" step="any" bind:value={ec} placeholder="e.g. 1.4" class="{field} mt-1" /></label>
						{/if}
					</div>
				{/if}

				{#if has('runoff')}
					<div class="grid gap-3 sm:grid-cols-2">
						<label class="block"><span class="text-xs text-rig-400">Runoff (ml) <span class="text-rig-600">optional</span></span>
							<input type="number" min="0" step="any" bind:value={runoffMl} class="{field} mt-1" /></label>
						<label class="block"><span class="text-xs text-rig-400">Runoff pH <span class="text-rig-600">optional</span></span>
							<input type="number" step="any" bind:value={runoffPh} class="{field} mt-1" /></label>
					</div>
				{/if}

				{#if has('trainType')}
					<label class="block"><span class="text-xs text-rig-400">Training method</span>
						<input bind:value={trainType} placeholder="e.g. LST, topping, defoliation" class="{field} mt-1" /></label>
				{/if}
				{#if has('product')}
					<label class="block"><span class="text-xs text-rig-400">Product</span>
						<input bind:value={product} placeholder="e.g. neem oil" class="{field} mt-1" /></label>
				{/if}
				{#if has('potSize')}
					<div class="grid gap-3 sm:grid-cols-3">
						<label class="block"><span class="text-xs text-rig-400">New pot size</span>
							<input type="number" min="0" step="any" bind:value={potSize} placeholder="e.g. 20" class="{field} mt-1" /></label>
						<label class="block"><span class="text-xs text-rig-400">Unit</span>
							<Select value={potUnitValue} onValueChange={(v) => (potUnitValue = v as PotUnit)} items={potUnitItems} class="mt-1" /></label>
						<label class="block"><span class="text-xs text-rig-400">Pot type</span>
							<Select value={potType} onValueChange={(v) => (potType = v)} items={potTypeItems} class="mt-1" /></label>
					</div>
					<p class="text-xs text-rig-500">
						{selectedPlants.length === 1 ? 'This plant moves' : `These ${selectedPlants.length} plants move`} into the new pot; the old one is kept in the pot history.
					</p>
				{/if}

				{#if has('note')}
					<label class="block"><span class="text-xs text-rig-400">Note <span class="text-rig-600">optional</span></span>
						<textarea bind:value={note} rows="2" class="{field} mt-1"></textarea></label>
				{/if}
				{#if has('photos')}
					<p class="text-xs text-rig-600">Photos coming soon.</p>
				{/if}

				<label class="block border-t border-rig-800 pt-3">
					<span class="text-xs text-rig-400">When</span>
					<input type="datetime-local" bind:value={occurredAt} max={localNow()} class="{field} mt-1" />
				</label>

				<p class="text-xs text-rig-500">
					Logging for {selectedPlants.length} {selectedPlants.length === 1 ? 'plant' : 'plants'}{backdated ? ` · backdated to ${fmtDateTime(occurredAt)}` : ''}.
				</p>
			</div>
		{/if}

		<!-- Footer -->
		<div class="flex items-center justify-between gap-2 border-t border-rig-800 pt-4">
			<div>
				{#if step === 'plants'}
					<Button variant="ghost" onclick={() => (step = 'action')}><ArrowLeft size={15} /> Action</Button>
				{:else if step === 'details' && !initialActionKey}
					<Button variant="ghost" onclick={() => (step = 'plants')}><ArrowLeft size={15} /> Plants</Button>
				{/if}
			</div>
			<div class="flex gap-2">
				<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
				{#if step === 'plants'}
					<Button onclick={() => (step = 'details')} disabled={selected.size === 0}>Next</Button>
				{:else if step === 'details'}
					<Button onclick={submit} disabled={!canSubmit}>Log care for {selectedPlants.length} {selectedPlants.length === 1 ? 'plant' : 'plants'}</Button>
				{/if}
			</div>
		</div>
	</div>
</Dialog>
