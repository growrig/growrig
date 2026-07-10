<script lang="ts">
	import { onMount } from 'svelte';
	import { live } from '$lib/live.svelte';
	import { history } from '$lib/api';
	import type { Reading } from '$lib/types';
	import Sparkline from '$lib/components/Sparkline.svelte';

	const snap = $derived(live.snapshot);
	const environments = $derived(snap?.environments ?? []);
	const devices = $derived(snap?.devices ?? []);

	// History per environment, refreshed slowly for the sparklines.
	let readings = $state<Record<string, Reading[]>>({});

	async function refreshHistory() {
		for (const env of environments) {
			try {
				readings[env.id] = await history(env.id, 120);
			} catch {
				/* Grow Core offline; keep last data */
			}
		}
	}

	onMount(() => {
		refreshHistory();
		const t = setInterval(refreshHistory, 5000);
		return () => clearInterval(t);
	});

	const deviceFor = (envId: string) => devices.filter((d) => d.environmentId === envId);

	const roleLabel: Record<string, string> = {
		exhaust: 'Exhaust',
		intake: 'Intake',
		circulation: 'Circulation',
		unassigned: 'Unassigned'
	};

	function tempTone(temp: number, target: number, emergency: number): string {
		if (temp >= emergency) return 'text-danger';
		if (temp - target >= 2) return 'text-warn';
		return 'text-leaf';
	}

	const healthTone = (health: string) =>
		health === 'online' ? 'bg-leaf/15 text-leaf' : 'bg-danger/15 text-danger';
</script>

{#if !snap}
	<div class="grid place-items-center py-24 text-rig-400">
		<p>Connecting to Grow Core…</p>
	</div>
{:else if environments.length === 0}
	<p class="text-rig-400">No environments configured yet.</p>
{:else}
	<div class="space-y-6">
		{#each environments as env (env.id)}
			{@const rs = readings[env.id] ?? []}
			{@const primary = deviceFor(env.id)[0]}
			<section class="rounded-xl border border-rig-800 bg-rig-900/40 p-5">
				<div class="mb-4 flex items-center justify-between">
					<h2 class="text-lg font-semibold">{env.name}</h2>
					<span class="text-sm text-rig-400">
						target {env.targetTempC}°C · {env.targetHumidity}% RH
					</span>
				</div>

				<div class="grid gap-4 sm:grid-cols-2">
					<div class="rounded-lg border border-rig-800 bg-rig-950/40 p-4">
						<div class="mb-1 flex items-baseline justify-between">
							<span class="text-sm text-rig-400">Temperature</span>
							<span
								class="text-2xl font-semibold tabular-nums {tempTone(
									primary?.tempC ?? env.targetTempC,
									env.targetTempC,
									env.emergencyTempC
								)}"
							>
								{(primary?.tempC ?? 0).toFixed(1)}°C
							</span>
						</div>
						<Sparkline values={rs.map((r) => r.tempC)} target={env.targetTempC} unit="°C" />
					</div>

					<div class="rounded-lg border border-rig-800 bg-rig-950/40 p-4">
						<div class="mb-1 flex items-baseline justify-between">
							<span class="text-sm text-rig-400">Humidity</span>
							<span class="text-2xl font-semibold tabular-nums text-rig-100">
								{(primary?.humidity ?? 0).toFixed(0)}%
							</span>
						</div>
						<Sparkline
							values={rs.map((r) => r.humidity)}
							target={env.targetHumidity}
							color="var(--color-rig-300)"
							unit="%"
						/>
					</div>
				</div>

				{#each deviceFor(env.id) as dev (dev.id)}
					<div class="mt-4">
						<div class="mb-2 flex items-center gap-2">
							<span class="font-medium">{dev.name}</span>
							<span class="rounded-full px-2 py-0.5 text-xs {healthTone(dev.health)}">{dev.health}</span
							>
							<span class="text-xs text-rig-500">{dev.adapter}</span>
						</div>
						<div class="grid gap-3 sm:grid-cols-2">
							{#each dev.channels as ch (ch.id)}
								<div class="rounded-lg border border-rig-800 bg-rig-950/40 p-3">
									<div class="mb-2 flex items-center justify-between text-sm">
										<span class="font-medium">{ch.name}</span>
										<span class="text-rig-400">{roleLabel[ch.role] ?? ch.role}</span>
									</div>
									<div class="mb-1 flex items-center justify-between text-xs text-rig-400">
										<span>speed</span>
										<span class="tabular-nums">{ch.desiredSpeed}% · {ch.rpm} rpm</span>
									</div>
									<div class="h-2 overflow-hidden rounded-full bg-rig-800">
										<div
											class="h-full rounded-full bg-rig-500 transition-all duration-500"
											style="width:{ch.desiredSpeed}%"
										></div>
									</div>
								</div>
							{/each}
						</div>
					</div>
				{/each}
			</section>
		{/each}
	</div>
{/if}
