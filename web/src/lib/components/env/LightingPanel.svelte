<script lang="ts">
	import { slide } from 'svelte/transition';
	import { errMsg } from '$lib/errors';
	import type { EnvironmentView, Grow, StageLightDefaults, ControlMode } from '$lib/types';
	import { setControl, setSwitch } from '$lib/api';
	import { fmtMinutes, lightWindow, nextTransition, ratioLabel, windowLabel } from '$lib/photoperiod';
	import { relTime, titleCase } from '$lib/format';
	import ControlModeToggle from './ControlModeToggle.svelte';
	import LightingModal from '$lib/components/LightingModal.svelte';
	import { Button, Switch } from '$lib/components/ui';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Lightbulb from '@lucide/svelte/icons/lightbulb';
	import LightbulbOff from '@lucide/svelte/icons/lightbulb-off';
	import Sun from '@lucide/svelte/icons/sun';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';

	interface Props {
		env: EnvironmentView;
		canWrite: boolean;
		grows: Grow[];
		defaults: StageLightDefaults;
		flash?: (kind: 'ok' | 'err', text: string) => void;
	}
	let { env, canWrite, grows, defaults, flash }: Props = $props();

	let open = $state(false);
	let editing = $state(false);

	const lights = $derived(env.controls?.filter((c) => c.kind === 'light') ?? []);
	const hasPrimaryLight = $derived(lights.length > 0);
	const mode = $derived<ControlMode>(env.control.lighting.mode);
	const stage = $derived(env.grow?.stage ?? '');
	// The grow whose stage drives the schedule: the nominated one, or the only
	// grow growing here (resolved by the engine into env.grow).
	const controlGrowId = $derived(env.controlGrowId || env.grow?.id || '');
	const controlGrowName = $derived(grows.find((g) => g.id === controlGrowId)?.name ?? env.grow?.name ?? 'None');

	// Live countdown to the next scheduled transition.
	let nowMs = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (nowMs = Date.now()), 30_000);
		return () => clearInterval(t);
	});

	// The stage list shown in the Automatic view.
	const scheduleStages = $derived.by(() => {
		const g = grows.find((gr) => gr.id === controlGrowId);
		return g?.stages?.length ? g.stages : Object.keys(defaults);
	});
	const stageHours = (st: string) => env.schedule?.stageOnHours?.[st] ?? defaults[st] ?? 18;
	// The wall-clock window a given photoperiod lands on under this schedule's
	// anchor, e.g. 12/12 centred on 15:00 → "09:00–21:00".
	const stageWindow = (st: string) => {
		const h = stageHours(st);
		if (h <= 0) return 'dark all day';
		if (h >= 24) return 'lit all day';
		const win = lightWindow(env.schedule, st, defaults);
		return win ? `${fmtMinutes(win.on)}–${fmtMinutes(win.off)}` : '';
	};
	const anchorLabel: Record<string, string> = {
		start: 'Lights on at',
		center: 'Day centred at',
		end: 'Lights off at'
	};

	const summary = $derived.by(() => {
		if (mode === 'manual') return 'Manual — you control the light';
		const follows = env.schedule?.mode !== 'phase' ? '' : stage ? ' · follows stage' : ' · no control grow';
		const next = nextTransition(env.schedule, stage, defaults, nowMs);
		const countdown = next ? ` · ${next.on ? 'on' : 'off'} in ${relTime(next.at - nowMs)}` : '';
		return `${windowLabel(env.schedule, stage, defaults)}${follows}${countdown}`;
	});

	async function setMode(next: ControlMode) {
		try {
			await setControl(env.id, { lighting: next });
		} catch (e) {
			flash?.('err', errMsg(e, 'Could not change lighting mode'));
		}
	}
	async function toggleLight(id: string, on: boolean) {
		try {
			await setSwitch(id, on);
		} catch {
			/* reconciles via live feed */
		}
	}

	const ro = 'rounded-md border border-rig-800 bg-rig-950/40 px-3 py-2 text-sm text-rig-200';
</script>

