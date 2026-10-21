<script lang="ts">
	import { errMsg } from '$lib/errors';
	import type { Binding, CameraType, CatalogProduct, DiscoveredEntity, FanType, IrrigationType, Measurement, RetentionPolicy, Role } from '$lib/types';
	import { createBinding, updateBinding } from '$lib/api';
	import { Button, Dialog, Select, Switch, type SelectItem } from '$lib/components/ui';
	import CatalogDevicePicker, { type BindingDraft } from '$lib/components/CatalogDevicePicker.svelte';
	import CameraPreview from '$lib/components/CameraPreview.svelte';

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
	let fanType = $state<FanType>('other');
	let sizeMm = $state(0);
	let maxRpm = $state(0);
	let airflowCfm = $state(0);
	let staticPressureMmH2O = $state(0);
	let startingVoltage = $state(0);
	let ductSizeInches = $state(0);
	let noiseDba = $state(0);
	let wattage = $state(0);
	let primary = $state(false);
	let powerControllerId = $state('');
	let controllerChannelId = $state('');
	let streamUrl = $state('');
	let cameraType = $state<CameraType>('snapshot');
	let cameraCaptureInterval = $state(60);
	let cameraRetentionDays = $state(0);
	let cameraStorageMb = $state(0);
	let cameraRetentionPolicy = $state<RetentionPolicy>('alert');
	let cameraSource = $state<'url' | 'homeassistant'>('url');
	let irrigationType = $state<IrrigationType>('autopot');
	let reservoirL = $state(0);
	let valveCount = $state(0);
	let busy = $state(false);

	// Reseed the form whenever the target binding changes.
	$effect(() => {
		if (binding) {
			name = binding.name;
			entity = binding.entity;
			streamUrl = binding.streamUrl ?? '';
			cameraType = binding.cameraType ?? 'snapshot';
			cameraCaptureInterval = binding.cameraCaptureInterval ?? 60;
			cameraRetentionDays = binding.cameraRetentionDays ?? 0;
			cameraStorageMb = binding.cameraStorageMb ?? 0;
			cameraRetentionPolicy = binding.cameraRetentionPolicy ?? 'alert';
			cameraSource = binding.entity ? 'homeassistant' : 'url';
			measurement = binding.measurement ?? 'temperature';
			role = binding.role ?? 'unassigned';
			rpmEntity = binding.rpmEntity ?? '';
			fanType = binding.fanType ?? 'other';
			sizeMm = binding.sizeMm ?? 0;
			maxRpm = binding.maxRpm ?? 0;
			airflowCfm = binding.airflowCfm ?? 0;
			staticPressureMmH2O = binding.staticPressureMmH2O ?? 0;
			startingVoltage = binding.startingVoltage ?? 0;
			ductSizeInches = binding.ductSizeInches ?? 0;
			noiseDba = binding.noiseDba ?? 0;
			wattage = binding.wattage ?? 0;
			primary = binding.primary ?? false;
			powerControllerId = binding.powerControllerId ?? '';
			controllerChannelId = binding.controllerChannelId ?? '';
			irrigationType = binding.irrigationType ?? 'autopot';
			reservoirL = binding.reservoirL ?? 0;
			valveCount = binding.valveCount ?? 0;
		}
	});

	const irrigationTypeItems: SelectItem[] = [
		{ value: 'autopot', label: 'AutoPot (gravity-fed trays)' },
		{ value: 'drip', label: 'Drip / emitters' },
		{ value: 'wick', label: 'Wick' },
		{ value: 'ebb_flow', label: 'Ebb & flow' },
		{ value: 'hand', label: 'Hand-watering' }
	];

	const cameraTypeItems: SelectItem[] = [
		{ value: 'snapshot', label: 'Snapshot (refreshing JPEG URL)' },
		{ value: 'mjpeg', label: 'MJPEG stream' },
		{ value: 'rtsp', label: 'RTSP unicast (relayed by GrowRig)' }
	];
	const retentionPolicyItems: SelectItem[] = [
		{ value: 'alert', label: 'Keep everything, alert me when a limit is passed' },
		{ value: 'delete', label: 'Delete the oldest snapshots to stay within limits' }
	];
	const cameraEntityItems = $derived<SelectItem[]>(discovered
		.filter((item) => item.kind === 'camera' && (item.entity === binding?.entity || !usedEntities.has(item.entity)))
		.map((item) => ({ value: item.entity, label: `${item.deviceName || item.name} — ${item.entity}` })));

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
	const fanTypeItems: SelectItem[] = [{ value: 'pc', label: 'PC fan' }, { value: 'inline', label: 'Inline duct fan' }, { value: 'other', label: 'Other' }];
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
			flash?.('err', errMsg(e, 'Add failed'));
		} finally {
			busy = false;
		}
	}

	// A camera edit is valid with either a Home Assistant entity or a stream URL;
	// other kinds keep their existing requirements.
	const canSaveEdit = $derived(
		!binding
			? false
			: binding.kind === 'camera'
				? cameraSource === 'homeassistant' ? !!entity.trim() : !!streamUrl.trim()
				: binding.kind === 'fan' || binding.kind === 'light' || binding.kind === 'irrigation' || !!entity.trim()
	);

	async function saveEdit() {
		if (!binding) return;
		busy = true;
		try {
			await updateBinding(binding.id, {
				deviceId: binding.deviceId,
				deviceName: name.trim() || binding.deviceName,
				productId: binding.productId,
				powerControllerId: binding.kind === 'light' ? powerControllerId || undefined : binding.powerControllerId,
				controllerChannelId: binding.kind === 'fan' ? controllerChannelId || undefined : binding.controllerChannelId,
				environmentId: binding.environmentId,
				kind: binding.kind,
				name: binding.name,
				entity: binding.kind === 'camera' && cameraSource === 'url' ? '' : binding.kind === 'irrigation' ? '' : entity.trim(),
				measurement: binding.kind === 'sensor' ? measurement : undefined,
				role: binding.kind === 'fan' || binding.kind === 'controller' ? role : undefined,
				rpmEntity: binding.kind === 'controller' ? rpmEntity.trim() : undefined,
				fanType: binding.kind === 'fan' ? fanType : undefined,
				sizeMm: binding.kind === 'fan' ? sizeMm || undefined : undefined,
				maxRpm: binding.kind === 'fan' ? maxRpm || undefined : undefined,
				airflowCfm: binding.kind === 'fan' ? airflowCfm || undefined : undefined,
				staticPressureMmH2O: binding.kind === 'fan' ? staticPressureMmH2O || undefined : undefined,
				startingVoltage: binding.kind === 'fan' ? startingVoltage || undefined : undefined,
				ductSizeInches: binding.kind === 'fan' ? ductSizeInches || undefined : undefined,
				noiseDba: binding.kind === 'fan' ? noiseDba || undefined : undefined,
				wattage: binding.kind === 'light' ? wattage || 0 : undefined,
				primary: binding.kind === 'light' ? primary : undefined,
				streamUrl: binding.kind === 'camera' && cameraSource === 'url' ? streamUrl.trim() || undefined : undefined,
				cameraType: binding.kind === 'camera' && cameraSource === 'url' && streamUrl.trim() ? cameraType : undefined
				,cameraCaptureInterval: binding.kind === 'camera' && cameraType === 'rtsp' ? cameraCaptureInterval : undefined
				,cameraRetentionDays: binding.kind === 'camera' && cameraType === 'rtsp' ? cameraRetentionDays : undefined
				,cameraStorageMb: binding.kind === 'camera' && cameraType === 'rtsp' ? cameraStorageMb : undefined
				,cameraRetentionPolicy: binding.kind === 'camera' && cameraType === 'rtsp' ? cameraRetentionPolicy : undefined
				,irrigationType: binding.kind === 'irrigation' ? irrigationType : undefined
				,irrigationMode: binding.kind === 'irrigation' ? (binding.irrigationMode ?? 'passive') : undefined
				,reservoirL: binding.kind === 'irrigation' ? reservoirL || undefined : undefined
				,valveCount: binding.kind === 'irrigation' ? valveCount || undefined : undefined
			});
			flash?.('ok', 'Device saved');
			open = false;
			onSaved();
		} catch (e) {
			flash?.('err', errMsg(e, 'Save failed'));
		} finally {
			busy = false;
		}
	}

	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';
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
			{#if binding.kind !== 'light' && binding.kind !== 'fan' && binding.kind !== 'camera' && binding.kind !== 'irrigation'}
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
				<label class="block"><span class="text-sm text-rig-400">Fan type</span><Select value={fanType} onValueChange={(value) => (fanType = value as FanType)} items={fanTypeItems} class="mt-1" /></label>
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
					<Select bind:value={controllerChannelId} placeholder="None — assign later" items={controllerItems} class="mt-1" />
					<p class="mt-1 text-xs text-rig-500">The channel contains both PWM control and RPM feedback. Multiple daisy-chained fans may share it.</p>
				</label>
				<div class="grid grid-cols-2 gap-3">
					<label><span class="text-sm text-rig-400">Fan size (mm)</span><input type="number" min="0" bind:value={sizeMm} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Maximum RPM</span><input type="number" min="0" bind:value={maxRpm} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Airflow (CFM)</span><input type="number" min="0" step="0.1" bind:value={airflowCfm} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Static pressure (mmH₂O)</span><input type="number" min="0" step="0.01" bind:value={staticPressureMmH2O} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Starting voltage (V)</span><input type="number" min="0" max="48" step="0.1" bind:value={startingVoltage} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Duct size (in)</span><input type="number" min="0" step="0.1" bind:value={ductSizeInches} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Noise (dBA)</span><input type="number" min="0" step="0.1" bind:value={noiseDba} class="{field} mt-1" /></label>
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
			{:else if binding.kind === 'camera'}
				<label class="block">
					<span class="text-sm text-rig-400">Camera source</span>
					<Select value={cameraSource} onValueChange={(v) => (cameraSource = v as 'url' | 'homeassistant')} items={[{ value: 'url', label: 'Direct stream URL' }, { value: 'homeassistant', label: 'Home Assistant entity' }]} class="mt-1" />
				</label>
				{#if cameraSource === 'homeassistant'}
				<label class="block">
					<span class="text-sm text-rig-400">Home Assistant camera</span>
					<Select bind:value={entity} placeholder="Choose a camera entity…" items={cameraEntityItems} class="mt-1" />
				</label>
				{:else}
				<label class="block">
					<span class="text-sm text-rig-400">Stream URL</span>
					<input bind:value={streamUrl} placeholder={cameraType === 'rtsp' ? 'rtsp://user:password@192.168.1.50/stream' : 'http://192.168.1.50/snapshot.jpg'} class="{field} mt-1 font-mono text-xs" />
				</label>
				<label class="block">
					<span class="text-sm text-rig-400">Stream type</span>
					<Select value={cameraType} onValueChange={(v) => (cameraType = v as CameraType)} items={cameraTypeItems} class="mt-1" />
				</label>
				{#if streamUrl.trim() && cameraType !== 'rtsp'}
					<div>
						<span class="text-sm text-rig-400">Preview</span>
						<CameraPreview url={streamUrl.trim()} type={cameraType} class="mt-1" />
					</div>
				{:else if cameraType === 'rtsp'}
					<p class="text-xs text-rig-500">RTSP is relayed over TCP after saving. Credentials remain on the GrowRig server.</p>
					<div class="grid grid-cols-3 gap-3">
						<label><span class="text-xs text-rig-400">Snapshot interval</span><div class="relative mt-1"><input type="number" min="5" max="3600" bind:value={cameraCaptureInterval} class={field} /><span class="pointer-events-none absolute inset-y-0 right-2 flex items-center text-xs text-rig-500">sec</span></div></label>
						<label><span class="text-xs text-rig-400">Age limit</span><div class="relative mt-1"><input type="number" min="0" max="3650" bind:value={cameraRetentionDays} class={field} /><span class="pointer-events-none absolute inset-y-0 right-2 flex items-center text-xs text-rig-500">days</span></div></label>
						<label><span class="text-xs text-rig-400">Storage limit</span><div class="relative mt-1"><input type="number" min="0" max="1024000" bind:value={cameraStorageMb} class={field} /><span class="pointer-events-none absolute inset-y-0 right-2 flex items-center text-xs text-rig-500">MB</span></div></label>
					</div>
					<label class="block">
						<span class="text-xs text-rig-400">When a limit is passed</span>
						<Select value={cameraRetentionPolicy} onValueChange={(v) => (cameraRetentionPolicy = v as RetentionPolicy)} items={retentionPolicyItems} class="mt-1" />
					</label>
					<p class="text-xs text-rig-500">Leave a limit at 0 for unlimited. Recordings are never deleted unless you choose deletion above.</p>
				{/if}
				{/if}
			{:else if binding.kind === 'irrigation'}
				<label class="block">
					<span class="text-sm text-rig-400">Irrigation type</span>
					<Select value={irrigationType} onValueChange={(v) => (irrigationType = v as IrrigationType)} items={irrigationTypeItems} class="mt-1" />
				</label>
				<div class="grid grid-cols-2 gap-3">
					<label><span class="text-sm text-rig-400">Reservoir (L)</span><input type="number" min="0" step="0.5" bind:value={reservoirL} class="{field} mt-1" /></label>
					<label><span class="text-sm text-rig-400">Trays / valves</span><input type="number" min="0" step="1" bind:value={valveCount} class="{field} mt-1" /></label>
				</div>
				<p class="text-xs text-rig-500">Passive setup — no pump or Home Assistant entity.</p>
			{/if}

			<div class="flex justify-end gap-2 pt-2">
				<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
				<Button onclick={saveEdit} disabled={busy || !canSaveEdit}>Save</Button>
			</div>
		</div>
	{:else}
		<CatalogDevicePicker {catalog} {discovered} {bindings} {usedEntities} onAdd={addDevices} onSelectProduct={onInstall} {initialCategory} />
	{/if}
</Dialog>
