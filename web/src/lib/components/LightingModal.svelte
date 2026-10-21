<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { untrack } from 'svelte';
	import type { Grow, LightAnchor, LightSchedule, LightScheduleMode, StageLightDefaults } from '$lib/types';
	import { setControlGrow, setSchedule } from '$lib/api';
	import { anchorTimeFor, fmtMinutes, onMinuteFor, ratioLabel } from '$lib/photoperiod';
	import { titleCase } from '$lib/format';
	import { Button, Dialog, Select, type SelectItem } from '$lib/components/ui';

	interface Props {
		open?: boolean;
		environmentId: string;
		controlGrowId: string;
		schedule?: LightSchedule;
		grows: Grow[];
		defaults: StageLightDefaults;
		hasPrimaryLight: boolean;
		onSaved?: () => void;
	}
	let {
		open = $bindable(false),
		environmentId,
		controlGrowId,
		schedule,
		grows,
		defaults,
		hasPrimaryLight,
		onSaved
	}: Props = $props();

	// --- control grow ---
	let growId = $state('');

	// --- lighting fields ---
	let mode = $state<LightScheduleMode>('off');
	let lightsOnAt = $state('06:00');
	let anchor = $state<LightAnchor>('center');
	let customHours = $state(18);
	// Per-stage hours for phase mode, seeded from overrides or recommended defaults.
	let stageHours = $state<Record<string, number>>({});

	let busy = $state(false);
	let err = $state('');

	// The active grows selectable as control grow.
	const activeGrows = $derived(grows.filter((g) => g.status === 'active'));
	// Stage list driving the per-stage editor: the selected grow's stages, else
	// the known default stage names.
	const selectedGrow = $derived(grows.find((g) => g.id === growId));
	const stages = $derived(selectedGrow?.stages?.length ? selectedGrow.stages : Object.keys(defaults));

	// Reseed the whole form whenever the modal opens. This must depend ONLY on
	// `open`: the body writes `growId`, and `stages` derives from `growId`, so
	// reading either here would re-run the effect and clobber the user's choice.
	$effect(() => {
		if (!open) return;
		untrack(() => {
			growId = controlGrowId ?? '';
			mode = schedule?.mode ?? 'off';
			anchor = schedule?.anchor || 'center';
			lightsOnAt = schedule?.lightsOnAt || (anchor === 'start' ? '06:00' : '15:00');
			customHours = schedule?.onHours ?? 18;
			// Seed from the control grow's stages (not the reactive `stages`).
			const seedGrow = grows.find((g) => g.id === (controlGrowId ?? ''));
			const seedStages = seedGrow?.stages?.length ? seedGrow.stages : Object.keys(defaults);
			const seeded: Record<string, number> = {};
			for (const st of seedStages) seeded[st] = schedule?.stageOnHours?.[st] ?? defaults[st] ?? 18;
			stageHours = seeded;
		});
	});

	// Keep the per-stage editor populated when the user switches control grow: any
	// stage missing an entry gets its recommended default. Never overwrites edits.
	$effect(() => {
		const current = stages;
		untrack(() => {
			for (const st of current) {
				if (stageHours[st] === undefined) stageHours[st] = defaults[st] ?? 18;
			}
		});
	});

	const growItems: SelectItem[] = $derived([
		{ value: '__none__', label: 'None (manual)' },
		...activeGrows.map((g) => ({ value: g.id, label: g.name }))
	]);
	const modeItems: SelectItem[] = [
		{ value: 'off', label: 'Manual only' },
		{ value: 'phase', label: 'Follow stage' },
		{ value: 'custom', label: 'Custom' }
	];
	const anchorItems: SelectItem[] = [
		{ value: 'center', label: 'Middle of the light period' },
		{ value: 'start', label: 'Lights-on time' },
		{ value: 'end', label: 'Lights-off time' }
	];
	const anchorLabel: Record<LightAnchor, string> = {
		start: 'Lights on at',
		center: 'Day centred at',
		end: 'Lights off at'
	};
	const anchorHint: Record<LightAnchor, string> = {
		start: 'A shorter photoperiod ends earlier — 18/6 from 06:00 becomes 06:00–18:00 at 12/12.',
		center: 'A shorter photoperiod shrinks around the same midday — 18/6 06:00–00:00 becomes 09:00–21:00 at 12/12.',
		end: 'A shorter photoperiod starts later — 18/6 ending at 00:00 becomes 12:00–00:00 at 12/12.'
	};

	// The reference day used when converting between anchors: the grow's longest
	// photoperiod (its full day, usually seedling/veg), not the current stage's
	// shortened one. Centring on the current stage would just re-describe the
	// window it already has — centring on the full day is what moves a 12/12
	// flowering window back to the middle of the grower's day.
	const referenceHours = $derived(
		mode === 'custom' ? customHours : Math.max(0, ...stages.map((st) => stageHours[st] ?? 0))
	);

	// Switching anchor keeps the reference day where it is and re-derives the
	// anchor time, so start ⇄ center ⇄ end round-trips without drifting.
	function changeAnchor(next: LightAnchor) {
		const onMin = onMinuteFor(anchor, lightsOnAt, referenceHours);
		if (onMin != null) lightsOnAt = anchorTimeFor(next, onMin, referenceHours);
		anchor = next;
	}

	function offHours(on: number): string {
		return ratioLabel(on);
	}

	/** "09:00–21:00" for a photoperiod of `hours` under the current anchor. */
	function windowFor(hours: number): string {
		if (hours <= 0) return 'dark all day';
		if (hours >= 24) return 'lit all day';
		const onMin = onMinuteFor(anchor, lightsOnAt, hours);
		if (onMin == null) return '';
		return `${fmtMinutes(onMin)}–${fmtMinutes(onMin + Math.round(hours * 60))}`;
	}

	async function save() {
		busy = true;
		err = '';
		try {
			if (growId !== (controlGrowId ?? '')) {
				await setControlGrow(environmentId, growId);
			}
			const stageOnHours: Record<string, number> = {};
			if (mode === 'phase') {
				for (const st of stages) {
					// Persist an override only when it differs from the recommendation.
					if (stageHours[st] !== (defaults[st] ?? 18)) stageOnHours[st] = stageHours[st];
				}
			}
			await setSchedule(environmentId, { mode, lightsOnAt, anchor, onHours: customHours, stageOnHours });
			open = false;
			onSaved?.();
		} catch (e) {
			err = errMsg(e, 'Save failed');
		} finally {
			busy = false;
		}
	}

	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-leaf focus:outline-none';
