<script lang="ts">
	import type { Binding, CatalogProduct, DiscoveredEntity, Measurement, Role } from '$lib/types';
	import { createBinding, updateBinding } from '$lib/api';
	import { Button, Dialog, Select, Switch, type SelectItem } from '$lib/components/ui';
	import CatalogDevicePicker, { type BindingDraft } from '$lib/components/CatalogDevicePicker.svelte';

	interface Props {
		/** Bindable open state. */
		open?: boolean;
		environmentId: string;
		catalog: CatalogProduct[];
		discovered: DiscoveredEntity[];
		bindings?: Binding[];
		usedEntities: Set<string>;
		/** When set, the modal edits this binding instead of adding new devices. */
		binding?: Binding | null;
		onSaved: () => void;
		onInstall?: (product: CatalogProduct) => void;
		initialCategory?: import('$lib/types').BindingKind;
		flash?: (kind: 'ok' | 'err', text: string) => void;
	}

	let {
		open = $bindable(false),
		environmentId,
		catalog,
		discovered,
		bindings = [],
		usedEntities,
		binding = null,
		onSaved,
		onInstall,
		initialCategory = 'sensor',
		flash
	}: Props = $props();

	const editing = $derived(!!binding);

	// --- edit form state, seeded when a binding is passed ---
	let name = $state('');
	let entity = $state('');
	let measurement = $state<Measurement>('temperature');
	let role = $state<Role>('unassigned');
	let rpmEntity = $state('');
	let sizeMm = $state(0);
	let maxRpm = $state(0);
	let airflowCfm = $state(0);
	let staticPressureMmH2O = $state(0);
	let startingVoltage = $state(0);
	let wattage = $state(0);
	let primary = $state(false);
	let powerControllerId = $state('');
	let controllerChannelId = $state('');
	let busy = $state(false);

	// Reseed the form whenever the target binding changes.
	$effect(() => {
		if (binding) {
			name = binding.name;
			entity = binding.entity;
			measurement = binding.measurement ?? 'temperature';
			role = binding.role ?? 'unassigned';
			rpmEntity = binding.rpmEntity ?? '';
			sizeMm = binding.sizeMm ?? 0;
			maxRpm = binding.maxRpm ?? 0;
			airflowCfm = binding.airflowCfm ?? 0;
			staticPressureMmH2O = binding.staticPressureMmH2O ?? 0;
			startingVoltage = binding.startingVoltage ?? 0;
			wattage = binding.wattage ?? 0;
			primary = binding.primary ?? false;
			powerControllerId = binding.powerControllerId ?? '';
			controllerChannelId = binding.controllerChannelId ?? '';
		}
	});

	const measurementItems: SelectItem[] = [
		{ value: 'temperature', label: 'Temperature' },
		{ value: 'humidity', label: 'Humidity' },
		{ value: 'co2', label: 'CO₂' }
		,{ value: 'power', label: 'Power' }
	];
	const roleItems: SelectItem[] = [
		{ value: 'unassigned', label: 'Unassigned' },
		{ value: 'exhaust', label: 'Exhaust' },
		{ value: 'intake', label: 'Intake' },
		{ value: 'circulation', label: 'Circulation' }
	];
	const controllerItems = $derived<SelectItem[]>(bindings
		.filter((b) => b.environmentId === environmentId && b.kind === 'controller')
		.map((b) => ({ value: b.id, label: `${b.deviceName} — ${b.name}${b.rpmEntity ? ' · RPM connected' : ''}` })));

	async function addDevices(drafts: BindingDraft[]) {
		busy = true;
		try {
			for (const d of drafts) await createBinding({ environmentId, ...d });
			flash?.('ok', drafts.length > 1 ? `${drafts.length} devices added` : 'Device added');
			open = false;
			onSaved();
		} catch (e) {
			flash?.('err', e instanceof Error ? e.message : 'Add failed');
		} finally {
			busy = false;
		}
	}

	async function saveEdit() {
		if (!binding) return;
		busy = true;
		try {
			await updateBinding(binding.id, {
				deviceId: binding.deviceId,
				deviceName: name.trim() || binding.deviceName,
				powerControllerId: binding.kind === 'light' ? powerControllerId || undefined : binding.powerControllerId,
				controllerChannelId: binding.kind === 'fan' ? controllerChannelId || undefined : binding.controllerChannelId,
				environmentId: binding.environmentId,
				kind: binding.kind,
				name: binding.name,
				entity: entity.trim(),
				measurement: binding.kind === 'sensor' ? measurement : undefined,
				role: binding.kind === 'fan' || binding.kind === 'controller' ? role : undefined,
				rpmEntity: binding.kind === 'controller' ? rpmEntity.trim() : undefined,
				sizeMm: binding.kind === 'fan' ? sizeMm || undefined : undefined,
				maxRpm: binding.kind === 'fan' ? maxRpm || undefined : undefined,
				airflowCfm: binding.kind === 'fan' ? airflowCfm || undefined : undefined,
				staticPressureMmH2O: binding.kind === 'fan' ? staticPressureMmH2O || undefined : undefined,
				startingVoltage: binding.kind === 'fan' ? startingVoltage || undefined : undefined,
				wattage: binding.kind === 'light' ? wattage || 0 : undefined,
				primary: binding.kind === 'light' ? primary : undefined
			});
			flash?.('ok', 'Device saved');
			open = false;
			onSaved();
		} catch (e) {
			flash?.('err', e instanceof Error ? e.message : 'Save failed');
		} finally {
			busy = false;
		}
	}

	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-rig-500 focus:outline-none';
