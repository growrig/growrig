<script lang="ts">
	import { goto } from '$app/navigation';
	import type { Cultivar, GrowDetail, PlantDetail } from '$lib/types';
	import { cultivarImageURL } from '$lib/api';
	import { plantDisplayName, plantNumbersById, daysSince } from '$lib/format';
	import { DropdownMenu, type DropdownItem } from '$lib/components/ui';
	import Sprout from '@lucide/svelte/icons/sprout';
	import Plus from '@lucide/svelte/icons/plus';
	import MoreHorizontal from '@lucide/svelte/icons/more-horizontal';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import LayoutGrid from '@lucide/svelte/icons/layout-grid';
	import Table from '@lucide/svelte/icons/table';
	import Droplet from '@lucide/svelte/icons/droplet';
	import Pencil from '@lucide/svelte/icons/pencil';
	import ArrowRightLeft from '@lucide/svelte/icons/arrow-right-left';

	interface Props {
		grow: GrowDetail;
		isAdmin: boolean;
		cultivars: Cultivar[];
		canLogCare: boolean;
		onAddPlant: () => void;
		onEdit: (p: PlantDetail) => void;
		onMove: (p: PlantDetail) => void;
		onHarvest: (p: PlantDetail) => void;
		onDiscard: (p: PlantDetail) => void;
		onLogCare: (plantId: string) => void;
	}
	let { grow, isAdmin, cultivars, canLogCare, onAddPlant, onEdit, onMove, onHarvest, onDiscard, onLogCare }: Props = $props();

	let view = $state<'cards' | 'table'>('cards');
	const cultivarByName = $derived(new Map(cultivars.map((c) => [c.name, c])));
	const plantNumbers = $derived(plantNumbersById(grow.plants));
	const statusTone = (s: string) =>
		s === 'active' ? 'text-leaf' : s === 'harvested' ? 'text-warn' : 'text-rig-500';

	function menuFor(p: PlantDetail): DropdownItem[] {
		const items: DropdownItem[] = [{ label: 'View plant', href: `/plants/${p.id}`, icon: ArrowRight }];
		if (isAdmin) {
			if (p.status === 'active' && canLogCare) items.push({ label: 'Log care', onSelect: () => onLogCare(p.id), icon: Droplet });
			items.push({ label: 'Edit', onSelect: () => onEdit(p), icon: Pencil });
			if (p.status === 'active') {
				items.push({ label: 'Change location', onSelect: () => onMove(p), icon: ArrowRightLeft });
				items.push({ label: 'Harvest', onSelect: () => onHarvest(p) });
				items.push({ label: 'Remove', onSelect: () => onDiscard(p) });
			}
		}
		return items;
	}
</script>

