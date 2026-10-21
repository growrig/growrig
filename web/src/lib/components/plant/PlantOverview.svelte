<script lang="ts">
	// What a grower wants to know standing in front of one plant: how old it is,
	// where it lives and how that space reads right now, what it is potted in,
	// when it was last cared for, and what has happened to it lately.
	import { live } from '$lib/live.svelte';
	import type { CareEvent, CareHistory, GrowDetail, PlantPot, PlantView } from '$lib/types';
	import { careVisual, ago, fmtVolume } from '$lib/care';
	import { daysSince, titleCase, vpdZone, toneClass } from '$lib/format';
	import { fmtDate } from '$lib/datetime';
	import MapPin from '@lucide/svelte/icons/map-pin';
	import FlaskConical from '@lucide/svelte/icons/flask-conical';
	import CalendarDays from '@lucide/svelte/icons/calendar-days';
	import Sprout from '@lucide/svelte/icons/sprout';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';

	interface Props {
		plant: PlantView;
		grow?: GrowDetail | null;
		care?: CareHistory | null;
		onShowHistory: () => void;
	}
	let { plant, grow, care, onShowHistory }: Props = $props();

	const potLabel = (p: PlantPot) => `${p.size} ${p.unit}${p.type ? ` · ${titleCase(p.type)}` : ''}`;

	// Live climate of the environment this plant sits in, so the page says
	// something about *now* and not only about the past.
	const env = $derived(
		plant.currentEnvironmentId
			? live.snapshot?.environments?.find((e) => e.id === plant.currentEnvironmentId)
			: undefined
	);
	const vpd = $derived(env?.hasClimate && env.vpd > 0 ? vpdZone(env.vpd) : null);

	// Care history is kept per grow, so narrow it to the events this plant was
	// actually part of — a grow-wide "watered 2h ago" can be false for a plant
	// that was skipped.
	const mine = $derived(
		(care?.events ?? []).filter((e) => e.applications.some((a) => a.plantUnitId === plant.id))
	);
	const lastByType = $derived.by(() => {
		const out = new Map<string, CareEvent>();
		// Events arrive newest first; the first of each type is the latest.
		for (const e of mine) if (!out.has(e.type)) out.set(e.type, e);
		return [...out.values()].sort(
			(a, b) => new Date(b.occurredAt).getTime() - new Date(a.occurredAt).getTime()
		);
	});
	const myAmount = (e: CareEvent) =>
		e.applications.filter((a) => a.plantUnitId === plant.id).reduce((n, a) => n + (a.amountMl ?? 0), 0);

	// What this plant drank over the last month — the number growers actually
	// compare between plants.
	const water30d = $derived.by(() => {
		const since = Date.now() - 30 * 86_400_000;
		let ml = 0;
		let times = 0;
		for (const e of mine) {
			if ((e.type !== 'water' && e.type !== 'feed') || new Date(e.occurredAt).getTime() < since) continue;
			const amount = myAmount(e);
			if (amount <= 0) continue;
			ml += amount;
			times++;
		}
		return { ml, times };
	});

	const pots = $derived([...(plant.pots ?? [])].sort(
		(a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime()
	));
	const placements = $derived([...(plant.placements ?? [])].sort(
		(a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime()
	));

	const potDays = $derived(plant.currentPot ? daysSince(plant.currentPot.startedAt) : 0);
	const stageDays = $derived(grow ? grow.stageDays : 0);

	const card = 'rounded-xl border border-rig-800 bg-rig-900/40 p-4';
	const cardLabel = 'flex items-center gap-1.5 text-xs uppercase tracking-wide text-rig-500';
</script>

<div class="space-y-6">
	<!-- At a glance -->
	<section class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
		<div class={card}>
			<div class={cardLabel}><CalendarDays size={13} /> Age</div>
			<div class="mt-1.5 text-xl font-semibold tabular-nums">{daysSince(plant.createdAt)}<span class="ml-1 text-sm font-normal text-rig-500">days</span></div>
			<div class="mt-0.5 text-xs text-rig-500">Started {fmtDate(plant.createdAt)}</div>
		</div>

		<div class={card}>
			<div class={cardLabel}><Sprout size={13} /> Stage</div>
			{#if grow}
				<div class="mt-1.5 text-xl font-semibold">{titleCase(grow.stage)}</div>
				<div class="mt-0.5 text-xs text-rig-500">Day {stageDays} of this stage · {grow.name}</div>
			{:else}
				<div class="mt-1.5 text-xl font-semibold text-rig-500">—</div>
			{/if}
		</div>

		<div class={card}>
			<div class={cardLabel}><MapPin size={13} /> Location</div>
			{#if plant.currentEnvironmentId}
				<a href="/env/{plant.currentEnvironmentId}" class="mt-1.5 block truncate text-xl font-semibold hover:underline">
					{plant.currentEnvironmentName || plant.currentEnvironmentId}
				</a>
				{#if env?.hasClimate}
					<div class="mt-0.5 flex flex-wrap gap-x-2 text-xs tabular-nums text-rig-500">
						{#if env.hasTemp}<span>{env.tempC.toFixed(1)}°C</span>{/if}
						{#if env.hasHum}<span>{env.humidity.toFixed(0)}%</span>{/if}
						{#if vpd}<span class={toneClass[vpd.tone]}>VPD {env.vpd.toFixed(2)}</span>{/if}
					</div>
				{:else}
					<div class="mt-0.5 text-xs text-rig-500">No live readings</div>
				{/if}
			{:else}
				<div class="mt-1.5 text-xl font-semibold text-rig-500">Nowhere</div>
			{/if}
		</div>

		<div class={card}>
			<div class={cardLabel}><FlaskConical size={13} /> Pot</div>
			{#if plant.currentPot}
				<div class="mt-1.5 text-xl font-semibold">{plant.currentPot.size}<span class="ml-1 text-sm font-normal text-rig-500">{plant.currentPot.unit}</span></div>
				<div class="mt-0.5 text-xs text-rig-500">
					{plant.currentPot.type ? `${titleCase(plant.currentPot.type)} · ` : ''}{potDays === 0 ? 'potted today' : `${potDays}d in this pot`}
				</div>
			{:else}
				<div class="mt-1.5 text-xl font-semibold text-rig-500">None</div>
				<div class="mt-0.5 text-xs text-rig-500">Log a transplant to record one</div>
			{/if}
		</div>
	</section>

	<!-- Last care, per action -->
	<section class="space-y-3">
		<div class="flex items-baseline justify-between">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Last care</h2>
			{#if water30d.times > 0}
				<span class="text-xs tabular-nums text-rig-500">{fmtVolume(water30d.ml)} over {water30d.times} {water30d.times === 1 ? 'watering' : 'waterings'} · 30d</span>
			{/if}
		</div>
		{#if lastByType.length === 0}
			<div class="rounded-xl border border-dashed border-rig-800 p-6 text-center text-sm text-rig-500">
				Nothing logged for this plant yet.
			</div>
		{:else}
			<div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
				{#each lastByType as event (event.id)}
					{@const visual = careVisual(event.type)}
					{@const amount = myAmount(event)}
					<div class="flex items-center gap-3 rounded-xl border border-rig-800 bg-rig-900/40 px-3 py-2.5">
						<span class="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-rig-800 text-rig-300">
							<visual.icon size={15} />
						</span>
						<div class="min-w-0 flex-1">
							<div class="truncate text-sm font-medium">{visual.label}</div>
							<div class="truncate text-xs text-rig-500">
								{[ago(event.occurredAt), amount > 0 ? fmtVolume(amount) : '', event.recipeName ?? '']
									.filter(Boolean)
									.join(' · ')}
							</div>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>

	<!-- The two histories that are specific to a plant rather than its grow -->
	<div class="grid gap-6 lg:grid-cols-2">
		<section class="space-y-3">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Pot history</h2>
			{#if pots.length === 0}
				<div class="rounded-xl border border-dashed border-rig-800 p-6 text-center text-sm text-rig-500">No pot recorded.</div>
			{:else}
				<ol class="space-y-2">
					{#each pots as pot (pot.id)}
						<li class="flex items-center justify-between gap-3 rounded-xl border border-rig-800 bg-rig-900/40 px-3 py-2.5">
							<div class="min-w-0">
								<div class="text-sm font-medium">{potLabel(pot)}</div>
								<div class="text-xs text-rig-500">
									{fmtDate(pot.startedAt)} – {pot.endedAt ? fmtDate(pot.endedAt) : 'now'}
								</div>
							</div>
							{#if !pot.endedAt}
								<span class="shrink-0 rounded-full bg-leaf/15 px-2 py-0.5 text-[11px] font-medium text-leaf">Current</span>
							{/if}
						</li>
					{/each}
				</ol>
			{/if}
		</section>

		<section class="space-y-3">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Where it has lived</h2>
			{#if placements.length === 0}
				<div class="rounded-xl border border-dashed border-rig-800 p-6 text-center text-sm text-rig-500">No placement recorded.</div>
			{:else}
				<ol class="space-y-2">
					{#each placements as placement (placement.id)}
						<li class="flex items-center justify-between gap-3 rounded-xl border border-rig-800 bg-rig-900/40 px-3 py-2.5">
							<div class="min-w-0">
								<a href="/env/{placement.environmentId}" class="block truncate text-sm font-medium hover:underline">
									{placement.environmentName || placement.environmentId}
								</a>
								<div class="text-xs text-rig-500">
									{fmtDate(placement.startedAt)} – {placement.endedAt ? fmtDate(placement.endedAt) : 'now'}
								</div>
							</div>
							{#if !placement.endedAt}
								<span class="shrink-0 rounded-full bg-leaf/15 px-2 py-0.5 text-[11px] font-medium text-leaf">Current</span>
							{/if}
						</li>
					{/each}
				</ol>
			{/if}
		</section>
	</div>

	<button
		type="button"
		onclick={onShowHistory}
		class="flex w-full items-center justify-center gap-1.5 rounded-lg border border-rig-800 py-2.5 text-sm text-rig-300 transition-colors hover:border-rig-600 hover:text-rig-100"
	>
		See the full history <ArrowRight size={15} />
	</button>
</div>
