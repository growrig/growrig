<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { live } from '$lib/live.svelte';
	import {
		getEnvironments,
		getBindings,
		getCatalog,
		getDiscovery,
		deleteBinding,
		updateBinding
	} from '$lib/api';
	import type { Binding, BindingKind, CatalogProduct, DiscoveredEntity, Environment } from '$lib/types';
	import { measurementUnit } from '$lib/format';
	import KindIcon from '$lib/components/KindIcon.svelte';
	import DeviceModal from '$lib/components/DeviceModal.svelte';
	import CameraStreamStats from '$lib/components/CameraStreamStats.svelte';
	import { Button } from '$lib/components/ui';
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import Plus from '@lucide/svelte/icons/plus';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import Star from '@lucide/svelte/icons/star';

	const id = $derived(page.params.id);
	interface Props { embedded?: boolean }
	let { embedded = false }: Props = $props();

	let environments = $state<Environment[]>([]);
	let bindings = $state<Binding[]>([]);
	let catalog = $state<CatalogProduct[]>([]);
	let discovered = $state<DiscoveredEntity[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let notice = $state<{ kind: 'ok' | 'err'; text: string } | null>(null);
	function flash(kind: 'ok' | 'err', text: string) {
		notice = { kind, text };
		setTimeout(() => (notice = null), 2500);
	}

	async function reload() {
		try {
			[environments, bindings, catalog, discovered] = await Promise.all([
				getEnvironments(),
				getBindings(),
				getCatalog(),
				getDiscovery()
			]);
			error = null;
		} catch (e) {
			error = errMsg(e, 'Failed to reach Grow Core');
		} finally {
			loading = false;
		}
	}
	onMount(() => {
		if (!embedded) {
			goto(`/env/${id}/settings#devices`);
			return;
		}
		reload();
	});

	const env = $derived(environments.find((e) => e.id === id));
	const myBindings = $derived(bindings.filter((b) => b.environmentId === id));
	type DeviceGroup = { id: string; name: string; bindings: Binding[] };
	const devices = $derived.by(() => {
		const grouped = new Map<string, DeviceGroup>();
		for (const b of myBindings) {
			const device = grouped.get(b.deviceId) ?? { id: b.deviceId, name: b.deviceName, bindings: [] };
			device.bindings.push(b);
			grouped.set(b.deviceId, device);
		}
		return [...grouped.values()];
	});
	const sections = $derived([
		{ label: 'Controllers', kind: 'controller' as BindingKind, devices: devices.filter((d) => d.bindings.some((b) => b.kind === 'controller')) },
		{ label: 'Lights', kind: 'light' as BindingKind, devices: devices.filter((d) => d.bindings.some((b) => b.kind === 'light')) },
		{ label: 'Fans & Airflow', kind: 'fan' as BindingKind, devices: devices.filter((d) => d.bindings.some((b) => b.kind === 'fan')) },
		{ label: 'Sensors', kind: 'sensor' as BindingKind, devices: devices.filter((d) => d.bindings.every((b) => b.kind === 'sensor')) },
		{ label: 'Cameras', kind: 'camera' as BindingKind, devices: devices.filter((d) => d.bindings.some((b) => b.kind === 'camera')) },
		{ label: 'Power & Switching', kind: 'power' as BindingKind, devices: devices.filter((d) => d.bindings.some((b) => b.kind === 'power')) },
		{ label: 'Climate Control', kind: 'controller' as BindingKind, devices: [] as DeviceGroup[], future: true }
	]);
	const usedEntities = $derived(new Set(bindings.map((b) => b.entity)));

	// Live view for this environment, keyed by binding id.
	const liveEnv = $derived(live.snapshot?.environments?.find((e) => e.id === id));
	const sensorById = $derived(new Map((liveEnv?.sensors ?? []).map((s) => [s.id, s])));
	const controlById = $derived(new Map((liveEnv?.controls ?? []).map((c) => [c.id, c])));

	// Latest value/state + reachability for one device, from the live snapshot.
	function status(b: Binding): { value: string; online: boolean | null } {
		if (b.kind === 'sensor') {
			const s = sensorById.get(b.id);
			if (!s) return { value: '—', online: null };
			const unit = b.measurement ? measurementUnit[b.measurement] : '';
			return { value: s.ok ? `${s.value}${unit}` : '—', online: s.ok };
		}
		if (b.kind === 'fan') {
			const c = controlById.get(b.id);
			if (!c) return { value: '—', online: liveEnv ? env?.kind === 'tent' : null };
			return { value: `${c.desiredSpeed}%${c.rpm ? ` · ${c.rpm} rpm` : ''}`, online: onlineFromHealth() };
		}
		if (b.kind === 'light') {
			if (!b.powerControllerId) return { value: 'Unassigned', online: null };
			const c = controlById.get(b.id);
			return { value: c ? (c.on ? 'On' : 'Off') : '—', online: onlineFromHealth() };
		}
		if (b.kind === 'power') {
			return { value: '', online: onlineFromHealth() };
		}
		if (b.kind === 'controller') return { value: b.rpmEntity ? 'RPM connected' : 'No RPM', online: onlineFromHealth() };
		return { value: '', online: onlineFromHealth() }; // camera
	}

	function onlineFromHealth(): boolean | null {
		if (!liveEnv) return null;
		return liveEnv.health === 'online';
	}

	function meta(b: Binding): string {
		if (b.kind === 'sensor') return b.measurement ?? 'sensor';
		if (b.kind === 'fan') return b.role ?? 'fan';
		if (b.kind === 'light') return b.wattage ? `${b.wattage} W` : 'light';
		if (b.kind === 'power') return 'switch';
		if (b.kind === 'controller') return b.name;
		return b.kind;
	}

	// Promote a light to primary; the backend clears the flag on the others.
	async function makePrimary(b: Binding) {
		try {
			await updateBinding(b.id, {
				deviceId: b.deviceId,
				deviceName: b.deviceName,
				powerControllerId: b.powerControllerId,
				environmentId: b.environmentId,
				kind: b.kind,
				name: b.name,
				entity: b.entity,
				wattage: b.wattage,
				primary: true
			});
			flash('ok', `${b.name} is now the primary light`);
			reload();
		} catch (e) {
			flash('err', errMsg(e, 'Update failed'));
		}
	}

	// --- add / edit modal ---
	let modalOpen = $state(false);
	let editTarget = $state<Binding | null>(null);
	let addCategory = $state<BindingKind>('sensor');

	function openAdd(category: BindingKind = 'sensor') {
		addCategory = category;
		editTarget = null;
		modalOpen = true;
	}
	function openEdit(b: Binding) {
		editTarget = b;
		modalOpen = true;
	}
	function installProduct(product: CatalogProduct) {
		modalOpen = false;
		goto(`/env/${id}/devices/install/${product.id}`);
	}

	async function remove(b: Binding) {
		if (!confirm(`Remove "${b.name}"?`)) return;
		try {
			await deleteBinding(b.id);
			flash('ok', 'Device removed');
			reload();
		} catch (e) {
			flash('err', errMsg(e, 'Delete failed'));
		}
	}

	async function removeDevice(device: DeviceGroup) {
		if (!confirm(`Remove "${device.name}" and all its capabilities?`)) return;
		try {
			for (const b of device.bindings) await deleteBinding(b.id);
			flash('ok', 'Device removed');
			reload();
		} catch (e) {
			flash('err', errMsg(e, 'Delete failed'));
		}
	}
</script>

{#if !embedded}
	<a href="/env/{id}" class="mb-4 inline-flex items-center gap-1 text-sm text-rig-400 hover:text-rig-100">
		<ArrowLeft size={15} /> Back to {env?.name ?? 'environment'}
	</a>
{/if}

{#if loading}
	<p class="text-rig-400">Loading…</p>
{:else if error}
	<p class="text-danger">{error}</p>
{:else if !env}
	<p class="text-rig-400">Environment not found. <a href="/" class="text-leaf hover:underline">Go back</a></p>
{:else}
	<div class="space-y-5">
		<div class="flex items-center justify-between" id="devices">
			<div>
				{#if embedded}<h2 class="text-xl font-semibold">Devices</h2>{:else}<h1 class="text-2xl font-semibold">Devices</h1>{/if}
				<p class="text-sm text-rig-400">{env.name} · {devices.length} physical device{devices.length === 1 ? '' : 's'} · {myBindings.length} capabilities</p>
			</div>
			<Button onclick={() => openAdd()}><Plus size={16} /> Add device</Button>
		</div>

		{#if notice}
			<div class="rounded-lg px-4 py-2 text-sm {notice.kind === 'ok' ? 'bg-leaf/15 text-leaf' : 'bg-danger/15 text-danger'}">
				{notice.text}
			</div>
		{/if}

		{#if myBindings.length === 0}
			<div class="rounded-xl border border-dashed border-rig-800 p-10 text-center">
				<p class="mb-4 text-sm text-rig-400">No devices yet.</p>
				<Button onclick={() => openAdd()}><Plus size={16} /> Add your first device</Button>
			</div>
		{:else}
			<div class="space-y-7">
			{#each sections as section (section.label)}
				<section>
					<div class="mb-2 flex items-center gap-2"><h2 class="text-sm font-semibold text-rig-200">{section.label}</h2><span class="text-xs text-rig-500">{section.future ? 'Coming later' : section.devices.length}</span></div>
					<div class="grid gap-3 xl:grid-cols-2">
					{#if section.devices.length === 0 && !section.future}
						<div class="flex items-center justify-between gap-4 rounded-xl border border-dashed border-rig-800 bg-rig-900/20 px-4 py-4 xl:col-span-2">
							<div>
								<div class="text-sm font-medium text-rig-300">No {section.label.toLowerCase()} added</div>
								<div class="mt-0.5 text-xs text-rig-500">Add a device from the catalogue.</div>
							</div>
							<button onclick={() => openAdd(section.kind)} class="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-sm text-rig-300 transition-colors hover:border-rig-500 hover:bg-rig-800 hover:text-rig-100">
								<Plus size={14} /> Add {section.label}
							</button>
						</div>
					{/if}
					{#each section.devices as device (device.id)}
					<div class="rounded-xl border border-rig-800 bg-rig-900/40">
						<div class="flex items-center gap-3 border-b border-rig-800 px-4 py-3">
							<KindIcon kind={device.bindings[0].kind} size={20} class="text-rig-400" />
							<div class="min-w-0 flex-1"><div class="truncate text-sm font-semibold">{device.name}</div><div class="text-xs text-rig-500">{device.bindings.length} {device.bindings.length === 1 ? 'capability' : 'capabilities'}</div></div>
							<button onclick={() => removeDevice(device)} class="rounded-md p-1.5 text-rig-500 hover:bg-rig-800 hover:text-danger" title="Remove device"><Trash2 size={15} /></button>
						</div>
						{#each device.bindings as b (b.id)}
						{@const st = status(b)}
						<div class="flex items-center gap-3 border-b border-rig-800/60 px-4 py-2.5 last:border-0">
							<div class="min-w-0 flex-1"><div class="text-xs font-medium capitalize text-rig-300">{meta(b)}</div></div>
							{#if b.kind === 'camera' && b.cameraType === 'rtsp'}<CameraStreamStats cameraId={b.id} class="text-xs font-semibold text-rig-300" />{:else}<div class="text-sm font-semibold tabular-nums">{st.value || '—'}</div>{/if}
							{#if b.kind !== 'camera' || b.cameraType !== 'rtsp'}<span class="h-2 w-2 rounded-full {st.online ? 'bg-leaf' : st.online === false ? 'bg-danger' : 'bg-rig-700'}" title={st.online ? 'Online' : st.online === false ? 'Offline' : 'Unknown'}></span>{/if}
							<div class="flex items-center gap-1">
							{#if b.kind === 'light'}
								{#if b.primary}
									<span class="flex items-center gap-1 rounded-md px-1.5 py-1 text-xs text-warn" title="Primary grow light">
										<Star size={15} fill="currentColor" /> Primary
									</span>
								{:else}
									<button
										onclick={() => makePrimary(b)}
										class="rounded-md p-1.5 text-rig-500 transition-colors hover:bg-rig-800 hover:text-warn"
										title="Make primary grow light"
										aria-label="Make {b.name} the primary light"
									>
										<Star size={15} />
									</button>
								{/if}
							{/if}
							<button
								onclick={() => openEdit(b)}
								class="rounded-md p-1.5 text-rig-400 transition-colors hover:bg-rig-800 hover:text-rig-100"
								title="Edit"
								aria-label="Edit {b.name}"
							>
								<Pencil size={15} />
							</button>
						</div>
					</div>
						{/each}
					</div>
				{/each}
					</div>
				</section>
			{/each}
			</div>
		{/if}
	</div>

	<DeviceModal
		bind:open={modalOpen}
		environmentId={id!}
		{catalog}
		{discovered}
		{usedEntities}
		bindings={myBindings}
		binding={editTarget}
		onSaved={reload}
		{flash}
		onInstall={installProduct}
		initialCategory={addCategory}
	/>
{/if}
