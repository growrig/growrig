<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { toast } from '$lib/toast.svelte';
	import { getEnvironments, getBindings, getLocations, deleteBinding, deleteEnvironment, getEnvironmentYAML, updateEnvironmentYAML } from '$lib/api';
	import type { Binding, Environment, Location } from '$lib/types';
	import EnvironmentCard from '$lib/components/EnvironmentCard.svelte';
	import StorageSection from './StorageSection.svelte';
	import Code2 from '@lucide/svelte/icons/code-2';
	import { Button, Dialog } from '$lib/components/ui';

	interface Props {
		id: string;
	}
	let { id }: Props = $props();

	let environments = $state<Environment[]>([]);
	let bindings = $state<Binding[]>([]);
	let locations = $state<Location[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let removing = $state(false);
	let yamlOpen = $state(false);
	let yamlText = $state('');
	let yamlBusy = $state(false);
	let yamlError = $state('');

	let notice = $state<{ kind: 'ok' | 'err'; text: string } | null>(null);
	function flash(kind: 'ok' | 'err', text: string) {
		notice = { kind, text };
		setTimeout(() => (notice = null), 2500);
	}

	async function reload() {
		try {
			[environments, bindings, locations] = await Promise.all([getEnvironments(), getBindings(), getLocations()]);
			error = null;
		} catch (e) {
			error = errMsg(e, 'Failed to reach Grow Core');
		} finally {
			loading = false;
		}
	}
	onMount(reload);

	const env = $derived(environments.find((e) => e.id === id));
	const rooms = $derived(environments.filter((e) => e.kind === 'room'));
	const myBindings = $derived(bindings.filter((b) => b.environmentId === id));

	async function removeEnvironment() {
		if (!env) return;
		const label = env.kind === 'tent' ? 'grow box' : 'room';
		if (!confirm(`Remove ${label} "${env.name}" and all its devices? This cannot be undone.`)) return;
		const removed = env.name;
		removing = true;
		try {
			// Cascade: delete this environment's bindings, then the environment.
			for (const b of myBindings) await deleteBinding(b.id);
			await deleteEnvironment(id);
			toast.success(`${label[0].toUpperCase()}${label.slice(1)} removed`, { description: removed });
			await goto('/');
		} catch (e) {
			toast.error('Remove failed', { description: errMsg(e, 'Remove failed') });
			error = errMsg(e, 'Remove failed');
			removing = false;
		}
	}

	async function openYAML() {
		yamlBusy = true;
		yamlError = '';
		try {
			yamlText = await getEnvironmentYAML(id);
			yamlOpen = true;
		} catch (e) {
			yamlError = errMsg(e, 'Could not load YAML');
		} finally {
			yamlBusy = false;
		}
	}

	async function saveYAML() {
		yamlBusy = true;
		yamlError = '';
		try {
			await updateEnvironmentYAML(id, yamlText);
			yamlOpen = false;
			window.location.reload();
		} catch (e) {
			yamlError = errMsg(e, 'Could not save YAML');
		} finally {
			yamlBusy = false;
		}
	}
</script>

{#if loading}
	<p class="text-rig-400">Loading…</p>
{:else if error}
	<p class="text-danger">{error}</p>
{:else if !env}
	<p class="text-rig-400">Environment not found.</p>
{:else}
	<div class="space-y-8">
		{#if notice}
			<div class="rounded-lg px-4 py-2 text-sm {notice.kind === 'ok' ? 'bg-leaf/15 text-leaf' : 'bg-danger/15 text-danger'}">
				{notice.text}
			</div>
		{/if}

		<!-- Environment settings -->
		<EnvironmentCard {env} {rooms} {locations} onChanged={reload} {flash} />

		<!-- Recorded data bound to this environment -->
		<StorageSection {id} />

		<!-- Danger zone -->
		<section class="space-y-3">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-danger/80">Danger zone</h2>
			<div class="flex items-center justify-between rounded-xl border border-danger/30 bg-danger/5 p-4">
				<div>
					<div class="text-sm font-medium">Remove this {env.kind === 'tent' ? 'grow box' : 'room'}</div>
					<div class="text-xs text-rig-500">Deletes the environment and all {myBindings.length} of its devices.</div>
				</div>
				<button
					onclick={removeEnvironment}
					disabled={removing}
					class="rounded-md bg-danger/90 px-4 py-1.5 text-sm font-medium text-white transition-colors hover:bg-danger disabled:opacity-50"
				>
					{removing ? 'Removing…' : env.kind === 'tent' ? 'Remove tent' : 'Remove room'}
				</button>
			</div>
		</section>

		<section class="space-y-3">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Configuration file</h2>
			<div class="flex items-center justify-between gap-4 rounded-xl border border-rig-800 bg-rig-900/40 p-4">
				<div>
					<div class="text-sm font-medium">Environment YAML</div>
					<div class="text-xs text-rig-500">Edit this environment and all of its device configuration directly.</div>
				</div>
				<Button variant="secondary" onclick={openYAML} disabled={yamlBusy}><Code2 size={15} /> Edit YAML</Button>
			</div>
			{#if yamlError && !yamlOpen}<p class="text-sm text-danger">{yamlError}</p>{/if}
		</section>
	</div>

	<Dialog bind:open={yamlOpen} title="Edit environment YAML" description="Changes are validated and applied immediately. Keep the environment id unchanged.">
		<div class="space-y-3">
			<textarea bind:value={yamlText} rows="24" spellcheck="false" class="w-full resize-y rounded-md border border-rig-700 bg-rig-950 p-3 font-mono text-xs leading-5 text-rig-200 focus:border-leaf focus:outline-none"></textarea>
			{#if yamlError}<p class="text-sm text-danger">{yamlError}</p>{/if}
			<div class="flex justify-end gap-2">
				<Button variant="ghost" onclick={() => (yamlOpen = false)} disabled={yamlBusy}>Cancel</Button>
				<Button onclick={saveYAML} disabled={yamlBusy || !yamlText.trim()}>{yamlBusy ? 'Saving…' : 'Save YAML'}</Button>
			</div>
		</div>
	</Dialog>
{/if}