<div class="mb-3 flex items-center justify-between">
	<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Plants · {grow.plantCount} active</h2>
	<div class="flex items-center gap-2">
		<div class="flex rounded-lg border border-rig-800 p-0.5">
			<button onclick={() => (view = 'cards')} title="Cards" aria-label="Card view" class="rounded-md p-1.5 {view === 'cards' ? 'bg-rig-800 text-rig-100' : 'text-rig-500 hover:text-rig-200'}"><LayoutGrid size={15} /></button>
			<button onclick={() => (view = 'table')} title="Table" aria-label="Table view" class="rounded-md p-1.5 {view === 'table' ? 'bg-rig-800 text-rig-100' : 'text-rig-500 hover:text-rig-200'}"><Table size={15} /></button>
		</div>
		{#if isAdmin}
			<button onclick={onAddPlant} class="inline-flex items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-sm text-rig-300 transition-colors hover:border-leaf"><Plus size={14} /> Add plant</button>
		{/if}
	</div>
</div>

{#if grow.plants.length === 0}
	<div class="rounded-xl border border-dashed border-rig-800 p-6 text-center text-sm text-rig-500">
		No plants yet.{#if isAdmin} Add some to start tracking placements.{/if}
	</div>
{:else if view === 'cards'}
	<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
		{#each grow.plants as p (p.id)}
			{@const cv = cultivarByName.get(p.cultivar)}
			<div class="relative rounded-xl border border-rig-800 bg-rig-900/40 p-4 transition-colors hover:border-rig-600">
				<!-- Full-card link: a real anchor covering the card, so the whole item
				     is clickable and keyboard-accessible. Interactive bits below sit
				     above it via `relative z-10`. -->
				<a href="/plants/{p.id}" class="absolute inset-0 rounded-xl" aria-label={plantDisplayName(p, plantNumbers.get(p.id))}></a>
				<div class="flex items-start gap-3">
					<div class="h-12 w-12 shrink-0 overflow-hidden rounded-full border border-rig-700 bg-rig-950">
						{#if cv?.imageType}
							<img src={cultivarImageURL(cv.id)} alt={p.cultivar} class="h-full w-full object-cover" />
						{:else}
							<div class="flex h-full w-full items-center justify-center text-rig-600"><Sprout size={18} /></div>
						{/if}
					</div>
					<div class="min-w-0 flex-1">
						<div class="flex items-center justify-between gap-2">
							<span class="truncate font-medium">{plantDisplayName(p, plantNumbers.get(p.id))}</span>
							<div class="relative z-10">
								<DropdownMenu items={menuFor(p)} align="end" triggerClass="grid h-7 w-7 shrink-0 place-items-center rounded-md text-rig-400 outline-none hover:bg-rig-800 hover:text-rig-100">
									{#snippet trigger()}<MoreHorizontal size={16} />{/snippet}
								</DropdownMenu>
							</div>
						</div>
						<div class="mt-0.5 text-xs capitalize {statusTone(p.status)}">{p.status} · Day {daysSince(p.createdAt)}</div>
						<div class="mt-2 space-y-0.5 text-xs text-rig-400">
							{#if p.currentEnvironmentId}
								<div><a href="/env/{p.currentEnvironmentId}" class="relative z-10 hover:underline">{p.currentEnvironmentName || p.currentEnvironmentId}</a>{#if p.currentPot} · {p.currentPot.size} {p.currentPot.unit} pot{/if}</div>
							{/if}
							{#if p.tracking === 'group' && p.quantity > 1}<div class="text-rig-500">Group ×{p.quantity}</div>{/if}
						</div>
					</div>
				</div>
			</div>
		{/each}
	</div>
{:else}
	<div class="overflow-x-auto rounded-xl border border-rig-800">
		<table class="w-full min-w-[36rem] text-sm">
			<thead class="border-b border-rig-800 text-left text-xs uppercase tracking-wide text-rig-500">
				<tr>
					<th class="px-4 py-2 font-medium">Plant</th>
					<th class="px-4 py-2 font-medium">Status</th>
					<th class="px-4 py-2 font-medium">Location</th>
					<th class="px-4 py-2 font-medium">Pot</th>
					<th class="px-4 py-2 font-medium">Age</th>
					<th class="px-4 py-2 font-medium"></th>
				</tr>
			</thead>
			<tbody>
				{#each grow.plants as p (p.id)}
					<tr
						class="cursor-pointer border-b border-rig-800/60 transition-colors last:border-0 hover:bg-rig-800/40"
						onclick={() => goto(`/plants/${p.id}`)}
					>
						<td class="px-4 py-2"><a href="/plants/{p.id}" class="font-medium hover:underline" onclick={(e) => e.stopPropagation()}>{plantDisplayName(p, plantNumbers.get(p.id))}</a>{#if p.tracking === 'group' && p.quantity > 1}<span class="ml-1 text-xs text-rig-500">×{p.quantity}</span>{/if}</td>
						<td class="px-4 py-2 capitalize {statusTone(p.status)}">{p.status}</td>
						<td class="px-4 py-2 text-rig-300">{#if p.currentEnvironmentId}<a href="/env/{p.currentEnvironmentId}" class="hover:underline" onclick={(e) => e.stopPropagation()}>{p.currentEnvironmentName || p.currentEnvironmentId}</a>{:else}—{/if}</td>
						<td class="px-4 py-2 tabular-nums text-rig-300">{p.currentPot ? `${p.currentPot.size} ${p.currentPot.unit}` : '—'}</td>
						<td class="px-4 py-2 tabular-nums text-rig-400">{daysSince(p.createdAt)}d</td>
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<td class="px-4 py-2 text-right" onclick={(e) => e.stopPropagation()}>
							<DropdownMenu items={menuFor(p)} align="end" triggerClass="grid h-7 w-7 place-items-center rounded-md text-rig-400 outline-none hover:bg-rig-800 hover:text-rig-100">
								{#snippet trigger()}<MoreHorizontal size={16} />{/snippet}
							</DropdownMenu>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
