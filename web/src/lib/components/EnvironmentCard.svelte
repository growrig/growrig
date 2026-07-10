<script lang="ts">
	import type { Environment } from '$lib/types';
	import { updateEnvironment, deleteEnvironment } from '$lib/api';

	interface Props {
		env: Environment;
		canDelete: boolean;
		onChanged: () => void;
		flash: (kind: 'ok' | 'err', text: string) => void;
	}
	let { env, canDelete, onChanged, flash }: Props = $props();

	// Editable drafts seeded from the initial prop values (intentional).
	// svelte-ignore state_referenced_locally
	let name = $state(env.name);
	// svelte-ignore state_referenced_locally
	let temp = $state(env.targetTempC);
	// svelte-ignore state_referenced_locally
	let humidity = $state(env.targetHumidity);
	// svelte-ignore state_referenced_locally
	let emergency = $state(env.emergencyTempC);
	let busy = $state(false);

	async function save() {
		busy = true;
		try {
			await updateEnvironment(env.id, {
				name,
				targetTempC: temp,
				targetHumidity: humidity,
				emergencyTempC: emergency
			});
			flash('ok', 'Environment saved');
			onChanged();
		} catch (e) {
			flash('err', e instanceof Error ? e.message : 'Save failed');
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (!confirm(`Delete environment "${env.name}"?`)) return;
		try {
			await deleteEnvironment(env.id);
			flash('ok', 'Environment deleted');
			onChanged();
		} catch (e) {
			flash('err', e instanceof Error ? e.message : 'Delete failed');
		}
	}
</script>

<section class="rounded-xl border border-rig-800 bg-rig-900/40 p-5">
	<div class="mb-4 flex items-center gap-3">
		<input
			bind:value={name}
			class="flex-1 rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-lg font-semibold focus:border-rig-500 focus:outline-none"
		/>
		<span class="text-xs text-rig-500">{env.id}</span>
	</div>

	<div class="grid gap-4 sm:grid-cols-3">
		<label class="block">
			<span class="text-sm text-rig-400">Target temp — {temp}°C</span>
			<input type="range" min="15" max="35" step="0.5" bind:value={temp} class="mt-2 w-full accent-rig-500" />
		</label>
		<label class="block">
			<span class="text-sm text-rig-400">Target humidity — {humidity}%</span>
			<input type="range" min="20" max="90" step="1" bind:value={humidity} class="mt-2 w-full accent-rig-500" />
		</label>
		<label class="block">
			<span class="text-sm text-rig-400">Emergency temp — {emergency}°C</span>
			<input type="range" min="28" max="45" step="0.5" bind:value={emergency} class="mt-2 w-full accent-warn" />
		</label>
	</div>

	<div class="mt-4 flex gap-2">
		<button
			onclick={save}
			disabled={busy}
			class="rounded-md bg-rig-500 px-4 py-1.5 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-400 disabled:opacity-50"
		>
			Save
		</button>
		{#if canDelete}
			<button
				onclick={remove}
				class="rounded-md border border-rig-700 px-4 py-1.5 text-sm text-rig-300 transition-colors hover:border-danger hover:text-danger"
			>
				Delete
			</button>
		{/if}
	</div>
</section>
