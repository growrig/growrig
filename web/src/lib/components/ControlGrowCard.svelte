<script lang="ts">
	import type { Grow, GrowSummary, LightSchedule, StageLightDefaults } from '$lib/types';
	import LightingModal from '$lib/components/LightingModal.svelte';
	import { nextTransition, windowLabel } from '$lib/photoperiod';
	import { relTime } from '$lib/format';
	import Sun from '@lucide/svelte/icons/sun';

	interface Props {
		environmentId: string;
		/** Live summary of this environment's control grow, if set. */
		grow?: GrowSummary;
		schedule?: LightSchedule;
		/** All grows, for the control-grow picker and its stage list. */
		grows: Grow[];
		defaults: StageLightDefaults;
		hasPrimaryLight: boolean;
		/** When false, everything is read-only. */
		canEdit?: boolean;
	}
	let { environmentId, grow, schedule, grows, defaults, hasPrimaryLight, canEdit = true }: Props = $props();

	let editing = $state(false);

	// Ticks so the countdown to the next light transition stays live.
	let nowMs = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (nowMs = Date.now()), 30_000);
		return () => clearInterval(t);
	});

	const stage = $derived(grow?.stage ?? '');

	const lightingSummary = $derived.by(() => {
		if (!schedule || schedule.mode === 'off') return 'Lighting: manual';
		const follows = schedule.mode !== 'phase' ? '' : stage ? ' · follows stage' : ' · no control grow';
		const next = nextTransition(schedule, stage, defaults, nowMs);
		const countdown = next ? ` · ${next.on ? 'on' : 'off'} in ${relTime(next.at - nowMs)}` : '';
		return `${windowLabel(schedule, stage, defaults)}${follows}${countdown}`;
	});
</script>

<section>
	<div class="mb-3 flex items-center justify-between">
		<h2 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Control grow &amp; lighting</h2>
	</div>
	{#snippet body()}
		{#if grow}
			<!-- The occupants panel above already names the grow; here we only surface
			     the lighting schedule this grow's stage drives. -->
			<div class="flex items-center gap-1.5 text-sm text-rig-400">
				<Sun size={14} class={schedule && schedule.mode !== 'off' ? 'text-warn' : 'text-rig-600'} />
				<span>{lightingSummary}</span>
			</div>
		{:else}
			<div class="text-sm text-rig-400">
				No control grow selected.
				<span class="text-rig-500">Nominate a grow to drive this tent's photoperiod.</span>
			</div>
		{/if}
	{/snippet}
	{#if canEdit}
		<button
			type="button"
			onclick={() => (editing = true)}
			class="block w-full rounded-lg border border-rig-800 bg-rig-950/40 p-4 text-left transition-colors hover:border-rig-600 focus-visible:border-leaf focus-visible:outline-none"
		>
			{@render body()}
		</button>
	{:else}
		<div class="rounded-lg border border-rig-800 bg-rig-950/40 p-4">
			{@render body()}
		</div>
	{/if}
</section>

{#if canEdit}
	<LightingModal
		bind:open={editing}
		{environmentId}
		controlGrowId={grow?.id ?? ''}
		{schedule}
		{grows}
		{defaults}
		{hasPrimaryLight}
	/>
{/if}
