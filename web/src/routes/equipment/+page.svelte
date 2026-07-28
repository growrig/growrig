<script lang="ts">
	import { onMount } from 'svelte';
	import { live } from '$lib/live.svelte';
	import { getBindings, getCatalog, getLocations } from '$lib/api';
	import { errMsg } from '$lib/errors';
	import { resolveLocationId } from '$lib/location';
	import { groupDevices, bindingStatus, type DeviceGroup } from '$lib/equipment';
	import type { Binding, BindingKind, CatalogProduct, EnvironmentView, Location } from '$lib/types';
	import KindIcon from '$lib/components/KindIcon.svelte';
	import MapPin from '@lucide/svelte/icons/map-pin';
	import Wind from '@lucide/svelte/icons/wind';
	import Tent from '@lucide/svelte/icons/tent';
	import Cpu from '@lucide/svelte/icons/cpu';

	let bindings = $state<Binding[]>([]);
	let catalog = $state<CatalogProduct[]>([]);
	let locations = $state<Location[]>([]);
	let error = $state('');
	let loaded = $state(false);

	onMount(async () => {
		try {
			[bindings, catalog, locations] = await Promise.all([
				getBindings(),
				getCatalog(),
				getLocations()
			]);
		} catch (e) {
			error = errMsg(e, 'Failed to reach Grow Core');
		} finally {
			loaded = true;
		}
	});

	const snap = $derived(live.snapshot);
	const environments = $derived(snap?.environments ?? []);
	const productById = $derived(new Map(catalog.map((p) => [p.id, p])));
	const envById = $derived(new Map(environments.map((e) => [e.id, e])));
	const locationById = $derived(new Map(locations.map((l) => [l.id, l])));

	// Category order doubles as the priority for placing a multi-capability
	// device: a controller that also reports a sensor is a controller.
	const categories: { kind: BindingKind; label: string }[] = [
		{ kind: 'controller', label: 'Controllers' },
		{ kind: 'light', label: 'Lights' },
		{ kind: 'fan', label: 'Fans & Airflow' },
		{ kind: 'camera', label: 'Cameras' },
		{ kind: 'irrigation', label: 'Irrigation' },
		{ kind: 'power', label: 'Power & Switching' },
		{ kind: 'sensor', label: 'Sensors' }
	];

	/** One physical device, with the environment it lives in resolved. */
	interface DeviceRow {
		device: DeviceGroup;
		env: EnvironmentView | undefined;
		locationName: string;
		kind: BindingKind;
	}

	const rows = $derived.by<DeviceRow[]>(() =>
		groupDevices(bindings).map((device) => {
			const env = envById.get(device.bindings[0]?.environmentId ?? '');
			const locId = env ? resolveLocationId(env, environments) : '';
			const kinds = new Set(device.bindings.map((b) => b.kind));
			return {
				device,
				env,
				locationName: locationById.get(locId)?.name ?? '',
				kind: categories.find((c) => kinds.has(c.kind))?.kind ?? device.bindings[0]?.kind ?? 'sensor'
			};
		})
	);

	// Category filter — chips above the table, counted in devices (not
	// capabilities) so the numbers match the rows below.
	let filter = $state<BindingKind | null>(null);
	const counts = $derived.by(() => {
		const m = new Map<BindingKind, number>();
		for (const r of rows) m.set(r.kind, (m.get(r.kind) ?? 0) + 1);
		return m;
	});
	const sections = $derived(
		categories
			.filter((c) => (filter ? c.kind === filter : true))
			.map((c) => ({
				...c,
				rows: rows
					.filter((r) => r.kind === c.kind)
					.sort((a, b) => a.device.name.localeCompare(b.device.name))
			}))
			.filter((s) => s.rows.length)
	);
	const visibleCategories = $derived(categories.filter((c) => counts.get(c.kind)));

	const totalCapabilities = $derived(bindings.length);

	/** Device is online when any of its capabilities reports as reachable. */
	function deviceOnline(row: DeviceRow): boolean | null {
		const states = row.device.bindings.map((b) => bindingStatus(b, row.env).online);
		if (states.some((s) => s === true)) return true;
		if (states.some((s) => s === false)) return false;
		return null;
	}
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h1 class="text-xl font-semibold">All Equipment</h1>
			<p class="text-sm text-rig-400">
				Every physical device installed across all lung rooms and grow boxes.
			</p>
		</div>
		{#if rows.length}
			<p class="text-sm tabular-nums text-rig-500">
				{rows.length} device{rows.length === 1 ? '' : 's'} · {totalCapabilities} capabilities
			</p>
		{/if}
	</div>

	{#if error}
		<div class="rounded-lg bg-danger/15 px-4 py-2 text-sm text-danger">{error}</div>
	{/if}

	{#if visibleCategories.length > 1}
		<nav class="-mx-1 flex flex-wrap gap-2 px-1">
			<button
				type="button"
				onclick={() => (filter = null)}
				class="rounded-full border px-3 py-1.5 text-xs font-medium transition-colors {filter === null
					? 'border-leaf bg-leaf/10 text-rig-50'
					: 'border-rig-800 bg-rig-950/40 text-rig-300 hover:border-rig-600 hover:text-rig-100'}"
			>
				All <span class="text-rig-500">{rows.length}</span>
			</button>
			{#each visibleCategories as cat (cat.kind)}
				<button
					type="button"
					onclick={() => (filter = filter === cat.kind ? null : cat.kind)}
					class="inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors {filter ===
					cat.kind
						? 'border-leaf bg-leaf/10 text-rig-50'
						: 'border-rig-800 bg-rig-950/40 text-rig-300 hover:border-rig-600 hover:text-rig-100'}"
				>
					<KindIcon kind={cat.kind} size={14} />
					{cat.label}
					<span class="text-rig-500">{counts.get(cat.kind)}</span>
				</button>
			{/each}
		</nav>
	{/if}

	{#if !snap || !loaded}
		<div class="grid place-items-center py-24 text-rig-400"><p>Connecting to Grow Core…</p></div>
	{:else if !sections.length}
		<div class="rounded-xl border border-dashed border-rig-800 p-10 text-center">
			<div class="mb-3 flex justify-center text-rig-500"><Cpu size={40} /></div>
			<h2 class="mb-1 text-lg font-semibold">
				{filter ? 'No devices in this category' : 'No equipment yet'}
			</h2>
			<p class="text-sm text-rig-400">
				Add devices from an environment's Equipment tab to see them here.
			</p>
		</div>
	{:else}
		<!-- One table per category — each stands alone with its own header row. -->
		<div class="space-y-8">
			{#each sections as section (section.kind)}
				<section>
					<div class="mb-3 flex items-center gap-2.5">
						<div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-rig-800 bg-rig-950/60">
							<KindIcon kind={section.kind} size={20} class="text-rig-300" />
						</div>
						<div>
							<h2 class="text-base font-semibold text-rig-100">{section.label}</h2>
							<p class="text-xs text-rig-500">
								{section.rows.length} device{section.rows.length === 1 ? '' : 's'}
							</p>
						</div>
					</div>
					<div class="overflow-x-auto rounded-xl border border-rig-800">
						<table class="w-full min-w-[38rem] table-fixed text-sm">
							<!-- Fixed layout with shared widths so every category table
							     lines up column-for-column down the page. -->
							<colgroup>
								<col class="w-[26%]" />
								<col class="hidden w-[26%] sm:table-column" />
								<col class="w-[20%]" />
								<col class="hidden w-[14%] md:table-column" />
								<col class="w-[9%]" />
								<col class="w-[13%]" />
							</colgroup>
							<thead>
								<tr class="text-left text-[11px] uppercase tracking-wide text-rig-500">
									<th class="px-3 py-2 font-medium">Device</th>
									<th class="hidden px-3 py-2 font-medium sm:table-cell">Product</th>
									<th class="px-3 py-2 font-medium">Environment</th>
									<th class="hidden px-3 py-2 font-medium md:table-cell">Location</th>
									<th class="px-3 py-2 text-right font-medium">Capabilities</th>
									<th class="px-3 py-2 font-medium">Status</th>
								</tr>
							</thead>
							<tbody>
								{#each section.rows as row (row.device.id)}
								{@const product = productById.get(row.device.bindings[0]?.productId ?? '')}
								{@const online = deviceOnline(row)}
								<tr class="border-t border-rig-800/70 transition-colors hover:bg-rig-800/30">
									<td class="py-2.5 pl-3 pr-3">
										<div class="flex min-w-0 items-center gap-2">
											{#if product?.image}
												<img
													src={product.image}
													alt=""
													class="h-8 w-8 shrink-0 rounded border border-rig-800 bg-rig-950 object-contain p-0.5"
												/>
											{/if}
											<span class="truncate font-medium text-rig-100">{row.device.name}</span>
										</div>
									</td>
									<td class="hidden truncate px-3 text-rig-400 sm:table-cell">
										{product ? `${product.brand} ${product.model}` : '—'}
									</td>
									<td class="px-3">
										{#if row.env}
											<a
												href="/env/{row.env.id}"
												class="flex min-w-0 items-center gap-1.5 text-rig-200 hover:underline"
											>
												{#if row.env.kind === 'room'}
													<Wind size={14} class="shrink-0 text-rig-400" />
												{:else}
													<Tent size={14} class="shrink-0 text-rig-400" />
												{/if}
												<span class="truncate">{row.env.name}</span>
											</a>
										{:else}
											<span class="text-rig-600">—</span>
										{/if}
									</td>
									<td class="hidden px-3 md:table-cell">
										{#if row.locationName}
											<span class="flex min-w-0 items-center gap-1.5 text-rig-400">
												<MapPin size={13} class="shrink-0" /><span class="truncate"
													>{row.locationName}</span
												>
											</span>
										{:else}<span class="text-rig-600">—</span>{/if}
									</td>
									<td class="px-3 text-right font-semibold tabular-nums text-rig-100">
										{row.device.bindings.length}
									</td>
									<td class="px-3">
										<span class="flex items-center gap-1.5 text-xs text-rig-400">
											<span
												class="h-2 w-2 shrink-0 rounded-full {online
													? 'bg-leaf'
													: online === false
														? 'bg-danger'
														: 'bg-rig-700'}"
											></span>
											{online ? 'Online' : online === false ? 'Offline' : 'Unknown'}
										</span>
									</td>
								</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</section>
			{/each}
		</div>
	{/if}
</div>
