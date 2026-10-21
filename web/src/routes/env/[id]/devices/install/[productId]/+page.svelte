<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { toast } from '$lib/toast.svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { createBinding, getBindings, getCatalog, getDiscovery, updateBinding } from '$lib/api';
	import type { Binding, BindingTemplate, CameraType, CatalogProduct, DiscoveredEntity, FanType, IrrigationType, RetentionPolicy, Role } from '$lib/types';
	import { Button, Select } from '$lib/components/ui';
	import CameraPreview from '$lib/components/CameraPreview.svelte';
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import CheckCircle2 from '@lucide/svelte/icons/circle-check';
	import ExternalLink from '@lucide/svelte/icons/external-link';

	const environmentId = $derived(page.params.id);
	const productId = $derived(page.params.productId);
	let product = $state<CatalogProduct | null>(null);
	let discovered = $state<DiscoveredEntity[]>([]);
	let bindings = $state<Binding[]>([]);
	let selected = $state<string[]>([]);
	let selectedRPM = $state<string[]>([]);
	let selectedHADevice = $state('');
	let manual = $state(false);
	let assignedLightId = $state('');
	let assignedPowerControllerId = $state('');
	let standaloneName = $state('');
	let controllerChannelId = $state('');
	let fanRole = $state<Role>('unassigned');
	let lightWattage = $state(100);
	let productVariantId = $state('__custom__');
	let overrideFanSpecs = $state(false);
	let fanType = $state<FanType>('other');
	let fanSizeMm = $state(0);
	let fanMaxRpm = $state(0);
	let fanAirflowCfm = $state(0);
	let fanStaticPressure = $state(0);
	let fanStartingVoltage = $state(0);
	let fanDuctSizeInches = $state(0);
	let fanNoiseDba = $state(0);
	let cameraStreamUrl = $state('');
	let cameraType = $state<CameraType>('snapshot');
	let cameraCaptureInterval = $state(60);
	let cameraRetentionDays = $state(0);
	let cameraStorageMb = $state(0);
	let cameraRetentionPolicy = $state<RetentionPolicy>('alert');
	let cameraSource = $state<'url' | 'homeassistant'>('url');
	let cameraEntity = $state('');
	let irrigationType = $state<IrrigationType>('autopot');
	let reservoirL = $state(0);
	let valveCount = $state(0);
	let loading = $state(true);
	let busy = $state(false);
	let error = $state<string | null>(null);

	function matches(template: BindingTemplate, entity: DiscoveredEntity) {
		return entity.kind === template.kind && (!template.measurement || entity.measurement === template.measurement);
	}

	// Capabilities that cannot report anything without a Home Assistant entity.
	// Fixtures, fans and passive irrigation carry none, and cameras get their
	// source from the camera section below.
	function needsEntity(template: BindingTemplate) {
		return template.kind !== 'light' && template.kind !== 'fan' && template.kind !== 'irrigation' && template.kind !== 'camera';
	}

	function candidates(template: BindingTemplate) {
		return discovered.filter((entity) =>
			matches(template, entity) &&
			(!selectedHADevice || entity.haDeviceId === selectedHADevice) &&
			!bindings.some((binding) => binding.entity === entity.entity)
		);
	}

	// Pre-map the likeliest entity for each measuring capability. The selects
	// stay visible so the grower can correct the guess before installing.
	function prefillEntities() {
		const used = new Set<string>();
		selected = (product?.provides ?? []).map((template) => {
			if (!needsEntity(template)) return '';
			const chosen = candidates(template)
				.filter((entity) => !used.has(entity.entity))
				.sort((a, b) => a.entity.length - b.entity.length)[0];
			if (chosen) used.add(chosen.entity);
			return chosen?.entity ?? '';
		});
	}

	function rpmCandidates() {
		return discovered.filter((entity) =>
			entity.kind === 'sensor' && entity.unit?.toLowerCase() === 'rpm' &&
			(!selectedHADevice || entity.haDeviceId === selectedHADevice) &&
			!bindings.some((binding) => binding.rpmEntity === entity.entity)
		);
	}

	function channelNumber(value: string) {
		return value.match(/(?:fan|channel|pwm)[ _-]?(\d+)/i)?.[1] ?? value.match(/(\d+)/)?.[1] ?? '';
	}

	function chooseDevice(deviceId: string) {
		selectedHADevice = deviceId;
		manual = false;
		const entities = discovered.filter((entity) => entity.haDeviceId === deviceId);
		const used = new Set<string>();
		selected = (product?.provides ?? []).map((template) => {
			if (template.kind === 'light') return '';
			const options = entities
				.filter((entity) => matches(template, entity))
				.filter((entity) => template.kind !== 'power' || !entity.entityCategory)
				.filter((entity) => !used.has(entity.entity));
			const n = channelNumber(template.label);
			const chosen = options.find((entity) => n && channelNumber(`${entity.name} ${entity.entity}`) === n) ?? options.sort((a, b) => a.entity.length - b.entity.length)[0];
			if (chosen) used.add(chosen.entity);
			return chosen?.entity ?? '';
		});
		const rpmOptions = entities.filter((entity) => entity.kind === 'sensor' && entity.unit?.toLowerCase() === 'rpm');
		const usedRPM = new Set<string>();
		selectedRPM = (product?.provides ?? []).map((template) => {
			if (!template.rpmEntityDomain) return '';
			const n = channelNumber(template.label);
			const options = rpmOptions.filter((entity) => !usedRPM.has(entity.entity));
			const chosen = options.find((entity) => n && channelNumber(`${entity.name} ${entity.entity}`) === n) ?? options[0];
			if (chosen) usedRPM.add(chosen.entity);
			return chosen?.entity ?? '';
		});
	}

	onMount(async () => {
		try {
			const [catalog, found, existing] = await Promise.all([getCatalog(), getDiscovery(), getBindings()]);
			product = catalog.find((entry) => entry.id === productId) ?? null;
			discovered = found;
			bindings = existing;
			if (!product) error = 'Catalog device not found';
			else {
				standaloneName = `${product.brand} ${product.model}`;
				fanRole = product.provides?.find((template) => template.kind === 'fan')?.role ?? 'unassigned';
				const light = product.provides?.find((template) => template.kind === 'light');
				lightWattage = light?.wattage || 100;
				fanType = product.fanType ?? 'other';
				irrigationType = product.provides?.find((template) => template.kind === 'irrigation')?.irrigationType ?? 'autopot';
				if (product.products?.length) selectVariant(product.products[0].id);
				if (!product.haIntegration) prefillEntities();
			}
		} catch (e) {
			error = errMsg(e, 'Installation data could not be loaded');
		} finally {
			loading = false;
		}
	});

	const haDevices = $derived.by(() => {
		if (!product) return [] as { id: string; name: string; model: string; entities: number }[];
		const groups = new Map<string, { id: string; name: string; model: string; entities: number }>();
		for (const entity of discovered) {
			if (!entity.haDeviceId || (product.haIntegration && entity.integration !== product.haIntegration)) continue;
			if (!(product.provides ?? []).some((template) => matches(template, entity))) continue;
			const group = groups.get(entity.haDeviceId) ?? { id: entity.haDeviceId, name: entity.deviceName || entity.name, model: entity.model || '', entities: 0 };
			group.entities++;
			groups.set(entity.haDeviceId, group);
		}
		return [...groups.values()].sort((a, b) => a.name.localeCompare(b.name));
	});
	const uniqueMappings = $derived(new Set(selected.filter(Boolean)).size === selected.filter(Boolean).length && new Set(selectedRPM.filter(Boolean)).size === selectedRPM.filter(Boolean).length);
	// A generic camera (a camera product without a Home Assistant integration)
	// streams from a URL the grower supplies here, instead of an HA entity.
	const isGenericCamera = $derived(!!product && !product.haIntegration && (product.provides ?? []).some((template) => template.kind === 'camera'));
	const isIrrigation = $derived(!!product && (product.provides ?? []).some((template) => template.kind === 'irrigation'));
	// Products without a Home Assistant integration still need entities when
	// they measure something — a BLE sensor reports through whichever
	// integration exposed it, so the mapping is offered here directly.
	const standaloneEntityTemplates = $derived(
		product && !product.haIntegration
			? (product.provides ?? []).map((template, index) => ({ template, index })).filter((item) => needsEntity(item.template))
			: []
	);
	const standaloneEntitiesReady = $derived(standaloneEntityTemplates.every((item) => !!selected[item.index]));
	const cameraEntities = $derived(discovered.filter((item) => item.kind === 'camera' && !bindings.some((binding) => binding.entity === item.entity)));
	const cameraUrlValid = $derived(cameraType === 'rtsp' ? /^rtsp:\/\//i.test(cameraStreamUrl.trim()) : /^https?:\/\//.test(cameraStreamUrl.trim()));
	const cameraSourceValid = $derived(cameraSource === 'url' ? cameraUrlValid : !!cameraEntity);
	const ready = $derived(!!product && !!standaloneName.trim() && uniqueMappings && (!(product.provides ?? []).some((template) => template.kind === 'light') || lightWattage > 0) && (!isGenericCamera || cameraSourceValid) && standaloneEntitiesReady && (!product.haIntegration ||(!!selectedHADevice && (product.provides ?? []).every((template, i) => (template.kind === 'light' || !!selected[i]) && (!template.rpmEntityDomain || !!selectedRPM[i])))));
	const detectedName = $derived(discovered.find((entity) => entity.haDeviceId === selectedHADevice)?.deviceName);
	const lights = $derived(bindings.filter((binding) => binding.environmentId === environmentId && binding.kind === 'light'));
	const powerControllers = $derived.by(() => {
		const devices = new Map<string, string>();
		for (const binding of bindings) {
			if (binding.environmentId === environmentId && binding.kind === 'power') devices.set(binding.deviceId, binding.deviceName);
		}
		return [...devices.entries()].map(([id, name]) => ({ id, name }));
	});
	const controllerChannels = $derived(bindings.filter((binding) => binding.environmentId === environmentId && binding.kind === 'controller'));
	const productVariantItems = $derived([...(product?.products ?? []).map((variant) => ({ value: variant.id, label: `${variant.brand ? `${variant.brand} ` : ''}${variant.model ?? variant.id}` })), { value: '__custom__', label: 'Custom' }]);
	const showFanSpecs = $derived(productVariantId === '__custom__' || overrideFanSpecs);
	function selectVariant(id: string) {
		productVariantId = id;
		const variant = product?.products?.find((item) => item.id === id);
		if (!variant) {
			overrideFanSpecs = true;
			standaloneName = `${product?.brand ?? ''} ${product?.model ?? 'Custom fan'}`.trim();
			fanSizeMm = fanMaxRpm = fanAirflowCfm = fanStaticPressure = fanStartingVoltage = fanDuctSizeInches = fanNoiseDba = 0;
			return;
		}
		overrideFanSpecs = false;
		standaloneName = `${variant.brand ?? product?.brand ?? ''} ${variant.model ?? ''}`.trim() || variant.id;
		const specs = variant.specs ?? {};
		lightWattage = specs.wattage ?? lightWattage;
		reservoirL = specs.reservoirL ?? reservoirL;
		valveCount = specs.valveCount ?? valveCount;
		fanSizeMm = specs.sizeMm ?? 0;
		fanMaxRpm = specs.maxRpm ?? 0;
		fanAirflowCfm = specs.airflowCfm ?? 0;
		fanStaticPressure = specs.staticPressureMmH2O ?? 0;
		fanStartingVoltage = specs.startingVoltage ?? 0;
		fanDuctSizeInches = specs.ductSizeInches ?? 0;
		fanNoiseDba = specs.noiseDba ?? 0;
	}

	async function install() {
		if (!product || !ready) return;
		busy = true;
		error = null;
		const deviceId = crypto.randomUUID();
		const deviceName = product.haIntegration ? detectedName || standaloneName : standaloneName.trim();
		try {
			for (const [i, template] of (product.provides ?? []).entries()) {
				await createBinding({
					deviceId,
					deviceName,
					productId: product.id,
					powerControllerId: template.kind === 'light' ? assignedPowerControllerId || undefined : undefined,
					controllerChannelId: template.kind === 'fan' ? controllerChannelId : undefined,
					environmentId: environmentId!,
					kind: template.kind,
					name: template.kind === 'light' || template.kind === 'camera' ? deviceName : template.label,
					entity: template.kind === 'camera' && isGenericCamera && cameraSource === 'homeassistant' ? cameraEntity : selected[i] ?? '',
					measurement: template.measurement,
					role: template.kind === 'fan' ? fanRole : template.role,
					fanType: template.kind === 'fan' ? fanType : undefined,
					sizeMm: template.kind === 'fan' ? fanSizeMm || undefined : undefined,
					maxRpm: template.kind === 'fan' ? fanMaxRpm || undefined : undefined,
					airflowCfm: template.kind === 'fan' ? fanAirflowCfm || undefined : undefined,
					staticPressureMmH2O: template.kind === 'fan' ? fanStaticPressure || undefined : undefined,
					startingVoltage: template.kind === 'fan' ? fanStartingVoltage || undefined : undefined,
					ductSizeInches: template.kind === 'fan' ? fanDuctSizeInches || undefined : undefined,
					noiseDba: template.kind === 'fan' ? fanNoiseDba || undefined : undefined,
					rpmEntity: template.kind === 'controller' ? selectedRPM[i] || undefined : undefined,
					wattage: template.kind === 'light' ? lightWattage : template.wattage,
					streamUrl: template.kind === 'camera' && isGenericCamera && cameraSource === 'url' ? cameraStreamUrl.trim() : undefined,
					cameraType: template.kind === 'camera' && isGenericCamera && cameraSource === 'url' ? cameraType : undefined
					,cameraCaptureInterval: template.kind === 'camera' && cameraType === 'rtsp' ? cameraCaptureInterval : undefined
					,cameraRetentionDays: template.kind === 'camera' && cameraType === 'rtsp' ? cameraRetentionDays : undefined
					,cameraStorageMb: template.kind === 'camera' && cameraType === 'rtsp' ? cameraStorageMb : undefined
					,cameraRetentionPolicy: template.kind === 'camera' && cameraType === 'rtsp' ? cameraRetentionPolicy : undefined
					,irrigationType: template.kind === 'irrigation' ? irrigationType : undefined
					// Manual AutoPot today: passive is the only supported mode.
					,irrigationMode: template.kind === 'irrigation' ? 'passive' : undefined
					,reservoirL: template.kind === 'irrigation' ? reservoirL || undefined : undefined
					,valveCount: template.kind === 'irrigation' ? valveCount || undefined : undefined
				});
			}
			if (assignedLightId) {
				const light = lights.find((binding) => binding.id === assignedLightId);
				if (light) {
					await updateBinding(light.id, {
						deviceId: light.deviceId,
						deviceName: light.deviceName,
						productId: light.productId,
						powerControllerId: deviceId,
						environmentId: light.environmentId,
						kind: light.kind,
						name: light.name,
						entity: '',
						wattage: light.wattage,
						primary: light.primary
					});
				}
			}
			toast.success('Device installed', { description: deviceName });
			goto(`/env/${environmentId}?tab=equipment`);
		} catch (e) {
			error = errMsg(e, 'Installation failed');
			toast.error('Installation failed', { description: errMsg(e, 'Installation failed') });
		} finally {
			busy = false;
		}
	}
</script>

<a href="/env/{environmentId}?tab=equipment" class="mb-5 inline-flex items-center gap-1 text-sm text-rig-400 hover:text-rig-100"><ArrowLeft size={15} /> Back to equipment</a>

{#if loading}
	<p class="text-rig-400">Inspecting Home Assistant…</p>
{:else if !product}
	<p class="text-danger">{error ?? 'Device not found'}</p>
{:else}
	<div class="mx-auto max-w-3xl space-y-5">
		<header>
			<div class="text-xs font-medium uppercase tracking-wider text-rig-400">Device installation</div>
			<h1 class="mt-1 text-3xl font-semibold">{product.brand} {product.model}</h1>
			<p class="mt-2 text-rig-400">{product.description}</p>
		</header>

		<div class="grid gap-3 rounded-xl border border-rig-800 bg-rig-900/40 p-4 text-sm sm:grid-cols-4">
			<div><div class="text-xs text-rig-500">Version</div>{product.version}</div>
			<div><div class="text-xs text-rig-500">Author</div>{product.author}</div>
			<div><div class="text-xs text-rig-500">Home Assistant</div>{product.haIntegration ?? 'Not required'}</div>
			{#if product.maxChannels}<div><div class="text-xs text-rig-500">PWM channels</div>{product.maxChannels}</div>{/if}
		</div>
		{#if product.haIntegration}
		<section class="rounded-xl border border-rig-800 bg-rig-900/40 p-5">
			<h2 class="font-semibold">Automatic setup</h2>
			<p class="mt-1 text-sm text-rig-500">Select the exact Home Assistant device you want to import. GrowRig will never choose one for you.</p>
			{#if haDevices.length}
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Home Assistant device</span>
					<Select value={selectedHADevice} onValueChange={chooseDevice} placeholder="Choose a device…" items={haDevices.map((device) => ({ value: device.id, label: `${device.name}${device.model ? ` · ${device.model}` : ''} · ${device.entities} matching entities` }))} class="mt-1" />
				</label>
			{:else}
				<div class="mt-3 rounded-lg bg-warn/10 px-3 py-3 text-sm text-warn">
					No configured {product.brand} device was found in Home Assistant. Add it through the {product.haIntegration} integration, then reload this page.
				</div>
			{/if}

			{#if selectedHADevice}
				{#if ready}
					<div class="mt-4 flex items-center gap-2 rounded-lg bg-leaf/10 px-3 py-2 text-sm text-leaf"><CheckCircle2 size={17} /> Entities matched automatically for {detectedName}</div>
				{:else}
					<div class="mt-4 rounded-lg bg-warn/10 px-3 py-2 text-sm text-warn">Automatic matching is incomplete. Open Extended options to finish the mapping.</div>
				{/if}
				<label class="mt-4 flex cursor-pointer items-center gap-2 text-sm text-rig-300">
					<input type="checkbox" bind:checked={manual} class="h-4 w-4 accent-green-500" />
					Extended options
				</label>
			{/if}

			{#if selectedHADevice && manual && (product.provides ?? []).some((template) => template.rpmEntityDomain)}
				<div class="mt-4 space-y-4 rounded-lg border border-rig-800 p-4">
					<div><h3 class="text-sm font-medium text-rig-200">PWM channel mapping</h3><p class="mt-1 text-xs text-rig-500">Select one speed control and its matching tachometer sensor for each channel.</p></div>
					{#each product.provides ?? [] as template, i}
						<label class="grid items-center gap-2 sm:grid-cols-[1fr_1.7fr]">
							<span class="text-sm text-rig-300">{template.label} speed</span>
							<Select bind:value={selected[i]} placeholder="Choose fan entity…" items={candidates(template).map((entity) => ({ value: entity.entity, label: `${entity.name} — ${entity.entity}` }))} />
						</label>
						{#if template.rpmEntityDomain}
							<label class="grid items-center gap-2 sm:grid-cols-[1fr_1.7fr]">
								<span class="text-sm text-rig-300">{template.label} RPM</span>
								<Select bind:value={selectedRPM[i]} placeholder="Choose RPM sensor…" items={rpmCandidates().map((entity) => ({ value: entity.entity, label: `${entity.name} — ${entity.entity}` }))} />
							</label>
						{/if}
					{/each}
					{#if !uniqueMappings}<p class="text-xs text-danger">Each channel must use a different fan and RPM entity.</p>{/if}
				</div>
			{/if}

			{#if selectedHADevice && manual && !(product.provides ?? []).some((template) => template.rpmEntityDomain)}
			<div class="mt-4 space-y-3 rounded-lg border border-rig-800 p-4">
				{#each product.provides ?? [] as template, i}
					<label class="grid items-center gap-2 sm:grid-cols-[1fr_1.7fr]">
						<span class="text-sm text-rig-300">{template.label}</span>
						{#if template.kind === 'light'}
							<span class="text-sm text-rig-500">No entity required</span>
						{:else}
							<Select bind:value={selected[i]} placeholder="Select manually…" items={candidates(template).map((entity) => ({ value: entity.entity, label: `${entity.deviceName || entity.name} — ${entity.entity}` }))} />
						{/if}
					</label>
				{/each}
			</div>
			{/if}
		</section>
		{:else}
		<section class="rounded-xl border border-rig-800 bg-rig-900/40 p-5">
			<h2 class="font-semibold">Device details</h2>
			<p class="mt-1 text-sm text-rig-500">This device does not require Home Assistant. You can connect a controller now or assign one later.</p>
			<label class="mt-4 block">
				<span class="text-sm text-rig-300">Name</span>
				<input bind:value={standaloneName} class="mt-1 w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 text-sm focus:border-leaf focus:outline-none" />
			</label>
			{#if standaloneEntityTemplates.length}
				{#if haDevices.length > 1}
					<label class="mt-4 block">
						<span class="text-sm text-rig-300">Device <span class="text-rig-600">(fills the mapping below)</span></span>
						<Select value={selectedHADevice} onValueChange={chooseDevice} placeholder="Choose a device…" items={haDevices.map((device) => ({ value: device.id, label: `${device.name}${device.model ? ` · ${device.model}` : ''} · ${device.entities} matching entities` }))} class="mt-1" />
					</label>
				{/if}
				<div class="mt-4 space-y-3 rounded-lg border border-rig-800 p-4">
					<div>
						<h3 class="text-sm font-medium text-rig-200">Readings</h3>
						<p class="mt-1 text-xs text-rig-500">Pick the entity that reports each measurement. Bluetooth devices appear once Home Assistant has paired them.</p>
					</div>
					{#each standaloneEntityTemplates as item}
						<label class="grid items-center gap-2 sm:grid-cols-[1fr_1.7fr]">
							<span class="text-sm text-rig-300">{item.template.label}</span>
							<Select bind:value={selected[item.index]} placeholder="Choose an entity…" items={candidates(item.template).map((entity) => ({ value: entity.entity, label: `${entity.deviceName || entity.name} — ${entity.entity}` }))} />
						</label>
					{/each}
					{#if !standaloneEntitiesReady && !standaloneEntityTemplates.some((item) => candidates(item.template).length)}
						<p class="text-xs text-warn">No matching entity was found. Pair the device in Home Assistant, then reload this page.</p>
					{/if}
					{#if !uniqueMappings}<p class="text-xs text-danger">Each reading must use a different entity.</p>{/if}
				</div>
			{/if}
			{#if isGenericCamera}
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Camera source</span>
					<Select value={cameraSource} onValueChange={(value) => (cameraSource = value as 'url' | 'homeassistant')} items={[{ value: 'url', label: 'Direct stream URL' }, { value: 'homeassistant', label: 'Home Assistant entity' }]} class="mt-1" />
				</label>
				{#if cameraSource === 'homeassistant'}
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Home Assistant camera</span>
					<Select bind:value={cameraEntity} placeholder="Choose a camera entity…" items={cameraEntities.map((item) => ({ value: item.entity, label: `${item.deviceName || item.name} — ${item.entity}` }))} class="mt-1" />
					<span class="mt-1 block text-xs text-rig-500">GrowRig fetches snapshots through Home Assistant, so its URL and access token stay private.</span>
				</label>
				{:else}
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Stream URL</span>
					<input bind:value={cameraStreamUrl} placeholder={cameraType === 'rtsp' ? 'rtsp://user:password@192.168.1.50/stream' : 'http://192.168.1.50/snapshot.jpg'} class="mt-1 w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 font-mono text-xs focus:border-leaf focus:outline-none" />
					<span class="mt-1 block text-xs text-rig-500">JPEG and MJPEG render directly. RTSP unicast is securely relayed over TCP by GrowRig.</span>
				</label>
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Stream type</span>
					<Select value={cameraType} onValueChange={(value) => (cameraType = value as CameraType)} items={[{ value: 'snapshot', label: 'Snapshot (refreshing JPEG URL)' }, { value: 'mjpeg', label: 'MJPEG stream' }, { value: 'rtsp', label: 'RTSP unicast (relayed by GrowRig)' }]} class="mt-1" />
				</label>
				{#if cameraUrlValid && cameraType !== 'rtsp'}
					<div class="mt-4">
						<span class="text-sm text-rig-300">Preview</span>
						<div class="mt-1 max-w-sm"><CameraPreview url={cameraStreamUrl.trim()} type={cameraType} /></div>
					</div>
				{:else if cameraType === 'rtsp'}
					<div class="mt-4 grid gap-3 sm:grid-cols-3">
						<label><span class="text-sm text-rig-300">Snapshot interval</span><div class="relative mt-1"><input type="number" min="5" max="3600" bind:value={cameraCaptureInterval} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-12 text-sm" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">sec</span></div></label>
						<label><span class="text-sm text-rig-300">Age limit</span><div class="relative mt-1"><input type="number" min="0" max="3650" bind:value={cameraRetentionDays} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-14 text-sm" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">days</span></div></label>
						<label><span class="text-sm text-rig-300">Storage limit</span><div class="relative mt-1"><input type="number" min="0" max="1024000" bind:value={cameraStorageMb} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-12 text-sm" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">MB</span></div></label>
					</div>
					<label class="mt-3 block">
						<span class="text-sm text-rig-300">When a limit is passed</span>
						<Select value={cameraRetentionPolicy} onValueChange={(v) => (cameraRetentionPolicy = v as RetentionPolicy)} items={[{ value: 'alert', label: 'Keep everything, alert me when a limit is passed' }, { value: 'delete', label: 'Delete the oldest snapshots to stay within limits' }]} class="mt-1" />
					</label>
					<p class="mt-2 text-xs text-rig-500">GrowRig stays connected and saves snapshots atomically. Leave a limit at 0 for unlimited — nothing is deleted unless you choose deletion above.</p>
				{/if}
				{/if}
			{/if}
			{#if isIrrigation}
				{#if product.products?.length}
					<label class="mt-4 block"><span class="text-sm text-rig-300">Kit</span><Select value={productVariantId} onValueChange={selectVariant} items={productVariantItems} class="mt-1" /></label>
				{/if}
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Irrigation type</span>
					<Select value={irrigationType} onValueChange={(value) => (irrigationType = value as IrrigationType)} items={[{ value: 'autopot', label: 'AutoPot (gravity-fed trays)' }, { value: 'drip', label: 'Drip / emitters' }, { value: 'wick', label: 'Wick' }, { value: 'ebb_flow', label: 'Ebb & flow' }, { value: 'hand', label: 'Hand-watering' }]} class="mt-1" />
				</label>
				<div class="mt-4 grid gap-3 sm:grid-cols-2">
					<label><span class="text-sm text-rig-300">Reservoir <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="0.5" bind:value={reservoirL} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-10 text-sm focus:border-leaf focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">L</span></div></label>
					<label><span class="text-sm text-rig-300">Trays / valves <span class="text-rig-600">(optional)</span></span><input type="number" min="0" step="1" bind:value={valveCount} class="mt-1 w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 text-sm focus:border-leaf focus:outline-none" /></label>
				</div>
				<p class="mt-2 text-xs text-rig-500">Passive setup — no pump or Home Assistant entity. Its presence marks the grow as auto-watered; you top up the reservoir and check pH/EC instead of hand-watering.</p>
			{/if}
			{#if (product.provides ?? []).some((template) => template.kind === 'light')}
				{#if product.products?.length}
					<label class="mt-4 block"><span class="text-sm text-rig-300">Light model</span><Select value={productVariantId} onValueChange={selectVariant} items={productVariantItems} class="mt-1" /></label>
				{/if}
				{#if (product.provides ?? []).some((template) => template.kind === 'light' && !template.wattage)}
					<label class="mt-4 block">
						<span class="text-sm text-rig-300">Rated wattage</span>
						<div class="relative mt-1">
							<input type="number" min="1" max="100000" step="1" bind:value={lightWattage} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-12 text-sm focus:border-leaf focus:outline-none" />
							<span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-sm text-rig-500">W</span>
						</div>
					</label>
				{/if}
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Power controller <span class="text-rig-600">(optional)</span></span>
					<Select bind:value={assignedPowerControllerId} placeholder="None — assign later" items={powerControllers.map((controller) => ({ value: controller.id, label: controller.name }))} class="mt-1" />
				</label>
			{/if}
			{#if (product.provides ?? []).some((template) => template.kind === 'fan')}
				{#if product.products?.length}
					<label class="mt-4 block"><span class="text-sm text-rig-300">Fan model</span><Select value={productVariantId} onValueChange={selectVariant} items={productVariantItems} class="mt-1" /></label>
					{#if productVariantId !== '__custom__'}
						<label class="mt-3 flex cursor-pointer items-center gap-2 text-sm text-rig-400"><input type="checkbox" bind:checked={overrideFanSpecs} class="h-4 w-4 accent-green-500" />Override preset specifications</label>
					{/if}
				{/if}
				{#if showFanSpecs}
				<div class="mt-4 grid gap-3 sm:grid-cols-2">
					<label><span class="text-sm text-rig-300">Fan size <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="1" bind:value={fanSizeMm} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-12 text-sm focus:border-leaf focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">mm</span></div></label>
					<label><span class="text-sm text-rig-300">Maximum speed <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="1" bind:value={fanMaxRpm} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-14 text-sm focus:border-leaf focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">RPM</span></div></label>
					<label><span class="text-sm text-rig-300">Airflow <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="0.1" bind:value={fanAirflowCfm} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-14 text-sm focus:border-leaf focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">CFM</span></div></label>
					<label><span class="text-sm text-rig-300">Static pressure <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="0.01" bind:value={fanStaticPressure} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-20 text-sm focus:border-leaf focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">mmH₂O</span></div></label>
					<label><span class="text-sm text-rig-300">Starting voltage <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" max="48" step="0.1" bind:value={fanStartingVoltage} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-10 text-sm focus:border-leaf focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">V</span></div></label>
					<label><span class="text-sm text-rig-300">Duct size <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="0.1" bind:value={fanDuctSizeInches} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-10 text-sm focus:border-leaf focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">in</span></div></label>
					<label><span class="text-sm text-rig-300">Noise <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="0.1" bind:value={fanNoiseDba} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-12 text-sm focus:border-leaf focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">dBA</span></div></label>
				</div>
				{/if}
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Role</span>
					<Select value={fanRole} onValueChange={(value) => (fanRole = value as Role)} items={[{ value: 'unassigned', label: 'Unassigned' }, { value: 'exhaust', label: 'Exhaust' }, { value: 'intake', label: 'Intake' }, { value: 'circulation', label: 'Circulation' }]} class="mt-1" />
				</label>
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Controller channel <span class="text-rig-600">(optional)</span></span>
					<Select bind:value={controllerChannelId} placeholder="None — assign later" items={controllerChannels.map((channel) => ({ value: channel.id, label: `${channel.deviceName} — ${channel.name}` }))} class="mt-1" />
				</label>
			{/if}
		</section>
		{/if}

		{#if product.category === 'plug'}
			<section class="rounded-xl border border-rig-800 bg-rig-900/40 p-5">
				<h2 class="font-semibold">Light assignment</h2>
				<p class="mt-1 text-sm text-rig-500">Optionally use this plug as the power controller for an existing light.</p>
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Power a light</span>
					<Select bind:value={assignedLightId} placeholder="None — assign later" items={lights.map((light) => ({ value: light.id, label: light.deviceName }))} class="mt-1" />
				</label>
			</section>
		{/if}

		{#if product.documentation}
			<a href={product.documentation} target="_blank" rel="noreferrer" class="inline-flex items-center gap-1 text-sm text-rig-400 hover:underline">Home Assistant integration instructions <ExternalLink size={14} /></a>
		{/if}
		{#if error}<p class="text-sm text-danger">{error}</p>{/if}
		<div class="flex justify-end"><Button onclick={install} disabled={!ready || busy}>{busy ? 'Installing…' : 'Install device'}</Button></div>
	</div>
{/if}
