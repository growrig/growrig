<script lang="ts">
	import { slide } from 'svelte/transition';
	import { untrack } from 'svelte';
	import { errMsg } from '$lib/errors';
	import type { EnvironmentView, ControlMode } from '$lib/types';
	import { setControl } from '$lib/api';
	import ControlModeToggle from './ControlModeToggle.svelte';
	import TargetsForm from './TargetsForm.svelte';
	import { Slider } from '$lib/components/ui';
	import Wind from '@lucide/svelte/icons/wind';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';

	interface Props {
		env: EnvironmentView;
		canWrite: boolean;
		onChanged?: () => void;
		flash?: (kind: 'ok' | 'err', text: string) => void;
	}
	let { env, canWrite, onChanged, flash }: Props = $props();

	let open = $state(false);

	const fans = $derived(env.controls?.filter((c) => c.kind === 'fan') ?? []);
	const exhaustFans = $derived(fans.filter((f) => f.role === 'exhaust' || f.role === 'intake'));
	const circFans = $derived(fans.filter((f) => f.role === 'circulation'));
	const hasFans = $derived(fans.length > 0);
	const mode = $derived<ControlMode>(env.control.airExchange.mode);

	const maxSpeed = (list: typeof fans) => list.reduce((m, f) => Math.max(m, f.desiredSpeed), 0);
	const liveExhaust = $derived(maxSpeed(exhaustFans));
	const liveCirc = $derived(maxSpeed(circFans));

	const autoSummary = $derived.by(() => {
		if (!hasFans) return 'No fans assigned';
		const parts = [];
		if (exhaustFans.length) parts.push(`Exhaust ${liveExhaust}%`);
		if (circFans.length) parts.push(`Circulation ${liveCirc}%`);
		return `Climate-driven · ${parts.join(' · ') || 'idle'}`;
	});

	// --- manual setpoints (local, debounced save) ---
	let exhaust = $state(0);
	let circulation = $state(0);
	const airSig = $derived(
		JSON.stringify({ e: env.control.airExchange.exhaust, c: env.control.airExchange.circulation })
	);
	$effect(() => {
		airSig; // track persisted values
		untrack(() => {
			exhaust = env.control.airExchange.exhaust;
			circulation = env.control.airExchange.circulation;
		});
	});

	let saveTimer: ReturnType<typeof setTimeout> | undefined;
	function queueSave() {
		clearTimeout(saveTimer);
		saveTimer = setTimeout(async () => {
			try {
				await setControl(env.id, { exhaust, circulation });
			} catch (e) {
				flash?.('err', errMsg(e, 'Could not set fan speeds'));
			}
		}, 400);
	}

	async function setMode(next: ControlMode) {
		try {
			if (next === 'manual') {
				await setControl(env.id, {
					air: 'manual',
					exhaust: env.control.airExchange.exhaust || liveExhaust,
					circulation: env.control.airExchange.circulation || liveCirc
				});
			} else {
				await setControl(env.id, { air: 'auto' });
			}
		} catch (e) {
			flash?.('err', errMsg(e, 'Could not change air mode'));
		}
	}
</script>

<section class="rounded-xl border border-rig-800 bg-rig-900/40">
	<div class="flex flex-wrap items-center justify-between gap-3 p-5">
		<button type="button" onclick={() => (open = !open)} class="flex min-w-0 flex-1 items-center gap-2.5 text-left" aria-expanded={open}>
			<ChevronDown size={16} class="shrink-0 text-rig-500 transition-transform duration-200 {open ? 'rotate-0' : '-rotate-90'}" />
			<Wind size={18} class="text-rig-300" />
			<div class="min-w-0">
				<h2 class="text-base font-semibold">Air exchange</h2>
				<p class="truncate text-xs text-rig-400">{mode === 'manual' ? `Manual · Exhaust ${exhaust}% · Circulation ${circulation}%` : autoSummary}</p>
			</div>
		</button>
		{#if canWrite}
			<ControlModeToggle value={mode} onChange={setMode} disabled={!hasFans} />
		{/if}
	</div>

	{#if open}
		<div transition:slide={{ duration: 200 }} class="border-t border-rig-800 p-5 pt-4">
			{#if mode === 'manual' && hasFans}
				<div class="grid gap-6 sm:grid-cols-2">
					<div>
						<div class="flex items-center justify-between text-sm text-rig-400">
							<span>Exhaust / intake</span><span class="tabular-nums text-rig-200">{exhaust}%</span>
						</div>
						<Slider min={0} max={100} step={1} bind:value={exhaust} onValueChange={queueSave} disabled={!canWrite || exhaustFans.length === 0} class="mt-3" />
					</div>
					<div>
						<div class="flex items-center justify-between text-sm text-rig-400">
							<span>Circulation</span><span class="tabular-nums text-rig-200">{circulation}%</span>
						</div>
						<Slider min={0} max={100} step={1} bind:value={circulation} onValueChange={queueSave} disabled={!canWrite || circFans.length === 0} class="mt-3" />
					</div>
				</div>
				<p class="mt-3 text-xs text-rig-500">Fixed speeds are held until you change them. An emergency over-temperature still forces every fan to full.</p>
			{/if}

			<!-- Climate targets & safety. These are the preferences automatic mode
			     follows, so they stay editable in both modes. -->
			{#if env.kind === 'tent'}
				<div class="{mode === 'manual' && hasFans ? 'mt-5 border-t border-rig-800 pt-4' : ''}">
					<div class="mb-3">
						<h3 class="text-xs font-semibold uppercase tracking-wide text-rig-400">Targets &amp; safety</h3>
						<p class="mt-0.5 text-xs text-rig-500">
							{mode === 'auto'
								? 'Climate targets drive automatic air exchange — adjust them here, no need to switch to manual. The emergency limit always applies.'
								: 'Adjust the climate targets and safety limits.'}
						</p>
					</div>
					<TargetsForm {env} readOnly={!canWrite} onSaved={onChanged} {flash} />
				</div>
			{/if}
		</div>
	{/if}
</section>
