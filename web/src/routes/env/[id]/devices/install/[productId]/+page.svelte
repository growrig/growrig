<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { createBinding, getBindings, getCatalog, getDiscovery, updateBinding } from '$lib/api';
	import type { Binding, BindingTemplate, CatalogProduct, DiscoveredEntity, Role } from '$lib/types';
	import { Button, Select } from '$lib/components/ui';
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
	let fanPresetId = $state('__custom__');
	let fanSizeMm = $state(0);
	let fanMaxRpm = $state(0);
	let fanAirflowCfm = $state(0);
	let fanStaticPressure = $state(0);
	let fanStartingVoltage = $state(0);
	let loading = $state(true);
	let busy = $state(false);
	let error = $state<string | null>(null);

	function matches(template: BindingTemplate, entity: DiscoveredEntity) {
		return entity.kind === template.kind && (!template.measurement || entity.measurement === template.measurement);
	}

	function candidates(template: BindingTemplate) {
		return discovered.filter((entity) =>
			matches(template, entity) &&
			(!selectedHADevice || entity.haDeviceId === selectedHADevice) &&
			!bindings.some((binding) => binding.entity === entity.entity)
		);
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
			}
		} catch (e) {
			error = e instanceof Error ? e.message : 'Installation data could not be loaded';
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
	const ready = $derived(!!product && !!standaloneName.trim() && uniqueMappings && (!(product.provides ?? []).some((template) => template.kind === 'light') || lightWattage > 0) && (!(product.provides ?? []).some((template) => template.kind === 'fan') || !!controllerChannelId) && (!product.haIntegration || (!!selectedHADevice && (product.provides ?? []).every((template, i) => (template.kind === 'light' || !!selected[i]) && (!template.rpmEntityDomain || !!selectedRPM[i])))));
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
	const fanPresetItems = $derived([{ value: '__custom__', label: 'Custom specifications' }, ...(product?.fanPresets ?? []).map((preset) => ({ value: preset.id, label: preset.label }))]);
	function selectFanPreset(id: string) {
		fanPresetId = id;
		const preset = product?.fanPresets?.find((item) => item.id === id);
		if (!preset) return;
		standaloneName = preset.label;
		fanSizeMm = preset.sizeMm ?? 0;
		fanMaxRpm = preset.maxRpm ?? 0;
		fanAirflowCfm = preset.airflowCfm ?? 0;
		fanStaticPressure = preset.staticPressureMmH2O ?? 0;
		fanStartingVoltage = preset.startingVoltage ?? 0;
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
					powerControllerId: template.kind === 'light' ? assignedPowerControllerId || undefined : undefined,
					controllerChannelId: template.kind === 'fan' ? controllerChannelId : undefined,
					environmentId: environmentId!,
					kind: template.kind,
					name: template.kind === 'light' ? deviceName : template.label,
					entity: selected[i] ?? '',
					measurement: template.measurement,
					role: template.kind === 'fan' ? fanRole : template.role,
					sizeMm: template.kind === 'fan' ? fanSizeMm || undefined : undefined,
					maxRpm: template.kind === 'fan' ? fanMaxRpm || undefined : undefined,
					airflowCfm: template.kind === 'fan' ? fanAirflowCfm || undefined : undefined,
					staticPressureMmH2O: template.kind === 'fan' ? fanStaticPressure || undefined : undefined,
					startingVoltage: template.kind === 'fan' ? fanStartingVoltage || undefined : undefined,
					rpmEntity: template.kind === 'controller' ? selectedRPM[i] || undefined : undefined,
					wattage: template.kind === 'light' ? lightWattage : template.wattage
				});
			}
			if (assignedLightId) {
				const light = lights.find((binding) => binding.id === assignedLightId);
				if (light) {
					await updateBinding(light.id, {
						deviceId: light.deviceId,
						deviceName: light.deviceName,
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
			goto(`/env/${environmentId}/settings#devices`);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Installation failed';
		} finally {
			busy = false;
		}
	}
</script>

<a href="/env/{environmentId}/settings#devices" class="mb-5 inline-flex items-center gap-1 text-sm text-rig-400 hover:text-rig-100"><ArrowLeft size={15} /> Back to settings</a>

{#if loading}
	<p class="text-rig-400">Inspecting Home Assistant…</p>
{:else if !product}
	<p class="text-danger">{error ?? 'Device not found'}</p>
{:else}
	<div class="mx-auto max-w-3xl space-y-5">
		<header>
			<div class="text-xs font-medium uppercase tracking-wider text-leaf">Device installation</div>
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
				<input bind:value={standaloneName} class="mt-1 w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 text-sm focus:border-rig-500 focus:outline-none" />
			</label>
			{#if (product.provides ?? []).some((template) => template.kind === 'light')}
				{#if (product.provides ?? []).some((template) => template.kind === 'light' && !template.wattage)}
					<label class="mt-4 block">
						<span class="text-sm text-rig-300">Rated wattage</span>
						<div class="relative mt-1">
							<input type="number" min="1" max="100000" step="1" bind:value={lightWattage} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-12 text-sm focus:border-rig-500 focus:outline-none" />
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
				{#if product.fanPresets?.length}
					<label class="mt-4 block"><span class="text-sm text-rig-300">Fan model</span><Select value={fanPresetId} onValueChange={selectFanPreset} items={fanPresetItems} class="mt-1" /></label>
				{/if}
				<div class="mt-4 grid gap-3 sm:grid-cols-2">
					<label><span class="text-sm text-rig-300">Fan size <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="1" bind:value={fanSizeMm} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-12 text-sm focus:border-rig-500 focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">mm</span></div></label>
					<label><span class="text-sm text-rig-300">Maximum speed <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="1" bind:value={fanMaxRpm} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-14 text-sm focus:border-rig-500 focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">RPM</span></div></label>
					<label><span class="text-sm text-rig-300">Airflow <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="0.1" bind:value={fanAirflowCfm} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-14 text-sm focus:border-rig-500 focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">CFM</span></div></label>
					<label><span class="text-sm text-rig-300">Static pressure <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" step="0.01" bind:value={fanStaticPressure} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-20 text-sm focus:border-rig-500 focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">mmH₂O</span></div></label>
					<label><span class="text-sm text-rig-300">Starting voltage <span class="text-rig-600">(optional)</span></span><div class="relative mt-1"><input type="number" min="0" max="48" step="0.1" bind:value={fanStartingVoltage} class="w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2.5 pr-10 text-sm focus:border-rig-500 focus:outline-none" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-rig-500">V</span></div></label>
				</div>
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Role</span>
					<Select value={fanRole} onValueChange={(value) => (fanRole = value as Role)} items={[{ value: 'unassigned', label: 'Unassigned' }, { value: 'exhaust', label: 'Exhaust' }, { value: 'intake', label: 'Intake' }, { value: 'circulation', label: 'Circulation' }]} class="mt-1" />
				</label>
				<label class="mt-4 block">
					<span class="text-sm text-rig-300">Controller channel</span>
					<Select bind:value={controllerChannelId} placeholder="Choose a channel…" items={controllerChannels.map((channel) => ({ value: channel.id, label: `${channel.deviceName} — ${channel.name}` }))} class="mt-1" />
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
			<a href={product.documentation} target="_blank" rel="noreferrer" class="inline-flex items-center gap-1 text-sm text-rig-400 hover:text-leaf">Home Assistant integration instructions <ExternalLink size={14} /></a>
		{/if}
		{#if error}<p class="text-sm text-danger">{error}</p>{/if}
		<div class="flex justify-end"><Button onclick={install} disabled={!ready || busy}>{busy ? 'Installing…' : 'Install device'}</Button></div>
	</div>
{/if}