</script>

<Dialog bind:open title="Control grow & lighting" description="Choose the grow whose stage drives this tent, and the light photoperiod.">
	<div class="space-y-6">
		{#if err}<p class="text-xs text-danger">{err}</p>{/if}

		<!-- Control grow -->
		<section class="space-y-3">
			<h3 class="text-xs font-semibold uppercase tracking-wide text-rig-400">Control grow</h3>
			<p class="text-xs text-rig-500">
				When several grows share this tent, the control grow's current stage supplies the automation
				presets. Choose "None" to keep lighting on defaults.
			</p>
			<Select
				value={growId || '__none__'}
				onValueChange={(v) => (growId = v === '__none__' ? '' : v)}
				items={growItems}
			/>
			{#if activeGrows.length === 0}
				<p class="rounded-md border border-rig-700 bg-rig-900/40 px-3 py-2 text-xs text-rig-400">
					No active grows yet. Create one under <a href="/grows" class="text-leaf hover:underline">Grows</a>.
				</p>
			{/if}
		</section>

		<!-- Lighting -->
		<section class="space-y-3 border-t border-rig-800 pt-4">
			<h3 class="text-xs font-semibold uppercase tracking-wide text-rig-400">Lighting (photoperiod)</h3>
			{#if !hasPrimaryLight}
				<p class="rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn">
					No primary grow light is set for this tent. Mark a light as primary in Settings &amp; Devices for the schedule to control it.
				</p>
			{/if}
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="block">
					<span class="text-xs text-rig-400">Mode</span>
					<Select value={mode} onValueChange={(v) => (mode = v as LightScheduleMode)} items={modeItems} class="mt-1" />
				</label>
				{#if mode !== 'off'}
					<label class="block">
						<span class="text-xs text-rig-400">Anchor the day on</span>
						<Select value={anchor} onValueChange={(v) => changeAnchor(v as LightAnchor)} items={anchorItems} class="mt-1" />
					</label>
					<label class="block">
						<span class="text-xs text-rig-400">{anchorLabel[anchor]}</span>
						<input type="time" bind:value={lightsOnAt} class="{field} mt-1" />
					</label>
				{/if}
			</div>
			{#if mode !== 'off'}
				<p class="text-xs text-rig-500">{anchorHint[anchor]}</p>
			{/if}

			{#if mode === 'custom'}
				<label class="block">
					<span class="text-xs text-rig-400">Hours of light ({offHours(customHours)} · {windowFor(customHours)})</span>
					<input type="number" min="0" max="24" step="0.5" bind:value={customHours} class="{field} mt-1" />
				</label>
			{:else if mode === 'phase'}
				<div class="space-y-1.5">
					<p class="text-xs text-rig-500">Hours of light per stage. Defaults are recommendations — override any of them.</p>
					{#each stages as st (st)}
						<div class="flex items-center gap-3">
							<span class="w-24 text-sm capitalize text-rig-300">{titleCase(st)}</span>
							<input
								type="number"
								min="0"
								max="24"
								step="0.5"
								bind:value={stageHours[st]}
								class="w-20 rounded-md border border-rig-700 bg-rig-950 px-2 py-1 text-sm focus:border-leaf focus:outline-none"
							/>
							<span class="text-xs text-rig-500">
								{offHours(stageHours[st] ?? 0)} · <span class="tabular-nums text-rig-400">{windowFor(stageHours[st] ?? 0)}</span>
								{#if stageHours[st] === (defaults[st] ?? 18)}· recommended{/if}
							</span>
						</div>
					{/each}
				</div>
			{/if}
		</section>

		<div class="flex justify-end gap-2 border-t border-rig-800 pt-4">
			<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
			<Button onclick={save} disabled={busy}>Save</Button>
		</div>
	</div>
</Dialog>
