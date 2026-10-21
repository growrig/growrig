<script lang="ts">
	// Everything behind one history row: for care that means the per-plant
	// breakdown the summary line can only hint at.
	import { type HistoryItem, isLink } from '$lib/history';
	import { growPhotoImageURL, updateCare, deleteCare } from '$lib/api';
	import type { LogCareInput } from '$lib/types';
	import { fmtVolume } from '$lib/care';
	import { fmtDateTime } from '$lib/datetime';
	import { Button, Dialog } from '$lib/components/ui';
	import { titleCase } from '$lib/format';
	import { toast } from '$lib/toast.svelte';
	import { errMsg } from '$lib/errors';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Trash2 from '@lucide/svelte/icons/trash-2';

	interface Props {
		open?: boolean;
		item: HistoryItem | null;
		growId?: string;
		/** Set when the viewer may correct or remove a logged care entry. */
		canEdit?: boolean;
		/** Called after an edit or a delete, so the caller can refetch. */
		onChanged?: () => void;
	}
	let { open = $bindable(false), item, growId, canEdit = false, onChanged }: Props = $props();

	const care = $derived(item?.entry.care);
	const activity = $derived(item?.entry.activity);
	// Plain-text title: a dialog heading can't carry links.
	const title = $derived(
		(item?.title ?? []).map((s) => (isLink(s) ? s.label : s)).join('') || 'Details'
	);

	// --- editing -------------------------------------------------------------
	// Only care entries are editable: an activity-log line is a record of what
	// the system did, not something the grower authored.
	const editable = $derived(canEdit && !!care);
	let editing = $state(false);
	let busy = $state(false);
	let editErr = $state('');
	let occurredAt = $state('');
	let note = $state('');
	let ph = $state<number | null>(null);
	let ec = $state<number | null>(null);
	let amounts = $state<Record<string, number | null>>({});

	// Reset whenever a different entry is opened.
	$effect(() => {
		void item?.id;
		void open;
		editing = false;
		editErr = '';
	});

	function startEdit() {
		if (!care) return;
		const at = new Date(care.occurredAt);
		at.setMinutes(at.getMinutes() - at.getTimezoneOffset());
		occurredAt = at.toISOString().slice(0, 16);
		note = care.notes ?? '';
		ph = care.ph ?? null;
		ec = care.ec ?? null;
		amounts = Object.fromEntries((care.applications ?? []).map((a) => [a.plantUnitId, a.amountMl ?? null]));
		editing = true;
	}

	async function saveEdit() {
		if (!care) return;
		busy = true;
		editErr = '';
		try {
			const body: LogCareInput = {
				type: care.type,
				notes: note.trim(),
				applications: (care.applications ?? []).map((a) => ({
					plantUnitId: a.plantUnitId,
					amountMl: amounts[a.plantUnitId] ?? 0,
					note: a.note
				}))
			};
			const at = new Date(occurredAt);
			if (!isNaN(at.getTime())) body.occurredAt = at.toISOString();
			if (ph != null) body.ph = ph;
			if (ec != null) body.ec = ec;
			await updateCare(care.id, body);
			toast.success('Entry updated');
			open = false;
			onChanged?.();
		} catch (e) {
			editErr = errMsg(e, 'Could not save the change');
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (!care || !confirm('Delete this entry? It disappears from the history for good.')) return;
		busy = true;
		editErr = '';
		try {
			await deleteCare(care.id);
			toast.success('Entry deleted');
			open = false;
			onChanged?.();
		} catch (e) {
			editErr = errMsg(e, 'Could not delete the entry');
		} finally {
			busy = false;
		}
	}

	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';

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
	{#if item && editing && care}
		<!-- Correcting an entry: what was logged, not which action it was. -->
		<div class="space-y-4">
			<label class="block">
				<span class="text-xs text-rig-400">When</span>
				<input type="datetime-local" bind:value={occurredAt} class="{field} mt-1" />
			</label>

			{#if (care.applications ?? []).length}
				<div>
					<h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-rig-400">Amount per plant (ml)</h3>
					<div class="space-y-2">
						{#each care.applications as a (a.id)}
							<label class="flex items-center gap-3">
								<span class="min-w-0 flex-1 truncate text-sm text-rig-200">{a.plantLabel || a.plantUnitId}</span>
								<input type="number" min="0" step="any" bind:value={amounts[a.plantUnitId]} class="{field} w-32" />
							</label>
						{/each}
					</div>
				</div>
			{/if}

			<div class="grid gap-3 sm:grid-cols-2">
				<label class="block"><span class="text-xs text-rig-400">pH</span>
					<input type="number" min="0" step="any" bind:value={ph} class="{field} mt-1" /></label>
				<label class="block"><span class="text-xs text-rig-400">EC</span>
					<input type="number" min="0" step="any" bind:value={ec} class="{field} mt-1" /></label>
			</div>

			<label class="block">
				<span class="text-xs text-rig-400">Note</span>
				<textarea bind:value={note} rows="3" class="{field} mt-1"></textarea>
			</label>

			{#if editErr}<p class="text-sm text-danger">{editErr}</p>{/if}

			<div class="flex items-center justify-between gap-2 border-t border-rig-800 pt-4">
				<button type="button" onclick={remove} disabled={busy} class="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm text-danger transition-colors hover:bg-danger/10 disabled:opacity-50">
					<Trash2 size={15} /> Delete
				</button>
				<div class="flex gap-2">
					<Button variant="ghost" onclick={() => (editing = false)} disabled={busy}>Cancel</Button>
					<Button onclick={saveEdit} disabled={busy}>{busy ? 'Saving…' : 'Save'}</Button>
				</div>
			</div>
		</div>
	{:else if item}
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

			{#if editable}
				<div class="flex items-center justify-between gap-2 border-t border-rig-800 pt-3">
					<button type="button" onclick={remove} disabled={busy} class="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm text-danger transition-colors hover:bg-danger/10 disabled:opacity-50">
						<Trash2 size={15} /> Delete
					</button>
					<Button variant="secondary" onclick={startEdit} disabled={busy}><Pencil size={15} /> Edit</Button>
				</div>
				{#if editErr}<p class="text-sm text-danger">{editErr}</p>{/if}
			{/if}
		</div>
	{/if}
</Dialog>
