<script lang="ts">
	// The shared History view: a day-grouped account of what happened to one
	// collection, where every row opens its own detail.
	//
	// It owns the query. Filtering, hiding automation and paging are all
	// server-side (GET /api/history) because a merged stream can't be paged
	// correctly in the client: to show the newest 50 things you would have to
	// hold every source in full, since the next-oldest could come from any of
	// them. Grow, plant and environment all render through this, so their
	// histories read alike.
	import { untrack } from 'svelte';
	import { getHistory } from '$lib/api';
	import type { HistoryEntry, HistoryKind } from '$lib/types';
	import { toItems, isLink, groupByDay, type HistoryItem } from '$lib/history';
	import { growPhotoImageURL } from '$lib/api';
	import { fmtDate, fmtTime } from '$lib/datetime';
	import HistoryDetailModal from '$lib/components/HistoryDetailModal.svelte';
	import Eye from '@lucide/svelte/icons/eye';
	import EyeOff from '@lucide/svelte/icons/eye-off';

	interface Props {
		/** Exactly one scope: history is always *of* something. */
		growId?: string;
		plantId?: string;
		environmentId?: string;
		/** Grow id used to resolve photo thumbnails (a plant's photos live on it). */
		photoGrowId?: string;
		empty?: string;
		/** Let the viewer correct or delete logged care entries. */
		canEdit?: boolean;
	}
	let {
		growId,
		plantId,
		environmentId,
		photoGrowId,
		empty = 'Nothing here yet — actions, photos and milestones appear as things happen.',
		canEdit = false
	}: Props = $props();

	const PAGE = 50;

	type Filter = 'all' | HistoryKind;
	let filter = $state<Filter>('all');
	let showAutomation = $state(false);

	let entries = $state<HistoryEntry[]>([]);
	let total = $state(0);
	let loading = $state(true);
	let loadingMore = $state(false);
	let err = $state('');

	const scope = $derived({ growId, plantId, environmentId });
	const photoScope = $derived(photoGrowId ?? growId);

	async function load(offset: number) {
		const res = await getHistory({
			...scope,
			...(filter === 'all' ? {} : { kinds: [filter] }),
			automation: showAutomation,
			limit: PAGE,
			offset
		});
		total = res.total;
		entries = offset === 0 ? res.items : [...entries, ...res.items];
	}

	// Everything the query depends on, flattened to a string. Scope props arrive
	// as getters over the caller's own reactive state — `environmentId={env.id}`
	// reads a fresh `env` object on every live snapshot — so an effect that read
	// them directly would refetch on each snapshot and blank the list behind the
	// loading state. A derived string only invalidates when a value truly
	// changes.
	const queryKey = $derived(
		[growId ?? '', plantId ?? '', environmentId ?? '', filter, showAutomation].join('|')
	);

	// Refetch whenever the query changes. The load itself runs untracked so it
	// neither re-reads the scope props nor re-triggers on the state it writes.
	$effect(() => {
		queryKey;
		loading = true;
		err = '';
		untrack(() => load(0))
			.catch((e) => (err = e instanceof Error ? e.message : 'Failed to load history'))
			.finally(() => (loading = false));
	});

	async function loadMore() {
		loadingMore = true;
		try {
			await load(entries.length);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load history';
		} finally {
			loadingMore = false;
		}
	}

	const items = $derived(toItems(entries, plantId));
	const groups = $derived(groupByDay(items));
	const remaining = $derived(Math.max(0, total - entries.length));

	// The server doesn't enumerate which kinds a scope contains, so the filter
	// chips are built from what has been seen — plus whatever is selected, so a
	// filter never disappears from under the user while it is active.
	let seen = $state<Set<HistoryKind>>(new Set());
	$effect(() => {
		if (filter !== 'all') return; // a filtered page says nothing about the rest
		const next = new Set(seen);
		for (const e of entries) next.add(e.kind);
		if (next.size !== seen.size) seen = next;
	});
	const allFilters: { id: Filter; label: string }[] = [
		{ id: 'care', label: 'Care' },
		{ id: 'photo', label: 'Photos' },
		{ id: 'milestone', label: 'Milestones' },
		{ id: 'placement', label: 'Moves' },
		{ id: 'pot', label: 'Pots' },
		{ id: 'system', label: 'System' }
	];
	const filters = $derived([
		{ id: 'all' as Filter, label: 'All' },
		...allFilters.filter((f) => seen.has(f.id as HistoryKind))
	]);

	const kindTone: Record<HistoryKind, string> = {
		photo: 'text-sky-400',
		care: 'text-leaf',
		milestone: 'text-warn',
		placement: 'text-violet-400',
		pot: 'text-amber-400',
		system: 'text-rig-400'
	};

	let selected = $state<HistoryItem | null>(null);
	let detailOpen = $state(false);
	function open(item: HistoryItem) {
		selected = item;
		detailOpen = true;
	}
