<script lang="ts">
	import type { Cycle, LightSchedule, Phase, PhotoperiodDefaults } from '$lib/types';
	import CycleModal from '$lib/components/CycleModal.svelte';
	import { nextTransition } from '$lib/photoperiod';
	import Sun from '@lucide/svelte/icons/sun';

	interface Props {
		environmentId: string;
		cycle?: Cycle;
		schedule?: LightSchedule;
		phases: Phase[];
		defaults: PhotoperiodDefaults;
		hasPrimaryLight: boolean;
		/** When false, the cycle/schedule is read-only (no edit controls). */
		canEdit?: boolean;
	}
	let { environmentId, cycle, schedule, phases, defaults, hasPrimaryLight, canEdit = true }: Props = $props();

	let editing = $state(false);

	// Ticks so the countdown to the next light transition stays live.
	let nowMs = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (nowMs = Date.now()), 30_000);
		return () => clearInterval(t);
	});

	function daysSince(iso?: string): number {
		if (!iso) return 0;
		return Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 86400000));
	}

	function relTime(ms: number): string {
		const min = Math.max(0, Math.round(ms / 60_000));
		const h = Math.floor(min / 60);
		const m = min % 60;
		if (min < 1) return 'now';
		if (h === 0) return `${m}m`;
		if (m === 0) return `${h}h`;
		return `${h}h ${m}m`;
	}

	// Effective hours of light for the current phase given the active schedule.
	const effectiveHours = $derived.by(() => {
		if (!schedule || schedule.mode === 'off') return null;
		if (schedule.mode === 'custom') return schedule.onHours;
		const p = cycle?.phase ?? 'vegetative';
		return schedule.phaseOnHours?.[p] ?? defaults[p] ?? 18;
	});

	const lightingSummary = $derived.by(() => {
		if (!schedule || schedule.mode === 'off') return 'Lighting: manual';
		const h = effectiveHours ?? 0;
		const off = Math.max(0, 24 - h);
		const follows = schedule.mode === 'phase' ? ' · follows phase' : '';
		const next = nextTransition(schedule, cycle?.phase ?? 'vegetative', defaults, nowMs);
		const countdown = next ? ` · ${next.on ? 'on' : 'off'} in ${relTime(next.at - nowMs)}` : '';
		return `${h}/${off} · on at ${schedule.lightsOnAt}${follows}${countdown}`;
	});
</script>

<section>
	<div class="mb-3 flex items-center justify-between">
		<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Cycle</h2>
	</div>
	<div class="rounded-lg border border-rig-800 bg-rig-950/40 p-4">
		{#if cycle}
			<div class="flex items-center justify-between">
				<div>
					<div class="text-lg font-semibold">{cycle.strain || 'Unnamed strain'}</div>
					<div class="mt-1 flex flex-wrap items-center gap-2 text-sm text-rig-400">
						<span class="rounded-full bg-rig-800 px-2 py-0.5 text-xs capitalize text-leaf">{cycle.phase}</span>
						<span>day {daysSince(cycle.startedAt)}</span>
						<span class="text-rig-600">·</span>
						<span>{daysSince(cycle.phaseStarted)}d in {cycle.phase}</span>
					</div>
					<div class="mt-2 flex items-center gap-1.5 text-sm text-rig-400">
						<Sun size={14} class={schedule && schedule.mode !== 'off' ? 'text-warn' : 'text-rig-600'} />
						<span>{lightingSummary}</span>
					</div>
				</div>
				{#if canEdit}
					<button onclick={() => (editing = true)} class="rounded-md border border-rig-700 px-3 py-1.5 text-sm text-rig-300 hover:border-rig-500">Edit</button>
				{/if}
			</div>
		{:else}
			<div class="flex items-center justify-between">
				<span class="text-sm text-rig-400">No active cycle.</span>
				{#if canEdit}
					<button onclick={() => (editing = true)} class="rounded-md bg-rig-500 px-3 py-1.5 text-sm font-medium text-rig-950 hover:bg-rig-400">Start a cycle</button>
				{/if}
			</div>
		{/if}
	</div>
</section>

{#if canEdit}
	<CycleModal bind:open={editing} {environmentId} {cycle} {schedule} {phases} {defaults} {hasPrimaryLight} />
{/if}
