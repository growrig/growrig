<script lang="ts">
	import { onMount } from 'svelte';
	import { getActivity, getEnvironments } from '$lib/api';
	import type { Activity, Environment } from '$lib/types';
	import SlidersHorizontal from '@lucide/svelte/icons/sliders-horizontal';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import CircleAlert from '@lucide/svelte/icons/circle-alert';
	import Info from '@lucide/svelte/icons/info';
	import { fmtDateTime } from '$lib/datetime';

	interface Props { environmentId?: string; limit?: number; showEnvironment?: boolean }
	let { environmentId, limit = 20, showEnvironment = false }: Props = $props();
	let events = $state<Activity[]>([]);
	let environments = $state<Environment[]>([]);
	let loading = $state(true);

	async function reload() {
		try {
			[events, environments] = await Promise.all([getActivity(environmentId, limit), showEnvironment ? getEnvironments() : Promise.resolve([])]);
		} finally { loading = false; }
	}
	onMount(() => { reload(); const timer = setInterval(reload, 5000); return () => clearInterval(timer); });
	const envName = (id?: string) => environments.find((env) => env.id === id)?.name ?? id ?? 'System';
	const time = (value: string) => fmtDateTime(value);
</script>

{#if loading}
	<p class="text-sm text-rig-500">Loading activity…</p>
{:else if events.length === 0}
	<div class="rounded-xl border border-dashed border-rig-800 p-6 text-center text-sm text-rig-500">No activity recorded yet.</div>
{:else}
	<div class="overflow-hidden rounded-xl border border-rig-800 bg-rig-900/30">
		{#each events as event, i (event.id)}
			<div class="flex gap-3 px-4 py-3 {i ? 'border-t border-rig-800' : ''}">
				<div class="mt-0.5 {event.level === 'error' ? 'text-danger' : event.level === 'warning' ? 'text-warn' : event.type === 'control' ? 'text-leaf' : 'text-rig-400'}">
					{#if event.level === 'error'}<CircleAlert size={17} />{:else if event.level === 'warning'}<TriangleAlert size={17} />{:else if event.type === 'control'}<SlidersHorizontal size={17} />{:else}<Info size={17} />{/if}
				</div>
				<div class="min-w-0 flex-1">
					<div class="text-sm text-rig-200">{event.message}</div>
					<div class="mt-0.5 flex flex-wrap gap-x-2 text-xs text-rig-500">{#if showEnvironment}<span>{envName(event.environmentId)}</span>{/if}<span>{time(event.time)}</span><span class="capitalize">{event.type}</span></div>
				</div>
			</div>
		{/each}
	</div>
{/if}
