<script lang="ts">
	import type { Binding, Role } from '$lib/types';
	import { updateBinding, deleteBinding } from '$lib/api';
	import { measurementLabel } from '$lib/format';
	import { bindingKindIcon, fallbackIcon } from '$lib/icons';
	import X from '@lucide/svelte/icons/x';
	import { Select } from '$lib/components/ui';

	interface Props {
		binding: Binding;
		onChanged: () => void;
		flash: (kind: 'ok' | 'err', text: string) => void;
	}
	let { binding, onChanged, flash }: Props = $props();

	const roles: Role[] = ['unassigned', 'exhaust', 'intake', 'circulation'];
	const Icon = $derived(bindingKindIcon[binding.kind] ?? fallbackIcon);

	async function changeRole(role: Role) {
		try {
			await updateBinding(binding.id, {
				deviceId: binding.deviceId,
				deviceName: binding.deviceName,
				controllerChannelId: binding.controllerChannelId,
				environmentId: binding.environmentId,
				kind: binding.kind,
				name: binding.name,
				entity: binding.entity,
				role,
				rpmEntity: binding.rpmEntity,
				sizeMm: binding.sizeMm,
				maxRpm: binding.maxRpm,
				airflowCfm: binding.airflowCfm,
				staticPressureMmH2O: binding.staticPressureMmH2O,
				startingVoltage: binding.startingVoltage
			});
			flash('ok', 'Role updated');
			onChanged();
		} catch (e) {
			flash('err', e instanceof Error ? e.message : 'Update failed');
		}
	}

	async function remove() {
		try {
			await deleteBinding(binding.id);
			flash('ok', 'Removed');
			onChanged();
		} catch (e) {
			flash('err', e instanceof Error ? e.message : 'Delete failed');
		}
	}
</script>

<div class="flex items-center gap-3 rounded-lg bg-rig-950/40 px-3 py-2">
	<Icon size={18} class="shrink-0 text-rig-400" />
	<div class="min-w-0 flex-1">
		<div class="truncate text-sm font-medium">{binding.name}</div>
		<div class="truncate text-xs text-rig-500">{binding.entity}</div>
	</div>

	{#if binding.kind === 'sensor'}
		<span class="rounded-full bg-rig-800 px-2 py-0.5 text-xs text-rig-300">
			{binding.measurement ? measurementLabel[binding.measurement] : 'sensor'}
		</span>
	{:else if binding.kind === 'fan'}
		<Select value={binding.role ?? 'unassigned'} onValueChange={(value) => changeRole(value as Role)} items={roles.map((role) => ({ value: role, label: role[0].toUpperCase() + role.slice(1) }))} class="h-8 w-36" />
	{:else}
		<span class="rounded-full bg-rig-800 px-2 py-0.5 text-xs capitalize text-rig-300">{binding.kind}</span>
	{/if}

	<button onclick={remove} class="p-1 text-rig-500 hover:text-danger" title="Remove" aria-label="Remove">
		<X size={16} />
	</button>
</div>
