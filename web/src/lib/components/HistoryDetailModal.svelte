<script lang="ts">
	// Everything behind one history row: for care that means the per-plant
	// breakdown the summary line can only hint at.
	import { type HistoryItem, isLink } from '$lib/history';
	import { growPhotoImageURL } from '$lib/api';
	import { fmtVolume } from '$lib/care';
	import { fmtDateTime } from '$lib/datetime';
	import { Dialog } from '$lib/components/ui';
	import { titleCase } from '$lib/format';

	interface Props {
		open?: boolean;
		item: HistoryItem | null;
		growId?: string;
	}
	let { open = $bindable(false), item, growId }: Props = $props();

	const care = $derived(item?.entry.care);
	const activity = $derived(item?.entry.activity);
	// Plain-text title: a dialog heading can't carry links.
	const title = $derived(
		(item?.title ?? []).map((s) => (isLink(s) ? s.label : s)).join('') || 'Details'
	);

	/** Solution readings, shown as a small facts grid when present. */
	const facts = $derived.by(() => {
		const out: { label: string; value: string }[] = [];
		if (!care) return out;
		const ml = (care.applications ?? []).reduce((n, a) => n + (a.amountMl ?? 0), 0);
		if (ml) out.push({ label: 'Total', value: fmtVolume(ml) });
		if (care.recipeName) out.push({ label: 'Recipe', value: care.recipeName });
		if (care.dosesLabel) out.push({ label: 'Doses', value: `${care.dosesLabel} per plant` });
		if (care.ph) out.push({ label: 'pH', value: String(care.ph) });
		if (care.ec) out.push({ label: 'EC', value: String(care.ec) });
		if (care.runoffMl) out.push({ label: 'Runoff', value: fmtVolume(care.runoffMl) });
		if (care.runoffPh) out.push({ label: 'Runoff pH', value: String(care.runoffPh) });
		if (care.source === 'automation') out.push({ label: 'Source', value: 'Automation' });
		return out;
	});
</script>

<Dialog bind:open {title} size="lg" description={item ? fmtDateTime(new Date(item.t)) : ''}>
	{#if item}
		<div class="space-y-4">
			{#if item.photoId && growId}
				<img
					src={growPhotoImageURL(growId, item.photoId)}
					alt={item.note ?? ''}
					class="max-h-80 w-full rounded-lg border border-rig-800 object-contain"
				/>
			{/if}

			{#if facts.length}
				<dl class="grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-3">
					{#each facts as f (f.label)}
						<div>
							<dt class="text-[11px] uppercase tracking-wide text-rig-500">{f.label}</dt>
							<dd class="text-sm text-rig-100">{f.value}</dd>
						</div>
					{/each}
				</dl>
			{/if}

			{#if care && (care.applications ?? []).length}
				<div>
					<h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-rig-400">
						Plants ({care.applications.length})
					</h3>
					<div class="overflow-hidden rounded-lg border border-rig-800">
						<table class="w-full text-sm">
							<tbody>
								{#each care.applications as a (a.id)}
									<tr class="border-b border-rig-800/60 last:border-0">
										<td class="px-3 py-2">
											<a href="/plants/{a.plantUnitId}" class="text-rig-100 hover:underline">
												{a.plantLabel || a.plantUnitId}
											</a>
											{#if a.note}
												<div class="text-xs italic text-rig-500">{a.note}</div>
											{/if}
										</td>
										<td class="px-3 py-2 text-right tabular-nums text-rig-300">
											{a.amountMl ? fmtVolume(a.amountMl) : '—'}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
			{/if}

			{#if item.note}
				<div>
					<h3 class="mb-1 text-xs font-semibold uppercase tracking-wide text-rig-400">Note</h3>
					<p class="whitespace-pre-wrap text-sm text-rig-200">{item.note}</p>
				</div>
			{/if}

			{#if activity}
				<div class="flex flex-wrap gap-2 text-xs">
					<span class="rounded-full bg-rig-800 px-2 py-0.5 capitalize text-rig-300">
						{titleCase(activity.type)}
					</span>
					<span
						class="rounded-full px-2 py-0.5 capitalize {activity.level === 'warning'
							? 'bg-warn/15 text-warn'
							: activity.level === 'error'
								? 'bg-danger/15 text-danger'
								: 'bg-rig-800 text-rig-300'}"
					>
						{titleCase(activity.level)}
					</span>
				</div>
			{/if}

			<!-- Any links from the headline stay reachable here. -->
			{#if item.title.some(isLink)}
				<div class="flex flex-wrap gap-2 border-t border-rig-800 pt-3 text-sm">
					{#each item.title.filter(isLink) as l, i (i)}
						<a href={l.href} class="text-rig-200 hover:underline">{l.label}</a>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</Dialog>
