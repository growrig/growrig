<script lang="ts">
	import { onMount } from 'svelte';
	import { live } from '$lib/live.svelte';
	import { auth } from '$lib/auth.svelte';
	import { getLocations } from '$lib/api';
	import { buildEnvTree, type EnvTreeLocation } from '$lib/location';
	import { addEnv } from '$lib/addEnv.svelte';
	import { climateTone, toneClass, vpdZone } from '$lib/format';
	import type { EnvironmentView, Location } from '$lib/types';
	import { Dialog } from '$lib/components/ui';
	import NewLocationForm from '$lib/components/NewLocationForm.svelte';
	import MapPin from '@lucide/svelte/icons/map-pin';
	import Wind from '@lucide/svelte/icons/wind';
	import Tent from '@lucide/svelte/icons/tent';
	import Plus from '@lucide/svelte/icons/plus';

	let locations = $state<Location[]>([]);
	async function refreshLocations() {
		try {
			locations = await getLocations();
		} catch {
			/* ignore */
		}
	}
	onMount(refreshLocations);

	// "Add location" dialog — a sited group a room can then belong to.
	let addingLocation = $state(false);
	function onLocationSaved() {
		addingLocation = false;
		void refreshLocations();
	}

	const snap = $derived(live.snapshot);
	const environments = $derived(snap?.environments ?? []);

	// Every location as a group — including ones with no rooms yet, so a freshly
	// added location is visible and fillable. buildEnvTree omits empty locations
	// (it feeds the compact nav menu), so we merge them back in here, alphabetical,
	// with the orphan "No location" group kept last.
	const groups = $derived.by<EnvTreeLocation[]>(() => {
		const built = buildEnvTree(environments, locations);
		const byKey = new Map(built.map((g) => [g.key, g]));
		const out: EnvTreeLocation[] = [];
		for (const loc of [...locations].sort((a, b) => a.name.localeCompare(b.name))) {
			out.push(
				byKey.get(loc.id) ?? { key: loc.id, name: loc.name, located: true, rooms: [], looseBoxes: [] }
			);
		}
		const none = byKey.get('__none__');
		if (none) out.push(none);
		return out;
	});
	const hasGroups = $derived(groups.length > 0);

	const healthDot = (h: string) =>
		h === 'online' ? 'bg-leaf' : h === 'stale' ? 'bg-warn' : 'bg-danger';
	const healthLabel = (h: string) =>
		h === 'online' ? 'Online' : h === 'stale' ? 'Stale' : 'Offline';

	const fmtDims = (e: EnvironmentView) =>
		e.widthCm && e.depthCm && e.heightCm ? `${e.widthCm}×${e.depthCm}×${e.heightCm}` : '';
</script>

