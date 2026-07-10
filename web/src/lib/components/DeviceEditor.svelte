<script lang="ts">
	import type { Device, Environment, Role } from '$lib/types';
	import { createDevice, updateDevice, deleteDevice, type ChannelInput } from '$lib/api';

	interface Props {
		device: Device | null; // null = create form
		environments: Environment[];
		adapter: string;
		onChanged: () => void;
		flash: (kind: 'ok' | 'err', text: string) => void;
	}
	let { device, environments, adapter, onChanged, flash }: Props = $props();

	const roles: Role[] = ['unassigned', 'exhaust', 'intake', 'circulation'];
	const showEntities = $derived(adapter === 'homeassistant');
	const isNew = $derived(device === null);

	function blankChannel(): ChannelInput {
		return { name: '', role: 'unassigned', entity: '', rpmEntity: '' };
	}

	// Editable drafts seeded from the initial prop values (intentional).
	// svelte-ignore state_referenced_locally
	let name = $state(device?.name ?? '');
	// svelte-ignore state_referenced_locally
	let environmentId = $state(device?.environmentId ?? environments[0]?.id ?? '');
	// svelte-ignore state_referenced_locally
	let tempEntity = $state(device?.tempEntity ?? '');
	// svelte-ignore state_referenced_locally
	let humidityEntity = $state(device?.humidityEntity ?? '');
	// svelte-ignore state_referenced_locally
	let channels = $state<ChannelInput[]>(
		device?.channels.map((c) => ({
			id: c.id,
			name: c.name,
			role: c.role,
			entity: c.entity,
			rpmEntity: c.rpmEntity
		})) ?? [blankChannel()]
	);
	let busy = $state(false);

	function addChannel() {
		channels = [...channels, blankChannel()];
	}
	function removeChannel(i: number) {
		channels = channels.filter((_, j) => j !== i);
	}

	function reset() {
		name = '';
		environmentId = environments[0]?.id ?? '';
		tempEntity = '';
		humidityEntity = '';
		channels = [blankChannel()];
	}

	async function save() {
		busy = true;
		const payload = { name, environmentId, tempEntity, humidityEntity, channels };
		try {
			if (device) await updateDevice(device.id, payload);
			else await createDevice(payload);
			flash('ok', device ? 'Device saved' : 'Device added');
			if (isNew) reset();
			onChanged();
		} catch (e) {
			flash('err', e instanceof Error ? e.message : 'Save failed');
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (!device || !confirm(`Delete device "${device.name}"?`)) return;
		try {
			await deleteDevice(device.id);
			flash('ok', 'Device deleted');
			onChanged();
		} catch (e) {
			flash('err', e instanceof Error ? e.message : 'Delete failed');
		}
	}

	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-2 py-1 text-sm focus:border-rig-500 focus:outline-none';
</script>

<div class="rounded-lg border border-rig-800 {isNew ? 'border-dashed' : ''} bg-rig-950/40 p-4">
	<div class="mb-3 grid gap-3 sm:grid-cols-2">
		<label class="block">
			<span class="text-xs text-rig-400">Device name</span>
			<input bind:value={name} placeholder="Grow Controller" class={field} />
		</label>
		<label class="block">
			<span class="text-xs text-rig-400">Environment</span>
			<select bind:value={environmentId} class={field}>
				{#each environments as env (env.id)}
					<option value={env.id}>{env.name}</option>
				{/each}
			</select>
		</label>
		{#if showEntities}
			<label class="block">
				<span class="text-xs text-rig-400">Temperature sensor entity</span>
				<input bind:value={tempEntity} placeholder="sensor.growbox_temperature" class={field} />
			</label>
			<label class="block">
				<span class="text-xs text-rig-400">Humidity sensor entity</span>
				<input bind:value={humidityEntity} placeholder="sensor.growbox_humidity" class={field} />
			</label>
		{/if}
	</div>

	<div class="space-y-2">
		<div class="flex items-center justify-between">
			<span class="text-xs font-medium text-rig-300">Fan channels</span>
			<button onclick={addChannel} class="text-xs text-rig-400 hover:text-rig-100">+ add channel</button>
		</div>
		{#each channels as ch, i (i)}
			<div class="grid items-center gap-2 rounded-md bg-rig-900/60 p-2 {showEntities ? 'sm:grid-cols-[1fr_auto_1fr_1fr_auto]' : 'sm:grid-cols-[1fr_auto_auto]'}">
				<input bind:value={ch.name} placeholder="Fan name" class={field} />
				<select bind:value={ch.role} class={field}>
					{#each roles as role (role)}
						<option value={role}>{role}</option>
					{/each}
				</select>
				{#if showEntities}
					<input bind:value={ch.entity} placeholder="fan.growbox_exhaust" class={field} />
					<input bind:value={ch.rpmEntity} placeholder="sensor.…_rpm (optional)" class={field} />
				{/if}
				<button
					onclick={() => removeChannel(i)}
					class="justify-self-end rounded-md px-2 py-1 text-xs text-rig-500 hover:text-danger"
					title="Remove channel"
				>
					✕
				</button>
			</div>
		{/each}
	</div>

	<div class="mt-3 flex gap-2">
		<button
			onclick={save}
			disabled={busy}
			class="rounded-md bg-rig-500 px-4 py-1.5 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-400 disabled:opacity-50"
		>
			{isNew ? 'Add device' : 'Save'}
		</button>
		{#if !isNew}
			<button
				onclick={remove}
				class="rounded-md border border-rig-700 px-4 py-1.5 text-sm text-rig-300 transition-colors hover:border-danger hover:text-danger"
			>
				Delete
			</button>
		{/if}
	</div>
</div>
