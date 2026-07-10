<script lang="ts">
	import { onMount } from 'svelte';
	import { getEnvironments, getDevices, getInfo, createEnvironment } from '$lib/api';
	import type { Device, Environment } from '$lib/types';
	import EnvironmentCard from '$lib/components/EnvironmentCard.svelte';
	import DeviceEditor from '$lib/components/DeviceEditor.svelte';

	let environments = $state<Environment[]>([]);
	let devices = $state<Device[]>([]);
	let adapter = $state('simulator');
	let loading = $state(true);
	let error = $state<string | null>(null);

	let notice = $state<{ kind: 'ok' | 'err'; text: string } | null>(null);
	function flash(kind: 'ok' | 'err', text: string) {
		notice = { kind, text };
		setTimeout(() => (notice = null), 2500);
	}

	async function reload() {
		try {
			const [envs, devs, info] = await Promise.all([getEnvironments(), getDevices(), getInfo()]);
			environments = envs;
			devices = devs;
			adapter = info.adapter;
			error = null;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to reach Grow Core';
		} finally {
			loading = false;
		}
	}

	onMount(reload);

	let newEnvName = $state('');
	async function addEnvironment() {
		if (!newEnvName.trim()) return;
		try {
			await createEnvironment({
				name: newEnvName,
				targetTempC: 24,
				targetHumidity: 55,
				emergencyTempC: 35
			});
			newEnvName = '';
			flash('ok', 'Environment added');
			reload();
		} catch (e) {
			flash('err', e instanceof Error ? e.message : 'Failed to add');
		}
	}

	const devicesFor = (envId: string) => devices.filter((d) => d.environmentId === envId);
</script>

<div class="space-y-8">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-xl font-semibold">Setup</h1>
			<p class="text-sm text-rig-400">
				Manage environments, devices, and channel roles.
				{#if adapter === 'homeassistant'}
					Map each device to its Home Assistant entities.
				{/if}
			</p>
		</div>
		<span class="rounded-full bg-rig-800 px-3 py-1 text-xs text-rig-300">adapter: {adapter}</span>
	</div>

	{#if notice}
		<div
			class="rounded-lg px-4 py-2 text-sm {notice.kind === 'ok'
				? 'bg-leaf/15 text-leaf'
				: 'bg-danger/15 text-danger'}"
		>
			{notice.text}
		</div>
	{/if}

	{#if loading}
		<p class="text-rig-400">Loading…</p>
	{:else if error}
		<p class="text-danger">{error}</p>
	{:else}
		<!-- Environments -->
		<section class="space-y-4">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Environments</h2>
			{#each environments as env (env.id)}
				<EnvironmentCard
					{env}
					canDelete={devicesFor(env.id).length === 0}
					onChanged={reload}
					{flash}
				/>
			{/each}
			<div class="flex gap-2">
				<input
					bind:value={newEnvName}
					placeholder="New environment name"
					class="flex-1 rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-rig-500 focus:outline-none"
				/>
				<button
					onclick={addEnvironment}
					class="rounded-md border border-rig-700 px-4 py-1.5 text-sm text-rig-200 transition-colors hover:border-rig-500"
				>
					Add environment
				</button>
			</div>
		</section>

		<!-- Devices -->
		<section class="space-y-4">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Devices</h2>
			{#each devices as device (device.id)}
				<DeviceEditor {device} {environments} {adapter} onChanged={reload} {flash} />
			{/each}

			<details class="rounded-lg border border-dashed border-rig-800 bg-rig-900/20">
				<summary class="cursor-pointer px-4 py-3 text-sm text-rig-300">+ Add device</summary>
				<div class="p-4 pt-0">
					<DeviceEditor device={null} {environments} {adapter} onChanged={reload} {flash} />
				</div>
			</details>
		</section>
	{/if}
</div>