<!-- One environment (room or grow box) as a table row, indented by tree depth. -->
{#snippet envRow(env: EnvironmentView, depth: number)}
	<tr class="border-t border-rig-800/70 transition-colors hover:bg-rig-800/30">
		<td class="py-2.5 pr-3">
			<a
				href="/env/{env.id}"
				class="flex items-center gap-2"
				style="padding-left: {depth * 1.25}rem"
			>
				{#if env.kind === 'room'}
					<Wind size={15} class="shrink-0 text-rig-400" />
				{:else}
					<Tent size={15} class="shrink-0 text-rig-400" />
				{/if}
				<span class="truncate font-medium text-rig-100 hover:underline">{env.name}</span>
			</a>
		</td>
		<td class="px-3">
			<span class="rounded-full bg-rig-800 px-2 py-0.5 text-[10px] uppercase tracking-wide text-rig-400">
				{env.kind}
			</span>
		</td>
		<td class="px-3 text-right tabular-nums">
			{#if env.hasTemp}
				<span class={climateTone(env.tempC, env.targetTempC, env.emergencyTempC)}>{env.tempC.toFixed(1)}°</span>
			{:else}<span class="text-rig-600">—</span>{/if}
		</td>
		<td class="px-3 text-right tabular-nums">
			{#if env.hasHum}{env.humidity.toFixed(0)}%{:else}<span class="text-rig-600">—</span>{/if}
		</td>
		<td class="px-3 text-right tabular-nums">
			{#if env.hasClimate}
				<span class={toneClass[vpdZone(env.vpd).tone]}>{env.vpd.toFixed(2)}</span>
			{:else}<span class="text-rig-600">—</span>{/if}
		</td>
		<td class="hidden px-3 text-right tabular-nums text-rig-400 sm:table-cell">
			{fmtDims(env) || '—'}
		</td>
		<td class="px-3">
			<span class="flex items-center gap-1.5 text-xs text-rig-400">
				<span class="h-2 w-2 rounded-full {healthDot(env.health)}"></span>
				{healthLabel(env.health)}
			</span>
		</td>
	</tr>
{/snippet}

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h1 class="text-xl font-semibold">Environments</h1>
			<p class="text-sm text-rig-400">All locations, lung rooms and grow boxes.</p>
		</div>
		{#if auth.isAdmin}
			<div class="flex items-center gap-2">
				<button
					onclick={() => (addingLocation = true)}
					class="inline-flex items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-sm text-rig-200 transition-colors hover:border-leaf hover:text-rig-50"
				>
					<MapPin size={15} /> Add location
				</button>
				<button
					onclick={() => addEnv.start()}
					class="inline-flex items-center gap-1.5 rounded-md border border-rig-700 px-3 py-1.5 text-sm text-rig-200 transition-colors hover:border-leaf hover:text-rig-50"
				>
					<Plus size={15} /> Add environment
				</button>
			</div>
		{/if}
	</div>

	{#if !snap}
		<div class="grid place-items-center py-24 text-rig-400"><p>Connecting to Grow Core…</p></div>
	{:else if !hasGroups}
		<div class="rounded-xl border border-dashed border-rig-800 p-10 text-center">
			<div class="mb-3 flex justify-center text-rig-500"><Tent size={40} /></div>
			<h2 class="mb-1 text-lg font-semibold">No environments yet</h2>
			<p class="text-sm text-rig-400">Add a location, grow box or lung room to see it here.</p>
		</div>
	{:else}
		<div class="overflow-x-auto rounded-xl border border-rig-800">
			<table class="w-full min-w-[36rem] text-sm">
				<thead>
					<tr class="text-left text-[11px] uppercase tracking-wide text-rig-500">
						<th class="px-3 py-2 font-medium">Name</th>
						<th class="px-3 py-2 font-medium">Type</th>
						<th class="px-3 py-2 text-right font-medium">Temp</th>
						<th class="px-3 py-2 text-right font-medium">RH</th>
						<th class="px-3 py-2 text-right font-medium">VPD</th>
						<th class="hidden px-3 py-2 text-right font-medium sm:table-cell">Size (cm)</th>
						<th class="px-3 py-2 font-medium">Status</th>
					</tr>
				</thead>
				<tbody>
					{#each groups as loc (loc.key)}
						{@const empty = !loc.rooms.length && !loc.looseBoxes.length}
						<tr class="group/loc bg-rig-900/40">
							<td colspan={7} class="px-3 py-1.5">
								<div class="flex items-center justify-between gap-3">
									<span
										class="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wide {loc.located
											? 'text-rig-300'
											: 'text-rig-500'}"
									>
										<MapPin size={12} />{loc.name}
									</span>
									{#if auth.isAdmin && loc.located}
										<button
											onclick={() => addEnv.start({ kind: 'room', locationId: loc.key })}
											class="inline-flex items-center gap-1 rounded-md border border-rig-700 px-2 py-0.5 text-[11px] font-medium normal-case tracking-normal text-rig-300 opacity-0 transition-opacity hover:border-leaf hover:text-rig-100 focus-visible:opacity-100 group-hover/loc:opacity-100"
										>
											<Plus size={12} /> Add lung room
										</button>
									{/if}
								</div>
							</td>
						</tr>
						{#if empty}
							<tr class="border-t border-rig-800/70">
								<td colspan={7} class="px-3 py-2.5 pl-9 text-sm text-rig-500">No rooms yet.</td>
							</tr>
						{/if}
						{#each loc.rooms as node (node.room.id)}
							{@render envRow(node.room, 1)}
							{#each node.boxes as box (box.id)}
								{@render envRow(box, 2)}
							{/each}
						{/each}
						{#each loc.looseBoxes as box (box.id)}
							{@render envRow(box, 1)}
						{/each}
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>

{#if auth.isAdmin}
	<Dialog bind:open={addingLocation} title="Add location" size="lg">
		<NewLocationForm onSaved={onLocationSaved} />
	</Dialog>
{/if}