<section class="rounded-xl border border-rig-800 bg-rig-900/40">
	<!-- Header (click to expand/collapse) -->
	<div class="flex flex-wrap items-center justify-between gap-3 p-5">
		<button type="button" onclick={() => (open = !open)} class="flex min-w-0 flex-1 items-center gap-2.5 text-left" aria-expanded={open}>
			<ChevronDown size={16} class="shrink-0 text-rig-500 transition-transform duration-200 {open ? 'rotate-0' : '-rotate-90'}" />
			<Sun size={18} class={mode === 'auto' ? 'text-warn' : 'text-rig-500'} />
			<div class="min-w-0">
				<h2 class="text-base font-semibold">Lighting</h2>
				<p class="truncate text-xs text-rig-400">{summary}</p>
			</div>
		</button>
		{#if canWrite}
			<ControlModeToggle value={mode} onChange={setMode} disabled={!hasPrimaryLight} />
		{/if}
	</div>

	{#if open}
		<div transition:slide={{ duration: 200 }} class="border-t border-rig-800 p-5 pt-4">
			{#if !hasPrimaryLight}
				<p class="rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn">
					No grow light assigned. Add one under <a href="/env/{env.id}?tab=equipment" class="underline">Equipment</a> to control the photoperiod.
				</p>
			{:else if mode === 'auto'}
				<!-- Automatic still has preferences: these are the settings the
				     automation follows, editable without dropping to manual. -->
				<div class="grid gap-3 sm:grid-cols-3">
					<div><span class="text-xs text-rig-400">Control grow</span><div class="mt-1 {ro}">{controlGrowName}</div></div>
					<div><span class="text-xs text-rig-400">Schedule</span><div class="mt-1 {ro}">{env.schedule?.mode === 'custom' ? 'Custom' : 'Follow stage'}</div></div>
					<div>
						<span class="text-xs text-rig-400">{anchorLabel[env.schedule?.anchor ?? 'center']}</span>
						<div class="mt-1 {ro}">{env.schedule?.lightsOnAt ?? '—'}</div>
					</div>
				</div>
				{#if env.schedule?.mode === 'custom'}
					<div class="mt-4 text-sm text-rig-300">{windowLabel(env.schedule, stage, defaults)} of light every day.</div>
				{:else}
					{#if !stage}
						<p class="mt-4 rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn">
							No control grow — several grows share this box, or none. Pick one under Edit schedule so the light follows its stage; until then it runs the default {ratioLabel(stageHours(''))}.
						</p>
					{/if}
					<div class="mt-4 space-y-1">
						<p class="text-xs text-rig-500">Hours of light per stage, and the window each one lands on.</p>
						{#each scheduleStages as st (st)}
							<div class="flex items-center gap-3 text-sm">
								<span class="w-24 capitalize text-rig-300">{titleCase(st)}</span>
								<span class="w-14 tabular-nums text-rig-200">{ratioLabel(stageHours(st))}</span>
								<span class="tabular-nums text-rig-400">{stageWindow(st)}</span>
								{#if st === stage}<span class="rounded-full bg-warn/15 px-2 py-0.5 text-[10px] uppercase tracking-wide text-warn">current</span>{/if}
							</div>
						{/each}
					</div>
				{/if}
				{#if canWrite}
					<div class="mt-4 flex items-center justify-between gap-3 border-t border-rig-800 pt-4">
						<p class="text-xs text-rig-500">Automatic follows these preferences — change them here, no need to switch to manual.</p>
						<Button variant="ghost" onclick={() => (editing = true)} class="shrink-0">
							<Pencil size={14} class="mr-1.5" />Edit schedule
						</Button>
					</div>
				{/if}
			{:else}
				<!-- Manual: direct light switches -->
				<div class="grid gap-3 sm:grid-cols-2">
					{#each lights as light (light.id)}
						<div class="flex items-center justify-between rounded-lg border border-rig-800 bg-rig-950/40 p-3">
							<div class="flex items-center gap-2 text-sm font-medium">
								{#if light.on}<Lightbulb size={16} class="text-leaf" />{:else}<LightbulbOff size={16} class="text-rig-500" />{/if}
								{light.name}
							</div>
							<div class="flex items-center gap-2">
								<span class="text-xs tabular-nums {light.on ? 'text-leaf' : 'text-rig-400'}">{light.on ? 'On' : 'Off'}</span>
								{#if canWrite}<Switch checked={light.on} onCheckedChange={(v) => toggleLight(light.id, v)} />{/if}
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</section>

{#if canWrite}
	<LightingModal
		bind:open={editing}
		environmentId={env.id}
		{controlGrowId}
		schedule={env.schedule}
		{grows}
		{defaults}
		{hasPrimaryLight}
	/>
{/if}
