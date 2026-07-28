<script lang="ts">
	import type { CareEvent, CareHistory } from '$lib/types';
	import { careVisual, fmtVolume } from '$lib/care';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';

	interface Props {
		care: CareHistory;
	}
	let { care }: Props = $props();

	// A compact "2d ago" / "5h ago" / "just now" from an ISO timestamp.
	function ago(iso: string): string {
		const ms = Date.now() - new Date(iso).getTime();
		if (ms < 60_000) return 'just now';
		const min = Math.floor(ms / 60_000);
		if (min < 60) return `${min}m ago`;
		const h = Math.floor(min / 60);
		if (h < 24) return `${h}h ago`;
		return `${Math.floor(h / 24)}d ago`;
	}

	// Secondary line for a care type: recipe, volume, pH — whatever's relevant.
	function detail(e: CareEvent): string {
		const bits: string[] = [];
		if (e.recipeName) bits.push(e.recipeName);
		if (e.dosesLabel) bits.push(`${e.dosesLabel} per plant`);
		const ml = (e.applications ?? []).reduce((s, a) => s + (a.amountMl ?? 0), 0);
		if (ml > 0) bits.push(fmtVolume(ml));
		if (e.ph) bits.push(`pH ${e.ph}`);
		return bits.join(' · ');
	}

	// The most recent action of each type, most-recent first — one row per type.
	const lastEntries = $derived(
		Object.values(care.summary.lastByType).sort(
			(a, b) => new Date(b.occurredAt).getTime() - new Date(a.occurredAt).getTime()
		)
	);
	const skipped = $derived(care.summary.skipped);
</script>

<section>
	<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-rig-400">Care</h2>

	{#if lastEntries.length === 0}
		<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-4 text-sm text-rig-500">
			No care logged yet.
		</div>
	{:else}
		<div class="overflow-hidden rounded-xl border border-rig-800 bg-rig-900/40">
			{#each lastEntries as e (e.type)}
				{@const v = careVisual(e.type)}
				<div class="flex items-center gap-3 border-b border-rig-800/60 px-4 py-2.5 last:border-0">
					<span class="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-rig-800 text-rig-300">
						<v.icon size={15} />
					</span>
					<div class="min-w-0 flex-1">
						<p class="text-sm text-rig-100">{v.label}</p>
						{#if detail(e)}<p class="truncate text-xs text-rig-500">{detail(e)}</p>{/if}
					</div>
					<span class="shrink-0 text-xs tabular-nums text-rig-500">{ago(e.occurredAt)}</span>
				</div>
			{/each}
		</div>
	{/if}

	{#if skipped.length > 0}
		<div class="mt-2 flex items-start gap-2 rounded-lg border border-warn/30 bg-warn/5 px-3 py-2 text-xs text-warn">
			<TriangleAlert size={14} class="mt-0.5 shrink-0" />
			<span>
				{skipped.length === 1 ? `${skipped[0].plantLabel} was` : `${skipped.length} plants were`}
				left out of the most recent care action.
			</span>
		</div>
	{/if}
</section>
