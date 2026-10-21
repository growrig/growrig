<script lang="ts">
	import { onMount } from 'svelte';
	import { getEnvironmentStorage, type CameraUsage, type EnvironmentStorage } from '$lib/api';
	import { errMsg } from '$lib/errors';
	import { fmtBytes } from '$lib/format';
	import { fmtDate } from '$lib/datetime';
	import { daysSince } from '$lib/format';
	import HardDrive from '@lucide/svelte/icons/hard-drive';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';

	interface Props { id: string }
	let { id }: Props = $props();

	let storage = $state<EnvironmentStorage | null>(null);
	let loading = $state(true);
	let error = $state('');

	onMount(async () => {
		try {
			storage = await getEnvironmentStorage(id);
		} catch (e) {
			error = errMsg(e, 'Could not read storage usage');
		} finally {
			loading = false;
		}
	});

	/** Percentage of the size limit in use; null when the limit is unlimited. */
	function usedPct(camera: CameraUsage): number | null {
		if (camera.limitBytes <= 0) return null;
		return Math.min(100, (camera.bytes / camera.limitBytes) * 100);
	}
	function span(camera: CameraUsage): string {
		if (!camera.oldest || !camera.newest) return 'no snapshots yet';
		const days = daysSince(camera.oldest) + 1;
		return `${fmtDate(camera.oldest)} – ${fmtDate(camera.newest)} · ${days} day${days === 1 ? '' : 's'}`;
	}
</script>

<section class="space-y-3">
	<div class="flex items-baseline justify-between">
		<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Recorded data</h2>
		{#if storage}<span class="text-xs tabular-nums text-rig-500">{fmtBytes(storage.totalBytes)} total</span>{/if}
	</div>

	{#if loading}
		<p class="text-sm text-rig-500">Measuring…</p>
	{:else if error}
		<p class="text-sm text-danger">{error}</p>
	{:else if !storage?.cameras.length}
		<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-4 text-sm text-rig-500">
			No recording cameras in this environment yet.
		</div>
	{:else}
		<div class="space-y-3">
			{#each storage.cameras as camera (camera.bindingId)}
				{@const pct = usedPct(camera)}
				{@const over = camera.overStorage || camera.overAge}
				<div class="rounded-xl border p-4 {over ? 'border-warn/40 bg-warn/5' : 'border-rig-800 bg-rig-900/40'}">
					<div class="flex items-start justify-between gap-4">
						<div class="min-w-0">
							<div class="flex items-center gap-2 text-sm font-medium">
								<HardDrive size={15} class="shrink-0 text-rig-500" />
								<span class="truncate">{camera.name}</span>
							</div>
							<div class="mt-0.5 text-xs text-rig-500">{camera.snapshots.toLocaleString()} snapshots · {span(camera)}</div>
						</div>
						<div class="shrink-0 text-right">
							<div class="text-sm font-semibold tabular-nums">{fmtBytes(camera.bytes)}</div>
							<div class="text-[11px] text-rig-500">
								{camera.limitBytes > 0 ? `of ${fmtBytes(camera.limitBytes)}` : 'no size limit'}
							</div>
						</div>
					</div>

					{#if pct !== null}
						<div class="mt-3 h-1.5 w-full overflow-hidden rounded-full bg-rig-800">
							<div class="h-full rounded-full {camera.overStorage ? 'bg-warn' : 'bg-leaf'}" style="width:{pct}%"></div>
						</div>
					{/if}

					<div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-rig-500">
						<span>Age limit: {camera.limitDays > 0 ? `${camera.limitDays} days` : 'unlimited'}</span>
						<span>
							Past a limit: {camera.policy === 'delete' ? 'oldest snapshots are deleted' : 'kept, alert only'}
						</span>
					</div>

					{#if camera.overStorage || camera.overAge}
						<div class="mt-3 flex items-start gap-2 rounded-lg bg-warn/10 px-3 py-2 text-xs text-warn">
							<TriangleAlert size={14} class="mt-0.5 shrink-0" />
							<span>
								{#if camera.overStorage && camera.overAge}
									Over both the size and age limit.
								{:else if camera.overStorage}
									Over the {fmtBytes(camera.limitBytes)} size limit.
								{:else}
									Holds footage older than the {camera.limitDays}-day limit.
								{/if}
								{camera.policy === 'delete' ? ' The oldest snapshots are being removed.' : ' Nothing has been deleted.'}
							</span>
						</div>
					{/if}
				</div>
			{/each}
		</div>
		<p class="text-xs text-rig-500">Limits are set per camera under Equipment. Recordings are only deleted by cameras set to the deletion policy.</p>
	{/if}
</section>