</script>

<Dialog
	bind:open
	title={editing ? 'Edit device' : 'Add device'}
	description={editing
		? 'Update this binding’s name, entity and role.'
		: 'Pick a product from the catalogue and map its entities.'}
>
	{#if editing && binding}
		<div class="space-y-4">
			<label class="block">
				<span class="text-sm text-rig-400">Name</span>
				<input bind:value={name} class="{field} mt-1" />
			</label>
			{#if binding.kind !== 'light' && binding.kind !== 'fan'}
				<label class="block">
					<span class="text-sm text-rig-400">Entity</span>
					<input bind:value={entity} class="{field} mt-1 font-mono text-xs" />
				</label>
			{/if}

			{#if binding.kind === 'sensor'}
				<label class="block">
					<span class="text-sm text-rig-400">Measurement</span>
					<Select
						items={measurementItems}
						value={measurement}
						onValueChange={(v) => (measurement = v as Measurement)}
						class="mt-1"
					/>
				</label>
			{:else if binding.kind === 'fan'}
				<label class="block">
					<span class="text-sm text-rig-400">Role</span>
					<Select
						items={roleItems}
						value={role}
						onValueChange={(v) => (role = v as Role)}
						class="mt-1"
					/>
				</label>
				<label class="block">
					<span class="text-sm text-rig-400">Controller channel</span>
					<Select bind:value={controllerChannelId} placeholder="Choose a channel…" items={controllerItems} class="mt-1" />
					<p class="mt-1 text-xs text-rig-500">The channel contains both PWM control and RPM feedback. Multiple daisy-chained fans may share it.</p>
				</label>
				<div class="grid grid-cols-2 gap-3">
					<label><span class="text-sm text-rig-400">Fan size (mm)</span><input type="number" min="0" bind:value={sizeMm} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Maximum RPM</span><input type="number" min="0" bind:value={maxRpm} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Airflow (CFM)</span><input type="number" min="0" step="0.1" bind:value={airflowCfm} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Static pressure (mmH₂O)</span><input type="number" min="0" step="0.01" bind:value={staticPressureMmH2O} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Starting voltage (V)</span><input type="number" min="0" max="48" step="0.1" bind:value={startingVoltage} class="{field} mt-1" /></label>
				</div>
			{:else if binding.kind === 'controller'}
				<label class="block">
					<span class="text-sm text-rig-400">Role</span>
					<Select items={roleItems} value={role} onValueChange={(v) => (role = v as Role)} class="mt-1" />
				</label>
				<label class="block">
					<span class="text-sm text-rig-400">RPM entity <span class="text-rig-600">(optional)</span></span>
					<input bind:value={rpmEntity} placeholder="sensor.fan_rpm" class="{field} mt-1 font-mono text-xs" />
				</label>
			{:else if binding.kind === 'light'}
				<label class="block">
					<span class="text-sm text-rig-400">Power controller</span>
					<Select bind:value={powerControllerId} placeholder="None" items={[...new Map(bindings.filter((b) => b.kind === 'power').map((b) => [b.deviceId, b.deviceName])).entries()].map(([value, label]) => ({ value, label }))} class="mt-1" />
				</label>
				<label class="block">
					<span class="text-sm text-rig-400">Wattage (W)</span>
					<input type="number" min="0" step="1" bind:value={wattage} placeholder="e.g. 150" class="{field} mt-1" />
				</label>
				<label class="flex items-center justify-between gap-3">
					<span>
						<span class="text-sm text-rig-100">Primary grow light</span>
						<span class="block text-xs text-rig-500">The box's main light. Only one can be primary.</span>
					</span>
					<Switch bind:checked={primary} />
				</label>
			{/if}

			<div class="flex justify-end gap-2 pt-2">
				<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
				<Button onclick={saveEdit} disabled={busy || (binding.kind === 'fan' ? !controllerChannelId : binding.kind !== 'light' && !entity.trim())}>Save</Button>
			</div>
		</div>
	{:else}
		<CatalogDevicePicker {catalog} {discovered} {bindings} {usedEntities} onAdd={addDevices} onSelectProduct={onInstall} {initialCategory} />
	{/if}
</Dialog>