</script>

{#if filters.length > 1}
	<div class="mb-4 flex flex-wrap items-center gap-2">
		{#each filters as f (f.id)}
			<button
				onclick={() => (filter = f.id)}
				class="rounded-full border px-3 py-1 text-xs font-medium transition-colors {filter === f.id
					? 'border-rig-600 bg-rig-800 text-rig-100'
					: 'border-rig-800 text-rig-400 hover:border-rig-700'}"
			>
				{f.label}
			</button>
		{/each}
		<button
			onclick={() => (showAutomation = !showAutomation)}
			class="ml-auto inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors {showAutomation
				? 'border-rig-600 bg-rig-800 text-rig-100'
				: 'border-rig-800 text-rig-400 hover:border-rig-700'}"
			title="Routine control-loop actions, e.g. fan speed adjustments"
		>
			{#if showAutomation}<EyeOff size={13} />{:else}<Eye size={13} />{/if}
			{showAutomation ? 'Hide' : 'Show'} automation
		</button>
	</div>
{/if}

{#if err}
	<p class="rounded-lg bg-danger/15 px-4 py-2 text-sm text-danger">{err}</p>
{:else if loading}
	<p class="py-10 text-center text-sm text-rig-500">Loading history…</p>
{:else if items.length === 0}
	<div class="rounded-xl border border-dashed border-rig-800 p-10 text-center text-sm text-rig-500">
		{empty}
	</div>
{:else}
	<div class="space-y-6">
		{#each groups as [day, dayItems] (day)}
			<div>
				<div class="mb-2 text-xs font-semibold uppercase tracking-wide text-rig-500">
					{fmtDate(new Date(day))}
				</div>
				<div class="space-y-2">
					{#each dayItems as it (it.id)}
						<!-- The row is a button so the whole entry opens its detail; the
						     links inside stop propagation so they navigate instead. -->
						<button
							type="button"
							onclick={() => open(it)}
							class="flex w-full items-start gap-3 rounded-lg border border-rig-800 bg-rig-900/40 px-3 py-2.5 text-left transition-colors hover:border-rig-600 hover:bg-rig-900/70"
						>
							<span
								class="mt-0.5 grid h-8 w-8 shrink-0 place-items-center rounded-md bg-rig-800 {kindTone[it.kind]}"
							>
								<it.icon size={16} />
							</span>
							{#if it.photoId && photoScope}
								<img
									src={growPhotoImageURL(photoScope, it.photoId)}
									alt=""
									class="h-12 w-12 shrink-0 rounded object-cover"
								/>
							{/if}
							<div class="min-w-0 flex-1">
								<p class="text-sm text-rig-100">
									{#each it.title as seg, i (i)}
										{#if isLink(seg)}
											<a
												href={seg.href}
												onclick={(e) => e.stopPropagation()}
												class="font-medium text-rig-50 hover:underline">{seg.label}</a
											>
										{:else}{seg}{/if}
									{/each}
								</p>
								{#if it.detail}<p class="truncate text-xs text-rig-500">{it.detail}</p>{/if}
								{#if it.note}
									<!-- A note the grower wrote is the whole point of writing it, so
									     it belongs in the list, not only behind the detail modal. -->
									<p class="mt-1 border-l-2 border-rig-700 pl-2 text-xs italic text-rig-400">
										{it.note}
									</p>
								{/if}
							</div>
							<span class="shrink-0 text-xs tabular-nums text-rig-600">
								{fmtTime(new Date(it.t), { hour: '2-digit', minute: '2-digit' })}
							</span>
						</button>
					{/each}
				</div>
			</div>
		{/each}

		{#if remaining > 0}
			<button
				type="button"
				onclick={loadMore}
				disabled={loadingMore}
				class="w-full rounded-lg border border-rig-800 py-2.5 text-sm text-rig-300 transition-colors hover:border-rig-600 hover:text-rig-100 disabled:opacity-50"
			>
				{loadingMore ? 'Loading…' : `Show older — ${remaining} more`}
			</button>
		{/if}
	</div>
{/if}

<HistoryDetailModal
	bind:open={detailOpen}
	item={selected}
	growId={photoScope}
	{canEdit}
	onChanged={() => {
		// The edited entry may have moved day or vanished, so reload the page
		// of results rather than patching it in place.
		seen = new Set();
		load(0).catch(() => {});
	}}
/>
