<script lang="ts">
	import { onMount } from 'svelte';
	import { getHomeAssistant, reloadHomeAssistant, updateHomeAssistant } from '$lib/api';
	import type { HAStatus, HAComponent, HAUpdateTarget } from '$lib/types';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import CircleCheck from '@lucide/svelte/icons/circle-check';
	import CircleArrowUp from '@lucide/svelte/icons/circle-arrow-up';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import Home from '@lucide/svelte/icons/house';
	import Cpu from '@lucide/svelte/icons/cpu';
	import Server from '@lucide/svelte/icons/server';
	import Puzzle from '@lucide/svelte/icons/puzzle';

	let status = $state<HAStatus | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let busy = $state<string | null>(null); // key of the in-flight action

	onMount(load);

	async function load() {
		loading = true;
		try {
			status = await getHomeAssistant();
			error = null;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load Home Assistant status';
		} finally {
			loading = false;
		}
	}

	const sup = $derived(status?.supervisor);
	const components = $derived(
		sup?.available
			? [
					{ key: 'core' as HAUpdateTarget, label: 'Home Assistant Core', icon: Home, info: sup.core },
					{ key: 'os' as HAUpdateTarget, label: 'Operating System', icon: Cpu, info: sup.os },
					{ key: 'supervisor' as HAUpdateTarget, label: 'Supervisor', icon: Server, info: sup.supervisor }
				]
			: []
	);
	const pendingCount = $derived(
		sup?.available
			? components.filter((c) => c.info.updateAvailable).length +
					(sup.addons?.filter((a) => a.updateAvailable).length ?? 0)
			: 0
	);

	const healthMeta: Record<string, { dot: string; label: string }> = {
		online: { dot: 'bg-leaf', label: 'Connected' },
		stale: { dot: 'bg-warn', label: 'Stale' },
		offline: { dot: 'bg-danger', label: 'Disconnected' }
	};

	async function checkUpdates() {
		busy = 'reload';
		error = null;
		try {
			await reloadHomeAssistant();
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to check for updates';
		} finally {
			busy = null;
		}
	}

	async function doUpdate(target: HAUpdateTarget, label: string, slug?: string) {
		if (!confirm(`Update ${label}? Home Assistant will apply this in the background and may restart.`)) return;
		busy = slug ?? target;
		error = null;
		try {
			await updateHomeAssistant(target, slug);
			// The Supervisor applies updates asynchronously; refresh shortly after.
			setTimeout(load, 1500);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to start update';
		} finally {
			busy = null;
		}
	}
</script>

{#snippet versionRow(label: string, icon: typeof Home, info: HAComponent, target: HAUpdateTarget, slug?: string)}
	{@const Icon = icon}
	<div class="flex items-center justify-between gap-3 px-4 py-3">
		<div class="flex items-center gap-3">
			<Icon size={18} class="text-rig-400" />
			<div>
				<div class="text-sm font-medium">{label}</div>
				<div class="text-xs text-rig-500 tabular-nums">
					{info.version || '—'}{#if info.updateAvailable && info.versionLatest}
						<span class="text-warn"> → {info.versionLatest}</span>{/if}
				</div>
			</div>
		</div>
		{#if info.updateAvailable}
			<button
				onclick={() => doUpdate(target, label, slug)}
				disabled={busy === (slug ?? target)}
				class="flex items-center gap-1.5 rounded-md bg-warn px-3 py-1.5 text-xs font-medium text-rig-950 transition-opacity hover:opacity-90 disabled:opacity-50"
			>
				<CircleArrowUp size={14} /> {busy === (slug ?? target) ? 'Updating…' : 'Update'}
			</button>
		{:else}
			<span class="flex items-center gap-1 text-xs text-leaf"><CircleCheck size={14} /> Up to date</span>
		{/if}
	</div>
{/snippet}

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-lg font-semibold">Home Assistant</h2>
		{#if sup?.available}
			<button
				onclick={checkUpdates}
				disabled={busy === 'reload'}
				class="flex items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-sm text-rig-300 transition-colors hover:border-rig-500 hover:text-rig-100 disabled:opacity-50"
			>
				<RefreshCw size={15} class={busy === 'reload' ? 'animate-spin' : ''} /> Check for updates
			</button>
		{/if}
	</div>

	{#if error}
		<div class="rounded-lg bg-danger/15 px-4 py-2 text-sm text-danger">{error}</div>
	{/if}

	{#if loading}
		<p class="text-sm text-rig-400">Loading…</p>
	{:else if status}
		<!-- Connection between GrowRig and Home Assistant -->
		<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-5">
			<div class="flex items-center justify-between gap-3">
				<div class="flex items-center gap-3">
					<span class="h-2.5 w-2.5 rounded-full {healthMeta[status.health]?.dot ?? 'bg-rig-600'}"></span>
					<div>
						<div class="font-medium">Device connection</div>
						<div class="text-sm text-rig-400">
							{#if status.adapter === 'homeassistant'}
								{healthMeta[status.health]?.label ?? status.health} to Home Assistant
							{:else}
								Running in simulator mode — no Home Assistant connected
							{/if}
						</div>
					</div>
				</div>
				<span class="rounded-full bg-rig-800 px-2.5 py-1 text-xs uppercase tracking-wide text-rig-300">
					{status.adapter === 'homeassistant' ? 'Home Assistant' : 'Simulator'}
				</span>
			</div>
		</div>

		{#if sup?.available}
			<!-- Update summary -->
			{#if sup.error}
				<div class="flex items-center gap-2 rounded-xl border border-warn/40 bg-warn/10 px-4 py-3 text-sm text-warn">
					<TriangleAlert size={16} /> Couldn't read some Supervisor data: {sup.error}
				</div>
			{:else if pendingCount === 0}
				<div class="flex items-center gap-2 rounded-xl border border-leaf/30 bg-leaf/10 px-4 py-3 text-sm text-leaf">
					<CircleCheck size={16} /> Everything is up to date.
				</div>
			{:else}
				<div class="flex items-center gap-2 rounded-xl border border-warn/40 bg-warn/10 px-4 py-3 text-sm text-warn">
					<CircleArrowUp size={16} />
					{pendingCount} update{pendingCount === 1 ? '' : 's'} available.
				</div>
			{/if}

			<!-- Core / OS / Supervisor -->
			<div class="overflow-hidden rounded-xl border border-rig-800 divide-y divide-rig-800">
				{#each components as c (c.key)}
					{@render versionRow(c.label, c.icon, c.info, c.key)}
				{/each}
			</div>

			<!-- Add-ons -->
			{#if sup.addons?.length}
				<div>
					<h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-rig-300">
						<Puzzle size={15} /> Add-ons
					</h3>
					<div class="overflow-hidden rounded-xl border border-rig-800 divide-y divide-rig-800">
						{#each sup.addons as a (a.slug)}
							{@render versionRow(a.name, Puzzle, a, 'addon', a.slug)}
						{/each}
					</div>
				</div>
			{/if}
		{:else}
			<div class="rounded-xl border border-dashed border-rig-800 p-5 text-sm text-rig-400">
				<p class="mb-1 font-medium text-rig-200">Appliance management unavailable</p>
				Update status and controls for Home Assistant Core, the operating system, the Supervisor
				and add-ons appear here when GrowRig runs as a Home Assistant OS add-on. It isn't currently
				running under the Supervisor.
			</div>
		{/if}
	{/if}
</div>
