<script lang="ts">
	import type { GrowAnalytics, GrowDetail, GrowPhoto } from '$lib/types';
	import { growPhotoImageURL } from '$lib/api';
	import { titleCase } from '$lib/format';
	import { fmtDate } from '$lib/datetime';
	import { careVisual } from '$lib/care';

	interface Props {
		grow: GrowDetail;
		analytics: GrowAnalytics | null;
		photos: GrowPhoto[];
	}
	let { grow, analytics, photos }: Props = $props();

	const maxWeekMl = $derived(Math.max(1, ...((analytics?.careByWeek ?? []).map((w) => w.totalMl))));
	const maxStageDays = $derived(Math.max(1, ...((analytics?.stageDurations ?? []).map((s) => s.days))));
	const careFreq = $derived(Object.entries(analytics?.careFrequency ?? {}).sort((a, b) => b[1] - a[1]));
	// Photos oldest → newest for a progression strip.
	const progression = $derived([...photos].reverse());
</script>

{#if !analytics}
	<p class="text-sm text-rig-500">Loading analytics…</p>
{:else}
	<div class="space-y-8">
		{#if analytics.pctInTarget != null}
			<section class="grid gap-4 sm:grid-cols-3">
				<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-4">
					<div class="text-xs uppercase tracking-wide text-rig-500">Climate in target</div>
					<div class="mt-1 text-3xl font-semibold tabular-nums text-leaf">{analytics.pctInTarget.toFixed(0)}<span class="text-base text-rig-500">%</span></div>
					<div class="text-xs text-rig-500">{analytics.sampleCount} samples this grow</div>
				</div>
				<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-4">
					<div class="text-xs uppercase tracking-wide text-rig-500">Total days</div>
					<div class="mt-1 text-3xl font-semibold tabular-nums">{grow.totalDays}</div>
				</div>
				<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-4">
					<div class="text-xs uppercase tracking-wide text-rig-500">Care events</div>
					<div class="mt-1 text-3xl font-semibold tabular-nums">{careFreq.reduce((n, [, c]) => n + c, 0)}</div>
				</div>
			</section>
		{/if}

		<!-- Stage durations -->
		<section>
			<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">Stage durations</h2>
			{#if analytics.stageDurations.length}
				<div class="space-y-2">
					{#each analytics.stageDurations as sd (sd.stage + sd.from)}
						<div class="flex items-center gap-3">
							<span class="w-28 shrink-0 truncate text-sm capitalize text-rig-300">{titleCase(sd.stage)}</span>
							<div class="h-4 flex-1 overflow-hidden rounded bg-rig-800">
								<div class="h-full rounded bg-leaf/60" style="width:{(sd.days / maxStageDays) * 100}%"></div>
							</div>
							<span class="w-16 shrink-0 text-right text-xs tabular-nums text-rig-400">{sd.days.toFixed(1)}d</span>
						</div>
					{/each}
				</div>
			{:else}
				<p class="text-sm text-rig-500">No stage history yet.</p>
			{/if}
		</section>

		<!-- Water / nutrient usage by week -->
		<section>
			<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">Water &amp; feeding by week</h2>
			{#if analytics.careByWeek.length}
				<div class="flex items-end gap-2 overflow-x-auto rounded-xl border border-rig-800 bg-rig-950/40 p-4" style="height:160px">
					{#each analytics.careByWeek as wk (wk.weekStart)}
						<div class="flex min-w-10 flex-1 flex-col items-center justify-end gap-1">
							<div class="w-full rounded-t bg-sky-500/60" style="height:{(wk.totalMl / maxWeekMl) * 110}px" title="{wk.totalMl} ml · {wk.count} events · {wk.feedCount} feeds"></div>
							<span class="text-[10px] tabular-nums text-rig-500">{new Date(wk.weekStart).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}</span>
						</div>
					{/each}
				</div>
			{:else}
				<p class="text-sm text-rig-500">No care logged yet.</p>
			{/if}
		</section>

		<!-- Care frequency -->
		{#if careFreq.length}
			<section>
				<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">Care frequency</h2>
				<div class="flex flex-wrap gap-2">
					{#each careFreq as [type, count] (type)}
						{@const v = careVisual(type)}
						<span class="inline-flex items-center gap-1.5 rounded-full border border-rig-700 px-3 py-1 text-xs text-rig-300"><v.icon size={13} /> {v.label} · {count}</span>
					{/each}
				</div>
			</section>
		{/if}

		<!-- Placement history -->
		{#if analytics.placements.length}
			<section>
				<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">Placement history</h2>
				<div class="space-y-2">
					{#each analytics.placements as pl (pl.environmentId + pl.from)}
						<div class="flex items-center justify-between rounded-lg border border-rig-800 bg-rig-950/40 px-4 py-2.5 text-sm">
							<a href="/env/{pl.environmentId}" class="hover:underline">{pl.environmentName || pl.environmentId}</a>
							<span class="text-xs text-rig-500">{fmtDate(pl.from)} → {pl.to ? fmtDate(pl.to) : 'now'}</span>
						</div>
					{/each}
				</div>
			</section>
		{/if}

		<!-- Photo progression -->
		{#if progression.length}
			<section>
				<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">Photo progression</h2>
				<div class="flex gap-2 overflow-x-auto pb-1">
					{#each progression as ph (ph.id)}
						<a href={growPhotoImageURL(grow.id, ph.id)} target="_blank" rel="noopener" class="shrink-0">
							<img src={growPhotoImageURL(grow.id, ph.id)} alt={ph.caption || 'Grow photo'} class="h-28 w-28 rounded-lg border border-rig-800 object-cover" />
							<div class="mt-1 text-center text-[10px] text-rig-500">{new Date(ph.takenAt).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}</div>
						</a>
					{/each}
				</div>
			</section>
		{/if}
	</div>
{/if}
